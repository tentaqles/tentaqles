from __future__ import annotations

from collections.abc import Callable, Mapping, Sequence
from dataclasses import asdict, dataclass, field
from typing import Any

from ..models import Candidate
from .faithfulness import FaithfulnessCheck
from .golden import GoldenItem
from .metrics import hit_at_k, mean, recall_at_k, reciprocal_rank

# (question, filters) -> ranked candidates
RetrieveFn = Callable[[str, dict[str, Any]], Sequence[Candidate]]


@dataclass
class ItemResult:
    id: str
    recall: float
    rr: float
    hit: float
    retrieved: list[str]
    relevant: list[str]
    faithfulness: float | None = None
    error: str = ""


@dataclass
class EvalReport:
    k: int
    recall_at_k: float
    mrr: float
    hit_rate: float
    faithfulness: float | None
    items: list[ItemResult] = field(default_factory=list)
    failures: list[str] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        return not self.failures

    def to_dict(self) -> dict[str, Any]:
        d = asdict(self)
        d["passed"] = self.passed
        return d

    def summary(self) -> str:
        f = "n/a" if self.faithfulness is None else f"{self.faithfulness:.3f}"
        lines = [
            f"items={len(self.items)}  recall@{self.k}={self.recall_at_k:.3f}  "
            f"mrr={self.mrr:.3f}  hit@{self.k}={self.hit_rate:.3f}  faithfulness={f}",
        ]
        misses = [i for i in self.items if i.recall < 1.0 or i.error]
        for i in misses[:10]:
            why = f"error: {i.error}" if i.error else f"recall={i.recall:.2f} got={i.retrieved[: self.k]}"
            lines.append(f"  miss {i.id}: want={i.relevant} {why}")
        lines.append("PASS" if self.passed else "FAIL: " + "; ".join(self.failures))
        return "\n".join(lines)


def evaluate(
    retrieve: RetrieveFn,
    golden: Sequence[GoldenItem],
    k: int = 5,
    answers: Mapping[str, str] | None = None,
    faithfulness: FaithfulnessCheck | None = None,
    min_recall: float | None = None,
    min_mrr: float | None = None,
    min_faithfulness: float | None = None,
) -> EvalReport:
    items: list[ItemResult] = []
    faith_scores: list[float] = []
    for g in golden:
        try:
            cands = list(retrieve(g.question, dict(g.filters)))
            err = ""
        except Exception as e:  # one broken query must not hide the rest of the report
            cands, err = [], f"{type(e).__name__}: {e}"
        ids = [c.source_id for c in cands]
        res = ItemResult(
            id=g.id,
            recall=recall_at_k(ids, g.relevant, k),
            rr=reciprocal_rank(ids, g.relevant, k) if g.relevant else 1.0,
            hit=hit_at_k(ids, g.relevant, k) if g.relevant else 1.0,
            retrieved=ids,
            relevant=list(g.relevant),
            error=err,
        )
        if faithfulness is not None and answers and g.id in answers:
            try:
                res.faithfulness = faithfulness.score(g.question, answers[g.id], [c.content for c in cands[:k]])
                faith_scores.append(res.faithfulness)
            except Exception as e:
                res.error = res.error or f"faithfulness {type(e).__name__}"
                faith_scores.append(0.0)  # a failed check counts against the gate
        items.append(res)

    scored = [i for i in items if i.relevant]  # unanswerable questions don't dilute recall/MRR
    report = EvalReport(
        k=k,
        recall_at_k=mean([i.recall for i in scored]),
        mrr=mean([i.rr for i in scored]),
        hit_rate=mean([i.hit for i in scored]),
        faithfulness=mean(faith_scores) if faith_scores else None,
        items=items,
    )
    if min_recall is not None and report.recall_at_k < min_recall:
        report.failures.append(f"recall@{k} {report.recall_at_k:.3f} < {min_recall}")
    if min_mrr is not None and report.mrr < min_mrr:
        report.failures.append(f"mrr {report.mrr:.3f} < {min_mrr}")
    if min_faithfulness is not None:
        if report.faithfulness is None:
            report.failures.append("faithfulness threshold set but no answers were scored")
        elif report.faithfulness < min_faithfulness:
            report.failures.append(f"faithfulness {report.faithfulness:.3f} < {min_faithfulness}")
    errors = [i.id for i in items if i.error]
    if errors:
        report.failures.append(f"{len(errors)} item(s) errored: {errors[:5]}")
    return report
