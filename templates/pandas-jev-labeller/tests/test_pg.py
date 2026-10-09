from __future__ import annotations

import sys
from pathlib import Path

import numpy as np
import pandas as pd
import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from jev_labeller.pg import ident, read_query, write_labels  # noqa: E402


class FakeCursor:
    def __init__(self, conn):
        self.conn = conn
        self.description = [("ticket_id",), ("body",)]

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False

    def execute(self, sql, params=None):
        self.conn.executed.append((sql, params))

    def executemany(self, sql, rows):
        self.conn.many.append((sql, list(rows)))

    def fetchall(self):
        return [(1, "refund"), (2, "login")]


class FakeConn:
    def __init__(self):
        self.executed, self.many, self.commits = [], [], 0

    def cursor(self):
        return FakeCursor(self)

    def commit(self):
        self.commits += 1


def test_read_query_returns_dataframe_and_passes_params():
    conn = FakeConn()
    df = read_query(conn, "select ticket_id, body from t where created_at > %s", ("2026-01-01",))
    assert list(df.columns) == ["ticket_id", "body"] and len(df) == 2
    assert conn.executed[0][1] == ("2026-01-01",)


def test_write_labels_creates_and_upserts():
    conn = FakeConn()
    df = pd.DataFrame(
        {
            "ticket_id": np.array([1, 2], dtype="int64"),
            "topic": ["billing", None],
            "topic_confidence": [0.9, float("nan")],
            "flag": [True, False],
            "ignored": ["x", "y"],
        }
    )
    n = write_labels(conn, df, "staging.ticket_topics", "ticket_id", ["topic", "topic_confidence", "flag"])
    assert n == 2 and conn.commits == 1
    create = conn.executed[0][0]
    assert 'create table if not exists "staging"."ticket_topics"' in create
    assert '"ticket_id" bigint primary key' in create
    assert '"topic_confidence" double precision' in create and '"flag" boolean' in create
    assert '"labelled_at" timestamptz' in create and "ignored" not in create
    sql, rows = conn.many[0]
    assert 'on conflict ("ticket_id") do update set "topic" = excluded."topic"' in sql
    assert sql.count("%s") == 4
    assert rows == [(1, "billing", 0.9, True), (2, None, None, False)]
    assert type(rows[0][0]) is int  # numpy scalars converted for the driver


def test_write_labels_chunks_rows():
    conn = FakeConn()
    df = pd.DataFrame({"k": range(5), "v": ["a"] * 5})
    write_labels(conn, df, "s.t", "k", ["v"], create=False, chunk_size=2)
    assert [len(r) for _, r in conn.many] == [2, 2, 1]
    assert conn.executed == []  # create=False issues no DDL


@pytest.mark.parametrize("bad", ["t; drop table x", 'a"b', "a.b.c", "1abc", ""])
def test_identifiers_are_validated(bad):
    with pytest.raises(ValueError):
        ident(bad)


def test_missing_columns():
    with pytest.raises(KeyError):
        write_labels(FakeConn(), pd.DataFrame({"k": [1]}), "t", "id")
    with pytest.raises(KeyError):
        write_labels(FakeConn(), pd.DataFrame({"k": [1]}), "t", "k", ["nope"])
