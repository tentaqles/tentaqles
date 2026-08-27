"""Core embedding service — model loading, embedding, similarity search."""

from __future__ import annotations

import numpy as np
from pathlib import Path
from typing import Sequence

from .cache import EmbeddingCache
from tentaqles.config import cache_dir

# Default model — snowflake-arctic-embed-s is 6x faster than bge-small on this system
DEFAULT_MODEL = "snowflake/snowflake-arctic-embed-s"
CACHE_DIR = cache_dir() / "embeddings"


class EmbeddingService:
    """Lightweight embedding service backed by fastembed + persistent disk cache."""

    def __init__(self, model_name: str = DEFAULT_MODEL, cache_dir: Path = CACHE_DIR):
        self._model_name = model_name
        self._model = None  # lazy load
        self._cache = EmbeddingCache(cache_dir / model_name.replace("/", "_"))
        self._dim: int | None = None

    def _ensure_model(self):
        if self._model is not None:
            return
        from fastembed import TextEmbedding
        self._model = TextEmbedding(self._model_name)
        # Determine dimension from a test embed
        test = list(self._model.embed(["test"]))[0]
        self._dim = len(test)

    @property
    def dimension(self) -> int:
        self._ensure_model()
        return self._dim

    def embed(self, texts: Sequence[str]) -> np.ndarray:
        """Embed a batch of texts, using cache where possible.

        Returns an (N, dim) numpy array of float32 embeddings.
        """
        self._ensure_model()
        texts = list(texts)
        if not texts:
            return np.empty((0, self._dim), dtype=np.float32)

        # Check cache for each text
        results = [None] * len(texts)
        uncached_indices = []
        for i, text in enumerate(texts):
            cached = self._cache.get(text)
            if cached is not None:
                results[i] = cached
            else:
                uncached_indices.append(i)

        # Embed uncached texts in batch
        if uncached_indices:
            uncached_texts = [texts[i] for i in uncached_indices]
            new_embeddings = list(self._model.embed(uncached_texts))
            for idx, emb in zip(uncached_indices, new_embeddings):
                vec = np.array(emb, dtype=np.float32)
                results[idx] = vec
                self._cache.put(texts[idx], vec)

        return np.stack(results)

    def embed_batch_from_db(
        self,
        conn,
        table: str,
        id_col: str,
        text_col: str,
        embedding_col: str,
        batch_size: int = 32,
    ) -> int:
        """Embed rows from a SQLite table that are missing an embedding.

        Reads rows where embedding_col IS NULL, embeds their text in batches
        of batch_size, and writes the resulting float32 blobs back.

        Args:
            conn: Open sqlite3 connection.
            table: Table to process.
            id_col: Primary key column name.
            text_col: Column whose text content should be embedded.
            embedding_col: Column to write the embedding blob into.
            batch_size: Number of texts to embed per model call.

        Returns:
            Number of rows embedded.
        """
        rows = conn.execute(
            f"SELECT {id_col}, {text_col} FROM {table} WHERE {embedding_col} IS NULL AND {text_col} IS NOT NULL"
        ).fetchall()

        total = 0
        for batch_start in range(0, len(rows), batch_size):
            batch = rows[batch_start : batch_start + batch_size]
            ids = [r[0] for r in batch]
            texts = [r[1] for r in batch]
            embeddings = self.embed(texts)  # (N, dim) float32
            for row_id, vec in zip(ids, embeddings):
                blob = vec.tobytes()
                conn.execute(
                    f"UPDATE {table} SET {embedding_col} = ? WHERE {id_col} = ?",
                    (blob, row_id),
                )
            conn.commit()
            total += len(batch)

        return total

    def search(
        self,
        query: str,
        corpus_texts: Sequence[str],
        corpus_embeddings: np.ndarray | None = None,
        top_k: int = 5,
    ) -> list[tuple[int, float]]:
        """Find top-K most similar texts to query.

        Args:
            query: The search query.
            corpus_texts: The texts to search through (used for embedding if corpus_embeddings is None).
            corpus_embeddings: Pre-computed embeddings for corpus_texts. If None, computed on the fly.
            top_k: Number of results to return.

        Returns:
            List of (index, similarity_score) tuples, sorted by score descending.
        """
        self._ensure_model()

        query_emb = self.embed([query])[0]

        if corpus_embeddings is None:
            corpus_embeddings = self.embed(corpus_texts)

        # Cosine similarity: dot(q, c) / (||q|| * ||c||)
        # Normalize both for fast dot-product similarity
        query_norm = query_emb / (np.linalg.norm(query_emb) + 1e-10)
        corpus_norms = corpus_embeddings / (
            np.linalg.norm(corpus_embeddings, axis=1, keepdims=True) + 1e-10
        )
        similarities = corpus_norms @ query_norm

        # Top-K
        k = min(top_k, len(similarities))
        top_indices = np.argpartition(similarities, -k)[-k:]
        top_indices = top_indices[np.argsort(similarities[top_indices])[::-1]]

        return [(int(idx), float(similarities[idx])) for idx in top_indices]
