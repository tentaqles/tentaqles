"""Search backends.

`SearchBackend` is what retrieval and ingestion talk to. `InMemoryStore` mirrors
the SQL in `migrations/` (cosine kNN + keyword ranking, fused with RRF) so the
demo, the evals and the tests run with no database. `rag_starter.pg.PostgresStore`
is the production backend over Supabase.
"""

from __future__ import annotations

import math
import re
from collections import Counter
from typing import Any, Protocol

from .models import Candidate, Chunk
from .rrf import K_DEFAULT, rrf_fuse


class SearchBackend(Protocol):
    def upsert_chunks(self, chunks: list[Chunk]) -> None: ...

    def delete_source(self, source_id: str) -> None: ...

    def hybrid_search(
        self,
        query_text: str,
        query_embedding: list[float],
        match_count: int,
        filters: dict[str, Any] | None = None,
    ) -> list[Candidate]: ...


def matches(metadata: dict[str, Any], filters: dict[str, Any] | None) -> bool:
    """Containment, like `metadata @> filters` in Postgres (top-level keys)."""
    if not filters:
        return True
    for key, want in filters.items():
        have = metadata.get(key)
        if isinstance(have, list) and not isinstance(want, list):
            if want not in have:
                return False
        elif have != want:
            return False
    return True


_TOKEN = re.compile(r"[a-z0-9]+")
_STOP = frozenset("a an and are as at be by for from how i in is it of on or the to what when where which who why with".split())


def tokens(text: str) -> list[str]:
    return [t for t in _TOKEN.findall(text.lower()) if t not in _STOP]


def _cos(a: list[float], b: list[float]) -> float:
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a)) or 1.0
    nb = math.sqrt(sum(y * y for y in b)) or 1.0
    return dot / (na * nb)


class InMemoryStore:
    def __init__(self, rrf_k: int = K_DEFAULT, candidate_multiplier: int = 4):
        self.chunks: dict[str, Chunk] = {}
        self.rrf_k = rrf_k
        self.candidate_multiplier = candidate_multiplier

    def upsert_chunks(self, chunks: list[Chunk]) -> None:
        for c in chunks:
            if c.embedding is None:
                raise ValueError(f"chunk {c.chunk_id} has no embedding")
            self.chunks[c.chunk_id] = c

    def delete_source(self, source_id: str) -> None:
        for cid in [cid for cid, c in self.chunks.items() if c.source_id == source_id]:
            del self.chunks[cid]

    def source_ids(self) -> set[str]:
        return {c.source_id for c in self.chunks.values()}

    def hybrid_search(
        self,
        query_text: str,
        query_embedding: list[float],
        match_count: int,
        filters: dict[str, Any] | None = None,
    ) -> list[Candidate]:
        pool = [c for c in self.chunks.values() if matches(c.metadata, filters)]
        n = match_count * self.candidate_multiplier

        sims = {c.chunk_id: _cos(query_embedding, c.embedding or []) for c in pool}
        semantic = sorted(sims, key=lambda cid: -sims[cid])[:n]

        q = set(tokens(query_text))
        fts: dict[str, float] = {}
        for c in pool:
            tf = Counter(tokens(c.content))
            hit = sum(tf[t] for t in q)
            if hit:
                fts[c.chunk_id] = hit / (1 + math.log(1 + sum(tf.values())))
        full_text = sorted(fts, key=lambda cid: -fts[cid])[:n]

        fused = rrf_fuse({"full_text": full_text, "semantic": semantic}, k=self.rrf_k, limit=match_count)
        out = []
        for f in fused:
            c = self.chunks[f.item]
            out.append(
                Candidate(
                    chunk_id=c.chunk_id,
                    source_id=c.source_id,
                    content=c.content,
                    metadata=c.metadata,
                    similarity=sims.get(c.chunk_id),
                    fts_rank=fts.get(c.chunk_id),
                    rrf_score=f.score,
                )
            )
        return out
