"""Faithfulness hook: is the generated answer supported by the retrieved context?

`FaithfulnessCheck` is a protocol so you can plug in Jev (cheap, ~300 ms), an
LLM judge, or both. A good pattern is Jev first and an LLM judge only when Jev
is unsure (0.35 < p < 0.65). It keeps judge spend to the hard cases.
"""

from __future__ import annotations

from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import Protocol

from ..jev import Decider, noul, untrusted_state


class FaithfulnessCheck(Protocol):
    def score(self, question: str, answer: str, contexts: Sequence[str]) -> float:
        """Probability in [0, 1] that every claim in `answer` is supported by `contexts`."""
        ...


@dataclass
class CallableCheck:
    """Wrap any function `(question, answer, contexts) -> float` as a check."""

    fn: Callable[[str, str, Sequence[str]], float]

    def score(self, question: str, answer: str, contexts: Sequence[str]) -> float:
        return float(self.fn(question, answer, contexts))


@dataclass
class JevFaithfulness:
    decider: Decider
    max_context_chars: int = 12000

    def score(self, question: str, answer: str, contexts: Sequence[str]) -> float:
        joined, used = [], 0
        for i, c in enumerate(contexts):
            if used >= self.max_context_chars:
                break
            piece = c[: self.max_context_chars - used]
            joined.append({"id": f"ctx{i}", "text": piece})
            used += len(piece)
        state = untrusted_state(
            {"question": question, "answer": answer, "contexts": joined},
            context="Citation check for a RAG answer.",
        )
        resp = self.decider.ask(
            state,
            {
                "supported": {
                    "type": "noul",
                    "instructions": "Is every factual claim in `answer` supported by the `contexts`?",
                    "criteria": {
                        "true": "all claims are stated in or directly implied by the contexts",
                        "false": "at least one claim is missing from, or contradicts, the contexts",
                    },
                }
            },
        )
        return noul(resp["answers"], "supported")


@dataclass
class TieredFaithfulness:
    """Jev first, then `judge` only when Jev lands in the uncertain band."""

    primary: FaithfulnessCheck
    judge: FaithfulnessCheck
    low: float = 0.35
    high: float = 0.65

    def score(self, question: str, answer: str, contexts: Sequence[str]) -> float:
        p = self.primary.score(question, answer, contexts)
        if self.low < p < self.high:
            return self.judge.score(question, answer, contexts)
        return p
