"""Label DataFrame rows with Jev: batched, cached, retried, costed, reviewable.

Each row becomes one question (`r0`, `r1`, ...) in a request whose state
holds the batch's rows, redacted and framed as untrusted. Output columns:

    <name>               label (choice value, or bool for noul)
    <name>_confidence    choice confidence, or max(p, 1 - p) for noul
    <name>_p_<choice>    probability per choice (noul: <name>_p_true)
    <name>_status        ok | cached | error | dry_run

Rows under `review_threshold`, plus rows that errored, are also returned
(and optionally written) as a review CSV for a human.
"""

from __future__ import annotations

import hashlib
import json
import logging
import random
import time
from collections.abc import Callable
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

import pandas as pd

from .cache import LabelCache, MemoryCache
from .jev import PINNED_MODEL, Decider, JevError, cost_usd, estimate_tokens, untrusted_state

log = logging.getLogger(__name__)


@dataclass(frozen=True)
class LabelTask:
    name: str  # output column prefix, e.g. "topic"
    instructions: str  # e.g. "What is the support ticket about?"
    columns: tuple[str, ...]  # which DataFrame columns Jev sees
    choices: dict[str, str] | None = None  # choice task: {value: description}; None -> noul (yes/no)
    noul_criteria: dict[str, str] | None = None  # {"true": "...", "false": "..."}
    context: str = ""  # trusted framing, e.g. "B2B SaaS support inbox"

    @property
    def kind(self) -> str:
        return "choice" if self.choices else "noul"

    def fingerprint(self, model: str) -> str:
        """Changing the task or model invalidates cached labels automatically."""
        raw = json.dumps({**asdict(self), "model": model}, sort_keys=True)
        return hashlib.sha256(raw.encode()).hexdigest()[:16]

    def question(self, rid: str) -> dict[str, Any]:
        q: dict[str, Any] = {"type": self.kind, "instructions": f"{self.instructions} Answer for row {rid} only."}
        if self.choices:
            q["criteria"] = dict(self.choices)
        elif self.noul_criteria:
            q["criteria"] = dict(self.noul_criteria)
        return q

    @classmethod
    def from_json(cls, path: str | Path) -> LabelTask:
        raw = json.loads(Path(path).read_text(encoding="utf-8"))
        raw["columns"] = tuple(raw["columns"])
        return cls(**raw)


@dataclass
class LabelStats:
    rows: int = 0
    unique_rows: int = 0
    cached: int = 0
    labelled: int = 0
    failed: int = 0
    review: int = 0
    requests: int = 0
    retries: int = 0
    input_tokens: int = 0
    estimated_tokens: int = 0
    dry_run: bool = False

    @property
    def cost_usd(self) -> float:
        return cost_usd(self.input_tokens or self.estimated_tokens)

    def summary(self) -> str:
        kind = "estimated" if self.dry_run or not self.input_tokens else "actual"
        return (
            f"rows={self.rows} unique={self.unique_rows} cached={self.cached} labelled={self.labelled} "
            f"failed={self.failed} review={self.review} requests={self.requests} retries={self.retries} "
            f"tokens={self.input_tokens or self.estimated_tokens} ({kind}) cost=${self.cost_usd:.6f}"
        )


@dataclass
class LabelResult:
    df: pd.DataFrame
    review: pd.DataFrame
    stats: LabelStats


def _cell(v: Any, max_chars: int) -> Any:
    if v is None or (isinstance(v, float) and pd.isna(v)):
        return None
    if isinstance(v, (int, float, bool)):
        return v
    return str(v)[:max_chars]


