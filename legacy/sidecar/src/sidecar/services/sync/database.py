"""SQLite persistence layer for the task sync engine."""

from __future__ import annotations

import json
import logging
import os
import platform
import sqlite3
import uuid
from datetime import datetime, timezone
from pathlib import Path

logger = logging.getLogger(__name__)


def _default_db_path() -> Path:
    """Platform-aware default path for the sync database."""
    if platform.system() == "Windows":
        base = Path(os.environ.get("LOCALAPPDATA", Path.home() / "AppData" / "Local"))
    else:
        base = Path(os.environ.get("XDG_DATA_HOME", Path.home() / ".local" / "share"))
    return base / "tentaqles" / "sync.db"


_SCHEMA_SQL = """
CREATE TABLE IF NOT EXISTS client_provider (
    slug            TEXT PRIMARY KEY,
    type            TEXT NOT NULL,       -- 'asana' | 'github' | 'azuredevops'
    config          TEXT NOT NULL DEFAULT '{}',  -- JSON provider-specific config
    credential_ref  TEXT NOT NULL DEFAULT '',     -- key name or vault ref
    poll_interval   INTEGER NOT NULL DEFAULT 60,  -- seconds
    sync_direction  TEXT NOT NULL DEFAULT 'bidirectional',  -- 'pull_only' | 'push_only' | 'bidirectional'
    last_synced     TEXT,                -- ISO-8601 timestamp
    sync_token      TEXT,                -- opaque continuation token
    enabled         INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS task_sync (
    id              TEXT PRIMARY KEY,
    client_slug     TEXT NOT NULL,
    remote_id       TEXT NOT NULL,
    remote_url      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT '',
    title           TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    assignee        TEXT,
    due_date        TEXT,
    priority        TEXT,
    labels          TEXT NOT NULL DEFAULT '[]',   -- JSON array
    dirty           INTEGER NOT NULL DEFAULT 0,
    sync_status     TEXT NOT NULL DEFAULT 'synced', -- 'synced' | 'pending' | 'conflict'
    remote_modified TEXT,                -- ISO-8601
    local_modified  TEXT,                -- ISO-8601
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE(client_slug, remote_id)
);

CREATE TABLE IF NOT EXISTS conflict_log (
    id              TEXT PRIMARY KEY,
    task_sync_id    TEXT NOT NULL,
    client_slug     TEXT NOT NULL,
    remote_id       TEXT NOT NULL,
    local_snapshot  TEXT NOT NULL DEFAULT '{}',   -- JSON
    remote_snapshot TEXT NOT NULL DEFAULT '{}',   -- JSON
    resolved        INTEGER NOT NULL DEFAULT 0,
    resolution      TEXT,                -- 'local' | 'remote' | 'merged'
    created_at      TEXT NOT NULL,
    resolved_at     TEXT,
    FOREIGN KEY(task_sync_id) REFERENCES task_sync(id)
);

CREATE TABLE IF NOT EXISTS push_queue (
    id              TEXT PRIMARY KEY,
    client_slug     TEXT NOT NULL,
    remote_id       TEXT NOT NULL,
    action          TEXT NOT NULL,       -- 'status' | 'update'
    payload         TEXT NOT NULL DEFAULT '{}',   -- JSON
    retries         INTEGER NOT NULL DEFAULT 0,
    max_retries     INTEGER NOT NULL DEFAULT 5,
    last_error      TEXT,
    created_at      TEXT NOT NULL,
    next_attempt_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_log (
    id              TEXT PRIMARY KEY,
    client_slug     TEXT NOT NULL,
    started_at      TEXT NOT NULL,
    finished_at     TEXT,
    status          TEXT NOT NULL DEFAULT 'running',  -- 'running' | 'success' | 'error'
    tasks_fetched   INTEGER NOT NULL DEFAULT 0,
    tasks_created   INTEGER NOT NULL DEFAULT 0,
    tasks_updated   INTEGER NOT NULL DEFAULT 0,
    conflicts       INTEGER NOT NULL DEFAULT 0,
    error           TEXT
);

CREATE INDEX IF NOT EXISTS idx_task_sync_client ON task_sync(client_slug);
CREATE INDEX IF NOT EXISTS idx_task_sync_dirty ON task_sync(dirty) WHERE dirty = 1;
CREATE INDEX IF NOT EXISTS idx_conflict_unresolved ON conflict_log(resolved) WHERE resolved = 0;
CREATE INDEX IF NOT EXISTS idx_push_queue_next ON push_queue(next_attempt_at);
"""


