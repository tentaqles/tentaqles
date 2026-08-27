"""Scope service — resolve paths and read/write settings.json across scopes."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from sidecar.parsers import read_file_content


def get_global_claude_dir() -> Path:
    """Return ~/.claude/ path (cross-platform)."""
    return Path.home() / ".claude"


def resolve_path(scope: str, workspace_path: str | None, relative: str) -> Path:
    """Resolve a path based on scope.

    - global  → ~/.claude/{relative}
    - project → {workspace_path}/.claude/{relative}
    - local   → {workspace_path}/.claude/{relative} (caller handles .local suffix)
    """
    if scope == "global":
        return get_global_claude_dir() / relative
    if scope in ("project", "local"):
        if not workspace_path:
            raise ValueError(f"workspace_path required for scope '{scope}'")
        return Path(workspace_path) / ".claude" / relative
    raise ValueError(f"Unknown scope: {scope}")


def _settings_file(scope: str, workspace_path: str | None) -> Path:
    """Get the settings.json path for a given scope."""
    if scope == "global":
        return get_global_claude_dir() / "settings.json"
    if scope == "project":
        if not workspace_path:
            raise ValueError("workspace_path required for project scope")
        return Path(workspace_path) / ".claude" / "settings.json"
    if scope == "local":
        if not workspace_path:
            raise ValueError("workspace_path required for local scope")
        return Path(workspace_path) / ".claude" / "settings.local.json"
    raise ValueError(f"Unknown scope: {scope}")


def read_settings_json(scope: str, workspace_path: str | None = None) -> dict:
    """Read settings.json from the specified scope. Returns {} if not found."""
    path = _settings_file(scope, workspace_path)
    content = read_file_content(str(path))
    if content is None:
        return {}
    try:
        return json.loads(content)
    except json.JSONDecodeError:
        return {}


def write_settings_json(scope: str, data: dict, workspace_path: str | None = None) -> None:
    """Write settings.json to the specified scope."""
    path = _settings_file(scope, workspace_path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def update_settings_section(
    scope: str,
    workspace_path: str | None,
    section_key: str,
    section_data: Any,
) -> None:
    """Read-modify-write: update only one section of settings.json.

    This is critical for safely sharing settings.json between hooks,
    permissions, and other settings modules.
    """
    settings = read_settings_json(scope, workspace_path)
    settings[section_key] = section_data
    write_settings_json(scope, settings, workspace_path)


def get_settings_section(
    scope: str,
    workspace_path: str | None,
    section_key: str,
) -> Any:
    """Read a single section from settings.json."""
    settings = read_settings_json(scope, workspace_path)
    return settings.get(section_key, None)
