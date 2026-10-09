"""Every plugin skill has valid SKILL.md frontmatter.

Claude Code discovers a skill from the YAML frontmatter at the top of its
SKILL.md: ``name`` must match the directory and ``description`` is what the
model reads to decide when to use it. A broken block silently drops the skill.
"""

from __future__ import annotations

import re
from pathlib import Path

import pytest
import yaml

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"
SKILL_FILES = sorted(SKILLS_DIR.glob("*/SKILL.md"))

# Skills added by the operational-workflow work; they must keep existing.
WORKFLOW_SKILLS = ["ship", "babysit-pr", "wrap", "n8n-triage"]

NAME_RE = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")


def _frontmatter(path: Path) -> dict:
    text = path.read_text(encoding="utf-8")
    assert text.startswith("---\n") or text.startswith("---\r\n"), f"{path}: no frontmatter"
    parts = re.split(r"^---\s*$", text, maxsplit=2, flags=re.MULTILINE)
    assert len(parts) == 3, f"{path}: frontmatter is not closed with ---"
    data = yaml.safe_load(parts[1])
    assert isinstance(data, dict), f"{path}: frontmatter is not a mapping"
    return data


def test_skills_found() -> None:
    assert len(SKILL_FILES) >= 20


@pytest.mark.parametrize("name", WORKFLOW_SKILLS)
def test_workflow_skill_exists(name: str) -> None:
    assert (SKILLS_DIR / name / "SKILL.md").is_file()


@pytest.mark.parametrize("path", SKILL_FILES, ids=lambda p: p.parent.name)
def test_frontmatter_valid(path: Path) -> None:
    fm = _frontmatter(path)
    name = fm.get("name")
    assert name == path.parent.name, f"name {name!r} != directory {path.parent.name!r}"
    assert NAME_RE.match(name) and len(name) <= 64
    desc = fm.get("description")
    assert isinstance(desc, str) and len(desc.strip()) >= 40, "description missing or too short"
    assert len(desc) <= 1024, f"description is {len(desc)} chars (limit 1024)"
    body = path.read_text(encoding="utf-8").split("---", 2)[2]
    assert body.strip(), "SKILL.md has no body"


def test_session_wrap_keeps_its_triggers() -> None:
    desc = _frontmatter(SKILLS_DIR / "session-wrap" / "SKILL.md")["description"]
    for phrase in ("done", "wrapping up", "end session", "save session", "record a decision"):
        assert phrase in desc


def test_wrap_delegates_memory_to_session_wrap() -> None:
    body = (SKILLS_DIR / "wrap" / "SKILL.md").read_text(encoding="utf-8")
    assert "`session-wrap`" in body


@pytest.mark.parametrize("name", ["ship", "babysit-pr"])
def test_admin_merge_only_appears_as_forbidden(name: str) -> None:
    section = ""
    for line in (SKILLS_DIR / name / "SKILL.md").read_text(encoding="utf-8").splitlines():
        if line.startswith("#"):
            section = line
        if "--admin" in line:
            forbidden = re.search(r"\b(never|not)\b", line + " " + section, re.IGNORECASE)
            assert forbidden, f"{name}: {line.strip()}"
