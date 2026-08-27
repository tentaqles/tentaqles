"""Graphify integration — embed graph nodes post-build, semantic query at search time."""

from __future__ import annotations

import json
import numpy as np
from pathlib import Path
from typing import Sequence

from tentaqles.embeddings.service import EmbeddingService

# Singleton — initialized on first use, shared across calls in same process
_service: EmbeddingService | None = None


def _get_service() -> EmbeddingService:
    global _service
    if _service is None:
        _service = EmbeddingService()
    return _service


def embed_graph(graph_json_path: str | Path) -> dict:
    """Embed all node labels in a graphify graph.json and save alongside it.

    Reads graph.json, embeds each node's label (+ source_file for context),
    saves embeddings as graph_embeddings.npz in the same directory.

    Returns stats about what was embedded.
    """
    graph_json_path = Path(graph_json_path)
    if not graph_json_path.exists():
        raise FileNotFoundError(f"Graph not found: {graph_json_path}")

    data = json.loads(graph_json_path.read_text(encoding="utf-8"))

    # Extract node info — build a rich text for each node
    nodes = data.get("nodes", [])
    if not nodes:
        return {"nodes_embedded": 0, "cached": 0}

    node_ids = []
    node_texts = []
    for node in nodes:
        nid = node.get("id", "")
        label = node.get("label", nid)
        source = node.get("source_file", "")
        file_type = node.get("file_type", "")
        # Build a text that captures the node's identity
        text = label
        if source:
            text += f" ({source})"
        if file_type:
            text += f" [{file_type}]"
        node_ids.append(nid)
        node_texts.append(text)

    service = _get_service()
    embeddings = service.embed(node_texts)

    # Save alongside graph.json
    out_path = graph_json_path.parent / "graph_embeddings.npz"
    np.savez_compressed(
        out_path,
        embeddings=embeddings,
        node_ids=np.array(node_ids, dtype=object),
        node_texts=np.array(node_texts, dtype=object),
    )

    cache_stats = service._cache.stats()

    return {
        "nodes_embedded": len(node_ids),
        "embedding_dim": embeddings.shape[1],
        "file": str(out_path),
        "file_size_kb": round(out_path.stat().st_size / 1024, 1),
        "cache_total": cache_stats["cached_embeddings"],
    }


def load_embeddings(graph_dir: str | Path) -> tuple[list[str], np.ndarray] | None:
    """Load pre-computed embeddings from a graphify-out directory.

    Returns (node_ids, embeddings_matrix) or None if not found.
    """
    emb_path = Path(graph_dir) / "graph_embeddings.npz"
    if not emb_path.exists():
        return None
    data = np.load(emb_path, allow_pickle=True)
    return list(data["node_ids"]), data["embeddings"]


def semantic_search(
    question: str,
    graph_json_path: str | Path,
    top_k: int = 5,
) -> list[dict]:
    """Find the most semantically relevant nodes for a question.

    Uses pre-computed embeddings if available, falls back to on-the-fly embedding.

    Returns list of dicts: [{"node_id": str, "label": str, "score": float, "source_file": str}, ...]
    """
    graph_json_path = Path(graph_json_path)
    graph_dir = graph_json_path.parent

    # Load graph for node metadata
    data = json.loads(graph_json_path.read_text(encoding="utf-8"))
    nodes = data.get("nodes", [])
    if not nodes:
        return []

    node_map = {n["id"]: n for n in nodes}

    # Try loading pre-computed embeddings
    precomputed = load_embeddings(graph_dir)

    service = _get_service()

    if precomputed is not None:
        node_ids, embeddings = precomputed
        # Search against pre-computed embeddings
        # Build texts for query context (not needed — we search against stored embeddings)
        query_emb = service.embed([question])[0]

        # Cosine similarity
        query_norm = query_emb / (np.linalg.norm(query_emb) + 1e-10)
        emb_norms = embeddings / (
            np.linalg.norm(embeddings, axis=1, keepdims=True) + 1e-10
        )
        similarities = emb_norms @ query_norm

        k = min(top_k, len(similarities))
        top_indices = np.argpartition(similarities, -k)[-k:]
        top_indices = top_indices[np.argsort(similarities[top_indices])[::-1]]

        results = []
        for idx in top_indices:
            nid = node_ids[idx]
            node = node_map.get(nid, {})
            results.append({
                "node_id": nid,
                "label": node.get("label", nid),
                "score": round(float(similarities[idx]), 3),
                "source_file": node.get("source_file", ""),
            })
        return results

    # Fallback: embed on the fly
    node_ids = [n["id"] for n in nodes]
    node_texts = []
    for n in nodes:
        label = n.get("label", n["id"])
        source = n.get("source_file", "")
        text = f"{label} ({source})" if source else label
        node_texts.append(text)

    results_raw = service.search(question, node_texts, top_k=top_k)
    results = []
    for idx, score in results_raw:
        nid = node_ids[idx]
        node = node_map.get(nid, {})
        results.append({
            "node_id": nid,
            "label": node.get("label", nid),
            "score": round(score, 3),
            "source_file": node.get("source_file", ""),
        })
    return results
