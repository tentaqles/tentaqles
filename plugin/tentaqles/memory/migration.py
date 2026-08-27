"""Migration helpers for SQLite schema evolution.

Provides a simple, idempotent way to apply a list of SQL migration strings
against an open sqlite3 connection, skipping statements that would fail
because the change already exists.
"""

from __future__ import annotations

import sqlite3


def apply_migrations(conn: sqlite3.Connection, migrations: list[str]) -> int:
    """Apply each SQL string via conn.execute().

    Skips statements that raise sqlite3.OperationalError with a message
    containing "duplicate column name" or "already exists".  All other
    errors are re-raised.

    Returns:
        Number of migrations actually applied (skipped ones are not counted).
    """
    applied = 0
    for sql in migrations:
        try:
            conn.execute(sql)
            conn.commit()
            applied += 1
        except sqlite3.OperationalError as exc:
            msg = str(exc).lower()
            if "duplicate column name" in msg or "already exists" in msg:
                # Idempotent — migration was already applied.
                pass
            else:
                raise
    return applied