class SyncDatabase:
    """Thin synchronous wrapper around a WAL-mode SQLite database."""

    def __init__(self, db_path: Path | None = None):
        self._path = db_path or _default_db_path()
        self._conn: sqlite3.Connection | None = None

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    def initialize(self) -> None:
        self._path.parent.mkdir(parents=True, exist_ok=True)
        self._conn = sqlite3.connect(str(self._path), check_same_thread=False)
        self._conn.row_factory = sqlite3.Row
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.execute("PRAGMA foreign_keys=ON")
        self._conn.executescript(_SCHEMA_SQL)
        self._run_migrations()
        self._conn.commit()
        logger.info("SyncDatabase initialised at %s", self._path)

    def _run_migrations(self) -> None:
        """Apply incremental schema migrations for existing databases."""
        assert self._conn is not None
        cursor = self._conn.execute("PRAGMA table_info(client_provider)")
        columns = {row[1] for row in cursor.fetchall()}

        if "sync_direction" not in columns:
            self._conn.execute(
                "ALTER TABLE client_provider ADD COLUMN sync_direction TEXT NOT NULL DEFAULT 'bidirectional'"
            )
            logger.info("Migration: added sync_direction column to client_provider")

        # Migration: add extra_data column to task_sync
        try:
            self._conn.execute("SELECT extra_data FROM task_sync LIMIT 1")
        except sqlite3.OperationalError:
            self._conn.execute("ALTER TABLE task_sync ADD COLUMN extra_data TEXT NOT NULL DEFAULT '{}'")
            logger.info("Migration: added extra_data column to task_sync")

    def close(self) -> None:
        if self._conn:
            self._conn.close()
            self._conn = None

    @property
    def conn(self) -> sqlite3.Connection:
        if self._conn is None:
            raise RuntimeError("SyncDatabase not initialised — call initialize() first")
        return self._conn

    # ------------------------------------------------------------------
    # Client providers
    # ------------------------------------------------------------------

    def upsert_provider(
        self,
        slug: str,
        provider_type: str,
        config: dict,
        credential_ref: str = "",
        poll_interval: int = 60,
        sync_direction: str = "bidirectional",
        enabled: bool = True,
    ) -> dict:
        now = _now_iso()
        self.conn.execute(
            """
            INSERT INTO client_provider
                (slug, type, config, credential_ref, poll_interval,
                 sync_direction, enabled, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(slug) DO UPDATE SET
                type=excluded.type, config=excluded.config,
                credential_ref=excluded.credential_ref,
                poll_interval=excluded.poll_interval,
                sync_direction=excluded.sync_direction,
                enabled=excluded.enabled, updated_at=excluded.updated_at
            """,
            (
                slug, provider_type, json.dumps(config), credential_ref,
                poll_interval, sync_direction, int(enabled), now, now,
            ),
        )
        self.conn.commit()
        return self.get_provider(slug)  # type: ignore[return-value]

    def get_provider(self, slug: str) -> dict | None:
        row = self.conn.execute("SELECT * FROM client_provider WHERE slug = ?", (slug,)).fetchone()
        return dict(row) if row else None

    def list_providers(self, enabled_only: bool = True) -> list[dict]:
        sql = "SELECT * FROM client_provider"
        if enabled_only:
            sql += " WHERE enabled = 1"
        return [dict(r) for r in self.conn.execute(sql).fetchall()]

    def update_last_synced(self, slug: str, timestamp: str, sync_token: str | None = None) -> None:
        if sync_token is not None:
            self.conn.execute(
                "UPDATE client_provider SET last_synced = ?, sync_token = ?, updated_at = ? WHERE slug = ?",
                (timestamp, sync_token, _now_iso(), slug),
            )
        else:
            self.conn.execute(
                "UPDATE client_provider SET last_synced = ?, updated_at = ? WHERE slug = ?",
                (timestamp, _now_iso(), slug),
            )
        self.conn.commit()

    # ------------------------------------------------------------------
    # Task sync
    # ------------------------------------------------------------------

    def upsert_task(
        self,
        client_slug: str,
        remote_id: str,
        *,
        remote_url: str = "",
        status: str = "",
        title: str = "",
        description: str = "",
        assignee: str | None = None,
        due_date: str | None = None,
        priority: str | None = None,
        labels: list[str] | None = None,
        remote_modified: str | None = None,
        extra_data: dict | None = None,
    ) -> dict:
        now = _now_iso()
        task_id = str(uuid.uuid4())
        labels_json = json.dumps(labels or [])
        extra_json = json.dumps(extra_data or {})
        self.conn.execute(
            """
            INSERT INTO task_sync
                (id, client_slug, remote_id, remote_url, status, title, description,
                 assignee, due_date, priority, labels, remote_modified, local_modified,
                 extra_data, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(client_slug, remote_id) DO UPDATE SET
                remote_url=excluded.remote_url, status=excluded.status, title=excluded.title,
                description=excluded.description, assignee=excluded.assignee, due_date=excluded.due_date,
                priority=excluded.priority, labels=excluded.labels, remote_modified=excluded.remote_modified,
                extra_data=excluded.extra_data, updated_at=excluded.updated_at
            """,
            (
                task_id, client_slug, remote_id, remote_url, status, title, description,
                assignee, due_date, priority, labels_json, remote_modified, now,
                extra_json, now, now,
            ),
        )
        self.conn.commit()
        return self.get_task(client_slug, remote_id)  # type: ignore[return-value]

    def get_task(self, client_slug: str, remote_id: str) -> dict | None:
        row = self.conn.execute(
            "SELECT * FROM task_sync WHERE client_slug = ? AND remote_id = ?",
            (client_slug, remote_id),
        ).fetchone()
        return dict(row) if row else None

    def get_tasks_for_client(self, client_slug: str) -> list[dict]:
        return [
            dict(r)
            for r in self.conn.execute(
                "SELECT * FROM task_sync WHERE client_slug = ? ORDER BY updated_at DESC", (client_slug,)
            ).fetchall()
        ]

    def get_all_tasks(self) -> list[dict]:
        return [dict(r) for r in self.conn.execute("SELECT * FROM task_sync ORDER BY updated_at DESC").fetchall()]

    def get_dirty_tasks(self, client_slug: str) -> list[dict]:
        return [
            dict(r)
            for r in self.conn.execute(
                "SELECT * FROM task_sync WHERE client_slug = ? AND dirty = 1", (client_slug,)
            ).fetchall()
        ]

    def mark_dirty(self, client_slug: str, remote_id: str, fields_json: str) -> None:
        now = _now_iso()
        self.conn.execute(
            "UPDATE task_sync SET dirty = 1, sync_status = 'pending', local_modified = ?, updated_at = ? WHERE client_slug = ? AND remote_id = ?",
            (now, now, client_slug, remote_id),
        )
        self.conn.commit()

    def clear_dirty(self, client_slug: str, remote_id: str) -> None:
        now = _now_iso()
        self.conn.execute(
            "UPDATE task_sync SET dirty = 0, sync_status = 'synced', updated_at = ? WHERE client_slug = ? AND remote_id = ?",
            (now, client_slug, remote_id),
        )
        self.conn.commit()

    # ------------------------------------------------------------------
    # Conflicts
    # ------------------------------------------------------------------

    def create_conflict(
        self, task_sync_id: str, client_slug: str, remote_id: str, local_snapshot: dict, remote_snapshot: dict,
    ) -> dict:
        cid = str(uuid.uuid4())
        now = _now_iso()
        self.conn.execute(
            """
            INSERT INTO conflict_log (id, task_sync_id, client_slug, remote_id, local_snapshot, remote_snapshot, created_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (cid, task_sync_id, client_slug, remote_id, json.dumps(local_snapshot), json.dumps(remote_snapshot), now),
        )
        # Mark task as conflicted
        self.conn.execute(
            "UPDATE task_sync SET sync_status = 'conflict', updated_at = ? WHERE id = ?",
            (now, task_sync_id),
        )
        self.conn.commit()
        return {"id": cid, "task_sync_id": task_sync_id, "client_slug": client_slug, "remote_id": remote_id}

    def get_conflicts(self, resolved: bool = False) -> list[dict]:
        return [
            dict(r)
            for r in self.conn.execute(
                "SELECT * FROM conflict_log WHERE resolved = ? ORDER BY created_at DESC", (int(resolved),)
            ).fetchall()
        ]

    def resolve_conflict(self, conflict_id: str, resolution: str) -> None:
        now = _now_iso()
        self.conn.execute(
            "UPDATE conflict_log SET resolved = 1, resolution = ?, resolved_at = ? WHERE id = ?",
            (resolution, now, conflict_id),
        )
        # Clear conflict status on the linked task
        row = self.conn.execute("SELECT task_sync_id FROM conflict_log WHERE id = ?", (conflict_id,)).fetchone()
        if row:
            self.conn.execute(
                "UPDATE task_sync SET sync_status = 'synced', dirty = 0, updated_at = ? WHERE id = ?",
                (now, row["task_sync_id"]),
            )
        self.conn.commit()

    # ------------------------------------------------------------------
    # Push queue
    # ------------------------------------------------------------------

    def enqueue_push(self, client_slug: str, remote_id: str, action: str, payload: dict) -> str:
        pid = str(uuid.uuid4())
        now = _now_iso()
        self.conn.execute(
            """
            INSERT INTO push_queue (id, client_slug, remote_id, action, payload, created_at, next_attempt_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (pid, client_slug, remote_id, action, json.dumps(payload), now, now),
        )
        self.conn.commit()
        return pid

    def get_pending_pushes(self) -> list[dict]:
        now = _now_iso()
        return [
            dict(r)
            for r in self.conn.execute(
                "SELECT * FROM push_queue WHERE next_attempt_at <= ? AND retries < max_retries ORDER BY created_at",
                (now,),
            ).fetchall()
        ]

    def push_succeeded(self, push_id: str) -> None:
        self.conn.execute("DELETE FROM push_queue WHERE id = ?", (push_id,))
        self.conn.commit()

    def push_failed(self, push_id: str, error: str, backoff_seconds: int = 30) -> None:
        now = datetime.now(timezone.utc)
        next_at = datetime.fromtimestamp(now.timestamp() + backoff_seconds, tz=timezone.utc).isoformat()
        self.conn.execute(
            "UPDATE push_queue SET retries = retries + 1, last_error = ?, next_attempt_at = ? WHERE id = ?",
            (error, next_at, push_id),
        )
        self.conn.commit()

    # ------------------------------------------------------------------
    # Sync log
    # ------------------------------------------------------------------

    def start_sync_log(self, client_slug: str) -> str:
        lid = str(uuid.uuid4())
        now = _now_iso()
        self.conn.execute(
            "INSERT INTO sync_log (id, client_slug, started_at) VALUES (?, ?, ?)",
            (lid, client_slug, now),
        )
        self.conn.commit()
        return lid

    def finish_sync_log(
        self, log_id: str, *, status: str = "success", tasks_fetched: int = 0,
        tasks_created: int = 0, tasks_updated: int = 0, conflicts: int = 0, error: str | None = None,
    ) -> None:
        now = _now_iso()
        self.conn.execute(
            """
            UPDATE sync_log SET finished_at = ?, status = ?, tasks_fetched = ?,
                tasks_created = ?, tasks_updated = ?, conflicts = ?, error = ?
            WHERE id = ?
            """,
            (now, status, tasks_fetched, tasks_created, tasks_updated, conflicts, error, log_id),
        )
        self.conn.commit()

    def recent_sync_logs(self, client_slug: str | None = None, limit: int = 20) -> list[dict]:
        if client_slug:
            rows = self.conn.execute(
                "SELECT * FROM sync_log WHERE client_slug = ? ORDER BY started_at DESC LIMIT ?",
                (client_slug, limit),
            ).fetchall()
        else:
            rows = self.conn.execute(
                "SELECT * FROM sync_log ORDER BY started_at DESC LIMIT ?", (limit,)
            ).fetchall()
        return [dict(r) for r in rows]


# ------------------------------------------------------------------
# Helpers
# ------------------------------------------------------------------

def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()
