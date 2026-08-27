"""Tests for MemoryConsolidator — Feature 7 (Wave 2).

Covers:
- maybe_compact() only fires at N-session boundaries
- run_compaction() writes semantic facts via store when llm_fn provided
- detect_procedural_patterns() finds a pattern after 3 identical decisions
- evict_stale() removes decayed entries that cross the age + score threshold
"""

from __future__ import annotations

import sqlite3
from datetime import datetime, timedelta, timezone

import pytest

from tentaqles.memory.store import MemoryStore
from tentaqles.memory.consolidator import MemoryConsolidator


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_store(tmp_path) -> MemoryStore:
    return MemoryStore(tmp_path)


def _seed_episodic_sessions(store: MemoryStore, n: int) -> list[str]:
    """Create n complete episodic sessions (started + ended)."""
    ids = []
    for i in range(n):
        sid = store.start_session(tags=["test"])
        store.end_session(f"Session summary {i}")
        ids.append(sid)
    return ids


def _make_llm_fn(responses: list[str]):
    """Return a callable that returns responses in order."""
    calls = []

    def llm_fn(prompt: str) -> str:
        calls.append(prompt)
        if responses:
            return responses.pop(0)
        return ""

    llm_fn.calls = calls  # type: ignore[attr-defined]
    return llm_fn


# ---------------------------------------------------------------------------
# maybe_compact — boundary tests
# ---------------------------------------------------------------------------

class TestMaybeCompact:
    def test_does_not_fire_below_threshold(self, tmp_path):
        """5 unsourced sessions with threshold=10 → compacted=False."""
        store = _make_store(tmp_path)
        _seed_episodic_sessions(store, 5)
        consolidator = MemoryConsolidator(store)
        result = consolidator.maybe_compact(every_n_sessions=10)
        assert result["compacted"] is False
        assert result["facts_added"] == 0
        store.close()

    def test_fires_at_exact_threshold(self, tmp_path):
        """10 unsourced sessions with threshold=10 → compacted=True (but no facts without llm_fn)."""
        store = _make_store(tmp_path)
        _seed_episodic_sessions(store, 10)
        consolidator = MemoryConsolidator(store)
        result = consolidator.maybe_compact(every_n_sessions=10)
        assert result["compacted"] is True
        # No llm_fn → no facts extracted, but compaction ran
        assert result["facts_added"] == 0
        store.close()

    def test_does_not_fire_at_11_sessions(self, tmp_path):
        """11 unsourced sessions with threshold=10 → compacted=False (11 % 10 != 0)."""
        store = _make_store(tmp_path)
        _seed_episodic_sessions(store, 11)
        consolidator = MemoryConsolidator(store)
        result = consolidator.maybe_compact(every_n_sessions=10)
        assert result["compacted"] is False
        store.close()

    def test_fires_at_20_sessions(self, tmp_path):
        """20 unsourced sessions with threshold=10 → compacted=True."""
        store = _make_store(tmp_path)
        _seed_episodic_sessions(store, 20)
        consolidator = MemoryConsolidator(store)
        result = consolidator.maybe_compact(every_n_sessions=10)
        assert result["compacted"] is True
        store.close()

    def test_result_dict_keys_present(self, tmp_path):
        """Return dict always has all four keys regardless of outcome."""
        store = _make_store(tmp_path)
        consolidator = MemoryConsolidator(store)
        result = consolidator.maybe_compact()
        assert set(result.keys()) == {"compacted", "facts_added", "patterns_found", "evicted"}
        store.close()

    def test_already_sourced_sessions_not_counted(self, tmp_path):
        """Sessions already referenced in semantic_memories do not count toward threshold."""
        store = _make_store(tmp_path)
        ids = _seed_episodic_sessions(store, 10)
        # Mark all 10 as already sourced in a semantic fact
        import json
        store.record_semantic_fact("pre-existing fact", source_sessions=ids)
        consolidator = MemoryConsolidator(store)
        result = consolidator.maybe_compact(every_n_sessions=10)
        # All 10 are sourced → 0 unsourced → should not fire
        assert result["compacted"] is False
        store.close()


# ---------------------------------------------------------------------------
# run_compaction — LLM path
# ---------------------------------------------------------------------------

