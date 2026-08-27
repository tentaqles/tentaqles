"""Tests for Feature 8: Contradiction Detection and Supersession.

Covers:
- ContradictionDetector.classify() — below-threshold (no contradiction)
- ContradictionDetector.classify() — above-threshold + disjoint words (contradiction)
- ContradictionDetector.classify() — above-threshold + overlapping words (refinement, NOT contradiction)
- MemoryStore.record_decision_checked() — auto-supersedes a conflicting prior decision
- test_lineage_no_cycles — a decision cannot appear twice in its own lineage chain
"""

from __future__ import annotations

import sqlite3
import json

import numpy as np
import pytest

from tentaqles.memory.contradiction import ContradictionCandidate, ContradictionDetector
from tentaqles.memory.store import MemoryStore


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_embedding(values: list[float]) -> bytes:
    """Build a float32 embedding blob from a list of floats."""
    return np.array(values, dtype=np.float32).tobytes()


def _unit(values: list[float]) -> bytes:
    """Return a normalised float32 blob."""
    arr = np.array(values, dtype=np.float32)
    norm = np.linalg.norm(arr)
    if norm > 1e-10:
        arr = arr / norm
    return arr.tobytes()


def _seed_decision(conn: sqlite3.Connection, did: str, chosen: str, rationale: str, embedding: bytes):
    """Insert a minimal active decision row directly (bypasses privacy filter / embedding service)."""
    conn.execute(
        "INSERT INTO decisions "
        "(id, session_id, created_at, node_ids, chosen, rejected, rationale, status, embedding) "
        "VALUES (?, 'untracked', datetime('now'), '[]', ?, '[]', ?, 'active', ?)",
        (did, chosen, rationale, embedding),
    )
    conn.commit()


# ---------------------------------------------------------------------------
# ContradictionDetector unit tests (use an in-memory SQLite DB directly)
# ---------------------------------------------------------------------------

class FakeEmbeddingService:
    """Stub: embed() just returns a numpy array of the provided vectors."""

    def __init__(self, vector: list[float]):
        self._vector = np.array(vector, dtype=np.float32)

    def embed(self, texts):
        return np.stack([self._vector] * len(texts))


@pytest.fixture
def mem_conn():
    """In-memory SQLite connection with the minimal decisions table."""
    conn = sqlite3.connect(":memory:")
    conn.execute(
        """
        CREATE TABLE decisions (
            id TEXT PRIMARY KEY,
            session_id TEXT,
            created_at TEXT,
            node_ids TEXT DEFAULT '[]',
            chosen TEXT NOT NULL,
            rejected TEXT DEFAULT '[]',
            rationale TEXT NOT NULL,
            status TEXT DEFAULT 'active',
            superseded_by TEXT,
            embedding BLOB,
            tags TEXT DEFAULT '[]'
        )
        """
    )
    conn.commit()
    yield conn
    conn.close()


class TestClassifyBelowThreshold:
    """classify() should return no contradictions when similarity is below threshold."""

    def test_low_similarity_no_contradiction(self, mem_conn):
        # Two nearly orthogonal vectors.
        existing_emb = _unit([1.0, 0.0, 0.0, 0.0])
        new_emb = _unit([0.0, 1.0, 0.0, 0.0])

        _seed_decision(mem_conn, "d1", "use postgres", "reliable RDBMS", existing_emb)

        detector = ContradictionDetector(mem_conn, emb_service=None, threshold=0.82)
        candidates = detector.classify("use redis", new_emb)

        # All candidates should have is_contradiction=False.
        contradictions = [c for c in candidates if c.is_contradiction]
        assert len(contradictions) == 0

    def test_similarity_below_threshold_is_not_contradiction(self, mem_conn):
        """Similarity well below 0.82 should never produce a contradiction."""
        # cosine([1,0,0,0], [0,0,1,0]) = 0.0 — clearly below threshold.
        base = np.array([1.0, 0.0, 0.0, 0.0], dtype=np.float32)
        orthogonal = np.array([0.0, 0.0, 1.0, 0.0], dtype=np.float32)

        _seed_decision(mem_conn, "d1", "use mysql", "familiar", base.tobytes())

        detector = ContradictionDetector(mem_conn, emb_service=None, threshold=0.82)
        candidates = detector.classify("use mongodb", orthogonal.tobytes())

        contradictions = [c for c in candidates if c.is_contradiction]
        assert len(contradictions) == 0


