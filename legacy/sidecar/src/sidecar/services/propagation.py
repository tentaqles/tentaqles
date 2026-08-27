"""Propagation engine — top-down config push from client to projects.

Replaces the PowerShell propagate-ai-config.ps1 script with a Python
implementation that respects toggle states from .tentaqles.json.
"""

from __future__ import annotations

import json
import shutil
import subprocess
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path

from sidecar.services.hierarchy import SKIP_DIRS
from sidecar.services.toggle_service import get_manager_state, save_manager_state

# Marker used to detect if a .gitignore already has our credentials block
_CREDENTIALS_MARKER = "Credentials and secrets (do not commit)"


@dataclass
class PropagationAction:
    """A single file copy/create action to be performed or previewed."""

    action_type: str  # "copy", "mkdir", "git-init", "gitignore-append", "gitignore-create"
    source: str | None = None
    destination: str = ""
    description: str = ""


@dataclass
class ProjectPropagationResult:
    """Result of propagating to one project."""

    project_name: str
    project_path: str
    actions: list[PropagationAction] = field(default_factory=list)
    skipped: bool = False
    skip_reason: str = ""
    error: str | None = None


@dataclass
class PropagationResult:
    """Result of a full client propagation."""

    client_name: str
    client_path: str
    projects: list[ProjectPropagationResult] = field(default_factory=list)
    timestamp: str = ""

    @property
    def total_actions(self) -> int:
        return sum(len(p.actions) for p in self.projects if not p.skipped)

    @property
    def projects_updated(self) -> int:
        return sum(1 for p in self.projects if not p.skipped and p.actions)


def get_credentials_block(base_path: str) -> str | None:
    """Read the gitignore credentials template from .templates/.scripts/."""
    template_path = Path(base_path) / ".templates" / ".scripts" / "gitignore-credentials.txt"
    if template_path.exists():
        return template_path.read_text(encoding="utf-8")
    return None


def list_propagation_targets(client_path: str) -> list[Path]:
    """List project subdirectories that are propagation targets."""
    client_dir = Path(client_path)
    state = get_manager_state(client_path)
    excludes = set(state.propagation_excludes)

    targets: list[Path] = []
    for child in sorted(client_dir.iterdir()):
        if not child.is_dir():
            continue
        if child.name.startswith(".") or child.name in SKIP_DIRS:
            continue
        if child.name in excludes:
            continue
        targets.append(child)
    return targets


def preview_propagation(
    client_path: str,
    base_path: str,
    config_types: set[str] | None = None,
) -> PropagationResult:
    """Preview what propagation would do without making changes.

    Args:
        client_path: Path to the client folder.
        base_path: Root repos path (for finding credentials template).
        config_types: Optional set of types to propagate. None means all.
                      Valid types: "claude", "copilot", "claude-md",
                      "git-init", "gitignore", "skills"
    """
    return _propagate(client_path, base_path, config_types, dry_run=True)


def propagate(
    client_path: str,
    base_path: str,
    config_types: set[str] | None = None,
) -> PropagationResult:
    """Propagate client configs to all project subfolders.

    Args:
        client_path: Path to the client folder.
        base_path: Root repos path (for finding credentials template).
        config_types: Optional set of types to propagate. None means all.
    """
    result = _propagate(client_path, base_path, config_types, dry_run=False)

    # Update last propagated timestamp
    state = get_manager_state(client_path)
    state.last_propagated_at = datetime.now(timezone.utc).isoformat()
    save_manager_state(client_path, state)

    return result


