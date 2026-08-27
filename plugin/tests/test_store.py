"""Tests for tentaqles.memory.store — privacy filter + F1/F3/F4 methods."""

from __future__ import annotations

import pytest

from tentaqles.memory.store import MemoryStore


@pytest.fixture
def store(tmp_path, monkeypatch):
    # Avoid loading real embedding model: stub _embed to return zero bytes.
    monkeypatch.setattr(
        MemoryStore, "_embed", lambda self, text: b"\x00" * 4
    )
    s = MemoryStore(tmp_path)
    s.start_session()
    yield s
    s.close()


# ---------------------------------------------------------------------------
# Task 1: Privacy filter on write methods
# ---------------------------------------------------------------------------


def test_touch_redacts_secrets(store):
    store.touch(
        node_id="ghp_fakesecretAAAAAAAAAAAAAAAAAAAAAAAA",
        node_type="file",
        action="edit",
    )
    row = store._conn.execute("SELECT node_id FROM touches").fetchone()
    assert "ghp_fakesecret" not in row[0]
    assert "[REDACTED" in row[0]


def test_record_decision_redacts_rationale(store):
    store.record_decision(
        chosen="pick option A",
        rationale="because api_key=sk_abcdef1234567890XYZ works",
        node_ids=["file.py"],
        rejected=["option B with AKIAIOSFODNN7EXAMPLE"],
    )
    row = store._conn.execute(
        "SELECT chosen, rationale, rejected FROM decisions"
    ).fetchone()
    assert "sk_abcdef1234567890XYZ" not in row[1]
    assert "[REDACTED" in row[1]
    assert "AKIAIOSFODNN7EXAMPLE" not in row[2]


def test_add_pending_redacts_description(store):
    store.add_pending(
        description="fix db: postgres://user:p4ssword@host/db connection",
        priority="high",
    )
    row = store._conn.execute("SELECT description FROM pending").fetchone()
    assert "p4ssword" not in row[0]
    assert "[REDACTED" in row[0]


# ---------------------------------------------------------------------------
# Task 2: F1 — get_compact_context
# ---------------------------------------------------------------------------


def test_get_compact_context_empty(store):
    out = store.get_compact_context()
    assert isinstance(out, str)
    assert "Workspace memory" in out


def test_get_compact_context_with_data(store):
    store.touch("hot_file.py", action="edit", weight=5.0)
    store.touch("hot_file.py", action="edit", weight=5.0)
    store.record_decision(
        chosen="use pytest",
        rationale="mature and fast",
        node_ids=["hot_file.py"],
    )
    store.add_pending(description="write more tests", priority="medium")

    out = store.get_compact_context()
    assert "use pytest" in out
    assert "hot_file.py" in out
    assert "write more tests" in out


def test_get_compact_context_token_budget(store):
    long = "x" * 5000
    store.add_pending(description=long)
    out = store.get_compact_context(max_tokens=100)
    assert len(out) <= 100 * 4 + len("\n... (truncated)") + 5
    assert "truncated" in out


# ---------------------------------------------------------------------------
# Task 2: F3 — get_node_history_enriched
# ---------------------------------------------------------------------------


def test_get_node_history_enriched_joins_sessions(store):
    store.touch("joined.py", action="edit")
    store.end_session(summary="worked on joined.py")
    store.start_session()

    result = store.get_node_history_enriched("joined.py")
    assert result["node_id"] == "joined.py"
    assert len(result["touches"]) == 1
    t = result["touches"][0]
    assert t["session_summary"] == "worked on joined.py"
    assert t["session_started_at"] is not None


def test_get_node_history_enriched_finds_decisions(store):
    store.touch("target.py", action="edit")
    store.record_decision(
        chosen="refactor target",
        rationale="complexity",
        node_ids=["target.py", "other.py"],
    )
    # A decision referencing a different file must not match via LIKE substring.
    store.record_decision(
        chosen="unrelated",
        rationale="noise",
        node_ids=["notarget.py"],
    )

    result = store.get_node_history_enriched("target.py")
    chosens = [d["chosen"] for d in result["related_decisions"]]
    assert "refactor target" in chosens
    assert "unrelated" not in chosens


# ---------------------------------------------------------------------------
# Task 2: F4 — find_similar_pending
# ---------------------------------------------------------------------------


def test_find_similar_pending_high_jaccard(store):
    store.add_pending(description="fix broken login flow on mobile devices")
    hits = store.find_similar_pending(
        "fix broken login flow on mobile devices"
    )
    assert len(hits) == 1


def test_find_similar_pending_low_jaccard(store):
    store.add_pending(description="fix the broken login flow on mobile")
    hits = store.find_similar_pending("migrate database to postgres 16")
    assert hits == []


def test_find_similar_pending_empty_store(store):
    assert store.find_similar_pending("anything at all") == []