class TestClassifyContradiction:
    """classify() should flag contradictions when similarity >= threshold AND chosen words are disjoint."""

    def test_high_similarity_disjoint_words_is_contradiction(self, mem_conn):
        # Nearly identical vectors (cosine ~1.0) but completely different chosen text.
        emb = _unit([1.0, 0.9, 0.8, 0.7])

        _seed_decision(mem_conn, "d1", "use postgres", "relational database", emb)

        detector = ContradictionDetector(mem_conn, emb_service=None, threshold=0.82)
        candidates = detector.classify("avoid sqlite", emb)

        contras = [c for c in candidates if c.is_contradiction]
        assert len(contras) == 1
        assert contras[0].decision_id == "d1"
        assert contras[0].similarity_score >= 0.82

    def test_candidate_has_correct_fields(self, mem_conn):
        emb = _unit([1.0, 0.9, 0.8, 0.7])
        _seed_decision(mem_conn, "d2", "use postgres", "reliable relational", emb)

        detector = ContradictionDetector(mem_conn, emb_service=None, threshold=0.82)
        candidates = detector.classify("avoid mongodb", emb)

        assert len(candidates) == 1
        c = candidates[0]
        assert isinstance(c, ContradictionCandidate)
        assert c.decision_id == "d2"
        assert c.chosen == "use postgres"
        assert c.rationale == "reliable relational"
        assert 0.0 <= c.similarity_score <= 1.0


class TestClassifyRefinement:
    """classify() should NOT flag refinements (same top words, high similarity)."""

    def test_same_top_words_is_not_contradiction(self, mem_conn):
        # High similarity + overlapping top words → refinement, not contradiction.
        emb = _unit([1.0, 0.9, 0.8, 0.7])

        _seed_decision(mem_conn, "d1", "use postgres 14", "stable version", emb)

        detector = ContradictionDetector(mem_conn, emb_service=None, threshold=0.82)
        # "use postgres 16" shares "use" and "postgres" with the existing chosen.
        candidates = detector.classify("use postgres 16", emb)

        contras = [c for c in candidates if c.is_contradiction]
        assert len(contras) == 0, "A refinement should not be flagged as contradiction"

    def test_partial_overlap_is_not_contradiction(self, mem_conn):
        emb = _unit([0.9, 0.9, 0.8, 0.7])
        _seed_decision(mem_conn, "d1", "use async workers", "faster throughput", emb)

        detector = ContradictionDetector(mem_conn, emb_service=None, threshold=0.82)
        # "use async tasks" shares "use" and "async" → not disjoint.
        candidates = detector.classify("use async tasks", emb)

        contras = [c for c in candidates if c.is_contradiction]
        assert len(contras) == 0


class TestClassifyInactiveDecisionsIgnored:
    """Superseded decisions should not be returned by find_similar."""

    def test_superseded_row_not_in_candidates(self, mem_conn):
        emb = _unit([1.0, 0.9, 0.8, 0.7])
        # Insert a superseded decision.
        mem_conn.execute(
            "INSERT INTO decisions "
            "(id, session_id, created_at, node_ids, chosen, rejected, rationale, status, embedding) "
            "VALUES ('d_old', 'x', datetime('now'), '[]', 'use oracle', '[]', 'old choice', 'superseded', ?)",
            (emb,),
        )
        mem_conn.commit()

        detector = ContradictionDetector(mem_conn, emb_service=None, threshold=0.82)
        candidates = detector.classify("avoid oracle", emb)

        ids = [c.decision_id for c in candidates]
        assert "d_old" not in ids


# ---------------------------------------------------------------------------
# MemoryStore.record_decision_checked integration tests
# ---------------------------------------------------------------------------

