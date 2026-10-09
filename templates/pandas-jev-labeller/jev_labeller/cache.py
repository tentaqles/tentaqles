"""Label cache keyed by row hash. A re-run only pays for new or changed rows."""

from __future__ import annotations

import json
import sqlite3
from pathlib import Path
from typing import Any, Protocol


class LabelCache(Protocol):
    def get_many(self, keys: list[str]) -> dict[str, dict[str, Any]]: ...

    def put_many(self, items: dict[str, dict[str, Any]]) -> None: ...


class MemoryCache:
    def __init__(self) -> None:
        self.data: dict[str, dict[str, Any]] = {}

    def get_many(self, keys: list[str]) -> dict[str, dict[str, Any]]:
        return {k: self.data[k] for k in keys if k in self.data}

    def put_many(self, items: dict[str, dict[str, Any]]) -> None:
        self.data.update(items)


class SqliteCache:
    """A single-file cache. Safe to delete at any time; it only costs a re-label."""

    def __init__(self, path: str | Path):
        self.conn = sqlite3.connect(str(path))
        self.conn.execute("create table if not exists labels (key text primary key, value text not null)")
        self.conn.commit()

    def get_many(self, keys: list[str]) -> dict[str, dict[str, Any]]:
        out: dict[str, dict[str, Any]] = {}
        for i in range(0, len(keys), 500):
            part = keys[i : i + 500]
            q = f"select key, value from labels where key in ({','.join('?' * len(part))})"
            out.update({k: json.loads(v) for k, v in self.conn.execute(q, part)})
        return out

    def put_many(self, items: dict[str, dict[str, Any]]) -> None:
        self.conn.executemany(
            "insert or replace into labels (key, value) values (?, ?)",
            [(k, json.dumps(v)) for k, v in items.items()],
        )
        self.conn.commit()

    def close(self) -> None:
        self.conn.close()
