"""Agent definitions: frontmatter, tool lists and the review contract."""

from __future__ import annotations

import re
from pathlib import Path

import pytest
import yaml

AGENTS = Path(__file__).resolve().parents[1] / "agents"
READ_ONLY = ["build-critic", "spec-critic", "arbiter", "security-reviewer", "privacy-reviewer", "llm-security-reviewer"]
WRITE_TOOLS = {"Edit", "Write", "MultiEdit", "NotebookEdit", "Bash", "PowerShell"}


def _fm(name: str) -> tuple[dict, str]:
    text = (AGENTS / f"{name}.md").read_text(encoding="utf-8")
    assert text.startswith("---\n")
    end = text.index("\n---\n", 4)
    return yaml.safe_load(text[4:end]), text[end + 5:]


@pytest.mark.parametrize("name", [p.stem for p in sorted(AGENTS.glob("*.md"))])
def test_every_agent_has_name_description_and_tools(name: str) -> None:
    fm, body = _fm(name)
    assert fm["name"] == name
    assert len(fm["description"]) >= 40
    assert fm.get("tools"), "agents list their tools explicitly"
    assert body.strip()


@pytest.mark.parametrize("name", READ_ONLY)
def test_reviewers_are_read_only(name: str) -> None:
    fm, _ = _fm(name)
    tools = {t.strip() for t in fm["tools"].split(",")}
    assert tools == {"Read", "Glob", "Grep"}
    disallowed = {t.strip() for t in fm["disallowedTools"].split(",")}
    assert WRITE_TOOLS <= disallowed and "Agent" in disallowed


@pytest.mark.parametrize("name", ["build-critic", "spec-critic", "security-reviewer", "privacy-reviewer", "llm-security-reviewer"])
def test_reviewers_return_the_contract(name: str) -> None:
    _, body = _fm(name)
    for token in ("RESULT: PASS | FAIL | BLOCKED", "BUILT-BY-ME:", "CANNOT JUDGE", "file:line"):
        assert token in body, (name, token)
    assert "VERDICT:" not in body


def test_planner_knows_the_workflow_fields() -> None:
    _, body = _fm("build-planner")
    for token in ('"workflow"', '"hotfix"', '"spec"'):
        assert token in body
    assert re.search(r"hotfix.*n/a", body, flags=re.DOTALL)


def test_breaker_requires_run_command_and_localhost() -> None:
    fm, body = _fm("breaker")
    tools = {t.strip() for t in fm["tools"].split(",")}
    assert "Bash" in tools and "Edit" not in tools and "Write" not in tools
    for token in (
        "127.0.0.1", "localhost", "commands.run", "BLOCKED", "never edit source",
        "third-party keys", "back up and restore", "--max-time", "stop the process",
    ):
        assert token in body, token
    assert "RESULT: PASS | FAIL | BLOCKED" in body


@pytest.mark.parametrize("name,ref", [
    ("security-reviewer", "auth-security/references/asvs-checklist.md"),
    ("privacy-reviewer", "privacy-review/references/checklist.md"),
    ("llm-security-reviewer", "llm-security-review/references/checklist.md"),
])
def test_reviewers_name_their_checklist(name: str, ref: str) -> None:
    _, body = _fm(name)
    assert ref in body
