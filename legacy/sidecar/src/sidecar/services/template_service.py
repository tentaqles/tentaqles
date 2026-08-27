"""Template service — renders client templates for new workspace creation.

Reads from .templates/ in the base directory, replaces ALL_CAPS placeholders,
strips .template extensions, and writes to the new client folder.
"""

from __future__ import annotations

import shutil
from dataclasses import dataclass, field
from pathlib import Path

# Directories to skip when copying the template
SKIP_COPY = {"scripts", "__pycache__"}

# Placeholder -> form field mapping
PLACEHOLDERS = {
    "CLIENT_NAME": "client_name",
    "CLIENT_DESCRIPTION": "client_description",
    "CLIENT_FOLDER_PATH": "client_folder_path",
    "DATABASE_TYPE": "database_type",
    "DATABASE_DIALECT": "database_dialect",
    "DATABASE_DIALECT_RULES": "database_dialect_rules",
    "SQL_DIALECT_NAME": "sql_dialect_name",
    "GIT_PLATFORM": "git_platform",
    "GIT_HOST": "git_host",
    "GIT_EMAIL": "git_email",
    "CLOUD_PROVIDER": "cloud_provider",
    "SUBSCRIPTION_NAME": "subscription_name",
    "SUBSCRIPTION_ID": "subscription_id",
    "CLOUD_EMAIL": "cloud_email",
    "CI_CD_PLATFORM": "ci_cd_platform",
    "LANGUAGE": "language",
    "YOUR_NAME": "your_name",
}


@dataclass
class TemplateFile:
    """A file from the template with placeholder info."""

    relative_path: str  # Path relative to template root (after stripping .template)
    is_template: bool  # Whether it has .template extension
    content: str | None = None  # Text content (None for binary)


@dataclass
class ClientPreview:
    """Preview of what will be created for a new client."""

    client_name: str
    dest_path: str
    files: list[TemplateFile] = field(default_factory=list)
    placeholders_used: set[str] = field(default_factory=set)


def get_template_path(base_path: str) -> Path:
    """Get the .templates directory path."""
    return Path(base_path) / ".templates"


def list_template_files(base_path: str) -> list[TemplateFile]:
    """List all files in the template directory with metadata."""
    template_dir = get_template_path(base_path)
    if not template_dir.is_dir():
        return []

    files: list[TemplateFile] = []
    for file_path in sorted(template_dir.rglob("*")):
        if not file_path.is_file():
            continue

        # Skip certain directories
        rel = file_path.relative_to(template_dir)
        if rel.parts[0] in SKIP_COPY:
            continue

        is_template = file_path.name.endswith(".template")
        rel_str = str(rel)
        if is_template:
            # Strip .template extension for display
            rel_str = rel_str.rsplit(".template", 1)[0]

        try:
            content = file_path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            content = None

        files.append(
            TemplateFile(
                relative_path=rel_str,
                is_template=is_template,
                content=content,
            )
        )

    return files


def preview_new_client(
    base_path: str,
    client_name: str,
    values: dict[str, str],
) -> ClientPreview:
    """Preview what will be created for a new client."""
    dest_path = str(Path(base_path) / client_name)
    preview = ClientPreview(client_name=client_name, dest_path=dest_path)

    template_files = list_template_files(base_path)
    for tf in template_files:
        rendered_path = tf.relative_path
        if tf.content is not None:
            rendered_content = _apply_replacements(tf.content, values)
            rendered_path = _apply_replacements(rendered_path, values)
        else:
            rendered_content = None

        preview.files.append(
            TemplateFile(
                relative_path=rendered_path,
                is_template=tf.is_template,
                content=rendered_content,
            )
        )

        # Track which placeholders are used
        if tf.content:
            for placeholder in PLACEHOLDERS:
                if placeholder in tf.content:
                    preview.placeholders_used.add(placeholder)

    return preview