@dataclass
class Labeller:
    decider: Decider
    task: LabelTask
    batch_size: int = 20
    review_threshold: float = 0.7
    cache: LabelCache = field(default_factory=MemoryCache)
    model: str = PINNED_MODEL  # only used for the cache fingerprint; the client pins the request
    max_retries: int = 4
    backoff_base: float = 0.5
    backoff_max: float = 8.0
    max_cell_chars: int = 2000
    sleep: Callable[[float], None] = time.sleep
    rng: random.Random = field(default_factory=random.Random)

    # ---- helpers -------------------------------------------------------------

    def _row_payload(self, row: pd.Series) -> dict[str, Any]:
        return {c: _cell(row[c], self.max_cell_chars) for c in self.task.columns}

    def _key(self, payload: dict[str, Any]) -> str:
        raw = self.task.fingerprint(self.model) + json.dumps(payload, sort_keys=True, default=str)
        return hashlib.sha256(raw.encode()).hexdigest()

    def _request(self, payloads: list[dict[str, Any]]) -> tuple[Any, dict[str, Any]]:
        ids = [f"r{i}" for i in range(len(payloads))]
        state = untrusted_state(
            dict(zip(ids, payloads)),
            context=self.task.context or "Label each row independently.",
        )
        return state, {rid: self.task.question(rid) for rid in ids}

    def _ask_with_retry(self, state: Any, questions: dict[str, Any], stats: LabelStats) -> dict[str, Any]:
        attempt = 0
        while True:
            try:
                stats.requests += 1
                return self.decider.ask(state, questions)
            except JevError as e:
                if not e.transient or attempt >= self.max_retries:
                    raise
                delay = min(self.backoff_max, self.backoff_base * 2**attempt) * (0.5 + self.rng.random() / 2)
                log.warning("jev %s, retry %d in %.2fs", e.status or "transport error", attempt + 1, delay)
                stats.retries += 1
                attempt += 1
                self.sleep(delay)

    def _parse(self, answer: Any) -> dict[str, Any]:
        if not isinstance(answer, dict):
            raise ValueError("missing answer")
        if self.task.kind == "noul":
            p = float(answer["noul"])
            return {"label": p >= 0.5, "confidence": max(p, 1 - p), "probs": {"true": p}}
        label = answer["choice"]
        if label not in (self.task.choices or {}):
            raise ValueError(f"unknown choice {label!r}")
        return {
            "label": label,
            "confidence": float(answer.get("confidence", 0.0)),
            "probs": {k: float(v) for k, v in (answer.get("probabilities") or {}).items()},
        }

    # ---- main ---------------------------------------------------------------------

    def label(self, df: pd.DataFrame, dry_run: bool = False, review_path: str | Path | None = None) -> LabelResult:
        missing = [c for c in self.task.columns if c not in df.columns]
        if missing:
            raise KeyError(f"columns not in DataFrame: {missing}")
        stats = LabelStats(rows=len(df), dry_run=dry_run)

        payloads: dict[str, dict[str, Any]] = {}
        row_keys: list[str] = []
        for _, row in df.iterrows():
            p = self._row_payload(row)
            k = self._key(p)
            payloads.setdefault(k, p)
            row_keys.append(k)
        stats.unique_rows = len(payloads)

        results: dict[str, dict[str, Any]] = {}
        status: dict[str, str] = {}
        for k, v in self.cache.get_many(list(payloads)).items():
            results[k], status[k] = v, "cached"
        stats.cached = len(results)

        todo = [k for k in payloads if k not in results]
        for i in range(0, len(todo), self.batch_size):
            keys = todo[i : i + self.batch_size]
            state, questions = self._request([payloads[k] for k in keys])
            if dry_run:
                stats.requests += 1
                stats.estimated_tokens += estimate_tokens({"state": state, "questions": questions})
                status.update({k: "dry_run" for k in keys})
                continue
            stats.estimated_tokens += estimate_tokens({"state": state, "questions": questions})
            try:
                resp = self._ask_with_retry(state, questions, stats)
            except JevError as e:
                log.error("batch of %d rows failed: %s", len(keys), e)
                status.update({k: "error" for k in keys})
                stats.failed += len(keys)
                continue
            stats.input_tokens += int((resp.get("usage") or {}).get("input_tokens") or 0)
            fresh: dict[str, dict[str, Any]] = {}
            for rid, k in zip(questions, keys):
                try:
                    fresh[k] = self._parse(resp["answers"].get(rid))
                    status[k] = "ok"
                except (KeyError, TypeError, ValueError):
                    status[k] = "error"
                    stats.failed += 1
            results.update(fresh)
            self.cache.put_many(fresh)
            stats.labelled += len(fresh)

        out = self._frame(df, row_keys, results, status)
        name = self.task.name
        conf = out[f"{name}_confidence"]
        needs = (out[f"{name}_status"] == "error") | (
            out[f"{name}_status"].isin(["ok", "cached"]) & (conf < self.review_threshold)
        )
        review = out[needs].copy()
        review["review_reason"] = review[f"{name}_status"].map(lambda s: "error" if s == "error" else "low_confidence")
        stats.review = len(review)
        if review_path is not None and not dry_run:
            review.to_csv(review_path, index=True)
        return LabelResult(out, review, stats)

    def _frame(self, df: pd.DataFrame, row_keys: list[str], results: dict, status: dict) -> pd.DataFrame:
        name = self.task.name
        out = df.copy()
        prob_keys = list(self.task.choices) if self.task.choices else ["true"]
        out[name] = [results[k]["label"] if k in results else None for k in row_keys]
        out[f"{name}_confidence"] = [results[k]["confidence"] if k in results else float("nan") for k in row_keys]
        for pk in prob_keys:
            out[f"{name}_p_{pk}"] = [
                results[k]["probs"].get(pk, float("nan")) if k in results else float("nan") for k in row_keys
            ]
        out[f"{name}_status"] = [status.get(k, "error") for k in row_keys]
        return out
