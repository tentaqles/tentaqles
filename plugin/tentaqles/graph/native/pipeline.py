"""Full graph build pipeline — detect -> extract -> build -> cluster -> analyze -> report -> export."""

from __future__ import annotations
import json
from pathlib import Path


def run_pipeline(root: Path, **kwargs) -> dict:
    """Run the complete graph build pipeline.

    Returns dict with: nodes, edges, communities, output_dir, god_nodes.
    """
    from tentaqles.graph.native.detect import detect
    from tentaqles.graph.native.extract import collect_files, extract
    from tentaqles.graph.native.build import build_from_json
    from tentaqles.graph.native.cluster import cluster, score_all
    from tentaqles.graph.native.analyze import god_nodes, surprising_connections
    from tentaqles.graph.native.report import generate
    from tentaqles.graph.native.export import to_json, to_html

    # Step 1: Detect files
    detection = detect(root)
    if detection["total_files"] == 0:
        return {"nodes": 0, "edges": 0, "communities": 0, "error": "No files found"}

    # Step 2: AST extraction
    code_files = collect_files(root)
    extraction = extract(code_files) if code_files else {"nodes": [], "edges": []}

    # Step 3: Build graph
    G = build_from_json(extraction)
    if G.number_of_nodes() == 0:
        return {"nodes": 0, "edges": 0, "communities": 0, "error": "No nodes extracted"}

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
    tokens = {"input": extraction.get("input_tokens", 0), "output": extraction.get("output_tokens", 0)}

    report_text = generate(G, communities, cohesion, labels, gods, surprises, detection, tokens, str(root))
    (out_dir / "GRAPH_REPORT.md").write_text(report_text)

    to_json(G, communities, str(out_dir / "graph.json"))

    if G.number_of_nodes() <= 5000:
        try:
            to_html(G, communities, str(out_dir / "graph.html"), community_labels=labels)
        except Exception:
            pass  # HTML generation is optional

    return {
        "nodes": G.number_of_nodes(),
        "edges": G.number_of_edges(),
        "communities": len(communities),
        "output_dir": str(out_dir),
        "god_nodes": gods[:5] if gods else [],
    }
