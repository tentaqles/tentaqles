"""Contradiction detection for decisions — Feature 8.

Detects when a newly recorded decision semantically contradicts an existing
active decision, using cosine similarity on embeddings plus a keyword
disjointness heuristic.
"""

from __future__ import annotations

import sqlite3
from dataclasses import dataclass, field

from tentaqles.memory.query_helpers import cosine_similarity_blob, top_k_by_embedding

# Common English stopwords to drop before keyword comparison.
_STOPWORDS: frozenset[str] = frozenset({
    "a", "an", "the", "and", "or", "but", "in", "on", "at", "to", "for",
    "of", "with", "by", "from", "as", "is", "it", "its", "was", "are",
    "be", "been", "this", "that", "we", "use", "using", "used", "should",
    "will", "can", "do", "not", "no", "so", "if", "than", "then", "all",
    "i", "s", "our", "over",
})


def _significant_words(text: str, n: int = 3) -> set[str]:
    """Return the top-n significant (non-stopword) words from text, lowercased."""
    words = [w.strip(".,;:!?\"'()[]{}") for w in text.lower().split()]
    sig = [w for w in words if w and w not in _STOPWORDS]
    return set(sig[:n])


@dataclass
class ContradictionCandidate:
    """A candidate active decision that may contradict the new one."""

    decision_id: str
    similarity_score: float
    chosen: str
    rationale: str
    is_contradiction: bool


class ContradictionDetector:
    """Detect contradictions between a new decision and active existing decisions.

    Args:
        conn: Open sqlite3 connection to the memory database.
        emb_service: EmbeddingService instance (or None to skip embedding-based
            similarity; similarity will be 0.0 for all candidates).
        threshold: Cosine similarity above which a candidate is eligible for
            contradiction classification. Default: 0.82.
    """

    def __init__(
        self,
        conn: sqlite3.Connection,
        emb_service,
        threshold: float = 0.82,
    ) -> None:
        self._conn = conn
        self._emb_service = emb_service
        self._threshold = threshold

    def find_similar(
        self, embedding: bytes, limit: int = 5
    ) -> list[ContradictionCandidate]:
        """Return up to *limit* active decisions most similar to *embedding*.

        Queries only rows with status='active' and a non-NULL embedding blob.
        Uses :func:`~tentaqles.memory.query_helpers.cosine_similarity_blob`
        for scoring.

        Returns:
            List of :class:`ContradictionCandidate` sorted by descending
            similarity.  ``is_contradiction`` is set to False at this stage;
            call :meth:`classify` to apply the disjointness heuristic.
        """
        rows = self._conn.execute(
            "SELECT id, embedding, chosen, rationale "
            "FROM decisions "
            "WHERE status = 'active' AND embedding IS NOT NULL"
        ).fetchall()

        scored: list[tuple[float, str, str, str]] = []
        for row_id, blob, chosen, rationale in rows:
            score = cosine_similarity_blob(embedding, blob)
            scored.append((score, row_id, chosen, rationale))

        scored.sort(key=lambda x: x[0], reverse=True)
        top = scored[:limit]

        return [
            ContradictionCandidate(
                decision_id=row_id,
                similarity_score=score,
                chosen=chosen,
                rationale=rationale,
                is_contradiction=False,
            )
            for score, row_id, chosen, rationale in top
        ]

    def classify(
        self, new_chosen: str, new_embedding: bytes
    ) -> list[ContradictionCandidate]:
        """Classify candidates as contradictions or refinements.

        A candidate is marked ``is_contradiction=True`` when BOTH conditions
        hold:

        1. ``similarity_score >= threshold``  (semantically close)
        2. The top-3 significant words of *new_chosen* are **disjoint** from
           the top-3 significant words of ``candidate.chosen``  (different
           direction/intent)

        Candidates below the threshold are still returned with
        ``is_contradiction=False`` so callers can inspect the full set.

        Args:
            new_chosen: The ``chosen`` field of the decision being recorded.
            new_embedding: Embedding blob for the new decision text.

        Returns:
            List of :class:`ContradictionCandidate`, ordered by descending
            similarity.
        """
        candidates = self.find_similar(new_embedding)
        new_top_words = _significant_words(new_chosen)

        result: list[ContradictionCandidate] = []
        for cand in candidates:
            is_contradiction = False
            if cand.similarity_score >= self._threshold:
                cand_top_words = _significant_words(cand.chosen)
                # Disjoint means zero overlap
                is_contradiction = new_top_words.isdisjoint(cand_top_words)
            result.append(
                ContradictionCandidate(
                    decision_id=cand.decision_id,
                    similarity_score=cand.similarity_score,
                    chosen=cand.chosen,
                    rationale=cand.rationale,
                    is_contradiction=is_contradiction,
                )
            )
        return result