def _scaffold_workspace(
    dest_dir: Path,
    client_name: str,
    values: dict[str, str],
    tentaqles_path: str | None = None,
) -> None:
    """Create the full Tentaqles workspace directory structure."""
    import json

    from sidecar.services.onboarding_service import generate_mcp_snippet, get_tentaqles_path

    if tentaqles_path is None:
        tentaqles_path = get_tentaqles_path()

    # .claude/rules/ — create directory and a starter identity rule
    rules_dir = dest_dir / ".claude" / "rules"
    rules_dir.mkdir(parents=True, exist_ok=True)

    identity_rule = f"""---
description: Identity and environment rules for {client_name}
---

# {client_name} — Workspace Rules

- Git platform: {values.get("git_platform", "github")}
- Cloud provider: {values.get("cloud_provider", "none")}
- Database: {values.get("database_type", "none")}
"""
    (rules_dir / "identity.md").write_text(identity_rule, encoding="utf-8")

    # .claude/skills/ — create empty directory
    (dest_dir / ".claude" / "skills").mkdir(parents=True, exist_ok=True)

    # Copy soul.md + user.md into .claude/ so CLAUDE.md can reference via @.claude/soul.md
    for personality_file in ["soul.md", "user.md"]:
        global_file = Path.home() / ".tentaqles" / personality_file
        if global_file.exists():
            dest_file = dest_dir / ".claude" / personality_file
            import shutil

            shutil.copy2(str(global_file), str(dest_file))

    # Install essential meta-skills from packs into client's .claude/skills/
    from sidecar.services.onboarding_service import get_packs_dir

    packs_dir = Path(get_packs_dir()) / "general"
    essential_skills = [
        "new-client",
        "new-project",
        "tentaqles-start-here",
        "tentaqles-task-routing",
        "tentaqles-output-standards",
        "tentaqles-session-wrap-up",
    ]
    client_skills_dir = dest_dir / ".claude" / "skills"
    for skill_name in essential_skills:
        src_skill = packs_dir / skill_name
        if src_skill.is_dir() and (src_skill / "SKILL.md").exists():
            dest_skill = client_skills_dir / skill_name
            if not dest_skill.exists():
                import shutil

                shutil.copytree(str(src_skill), str(dest_skill))

    # .claude/knowledge/ — create empty directory
    (dest_dir / ".claude" / "knowledge").mkdir(parents=True, exist_ok=True)

    # context/memory/ — create empty directory
    (dest_dir / "context" / "memory").mkdir(parents=True, exist_ok=True)

    # context/learnings.md — create starter file
    learnings_path = dest_dir / "context" / "learnings.md"
    learnings_path.write_text(
        f"""# Learnings — {client_name}

# General
## What works well

## What doesn't work well

# Individual Skills
""",
        encoding="utf-8",
    )

    # personality/ — create empty directory (inherits from global)
    (dest_dir / "personality").mkdir(parents=True, exist_ok=True)

    # brand_context/ — create directory for brand voice, positioning, ICP
    (dest_dir / "brand_context").mkdir(parents=True, exist_ok=True)

    # jobs/ — create empty directory
    (dest_dir / "jobs").mkdir(parents=True, exist_ok=True)

    # .gitconfig-{client} — create git identity file
    git_email = values.get("git_email", "")
    git_name = values.get("your_name", "")
    if git_email or git_name:
        gitconfig = f"""[user]
    name = {git_name}
    email = {git_email}
"""
        (dest_dir / f".gitconfig-{client_name}").write_text(gitconfig, encoding="utf-8")

    # .mcp.json — set up MCP server connection
    mcp_snippet = generate_mcp_snippet(tentaqles_path, str(dest_dir))
    mcp_path = dest_dir / ".mcp.json"
    mcp_path.write_text(json.dumps(mcp_snippet, indent=2) + "\n", encoding="utf-8")

    # .tentaqles.json — create default toggle state
    tentaqles_state = {
        "$schema": "tentaqles-v1",
        "toggles": {"rules": {}, "mcps": {}, "hooks": {}, "skills": {}, "commands": {}},
        "propagation_excludes": [],
        "last_propagated_at": None,
    }
    (dest_dir / ".tentaqles.json").write_text(json.dumps(tentaqles_state, indent=2) + "\n", encoding="utf-8")

    # CLAUDE.md — auto-generate from workspace state
    try:
        from sidecar.services.claudemd_generator import generate_claude_md

        claude_md = generate_claude_md(str(dest_dir))
        (dest_dir / "CLAUDE.md").write_text(claude_md, encoding="utf-8")
    except Exception:
        # Fallback: create a minimal CLAUDE.md
        (dest_dir / "CLAUDE.md").write_text(
            f"""# CLAUDE.md — {client_name}

*Auto-generated by Tentaqles.*

## Heartbeat

Before doing anything else in any session, call these MCP tools:

1. `get_personality()` — load soul + user profile
2. `get_session_memory()` — restore context from recent sessions
3. `get_active_skills()` — know what skills are available
4. `discover_knowledge()` — find relevant cross-project insights
5. `verify_identity()` — confirm correct git/cloud identity
6. `get_workspace_context()` — load rules, MCPs, dev rules
""",
            encoding="utf-8",
        )