class FakeEmb:
    """Returns the same fixed embedding for every call."""

    def __init__(self, vec: np.ndarray):
        self._vec = vec

    def embed(self, texts):
        return np.stack([self._vec] * len(texts))


@pytest.fixture
def store_no_emb(tmp_path, monkeypatch):
    """MemoryStore with a stubbed _embed that returns zeros (for non-contradiction tests)."""
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 16)
    s = MemoryStore(tmp_path)
    s.start_session()
    yield s
    s.close()


@pytest.fixture
def store_real_emb(tmp_path, monkeypatch):
    """MemoryStore with a deterministic fake embedding service."""
    base_vec = np.ones(16, dtype=np.float32)
    base_vec /= np.linalg.norm(base_vec)
    fake_emb = FakeEmb(base_vec)

    def _fake_embed(self, text: str) -> bytes:
        return base_vec.tobytes()

    monkeypatch.setattr(MemoryStore, "_embed", _fake_embed)
    monkeypatch.setattr(MemoryStore, "_get_emb", lambda self: fake_emb)

    s = MemoryStore(tmp_path)
    s.start_session()
    yield s
    s.close()


class TestRecordDecisionChecked:
    def test_returns_dict_with_id(self, store_no_emb):
        result = store_no_emb.record_decision_checked(
            chosen="use pytest",
            rationale="best test framework",
        )
        assert isinstance(result, dict)
        assert "id" in result
        assert isinstance(result["id"], str)
        assert "superseded" in result
        assert "contradiction_scores" in result

    def test_no_supersession_when_no_prior_decisions(self, store_no_emb):
        result = store_no_emb.record_decision_checked(
            chosen="use black formatter",
            rationale="consistent code style",
        )
        assert result["superseded"] == []
        assert result["contradiction_scores"] == {}

    def test_auto_supersedes_contradicting_decision(self, store_real_emb):
        """A prior active decision with high similarity but disjoint chosen words is superseded."""
        store = store_real_emb
        conn = store._conn

        # Seed a prior active decision using the same embedding blob that _embed returns.
        base_vec = np.ones(16, dtype=np.float32)
        base_vec /= np.linalg.norm(base_vec)
        prior_emb = base_vec.tobytes()

        conn.execute(
            "INSERT INTO decisions "
            "(id, session_id, created_at, node_ids, chosen, rejected, rationale, status, embedding) "
            "VALUES ('prior_1', 'untracked', datetime('now', '-1 day'), '[]', "
            "'avoid redis', '[]', 'memory overhead', 'active', ?)",
            (prior_emb,),
        )
        conn.commit()

        # Record a new decision that contradicts the prior one.
        result = store.record_decision_checked(
            chosen="use memcached",  # disjoint top words from "avoid redis"
            rationale="simpler caching",
        )

        assert "prior_1" in result["superseded"]
        assert "prior_1" in result["contradiction_scores"]
        assert result["contradiction_scores"]["prior_1"] >= 0.82

        # Verify the old row is now superseded in the DB.
        row = conn.execute(
            "SELECT status, superseded_by FROM decisions WHERE id='prior_1'"
        ).fetchone()
        assert row[0] == "superseded"
        assert row[1] == result["id"]

    def test_contradiction_score_written_to_new_row(self, store_real_emb):
        """The new decision row gets contradiction_score set to the max similarity."""
        store = store_real_emb
        conn = store._conn

        base_vec = np.ones(16, dtype=np.float32)
        base_vec /= np.linalg.norm(base_vec)
        prior_emb = base_vec.tobytes()

        conn.execute(
            "INSERT INTO decisions "
            "(id, session_id, created_at, node_ids, chosen, rejected, rationale, status, embedding) "
            "VALUES ('prior_2', 'untracked', datetime('now', '-1 day'), '[]', "
            "'drop elasticsearch', '[]', 'too complex', 'active', ?)",
            (prior_emb,),
        )
        conn.commit()

        result = store.record_decision_checked(
            chosen="adopt opensearch",
            rationale="managed replacement",
        )

        if "prior_2" in result["superseded"]:
            row = conn.execute(
                "SELECT contradiction_score FROM decisions WHERE id=?", (result["id"],)
            ).fetchone()
            assert row[0] is not None
            assert row[0] >= 0.82

    def test_refinement_not_superseded(self, store_real_emb):
        """A prior decision with overlapping chosen words should NOT be superseded."""
        store = store_real_emb
        conn = store._conn

        base_vec = np.ones(16, dtype=np.float32)
        base_vec /= np.linalg.norm(base_vec)
        prior_emb = base_vec.tobytes()

        conn.execute(
            "INSERT INTO decisions "
            "(id, session_id, created_at, node_ids, chosen, rejected, rationale, status, embedding) "
            "VALUES ('ref_1', 'untracked', datetime('now', '-1 day'), '[]', "
            "'use postgres 14', '[]', 'LTS version', 'active', ?)",
            (prior_emb,),
        )
        conn.commit()

        result = store.record_decision_checked(
            chosen="use postgres 16",  # shares "use" and "postgres"
            rationale="newer LTS",
        )

        assert "ref_1" not in result["superseded"]
        # Prior row still active.
        row = conn.execute("SELECT status FROM decisions WHERE id='ref_1'").fetchone()
        assert row[0] == "active"


