"""Agent definitions: the five read-only reviewers, their frontmatter, tools and the review contract."""

from __future__ import annotations

from pathlib import Path

import pytest
import yaml

AGENTS = Path(__file__).resolve().parents[1] / "agents"
READ_ONLY = ["critic", "spec-critic", "security-reviewer", "privacy-reviewer", "llm-security-reviewer"]
WRITE_TOOLS = {"Edit", "Write", "MultiEdit", "NotebookEdit", "Bash", "PowerShell"}


def _fm(name: str) -> tuple[dict, str]:
    text = (AGENTS / f"{name}.md").read_text(encoding="utf-8")
    assert text.startswith("---\n")
    end = text.index("\n---\n", 4)
    return yaml.safe_load(text[4:end]), text[end + 5:]


def test_agent_set_is_exactly_the_five_reviewers() -> None:
    assert sorted(p.stem for p in AGENTS.glob("*.md")) == sorted(READ_ONLY)


@pytest.mark.parametrize("name", READ_ONLY)
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


@pytest.mark.parametrize("name", READ_ONLY)
def test_reviewers_return_the_contract(name: str) -> None:
    _, body = _fm(name)
    for token in ("RESULT: PASS | FAIL | BLOCKED", "BUILT-BY-ME:", "CANNOT JUDGE", "file:line"):
        assert token in body, (name, token)
    assert "VERDICT:" not in body


@pytest.mark.parametrize("name", READ_ONLY)
def test_reviewers_read_the_diff_the_caller_names(name: str) -> None:
    fm, body = _fm(name)
    text = fm["description"] + body
    assert "git diff <base>...HEAD" in body, name
    for token in ("spec slice", "`CLAUDE.md`", "`docs/LEARNINGS.md`", "`docs/threat-model.md`"):
        assert token in body, (name, token)
    for forbidden in ("review bundle", "checkpoint", "triage", "/tentaqles:build", "manifest `data:`", "data block",
                      "build-gate", ".claude/build", "tq redact", "prompts.allow", "orchestrator"):
        assert forbidden not in text, (name, forbidden)


def test_critic_is_the_renamed_quality_reviewer() -> None:
    fm, body = _fm("critic")
    assert fm["name"] == "critic"
    assert "quality" in fm["description"].lower() and "read-only" in fm["description"].lower()
    assert "the caller then runs a fresh critic" in body


@pytest.mark.parametrize("name,ref", [
    ("security-reviewer", "auth-security/references/asvs-checklist.md"),
    ("privacy-reviewer", "privacy-review/references/checklist.md"),
    ("llm-security-reviewer", "llm-security-review/references/checklist.md"),
])
def test_reviewers_name_their_checklist(name: str, ref: str) -> None:
    _, body = _fm(name)
    assert ref in body


@pytest.mark.parametrize("name,tokens", [
    ("security-reviewer", ("cite the file and line, never the secret value",)),
    ("llm-security-reviewer", ("model-driven HTTP", "markdown", "image URL")),
    ("privacy-reviewer", ("docs/privacy.md", "Never copy personal-data values into your findings")),
])
def test_reviewers_keep_values_out_and_name_model_exits(name: str, tokens: tuple) -> None:
    _, body = _fm(name)
    for token in tokens:
        assert token in body, (name, token)
