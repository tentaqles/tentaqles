"""Graphify backend — wraps the external graphify package.

This backend uses `pip install graphifyy` and applies Tentaqles enhancements
at runtime (ignore support, semantic search, memory integration).
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from tentaqles.graph.engine import GraphEngine


class GraphifyEngine(GraphEngine):
    """Graph engine backed by the graphify package."""

    @property
    def name(self) -> str:
        return "graphify"

    @property
    def available(self) -> bool:
        try:
            import graphify
            return True
        except ImportError:
            return False

    def detect(self, root: Path, **kwargs) -> dict:
        from graphify.detect import detect
        return detect(root, **kwargs)

    def build(self, root: Path, **kwargs) -> dict:
        """Run graphify's full pipeline.

        This delegates to graphify's modules directly rather than the
        skill/CLI — giving us programmatic control over each step.
        """
        import json
        from graphify.detect import detect
        from graphify.build import build_from_json
        from graphify.cluster import cluster, score_all
        from graphify.analyze import god_nodes, surprising_connections
        from graphify.report import generate
        from graphify.export import to_json, to_html

        # Step 1: Detect
        detection = detect(root)
        if detection["total_files"] == 0:
            return {"nodes": 0, "edges": 0, "communities": 0, "error": "No files found"}

        # Step 2: AST extraction for code files
        from graphify.extract import collect_files, extract as ast_extract
        code_files = collect_files(root)
        ast_result = ast_extract(code_files) if code_files else {"nodes": [], "edges": []}

        # Step 3: Build graph from extraction
        G = build_from_json(ast_result)

        # Step 4: Cluster
        communities = cluster(G)
        cohesion = score_all(G, communities)

        # Step 5: Analyze
        gods = god_nodes(G)
        surprises = surprising_connections(G, communities)

        # Step 6: Output
        out_dir = root / "graphify-out"
        out_dir.mkdir(exist_ok=True)

        labels = {cid: f"Community {cid}" for cid in communities}
        tokens = {"input": ast_result.get("input_tokens", 0), "output": ast_result.get("output_tokens", 0)}

        report = generate(G, communities, cohesion, labels, gods, surprises, detection, tokens, str(root))
        (out_dir / "GRAPH_REPORT.md").write_text(report)

        to_json(G, communities, str(out_dir / "graph.json"))

        if G.number_of_nodes() <= 5000:
            to_html(G, communities, str(out_dir / "graph.html"), community_labels=labels)

        return {
            "nodes": G.number_of_nodes(),
            "edges": G.number_of_edges(),
            "communities": len(communities),
            "output_dir": str(out_dir),
            "god_nodes": gods[:5],
        }

    def query(self, question: str, graph_path: Path, **kwargs) -> str:
        """Query using graphify's serve module with semantic search."""
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

        # Try semantic search first
        start_nodes = []
        try:
            from tentaqles.embeddings.graphify_hook import semantic_search
            results = semantic_search(question, str(graph_path), top_k=3)
            start_nodes = [r["node_id"] for r in results if r["node_id"] in G]
        except Exception:
            pass

        # Fallback to keyword
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

        # BFS/DFS traversal
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

        lines = [f"Traversal: {mode.upper()} | Start: {[G.nodes[n].get('label', n) for n in start_nodes]} | {len(visited)} nodes"]
        for nid in sorted(visited, key=lambda n: G.degree(n), reverse=True):
            d = G.nodes[nid]
            lines.append(f"  NODE {d.get('label', nid)} [src={d.get('source_file', '')}]")
        for u, v in edges_seen:
            if u in visited and v in visited:
                d = G.edges[u, v]
                lines.append(f"  EDGE {G.nodes[u].get('label', u)} --{d.get('relation', '')}--> {G.nodes[v].get('label', v)}")

        output = "\n".join(lines)
        char_budget = budget * 4
        if len(output) > char_budget:
            output = output[:char_budget] + "\n... (truncated)"
        return output

    def update(self, root: Path, **kwargs) -> dict:
        from graphify.detect import detect_incremental
        result = detect_incremental(root)
        if result.get("new_total", 0) == 0:
            return {"status": "no_changes", "new_files": 0}
        # Re-run build on changed files
        return self.build(root, **kwargs)
