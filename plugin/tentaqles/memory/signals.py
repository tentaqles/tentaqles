"""Inter-workspace signal bus backed by meta.db."""

from __future__ import annotations

import json
import sqlite3
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path


class SignalBus:
    """Lightweight pub/sub primitive for cross-workspace broadcast.

    Backed by the global meta.db. Opens a fresh connection per call to avoid
    long-lived connection state (meta.db may be written from multiple processes).
    Use PRAGMA journal_mode=WAL on each connect.
    """

    def __init__(self, meta_db_path: Path | None = None):
        if meta_db_path is None:
            from tentaqles.config import meta_db_path as _meta_db_path
            meta_db_path = _meta_db_path()
        self._db_path = Path(meta_db_path)

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _connect(self) -> sqlite3.Connection:
        conn = sqlite3.connect(str(self._db_path))
        conn.execute("PRAGMA journal_mode=WAL")
        conn.row_factory = sqlite3.Row
        return conn

    @staticmethod
    def _ts(dt: datetime) -> str:
        """Format a UTC datetime as a SQLite-compatible string with microseconds."""
        return dt.strftime("%Y-%m-%d %H:%M:%S.%f")

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def emit(
        self,
        from_workspace: str,
        to_workspace: str,
        event_type: str,
        message: str,
        payload: dict | None = None,
        ttl_hours: float = 48.0,
    ) -> str:
        """Insert a signal row and return its uuid4 hex id.

        Validates that to_workspace exists in workspace_status. Exception:
        if workspace_status is empty (first run), the emit is accepted anyway
        so smoke tests and bootstrapping work without pre-registration.
        """
        signal_id = uuid.uuid4().hex
        now = datetime.now(timezone.utc)
        expires_at = now + timedelta(hours=ttl_hours)
        payload_json = json.dumps(payload or {})

        with self._connect() as conn:
            # Validate target workspace exists (unless table is empty)
            row_count = conn.execute(
                "SELECT COUNT(*) FROM workspace_status"
            ).fetchone()[0]
            if row_count > 0:
                target = conn.execute(
                    "SELECT workspace_id FROM workspace_status WHERE workspace_id = ?",
                    (to_workspace,),
                ).fetchone()
                if target is None:
                    raise ValueError(
                        f"Unknown target workspace: {to_workspace!r}. "
                        "Register it via MetaMemory.update_workspace() first."
                    )

            conn.execute(
                """INSERT INTO signals
                   (id, from_workspace, to_workspace, event_type, payload,
                    message, emitted_at, expires_at)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?)""",
                (
                    signal_id,
                    from_workspace,
                    to_workspace,
                    event_type,
                    payload_json,
                    message,
                    self._ts(now),
                    self._ts(expires_at),
                ),
            )

        return signal_id

    def read_pending(self, workspace_id: str) -> list[dict]:
        """Return unread, non-expired signals directed at workspace_id.

        Each dict has: id, from_workspace, event_type, message, emitted_at, payload.
        """
        with self._connect() as conn:
            rows = conn.execute(
                """SELECT id, from_workspace, event_type, message, emitted_at, payload
                   FROM signals
                   WHERE to_workspace = ?
                     AND read_by IS NULL
                     AND expires_at > datetime('now')
                   ORDER BY emitted_at ASC""",
                (workspace_id,),
            ).fetchall()

        return [
            {
                "id": r["id"],
                "from_workspace": r["from_workspace"],
                "event_type": r["event_type"],
                "message": r["message"],
                "emitted_at": r["emitted_at"],
                "payload": json.loads(r["payload"] or "{}"),
            }
            for r in rows
        ]

    def acknowledge(self, signal_id: str, workspace_id: str) -> None:
        """Mark a signal as read using an exclusive transaction."""
        now = self._ts(datetime.now(timezone.utc))
        conn = sqlite3.connect(str(self._db_path))
        try:
            conn.execute("PRAGMA journal_mode=WAL")
            conn.execute("BEGIN IMMEDIATE")
            conn.execute(
                "UPDATE signals SET read_by = ?, read_at = ? WHERE id = ?",
                (workspace_id, now, signal_id),
            )
            conn.execute("COMMIT")
        except Exception:
            conn.execute("ROLLBACK")
            raise
        finally:
            conn.close()

    def prune_expired(self) -> int:
        """Delete expired signals and return the count deleted."""
        with self._connect() as conn:
            cursor = conn.execute(
                "DELETE FROM signals WHERE expires_at < datetime('now')"
            )
            return cursor.rowcount

    def list_recent(self, workspace_id: str, limit: int = 20) -> list[dict]:
        """Return all signals to or from workspace_id, newest first."""
        with self._connect() as conn:
            rows = conn.execute(
                """SELECT id, from_workspace, to_workspace, event_type,
                          message, emitted_at, expires_at, read_by, read_at, payload
                   FROM signals
                   WHERE to_workspace = ? OR from_workspace = ?
                   ORDER BY emitted_at DESC
                   LIMIT ?""",
                (workspace_id, workspace_id, limit),
            ).fetchall()

        return [
            {
                "id": r["id"],
                "from_workspace": r["from_workspace"],
                "to_workspace": r["to_workspace"],
                "event_type": r["event_type"],
                "message": r["message"],
                "emitted_at": r["emitted_at"],
                "expires_at": r["expires_at"],
                "read_by": r["read_by"],
                "read_at": r["read_at"],
                "payload": json.loads(r["payload"] or "{}"),
            }
            for r in rows
        ]
