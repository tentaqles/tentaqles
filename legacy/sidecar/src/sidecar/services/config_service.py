"""Config service — read/write rules, MCPs, CLAUDE.md, identities at any level."""

from __future__ import annotations

import json
from pathlib import Path

from sidecar.parsers import get_rule_description, read_file_content


def get_rules(workspace_path: str) -> dict[str, str]:
    """Get all .claude/rules/*.md files. Returns {filename: content}."""
    rules_dir = Path(workspace_path) / ".claude" / "rules"
    rules: dict[str, str] = {}
    if rules_dir.is_dir():
        for md_file in sorted(rules_dir.glob("*.md")):
            content = read_file_content(str(md_file))
            if content is not None:
                rules[md_file.name] = content
    return rules


def get_rules_with_meta(workspace_path: str) -> dict[str, dict[str, str | None]]:
    """Get all .claude/rules/*.md files with metadata.

    Returns {filename: {"content": str, "description": str | None}}.
    """
    rules_dir = Path(workspace_path) / ".claude" / "rules"
    rules: dict[str, dict[str, str | None]] = {}
    if rules_dir.is_dir():
        for md_file in sorted(rules_dir.glob("*.md")):
            content = read_file_content(str(md_file))
            if content is not None:
                rules[md_file.name] = {
                    "content": content,
                    "description": get_rule_description(content),
                }
    return rules


def get_claude_md(workspace_path: str) -> str | None:
    """Get CLAUDE.md content."""
    return read_file_content(str(Path(workspace_path) / "CLAUDE.md"))


def save_claude_md(workspace_path: str, content: str) -> None:
    """Save CLAUDE.md content."""
    path = Path(workspace_path) / "CLAUDE.md"
    path.write_text(content, encoding="utf-8")


def get_rule_content(workspace_path: str, filename: str) -> str | None:
    """Get a specific rule file's content."""
    if ".." in filename or "/" in filename or "\\" in filename:
        return None
    return read_file_content(str(Path(workspace_path) / ".claude" / "rules" / filename))


def save_rule(workspace_path: str, filename: str, content: str) -> None:
    """Save a rule file."""
    if ".." in filename or "/" in filename or "\\" in filename:
        raise ValueError(f"Invalid filename: {filename}")
    rules_dir = Path(workspace_path) / ".claude" / "rules"
    rules_dir.mkdir(parents=True, exist_ok=True)
    (rules_dir / filename).write_text(content, encoding="utf-8")


def delete_rule(workspace_path: str, filename: str) -> bool:
    """Delete a rule file. Returns True if deleted."""
    if ".." in filename or "/" in filename or "\\" in filename:
        return False
    path = Path(workspace_path) / ".claude" / "rules" / filename
    if path.exists():
        path.unlink()
        return True
    return False


def get_mcp_config(workspace_path: str) -> dict:
    """Get the full MCP config from .mcp.json."""
    path = Path(workspace_path) / ".mcp.json"
    content = read_file_content(str(path))
    if content:
        try:
            return json.loads(content)
        except json.JSONDecodeError:
            pass
    return {"mcpServers": {}}


def get_mcp_config_scoped(scope: str, workspace_path: str | None = None) -> dict:
    """Get MCP config for a specific scope.

    - global: ~/.claude/.mcp.json
    - project: {workspace_path}/.mcp.json
    """
    if scope == "global":
        from sidecar.services.scope_service import get_global_claude_dir

        path = get_global_claude_dir() / ".mcp.json"
        content = read_file_content(str(path))
        if content:
            try:
                return json.loads(content)
            except json.JSONDecodeError:
                pass
        return {"mcpServers": {}}
    if scope == "project":
        if not workspace_path:
            raise ValueError("workspace_path required for project scope")
        return get_mcp_config(workspace_path)
    raise ValueError(f"MCP only supports 'global' or 'project' scope, got: {scope}")


def save_mcp_config_scoped(scope: str, config: dict, workspace_path: str | None = None) -> None:
    """Save MCP config for a specific scope."""
    if scope == "global":
        from sidecar.services.scope_service import get_global_claude_dir

        path = get_global_claude_dir() / ".mcp.json"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(config, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    elif scope == "project":
        if not workspace_path:
            raise ValueError("workspace_path required for project scope")
        save_mcp_config(workspace_path, config)
    else:
        raise ValueError(f"MCP only supports 'global' or 'project' scope, got: {scope}")


def get_mcp_servers(workspace_path: str) -> dict[str, dict]:
    """Get MCP servers dict from config."""
    config = get_mcp_config(workspace_path)
    return config.get("mcpServers", {})


def save_mcp_config(workspace_path: str, config: dict, target: str = ".mcp.json") -> None:
    """Save MCP config to the specified file."""
    path = Path(workspace_path) / target
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(config, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def add_mcp_server(workspace_path: str, name: str, server_config: dict) -> None:
    """Add or update an MCP server in .mcp.json."""
    path = Path(workspace_path) / ".mcp.json"
    if path.exists():
        content = read_file_content(str(path))
        config = json.loads(content) if content else {"mcpServers": {}}
    else:
        config = {"mcpServers": {}}
    config.setdefault("mcpServers", {})[name] = server_config
    save_mcp_config(workspace_path, config)


def remove_mcp_server(workspace_path: str, name: str) -> None:
    """Remove an MCP server from .mcp.json."""
    path = Path(workspace_path) / ".mcp.json"
    if path.exists():
        content = read_file_content(str(path))
        if content:
            config = json.loads(content)
            config.get("mcpServers", {}).pop(name, None)
            save_mcp_config(workspace_path, config)
