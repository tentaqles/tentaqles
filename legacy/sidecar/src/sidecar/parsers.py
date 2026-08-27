"""File parsers for workspace configuration files."""

from __future__ import annotations

import configparser
import json
import re
from pathlib import Path

from sidecar.models import (
    ClaudeProfile,
    DevelopmentRules,
    GitIdentity,
    GlobalManagerState,
    LearnedPattern,
    ManagerState,
    WorkspaceProfile,
)


def read_file_content(path: str) -> str | None:
    """Read a file's text content. Returns None if file not found."""
    try:
        return Path(path).read_text(encoding="utf-8")
    except (FileNotFoundError, OSError):
        return None


def parse_gitconfig(path: str, platform: str, host: str, account: str | None) -> GitIdentity | None:
    """Parse a .gitconfig-{client} INI file to extract git identity."""
    content = read_file_content(path)
    if content is None:
        return None

    config = configparser.ConfigParser()
    config.read_string(content)

    name = config.get("user", "name", fallback="unknown")
    email = config.get("user", "email", fallback="unknown")
    hooks_path = config.get("core", "hookspath", fallback=None)

    return GitIdentity(
        name=name,
        email=email,
        platform=platform,
        host=host,
        account=account,
        hooks_path=hooks_path,
    )


def parse_learned_patterns(path: str) -> list[LearnedPattern]:
    """Parse learned-patterns.md into structured entries.

    Splits on '---' separators, extracts fields from each pattern section.
    """
    content = read_file_content(path)
    if content is None:
        return []

    sections = re.split(r"\n---\n", content)
    patterns: list[LearnedPattern] = []

    for section in sections:
        match = re.search(r"## Pattern (\d+):\s*(.+)", section)
        if not match:
            continue

        number = int(match.group(1))
        title = match.group(2).strip()

        category = _extract_field(section, "Category")
        date_discovered = _extract_field(section, "Date Discovered")
        description = _extract_field(section, "Description")
        prevention = _extract_field(section, "Prevention")

        wrong_example = _extract_code_block_after(section, "Wrong Example")
        right_example = _extract_code_block_after(section, "Right Example")

        applied_to = _extract_bullet_list(section, "Applied To")

        patterns.append(
            LearnedPattern(
                number=number,
                title=title,
                category=category,
                date_discovered=date_discovered,
                description=description,
                wrong_example=wrong_example,
                right_example=right_example,
                prevention=prevention,
                applied_to=applied_to,
            )
        )

    return patterns


def _extract_field(text: str, field_name: str) -> str:
    """Extract a **FieldName**: value line."""
    pattern = rf"\*\*{re.escape(field_name)}\*\*:\s*(.+)"
    match = re.search(pattern, text)
    return match.group(1).strip() if match else ""


def _extract_code_block_after(text: str, header: str) -> str:
    """Extract all fenced code blocks after a **Header** marker until next **Header** or end."""
    # Find position of the header
    header_pattern = rf"\*\*{re.escape(header)}\*\*"
    header_match = re.search(header_pattern, text)
    if not header_match:
        return ""

    # Find the next bold header after this one
    remaining = text[header_match.end() :]
    next_header = re.search(r"\n\*\*(?:Right Example|Prevention|Applied To)\*\*", remaining)
    if next_header:
        remaining = remaining[: next_header.start()]

    # Extract all fenced code blocks in this region
    blocks = re.findall(r"```[\w]*\n(.*?)```", remaining, re.DOTALL)
    return "\n\n".join(block.strip() for block in blocks)


def _extract_bullet_list(text: str, header: str) -> list[str]:
    """Extract a bullet list after a **Header** marker."""
    header_pattern = rf"\*\*{re.escape(header)}\*\*:"
    header_match = re.search(header_pattern, text)
    if not header_match:
        return []

    remaining = text[header_match.end() :]
    items: list[str] = []
    for line in remaining.split("\n"):
        line = line.strip()
        if line.startswith("- "):
            items.append(line[2:].strip())
        elif items and not line:
            continue
        elif items:
            break
    return items


