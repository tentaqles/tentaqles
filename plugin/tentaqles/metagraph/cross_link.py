"""Cross-workspace similarity — find semantic connections between isolated client graphs."""

from __future__ import annotations

import json
import numpy as np
from pathlib import Path

from .merge import META_GRAPH_PATH
from tentaqles.embeddings.service import EmbeddingService


def add_cross_workspace_edges(
    similarity_threshold: float = 0.75,
    max_edges_per_pair: int = 10,
) -> dict:
    """Find semantically similar nodes across different workspaces and add edges.

    Uses the Tentaqles embedding service to embed all meta-graph node labels,
    then finds high-similarity pairs that span workspace boundaries.

    Args:
        similarity_threshold: Minimum cosine similarity to create an edge (0.0-1.0).
            Higher = fewer, more confident edges. Default 0.75.
        max_edges_per_pair: Maximum cross-workspace edges between any two workspaces.
            Prevents one workspace pair from dominating. Default 10.

    Returns:
        Summary dict with stats.
    """
    if not META_GRAPH_PATH.exists():
        raise FileNotFoundError("Meta-graph not found. Run build_meta_graph() first.")

    meta = json.loads(META_GRAPH_PATH.read_text(encoding="utf-8"))
    nodes = meta.get("nodes", [])

    if len(nodes) < 2:
        return {"cross_edges_added": 0, "reason": "Not enough nodes"}

    # Get unique workspaces
    workspaces = {n["workspace"] for n in nodes}
    if len(workspaces) < 2:
        return {"cross_edges_added": 0, "reason": "Only one workspace — nothing to cross-link"}

    service = EmbeddingService()

    # Build texts for all nodes
    node_texts = []
    for n in nodes:
        label = n.get("label", n["id"])
        file_type = n.get("file_type", "")
        text = f"{label} [{file_type}]" if file_type else label
        node_texts.append(text)

    # Embed all nodes
    embeddings = service.embed(node_texts)

    # Normalize for cosine similarity
    norms = np.linalg.norm(embeddings, axis=1, keepdims=True) + 1e-10
    normed = embeddings / norms

    # Compute full similarity matrix
    sim_matrix = normed @ normed.T

    # Find cross-workspace pairs above threshold
    cross_edges = []
    pair_counts: dict[tuple[str, str], int] = {}

    # Get indices sorted by similarity (descending) — only upper triangle
    n_nodes = len(nodes)
    for i in range(n_nodes):
        ws_i = nodes[i]["workspace"]
        for j in range(i + 1, n_nodes):
            ws_j = nodes[j]["workspace"]

            # Only cross-workspace
            if ws_i == ws_j:
                continue

            score = float(sim_matrix[i, j])
            if score < similarity_threshold:
                continue

            # Check pair limit
            pair_key = tuple(sorted([ws_i, ws_j]))
            count = pair_counts.get(pair_key, 0)
            if count >= max_edges_per_pair:
                continue
            pair_counts[pair_key] = count + 1

            cross_edges.append({
                "source": nodes[i]["id"],
                "target": nodes[j]["id"],
                "relation": "cross_workspace_similar",
                "confidence": "INFERRED",
                "confidence_score": round(score, 3),
                "weight": round(score, 3),
                "source_workspace": ws_i,
                "target_workspace": ws_j,
                "similarity_method": "embedding_cosine",
                "similarity_score": round(score, 3),
            })

    # Sort by score descending
    cross_edges.sort(key=lambda e: e["confidence_score"], reverse=True)

    # Save back to meta-graph
    meta["cross_workspace_links"] = cross_edges
    META_GRAPH_PATH.write_text(json.dumps(meta, indent=2), encoding="utf-8")

    # Stats per workspace pair
    pair_stats = {}
    for edge in cross_edges:
        pair = f"{edge['source_workspace']} <-> {edge['target_workspace']}"
        pair_stats[pair] = pair_stats.get(pair, 0) + 1

    return {
        "cross_edges_added": len(cross_edges),
        "threshold": similarity_threshold,
        "workspace_pairs": pair_stats,
        "top_connections": [
            {
                "source": e["source"],
                "target": e["target"],
                "score": e["confidence_score"],
            }
            for e in cross_edges[:5]
        ],
    }


def query_cross_workspace(
    question: str,
    top_k: int = 5,
) -> list[dict]:
    """Search across all workspaces in the meta-graph semantically.

    Returns nodes from ANY workspace ranked by relevance to the question.
    Each result includes the workspace it belongs to.
    """
    if not META_GRAPH_PATH.exists():
        raise FileNotFoundError("Meta-graph not found. Run build_meta_graph() first.")

    meta = json.loads(META_GRAPH_PATH.read_text(encoding="utf-8"))
    nodes = meta.get("nodes", [])

    if not nodes:
        return []

    service = EmbeddingService()

    # Build node texts
    node_texts = []
    for n in nodes:
        label = n.get("label", n["id"])
        file_type = n.get("file_type", "")
        text = f"{label} [{file_type}]" if file_type else label
        node_texts.append(text)

    # Search
    results = service.search(question, node_texts, top_k=top_k)

    return [
        {
            "node_id": nodes[idx]["id"],
            "original_id": nodes[idx].get("original_id", ""),
            "workspace": nodes[idx]["workspace"],
            "label": nodes[idx].get("label", ""),
            "file_type": nodes[idx].get("file_type", ""),
            "community": nodes[idx].get("community"),
            "score": round(score, 3),
        }
        for idx, score in results
    ]