class TestRunCompaction:
    def test_writes_facts_when_llm_fn_provided(self, tmp_path):
        """run_compaction writes semantic facts returned by llm_fn."""
        store = _make_store(tmp_path)
        ids = _seed_episodic_sessions(store, 3)
        llm_fn = _make_llm_fn(["Always use UTC\nPrefer immutable data structures"])
        consolidator = MemoryConsolidator(store, llm_fn=llm_fn)
        new_ids = consolidator.run_compaction(ids)
        assert len(new_ids) == 2
        facts = store.get_semantic_facts(limit=10)
        fact_texts = {f["fact"] for f in facts}
        assert "Always use UTC" in fact_texts
        assert "Prefer immutable data structures" in fact_texts
        store.close()

    def test_source_sessions_recorded(self, tmp_path):
        """Semantic facts reference the session IDs they were extracted from."""
        store = _make_store(tmp_path)
        ids = _seed_episodic_sessions(store, 2)
        llm_fn = _make_llm_fn(["Single extracted fact"])
        consolidator = MemoryConsolidator(store, llm_fn=llm_fn)
        new_ids = consolidator.run_compaction(ids)
        assert len(new_ids) == 1
        facts = store.get_semantic_facts(limit=5)
        assert set(ids).issubset(set(facts[0]["source_sessions"]))
        store.close()

    def test_no_llm_fn_returns_empty(self, tmp_path):
        """Without llm_fn, run_compaction is a no-op."""
        store = _make_store(tmp_path)
        ids = _seed_episodic_sessions(store, 5)
        consolidator = MemoryConsolidator(store)
        new_ids = consolidator.run_compaction(ids)
        assert new_ids == []
        assert store.get_semantic_facts() == []
        store.close()

    def test_empty_session_list_returns_empty(self, tmp_path):
        """run_compaction([]) always returns []."""
        store = _make_store(tmp_path)
        llm_fn = _make_llm_fn(["some fact"])
        consolidator = MemoryConsolidator(store, llm_fn=llm_fn)
        assert consolidator.run_compaction([]) == []
        store.close()

    def test_llm_exception_returns_empty(self, tmp_path):
        """If llm_fn raises, run_compaction returns [] without crashing."""
        store = _make_store(tmp_path)
        ids = _seed_episodic_sessions(store, 2)

        def bad_llm(prompt):
            raise RuntimeError("LLM unavailable")

        consolidator = MemoryConsolidator(store, llm_fn=bad_llm)
        result = consolidator.run_compaction(ids)
        assert result == []
        store.close()

    def test_filters_trivially_short_lines(self, tmp_path):
        """Lines shorter than 10 chars are excluded from extracted facts."""
        store = _make_store(tmp_path)
        ids = _seed_episodic_sessions(store, 1)
        # One real fact, two throwaway lines
        llm_fn = _make_llm_fn(["ok\n\nReal substantive fact here\n- yes"])
        consolidator = MemoryConsolidator(store, llm_fn=llm_fn)
        new_ids = consolidator.run_compaction(ids)
        facts = store.get_semantic_facts()
        assert len(facts) == 1
        assert "Real substantive fact here" in facts[0]["fact"]
        store.close()


# ---------------------------------------------------------------------------
# detect_procedural_patterns
# ---------------------------------------------------------------------------

class TestDetectProceduralPatterns:
    def test_pattern_detected_after_min_occurrences(self, tmp_path):
        """3 decisions with the same chosen token signature → pattern detected."""
        store = _make_store(tmp_path)
        sid = store.start_session()
        for _ in range(3):
            store.record_decision(
                chosen="use dependency injection",
                rationale="promotes testability",
            )
        store.end_session("test session")
        consolidator = MemoryConsolidator(store)
        patterns = consolidator.detect_procedural_patterns(min_occurrences=3)
        assert len(patterns) >= 1
        trigger_patterns = [p["trigger_pattern"] for p in patterns]
        assert any("use" in tp for tp in trigger_patterns)
        store.close()

    def test_below_threshold_not_detected(self, tmp_path):
        """2 identical decisions with threshold=3 → no pattern."""
        store = _make_store(tmp_path)
        sid = store.start_session()
        for _ in range(2):
            store.record_decision(
                chosen="refactor legacy code",
                rationale="reduce tech debt",
            )
        store.end_session("test")
        consolidator = MemoryConsolidator(store)
        patterns = consolidator.detect_procedural_patterns(min_occurrences=3)
        assert len(patterns) == 0
        store.close()

    def test_pattern_written_to_procedural_memories(self, tmp_path):
        """Detected pattern is persisted in procedural_memories table."""
        store = _make_store(tmp_path)
        sid = store.start_session()
        for _ in range(4):
            store.record_decision(
                chosen="write unit tests first",
                rationale="TDD approach",
            )
        store.end_session("tdd session")
        consolidator = MemoryConsolidator(store)
        consolidator.detect_procedural_patterns(min_occurrences=3)
        patterns = store.get_procedural_patterns(limit=5)
        assert len(patterns) >= 1
        assert patterns[0]["occurrence_count"] >= 3
        store.close()

    def test_upserts_on_redetection(self, tmp_path):
        """Running detect_procedural_patterns twice does not duplicate rows."""
        store = _make_store(tmp_path)
        sid = store.start_session()
        for _ in range(3):
            store.record_decision("prefer async io", "non-blocking")
        store.end_session("session")
        consolidator = MemoryConsolidator(store)
        consolidator.detect_procedural_patterns(min_occurrences=3)
        consolidator.detect_procedural_patterns(min_occurrences=3)
        patterns = store.get_procedural_patterns(limit=10)
        sig_counts: dict[str, int] = {}
        for p in patterns:
            sig_counts[p["trigger_pattern"]] = sig_counts.get(p["trigger_pattern"], 0) + 1
        for count in sig_counts.values():
            assert count == 1, "Duplicate procedural pattern rows found"
        store.close()


