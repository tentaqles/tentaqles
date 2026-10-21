# plugin/tests/test_kickoff_skill.py
"""Content checks for the kickoff skill (spine task 12)."""

from __future__ import annotations

from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_kickoff_requires_a_planning_ready_prd_and_scaffolds_everything() -> None:
    body = _body("kickoff")
    for token in ("prd-status.py", "prd_exit", "/tentaqles:prd", "docs/adr/0001-stack-and-layout.md",
                  "templates/process/CLAUDE-project.md", "/tentaqles:threat-model", "/tentaqles:classify-data",
                  "tq allow", "/tentaqles:ci-pipeline", "/tentaqles:supply-chain", "tq settings render",
                  ".gitignore", "workflow=kickoff", "/tentaqles:direction", "Must not trade off"):
        assert token in body, token
    assert SNIPPET in body


def test_kickoff_gate_branch_headless_and_planner_fields() -> None:
    body = _body("kickoff")
    assert "only `prd_exit=0`" in body and "prd_exit=3" in body and "prd_exit=4" in body
    assert "You never commit on `main` or `master`" in body and "/tentaqles:build` Phase 0" in body
    assert "TENTAQLES_HEADLESS=1" in body and "--headless" in body and "build-gate ask <slug>" in body
    assert '"workflow": "kickoff"' in body and '"hotfix": false' in body and "spec=<path>" in body
    assert "you never run `build-gate dod <slug> --override`" in body
    assert "lock-tests" in body
    assert "git add -A" not in body.replace("`git add -A`", "")
    assert "If that skill is not installed" in body and "NEEDS-DECISION" in body


def test_kickoff_branches_before_the_prd_and_records_pending_items() -> None:
    body = _body("kickoff")
    assert body.index("**The branch.**") < body.index("prd-status.py\" --cwd")
    assert "`/tentaqles:prd`\ncommits too" in body or "/tentaqles:prd` included" in body
    for skill in ("threat-model", "classify-data", "supply-chain"):
        assert f"(settle by: run /tentaqles:{skill} when installed)" in body, skill
    assert "`docs/prd.md` §10" in body and "settle or explicitly accept before approving" in body
    assert "`AskUserQuestion` is never called" in body
    assert "/tentaqles:direction <slug>" in body and "--kind result" in body
    assert "`tq status`" in body
