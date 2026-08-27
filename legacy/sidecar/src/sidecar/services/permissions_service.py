"""Permissions service — read/write permissions from settings.json at any scope."""

from __future__ import annotations

from sidecar.services.scope_service import (
    get_settings_section,
    read_settings_json,
    update_settings_section,
)

DEFAULT_PERMISSIONS = {
    "allow": [],
    "deny": [],
    "ask": [],
}


def get_permissions(scope: str, workspace_path: str | None = None) -> dict:
    """Read permissions from settings.json at the specified scope."""
    perms = get_settings_section(scope, workspace_path, "permissions")
    if not isinstance(perms, dict):
        return dict(DEFAULT_PERMISSIONS)
    # Ensure all expected keys exist
    for key in DEFAULT_PERMISSIONS:
        if key not in perms:
            perms[key] = list(DEFAULT_PERMISSIONS[key])
    return perms


def save_permissions(scope: str, workspace_path: str | None, permissions: dict) -> None:
    """Write the entire permissions section to settings.json."""
    update_settings_section(scope, workspace_path, "permissions", permissions)


def add_rule(scope: str, workspace_path: str | None, category: str, rule: str) -> None:
    """Add a rule to allow/deny/ask list."""
    if category not in ("allow", "deny", "ask"):
        raise ValueError(f"Invalid category: {category}")

    perms = get_permissions(scope, workspace_path)
    rules = perms.get(category, [])
    if rule not in rules:
        rules.append(rule)
        perms[category] = rules
        save_permissions(scope, workspace_path, perms)


def remove_rule(scope: str, workspace_path: str | None, category: str, rule: str) -> bool:
    """Remove a rule from allow/deny/ask list. Returns True if removed."""
    if category not in ("allow", "deny", "ask"):
        raise ValueError(f"Invalid category: {category}")

    perms = get_permissions(scope, workspace_path)
    rules = perms.get(category, [])
    if rule in rules:
        rules.remove(rule)
        perms[category] = rules
        save_permissions(scope, workspace_path, perms)
        return True
    return False


def get_merged_permissions(workspace_path: str) -> dict:
    """Merge global + project + local permissions for display.

    Later scopes override earlier scopes for scalar fields.
    Lists are merged (union).
    """
    global_perms = get_permissions("global")
    project_perms = get_permissions("project", workspace_path)
    local_perms = get_permissions("local", workspace_path)

    merged: dict = {}
    for key in ("allow", "deny", "ask"):
        # Union of all lists across scopes
        seen: set[str] = set()
        merged_list: list[str] = []
        for perms in [global_perms, project_perms, local_perms]:
            for rule in perms.get(key, []):
                if rule not in seen:
                    seen.add(rule)
                    merged_list.append(rule)
        merged[key] = merged_list

    # Scalar fields: local > project > global
    for key in ("disableBypassPermissionsMode", "additionalDirectories"):
        for perms in [local_perms, project_perms, global_perms]:
            if key in perms:
                merged[key] = perms[key]
                break

    return merged
