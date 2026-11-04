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

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
SKILLS_DIR = PLUGIN_ROOT / "skills"
SKILL_FILES = sorted(SKILLS_DIR.glob("*/SKILL.md"))

# The 40 skills of the slim plugin (0.16.0): memory, identity, knowledge, the dev-process
# checklists and the engineering-practice skills. The build gate, loop, hub, sync, refine,
# plan-cards, guard, pack, the nine pack skills, ml-review, pipeline-review and explore are gone.
SKILLS = [
    "add-client", "add-project", "auth-security", "babysit-pr", "build-graph", "ci-pipeline",
    "classify-data", "client-settings", "compact-memory", "cross-patterns", "dashboard", "db-migration",
    "decision-history", "direction", "dod", "emit-signal", "export-vault", "feature", "file-history", "fix",
    "grill-me", "index-advisor", "kickoff", "llm-security-review", "n8n-triage", "onboard", "prd",
    "privacy-review", "profile-refresh", "query-memory", "rollback", "session-wrap", "setup-demo", "ship",
    "supply-chain", "switch-client", "system-design-review", "threat-model", "workspace-status", "wrap",
]

# The dev-process checklists: plain skills, no gate, no hub, no headless mode.
WORKFLOW_SKILLS = ["ship", "babysit-pr", "wrap", "n8n-triage", "feature", "fix", "kickoff", "dod", "direction"]

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


def test_skill_set_is_exactly_the_forty() -> None:
    found = sorted(p.name for p in SKILLS_DIR.iterdir() if (p / "SKILL.md").is_file())
    assert found == sorted(SKILLS), set(found) ^ set(SKILLS)
    assert len(SKILLS) == 40


def test_claude_md_skill_count_matches_the_tree() -> None:
    n = len([p for p in SKILLS_DIR.iterdir() if (p / "SKILL.md").is_file()])
    m = re.search(r"(\d+) skill directories", (PLUGIN_ROOT / "CLAUDE.md").read_text(encoding="utf-8"))
    assert m and int(m.group(1)) == n, (m and m.group(1), n)


@pytest.mark.parametrize("name", WORKFLOW_SKILLS)
def test_workflow_skill_exists(name: str) -> None:
    assert (SKILLS_DIR / name / "SKILL.md").is_file()


@pytest.mark.parametrize("path", SKILL_FILES, ids=lambda p: p.parent.name)
def test_no_skill_names_a_removed_command(path: Path) -> None:
    text = path.read_text(encoding="utf-8")
    for forbidden in ("build-gate", "tq hub", "tq pack", "tq decide", "TQ_HUB_TASK", "TENTAQLES_HEADLESS",
                      "tq mode", "tq guard", "tq redact", "tq insights", "/tentaqles:build", "/tentaqles:hub",
                      "/tentaqles:sync", "/tentaqles:pack", "/tentaqles:guard", "/tentaqles:explore",
                      "/tentaqles:loop", "/tentaqles:refine", "/tentaqles:plan-cards"):
        assert forbidden not in text, (path.parent.name, forbidden)


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
