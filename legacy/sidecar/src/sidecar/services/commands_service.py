"""Commands service — CRUD for .claude/commands/*.md at global and project scopes."""

from __future__ import annotations

from pathlib import Path

from sidecar.parsers import parse_yaml_frontmatter, read_file_content
from sidecar.services.scope_service import get_global_claude_dir


def _commands_dir(scope: str, workspace_path: str | None) -> Path:
    """Resolve the commands directory for a given scope."""
    if scope == "global":
        return get_global_claude_dir() / "commands"
    if scope == "project":
        if not workspace_path:
            raise ValueError("workspace_path required for project scope")
        return Path(workspace_path) / ".claude" / "commands"
    raise ValueError(f"Commands only support 'global' or 'project' scope, got: {scope}")


def list_commands(scope: str, workspace_path: str | None = None) -> list[dict]:
    """List all .md command files with parsed frontmatter."""
    commands_dir = _commands_dir(scope, workspace_path)
    results: list[dict] = []

    if not commands_dir.is_dir():
        return results

    for md_file in sorted(commands_dir.glob("*.md")):
        content = read_file_content(str(md_file))
        if content is None:
            continue

        frontmatter, body = parse_yaml_frontmatter(content)
        name = md_file.stem  # filename without extension

        # Parse allowed-tools — can be comma-separated string or list
        allowed_tools = frontmatter.get("allowed-tools")
        if isinstance(allowed_tools, str):
            allowed_tools = [t.strip() for t in allowed_tools.split(",")]

        results.append(
            {
                "filename": md_file.name,
                "name": frontmatter.get("name", name),
                "description": frontmatter.get("description"),
                "model": frontmatter.get("model"),
                "allowed_tools": allowed_tools,
                "argument_hint": frontmatter.get("argument-hint"),
                "body": body,
            }
        )

    return results


def get_command(scope: str, workspace_path: str | None, filename: str) -> dict | None:
    """Read a single command file with parsed frontmatter."""
    if ".." in filename or "/" in filename or "\\" in filename:
        return None

    commands_dir = _commands_dir(scope, workspace_path)
    path = commands_dir / filename
    content = read_file_content(str(path))
    if content is None:
        return None

    frontmatter, body = parse_yaml_frontmatter(content)
    name = path.stem

    allowed_tools = frontmatter.get("allowed-tools")
    if isinstance(allowed_tools, str):
        allowed_tools = [t.strip() for t in allowed_tools.split(",")]

    return {
        "filename": filename,
        "name": frontmatter.get("name", name),
        "description": frontmatter.get("description"),
        "model": frontmatter.get("model"),
        "allowed_tools": allowed_tools,
        "argument_hint": frontmatter.get("argument-hint"),
        "body": body,
        "raw_content": content,
    }


def save_command(scope: str, workspace_path: str | None, filename: str, content: str) -> None:
    """Write a command .md file."""
    if ".." in filename or "/" in filename or "\\" in filename:
        raise ValueError(f"Invalid filename: {filename}")

    commands_dir = _commands_dir(scope, workspace_path)
    commands_dir.mkdir(parents=True, exist_ok=True)
    (commands_dir / filename).write_text(content, encoding="utf-8")


def delete_command(scope: str, workspace_path: str | None, filename: str) -> bool:
    """Delete a command file. Returns True if deleted."""
    if ".." in filename or "/" in filename or "\\" in filename:
        return False

    path = _commands_dir(scope, workspace_path) / filename
    if path.exists():
        path.unlink()
        return True
    return False
