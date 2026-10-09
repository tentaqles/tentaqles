"""Postgres helpers: query -> DataFrame, labels -> staging table.

Works with any DB-API connection that uses `%s` placeholders (psycopg 3 or
psycopg2). Get one from `connect()`, which reads `DATABASE_URL`, or pass your own.
Labels go to a *staging* table keyed by your row id. Promote them into
production tables with a reviewed SQL statement, never directly from a notebook.
"""

from __future__ import annotations

import math
import os
import re
from typing import Any

import pandas as pd

_IDENT = re.compile(r"^[A-Za-z_][A-Za-z0-9_]{0,62}$")


def connect(url_env: str = "DATABASE_URL"):
    import psycopg  # lazy optional dependency

    url = os.environ.get(url_env, "")
    if not url:
        raise RuntimeError(f"{url_env} is not set (run with `tq dotenv run -- ...`)")
    return psycopg.connect(url)


def ident(name: str) -> str:
    """Validate and double-quote an identifier. `schema.table` is allowed."""
    parts = name.split(".")
    if not 1 <= len(parts) <= 2 or not all(_IDENT.match(p) for p in parts):
        raise ValueError(f"unsafe SQL identifier: {name!r}")
    return ".".join(f'"{p}"' for p in parts)


def read_query(conn, sql: str, params: tuple | dict | None = None) -> pd.DataFrame:
    """Run a SELECT and return a DataFrame. Pass values via `params`, never by f-string."""
    with conn.cursor() as cur:
        cur.execute(sql, params)
        cols = [d[0] for d in cur.description]
        rows = cur.fetchall()
    return pd.DataFrame(rows, columns=cols)


def _pg_type(dtype) -> str:
    if pd.api.types.is_bool_dtype(dtype):
        return "boolean"
    if pd.api.types.is_integer_dtype(dtype):
        return "bigint"
    if pd.api.types.is_float_dtype(dtype):
        return "double precision"
    return "text"


def _py(v: Any) -> Any:
    if v is None:
        return None
    if hasattr(v, "item"):  # numpy scalar
        v = v.item()
    if isinstance(v, float) and math.isnan(v):
        return None
    return v


def write_labels(
    conn,
    df: pd.DataFrame,
    table: str,
    key_column: str,
    columns: list[str] | None = None,
    create: bool = True,
    chunk_size: int = 1000,
) -> int:
    """Upsert `key_column` + label `columns` into `table`. Returns rows written.

    Re-running is idempotent: rows are keyed by `key_column` and updated in place.
    """
    if key_column not in df.columns:
        raise KeyError(f"{key_column!r} not in DataFrame")
    columns = [c for c in (columns or list(df.columns)) if c != key_column]
    missing = [c for c in columns if c not in df.columns]
    if missing:
        raise KeyError(f"columns not in DataFrame: {missing}")
    t, k = ident(table), ident(key_column)
    cols = [ident(c) for c in columns]

    with conn.cursor() as cur:
        if create:
            defs = [f"{k} {_pg_type(df[key_column].dtype)} primary key"]
            defs += [f"{qc} {_pg_type(df[c].dtype)}" for qc, c in zip(cols, columns)]
            defs.append('"labelled_at" timestamptz not null default now()')
            cur.execute(f"create table if not exists {t} ({', '.join(defs)})")
        updates = ", ".join([f"{qc} = excluded.{qc}" for qc in cols] + ['"labelled_at" = now()'])
        sql = (
            f"insert into {t} ({', '.join([k, *cols])}) values ({', '.join(['%s'] * (len(cols) + 1))}) "
            f"on conflict ({k}) do update set {updates}"
        )
        rows = [tuple(_py(v) for v in r) for r in df[[key_column, *columns]].itertuples(index=False, name=None)]
        for i in range(0, len(rows), chunk_size):
            cur.executemany(sql, rows[i : i + chunk_size])
    conn.commit()
    return len(rows)
