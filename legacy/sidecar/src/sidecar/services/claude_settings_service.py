"""Claude Settings service — settings.json sections (excluding hooks/permissions)."""

from __future__ import annotations

from sidecar.services.scope_service import (
    get_settings_section,
    read_settings_json,
    update_settings_section,
)

# Keys managed by other modules — excluded from "general settings" view
_MANAGED_KEYS = {"hooks", "permissions"}


def get_claude_settings(scope: str, workspace_path: str | None = None) -> dict:
    """Return all settings.json content for a scope, excluding hooks/permissions."""
    full = read_settings_json(scope, workspace_path)
    return {k: v for k, v in full.items() if k not in _MANAGED_KEYS}


def save_claude_settings(scope: str, workspace_path: str | None, settings: dict) -> None:
    """Write settings back, preserving hooks/permissions sections."""
    existing = read_settings_json(scope, workspace_path)
    # Keep managed sections intact
    for key in _MANAGED_KEYS:
        if key in existing:
            settings[key] = existing[key]
    from sidecar.services.scope_service import write_settings_json

    write_settings_json(scope, settings, workspace_path)


def get_setting_value(scope: str, workspace_path: str | None, key: str) -> object:
    """Get a single top-level setting value."""
    settings = read_settings_json(scope, workspace_path)
    return settings.get(key)


def set_setting_value(scope: str, workspace_path: str | None, key: str, value: object) -> None:
    """Set a single top-level setting value (read-modify-write safe)."""
    if key in _MANAGED_KEYS:
        raise ValueError(f"Cannot set managed key '{key}' via settings — use its own endpoint")
    update_settings_section(scope, workspace_path, key, value)
