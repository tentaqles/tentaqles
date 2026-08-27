"""Skills service — hierarchical skill discovery across global/client/project."""

from __future__ import annotations

import json
from pathlib import Path

from sidecar.models import SkillLevel, SkillMeta, SkillPack
from sidecar.parsers import parse_yaml_frontmatter, read_file_content
from sidecar.services.toggle_service import is_enabled


def _global_skills_dir() -> Path:
    """~/.claude/skills/ — Claude Code's native global discovery path."""
    return Path.home() / ".claude" / "skills"


def _client_skills_dir(client_path: str) -> Path:
    """{client}/.claude/skills/ — shared across client projects."""
    return Path(client_path) / ".claude" / "skills"


def _project_skills_dir(project_path: str) -> Path:
    """{project}/.claude/skills/ — project-specific."""
    return Path(project_path) / ".claude" / "skills"


def _parse_skill_dir(skill_dir: Path, level: SkillLevel) -> SkillMeta | None:
    """Parse a single skill directory into SkillMeta."""
    skill_md = skill_dir / "SKILL.md"
    content = read_file_content(str(skill_md))
    if content is None:
        return None

    frontmatter, body = parse_yaml_frontmatter(content)

    files = [str(f.relative_to(skill_dir)) for f in skill_dir.rglob("*") if f.is_file()]

    # Extract category from name prefix (e.g., "mkt-brand-voice" -> "mkt")
    name = frontmatter.get("name", skill_dir.name)
    category = name.split("-")[0] if "-" in name else None

    # Parse context_needs from frontmatter
    context_needs = {}
    raw_context = frontmatter.get("context_needs", "")
    if isinstance(raw_context, str) and raw_context:
        for pair in raw_context.split(","):
            pair = pair.strip()
            if ":" in pair:
                k, v = pair.split(":", 1)
                context_needs[k.strip()] = v.strip()

    # Parse dependencies from frontmatter
    deps = frontmatter.get("dependencies", [])
    if isinstance(deps, str):
        deps = [d.strip() for d in deps.split(",") if d.strip()]

    return SkillMeta(
        name=name,
        description=frontmatter.get("description"),
        version=frontmatter.get("version"),
        category=category,
        level=level,
        source_path=str(skill_dir),
        body=body,
        files=sorted(files),
        context_needs=context_needs,
        dependencies=deps if isinstance(deps, list) else [],
    )


def _scan_skills_dir(skills_dir: Path, level: SkillLevel) -> list[SkillMeta]:
    """Scan a skills directory and return all valid skills."""
    results: list[SkillMeta] = []
    if not skills_dir.is_dir():
        return results

    for skill_dir in sorted(skills_dir.iterdir()):
        if not skill_dir.is_dir() or skill_dir.name.startswith("_"):
            continue
        meta = _parse_skill_dir(skill_dir, level)
        if meta:
            results.append(meta)
    return results


def discover_skills(
    workspace_path: str,
    client_path: str | None = None,
) -> list[SkillMeta]:
    """Discover all skills for a workspace with hierarchy resolution.

    Resolution order: project > client > global.
    Same skill name at a more specific level overrides the inherited one.
    Toggle state from .tentaqles.json is applied.
    """
    global_skills = _scan_skills_dir(_global_skills_dir(), SkillLevel.GLOBAL)
    client_skills = _scan_skills_dir(_client_skills_dir(client_path), SkillLevel.CLIENT) if client_path else []
    project_skills = _scan_skills_dir(_project_skills_dir(workspace_path), SkillLevel.PROJECT)

    # Merge with most-specific-wins
    merged: dict[str, SkillMeta] = {}
    for skill in global_skills:
        merged[skill.name] = skill
    for skill in client_skills:
        merged[skill.name] = skill
    for skill in project_skills:
        merged[skill.name] = skill

    # Apply toggle state
    for skill in merged.values():
        skill.enabled = is_enabled(workspace_path, "skills", skill.name)

    return sorted(merged.values(), key=lambda s: s.name)


def get_skill(workspace_path: str, skill_name: str, client_path: str | None = None) -> SkillMeta | None:
    """Get a single skill by name with hierarchy resolution."""
    if ".." in skill_name or "/" in skill_name or "\\" in skill_name:
        return None

    all_skills = discover_skills(workspace_path, client_path)
    for skill in all_skills:
        if skill.name == skill_name:
            return skill
    return None


