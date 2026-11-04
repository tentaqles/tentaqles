# plugin/tests/test_kickoff_skill.py
"""Content checks for the kickoff skill (a plain checklist that hands the first slice to feature)."""

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
                  "/tentaqles:ci-pipeline", "/tentaqles:supply-chain", "tq settings render",
                  ".gitignore", "/tentaqles:feature <FR-nn>", "/tentaqles:direction", "Must not trade off",
                  "**Checks** line", "docs/privacy.md"):
        assert token in body, token
    assert SNIPPET in body


def test_kickoff_gate_branch_and_stops() -> None:
    body = _body("kickoff")
    assert "only `prd_exit=0`" in body and "prd_exit=3" in body and "prd_exit=4" in body
    assert "You never commit on `main` or `master`" in body and "`git switch -c feat/<slug>`" in body
    assert "**Human stop 1**" in body and "**stop 2**" in body
    assert "git add -A" not in body.replace("`git add -A`", "")
    assert "If that skill is not installed" in body and "NEEDS-DECISION" in body
    for forbidden in ("build-gate", "TENTAQLES_HEADLESS", "--headless", "workflow=kickoff", "lock-tests",
                      "/tentaqles:build", "checkpoint", "tq-project", "classified_paths", "tq pack", "/tentaqles:dod <slug>"):
        assert forbidden not in body, forbidden
    assert "headless" not in body.lower()


def test_kickoff_branches_before_the_prd_and_records_pending_items() -> None:
    body = _body("kickoff")
    assert body.index("**The branch.**") < body.index("prd-status.py\" --cwd")
    assert "/tentaqles:prd` included" in body
    for skill in ("threat-model", "classify-data", "supply-chain"):
        assert f"(settle by: run /tentaqles:{skill} when installed)" in body, skill
    assert "`docs/prd.md` §10" in body and "settle or explicitly accept before approving" in body
    assert "`AskUserQuestion`" in body  # stop 1 is one question to the human
    assert "`tq status`" in body
    assert "you never run it" in body  # tq allow stays the human's