def parse_yaml_frontmatter(content: str) -> tuple[dict[str, str | list[str]], str]:
    """Parse YAML frontmatter from any markdown file.

    Returns (frontmatter_dict, body). Handles simple key: value pairs and
    list items (- value). Does NOT depend on PyYAML.
    """
    fm_match = re.match(r"^---\s*\n(.*?)\n---\s*\n?(.*)", content, re.DOTALL)
    if not fm_match:
        return {}, content.strip()

    frontmatter_text = fm_match.group(1)
    body = fm_match.group(2).strip()
    result: dict[str, str | list[str]] = {}

    current_key: str | None = None
    current_list: list[str] = []

    for line in frontmatter_text.split("\n"):
        stripped = line.strip()

        # List item under current key
        if stripped.startswith("- ") and current_key:
            val = stripped[2:].strip().strip("\"'")
            current_list.append(val)
            continue

        # Flush previous list
        if current_key and current_list:
            result[current_key] = current_list
            current_list = []
            current_key = None

        # Key-value pair
        kv_match = re.match(r"^([\w-]+):\s*(.*)$", stripped)
        if kv_match:
            key = kv_match.group(1)
            value = kv_match.group(2).strip().strip("\"'")
            if value:
                result[key] = value
            else:
                # Empty value — might be followed by list items
                current_key = key
                current_list = []

    # Flush trailing list
    if current_key and current_list:
        result[current_key] = current_list

    return result, body


def parse_rule_frontmatter(content: str) -> dict[str, str | list[str] | None]:
    """Parse YAML frontmatter from a rule .md file.

    Extracts simple key-value pairs from the ``---`` delimited block.
    Returns a dict with 'description', 'paths', and 'body' keys.
    """
    result: dict[str, str | list[str] | None] = {
        "description": None,
        "paths": [],
        "body": content,
    }

    fm_match = re.match(r"^---\s*\n(.*?)\n---\s*\n?(.*)", content, re.DOTALL)
    if not fm_match:
        return result

    frontmatter_text = fm_match.group(1)
    result["body"] = fm_match.group(2).strip()

    desc_match = re.search(r"^description:\s*(.+)$", frontmatter_text, re.MULTILINE)
    if desc_match:
        result["description"] = desc_match.group(1).strip().strip("\"'")

    result["paths"] = re.findall(r'^\s*-\s*"?([^"\n]+)"?\s*$', frontmatter_text, re.MULTILINE)
    return result


def get_rule_description(content: str) -> str | None:
    """Extract a human-readable description for a rule file.

    Priority: 1) ``description`` field from frontmatter, 2) first ``# Heading``.
    """
    parsed = parse_rule_frontmatter(content)

    if parsed["description"]:
        return parsed["description"]

    body = parsed["body"]
    heading_match = re.search(r"^#\s+(.+)$", body, re.MULTILINE)
    if heading_match:
        return heading_match.group(1).strip()

    return None


# Built-in descriptions for known MCP servers.
_MCP_DESCRIPTIONS: dict[str, str] = {
    # By server name
    "workspace-context": "Multi-client workspace context and identity management",
    "microsoft-learn": "Microsoft Learn documentation and API reference",
    "snowflake": "Snowflake data warehouse queries and management",
    "analytics-mcp": "Google Analytics data access and reporting",
    "asana": "Asana project and task management",
    "atlassian": "Atlassian (Jira / Confluence) project management",
    "google-drive": "Google Drive and Sheets file access",
    "slack": "Slack messaging and channel management",
    "duckduckgo-search": "Web search via DuckDuckGo",
    "fetch": "HTTP fetch for web content retrieval",
    "memory": "Persistent memory and knowledge graph storage",
    "github": "GitHub repos, PRs, and issues",
    "GitLab": "GitLab repos, MRs, and pipelines",
    "postgres": "PostgreSQL database queries",
    "azure": "Azure cloud resource management",
    "azure-devops": "Azure DevOps boards, repos, and pipelines",
    "databricks": "Databricks SQL warehouse queries",
    "trello": "Trello board and card management",
    "n8n-mcp": "n8n workflow automation",
    "Tool Manager MCP": "Claude Code tool permission manager",
    "tauri-mcp": "Tauri desktop app integration",
    # By npm / PyPI package name (fallback match via args)
    "@modelcontextprotocol/server-slack": "Slack messaging and channel management",
    "@modelcontextprotocol/server-memory": "Persistent memory and knowledge graph storage",
    "@modelcontextprotocol/server-postgres": "PostgreSQL database queries",
    "@modelcontextprotocol/server-github": "GitHub repos, PRs, and issues",
    "@tokenizin/mcp-npx-fetch": "HTTP fetch for web content retrieval",
    "@azure/mcp": "Azure cloud resource management",
    "duckduckgo-mcp-server": "Web search via DuckDuckGo",
    "snowflake-labs-mcp": "Snowflake data warehouse queries and management",
    "woocommerce-mcp-server": "WooCommerce store management",
    "mcp-server-trello": "Trello board and card management",
}


