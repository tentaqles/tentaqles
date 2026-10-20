"""MemoryStore.search_memory: full-text search with recency weighting.

Search used to be embeddings-only: with fastembed missing it returned nothing,
and even with it present it only covered sessions and decisions and ranked
purely by cosine, so a two-year-old decision outranked last week's. These tests
pin the replacement: SQLite FTS5 (BM25) over sessions, decisions, pending items
and semantic facts, scores boosted for recent rows, with a plain LIKE fallback
when the interpreter's SQLite lacks FTS5. No test here needs fastembed.
"""

from __future__ import annotations

import sqlite3
from datetime import datetime, timedelta, timezone

import pytest

from tentaqles.memory import store as store_mod
from tentaqles.memory.store import MemoryStore


@pytest.fixture
def store(tmp_path, monkeypatch):
    # No embedding backend at all: writes store NULL, and anything that tries
    # to embed a query blows up. Search must not depend on it.
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: None)

    def _no_backend(self):
        raise RuntimeError("fastembed is not installed")

    monkeypatch.setattr(MemoryStore, "_get_emb", _no_backend)
    s = MemoryStore(tmp_path)
    s.start_session()
    yield s
    s.close()


def _types(results):
    return {r["type"] for r in results}


def test_search_finds_a_decision_by_its_words_without_embeddings(store):
    did = store.record_decision("Use FTS5 for memory search", "BM25 ranks text without fastembed")
    store.record_decision("Keep the Go CLI stateless", "state travels in __TQ_STATE")

    results = store.search_memory("fts5 bm25 search")

    assert results, "full-text search returned nothing"
    assert results[0]["type"] == "decision"
    assert results[0]["id"] == did


def test_search_covers_pending_items_and_semantic_facts(store):
    pid = store.add_pending("Rotate the signing certificate before launch")
    fid = store.record_semantic_fact("Deploys go through a Vercel preview first", ["s1"])

    pending = store.search_memory("signing certificate")
    facts = store.search_memory("vercel preview")

    assert [r["id"] for r in pending][:1] == [pid]
    assert pending[0]["type"] == "pending"
    assert [r["id"] for r in facts][:1] == [fid]
    assert facts[0]["type"] == "semantic"


def test_search_covers_ended_session_summaries(store):
    store.end_session("Refactored the invoice exporter and fixed the CSV quoting bug")

    results = store.search_memory("csv quoting invoice")

    assert results and results[0]["type"] == "session"
    assert "CSV quoting" in results[0]["text"]


def test_search_prefers_the_recent_of_two_equal_matches(store):
    old = store.record_decision("Use Postgres for the ledger", "ACID and the team knows it")
    new = store.record_decision("Use Postgres for the ledger", "ACID and the team knows it")
    long_ago = (datetime.now(timezone.utc) - timedelta(days=400)).isoformat()
    store._conn.execute("UPDATE decisions SET created_at=? WHERE id=?", (long_ago, old))
    store._conn.commit()

    results = store.search_memory("postgres ledger")

    assert [r["id"] for r in results[:2]] == [new, old]
    assert results[0]["score"] > results[1]["score"]


def test_search_skips_resolved_pending_and_superseded_decisions(store):
    pid = store.add_pending("Migrate the webhook retries to a queue")
    store.resolve_pending(pid)
    old = store.record_decision("Retry webhooks inline", "simplest thing")
    store.supersede_decision(old, "Retry webhooks from a queue", "inline retries blocked requests")

    results = store.search_memory("webhook retries queue inline")

    ids = [r["id"] for r in results]
    assert pid not in ids
    assert old not in ids
    assert any(r["type"] == "decision" for r in results)


def test_search_tolerates_fts_operators_in_the_query(store):
    store.record_decision("Quote user input", "FTS syntax must never leak")

    results = store.search_memory('what "about" (this) OR that* AND -never NEAR/2 ^x')

    assert isinstance(results, list)
    assert results and results[0]["type"] == "decision"


def test_search_with_no_words_returns_nothing(store):
    store.record_decision("Anything", "at all")
    assert store.search_memory("  *** ") == []


def test_search_result_shape(store):
    store.record_decision("Shape matters", "callers print type, text and score")

    (r,) = store.search_memory("shape matters", limit=1)

    assert set(r) >= {"type", "id", "text", "date", "score"}
    assert isinstance(r["score"], float) and r["score"] > 0
    assert r["text"].startswith("Shape matters")


def test_search_sees_rows_written_through_another_connection(store, tmp_path):
    other = MemoryStore(tmp_path)
    try:
        did = other.record_decision("Written elsewhere", "a hook process wrote this row")
    finally:
        other.close()

    results = store.search_memory("written elsewhere hook")

    assert [r["id"] for r in results][:1] == [did]


def test_search_falls_back_to_like_without_fts5(tmp_path, monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: None)
    monkeypatch.setattr(store_mod, "_has_fts5", lambda: False)
    s = MemoryStore(tmp_path)
    try:
        s.start_session()
        did = s.record_decision("Fallback path works", "older SQLite builds lack FTS5")
        tables = {
            r[0]
            for r in s._conn.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall()
        }
        assert not any(t.startswith("memory_fts") for t in tables), "no FTS table without FTS5"

        results = s.search_memory("fallback sqlite")

        assert [r["id"] for r in results][:1] == [did]
    finally:
        s.close()


def test_has_fts5_reports_the_real_interpreter():
    probe = sqlite3.connect(":memory:")
    try:
        probe.execute("CREATE VIRTUAL TABLE t USING fts5(x)")
        expected = True
    except sqlite3.OperationalError:
        expected = False
    finally:
        probe.close()
    assert store_mod._has_fts5() is expected
