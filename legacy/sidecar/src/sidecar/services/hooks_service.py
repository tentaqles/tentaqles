"""Hooks service — read/write hooks from settings.json at any scope."""

from __future__ import annotations

from sidecar.services.scope_service import get_settings_section, update_settings_section

HOOK_EVENT_TYPES = [
    "PreToolUse",
    "PostToolUse",
    "Stop",
    "SubagentStop",
    "SessionStart",
    "SessionEnd",
    "UserPromptSubmit",
    "PreCompact",
    "Notification",
]


def get_hooks(scope: str, workspace_path: str | None = None) -> dict:
    """Read hooks from settings.json at the specified scope."""
    hooks = get_settings_section(scope, workspace_path, "hooks")
    if not isinstance(hooks, dict):
        return {}
    return hooks


def save_hooks(scope: str, workspace_path: str | None, hooks: dict) -> None:
    """Write the entire hooks section to settings.json."""
    update_settings_section(scope, workspace_path, "hooks", hooks)


def add_hook_group(
    scope: str,
    workspace_path: str | None,
    event: str,
    matcher: str | None,
    hooks: list[dict],
) -> None:
    """Add a hook group to an event type."""
    if event not in HOOK_EVENT_TYPES:
        raise ValueError(f"Invalid hook event: {event}")

    current = get_hooks(scope, workspace_path)
    groups = current.get(event, [])

    group: dict = {"hooks": hooks}
    if matcher:
        group["matcher"] = matcher

    groups.append(group)
    current[event] = groups
    save_hooks(scope, workspace_path, current)


def remove_hook_group(
    scope: str,
    workspace_path: str | None,
    event: str,
    group_index: int,
) -> bool:
    """Remove a hook group by index. Returns True if removed."""
    current = get_hooks(scope, workspace_path)
    groups = current.get(event, [])

    if 0 <= group_index < len(groups):
        groups.pop(group_index)
        if groups:
            current[event] = groups
        else:
            current.pop(event, None)
        save_hooks(scope, workspace_path, current)
        return True
    return False


def update_hook_group(
    scope: str,
    workspace_path: str | None,
    event: str,
    group_index: int,
    matcher: str | None,
    hooks: list[dict],
) -> bool:
    """Update a specific hook group. Returns True if updated."""
    current = get_hooks(scope, workspace_path)
    groups = current.get(event, [])

    if 0 <= group_index < len(groups):
        group: dict = {"hooks": hooks}
        if matcher:
            group["matcher"] = matcher
        groups[group_index] = group
        current[event] = groups
        save_hooks(scope, workspace_path, current)
        return True
    return False
