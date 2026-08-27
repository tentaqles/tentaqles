"""Memory service — read/write CLAUDE.md at global, project, and local scopes."""

from __future__ import annotations

from pathlib import Path

from sidecar.parsers import read_file_content
from sidecar.services.scope_service import get_global_claude_dir


def _memory_path(scope: str, workspace_path: str | None) -> Path:
    """Resolve the CLAUDE.md path for a given scope."""
    if scope == "global":
        return get_global_claude_dir() / "CLAUDE.md"
    if scope == "project":
        if not workspace_path:
            raise ValueError("workspace_path required for project scope")
        return Path(workspace_path) / "CLAUDE.md"
    if scope == "local":
        if not workspace_path:
            raise ValueError("workspace_path required for local scope")
        return Path(workspace_path) / "CLAUDE.local.md"
    raise ValueError(f"Unknown scope: {scope}")


def get_memory(scope: str, workspace_path: str | None = None) -> dict:
    """Read CLAUDE.md from the specified scope."""
    path = _memory_path(scope, workspace_path)
    content = read_file_content(str(path))
    return {
        "scope": scope,
        "exists": content is not None,
        "file_path": str(path),
        "content": content or "",
    }


def save_memory(scope: str, workspace_path: str | None, content: str) -> None:
    """Write CLAUDE.md at the specified scope."""
    path = _memory_path(scope, workspace_path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def get_all_memory(workspace_path: str) -> dict:
    """Read all three CLAUDE.md scopes for a given workspace."""
    return {
        "global": get_memory("global"),
        "project": get_memory("project", workspace_path),
        "local": get_memory("local", workspace_path),
    }
