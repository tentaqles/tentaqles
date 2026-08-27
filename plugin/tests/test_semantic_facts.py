"""Tests for record_semantic_fact / get_semantic_facts round-trip."""

import pytest

from tentaqles.memory.store import MemoryStore


class TestSemanticFacts:
    def test_record_and_retrieve(self, tmp_path):
        store = MemoryStore(tmp_path)
        fid = store.record_semantic_fact(
            fact="Always use UTC for timestamps",
            source_sessions=["sess1", "sess2"],
            category="pattern",
            tags=["datetime", "utc"],
        )
        assert isinstance(fid, str) and len(fid) == 32  # uuid4 hex

        facts = store.get_semantic_facts(limit=10)
        assert len(facts) == 1
        f = facts[0]
        assert f["id"] == fid
        assert f["fact"] == "Always use UTC for timestamps"
        assert f["category"] == "pattern"
        assert "utc" in f["tags"]
        assert "sess1" in f["source_sessions"]
        store.close()

    def test_multiple_facts_returned_by_strength(self, tmp_path):
        store = MemoryStore(tmp_path)
        id1 = store.record_semantic_fact("fact A", [], category="general")
        id2 = store.record_semantic_fact("fact B", [], category="general")
        # Lower strength for id1 directly
        store._conn.execute("UPDATE semantic_memories SET strength=0.3 WHERE id=?", (id1,))
        store._conn.commit()
        facts = store.get_semantic_facts(limit=10)
        assert facts[0]["id"] == id2  # higher strength first
        store.close()

    def test_category_filter(self, tmp_path):
        store = MemoryStore(tmp_path)
        store.record_semantic_fact("arch fact", [], category="architecture")
        store.record_semantic_fact("pref fact", [], category="preference")
        arch = store.get_semantic_facts(limit=10, category="architecture")
        assert len(arch) == 1
        assert arch[0]["fact"] == "arch fact"
        store.close()

    def test_unique_ids(self, tmp_path):
        store = MemoryStore(tmp_path)
        ids = [store.record_semantic_fact(f"fact {i}", []) for i in range(5)]
        assert len(set(ids)) == 5
        store.close()

    def test_empty_db_returns_empty_list(self, tmp_path):
        store = MemoryStore(tmp_path)
        assert store.get_semantic_facts() == []
        store.close()

    def test_get_procedural_patterns_empty(self, tmp_path):
        store = MemoryStore(tmp_path)
        assert store.get_procedural_patterns() == []
        store.close()
