"""Tests for MemoryStore.get_decision_lineage."""

import pytest

from tentaqles.memory.store import MemoryStore


def _seed_chain(store: MemoryStore) -> tuple[str, str, str]:
    """Insert a 3-generation supersession chain and return (root_id, mid_id, leaf_id)."""
    # We bypass record_decision (which needs embeddings) and insert directly.
    conn = store._conn
    conn.execute(
        "INSERT INTO decisions (id, session_id, created_at, node_ids, chosen, rationale, status) "
        "VALUES ('root1', 'untracked', '2026-01-01T00:00:00', '[]', 'use postgres', 'reliable', 'superseded')"
    )
    conn.execute(
        "INSERT INTO decisions (id, session_id, created_at, node_ids, chosen, rationale, status) "
        "VALUES ('mid1', 'untracked', '2026-01-15T00:00:00', '[]', 'use sqlite', 'simpler', 'superseded')"
    )
    conn.execute(
        "INSERT INTO decisions (id, session_id, created_at, node_ids, chosen, rationale, status) "
        "VALUES ('leaf1', 'untracked', '2026-02-01T00:00:00', '[]', 'use duckdb', 'analytics', 'active')"
    )
    # root1 -> mid1 -> leaf1
    conn.execute("UPDATE decisions SET superseded_by='mid1' WHERE id='root1'")
    conn.execute("UPDATE decisions SET superseded_by='leaf1' WHERE id='mid1'")
    conn.commit()
    return "root1", "mid1", "leaf1"


class TestDecisionLineage:
    def test_lineage_from_root(self, tmp_path):
        store = MemoryStore(tmp_path)
        root_id, mid_id, leaf_id = _seed_chain(store)

        result = store.get_decision_lineage(root_id)
        assert result["root"]["id"] == root_id
        assert result["current"]["id"] == leaf_id
        chain_ids = [d["id"] for d in result["chain"]]
        assert root_id in chain_ids
        assert mid_id in chain_ids
        assert leaf_id in chain_ids
        store.close()

    def test_lineage_from_middle(self, tmp_path):
        store = MemoryStore(tmp_path)
        root_id, mid_id, leaf_id = _seed_chain(store)

        result = store.get_decision_lineage(mid_id)
        # Walking backward should find root_id as the root.
        assert result["root"]["id"] == root_id
        assert result["current"]["id"] == leaf_id
        store.close()

    def test_lineage_from_leaf(self, tmp_path):
        store = MemoryStore(tmp_path)
        root_id, mid_id, leaf_id = _seed_chain(store)

        result = store.get_decision_lineage(leaf_id)
        # leaf has no superseded_by, so root walked from leaf is mid1's predecessor
        assert result["root"]["id"] == root_id
        assert result["current"]["id"] == leaf_id
        store.close()

    def test_single_decision_lineage(self, tmp_path):
        store = MemoryStore(tmp_path)
        store._conn.execute(
            "INSERT INTO decisions (id, session_id, created_at, node_ids, chosen, rationale, status) "
            "VALUES ('solo', 'untracked', '2026-01-01T00:00:00', '[]', 'do X', 'because', 'active')"
        )
        store._conn.commit()

        result = store.get_decision_lineage("solo")
        assert result["root"]["id"] == "solo"
        assert result["current"]["id"] == "solo"
        assert len(result["chain"]) == 1
        store.close()

    def test_chain_ordered_root_to_leaf(self, tmp_path):
        store = MemoryStore(tmp_path)
        root_id, mid_id, leaf_id = _seed_chain(store)

        result = store.get_decision_lineage(root_id)
        chain_ids = [d["id"] for d in result["chain"]]
        assert chain_ids.index(root_id) < chain_ids.index(mid_id)
        assert chain_ids.index(mid_id) < chain_ids.index(leaf_id)
        store.close()
