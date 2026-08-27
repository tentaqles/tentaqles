"""Graph engine abstraction — common interface for graphify and native backends.

The user selects their preferred engine via plugin config or .tentaqles.yaml.
Both backends produce the same output: graph.json, GRAPH_REPORT.md, graph.html.
"""

from __future__ import annotations

import os
from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any


class GraphEngine(ABC):
    """Base class for graph engine backends."""

    @property
    @abstractmethod
    def name(self) -> str:
        """Engine identifier: 'graphify' or 'native'."""

    @property
    @abstractmethod
    def available(self) -> bool:
        """Whether this engine's dependencies are installed."""

    @abstractmethod
    def detect(self, root: Path, **kwargs) -> dict:
        """Scan a directory for files. Returns detection result dict.

        Keys: files, total_files, total_words, needs_graph, warning,
              skipped_sensitive, graphifyignore_patterns
        """

    @abstractmethod
    def build(self, root: Path, **kwargs) -> dict:
        """Run the full pipeline: detect → extract → build → cluster → report.

        Returns summary dict with: nodes, edges, communities, output_dir.
        Output files are written to {root}/graphify-out/.
        """

    @abstractmethod
    def query(self, question: str, graph_path: Path, **kwargs) -> str:
        """Query the knowledge graph. Returns text answer."""

    @abstractmethod
    def update(self, root: Path, **kwargs) -> dict:
        """Incremental update — re-extract only changed files."""

    def embed(self, graph_path: Path) -> dict:
        """Embed all graph nodes for semantic search.

        This is engine-agnostic — uses Tentaqles embedding service
        regardless of which backend built the graph.
        """
        from tentaqles.embeddings.graphify_hook import embed_graph
        return embed_graph(graph_path)


def get_engine(preference: str | None = None) -> GraphEngine:
    """Get the graph engine based on user preference.

    Resolution order:
    1. Explicit preference argument
    2. TENTAQLES_GRAPH_ENGINE env var
    3. Plugin config (CLAUDE_PLUGIN_OPTION_graph_engine)
    4. Auto-detect: graphify if installed, native otherwise
    """
    choice = (
        preference
        or os.environ.get("TENTAQLES_GRAPH_ENGINE")
        or os.environ.get("CLAUDE_PLUGIN_OPTION_graph_engine")
    )

    if choice == "native":
        from tentaqles.graph.native_backend import NativeEngine
        engine = NativeEngine()
        if engine.available:
            return engine
        raise RuntimeError(
            "Native graph engine selected but dependencies are missing. "
            "Run: pip install tentaqles[graph]"
        )

    if choice == "graphify":
        from tentaqles.graph.graphify_backend import GraphifyEngine
        engine = GraphifyEngine()
        if engine.available:
            return engine
        raise RuntimeError(
            "Graphify engine selected but not installed. "
            "Run: pip install graphifyy"
        )

    # Auto-detect: try graphify first (more mature), fall back to native
    try:
        from tentaqles.graph.graphify_backend import GraphifyEngine
        engine = GraphifyEngine()
        if engine.available:
            return engine
    except Exception:
        pass

    try:
        from tentaqles.graph.native_backend import NativeEngine
        engine = NativeEngine()
        if engine.available:
            return engine
    except Exception:
        pass

    raise RuntimeError(
        "No graph engine available. Install one:\n"
        "  pip install graphifyy          # graphify backend\n"
        "  pip install tentaqles[graph]   # native backend"
    )
