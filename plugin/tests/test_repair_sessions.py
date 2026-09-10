"""Tests for scripts/repair-sessions.py.

The repair tool rewrites rows in live client memory.db files, so its rules are
pinned here: only orphans are touched, durations come from the transcript
rather than wall-clock now, duplicate rows from one Claude session collapse to
one, and rows that still own touches/decisions/pending are never deleted.
"""

from __future__ import annotations

import importlib.util
import json
import sqlite3
import sys
from pathlib import Path

import pytest


SCRIPTS = Path(__file__).resolve().parent.parent / "scripts"
_REPAIR = None


@pytest.fixture(scope="module")
def repair():
    """Load scripts/repair-sessions.py, protecting pytest's stdio capture."""
    global _REPAIR
    if _REPAIR is None:
        spec = importlib.util.spec_from_file_location("repair_sessions_mod",
                                                      SCRIPTS / "repair-sessions.py")
        mod = importlib.util.module_from_spec(spec)
        saved_out, saved_err = sys.stdout, sys.stderr
        try:
            spec.loader.exec_module(mod)
        finally:
            for stream, original in ((sys.stdout, saved_out), (sys.stderr, saved_err)):
                if stream is not original and hasattr(stream, "detach"):
                    try:
                        stream.detach()
                    except Exception:
                        pass
            sys.stdout, sys.stderr = saved_out, saved_err
        _REPAIR = mod
    return _REPAIR


SCHEMA = """
CREATE TABLE sessions (id TEXT PRIMARY KEY, started_at TEXT, ended_at TEXT,
                       duration_s INTEGER, summary TEXT, embedding BLOB,
                       tags TEXT, metadata TEXT, memory_tier TEXT);
CREATE TABLE touches (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT, node_id TEXT);
CREATE TABLE decisions (id TEXT PRIMARY KEY, session_id TEXT);
CREATE TABLE pending (id TEXT PRIMARY KEY, session_id TEXT);
"""


def _make_db(tmp_path: Path, rows: list[tuple]) -> Path:
    db = tmp_path / "memory.db"
    conn = sqlite3.connect(db)
    conn.executescript(SCHEMA)
    conn.executemany(
        "INSERT INTO sessions (id, started_at, ended_at, summary, metadata) VALUES (?,?,?,?,?)",
        rows,
    )
    conn.commit()
    conn.close()
    return db


def _make_transcript(tmp_path: Path, claude_sid: str) -> Path:
    d = tmp_path / "projects" / "proj"
    d.mkdir(parents=True, exist_ok=True)
    p = d / f"{claude_sid}.jsonl"
    p.write_text("\n".join(json.dumps(e) for e in [
        {"type": "user", "timestamp": "2026-01-01T00:00:00+00:00",
         "message": {"role": "user", "content": [{"type": "text", "text": "Fix the billing rounding bug please"}]}},
        {"type": "assistant", "timestamp": "2026-01-01T00:20:00+00:00",
         "message": {"role": "assistant", "content": [
             {"type": "tool_use", "name": "Edit", "input": {"file_path": "billing.py"}}]}},
    ]), encoding="utf-8")
    return p


def _sessions(db: Path) -> list[sqlite3.Row]:
    conn = sqlite3.connect(db)
    conn.row_factory = sqlite3.Row
    out = list(conn.execute("SELECT * FROM sessions ORDER BY started_at"))
    conn.close()
    return out


def test_repairs_orphan_from_transcript(repair, tmp_path: Path) -> None:
    meta = json.dumps({"session_id": "abc-123", "auto": True})
    db = _make_db(tmp_path, [("row1", "2026-01-01T00:00:00+00:00", None, None, meta)])
    index = {"abc-123": _make_transcript(tmp_path, "abc-123")}

    stats = repair.repair_db(db, index, apply=True, prune=False, show_unrecoverable=False)
    assert stats["repaired"] == 1

    row = _sessions(db)[0]
    assert row["summary"] and "no file changes" not in row["summary"]
    assert "billing.py" in row["summary"]
    # Duration comes from the transcript (00:00 → 00:20), not from "now".
    assert row["duration_s"] == 1200
    assert row["ended_at"].startswith("2026-01-01T00:20")
    assert row["memory_tier"] == "episodic"


