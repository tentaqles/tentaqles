"""Filesystem discovery for workspace configuration.

Clients are discovered dynamically by scanning REPOS_ROOT for directories
that contain a .workspace-profile.json file. No hardcoded client list.
"""

from __future__ import annotations

from pathlib import Path

from sidecar.mcp_server.config import REPOS_ROOT
from sidecar.models import (
    CloudIdentity,
    DatabaseConfig,
    GitIdentity,
    WorkspaceConfig,
    WorkspaceSummary,
)
from sidecar.parsers import (
    parse_development_rules,
    parse_gitconfig,
    parse_mcp_json,
    parse_workspace_profile,
    read_file_content,
)


def resolve_client_name(workspace_path: str) -> str:
    """Extract client name from workspace path.

    Handles paths like 'd:/repos/booster', 'd:/repos/booster/', 'd:\\repos\\booster'.
    """
    normalized = Path(workspace_path).resolve()
    return normalized.name.lower()


def _load_profile_dict(client: str, ws_path: str) -> dict | None:
    """Load client profile from .workspace-profile.json. Returns None if not found."""
    profile_path = f"{ws_path}/.workspace-profile.json"
    file_profile = parse_workspace_profile(profile_path)
    if file_profile is None:
        return None

    return {
        "client_description": file_profile.client_description,
        "git": {
            "platform": file_profile.git.platform.value,
            "host": file_profile.git.host,
            "account": file_profile.git.account,
        },
        "cloud": {
            "provider": file_profile.cloud.provider.value,
            "subscription_name": file_profile.cloud.subscription_name,
            "subscription_id": file_profile.cloud.subscription_id,
        },
        "database": {
            "type": file_profile.database.type.value,
            "dialect": file_profile.database.dialect,
            "connection_details": file_profile.database.connection_details,
        },
        "language": file_profile.language.value,
        "tech_stack": file_profile.tech_stack,
        "special_rules": file_profile.special_rules,
    }


def resolve_workspace(workspace_path: str) -> WorkspaceConfig:
    """Build a complete WorkspaceConfig from filesystem discovery.

    Loads client profile from .workspace-profile.json in the workspace directory.
    """
    client = resolve_client_name(workspace_path)
    ws_path = str(Path(workspace_path).resolve())

    profile = _load_profile_dict(client, ws_path)

    if profile is None:
        # Also try the canonical path under REPOS_ROOT
        canonical_path = str(Path(REPOS_ROOT) / client)
        profile = _load_profile_dict(client, canonical_path)
        if profile is not None:
            ws_path = canonical_path

    if profile is None:
        known = [c.name for c in Path(REPOS_ROOT).iterdir() if c.is_dir() and (c / ".workspace-profile.json").exists()]
        raise ValueError(
            f"Unknown client '{client}'. No .workspace-profile.json found. "
            f"Known clients (via .workspace-profile.json): {', '.join(sorted(known))}"
        )

    # Parse git identity from .gitconfig-{client}
    git_profile = profile["git"]
    gitconfig_path = f"{ws_path}/.gitconfig-{client}"
    git_identity = parse_gitconfig(
        gitconfig_path,
        platform=git_profile["platform"],
        host=git_profile["host"],
        account=git_profile.get("account"),
    )
    if git_identity is None:
        git_identity = GitIdentity(
            name="unknown",
            email="unknown",
            platform=git_profile["platform"],
            host=git_profile["host"],
            account=git_profile.get("account"),
        )

    # Cloud identity from profile
    cloud_profile = profile["cloud"]
    cloud_identity = CloudIdentity(
        provider=cloud_profile["provider"],
        subscription_name=cloud_profile.get("subscription_name"),
        subscription_id=cloud_profile.get("subscription_id"),
    )

    # Database config from profile
    db_profile = profile["database"]
    database_config = DatabaseConfig(
        type=db_profile["type"],
        dialect=db_profile.get("dialect"),
        connection_details=db_profile.get("connection_details"),
    )

    # Discover SQL dialect rules
    sql_rules = discover_sql_dialect_rules(ws_path)

    # Discover identity rules
    identity_rules = read_file_content(f"{ws_path}/.claude/rules/identity.md")

    # Discover all workspace rules
    workspace_rules = discover_workspace_rules(ws_path)

    # Read CLAUDE.md
    claude_md = read_file_content(f"{ws_path}/CLAUDE.md")

    # Parse development rules from CLAUDE.md
    dev_rules = parse_development_rules(claude_md) if claude_md else None

    # Discover active MCP servers
    mcp_servers = parse_mcp_json(f"{ws_path}/.mcp.json")

    return WorkspaceConfig(
        client_name=client,
        client_description=profile["client_description"],
        workspace_path=ws_path,
        git=git_identity,
        cloud=cloud_identity,
        database=database_config,
        language=profile.get("language", "en"),
        tech_stack=profile.get("tech_stack", []),
        special_rules=profile.get("special_rules", []),
        active_mcp_servers=mcp_servers,
        sql_dialect_rules=sql_rules,
        identity_rules=identity_rules,
        workspace_rules=workspace_rules,
        claude_md_content=claude_md,
        development_rules=dev_rules,
    )


def discover_sql_dialect_rules(workspace_path: str) -> str | None:
    """Look for known SQL dialect rule files in .claude/rules/."""
    dialect_files = ["snowflake.md", "postgres.md", "databricks.md"]
    rules_dir = Path(workspace_path) / ".claude" / "rules"

    for filename in dialect_files:
        content = read_file_content(str(rules_dir / filename))
        if content is not None:
            return content

    return None


def discover_workspace_rules(workspace_path: str) -> dict[str, str]:
    """Read all .claude/rules/*.md files. Returns {filename: content}."""
    rules_dir = Path(workspace_path) / ".claude" / "rules"
    rules: dict[str, str] = {}

    if not rules_dir.is_dir():
        return rules

    for md_file in sorted(rules_dir.glob("*.md")):
        content = read_file_content(str(md_file))
        if content is not None:
            rules[md_file.name] = content

    return rules


def _get_known_clients() -> list[str]:
    """Discover clients by scanning REPOS_ROOT for directories with .workspace-profile.json."""
    repos_root = Path(REPOS_ROOT)
    if not repos_root.is_dir():
        return []

    return sorted(
        child.name.lower()
        for child in repos_root.iterdir()
        if child.is_dir() and (child / ".workspace-profile.json").exists()
    )


def list_all_workspaces() -> list[WorkspaceSummary]:
    """List all configured client workspaces with summary info.

    Discovers clients dynamically from directories containing .workspace-profile.json.
    """
    summaries: list[WorkspaceSummary] = []

    for client in _get_known_clients():
        ws_path = str(Path(REPOS_ROOT) / client)
        profile = _load_profile_dict(client, ws_path)

        if profile is None:
            continue

        # Try to get actual git email from .gitconfig
        git_profile = profile["git"]
        git_identity = parse_gitconfig(
            f"{ws_path}/.gitconfig-{client}",
            platform=git_profile["platform"],
            host=git_profile["host"],
            account=git_profile.get("account"),
        )
        git_email = git_identity.email if git_identity else "unknown"

        summaries.append(
            WorkspaceSummary(
                client_name=client,
                workspace_path=ws_path,
                git_email=git_email,
                git_platform=git_profile["platform"],
                cloud_provider=profile["cloud"]["provider"],
                database_type=profile["database"]["type"],
                language=profile.get("language", "en"),
                special_rules=profile.get("special_rules", []),
            )
        )

    return summaries
