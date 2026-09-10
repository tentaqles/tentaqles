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


# ---------------------------------------------------------------------------
# Untracked-session fallback: child rows must never dangle
# ---------------------------------------------------------------------------


@pytest.fixture
def sessionless_store(tmp_path, monkeypatch):
    """A store with no active session — exercises the "untracked" fallback."""
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 4)
    s = MemoryStore(tmp_path)
    assert s._active_session_id is None
    yield s
    s.close()


def _dangling(conn) -> dict:
    """Count child rows whose session reference is not in sessions."""
    return {
        (table, col): conn.execute(
            f"SELECT COUNT(*) FROM {table} WHERE {col} IS NOT NULL "
            f"AND {col} NOT IN (SELECT id FROM sessions)"
        ).fetchone()[0]
        for table, col in (
            ("touches", "session_id"),
            ("decisions", "session_id"),
            ("pending", "session_id"),
            ("pending", "resolved_by"),
        )
    }


def test_record_decision_without_session_creates_placeholder(sessionless_store):
    store = sessionless_store
    store.record_decision(chosen="ship it", rationale="deadline")
    sid = store._conn.execute("SELECT session_id FROM decisions").fetchone()[0]
    assert sid == "untracked"
    assert store._conn.execute(
        "SELECT COUNT(*) FROM sessions WHERE id = ?", (sid,)
    ).fetchone()[0] == 1


def test_add_pending_without_session_creates_placeholder(sessionless_store):
    store = sessionless_store
    store.add_pending(description="write the migration")
    sid = store._conn.execute("SELECT session_id FROM pending").fetchone()[0]
    assert sid == "untracked"
    assert store._conn.execute(
        "SELECT COUNT(*) FROM sessions WHERE id = ?", (sid,)
    ).fetchone()[0] == 1


def test_resolve_pending_without_session_creates_placeholder(sessionless_store):
    store = sessionless_store
    pid = store.add_pending(description="close the loop")
    store.resolve_pending(pid)
    resolved_by = store._conn.execute(
        "SELECT resolved_by FROM pending WHERE id = ?", (pid,)
    ).fetchone()[0]
    assert resolved_by == "untracked"
    assert store._conn.execute(
        "SELECT COUNT(*) FROM sessions WHERE id = ?", (resolved_by,)
    ).fetchone()[0] == 1


def test_no_child_row_references_a_missing_session(sessionless_store):
    store = sessionless_store
    # Recorded with no session active, and without touch() — which has always
    # created the placeholder and would mask a dangling reference written by
    # the other writers.
    store.record_decision(chosen="use sqlite", rationale="simple")
    pid = store.add_pending(description="benchmark it")
    store.resolve_pending(pid)
    assert _dangling(store._conn) == {
        ("touches", "session_id"): 0,
        ("decisions", "session_id"): 0,
        ("pending", "session_id"): 0,
        ("pending", "resolved_by"): 0,
    }

    # And again with touch() plus a real session, and after that session ends.
    store.touch("a.py", action="edit")
    store.start_session()
    store.touch("b.py", action="edit")
    store.record_decision(chosen="use duckdb", rationale="analytics")
    pid2 = store.add_pending(description="revisit later")
    store.resolve_pending(pid2)
    store.end_session(summary="did things")
    store.record_decision(chosen="post-session call", rationale="after end")
    assert _dangling(store._conn) == {
        ("touches", "session_id"): 0,
        ("decisions", "session_id"): 0,
        ("pending", "session_id"): 0,
        ("pending", "resolved_by"): 0,
    }
