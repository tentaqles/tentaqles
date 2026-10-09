"""Retrieval metrics over source ids (documents), not chunk ids.

Chunk ids change whenever chunking changes, while source ids are what a human
labels in a golden set. Retrieved lists are de-duplicated by source, keeping the
first (best) rank, before scoring.
"""

from __future__ import annotations

from collections.abc import Iterable, Sequence


def dedupe(ids: Iterable[str]) -> list[str]:
    seen: set[str] = set()
    out = []
    for i in ids:
        if i not in seen:
            seen.add(i)
            out.append(i)
    return out


def recall_at_k(retrieved: Sequence[str], relevant: Iterable[str], k: int) -> float:
    """|relevant ∩ top-k| / |relevant|. Returns 1.0 when nothing is relevant (vacuously found)."""
    rel = set(relevant)
    if not rel:
        return 1.0
    top = set(dedupe(retrieved)[:k])
    return len(top & rel) / len(rel)


def reciprocal_rank(retrieved: Sequence[str], relevant: Iterable[str], k: int | None = None) -> float:
    """1 / rank of the first relevant id (1-based), 0 if none appears in the top k."""
    rel = set(relevant)
    ranked = dedupe(retrieved)
    if k is not None:
        ranked = ranked[:k]
    for rank, i in enumerate(ranked, start=1):
        if i in rel:
            return 1.0 / rank
    return 0.0


def hit_at_k(retrieved: Sequence[str], relevant: Iterable[str], k: int) -> float:
    rel = set(relevant)
    return 1.0 if set(dedupe(retrieved)[:k]) & rel else 0.0


def mean(values: Sequence[float]) -> float:
    return sum(values) / len(values) if values else 0.0
