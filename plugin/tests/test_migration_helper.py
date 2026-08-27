"""Tests for tentaqles.memory.migration.apply_migrations."""

import sqlite3

import pytest

from tentaqles.memory.migration import apply_migrations


def _fresh_conn():
    conn = sqlite3.connect(":memory:")
    conn.execute("CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)")
    conn.commit()
    return conn


def test_apply_new_migration_returns_count():
    conn = _fresh_conn()
    n = apply_migrations(conn, ["ALTER TABLE items ADD COLUMN score REAL DEFAULT 0.0"])
    assert n == 1


def test_duplicate_alter_is_skipped():
    conn = _fresh_conn()
    sql = "ALTER TABLE items ADD COLUMN score REAL DEFAULT 0.0"
    first = apply_migrations(conn, [sql])
    # Running the same migration again must not raise and must return 0.
    second = apply_migrations(conn, [sql])
    assert first == 1
    assert second == 0


def test_already_exists_table_skipped():
    conn = _fresh_conn()
    sql = "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)"
    # items already exists — "already exists" in error message.
    n = apply_migrations(conn, [sql])
    assert n == 0


def test_real_error_is_reraised():
    conn = _fresh_conn()
    with pytest.raises(sqlite3.OperationalError):
        apply_migrations(conn, ["ALTER TABLE nonexistent ADD COLUMN foo TEXT"])


def test_multiple_migrations_mixed():
    conn = _fresh_conn()
    migrations = [
        "ALTER TABLE items ADD COLUMN col1 TEXT",
        "ALTER TABLE items ADD COLUMN col1 TEXT",  # duplicate — skipped
        "ALTER TABLE items ADD COLUMN col2 TEXT",
    ]
    n = apply_migrations(conn, migrations)
    assert n == 2  # first and third applied; second skipped
