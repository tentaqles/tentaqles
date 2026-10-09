"""Retrieval pipeline: embed -> hybrid search (vector + FTS, RRF) -> filter fallback -> rerank."""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import Any

from .embeddings import Embedder
from .models import Candidate
from .rerank import NoopReranker, Reranker
from .store import SearchBackend

log = logging.getLogger(__name__)


@dataclass
class RetrievalResult:
    query: str
    candidates: list[Candidate]
    filters_requested: dict[str, Any] | None
    filters_applied: dict[str, Any] | None
    filter_fallback: bool = False
    fetched: int = 0

    def trace(self) -> list[dict[str, Any]]:
        """Per-chunk stage scores, e.g. `rerank 0.75, rrf 0.03, similarity 0.47`, for logs/Langfuse."""
        return [{"chunk_id": c.chunk_id, **c.scores()} for c in self.candidates]


@dataclass
class Retriever:
    store: SearchBackend
    embedder: Embedder
    reranker: Reranker = field(default_factory=NoopReranker)
    candidates: int = 40  # fused candidates sent to the reranker
    top_n: int = 8  # chunks handed to the generator

    def retrieve(self, query: str, filters: dict[str, Any] | None = None) -> RetrievalResult:
        """Search with `filters`; if that returns nothing, retry once without them.

        Agents over-filter (e.g. `document_type=article` when the doc is a
        `reference`) and then get zero rows. An empty filtered result is
        treated as a bad filter, not as "no answer exists".
        """
        query = query.strip()
        if not query:
            return RetrievalResult(query, [], filters, filters)
        vec = self.embedder.embed([query])[0]
        applied = filters or None
        hits = self.store.hybrid_search(query, vec, self.candidates, applied)
        fallback = False
        if not hits and applied:
            log.info("filters %s returned nothing; retrying unfiltered", applied)
            hits = self.store.hybrid_search(query, vec, self.candidates, None)
            applied, fallback = None, True
        ranked = self.reranker.rerank(query, hits, self.top_n)
        return RetrievalResult(query, ranked, filters, applied, fallback, fetched=len(hits))
