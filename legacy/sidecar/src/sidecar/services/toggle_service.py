"""Toggle service — manage enable/disable state for config items."""

from __future__ import annotations

from pathlib import Path

from sidecar.models import GlobalManagerState, ManagerState
from sidecar.parsers import (
    parse_global_manager_state,
    parse_manager_state,
    write_global_manager_state,
    write_manager_state,
)


def get_manager_state(workspace_path: str) -> ManagerState:
    """Load the manager state for a client or project."""
    path = str(Path(workspace_path) / ".tentaqles.json")
    return parse_manager_state(path)


def save_manager_state(workspace_path: str, state: ManagerState) -> None:
    """Save the manager state for a client or project."""
    path = str(Path(workspace_path) / ".tentaqles.json")
    write_manager_state(path, state)


def get_global_state(base_path: str) -> GlobalManagerState:
    """Load the global manager state."""
    path = str(Path(base_path) / ".tentaqles.json")
    return parse_global_manager_state(path)


def save_global_state(base_path: str, state: GlobalManagerState) -> None:
    """Save the global manager state."""
    path = str(Path(base_path) / ".tentaqles.json")
    write_global_manager_state(path, state)


def toggle_rule(workspace_path: str, filename: str, enabled: bool) -> None:
    """Toggle a rule file on/off."""
    state = get_manager_state(workspace_path)
    state.toggles.rules[filename] = enabled
    save_manager_state(workspace_path, state)


def toggle_mcp(workspace_path: str, name: str, enabled: bool) -> None:
    """Toggle an MCP server on/off."""
    state = get_manager_state(workspace_path)
    state.toggles.mcps[name] = enabled
    save_manager_state(workspace_path, state)


def toggle_hook(workspace_path: str, name: str, enabled: bool) -> None:
    """Toggle a hook on/off."""
    state = get_manager_state(workspace_path)
    state.toggles.hooks[name] = enabled
    save_manager_state(workspace_path, state)


def toggle_skill(workspace_path: str, name: str, enabled: bool) -> None:
    """Toggle a skill on/off."""
    state = get_manager_state(workspace_path)
    state.toggles.skills[name] = enabled
    save_manager_state(workspace_path, state)


def is_enabled(workspace_path: str, config_type: str, name: str) -> bool:
    """Check if a config item is enabled (defaults to True if not explicitly set)."""
    state = get_manager_state(workspace_path)
    toggles_dict = getattr(state.toggles, config_type, {})
    return toggles_dict.get(name, True)
