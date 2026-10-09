"""Eval CLI for CI. Exits 0 on pass, 1 below thresholds, 2 on bad input.

    python -m rag_starter.evals --golden examples/golden.jsonl --k 5 --min-recall 0.8 --min-mrr 0.6

With Jev reranking or faithfulness, inject the key without printing it:

    tq dotenv run -- python -m rag_starter.evals --golden examples/golden.jsonl --reranker jev
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from ..app import build_retriever
from ..jev import JevClient
from .faithfulness import JevFaithfulness
from .golden import load_golden
from .runner import evaluate


def _answers(path: str) -> dict[str, str]:
    out = {}
    for line in Path(path).read_text(encoding="utf-8").splitlines():
        if line.strip():
            row = json.loads(line)
            out[str(row["id"])] = str(row["answer"])
    return out


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m rag_starter.evals", description=__doc__.splitlines()[0])
    p.add_argument("--golden", required=True, help="golden set JSONL")
    p.add_argument("--backend", default="memory", choices=["memory", "postgres"])
    p.add_argument("--corpus", help="folder of .md/.txt for the memory backend (default: examples/corpus)")
    p.add_argument("--embedder", default="hash", choices=["hash", "openai"])
    p.add_argument("--reranker", default="none", choices=["none", "jev", "cohere"])
    p.add_argument("--k", type=int, default=5)
    p.add_argument("--candidates", type=int, default=40)
    p.add_argument("--min-recall", type=float)
    p.add_argument("--min-mrr", type=float)
    p.add_argument("--answers", help="JSONL of {id, answer} produced by your app, for faithfulness")
    p.add_argument("--faithfulness", choices=["jev"], help="score answers with this check")
    p.add_argument("--min-faithfulness", type=float)
    p.add_argument("--json", dest="json_out", help="write the full report as JSON here")
    a = p.parse_args(argv)

    try:
        golden = load_golden(a.golden)
        retriever = build_retriever(
            a.backend, a.reranker, a.embedder, a.corpus, candidates=a.candidates, top_n=max(a.k, 1)
        )
        answers = _answers(a.answers) if a.answers else None
        check = JevFaithfulness(JevClient()) if a.faithfulness == "jev" else None
    except (OSError, ValueError, RuntimeError, KeyError) as e:
        print(f"error: {e}", file=sys.stderr)
        return 2

    report = evaluate(
        lambda q, f: retriever.retrieve(q, f or None).candidates,
        golden,
        k=a.k,
        answers=answers,
        faithfulness=check,
        min_recall=a.min_recall,
        min_mrr=a.min_mrr,
        min_faithfulness=a.min_faithfulness,
    )
    print(report.summary())
    if a.json_out:
        Path(a.json_out).write_text(json.dumps(report.to_dict(), indent=2), encoding="utf-8")
    return 0 if report.passed else 1


if __name__ == "__main__":
    sys.exit(main())
