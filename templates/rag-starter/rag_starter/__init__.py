"""RAG starter for Supabase/pgvector: hybrid search + RRF, rerankers, record manager, evals."""

from .chunking import chunk_document, content_hash, split_text
from .embeddings import MAX_HNSW_HALFVEC_DIMS, Embedder, HashEmbedder, OpenAICompatibleEmbedder
from .ingest import IngestReport, Ingestor
from .jev import PINNED_MODEL, Decider, JevClient, JevError, untrusted_state
from .loop import LoopResult, Step, ToolCall, run_tool_loop
from .models import Candidate, Chunk, SourceDocument
from .record_manager import InMemoryRecordManager, RecordManager
from .rerank import CohereReranker, JevReranker, NoopReranker, Reranker
from .retrieval import RetrievalResult, Retriever
from .router import ModelRouter, Route
from .rrf import rrf_fuse
from .store import InMemoryStore, SearchBackend

__all__ = [
    "MAX_HNSW_HALFVEC_DIMS",
    "PINNED_MODEL",
    "Candidate",
    "Chunk",
    "CohereReranker",
    "Decider",
    "Embedder",
    "HashEmbedder",
    "InMemoryRecordManager",
    "InMemoryStore",
    "IngestReport",
    "Ingestor",
    "JevClient",
    "JevError",
    "JevReranker",
    "LoopResult",
    "ModelRouter",
    "NoopReranker",
    "OpenAICompatibleEmbedder",
    "RecordManager",
    "Reranker",
    "RetrievalResult",
    "Retriever",
    "Route",
    "SearchBackend",
    "SourceDocument",
    "Step",
    "ToolCall",
    "chunk_document",
    "content_hash",
    "rrf_fuse",
    "run_tool_loop",
    "split_text",
    "untrusted_state",
]
