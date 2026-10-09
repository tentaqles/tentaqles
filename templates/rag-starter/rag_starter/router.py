"""Per-query model routing with a Jev choice question.

Jev answers "cheap or strong?" in about 300 ms for a fraction of a cent. If
the call fails, or Jev is not confident enough, the router uses `fallback`
(the strong model by default). A misroute to the cheap model costs answer
quality; a misroute to the strong model only costs money.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field

from .jev import Decider, JevError, choice, untrusted_state

log = logging.getLogger(__name__)


@dataclass(frozen=True)
class Route:
    model: str
    tier: str  # "cheap" | "strong"
    reason: str  # "jev" | "low_confidence" | "error"
    confidence: float = 0.0


@dataclass
class ModelRouter:
    decider: Decider
    cheap_model: str
    strong_model: str
    min_confidence: float = 0.7
    fallback_tier: str = "strong"
    criteria: dict[str, str] = field(
        default_factory=lambda: {
            "cheap": "a lookup, greeting, or single-fact question answerable from one passage",
            "strong": "multi-step reasoning, comparison across documents, math, code, or ambiguity",
        }
    )

    def _model(self, tier: str) -> str:
        return self.cheap_model if tier == "cheap" else self.strong_model

    def route(self, query: str) -> Route:
        fb = self.fallback_tier
        try:
            resp = self.decider.ask(
                untrusted_state(query, context="Pick the cheapest model that can answer this user query well."),
                {
                    "tier": {
                        "type": "choice",
                        "instructions": "Which model tier does the query in `data` need?",
                        "criteria": self.criteria,
                    }
                },
            )
            tier, conf, _ = choice(resp["answers"], "tier")
        except (JevError, KeyError, TypeError, ValueError) as e:
            log.warning("router fell back to %s (%s)", fb, type(e).__name__)
            return Route(self._model(fb), fb, "error")
        if tier not in self.criteria:
            return Route(self._model(fb), fb, "error", conf)
        if conf < self.min_confidence:
            return Route(self._model(fb), fb, "low_confidence", conf)
        return Route(self._model(tier), tier, "jev", conf)
