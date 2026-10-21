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


@pytest.mark.parametrize("name", ["build-critic", "spec-critic", "security-reviewer", "privacy-reviewer", "llm-security-reviewer", "breaker"])
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
        "third-party keys", "back up and restore", "--max-time", "stop the process", "timeout", "starts with `<`", "::1", "taskkill",
        "PGCONNECT_TIMEOUT", "RESTORED: no",
    ):
        assert token in body, token
    assert "RESULT: PASS | FAIL | BLOCKED" in body
    disallowed = {t.strip() for t in fm["disallowedTools"].split(",")}
    assert {"Edit", "Write", "MultiEdit", "NotebookEdit", "PowerShell", "Agent"} <= disallowed


@pytest.mark.parametrize("name,ref", [
    ("security-reviewer", "auth-security/references/asvs-checklist.md"),
    ("privacy-reviewer", "privacy-review/references/checklist.md"),
    ("llm-security-reviewer", "llm-security-review/references/checklist.md"),
])
def test_reviewers_name_their_checklist(name: str, ref: str) -> None:
    _, body = _fm(name)
    assert ref in body


def test_breaker_exit_sequence_is_safe_when_the_app_will_not_stop() -> None:
    _, body = _fm("breaker")
    for token in (
        "setsid", "kill -- -", "pkill -P", "winpid", "//T //F", "MSYS",
        "-v ON_ERROR_STOP=1", "port stays open", "restore while the app runs",
        "RESTORED: no — <backup path> — <manual restore command>",
        "must not be used to override the data store decision",
    ):
        assert token in body, token
    # every scratch path is spelled out under the breaker directory
    assert not re.search(r"cat app\.pid|-f backup\.sql|\$\(cat app", body)
    assert ".tq-breaker/<slug>/app.pid" in body
    # the scratch directory lives outside the gate's state directory (fix round 6)
    assert ".claude/build/<slug>/breaker" not in body
    for token in (".tq-breaker/<slug>/backup.sql", ".tq-breaker/<slug>/app.log"):
        assert token in body, token


@pytest.mark.parametrize("name,tokens", [
    ("security-reviewer", ("cite the file and line, never the secret value",)),
    ("llm-security-reviewer", ("model-driven HTTP", "markdown", "image URL")),
])
def test_reviewers_keep_values_out_and_name_model_exits(name: str, tokens: tuple) -> None:
    _, body = _fm(name)
    for token in tokens:
        assert token in body, (name, token)


def test_breaker_writes_new_scratch_files_instead_of_editing_in_place() -> None:
    _, body = _fm("breaker")
    assert "never edit a scratch file in place" in body
    assert "write a new file instead" in body
    assert "only new files" in body
    assert "cp`, `mv` or `install`" not in body  # the carve-out's copier allowlist is gone
