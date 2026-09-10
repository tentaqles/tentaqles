#!/usr/bin/env python3
"""Repair session rows that the SessionEnd hook left half-written.

Claude Code budgets *all* SessionEnd hooks with one shared abort signal —
1500ms by default, and a plugin's declared `timeout` does not raise it. Before
the interpreter caching in tq_env.sh, the hook regularly ran out of budget
between `start_session()` and `end_session()`, committing a row with a
`started_at` but no `ended_at` and no summary. Those rows are what makes the
session preamble say "Last session (?, ...)".

This backfills them from the transcript each row names in its metadata, using
the same parser the hook uses, so recovered summaries match new ones.

Dry run (default) — shows what would change:

    tq_run.sh repair-sessions.py --all

Apply, backing up each database first:

    tq_run.sh repair-sessions.py --all --apply

Rows whose transcript is gone cannot be reconstructed; list them with
--show-unrecoverable, and drop the empty ones (no touches, decisions or
pending items attached) with --prune.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import argparse
import glob
import json
import shutil
import sqlite3
from datetime import datetime, timedelta, timezone
from pathlib import Path

from tentaqles.transcript import build_summary, parse_transcript

try:
    from tentaqles.privacy import redact_text
except ImportError:  # pragma: no cover - graceful fallback
    def redact_text(text):
        return text, []


def find_transcript_roots() -> list[Path]:
    """Directories that may hold Claude Code JSONL transcripts."""
    roots: list[Path] = []
    cfg = os.environ.get("CLAUDE_CONFIG_DIR")
    if cfg:
        roots.append(Path(cfg) / "projects")
    home = Path.home()
    roots.append(home / ".claude" / "projects")
    roots.extend(Path(p) for p in glob.glob(str(home / ".tentaqles" / "identities" / "*" / "claude" / "projects")))
    return [r for r in roots if r.is_dir()]


def index_transcripts(roots: list[Path]) -> dict[str, Path]:
    """Map session_id -> transcript path (newest wins on collision)."""
    index: dict[str, Path] = {}
    for root in roots:
        for path in root.glob("*/*.jsonl"):
            sid = path.stem
            prev = index.get(sid)
            if prev is None or path.stat().st_mtime > prev.stat().st_mtime:
                index[sid] = path
    return index


def orphaned_sessions(conn: sqlite3.Connection) -> list[tuple[str, str, str]]:
    """Rows with a start but no ending — (id, started_at, metadata)."""
    return list(conn.execute(
        "SELECT id, started_at, metadata FROM sessions "
        "WHERE ended_at IS NULL OR summary IS NULL"
    ))


def _session_id_of(metadata: str) -> str:
    try:
        return json.loads(metadata or "{}").get("session_id") or ""
    except Exception:
        return ""


def _ended_at(started_at: str, duration_s: int) -> str:
    """Reconstruct an end timestamp from the start plus measured duration.

    Using "now" here would be wrong by however long the row sat orphaned —
    that is where the nonsensical multi-thousand-minute durations came from.
    """
    try:
        started = datetime.fromisoformat(started_at)
        if started.tzinfo is None:
            started = started.replace(tzinfo=timezone.utc)
        return (started + timedelta(seconds=max(0, duration_s))).isoformat()
    except Exception:
        return started_at


def _is_empty(conn: sqlite3.Connection, sid: str) -> bool:
    """True when nothing else in the database references this session."""
    for table in ("touches", "decisions", "pending"):
        try:
            n = conn.execute(
                f"SELECT COUNT(*) FROM {table} WHERE session_id = ?", (sid,)
            ).fetchone()[0]
        except sqlite3.Error:
            continue  # table absent in this schema version
        if n:
            return False
    return True


def repair_db(db_path: Path, index: dict[str, Path], apply: bool,
              prune: bool, show_unrecoverable: bool) -> dict:
    stats = {"orphans": 0, "repaired": 0, "unrecoverable": 0, "pruned": 0}
    conn = sqlite3.connect(str(db_path))
    try:
        orphans = orphaned_sessions(conn)
        stats["orphans"] = len(orphans)
        if not orphans:
            return stats

        if apply:
            backup = db_path.with_suffix(db_path.suffix + ".pre-repair")
            if not backup.exists():
                shutil.copy2(db_path, backup)

        # Group by the Claude session the row came from. One Claude session
        # fires SessionEnd more than once (clear, resume, exit), so several
        # orphan rows can share a transcript — repairing each would write the
        # same session into memory repeatedly. Keep the earliest, treat the
        # rest as duplicates.
        by_claude_sid: dict[str, list[tuple[str, str]]] = {}
        unrecoverable: list[str] = []
        for sid, started_at, metadata in orphans:
            claude_sid = _session_id_of(metadata)
            transcript = index.get(claude_sid) if claude_sid else None
            if transcript is None or not transcript.exists():
                unrecoverable.append(sid)
                continue
            by_claude_sid.setdefault(claude_sid, []).append((started_at or "", sid))

        duplicates: list[str] = []
        for claude_sid, rows in by_claude_sid.items():
            rows.sort()  # earliest started_at first
            keep_started, keep_sid = rows[0]
            duplicates.extend(sid for _, sid in rows[1:])

            transcript = index[claude_sid]
            sid, started_at = keep_sid, keep_started
            activity = parse_transcript(str(transcript))
            summary = build_summary(activity)
            try:
                summary, _ = redact_text(summary)
            except Exception:
                pass

            duration = activity.get("duration_s", 0)
            if apply:
                conn.execute(
                    "UPDATE sessions SET ended_at=?, duration_s=?, summary=?, "
                    "memory_tier='episodic' WHERE id=?",
                    (_ended_at(started_at, duration), duration, summary, sid),
                )
            stats["repaired"] += 1

        # Duplicates and transcript-less rows are both dead weight; drop the
        # ones nothing else references, keep any that still own touches,
        # decisions or pending items so those are not left dangling.
        for sid, reason in ([(s, "duplicate") for s in duplicates]
                            + [(s, "no transcript on disk") for s in unrecoverable]):
            if prune and _is_empty(conn, sid):
                if apply:
                    conn.execute("DELETE FROM sessions WHERE id = ?", (sid,))
                stats["pruned"] += 1
            else:
                stats["unrecoverable"] += 1
                if show_unrecoverable:
                    print(f"    kept: {sid} ({reason}, has attached data)"
                          if prune else f"    unrecoverable: {sid} ({reason})")

        if apply:
            conn.commit()
    finally:
        conn.close()
    return stats


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("workspaces", nargs="*", help="workspace roots holding .claude/memory.db")
    ap.add_argument("--all", action="store_true",
                    help="scan every workspace under the configured workspaces root")
    ap.add_argument("--apply", action="store_true",
                    help="write changes (default is a dry run); backs up each db first")
    ap.add_argument("--prune", action="store_true",
                    help="delete unrecoverable rows that have no touches/decisions/pending")
    ap.add_argument("--show-unrecoverable", action="store_true")
    args = ap.parse_args()

    dbs: list[Path] = []
    if args.all:
        root = os.environ.get("TENTAQLES_WORKSPACES_ROOT") or str(Path("C:/repos") if os.name == "nt" else Path.home() / "repos")
        dbs = [Path(p) for p in glob.glob(os.path.join(root, "**", ".claude", "memory.db"), recursive=True)]
    for ws in args.workspaces:
        candidate = Path(ws) / ".claude" / "memory.db"
        if candidate.exists():
            dbs.append(candidate)
    dbs = sorted({d.resolve() for d in dbs})

    if not dbs:
        print("No memory.db found. Pass workspace roots explicitly, or use --all.")
        return 1

    index = index_transcripts(find_transcript_roots())
    print(f"Indexed {len(index)} transcripts")
    print("DRY RUN — nothing written (use --apply)\n" if not args.apply else "APPLYING changes\n")

    totals = {"orphans": 0, "repaired": 0, "unrecoverable": 0, "pruned": 0}
    for db in dbs:
        s = repair_db(db, index, args.apply, args.prune, args.show_unrecoverable)
        for k in totals:
            totals[k] += s[k]
        if s["orphans"]:
            print(f"  {db}\n    orphans={s['orphans']} repairable={s['repaired']} "
                  f"unrecoverable={s['unrecoverable']} prunable={s['pruned']}")

    print(f"\nTOTAL orphans={totals['orphans']} repaired={totals['repaired']} "
          f"unrecoverable={totals['unrecoverable']} pruned={totals['pruned']}")
    if not args.apply and totals["repaired"]:
        print("Re-run with --apply to write these repairs.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