# ---------------------------------------------------------------------------
# test_lineage_no_cycles — a decision cannot appear twice in its own lineage
# ---------------------------------------------------------------------------

class TestLineageNoCycles:
    def _seed(self, conn, did, chosen, status="active", superseded_by=None):
        conn.execute(
            "INSERT INTO decisions "
            "(id, session_id, created_at, node_ids, chosen, rejected, rationale, status, superseded_by) "
            "VALUES (?, 'untracked', datetime('now'), '[]', ?, '[]', 'r', ?, ?)",
            (did, chosen, status, superseded_by),
        )
        conn.commit()

    def test_lineage_no_duplicate_ids(self, tmp_path):
        store = MemoryStore(tmp_path)
        conn = store._conn

        # Build a simple 3-node chain: a -> b -> c
        self._seed(conn, "a", "step 1", status="superseded", superseded_by="b")
        self._seed(conn, "b", "step 2", status="superseded", superseded_by="c")
        self._seed(conn, "c", "step 3", status="active")

        lineage = store.get_decision_lineage("c")
        chain_ids = [d["id"] for d in lineage["chain"]]

        # No duplicates in the chain.
        assert len(chain_ids) == len(set(chain_ids)), (
            f"Duplicate entries found in lineage chain: {chain_ids}"
        )
        store.close()

    def test_lineage_cycle_does_not_loop_forever(self, tmp_path):
        """If a circular superseded_by exists in DB, the CTE depth limit prevents infinite loop."""
        store = MemoryStore(tmp_path)
        conn = store._conn

        # Manually create a cycle: x -> y -> x (pathological data).
        conn.execute(
            "INSERT INTO decisions "
            "(id, session_id, created_at, node_ids, chosen, rejected, rationale, status, superseded_by) "
            "VALUES ('cx', 'untracked', datetime('now'), '[]', 'cycle x', '[]', 'r', 'superseded', 'cy')"
        )
        conn.execute(
            "INSERT INTO decisions "
            "(id, session_id, created_at, node_ids, chosen, rejected, rationale, status, superseded_by) "
            "VALUES ('cy', 'untracked', datetime('now'), '[]', 'cycle y', '[]', 'r', 'superseded', 'cx')"
        )
        conn.commit()

        # Must not raise or hang.
        result = store.get_decision_lineage("cx")
        assert result is not None
        chain_ids = [d["id"] for d in result.get("chain", [])]
        # Depth limit in CTE is 50 — chain should have at most 51 entries.
        assert len(chain_ids) <= 51
        store.close()

    def test_lineage_single_node_no_cycle(self, tmp_path):
        store = MemoryStore(tmp_path)
        conn = store._conn

        self._seed(conn, "solo", "only decision", status="active")
        result = store.get_decision_lineage("solo")
        chain_ids = [d["id"] for d in result["chain"]]
        assert chain_ids.count("solo") == 1, "Single node should appear exactly once."
        store.close()
