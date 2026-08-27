"""SQLite query helpers for embedding-based retrieval.

Pure utility functions — no class, no side effects beyond reads.
Depends only on numpy (already a project dependency).
"""

from __future__ import annotations

import sqlite3

import numpy as np


def cosine_similarity_blob(blob1: bytes, blob2: bytes) -> float:
    """Compute cosine similarity between two float32 embedding blobs.

    Args:
        blob1: Raw bytes of a float32 numpy array.
        blob2: Raw bytes of a float32 numpy array.

    Returns:
        Cosine similarity in [-1.0, 1.0], or 0.0 if either blob is empty.
    """
    if not blob1 or not blob2:
        return 0.0
    vec1 = np.frombuffer(blob1, dtype=np.float32)
    vec2 = np.frombuffer(blob2, dtype=np.float32)
    norm1 = np.linalg.norm(vec1)
    norm2 = np.linalg.norm(vec2)
    if norm1 < 1e-10 or norm2 < 1e-10:
        return 0.0
    return float(np.dot(vec1, vec2) / (norm1 * norm2))


def top_k_by_embedding(
    conn: sqlite3.Connection,
    query_embedding: bytes,
    table: str,
    embedding_col: str,
    id_col: str,
    limit: int = 10,
    extra_cols: list[str] | None = None,
) -> list[tuple]:
    """Return the top-K rows most similar to query_embedding.

    Loads all rows where embedding_col IS NOT NULL, computes cosine similarity
    in Python, and returns the best matches sorted by descending similarity.

    Args:
        conn: Open sqlite3 connection.
        query_embedding: Blob of the query vector (float32 bytes).
        table: Table to search.
        embedding_col: Column that stores the embedding blob.
        id_col: Primary key / id column.
        limit: Maximum number of results.
        extra_cols: Additional column names to include in each result tuple.

    Returns:
        List of tuples: (id_value, similarity_score, *extra_col_values),
        sorted by similarity descending.
    """
    extra = extra_cols or []
    select_cols = ", ".join([id_col, embedding_col] + extra)
    rows = conn.execute(
        f"SELECT {select_cols} FROM {table} WHERE {embedding_col} IS NOT NULL"
    ).fetchall()

    scored: list[tuple] = []
    for row in rows:
        row_id = row[0]
        blob = row[1]
        extra_values = row[2:]
        score = cosine_similarity_blob(query_embedding, blob)
        scored.append((row_id, score) + tuple(extra_values))

    scored.sort(key=lambda r: r[1], reverse=True)
    return scored[:limit]
