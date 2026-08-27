"""Native backend — Tentaqles built-in graph engine.

Implements the same GraphEngine interface as graphify_backend but uses
the native modules in tentaqles.graph.native.* instead of the external
graphify package.

All modules fully ported from the patched graphify installation with
Tentaqles enhancements built in as first-class code.

Dependencies (declared in pyproject.toml under [graph] extra):
  - networkx            (graph data structure, clustering)
  - tree-sitter>=0.23   (AST parsing for code files)
  - tree-sitter-python, tree-sitter-javascript, tree-sitter-typescript,
    tree-sitter-go, tree-sitter-rust, tree-sitter-java, tree-sitter-c,
    tree-sitter-cpp, tree-sitter-ruby, tree-sitter-c-sharp,
    tree-sitter-kotlin, tree-sitter-scala, tree-sitter-php,
    tree-sitter-swift, tree-sitter-lua
  - pathspec            (.gitignore pattern matching)
  - pyyaml              (manifest loading)

Optional dependencies:
  - docling             (PPTX + rich PDF processing)
  - graspologic         (Leiden clustering, falls back to Louvain)
"""

from __future__ import annotations

from pathlib import Path

from tentaqles.graph.engine import GraphEngine


class NativeEngine(GraphEngine):
    """Native Tentaqles graph engine."""

    @property
    def name(self) -> str:
        return "native"

    @property
    def available(self) -> bool:
        """Check if core dependencies are installed."""
        try:
            import networkx
            import tree_sitter
            import pathspec
            return True
        except ImportError:
            return False

    def detect(self, root: Path, **kwargs) -> dict:
        from tentaqles.graph.native.detect import detect
        return detect(root, **kwargs)

    def build(self, root: Path, **kwargs) -> dict:
        from tentaqles.graph.native.pipeline import run_pipeline
        return run_pipeline(root, **kwargs)

    def query(self, question: str, graph_path: Path, **kwargs) -> str:
        """Query works with any graph.json — engine-agnostic."""
        import json
        import networkx as nx
        from networkx.readwrite import json_graph

        data = json.loads(graph_path.read_text())
        try:
            G = json_graph.node_link_graph(data, edges="links")
        except TypeError:
            G = json_graph.node_link_graph(data)

        mode = kwargs.get("mode", "bfs")
        depth = kwargs.get("depth", 3)
        budget = kwargs.get("token_budget", 2000)

        # Semantic search (uses Tentaqles embeddings, not graphify)
        start_nodes = []
        try:
            from tentaqles.embeddings.graphify_hook import semantic_search
            results = semantic_search(question, str(graph_path), top_k=3)
            start_nodes = [r["node_id"] for r in results if r["node_id"] in G]
        except Exception:
            pass

        # Keyword fallback
        if not start_nodes:
            terms = [t.lower() for t in question.split() if len(t) > 2]
            scored = []
            for nid, d in G.nodes(data=True):
                label = d.get("label", "").lower()
                score = sum(1 for t in terms if t in label)
                if score > 0:
                    scored.append((score, nid))
            scored.sort(reverse=True)
            start_nodes = [nid for _, nid in scored[:3]]

        if not start_nodes:
            return "No matching nodes found."

        # BFS traversal
        visited = set(start_nodes)
        frontier = set(start_nodes)
        edges_seen = []
        for _ in range(depth):
            nxt = set()
            for n in frontier:
                for nb in G.neighbors(n):
                    if nb not in visited:
                        nxt.add(nb)
                        edges_seen.append((n, nb))
            visited.update(nxt)
            frontier = nxt

        lines = [f"Query: {question}"]
        lines.append(f"Start: {[G.nodes[n].get('label', n) for n in start_nodes]} | {len(visited)} nodes\n")
        for nid in sorted(visited, key=lambda n: G.degree(n), reverse=True):
            d = G.nodes[nid]
            lines.append(f"  {d.get('label', nid)} [{d.get('source_file', '')}]")
        for u, v in edges_seen:
            if u in visited and v in visited:
                d = G.edges[u, v]
                lines.append(f"  {G.nodes[u].get('label', u)} --{d.get('relation', '')}--> {G.nodes[v].get('label', v)}")

        output = "\n".join(lines)
        if len(output) > budget * 4:
            output = output[: budget * 4] + "\n... (truncated)"
        return output

    def update(self, root: Path, **kwargs) -> dict:
        from tentaqles.graph.native.detect import detect_incremental
        result = detect_incremental(root)
        if result.get("new_total", 0) == 0:
            return {"status": "no_changes"}
        return self.build(root, **kwargs)
