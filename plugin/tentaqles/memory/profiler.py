"""Workspace Profiles — Feature 10.

Mines memory.db to build a learned profile of a workspace: hot files,
session frequency, concept clusters, and commit velocity.

Profile is written to {workspace}/.claude/profile.json and refreshed when
stale (default: older than 7 days).  No new SQLite tables are required;
all queries use the existing `touches`, `sessions`, and `decisions` tables
plus `semantic_memories` when available.
"""

from __future__ import annotations

import json
import math
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from tentaqles.memory.store import MemoryStore

_PROFILE_SCHEMA = "tentaqles-profile-v1"


def _now_utc() -> str:
    return datetime.now(timezone.utc).isoformat()


def _days_since(iso_ts: str) -> float:
    """Return days elapsed since an ISO-8601 timestamp."""
    try:
        dt = datetime.fromisoformat(iso_ts)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        delta = datetime.now(timezone.utc) - dt
        return delta.total_seconds() / 86400.0
    except Exception:
        return 0.0


class WorkspaceProfiler:
    """Compute and cache a learned workspace profile.

    Args:
        store: An open :class:`~tentaqles.memory.store.MemoryStore` instance.
        workspace_path: Root directory of the workspace. The profile is
            written to ``{workspace_path}/.claude/profile.json``.
    """

    def __init__(self, store: "MemoryStore", workspace_path: str | Path) -> None:
        self._store = store
        self._workspace_path = Path(workspace_path)
        self._profile_path = self._workspace_path / ".claude" / "profile.json"

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def generate(self) -> dict:
        """Compute the full workspace profile, persist it, and return it."""
        workspace_name = self._workspace_path.name

        hot_files = self._compute_hot_files()
        session_freq = self._compute_session_frequency()
        concepts = self._compute_concept_clusters()
        commit_vel = self._compute_commit_velocity()

        # Build a short summary sentence
        parts: list[str] = []
        if session_freq:
            n = session_freq.get("sessions_last_30d", 0)
            parts.append(f"{n} sessions in 30 days")
        if hot_files:
            top = hot_files[0]["path"]
            parts.append(f"Hot: {top}")
        if concepts:
            label = concepts[0].get("label", "")
            if label:
                parts.append(f"Main theme: {label}")

        summary = (
            f"Active workspace, {', '.join(parts)}." if parts else "No activity recorded yet."
        )

        profile: dict = {
            "schema": _PROFILE_SCHEMA,
            "generated_at": _now_utc(),
            "workspace": workspace_name,
            "session_frequency": session_freq,
            "hot_files": hot_files,
            "top_concepts": concepts,
            "commit_velocity": commit_vel,
            "function_activity": [],
            "summary_sentence": summary,
        }

        # Write profile.json (create parent dirs if needed)
        self._profile_path.parent.mkdir(parents=True, exist_ok=True)
        self._profile_path.write_text(json.dumps(profile, indent=2), encoding="utf-8")

        return profile

    def load(self) -> dict | None:
        """Read and return the persisted profile, or None if it does not exist."""
        if not self._profile_path.exists():
            return None
        try:
            return json.loads(self._profile_path.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError):
            return None

    def is_stale(self, max_age_days: float = 7.0) -> bool:
        """Return True if the profile is missing or older than *max_age_days*."""
        if not self._profile_path.exists():
            return True
        profile = self.load()
        if not profile:
            return True
        generated_at = profile.get("generated_at", "")
        if not generated_at:
            return True
        return _days_since(generated_at) > max_age_days

    # ------------------------------------------------------------------
    # Private compute methods
    # ------------------------------------------------------------------

    def _compute_hot_files(self, limit: int = 15) -> list[dict]:
        """Return top *limit* files ranked by decay-weighted touch score.

        Score = SUM(weight * exp(-days_since_touch / 30)) per file node.
        Trend: "rising" if last-7d touches > prior-7d touches, else
        "stable" or "falling".
        """
        conn = self._store._conn
        half_life = 30.0

        rows = conn.execute(
            f"""
            SELECT
                node_id,
                COUNT(*) AS touch_count,
                SUM(weight * exp(-(julianday('now') - julianday(touched_at)) / {half_life:.1f})) AS score,
                SUM(CASE WHEN julianday('now') - julianday(touched_at) <= 7
                         THEN 1 ELSE 0 END) AS recent_7d,
                SUM(CASE WHEN julianday('now') - julianday(touched_at) > 7
                              AND julianday('now') - julianday(touched_at) <= 14
                         THEN 1 ELSE 0 END) AS prior_7d
            FROM touches
            WHERE node_type = 'file'
            GROUP BY node_id
            ORDER BY score DESC
            LIMIT ?
            """,
            (limit,),
        ).fetchall()

        result = []
        for node_id, touch_count, score, recent_7d, prior_7d in rows:
            if prior_7d and prior_7d > 0:
                ratio = recent_7d / prior_7d
                trend = "rising" if ratio > 1.5 else ("falling" if ratio < 0.5 else "stable")
            else:
                trend = "rising" if recent_7d > 0 else "stable"

            result.append(
                {
                    "path": node_id,
                    "score": round(score or 0.0, 3),
                    "touch_count": touch_count,
                    "trend": trend,
                }
            )

        return result

    def _compute_session_frequency(self, weeks: int = 12) -> dict:
        """Compute session frequency stats from the *sessions* table.

        Returns a dict with:
        - ``sessions_last_30d`` — count of completed sessions in last 30 days
        - ``sessions_per_week_avg`` — average sessions per week over *weeks*
        - ``most_active_hour`` — UTC hour with the most session starts (0–23)
        """
        conn = self._store._conn

        # Sessions in last 30 days (completed)
        row = conn.execute(
            "SELECT COUNT(*) FROM sessions "
            "WHERE ended_at IS NOT NULL "
            "AND julianday('now') - julianday(started_at) <= 30"
        ).fetchone()
        sessions_30d: int = row[0] if row else 0

        # Sessions per week average over the window
        row2 = conn.execute(
            "SELECT COUNT(*) FROM sessions "
            "WHERE ended_at IS NOT NULL "
            "AND julianday('now') - julianday(started_at) <= ?",
            (weeks * 7,),
        ).fetchone()
        total_in_window: int = row2[0] if row2 else 0
        sessions_per_week = round(total_in_window / weeks, 2) if weeks > 0 else 0.0

        # Most active hour (extract hour from started_at)
        hour_rows = conn.execute(
            "SELECT CAST(strftime('%H', started_at) AS INTEGER) AS hr, COUNT(*) AS cnt "
            "FROM sessions WHERE started_at IS NOT NULL "
            "GROUP BY hr ORDER BY cnt DESC LIMIT 1"
        ).fetchone()
        most_active_hour: int = hour_rows[0] if hour_rows else 0

        return {
            "sessions_last_30d": sessions_30d,
            "sessions_per_week_avg": sessions_per_week,
            "most_active_hour": most_active_hour,
        }

    def _compute_concept_clusters(self, n_clusters: int = 5) -> list[dict]:
        """Return top-N concept clusters.

        Strategy (no scipy required):
        1. If ``semantic_memories`` has rows, return top-N by
           ``strength * exp(-days_since_created / 30)`` as proxy for
           relevance.
        2. Else, return the top-N most-tagged active decisions as concepts.

        Returns ``[{"label", "decision_count", "representative"}, ...]``.
        """
        conn = self._store._conn

        # --- Strategy 1: semantic_memories table ---
        sem_rows = conn.execute(
            """
            SELECT fact, category, strength,
                   julianday('now') - julianday(created_at) AS age_days,
                   tags
            FROM semantic_memories
            ORDER BY strength * exp(-(julianday('now') - julianday(created_at)) / 30.0) DESC
            LIMIT ?
            """,
            (n_clusters,),
        ).fetchall()

        if sem_rows:
            result = []
            for fact, category, strength, age_days, tags_json in sem_rows:
                tags = []
                try:
                    tags = json.loads(tags_json or "[]")
                except (json.JSONDecodeError, TypeError):
                    pass
                label = category or (tags[0] if tags else "general")
                result.append(
                    {
                        "label": label,
                        "decision_count": 1,
                        "representative": fact[:200] if fact else "",
                    }
                )
            return result

        # --- Strategy 2: top-N most-tagged active decisions ---
        dec_rows = conn.execute(
            """
            SELECT chosen, rationale, tags
            FROM decisions
            WHERE status = 'active' AND tags != '[]'
            ORDER BY created_at DESC
            LIMIT 50
            """
        ).fetchall()

        if not dec_rows:
            # Fall back to any recent active decisions
            dec_rows = conn.execute(
                "SELECT chosen, rationale, tags FROM decisions "
                "WHERE status = 'active' ORDER BY created_at DESC LIMIT 50"
            ).fetchall()

        # Count decisions per tag label
        tag_counts: dict[str, int] = {}
        tag_reps: dict[str, str] = {}
        for chosen, rationale, tags_json in dec_rows:
            try:
                tags = json.loads(tags_json or "[]")
            except (json.JSONDecodeError, TypeError):
                tags = []
            if not tags:
                tags = ["general"]
            for tag in tags:
                tag_counts[tag] = tag_counts.get(tag, 0) + 1
                if tag not in tag_reps:
                    tag_reps[tag] = chosen[:200] if chosen else ""

        # Sort by frequency, take top n_clusters
        sorted_tags = sorted(tag_counts.items(), key=lambda x: x[1], reverse=True)
        result = []
        for label, count in sorted_tags[:n_clusters]:
            result.append(
                {
                    "label": label,
                    "decision_count": count,
                    "representative": tag_reps.get(label, ""),
                }
            )
        return result

    def _compute_commit_velocity(self) -> dict | None:
        """Return commit velocity over the last 30 days using ``git log``.

        Returns ``{"commits_30d": int, "commits_per_week_avg": float}``
        or ``None`` if the workspace is not a git repository or git is not
        available.
        """
        try:
            result = subprocess.run(
                ["git", "-C", str(self._workspace_path), "log", "--oneline", "--since=30.days.ago"],
                capture_output=True,
                text=True,
                timeout=10,
            )
            if result.returncode != 0:
                return None
            lines = [ln for ln in result.stdout.splitlines() if ln.strip()]
            commits_30d = len(lines)
            commits_per_week = round(commits_30d / 4.0, 2)
            return {
                "commits_30d": commits_30d,
                "commits_per_week_avg": commits_per_week,
            }
        except Exception:
            return None
