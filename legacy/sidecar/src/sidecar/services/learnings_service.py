"""Learnings service — append-only per-skill feedback journal."""

from __future__ import annotations

import re
from datetime import date
from pathlib import Path

from sidecar.parsers import read_file_content


def _learnings_path(workspace_path: str) -> Path:
    return Path(workspace_path) / "context" / "learnings.md"


def _global_learnings_path() -> Path:
    return Path.home() / ".tentaqles" / "context" / "learnings.md"


def _find_client_path(workspace_path: str) -> str | None:
    current = Path(workspace_path)
    for parent in [current] + list(current.parents):
        if (parent / ".workspace-profile.json").exists():
            return str(parent)
        if (parent / ".tentaqles.json").exists():
            return str(parent)
    return None


def _filter_by_skill(content: str, skill_name: str) -> str:
    """Extract a specific ## {skill_name} section from learnings markdown."""
    pattern = rf"(## {re.escape(skill_name)})\n(.*?)(?=\n## |\Z)"
    match = re.search(pattern, content, re.DOTALL)
    if match:
        return match.group(0).strip()
    return ""


def get_learnings(workspace_path: str, client_path: str | None = None, skill_name: str | None = None) -> dict:
    """Read learnings from all three levels."""
    result = {"project": "", "client": "", "global": ""}

    # Project level
    proj_content = read_file_content(str(_learnings_path(workspace_path))) or ""
    result["project"] = _filter_by_skill(proj_content, skill_name) if skill_name else proj_content

    # Client level
    cp = client_path or _find_client_path(workspace_path)
    if cp and cp != workspace_path:
        co_content = read_file_content(str(_learnings_path(cp))) or ""
        result["client"] = _filter_by_skill(co_content, skill_name) if skill_name else co_content

    # Global
    gl_content = read_file_content(str(_global_learnings_path())) or ""
    result["global"] = _filter_by_skill(gl_content, skill_name) if skill_name else gl_content

    return result


def append_learning(workspace_path: str, skill_name: str, entry: str) -> None:
    """Append a dated entry to learnings.md."""
    path = _learnings_path(workspace_path)
    path.parent.mkdir(parents=True, exist_ok=True)

    today = date.today().isoformat()
    dated_entry = f"- {today}: {entry}"

    if not path.exists():
        # Create skeleton
        skeleton = f"""# General
## What works well

## What doesn't work well

# Individual Skills
## {skill_name}
{dated_entry}
"""
        path.write_text(skeleton, encoding="utf-8")
        return

    content = path.read_text(encoding="utf-8")

    # Handle general sections
    if skill_name.startswith("general/"):
        sub = skill_name.split("/", 1)[1]
        section_name = "What works well" if sub == "what-works" else "What doesn't work well"
        marker = f"## {section_name}"
        if marker in content:
            # Find the section and append
            idx = content.index(marker) + len(marker)
            # Find next section
            next_section = re.search(r"\n## ", content[idx:])
            insert_at = idx + next_section.start() if next_section else len(content)
            content = content[:insert_at].rstrip() + f"\n{dated_entry}\n" + content[insert_at:]
        path.write_text(content, encoding="utf-8")
        return

    # Handle skill sections
    section_marker = f"## {skill_name}"
    if section_marker in content:
        idx = content.index(section_marker) + len(section_marker)
        next_section = re.search(r"\n## ", content[idx:])
        insert_at = idx + next_section.start() if next_section else len(content)
        content = content[:insert_at].rstrip() + f"\n{dated_entry}\n" + content[insert_at:]
    else:
        # Add new section at the end
        content = content.rstrip() + f"\n\n## {skill_name}\n{dated_entry}\n"

    path.write_text(content, encoding="utf-8")


def promote_learning(
    workspace_path: str,
    client_path: str | None,
    skill_name: str,
    entry_text: str,
    to_level: str,
) -> None:
    """Copy a learning entry to a higher level."""
    if to_level == "client":
        cp = client_path or _find_client_path(workspace_path)
        if not cp:
            raise ValueError("Cannot find client path")
        target_workspace = cp
    elif to_level == "global":
        target_workspace = str(_global_learnings_path().parent.parent)
    else:
        raise ValueError(f"Invalid to_level: {to_level}")

    append_learning(target_workspace, skill_name, entry_text)