def get_skill_context(workspace_path: str, skill_name: str, client_path: str | None = None) -> dict | None:
    """Get full skill content for Claude to use during invocation."""
    skill = get_skill(workspace_path, skill_name, client_path)
    if not skill:
        return None

    skill_md = Path(skill.source_path) / "SKILL.md"
    raw_content = read_file_content(str(skill_md))

    refs_dir = Path(skill.source_path) / "references"
    references: dict[str, str] = {}
    if refs_dir.is_dir():
        for ref_file in sorted(refs_dir.iterdir()):
            if ref_file.is_file() and ref_file.suffix == ".md":
                ref_content = read_file_content(str(ref_file))
                if ref_content:
                    references[ref_file.name] = ref_content

    return {
        "name": skill.name,
        "raw_content": raw_content,
        "context_needs": skill.context_needs,
        "dependencies": skill.dependencies,
        "references": references,
    }


def install_skill(
    level: str,
    skill_name: str,
    source_path: str,
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> str:
    """Install a skill by copying from source to the target level."""
    import shutil

    src = Path(source_path)
    if not src.is_dir():
        return f"Source skill directory not found: {source_path}"

    if level == "global":
        dest = _global_skills_dir() / skill_name
    elif level == "client":
        if not client_path:
            return "client_path required for client-level install"
        dest = _client_skills_dir(client_path) / skill_name
    elif level == "project":
        if not workspace_path:
            return "workspace_path required for project-level install"
        dest = _project_skills_dir(workspace_path) / skill_name
    else:
        return f"Unknown level: {level}"

    dest.parent.mkdir(parents=True, exist_ok=True)
    if dest.exists():
        shutil.rmtree(dest)
    shutil.copytree(src, dest)
    return f"Installed {skill_name} at {level} level: {dest}"


def remove_skill(
    level: str,
    skill_name: str,
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> str:
    """Remove a skill from a specific hierarchy level."""
    import shutil

    if ".." in skill_name or "/" in skill_name or "\\" in skill_name:
        return "Invalid skill name"

    if level == "global":
        target = _global_skills_dir() / skill_name
    elif level == "client":
        if not client_path:
            return "client_path required for client-level removal"
        target = _client_skills_dir(client_path) / skill_name
    elif level == "project":
        if not workspace_path:
            return "workspace_path required for project-level removal"
        target = _project_skills_dir(workspace_path) / skill_name
    else:
        return f"Unknown level: {level}"

    if not target.exists():
        return f"Skill not found at {level} level: {skill_name}"

    shutil.rmtree(target)
    return f"Removed {skill_name} from {level} level"


def promote_skill(
    skill_name: str,
    from_level: str,
    to_level: str,
    workspace_path: str,
    client_path: str | None = None,
) -> str:
    """Copy a skill from one level to a higher level."""
    if from_level == "project":
        src = _project_skills_dir(workspace_path) / skill_name
    elif from_level == "client" and client_path:
        src = _client_skills_dir(client_path) / skill_name
    else:
        return f"Cannot promote from {from_level}"

    if not src.is_dir():
        return f"Skill not found at {from_level} level: {skill_name}"

    return install_skill(to_level, skill_name, str(src), workspace_path, client_path)


def list_packs(packs_dir: str | None = None) -> list[SkillPack]:
    """List available skill packs from the packs directory."""
    if packs_dir is None:
        from sidecar.services.onboarding_service import get_packs_dir

        packs_dir = get_packs_dir()

    packs_path = Path(packs_dir)
    results: list[SkillPack] = []
    if not packs_path.is_dir():
        return results

    for pack_dir in sorted(packs_path.iterdir()):
        if not pack_dir.is_dir():
            continue
        manifest = pack_dir / "pack.json"
        content = read_file_content(str(manifest))
        if content is None:
            continue
        try:
            data = json.loads(content)
            results.append(SkillPack(**data))
        except (json.JSONDecodeError, ValueError):
            continue

    return results


def install_pack(
    pack_name: str,
    level: str,
    workspace_path: str | None = None,
    client_path: str | None = None,
    packs_dir: str | None = None,
) -> list[str]:
    """Install all skills from a pack at the specified level."""
    if packs_dir is None:
        from sidecar.services.onboarding_service import get_packs_dir

        packs_dir = get_packs_dir()

    pack_dir = Path(packs_dir) / pack_name
    manifest = pack_dir / "pack.json"
    content = read_file_content(str(manifest))
    if content is None:
        return [f"Pack not found: {pack_name}"]

    try:
        data = json.loads(content)
        pack = SkillPack(**data)
    except (json.JSONDecodeError, ValueError) as e:
        return [f"Invalid pack manifest: {e}"]

    results: list[str] = []

    for dep in pack.dependencies:
        dep_path = pack_dir / dep
        if dep_path.is_dir():
            results.append(install_skill(level, dep, str(dep_path), workspace_path, client_path))

    for skill_name in pack.skills:
        skill_path = pack_dir / skill_name
        if skill_path.is_dir():
            results.append(install_skill(level, skill_name, str(skill_path), workspace_path, client_path))
        else:
            results.append(f"Skill directory not found in pack: {skill_name}")

    return results
