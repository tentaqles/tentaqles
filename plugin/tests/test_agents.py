"""Agent definitions: frontmatter, tool lists and the review contract."""

from __future__ import annotations

import re
from pathlib import Path

import pytest
import yaml

AGENTS = Path(__file__).resolve().parents[1] / "agents"
READ_ONLY = ["build-critic", "spec-critic", "arbiter"]
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


@pytest.mark.parametrize("name", ["build-critic", "spec-critic"])
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


def test_planner_documents_the_runnable_field() -> None:
    _, body = _fm("build-planner")
    assert '"runnable": true' in body and "commands.run" in body