# ---------------------------------------------------------------------------
# evict_stale
# ---------------------------------------------------------------------------

class TestEvictStale:
    def _insert_old_fact(
        self,
        conn: sqlite3.Connection,
        fact_id: str,
        strength: float,
        recall_count: int,
        age_days: int,
        last_recalled_days_ago: int | None = None,
    ) -> None:
        """Directly insert a semantic_memory row with a backdated created_at."""
        import uuid as _uuid

        created = (
            datetime.now(timezone.utc) - timedelta(days=age_days)
        ).isoformat()
        if last_recalled_days_ago is not None:
            last_recalled = (
                datetime.now(timezone.utc) - timedelta(days=last_recalled_days_ago)
            ).isoformat()
        else:
            last_recalled = None

        conn.execute(
            "INSERT INTO semantic_memories "
            "(id, created_at, fact, category, strength, recall_count, last_recalled, source_sessions, tags) "
            "VALUES (?, ?, ?, 'general', ?, ?, ?, '[]', '[]')",
            (fact_id, created, f"fact {fact_id}", strength, recall_count, last_recalled),
        )
        conn.commit()

    def test_evicts_decayed_old_fact(self, tmp_path):
        """A fact with near-zero score older than 180 days is deleted."""
        store = _make_store(tmp_path)
        # Insert a fact: old (200 days), never recalled, strength=0.001
        self._insert_old_fact(
            store._conn,
            fact_id="stale001",
            strength=0.001,
            recall_count=0,
            age_days=200,
            last_recalled_days_ago=None,
        )
        consolidator = MemoryConsolidator(store)
        evicted = consolidator.evict_stale(min_score=0.01, older_than_days=180)
        assert evicted == 1
        remaining = store.get_semantic_facts()
        assert all(f["id"] != "stale001" for f in remaining)
        store.close()

    def test_does_not_evict_recent_fact(self, tmp_path):
        """A decayed fact younger than older_than_days cutoff is kept."""
        store = _make_store(tmp_path)
        # Insert a fact: young (30 days), never recalled, strength=0.001
        self._insert_old_fact(
            store._conn,
            fact_id="young001",
            strength=0.001,
            recall_count=0,
            age_days=30,
            last_recalled_days_ago=None,
        )
        consolidator = MemoryConsolidator(store)
        evicted = consolidator.evict_stale(min_score=0.01, older_than_days=180)
        assert evicted == 0
        store.close()

    def test_does_not_evict_strong_old_fact(self, tmp_path):
        """A strong fact (frequently recalled) is kept even if old."""
        store = _make_store(tmp_path)
        # Insert a fact: old but recalled yesterday, high recall_count
        self._insert_old_fact(
            store._conn,
            fact_id="strong001",
            strength=1.0,
            recall_count=20,
            age_days=200,
            last_recalled_days_ago=1,
        )
        consolidator = MemoryConsolidator(store)
        evicted = consolidator.evict_stale(min_score=0.01, older_than_days=180)
        assert evicted == 0
        store.close()

    def test_evicts_only_qualifying_rows(self, tmp_path):
        """Mixed set: evicts only rows meeting both conditions."""
        store = _make_store(tmp_path)
        # Stale and decayed — should be evicted
        self._insert_old_fact(
            store._conn, "stale_a", strength=0.0001, recall_count=0, age_days=200
        )
        # Old but strong — should be kept
        self._insert_old_fact(
            store._conn, "strong_b", strength=1.0, recall_count=15, age_days=200,
            last_recalled_days_ago=2
        )
        # Recent and decayed — should be kept (not old enough)
        self._insert_old_fact(
            store._conn, "young_c", strength=0.0001, recall_count=0, age_days=10
        )
        consolidator = MemoryConsolidator(store)
        evicted = consolidator.evict_stale(min_score=0.01, older_than_days=180)
        assert evicted == 1
        remaining_ids = {f["id"] for f in store.get_semantic_facts(limit=10)}
        assert "stale_a" not in remaining_ids
        assert "strong_b" in remaining_ids
        assert "young_c" in remaining_ids
        store.close()

    def test_empty_db_returns_zero(self, tmp_path):
        """evict_stale on an empty semantic_memories table returns 0."""
        store = _make_store(tmp_path)
        consolidator = MemoryConsolidator(store)
        assert consolidator.evict_stale() == 0
        store.close()
