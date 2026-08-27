"""Agents service — CRUD for .claude/agents/*.md (project scope only)."""

from __future__ import annotations

from pathlib import Path

from sidecar.parsers import parse_yaml_frontmatter, read_file_content


def _agents_dir(workspace_path: str) -> Path:
    """Resolve the agents directory (project-only: .claude/agents/)."""
    if not workspace_path:
        raise ValueError("workspace_path required for agents")
    return Path(workspace_path) / ".claude" / "agents"


def list_agents(workspace_path: str) -> list[dict]:
    """List all .md agent files with parsed frontmatter."""
    agents_dir = _agents_dir(workspace_path)
    results: list[dict] = []

    if not agents_dir.is_dir():
        return results

    for md_file in sorted(agents_dir.glob("*.md")):
        content = read_file_content(str(md_file))
        if content is None:
            continue

        frontmatter, body = parse_yaml_frontmatter(content)
        name = md_file.stem

        # Parse tools — can be comma-separated string or list
        tools = frontmatter.get("tools")
        if isinstance(tools, str):
            tools = [t.strip() for t in tools.split(",")]

        results.append(
            {
                "filename": md_file.name,
                "name": frontmatter.get("name", name),
                "description": frontmatter.get("description"),
                "model": frontmatter.get("model"),
                "color": frontmatter.get("color"),
                "tools": tools,
                "body": body,
            }
        )

    return results


def get_agent(workspace_path: str, filename: str) -> dict | None:
    """Read a single agent file with parsed frontmatter."""
    if ".." in filename or "/" in filename or "\\" in filename:
        return None

    agents_dir = _agents_dir(workspace_path)
    path = agents_dir / filename
    content = read_file_content(str(path))
    if content is None:
        return None

    frontmatter, body = parse_yaml_frontmatter(content)
    name = path.stem

    tools = frontmatter.get("tools")
    if isinstance(tools, str):
        tools = [t.strip() for t in tools.split(",")]

    return {
        "filename": filename,
        "name": frontmatter.get("name", name),
        "description": frontmatter.get("description"),
        "model": frontmatter.get("model"),
        "color": frontmatter.get("color"),
        "tools": tools,
        "body": body,
        "raw_content": content,
    }


def save_agent(workspace_path: str, filename: str, content: str) -> None:
    """Write an agent .md file."""
    if ".." in filename or "/" in filename or "\\" in filename:
        raise ValueError(f"Invalid filename: {filename}")

    agents_dir = _agents_dir(workspace_path)
    agents_dir.mkdir(parents=True, exist_ok=True)
    (agents_dir / filename).write_text(content, encoding="utf-8")


def delete_agent(workspace_path: str, filename: str) -> bool:
    """Delete an agent file. Returns True if deleted."""
    if ".." in filename or "/" in filename or "\\" in filename:
        return False

    path = _agents_dir(workspace_path) / filename
    if path.exists():
        path.unlink()
        return True
    return False
