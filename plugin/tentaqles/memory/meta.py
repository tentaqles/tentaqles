"""Cross-workspace memory — aggregates temporal data across all client workspaces."""

from __future__ import annotations

import json
import os
import sqlite3
import time
from datetime import datetime, timezone
from pathlib import Path

# Lazy path resolution — computed on first use so env vars can be set after import
_meta_db_path: Path | None = None


def _get_meta_db_path() -> Path:
    global _meta_db_path
    if _meta_db_path is None:
        from tentaqles.config import meta_db_path
        _meta_db_path = meta_db_path()
    return _meta_db_path

_META_SCHEMA = """
CREATE TABLE IF NOT EXISTS workspace_status (
    workspace_id    TEXT PRIMARY KEY,
    display_name    TEXT,
    root_path       TEXT,
    last_active     TEXT,
    last_summary    TEXT,
    active_nodes    TEXT DEFAULT '[]',
    session_count   INTEGER DEFAULT 0,
    total_touches   INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS signals (
    id              TEXT PRIMARY KEY,
    from_workspace  TEXT NOT NULL,
    to_workspace    TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    payload         TEXT NOT NULL DEFAULT '{}',
    message         TEXT,
    emitted_at      TEXT NOT NULL,
    expires_at      TEXT NOT NULL,
    read_by         TEXT DEFAULT NULL,
    read_at         TEXT DEFAULT NULL
);
CREATE INDEX IF NOT EXISTS idx_signals_to ON signals(to_workspace, read_by, expires_at);
CREATE INDEX IF NOT EXISTS idx_signals_from ON signals(from_workspace, emitted_at DESC);
"""


class MetaMemory:
    """Cross-workspace memory aggregator.

    Stores lightweight summaries from each workspace — never stores code,
    file paths, or client-specific details beyond workspace-level stats.
    """

    def __init__(self, db_path: Path | None = None):
        if db_path is None:
            db_path = _get_meta_db_path()
        self._conn = sqlite3.connect(str(db_path))
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.executescript(_META_SCHEMA)
        self._conn.commit()

    def update_workspace(
        self,
        workspace_id: str,
        display_name: str,
        root_path: str,
        summary: str,
        active_nodes: list[str],
        session_count: int = 0,
        total_touches: int = 0,
    ) -> None:
        """Update workspace status after a session ends."""
        self._conn.execute(
            """INSERT INTO workspace_status
               (workspace_id, display_name, root_path, last_active, last_summary, active_nodes, session_count, total_touches)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?)
               ON CONFLICT(workspace_id) DO UPDATE SET
                   display_name=excluded.display_name,
                   root_path=excluded.root_path,
                   last_active=excluded.last_active,
                   last_summary=excluded.last_summary,
                   active_nodes=excluded.active_nodes,
                   session_count=excluded.session_count,
                   total_touches=excluded.total_touches
            """,
            (
                workspace_id,
                display_name,
                root_path,
                datetime.now(timezone.utc).isoformat(),
                summary,
                json.dumps(active_nodes[:20]),  # cap at 20 to keep meta lightweight
                session_count,
                total_touches,
            ),
        )
        self._conn.commit()

    def get_all_status(self) -> list[dict]:
        """Get status of all workspaces — the 'what have I been doing' view."""
        rows = self._conn.execute(
            "SELECT workspace_id, display_name, last_active, last_summary, active_nodes, session_count, total_touches FROM workspace_status ORDER BY last_active DESC"
        ).fetchall()
        return [
            {
                "workspace_id": r[0],
                "display_name": r[1],
                "last_active": r[2],
                "last_summary": r[3],
                "active_nodes": json.loads(r[4] or "[]"),
                "session_count": r[5],
                "total_touches": r[6],
            }
            for r in rows
        ]

    def get_workspace_summary(self, workspace_id: str) -> dict | None:
        row = self._conn.execute(
            "SELECT workspace_id, display_name, last_active, last_summary, active_nodes, session_count, total_touches FROM workspace_status WHERE workspace_id=?",
            (workspace_id,),
        ).fetchone()
        if not row:
            return None
        return {
            "workspace_id": row[0],
            "display_name": row[1],
            "last_active": row[2],
            "last_summary": row[3],
            "active_nodes": json.loads(row[4] or "[]"),
            "session_count": row[5],
            "total_touches": row[6],
        }

    def get_cross_workspace_context(self, max_tokens: int = 300) -> str:
        """Generate a brief cross-workspace status summary."""
        statuses = self.get_all_status()
        if not statuses:
            return "No workspace activity recorded yet."

        lines = ["## Workspace activity"]
        for ws in statuses[:5]:
            last = ws["last_active"][:10] if ws["last_active"] else "never"
            nodes = ", ".join(ws["active_nodes"][:3]) if ws["active_nodes"] else "none"
            lines.append(f"- **{ws['display_name']}** (last: {last}, {ws['session_count']} sessions)")
            if ws["last_summary"]:
                lines.append(f"  {ws['last_summary'][:120]}")
            if nodes != "none":
                lines.append(f"  Hot: {nodes}")

        # Append cross-workspace patterns if patterns.json exists and is < 7 days old
        try:
            from tentaqles.config import data_dir
            patterns_path = os.path.join(data_dir(), "metagraph", "patterns.json")
            if (
                os.path.exists(patterns_path)
                and (time.time() - os.path.getmtime(patterns_path)) < 7 * 86400
            ):
                patterns = self.get_patterns()
                if patterns:
                    lines.append("\n## Cross-workspace patterns")
                    for p in patterns[:5]:
                        lines.append(
                            f"- **{p.get('label', 'unknown')}** "
                            f"({p.get('decision_count', 0)} decisions across "
                            f"{len(p.get('workspaces', []))} workspaces)"
                        )
        except Exception:
            pass

        text = "\n".join(lines)
        if len(text) > max_tokens * 4:
            text = text[: max_tokens * 4] + "\n..."
        return text

    def get_pending_signals(self, workspace_id: str) -> list[dict]:
        """Return unread, non-expired signals directed at workspace_id via SignalBus."""
        from tentaqles.memory.signals import SignalBus
        bus = SignalBus(_get_meta_db_path())
        return bus.read_pending(workspace_id)

    def get_patterns(self) -> list[dict]:
        """Return cross-workspace patterns from patterns.json via CrossWorkspacePatternDetector."""
        from tentaqles.memory.pattern_detector import CrossWorkspacePatternDetector
        return CrossWorkspacePatternDetector().load_patterns()

    def close(self):
        self._conn.close()
