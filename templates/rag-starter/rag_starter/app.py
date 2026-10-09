"""Wiring shared by the demo and the eval CLI. Copy and adapt for your app."""

from __future__ import annotations

from pathlib import Path

from .corpus import load_dir
from .embeddings import Embedder, HashEmbedder, OpenAICompatibleEmbedder
from .ingest import Ingestor
from .jev import JevClient
from .record_manager import InMemoryRecordManager
from .rerank import CohereReranker, JevReranker, NoopReranker, Reranker
from .retrieval import Retriever
from .store import InMemoryStore

EXAMPLE_CORPUS = Path(__file__).resolve().parent.parent / "examples" / "corpus"


def make_embedder(name: str) -> Embedder:
    if name == "hash":
        return HashEmbedder()
    if name == "openai":
        return OpenAICompatibleEmbedder()
    raise ValueError(f"unknown embedder {name!r} (hash|openai)")


def make_reranker(name: str) -> Reranker:
    if name == "none":
        return NoopReranker()
    if name == "jev":
        return JevReranker(JevClient())
    if name == "cohere":
        return CohereReranker()
    raise ValueError(f"unknown reranker {name!r} (none|jev|cohere)")


def build_retriever(
    backend: str = "memory",
    reranker: str = "none",
    embedder: str = "hash",
    corpus: str | Path | None = None,
    candidates: int = 40,
    top_n: int = 8,
    namespace: str = "default",
) -> Retriever:
    emb = make_embedder(embedder)
    if backend == "memory":
        store = InMemoryStore()
        Ingestor(store, emb, InMemoryRecordManager()).sync(load_dir(corpus or EXAMPLE_CORPUS), namespace)
    elif backend == "postgres":
        from .pg import PostgresStore, connect

        store = PostgresStore(connect(), namespace)  # type: ignore[assignment]
    else:
        raise ValueError(f"unknown backend {backend!r} (memory|postgres)")
    return Retriever(store, emb, make_reranker(reranker), candidates=candidates, top_n=top_n)