def create_project(
    client_path: str,
    project_name: str,
    description: str = "",
    goal: str = "",
    deliverables: str = "",
    tech_stack: str = "",
) -> str:
    """Create a new project inside a client workspace.

    Merges three philosophies:
    - Cowork: self-contained workspace with instructions, context, memory, tasks
    - Agentic OS: brief.md with goal, deliverables, acceptance criteria
    - Tentaqles: git-backed, hierarchical config inheritance, MCP-connected

    Returns the path to the new project folder.
    """
    import json
    import shutil
    import subprocess

    from sidecar.services.onboarding_service import generate_mcp_snippet, get_tentaqles_path
    from sidecar.services.toggle_service import is_enabled

    client_dir = Path(client_path)
    project_dir = client_dir / project_name

    if project_dir.exists():
        raise FileExistsError(f"Directory already exists: {project_dir}")

    project_dir.mkdir(parents=True)

    # ── Git init ──
    subprocess.run(["git", "init"], cwd=str(project_dir), capture_output=True, text=True, check=False)

    # ── .claude/rules/ — propagate enabled client rules ──
    rules_dir = project_dir / ".claude" / "rules"
    rules_dir.mkdir(parents=True, exist_ok=True)
    client_rules = client_dir / ".claude" / "rules"
    if client_rules.is_dir():
        for rule_file in sorted(client_rules.glob("*.md")):
            if is_enabled(client_path, "rules", rule_file.name):
                shutil.copy2(str(rule_file), str(rules_dir / rule_file.name))

    # ── .claude/skills/ — propagate enabled client skills ──
    dest_skills = project_dir / ".claude" / "skills"
    dest_skills.mkdir(parents=True, exist_ok=True)
    client_skills = client_dir / ".claude" / "skills"
    if client_skills.is_dir():
        for skill_dir in sorted(client_skills.iterdir()):
            if skill_dir.is_dir() and not skill_dir.name.startswith("_"):
                if is_enabled(client_path, "skills", skill_dir.name):
                    dest = dest_skills / skill_dir.name
                    if not dest.exists():
                        shutil.copytree(str(skill_dir), str(dest))

    # Install essential meta-skills from packs if not already propagated from client
    from sidecar.services.onboarding_service import get_packs_dir

    packs_dir = Path(get_packs_dir()) / "general"
    essential_skills = [
        "new-client",
        "new-project",
        "tentaqles-start-here",
        "tentaqles-task-routing",
        "tentaqles-output-standards",
        "tentaqles-session-wrap-up",
    ]
    for skill_name in essential_skills:
        src_skill = packs_dir / skill_name
        if src_skill.is_dir() and (src_skill / "SKILL.md").exists():
            dest_skill = dest_skills / skill_name
            if not dest_skill.exists():
                shutil.copytree(str(src_skill), str(dest_skill))

    # Copy soul.md + user.md into .claude/ so CLAUDE.md can reference via @.claude/soul.md
    for personality_file in ["soul.md", "user.md"]:
        global_file = Path.home() / ".tentaqles" / personality_file
        if global_file.exists():
            dest_file = project_dir / ".claude" / personality_file
            import shutil

            shutil.copy2(str(global_file), str(dest_file))

    # ── .claude/knowledge/ ──
    (project_dir / ".claude" / "knowledge").mkdir(parents=True, exist_ok=True)

    # ── context/memory/ — Cowork-style scoped memory ──
    (project_dir / "context" / "memory").mkdir(parents=True, exist_ok=True)

    # ── context/learnings.md — per-project learnings journal ──
    (project_dir / "context" / "learnings.md").write_text(
        f"# Learnings — {project_name}\n\n"
        "# General\n## What works well\n\n## What doesn't work well\n\n"
        "# Individual Skills\n",
        encoding="utf-8",
    )

    # ── brand_context/ — inherits from client ──
    (project_dir / "brand_context").mkdir(parents=True, exist_ok=True)

    # ── personality/ — inherits from client/global ──
    (project_dir / "personality").mkdir(parents=True, exist_ok=True)

    # ── jobs/ — Cowork-style project-scoped scheduled tasks ──
    (project_dir / "jobs").mkdir(parents=True, exist_ok=True)

    # ── projects/ — Agentic OS output structure ──
    (project_dir / "projects").mkdir(parents=True, exist_ok=True)
    (project_dir / "projects" / "briefs").mkdir(parents=True, exist_ok=True)

    # ── brief.md — Agentic OS project scoping (if goal provided) ──
    if goal or description:
        from datetime import date

        deliverable_lines = ""
        if deliverables:
            deliverable_lines = "\n".join(f"- [ ] {d.strip()}" for d in deliverables.split("\n") if d.strip())

        brief_content = f"""---
project: {project_name}
status: active
level: 2
created: {date.today().isoformat()}
---

# {project_name}

## Goal
{goal or description}

## Deliverables
{deliverable_lines or "- [ ] (define deliverables)"}

## Acceptance Criteria
- (define how you'll know it's done)

## Tech Stack
{tech_stack or "(inherited from client)"}

## Notes
Created by Tentaqles. This brief scopes the project — all outputs go in this folder
or in `projects/` subdirectories following the output standards in CLAUDE.md.
"""
        (project_dir / "brief.md").write_text(brief_content, encoding="utf-8")

    # ── .tentaqles.json — toggle state ──
    (project_dir / ".tentaqles.json").write_text(
        json.dumps(
            {
                "$schema": "tentaqles-v1",
                "toggles": {"rules": {}, "mcps": {}, "hooks": {}, "skills": {}, "commands": {}},
                "propagation_excludes": [],
                "last_propagated_at": None,
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )

    # ── .mcp.json — point to PROJECT path so project-scoped memory/learnings work ──
    # _find_client_path in MCP tools walks up to find client for hierarchy resolution
    tentaqles_path = get_tentaqles_path()
    mcp_snippet = generate_mcp_snippet(tentaqles_path, str(project_dir))
    (project_dir / ".mcp.json").write_text(json.dumps(mcp_snippet, indent=2) + "\n", encoding="utf-8")

    # ── CLAUDE.md — auto-generated with heartbeat, task routing, output standards ──
    try:
        from sidecar.services.claudemd_generator import generate_claude_md

        claude_md = generate_claude_md(str(project_dir), client_path)
        (project_dir / "CLAUDE.md").write_text(claude_md, encoding="utf-8")
    except Exception:
        (project_dir / "CLAUDE.md").write_text(
            f"# CLAUDE.md — {project_name}\n\n*Generated by Tentaqles.*\n", encoding="utf-8"
        )

    # ── .gitignore ──
    (project_dir / ".gitignore").write_text(
        "# Dependencies\nnode_modules/\n.venv/\nvenv/\n__pycache__/\n\n"
        "# Environment\n.env\n.env.*\n!.env.example\n\n"
        "# IDE\n.idea/\n.vscode/\n*.swp\n\n"
        "# OS\n.DS_Store\nThumbs.db\n\n"
        "# Tentaqles local context\ncontext/memory/\n",
        encoding="utf-8",
    )

    return str(project_dir)


def create_client(
    base_path: str,
    client_name: str,
    values: dict[str, str],
) -> str:
    """Create a new client workspace from the template.

    Args:
        base_path: Root repos directory (e.g. D:/repos).
        client_name: Name of the new client (used as folder name).
        values: Dict of placeholder values (keys from PLACEHOLDERS values).

    Returns:
        Path to the new client folder.
    """
    template_dir = get_template_path(base_path)
    dest_dir = Path(base_path) / client_name

    if dest_dir.exists():
        raise FileExistsError(f"Directory already exists: {dest_dir}")

    # Build replacement map: PLACEHOLDER -> value
    replacements = _build_replacements(client_name, values, base_path)

    dest_dir.mkdir(parents=True)

    # Copy and render all template files
    for src_file in sorted(template_dir.rglob("*")):
        if not src_file.is_file():
            continue

        rel = src_file.relative_to(template_dir)
        if rel.parts[0] in SKIP_COPY:
            continue

        # Determine destination path
        dest_rel = str(rel)
        is_template = src_file.name.endswith(".template")
        if is_template:
            dest_rel = dest_rel.rsplit(".template", 1)[0]

        # Apply replacements to path (e.g. .gitconfig-template -> .gitconfig-client_name)
        dest_rel = dest_rel.replace("template", client_name)
        dest_file = dest_dir / dest_rel

        dest_file.parent.mkdir(parents=True, exist_ok=True)

        if is_template:
            # Read, replace, write
            try:
                content = src_file.read_text(encoding="utf-8")
                rendered = _apply_replacements(content, replacements)
                dest_file.write_text(rendered, encoding="utf-8")
            except UnicodeDecodeError:
                # Binary file — just copy
                shutil.copy2(str(src_file), str(dest_file))
        else:
            # Non-template — copy as-is
            shutil.copy2(str(src_file), str(dest_file))

    # Create .workspace-profile.json
    _create_workspace_profile(dest_dir, client_name, values)

    # Scaffold the full workspace structure
    _scaffold_workspace(dest_dir, client_name, values)

    return str(dest_dir)


def _build_replacements(
    client_name: str,
    values: dict[str, str],
    base_path: str,
) -> dict[str, str]:
    """Build the full placeholder -> value replacement map."""
    replacements: dict[str, str] = {}

    for placeholder, field_name in PLACEHOLDERS.items():
        val = values.get(field_name, "")
        if val:
            replacements[placeholder] = val

    # Always set these from the client name if not explicitly provided
    replacements.setdefault("CLIENT_NAME", client_name)
    replacements.setdefault("CLIENT_FOLDER_PATH", str(Path(base_path) / client_name))

    return replacements


def _apply_replacements(text: str, replacements: dict[str, str]) -> str:
    """Replace all placeholders in text."""
    result = text
    for placeholder, value in replacements.items():
        result = result.replace(placeholder, value)
    return result


def _create_workspace_profile(
    dest_dir: Path,
    client_name: str,
    values: dict[str, str],
) -> None:
    """Create a .workspace-profile.json for the new client."""
    import json

    profile = {
        "$schema": "workspace-profile-v1",
        "client_name": client_name,
        "client_description": values.get("client_description", ""),
        "git": {
            "platform": values.get("git_platform", "github"),
            "host": values.get("git_host", "github.com"),
            "account": None,
        },
        "cloud": {
            "provider": values.get("cloud_provider", "none"),
            "subscription_name": values.get("subscription_name", None),
            "subscription_id": values.get("subscription_id", None),
        },
        "database": {
            "type": values.get("database_type", "none"),
            "dialect": values.get("database_dialect", None),
            "connection_details": None,
        },
        "language": values.get("language", "en"),
        "tech_stack": [t.strip() for t in values.get("tech_stack", "").split(",") if t.strip()],
        "special_rules": [],
    }

    profile_path = dest_dir / ".workspace-profile.json"
    profile_path.write_text(
        json.dumps(profile, indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )
