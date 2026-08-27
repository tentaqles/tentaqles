"""Workspace registry — tracks which client workspaces participate in the meta-graph."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path

from tentaqles.config import data_dir

CONFIG_PATH = data_dir() / "metagraph" / "config.json"
META_GRAPH_DIR = data_dir() / "metagraph"


def load_config() -> dict:
    """Load or create the workspace config."""
    if CONFIG_PATH.exists():
        return json.loads(CONFIG_PATH.read_text(encoding="utf-8"))
    # Initialize with empty defaults (no hardcoded workspaces)
    config = {
        "schema": "tentaqles-metagraph-config-v1",
        "workspaces": {},
    }
    _save_config(config)
    return config


def _save_config(config: dict) -> None:
    CONFIG_PATH.parent.mkdir(parents=True, exist_ok=True)
    CONFIG_PATH.write_text(json.dumps(config, indent=2), encoding="utf-8")


def register_workspace(
    workspace_id: str,
    root_path: str,
    display_name: str | None = None,
    graph_paths: list[str] | None = None,
) -> dict:
    """Register a client workspace for meta-graph inclusion.

    Args:
        workspace_id: Short identifier (lowercase, no double underscores).
        root_path: Filesystem path to the client workspace root.
        display_name: Human-readable name. Defaults to workspace_id.
        graph_paths: Specific graph.json paths to include. If None, auto-discovers.
    """
    if "__" in workspace_id:
        raise ValueError("workspace_id cannot contain '__' (reserved as separator)")

    config = load_config()
    config["workspaces"][workspace_id] = {
        "root_path": str(root_path),
        "display_name": display_name or workspace_id,
        "graph_paths": graph_paths or [],
        "registered_at": datetime.now(timezone.utc).isoformat(),
        "last_merged": None,
    }
    _save_config(config)
    return config["workspaces"][workspace_id]


def unregister_workspace(workspace_id: str) -> bool:
    """Remove a workspace from the meta-graph config."""
    config = load_config()
    if workspace_id in config["workspaces"]:
        del config["workspaces"][workspace_id]
        _save_config(config)
        return True
    return False


def list_workspaces() -> dict[str, dict]:
    """Return all registered workspaces with their config."""
    config = load_config()
    return config.get("workspaces", {})


def discover_graphs(workspace_id: str) -> list[Path]:
    """Find all graphify-out/graph.json files in a workspace, max 3 levels deep."""
    config = load_config()
    ws = config["workspaces"].get(workspace_id)
    if not ws:
        return []

    root = Path(ws["root_path"])
    if not root.exists():
        return []

    graphs = []
    for depth in range(4):
        pattern = "/".join(["*"] * depth) + "/graphify-out/graph.json" if depth > 0 else "graphify-out/graph.json"
        graphs.extend(root.glob(pattern))

    return sorted(set(graphs))


def auto_register_defaults() -> int:
    """Register all known default workspaces that exist on disk.

    Note: No hardcoded workspace paths — this discovers from existing config only.
    Override by setting workspaces in the config file or calling register_workspace().
    """
    # No hardcoded defaults; return 0 as nothing to auto-register
    return 0
