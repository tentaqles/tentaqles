from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass(frozen=True)
class SourceDocument:
    """One document as it exists in the source system (a file, a CMS page, a row)."""

    source_id: str
    text: str
    title: str = ""
    metadata: dict[str, Any] = field(default_factory=dict)


@dataclass
class Chunk:
    chunk_id: str
    source_id: str
    index: int
    content: str
    content_hash: str
    metadata: dict[str, Any] = field(default_factory=dict)
    embedding: list[float] | None = None


@dataclass
class Candidate:
    """A retrieved chunk plus the score from each stage, so traces show where it came from."""

    chunk_id: str
    source_id: str
    content: str
    metadata: dict[str, Any] = field(default_factory=dict)
    similarity: float | None = None
    fts_rank: float | None = None
    rrf_score: float = 0.0
    rerank_score: float | None = None

    def scores(self) -> dict[str, float | None]:
        return {
            "similarity": self.similarity,
            "fts_rank": self.fts_rank,
            "rrf": self.rrf_score,
            "rerank": self.rerank_score,
        }