def _propagate(
    client_path: str,
    base_path: str,
    config_types: set[str] | None,
    dry_run: bool,
) -> PropagationResult:
    """Core propagation logic."""
    client_dir = Path(client_path)
    all_types = config_types is None

    result = PropagationResult(
        client_name=client_dir.name,
        client_path=str(client_dir),
        timestamp=datetime.now(timezone.utc).isoformat(),
    )

    # Load toggle state
    state = get_manager_state(client_path)
    toggles = state.toggles

    # Load credentials block
    credentials_block = get_credentials_block(base_path)

    # Get project targets
    targets = list_propagation_targets(client_path)

    for project_dir in targets:
        project_result = ProjectPropagationResult(
            project_name=project_dir.name,
            project_path=str(project_dir),
        )

        try:
            # 1. Git init if not a repo
            if all_types or "git-init" in config_types:
                _propagate_git_init(project_dir, project_result, dry_run)

            # 2. Gitignore credentials block
            if all_types or "gitignore" in config_types:
                _propagate_gitignore(project_dir, credentials_block, project_result, dry_run)

            # 3. .claude/ (Claude Code — rules, settings, etc.)
            if all_types or "claude" in config_types:
                _propagate_claude_dir(client_dir, project_dir, toggles.rules, project_result, dry_run)

            # 4. .github/copilot-instructions.md
            if all_types or "copilot" in config_types:
                _propagate_copilot(client_dir, project_dir, project_result, dry_run)

            # 5. CLAUDE.md
            if all_types or "claude-md" in config_types:
                _propagate_claude_md(client_dir, project_dir, project_result, dry_run)

            # 6. .mcp.json (Claude Code MCPs)
            if all_types or "mcp-json" in config_types:
                _propagate_mcp_json(client_dir, project_dir, toggles.mcps, project_result, dry_run)

            # 7. .claude/skills/ (Skills)
            if all_types or "skills" in config_types:
                _propagate_skills(client_dir, project_dir, toggles.skills, project_result, dry_run)

        except Exception as e:
            project_result.error = str(e)

        result.projects.append(project_result)

    return result


def _propagate_git_init(
    project_dir: Path,
    result: ProjectPropagationResult,
    dry_run: bool,
) -> None:
    """Ensure the project has a .git directory."""
    if (project_dir / ".git").exists():
        return

    action = PropagationAction(
        action_type="git-init",
        destination=str(project_dir),
        description=f"git init in {project_dir.name}",
    )
    result.actions.append(action)

    if not dry_run:
        subprocess.run(
            ["git", "init"],
            cwd=str(project_dir),
            capture_output=True,
            text=True,
            check=False,
        )


def _propagate_gitignore(
    project_dir: Path,
    credentials_block: str | None,
    result: ProjectPropagationResult,
    dry_run: bool,
) -> None:
    """Ensure .gitignore has the credentials block."""
    if not credentials_block:
        return

    gitignore_path = project_dir / ".gitignore"
    needs_block = False

    if not gitignore_path.exists():
        needs_block = True
        action_type = "gitignore-create"
    else:
        existing = gitignore_path.read_text(encoding="utf-8", errors="replace")
        if _CREDENTIALS_MARKER not in existing:
            needs_block = True
            action_type = "gitignore-append"
        else:
            return  # Already has it

    if not needs_block:
        return

    action = PropagationAction(
        action_type=action_type,
        destination=str(gitignore_path),
        description=f"Add credentials block to .gitignore in {project_dir.name}",
    )
    result.actions.append(action)

    if not dry_run:
        if gitignore_path.exists():
            with gitignore_path.open("a", encoding="utf-8") as f:
                f.write(f"\n{credentials_block}")
        else:
            gitignore_path.write_text(credentials_block, encoding="utf-8")


def _propagate_claude_dir(
    client_dir: Path,
    project_dir: Path,
    rule_toggles: dict[str, bool],
    result: ProjectPropagationResult,
    dry_run: bool,
) -> None:
    """Copy .claude/ directory recursively, respecting rule toggles."""
    src_claude = client_dir / ".claude"
    if not src_claude.is_dir():
        return

    dest_claude = project_dir / ".claude"

    for src_file in sorted(src_claude.rglob("*")):
        if not src_file.is_file():
            continue

        rel_path = src_file.relative_to(src_claude)

        # Check toggles for rule files under .claude/rules/
        if rel_path.parts[0] == "rules" and rel_path.suffix in (".md", ".mdc"):
            if not rule_toggles.get(rel_path.name, True):
                continue

        dest_file = dest_claude / rel_path
        action = PropagationAction(
            action_type="copy",
            source=str(src_file),
            destination=str(dest_file),
            description=f".claude/{rel_path}",
        )
        result.actions.append(action)

        if not dry_run:
            dest_file.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(str(src_file), str(dest_file))


