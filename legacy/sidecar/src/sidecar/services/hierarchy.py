"""Hierarchy scanning service — discovers clients and projects from the filesystem."""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path

from sidecar.parsers import (
    parse_gitconfig,
    parse_mcp_json,
    parse_workspace_profile,
)

# Directories to skip when scanning for projects
SKIP_DIRS = {
    ".scripts",
    ".templates",
    "#prompts",
    ".git",
    ".venv",
    "venv",
    "node_modules",
    "__pycache__",
    ".claude",
    ".github",
    ".githooks",
    ".agent",
    ".kiro",
    ".vscode",
    ".idea",
}


@dataclass
class ProjectNode:
    """A project (sub-repo) within a client folder."""

    name: str
    path: str
    has_git: bool = False
    has_claude_md: bool = False
    has_claude_rules: bool = False
    has_mcp_json: bool = False
    rule_files: list[str] = field(default_factory=list)
    mcp_servers: list[str] = field(default_factory=list)


@dataclass
class ClientNode:
    """A client/entity folder under the base directory."""

    name: str
    path: str
    description: str = ""
    git_email: str = ""
    git_platform: str = ""
    cloud_provider: str = ""
    database_type: str = ""
    language: str = "en"
    has_profile: bool = False
    projects: list[ProjectNode] = field(default_factory=list)
    rule_files: list[str] = field(default_factory=list)
    mcp_servers: list[str] = field(default_factory=list)


@dataclass
class HierarchyTree:
    """The full workspace hierarchy: base → clients → projects."""

    base_path: str
    clients: list[ClientNode] = field(default_factory=list)


def scan_hierarchy(base_path: str) -> HierarchyTree:
    """Scan the base directory and build the complete hierarchy tree."""
    base = Path(base_path)
    tree = HierarchyTree(base_path=str(base))

    if not base.is_dir():
        return tree

    for child in sorted(base.iterdir()):
        if not child.is_dir():
            continue
        if child.name.startswith(".") or child.name.startswith("_"):
            continue

        client = _scan_client(child)
        if client is not None:
            tree.clients.append(client)

    return tree


def _scan_client(client_dir: Path) -> ClientNode | None:
    """Scan a client directory for profile, git config, rules, MCPs, and projects."""
    name = client_dir.name.lower()

    # Must have either .workspace-profile.json or .gitconfig-{name} to be recognized
    profile_path = client_dir / ".workspace-profile.json"
    gitconfig_path = client_dir / f".gitconfig-{name}"
    has_profile = profile_path.exists()
    has_gitconfig = gitconfig_path.exists()

    if not has_profile and not has_gitconfig:
        return None

    client = ClientNode(name=name, path=str(client_dir), has_profile=has_profile)

    # Load profile data
    if has_profile:
        profile = parse_workspace_profile(str(profile_path))
        if profile:
            client.description = profile.client_description
            client.git_platform = profile.git.platform.value
            client.cloud_provider = profile.cloud.provider.value
            client.database_type = profile.database.type.value
            client.language = profile.language.value

    # Load git email
    if has_gitconfig:
        git_identity = parse_gitconfig(
            str(gitconfig_path),
            platform=client.git_platform or "github",
            host="github.com",
            account=None,
        )
        if git_identity:
            client.git_email = git_identity.email

    # Discover client-level rules
    rules_dir = client_dir / ".claude" / "rules"
    if rules_dir.is_dir():
        client.rule_files = sorted(f.name for f in rules_dir.glob("*.md"))

    # Discover client-level MCPs
    mcp_path = client_dir / ".mcp.json"
    if mcp_path.exists():
        client.mcp_servers = parse_mcp_json(str(mcp_path))

    # Scan for projects (subfolders)
    for child in sorted(client_dir.iterdir()):
        if not child.is_dir():
            continue
        if child.name in SKIP_DIRS or child.name.startswith("."):
            continue

        project = _scan_project(child)
        client.projects.append(project)

    return client


def _scan_project(project_dir: Path) -> ProjectNode:
    """Scan a project directory for git, rules, MCPs."""
    project = ProjectNode(
        name=project_dir.name,
        path=str(project_dir),
        has_git=(project_dir / ".git").exists(),
        has_claude_md=(project_dir / "CLAUDE.md").exists(),
    )

    # Check for rules
    rules_dir = project_dir / ".claude" / "rules"
    if rules_dir.is_dir():
        project.has_claude_rules = True
        project.rule_files = sorted(f.name for f in rules_dir.glob("*.md"))

    # Check for MCP config
    mcp_path = project_dir / ".mcp.json"
    if mcp_path.exists():
        project.has_mcp_json = True
        project.mcp_servers = parse_mcp_json(str(mcp_path))

    return project