def test_completed_sessions_are_left_alone(repair, tmp_path: Path) -> None:
    meta = json.dumps({"session_id": "abc-123"})
    db = _make_db(tmp_path, [
        ("done", "2026-01-01T00:00:00+00:00", "2026-01-01T00:05:00+00:00", "already summarised", meta),
    ])
    index = {"abc-123": _make_transcript(tmp_path, "abc-123")}
    stats = repair.repair_db(db, index, apply=True, prune=False, show_unrecoverable=False)
    assert stats["orphans"] == 0
    assert _sessions(db)[0]["summary"] == "already summarised"


def test_duplicate_rows_collapse_to_one(repair, tmp_path: Path) -> None:
    """One Claude session fires SessionEnd several times; keep the earliest."""
    meta = json.dumps({"session_id": "dup-1", "auto": True})
    db = _make_db(tmp_path, [
        ("early", "2026-01-01T00:00:00+00:00", None, None, meta),
        ("later", "2026-01-01T09:00:00+00:00", None, None, meta),
    ])
    index = {"dup-1": _make_transcript(tmp_path, "dup-1")}

    stats = repair.repair_db(db, index, apply=True, prune=True, show_unrecoverable=False)
    assert stats["repaired"] == 1
    assert stats["pruned"] == 1

    rows = _sessions(db)
    assert [r["id"] for r in rows] == ["early"]
    assert rows[0]["summary"]


def test_rows_with_attached_data_are_never_deleted(repair, tmp_path: Path) -> None:
    meta = json.dumps({"session_id": "gone-forever", "auto": True})
    db = _make_db(tmp_path, [("keeper", "2026-01-01T00:00:00+00:00", None, None, meta)])
    conn = sqlite3.connect(db)
    conn.execute("INSERT INTO touches (session_id, node_id) VALUES ('keeper', 'src/app.py')")
    conn.commit()
    conn.close()

    stats = repair.repair_db(db, index={}, apply=True, prune=True, show_unrecoverable=False)
    assert stats["pruned"] == 0
    assert stats["unrecoverable"] == 1
    assert [r["id"] for r in _sessions(db)] == ["keeper"]


def test_empty_unrecoverable_rows_pruned_only_with_flag(repair, tmp_path: Path) -> None:
    meta = json.dumps({"session_id": "gone", "auto": True})
    rows = [("shell", "2026-01-01T00:00:00+00:00", None, None, meta)]

    without_flag = tmp_path / "a"
    without_flag.mkdir()
    db = _make_db(without_flag, rows)
    stats = repair.repair_db(db, index={}, apply=True, prune=False, show_unrecoverable=False)
    assert stats["pruned"] == 0 and stats["unrecoverable"] == 1
    assert len(_sessions(db)) == 1

    with_flag = tmp_path / "b"
    with_flag.mkdir()
    db2 = _make_db(with_flag, rows)
    stats2 = repair.repair_db(db2, index={}, apply=True, prune=True, show_unrecoverable=False)
    assert stats2["pruned"] == 1
    assert _sessions(db2) == []


def test_dry_run_writes_nothing(repair, tmp_path: Path) -> None:
    meta = json.dumps({"session_id": "abc-123", "auto": True})
    db = _make_db(tmp_path, [("row1", "2026-01-01T00:00:00+00:00", None, None, meta)])
    index = {"abc-123": _make_transcript(tmp_path, "abc-123")}

    stats = repair.repair_db(db, index, apply=False, prune=True, show_unrecoverable=False)
    assert stats["repaired"] == 1  # reported...
    assert _sessions(db)[0]["summary"] is None  # ...but not written
    assert not db.with_suffix(db.suffix + ".pre-repair").exists()


def test_apply_backs_up_the_database(repair, tmp_path: Path) -> None:
    meta = json.dumps({"session_id": "abc-123", "auto": True})
    db = _make_db(tmp_path, [("row1", "2026-01-01T00:00:00+00:00", None, None, meta)])
    index = {"abc-123": _make_transcript(tmp_path, "abc-123")}

    repair.repair_db(db, index, apply=True, prune=False, show_unrecoverable=False)
    backup = db.with_suffix(db.suffix + ".pre-repair")
    assert backup.exists()
    conn = sqlite3.connect(backup)
    assert conn.execute("SELECT summary FROM sessions WHERE id='row1'").fetchone()[0] is None
    conn.close()