def _propagate_copilot(
    client_dir: Path,
    project_dir: Path,
    result: ProjectPropagationResult,
    dry_run: bool,
) -> None:
    """Copy .github/copilot-instructions.md to project."""
    src = client_dir / ".github" / "copilot-instructions.md"
    if not src.exists():
        return

    dest = project_dir / ".github" / "copilot-instructions.md"
    action = PropagationAction(
        action_type="copy",
        source=str(src),
        destination=str(dest),
        description=".github/copilot-instructions.md",
    )
    result.actions.append(action)

    if not dry_run:
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(str(src), str(dest))


def _propagate_claude_md(
    client_dir: Path,
    project_dir: Path,
    result: ProjectPropagationResult,
    dry_run: bool,
) -> None:
    """Copy CLAUDE.md to project."""
    src = client_dir / "CLAUDE.md"
    if not src.exists():
        return

    dest = project_dir / "CLAUDE.md"
    action = PropagationAction(
        action_type="copy",
        source=str(src),
        destination=str(dest),
        description="CLAUDE.md",
    )
    result.actions.append(action)

    if not dry_run:
        shutil.copy2(str(src), str(dest))


def _propagate_mcp_json(
    client_dir: Path,
    project_dir: Path,
    mcp_toggles: dict[str, bool],
    result: ProjectPropagationResult,
    dry_run: bool,
) -> None:
    """Copy .mcp.json to project, filtering disabled MCPs."""
    src_mcp = client_dir / ".mcp.json"
    if not src_mcp.exists():
        return

    dest_mcp = project_dir / ".mcp.json"

    # Read source config and filter by toggles
    src_content = src_mcp.read_text(encoding="utf-8")
    try:
        config = json.loads(src_content)
    except json.JSONDecodeError:
        return

    servers = config.get("mcpServers", {})
    filtered_servers = {name: cfg for name, cfg in servers.items() if mcp_toggles.get(name, True)}
    filtered_config = {**config, "mcpServers": filtered_servers}

    action = PropagationAction(
        action_type="copy",
        source=str(src_mcp),
        destination=str(dest_mcp),
        description=f".mcp.json ({len(filtered_servers)} servers)",
    )
    result.actions.append(action)

    if not dry_run:
        dest_mcp.write_text(
            json.dumps(filtered_config, indent=2, ensure_ascii=False) + "\n",
            encoding="utf-8",
        )


def _propagate_skills(
    client_dir: Path,
    project_dir: Path,
    skill_toggles: dict[str, bool],
    result: ProjectPropagationResult,
    dry_run: bool,
) -> None:
    """Copy .claude/skills/ from client to project, respecting toggles."""
    src_skills = client_dir / ".claude" / "skills"
    if not src_skills.is_dir():
        return

    dest_skills = project_dir / ".claude" / "skills"

    for skill_dir in sorted(src_skills.iterdir()):
        if not skill_dir.is_dir() or skill_dir.name.startswith("_"):
            continue

        # Check toggle: default to enabled if not explicitly set
        if not skill_toggles.get(skill_dir.name, True):
            continue

        dest_skill = dest_skills / skill_dir.name
        action = PropagationAction(
            action_type="skill-copy",
            source=str(skill_dir),
            destination=str(dest_skill),
            description=f".claude/skills/{skill_dir.name}",
        )
        result.actions.append(action)

        if not dry_run:
            dest_skill.parent.mkdir(parents=True, exist_ok=True)
            if dest_skill.exists():
                shutil.rmtree(dest_skill)
            shutil.copytree(str(skill_dir), str(dest_skill))
