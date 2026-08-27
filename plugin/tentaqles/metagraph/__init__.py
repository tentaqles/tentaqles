"""Tentaqles meta-graph — cross-workspace knowledge graph with client isolation."""

from .merge import build_meta_graph, update_workspace
from .cross_link import add_cross_workspace_edges
from .config import load_config, register_workspace, list_workspaces

__all__ = [
    "build_meta_graph",
    "update_workspace",
    "add_cross_workspace_edges",
    "load_config",
    "register_workspace",
    "list_workspaces",
]
