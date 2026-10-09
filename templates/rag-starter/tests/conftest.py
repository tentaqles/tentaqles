from __future__ import annotations

import sys
from pathlib import Path
from typing import Any

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from rag_starter.models import Candidate  # noqa: E402


class FakeDecider:
    """Records every call; answers with `answer_fn(state, questions)` or raises `error`."""

    def __init__(self, answer_fn=None, error: Exception | None = None):
        self.answer_fn = answer_fn
        self.error = error
        self.calls: list[tuple[Any, dict[str, Any]]] = []

    def ask(self, state: Any, questions: dict[str, Any]) -> dict[str, Any]:
        self.calls.append((state, questions))
        if self.error:
            raise self.error
        return {"answers": self.answer_fn(state, questions), "usage": {"input_tokens": 100}}


def fake_secret() -> str:
    """A secret-shaped value assembled at runtime so no literal key lives in the repo."""
    return "sk-" + "test" + "Z9" * 12


@pytest.fixture
def candidates() -> list[Candidate]:
    return [
        Candidate(chunk_id=f"doc{i}#0", source_id=f"doc{i}", content=f"chunk {i} text", rrf_score=1.0 / (60 + i + 1))
        for i in range(45)
    ]