def get_mcp_description(name: str, config: dict) -> str | None:
    """Get a human-readable description for an MCP server.

    Resolution: 1) server name match, 2) package name from args, 3) URL hostname.
    """
    if name in _MCP_DESCRIPTIONS:
        return _MCP_DESCRIPTIONS[name]

    # Match package name from args
    args = config.get("args", [])
    for arg in args:
        if isinstance(arg, str) and arg in _MCP_DESCRIPTIONS:
            return _MCP_DESCRIPTIONS[arg]

    # For HTTP/SSE servers or mcp-remote proxied URLs
    url = config.get("url", "")
    if not url:
        for arg in args:
            if isinstance(arg, str) and arg.startswith("http"):
                url = arg
                break

    if url:
        from urllib.parse import urlparse

        hostname = urlparse(url).hostname
        if hostname:
            return f"Remote MCP at {hostname}"

    return None


def parse_mcp_json(path: str) -> list[str]:
    """Parse a .mcp.json file to extract MCP server names."""
    content = read_file_content(path)
    if content is None:
        return []

    try:
        data = json.loads(content)
        servers = data.get("mcpServers", {})
        return list(servers.keys())
    except (json.JSONDecodeError, AttributeError):
        return []


def parse_development_rules(claude_md_content: str) -> DevelopmentRules | None:
    """Extract development rules from CLAUDE.md content.

    Looks for '## Development Rules' section and parses
    Blocked/Confirm/Allowed command lists. Handles both
    single-paragraph and multi-line formats.
    """
    # Find the Development Rules section
    match = re.search(r"## Development Rules\s*\n(.*?)(?=\n## |\Z)", claude_md_content, re.DOTALL)
    if not match:
        return None

    section = match.group(1)

    blocked = _extract_rule_list(section, r"\*\*Blocked[^*]*\*\*:?\s*")
    confirm = _extract_rule_list(section, r"\*\*Confirm[^*]*\*\*:?\s*")
    allowed = _extract_rule_list(section, r"\*\*Allowed[^*]*\*\*:?\s*")

    if not blocked and not confirm and not allowed:
        return None

    return DevelopmentRules(
        blocked_commands=blocked,
        confirm_commands=confirm,
        allowed_commands=allowed,
    )


def _extract_rule_list(text: str, header_pattern: str) -> list[str]:
    """Extract a list of commands after a bold header in the dev rules section.

    Handles inline format: **Blocked:** `cmd1`, `cmd2`, `cmd3`.
    """
    match = re.search(header_pattern, text)
    if not match:
        return []

    # Get text after the header until next bold header or end
    remaining = text[match.end() :]
    next_bold = re.search(r"\*\*\w", remaining)
    if next_bold:
        remaining = remaining[: next_bold.start()]

    # Extract backtick-quoted items
    items = re.findall(r"`([^`]+)`", remaining)
    return items


def parse_workspace_profile(path: str) -> WorkspaceProfile | None:
    """Parse a .workspace-profile.json file into a WorkspaceProfile model."""
    content = read_file_content(path)
    if content is None:
        return None

    try:
        data = json.loads(content)
        return WorkspaceProfile.model_validate(data)
    except (json.JSONDecodeError, Exception):
        return None


def write_workspace_profile(path: str, profile: WorkspaceProfile) -> None:
    """Write a WorkspaceProfile to a .workspace-profile.json file."""
    data = profile.model_dump(by_alias=True)
    Path(path).write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def parse_manager_state(path: str) -> ManagerState:
    """Parse a .tentaqles.json file. Returns default state if file not found."""
    content = read_file_content(path)
    if content is None:
        return ManagerState()

    try:
        data = json.loads(content)
        return ManagerState.model_validate(data)
    except (json.JSONDecodeError, Exception):
        return ManagerState()


def write_manager_state(path: str, state: ManagerState) -> None:
    """Write a ManagerState to a .tentaqles.json file."""
    data = state.model_dump(by_alias=True)
    Path(path).write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def parse_global_manager_state(path: str) -> GlobalManagerState:
    """Parse a global .tentaqles.json file. Returns default state if file not found."""
    content = read_file_content(path)
    if content is None:
        return GlobalManagerState()

    try:
        data = json.loads(content)
        return GlobalManagerState.model_validate(data)
    except (json.JSONDecodeError, Exception):
        return GlobalManagerState()


def write_global_manager_state(path: str, state: GlobalManagerState) -> None:
    """Write a GlobalManagerState to the global .tentaqles.json file."""
    data = state.model_dump(by_alias=True)
    Path(path).write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def parse_claude_profile(path: str) -> ClaudeProfile | None:
    """Parse a .claude-profile.json file into a ClaudeProfile model."""
    content = read_file_content(path)
    if content is None:
        return None
    try:
        data = json.loads(content)
        return ClaudeProfile.model_validate(data)
    except (json.JSONDecodeError, Exception):
        return None


def write_claude_profile(path: str, profile: ClaudeProfile) -> None:
    """Write a ClaudeProfile to a .claude-profile.json file."""
    data = profile.model_dump(by_alias=True)
    Path(path).write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
