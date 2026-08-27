"""Merge per-workspace graphs into a single meta-graph with client isolation."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path

from .config import load_config, discover_graphs, META_GRAPH_DIR

# Fields safe to include in meta-graph nodes
_NODE_ALLOW = {"id", "label", "file_type", "community", "source_url", "captured_at", "author", "contributor"}
# Fields safe to include in meta-graph edges
_EDGE_ALLOW = {"source", "target", "relation", "confidence", "confidence_score", "weight", "_src", "_tgt"}
# Fields safe to include in meta-graph hyperedges
_HYPER_ALLOW = {"id", "label", "nodes", "relation", "confidence", "confidence_score"}

META_GRAPH_PATH = META_GRAPH_DIR / "meta-graph.json"


def _sanitize_node(node: dict, workspace_id: str, community_offset: int) -> dict:
    """Transform a graphify node into a meta-graph node with isolation."""
    original_id = node.get("id", "")
    meta_node = {
        "id": f"{workspace_id}__{original_id}",
        "original_id": original_id,
        "workspace": workspace_id,
    }
    for key in _NODE_ALLOW:
        if key == "id":
            continue  # already handled
        if key == "community" and key in node:
            meta_node["community"] = node["community"] + community_offset
        elif key in node and node[key] is not None:
            meta_node[key] = node[key]
    return meta_node


def _sanitize_edge(edge: dict, workspace_id: str) -> dict:
    """Transform a graphify edge into a meta-graph edge with isolation."""
    meta_edge = {"workspace": workspace_id}
    for key in _EDGE_ALLOW:
        if key in edge:
            val = edge[key]
            if key in ("source", "target", "_src", "_tgt"):
                val = f"{workspace_id}__{val}"
            meta_edge[key] = val
    # Backfill confidence_score if missing
    if "confidence_score" not in meta_edge and "confidence" in meta_edge:
        defaults = {"EXTRACTED": 1.0, "INFERRED": 0.5, "AMBIGUOUS": 0.2}
        meta_edge["confidence_score"] = defaults.get(meta_edge["confidence"], 0.5)
    return meta_edge


def _sanitize_hyperedge(hyper: dict, workspace_id: str) -> dict:
    """Transform a graphify hyperedge into a meta-graph hyperedge."""
    original_id = hyper.get("id", "")
    meta_hyper = {
        "id": f"{workspace_id}__{original_id}",
        "original_id": original_id,
        "workspace": workspace_id,
    }
    for key in _HYPER_ALLOW:
        if key == "id":
            continue
        if key == "nodes" and key in hyper:
            meta_hyper["nodes"] = [f"{workspace_id}__{nid}" for nid in hyper["nodes"]]
        elif key in hyper and hyper[key] is not None:
            meta_hyper[key] = hyper[key]
    return meta_hyper


def _merge_single_graph(
    graph_path: Path,
    workspace_id: str,
    community_offset: int,
) -> tuple[list[dict], list[dict], list[dict], int]:
    """Merge a single graph.json into meta-graph format.

    Returns (nodes, edges, hyperedges, community_count).
    """
    data = json.loads(graph_path.read_text(encoding="utf-8"))

    # Nodes
    raw_nodes = data.get("nodes", [])
    nodes = [_sanitize_node(n, workspace_id, community_offset) for n in raw_nodes]

    # Edges (in 'links' array per graphify convention)
    raw_edges = data.get("links", [])
    edges = [_sanitize_edge(e, workspace_id) for e in raw_edges]

    # Hyperedges
    raw_hypers = data.get("hyperedges", [])
    hyperedges = [_sanitize_hyperedge(h, workspace_id) for h in raw_hypers]

    # Count communities for offset
    community_ids = {n.get("community") for n in raw_nodes if "community" in n}
    community_count = max(community_ids) + 1 if community_ids else 0

    return nodes, edges, hyperedges, community_count


def build_meta_graph(workspace_ids: list[str] | None = None) -> dict:
    """Build the full meta-graph from all (or specified) registered workspaces.

    Args:
        workspace_ids: Specific workspaces to include. If None, uses all registered.

    Returns:
        Summary dict with stats.
    """
    config = load_config()
    workspaces = config.get("workspaces", {})

    if workspace_ids:
        workspaces = {k: v for k, v in workspaces.items() if k in workspace_ids}

    all_nodes: list[dict] = []
    all_edges: list[dict] = []
    all_hyperedges: list[dict] = []
    workspace_metas: list[dict] = []
    community_offset = 0

    for ws_id, ws_config in sorted(workspaces.items()):
        # Find graphs — use configured paths first, then auto-discover
        graph_paths = [Path(p) for p in ws_config.get("graph_paths", []) if Path(p).exists()]
        if not graph_paths:
            graph_paths = discover_graphs(ws_id)

        if not graph_paths:
            continue

        ws_nodes: list[dict] = []
        ws_edges: list[dict] = []
        ws_hyperedges: list[dict] = []
        ws_community_count = 0

        for gp in graph_paths:
            nodes, edges, hyperedges, cc = _merge_single_graph(gp, ws_id, community_offset + ws_community_count)
            ws_nodes.extend(nodes)
            ws_edges.extend(edges)
            ws_hyperedges.extend(hyperedges)
            ws_community_count += cc

        # Deduplicate nodes by meta-ID (same node might appear in multiple project graphs)
        seen_ids = set()
        deduped_nodes = []
        for n in ws_nodes:
            if n["id"] not in seen_ids:
                seen_ids.add(n["id"])
                deduped_nodes.append(n)

        all_nodes.extend(deduped_nodes)
        all_edges.extend(ws_edges)
        all_hyperedges.extend(ws_hyperedges)

        workspace_metas.append({
            "id": ws_id,
            "display_name": ws_config.get("display_name", ws_id),
            "node_count": len(deduped_nodes),
            "edge_count": len(ws_edges),
            "community_offset": community_offset,
            "community_count": ws_community_count,
            "graph_sources": [str(gp) for gp in graph_paths],
            "ingested_at": datetime.now(timezone.utc).isoformat(),
        })

        community_offset += ws_community_count

        # Update last_merged in config
        config["workspaces"][ws_id]["last_merged"] = datetime.now(timezone.utc).isoformat()

    # Assemble meta-graph
    meta_graph = {
        "schema": "tentaqles-metagraph-v1",
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "workspaces": workspace_metas,
        "directed": False,
        "multigraph": False,
        "graph": {
            "total_nodes": len(all_nodes),
            "total_edges": len(all_edges),
            "total_workspaces": len(workspace_metas),
        },
        "nodes": all_nodes,
        "links": all_edges,
        "hyperedges": all_hyperedges,
        "cross_workspace_links": [],  # populated by cross_link.py
    }

    # Save
    META_GRAPH_PATH.parent.mkdir(parents=True, exist_ok=True)
    META_GRAPH_PATH.write_text(json.dumps(meta_graph, indent=2), encoding="utf-8")

    # Save config with updated timestamps
    from .config import _save_config
    _save_config(config)

    return {
        "meta_graph_path": str(META_GRAPH_PATH),
        "workspaces_merged": len(workspace_metas),
        "total_nodes": len(all_nodes),
        "total_edges": len(all_edges),
        "total_hyperedges": len(all_hyperedges),
        "workspaces": {m["id"]: {"nodes": m["node_count"], "edges": m["edge_count"]} for m in workspace_metas},
    }


def update_workspace(workspace_id: str) -> dict:
    """Incrementally update a single workspace in the meta-graph.

    Re-merges only the specified workspace's graphs, replacing its nodes/edges
    in the existing meta-graph. Other workspaces are untouched.
    """
    if not META_GRAPH_PATH.exists():
        return build_meta_graph([workspace_id])

    meta = json.loads(META_GRAPH_PATH.read_text(encoding="utf-8"))

    # Remove old data for this workspace
    meta["nodes"] = [n for n in meta["nodes"] if n.get("workspace") != workspace_id]
    meta["links"] = [e for e in meta["links"] if e.get("workspace") != workspace_id]
    meta["hyperedges"] = [h for h in meta["hyperedges"] if h.get("workspace") != workspace_id]
    meta["cross_workspace_links"] = [
        e for e in meta.get("cross_workspace_links", [])
        if e.get("source_workspace") != workspace_id and e.get("target_workspace") != workspace_id
    ]

    # Find this workspace's community offset (reuse existing or append)
    existing_ws = {w["id"]: w for w in meta.get("workspaces", [])}
    if workspace_id in existing_ws:
        community_offset = existing_ws[workspace_id]["community_offset"]
    else:
        # New workspace — offset after all existing
        community_offset = sum(w.get("community_count", 0) for w in meta.get("workspaces", []))

    # Merge fresh data
    config = load_config()
    ws_config = config["workspaces"].get(workspace_id, {})
    graph_paths = [Path(p) for p in ws_config.get("graph_paths", []) if Path(p).exists()]
    if not graph_paths:
        graph_paths = discover_graphs(workspace_id)

    ws_nodes = []
    ws_edges = []
    ws_hyperedges = []
    ws_community_count = 0

    for gp in graph_paths:
        nodes, edges, hyperedges, cc = _merge_single_graph(gp, workspace_id, community_offset + ws_community_count)
        ws_nodes.extend(nodes)
        ws_edges.extend(edges)
        ws_hyperedges.extend(hyperedges)
        ws_community_count += cc

    # Dedupe
    seen = set()
    deduped = []
    for n in ws_nodes:
        if n["id"] not in seen:
            seen.add(n["id"])
            deduped.append(n)

    meta["nodes"].extend(deduped)
    meta["links"].extend(ws_edges)
    meta["hyperedges"].extend(ws_hyperedges)

    # Update workspace meta
    new_ws_meta = {
        "id": workspace_id,
        "display_name": ws_config.get("display_name", workspace_id),
        "node_count": len(deduped),
        "edge_count": len(ws_edges),
        "community_offset": community_offset,
        "community_count": ws_community_count,
        "graph_sources": [str(gp) for gp in graph_paths],
        "ingested_at": datetime.now(timezone.utc).isoformat(),
    }
    meta["workspaces"] = [w for w in meta["workspaces"] if w["id"] != workspace_id]
    meta["workspaces"].append(new_ws_meta)
    meta["workspaces"].sort(key=lambda w: w["id"])

    # Update totals
    meta["graph"]["total_nodes"] = len(meta["nodes"])
    meta["graph"]["total_edges"] = len(meta["links"])
    meta["graph"]["total_workspaces"] = len(meta["workspaces"])
    meta["generated_at"] = datetime.now(timezone.utc).isoformat()

    META_GRAPH_PATH.write_text(json.dumps(meta, indent=2), encoding="utf-8")

    return {
        "meta_graph_path": str(META_GRAPH_PATH),
        "workspace_updated": workspace_id,
        "nodes_added": len(deduped),
        "edges_added": len(ws_edges),
        "total_nodes": len(meta["nodes"]),
        "total_edges": len(meta["links"]),
    }
