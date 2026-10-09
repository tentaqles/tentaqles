"""Golden set: one JSON object per line (JSONL).

    {"id": "q1",
     "question": "How long do refunds take?",
     "relevant": ["refund-policy"],                    # source_ids a correct retrieval must surface
     "filters": {"document_type": "policy"},           # optional, passed to retrieve()
     "reference_answer": "Within 14 days ...",         # optional, for answer-level evals
     "tags": ["billing"]}                              # optional, for slicing reports

Keep it per client, 30-100 questions, written by someone who knows the
documents. Include questions whose answer lives in two documents, and a few
with `"relevant": []` (answer not in the corpus) to catch hallucinated retrieval.
"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class GoldenItem:
    id: str
    question: str
    relevant: tuple[str, ...]
    filters: dict[str, Any] = field(default_factory=dict)
    reference_answer: str = ""
    tags: tuple[str, ...] = ()


def load_golden(path: str | Path) -> list[GoldenItem]:
    items: list[GoldenItem] = []
    seen: set[str] = set()
    for n, line in enumerate(Path(path).read_text(encoding="utf-8").splitlines(), start=1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        try:
            raw = json.loads(line)
        except json.JSONDecodeError as e:
            raise ValueError(f"{path}:{n}: invalid JSON ({e.msg})") from None
        for key in ("id", "question", "relevant"):
            if key not in raw:
                raise ValueError(f"{path}:{n}: missing '{key}'")
        if not isinstance(raw["relevant"], list):
            raise ValueError(f"{path}:{n}: 'relevant' must be a list of source ids")
        if raw["id"] in seen:
            raise ValueError(f"{path}:{n}: duplicate id {raw['id']!r}")
        seen.add(raw["id"])
        items.append(
            GoldenItem(
                id=str(raw["id"]),
                question=str(raw["question"]),
                relevant=tuple(str(r) for r in raw["relevant"]),
                filters=dict(raw.get("filters") or {}),
                reference_answer=str(raw.get("reference_answer", "")),
                tags=tuple(raw.get("tags") or ()),
            )
        )
    if not items:
        raise ValueError(f"{path}: golden set is empty")
    return items
