"""Tests that MetaMemory creates the signals table on init."""

import sqlite3
import tempfile
from pathlib import Path

import pytest

from tentaqles.memory.meta import MetaMemory


def _tables(conn: sqlite3.Connection) -> set[str]:
    rows = conn.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall()
    return {r[0] for r in rows}


def _columns(conn: sqlite3.Connection, table: str) -> set[str]:
    rows = conn.execute(f"PRAGMA table_info({table})").fetchall()
    return {r[1] for r in rows}


class TestMetaSignalsSchema:
    def test_signals_table_created(self, tmp_path):
        db_path = tmp_path / "meta.db"
        meta = MetaMemory(db_path=db_path)
        tables = _tables(meta._conn)
        assert "signals" in tables
        meta.close()

    def test_signals_columns(self, tmp_path):
        db_path = tmp_path / "meta.db"
        meta = MetaMemory(db_path=db_path)
        cols = _columns(meta._conn, "signals")
        expected = {
            "id", "from_workspace", "to_workspace", "event_type",
            "payload", "message", "emitted_at", "expires_at",
            "read_by", "read_at",
        }
        assert expected.issubset(cols)
        meta.close()

    def test_workspace_status_table_still_exists(self, tmp_path):
        """Ensure existing workspace_status table is not broken by signals addition."""
        db_path = tmp_path / "meta.db"
        meta = MetaMemory(db_path=db_path)
        assert "workspace_status" in _tables(meta._conn)
        meta.close()

    def test_reinit_is_idempotent(self, tmp_path):
        """Opening MetaMemory twice on the same db must not raise."""
        db_path = tmp_path / "meta.db"
        meta1 = MetaMemory(db_path=db_path)
        meta1.close()
        meta2 = MetaMemory(db_path=db_path)
        meta2.close()

    def test_can_insert_signal_row(self, tmp_path):
        db_path = tmp_path / "meta.db"
        meta = MetaMemory(db_path=db_path)
        meta._conn.execute(
            "INSERT INTO signals (id, from_workspace, to_workspace, event_type, "
            "emitted_at, expires_at) VALUES ('sig1', 'ws_a', 'ws_b', 'alert', "
            "datetime('now'), datetime('now', '+48 hours'))"
        )
        meta._conn.commit()
        row = meta._conn.execute("SELECT id FROM signals WHERE id='sig1'").fetchone()
        assert row is not None
        meta.close()
