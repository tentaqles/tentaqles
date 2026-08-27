"""Tentaqles embedding service — shared semantic search across all workspaces."""

from .service import EmbeddingService
from .graphify_hook import embed_graph, semantic_search

__all__ = ["EmbeddingService", "embed_graph", "semantic_search"]
