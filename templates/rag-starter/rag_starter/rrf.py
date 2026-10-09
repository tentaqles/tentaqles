"""Reciprocal Rank Fusion.

score(d) = sum over lists L containing d of  weight_L / (k + rank_L(d))

`rank` is 1-based. k = 60 is the value from the original paper (Cormack et al.,
2009) and the one the SQL function uses. A larger k flattens the curve, so
agreement between lists matters more than being first in one of them.
"""

from __future__ import annotations

from collections.abc import Hashable, Mapping, Sequence
from dataclasses import dataclass, field
from typing import Generic, TypeVar

K_DEFAULT = 60
T = TypeVar("T", bound=Hashable)


@dataclass
class Fused(Generic[T]):
    item: T
    score: float
    ranks: dict[str, int] = field(default_factory=dict)  # list name -> 1-based rank


def rrf_fuse(
    rankings: Mapping[str, Sequence[T]],
    k: int = K_DEFAULT,
    weights: Mapping[str, float] | None = None,
    limit: int | None = None,
) -> list[Fused[T]]:
    """Fuse named rankings (best first). Ties keep the first-seen order, so output is deterministic."""
    if k < 0:
        raise ValueError("k must be >= 0")
    fused: dict[T, Fused[T]] = {}
    for name, items in rankings.items():
        w = 1.0 if weights is None else weights.get(name, 1.0)
        seen: set[T] = set()
        for rank, item in enumerate(items, start=1):
            if item in seen:  # a list should not vote twice for the same item
                continue
            seen.add(item)
            f = fused.setdefault(item, Fused(item, 0.0))
            f.score += w / (k + rank)
            f.ranks[name] = rank
    out = sorted(fused.values(), key=lambda f: -f.score)  # stable sort keeps first-seen ties
    return out[:limit] if limit is not None else out
