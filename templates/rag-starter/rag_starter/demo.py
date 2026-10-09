"""Offline demo: ingest examples/corpus into memory and answer a query with traces.

    python -m rag_starter.demo "how long do refunds take"
    python -m rag_starter.demo --filter document_type=article "refund window"   # shows filter fallback
    tq dotenv run -- python -m rag_starter.demo --reranker jev --route "compare the plans"
"""

from __future__ import annotations

import argparse
import json
import sys

from .app import build_retriever
from .jev import JevClient
from .router import ModelRouter


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m rag_starter.demo")
    p.add_argument("query")
    p.add_argument("--reranker", default="none", choices=["none", "jev", "cohere"])
    p.add_argument("--filter", action="append", default=[], help="key=value metadata filter")
    p.add_argument("--top", type=int, default=4)
    p.add_argument("--route", action="store_true", help="also ask Jev which model tier to use")
    a = p.parse_args(argv)

    filters = dict(f.split("=", 1) for f in a.filter) or None
    retriever = build_retriever(reranker=a.reranker, top_n=a.top)
    res = retriever.retrieve(a.query, filters)
    if res.filter_fallback:
        print(f"(filters {res.filters_requested} matched nothing; retried without filters)")
    for c in res.candidates:
        scores = ", ".join(f"{k} {v:.3f}" for k, v in c.scores().items() if v is not None)
        print(f"- {c.chunk_id}  [{scores}]\n    {c.content[:160]}")
    if a.route:
        route = ModelRouter(JevClient(), cheap_model="cheap-model", strong_model="strong-model").route(a.query)
        print("route:", json.dumps(route.__dict__))
    return 0


if __name__ == "__main__":
    sys.exit(main())
