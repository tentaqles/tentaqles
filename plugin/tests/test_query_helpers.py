"""Tests for tentaqles.memory.query_helpers."""

import sqlite3

import numpy as np
import pytest

from tentaqles.memory.query_helpers import cosine_similarity_blob, top_k_by_embedding


def _blob(vec: list[float]) -> bytes:
    return np.array(vec, dtype=np.float32).tobytes()


class TestCosineSimilarityBlob:
    def test_identical_vectors(self):
        blob = _blob([1.0, 0.0, 0.0])
        assert cosine_similarity_blob(blob, blob) == pytest.approx(1.0)

    def test_orthogonal_vectors(self):
        a = _blob([1.0, 0.0])
        b = _blob([0.0, 1.0])
        assert cosine_similarity_blob(a, b) == pytest.approx(0.0, abs=1e-6)

    def test_opposite_vectors(self):
        a = _blob([1.0, 0.0])
        b = _blob([-1.0, 0.0])
        assert cosine_similarity_blob(a, b) == pytest.approx(-1.0)

    def test_empty_blob_returns_zero(self):
        a = _blob([1.0, 0.0])
        assert cosine_similarity_blob(a, b"") == 0.0
        assert cosine_similarity_blob(b"", a) == 0.0
        assert cosine_similarity_blob(b"", b"") == 0.0

    def test_known_vectors(self):
        a = _blob([3.0, 4.0])
        b = _blob([4.0, 3.0])
        # dot = 12 + 12 = 24, |a|=|b|=5, cos = 24/25
        assert cosine_similarity_blob(a, b) == pytest.approx(24 / 25, abs=1e-5)


class TestTopKByEmbedding:
    def _make_db(self):
        conn = sqlite3.connect(":memory:")
        conn.execute(
            """CREATE TABLE items (
                id TEXT PRIMARY KEY,
                text TEXT,
                embedding BLOB
            )"""
        )
        vecs = {
            "a": [1.0, 0.0, 0.0],
            "b": [0.0, 1.0, 0.0],
            "c": [0.0, 0.0, 1.0],
            "d": [1.0, 1.0, 0.0],
        }
        for iid, vec in vecs.items():
            conn.execute(
                "INSERT INTO items VALUES (?, ?, ?)",
                (iid, f"text_{iid}", _blob(vec)),
            )
        conn.commit()
        return conn

    def test_top1_returns_best_match(self):
        conn = self._make_db()
        query = _blob([1.0, 0.0, 0.0])
        results = top_k_by_embedding(conn, query, "items", "embedding", "id", limit=1)
        assert len(results) == 1
        assert results[0][0] == "a"
        assert results[0][1] == pytest.approx(1.0)

    def test_sorted_descending(self):
        conn = self._make_db()
        query = _blob([1.0, 0.0, 0.0])
        results = top_k_by_embedding(conn, query, "items", "embedding", "id", limit=4)
        scores = [r[1] for r in results]
        assert scores == sorted(scores, reverse=True)

    def test_extra_cols_included(self):
        conn = self._make_db()
        query = _blob([1.0, 0.0, 0.0])
        results = top_k_by_embedding(
            conn, query, "items", "embedding", "id", limit=1, extra_cols=["text"]
        )
        assert len(results[0]) == 3  # (id, score, text)
        assert results[0][2] == "text_a"

    def test_null_embeddings_excluded(self):
        conn = self._make_db()
        conn.execute("INSERT INTO items VALUES ('e', 'no emb', NULL)")
        conn.commit()
        query = _blob([1.0, 0.0, 0.0])
        results = top_k_by_embedding(conn, query, "items", "embedding", "id")
        ids = [r[0] for r in results]
        assert "e" not in ids

    def test_limit_respected(self):
        conn = self._make_db()
        query = _blob([1.0, 0.0, 0.0])
        results = top_k_by_embedding(conn, query, "items", "embedding", "id", limit=2)
        assert len(results) == 2
