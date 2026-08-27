"""Personality service — soul + user profiles with hierarchy resolution."""

from __future__ import annotations

from pathlib import Path

from sidecar.models import PersonalityFile, PersonalityPair
from sidecar.parsers import read_file_content


def _personality_path(file_type: str, level: str, workspace_path: str | None, client_path: str | None) -> Path:
    if level == "global":
        return Path.home() / ".tentaqles" / f"{file_type}.md"
    elif level == "client" and client_path:
        return Path(client_path) / ".claude" / f"{file_type}.md"
    elif level == "project" and workspace_path:
        return Path(workspace_path) / ".claude" / f"{file_type}.md"
    raise ValueError(f"Cannot resolve path for {file_type} at {level}")


def _read_personality_file(
    file_type: str, level: str, workspace_path: str | None, client_path: str | None
) -> PersonalityFile:
    path = _personality_path(file_type, level, workspace_path, client_path)
    content = read_file_content(str(path))
    return PersonalityFile(
        level=level,
        file_path=str(path),
        exists=content is not None,
        content=content or "",
    )


def get_personality(workspace_path: str | None = None, client_path: str | None = None) -> PersonalityPair:
    """Resolve the effective personality. Most specific level with content wins."""
    levels = ["project", "client", "global"]
    effective_soul = PersonalityFile(level="global", file_path="", exists=False)
    effective_user = PersonalityFile(level="global", file_path="", exists=False)
    effective_level = "global"

    for level in levels:
        if level == "project" and not workspace_path:
            continue
        if level == "client" and not client_path:
            continue

        soul = _read_personality_file("soul", level, workspace_path, client_path)
        user = _read_personality_file("user", level, workspace_path, client_path)

        if soul.exists and not effective_soul.exists:
            effective_soul = soul
            effective_level = level
        if user.exists and not effective_user.exists:
            effective_user = user
            if not effective_soul.exists:
                effective_level = level

    return PersonalityPair(soul=effective_soul, user=effective_user, effective_level=effective_level)


def save_personality(
    file_type: str,
    level: str,
    content: str,
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> None:
    """Write soul.md or user.md at the specified level."""
    path = _personality_path(file_type, level, workspace_path, client_path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def list_personality_overrides(
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> dict:
    """Return all personality files across levels with their existence and content."""
    result: dict[str, dict] = {"soul": {}, "user": {}}
    for file_type in ["soul", "user"]:
        for level in ["global", "client", "project"]:
            if level == "project" and not workspace_path:
                continue
            if level == "client" and not client_path:
                continue
            pf = _read_personality_file(file_type, level, workspace_path, client_path)
            result[file_type][level] = pf.model_dump()
    return result
