"""Tests that Tier-2 schema additions are created correctly by MemoryStore."""

import sqlite3
import tempfile
from pathlib import Path

import pytest

from tentaqles.memory.store import MemoryStore


def _tables(conn: sqlite3.Connection) -> set[str]:
    rows = conn.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall()
    return {r[0] for r in rows}


def _columns(conn: sqlite3.Connection, table: str) -> set[str]:
    rows = conn.execute(f"PRAGMA table_info({table})").fetchall()
    return {r[1] for r in rows}


class TestNewTablesCreated:
    def test_semantic_memories_table_exists(self, tmp_path):
        store = MemoryStore(tmp_path)
        tables = _tables(store._conn)
        assert "semantic_memories" in tables
        store.close()

    def test_procedural_memories_table_exists(self, tmp_path):
        store = MemoryStore(tmp_path)
        tables = _tables(store._conn)
        assert "procedural_memories" in tables
        store.close()

    def test_semantic_memories_columns(self, tmp_path):
        store = MemoryStore(tmp_path)
        cols = _columns(store._conn, "semantic_memories")
        expected = {
            "id", "created_at", "source_sessions", "fact", "category",
            "strength", "recall_count", "last_recalled", "embedding", "tags",
        }
        assert expected.issubset(cols)
        store.close()

    def test_procedural_memories_columns(self, tmp_path):
        store = MemoryStore(tmp_path)
        cols = _columns(store._conn, "procedural_memories")
        expected = {
            "id", "created_at", "workflow_name", "steps", "trigger_pattern",
            "occurrence_count", "last_seen", "strength", "embedding", "tags",
        }
        assert expected.issubset(cols)
        store.close()


class TestMigrationColumns:
    def test_memory_tier_column_added(self, tmp_path):
        store = MemoryStore(tmp_path)
        cols = _columns(store._conn, "sessions")
        assert "memory_tier" in cols
        store.close()

    def test_contradiction_score_column_added(self, tmp_path):
        store = MemoryStore(tmp_path)
        cols = _columns(store._conn, "decisions")
        assert "contradiction_score" in cols
        store.close()

    def test_opening_existing_db_twice_no_error(self, tmp_path):
        """Opening the same workspace twice must not raise (idempotent migrations)."""
        store1 = MemoryStore(tmp_path)
        store1.close()
        # Second open applies migrations again — must be a no-op.
        store2 = MemoryStore(tmp_path)
        store2.close()

    def test_legacy_db_without_memory_tier(self, tmp_path):
        """Simulate a legacy database missing the memory_tier column."""
        db_path = tmp_path / ".claude" / "memory.db"
        db_path.parent.mkdir(parents=True)

        # Create a minimal database without memory_tier
        conn = sqlite3.connect(str(db_path))
        conn.executescript(
            """
            CREATE TABLE sessions (
                id TEXT PRIMARY KEY,
                started_at TEXT NOT NULL,
                ended_at TEXT,
                duration_s INTEGER,
                summary TEXT,
                embedding BLOB,
                tags TEXT DEFAULT '[]',
                metadata TEXT DEFAULT '{}'
            );
            CREATE TABLE decisions (
                id TEXT PRIMARY KEY,
                session_id TEXT,
                created_at TEXT NOT NULL,
                node_ids TEXT NOT NULL DEFAULT '[]',
                chosen TEXT NOT NULL,
                rejected TEXT DEFAULT '[]',
                rationale TEXT NOT NULL,
                confidence TEXT DEFAULT 'medium',
                status TEXT DEFAULT 'active',
                superseded_by TEXT,
                embedding BLOB,
                tags TEXT DEFAULT '[]'
            );
            CREATE TABLE touches (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                session_id TEXT NOT NULL,
                node_id TEXT NOT NULL,
                node_type TEXT NOT NULL,
                touched_at TEXT NOT NULL,
                action TEXT NOT NULL,
                weight REAL DEFAULT 1.0
            );
            CREATE TABLE pending (
                id TEXT PRIMARY KEY,
                session_id TEXT,
                created_at TEXT NOT NULL,
                description TEXT NOT NULL,
                node_ids TEXT DEFAULT '[]',
                priority TEXT DEFAULT 'medium',
                resolved_at TEXT,
                resolved_by TEXT
            );
            """
        )
        conn.commit()
        conn.close()

        # Now open via MemoryStore — must add missing column without error.
        store = MemoryStore(tmp_path)
        cols = _columns(store._conn, "sessions")
        assert "memory_tier" in cols
        store.close()
