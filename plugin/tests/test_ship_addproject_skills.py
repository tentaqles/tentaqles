# plugin/tests/test_ship_addproject_skills.py
"""Content checks for the add-project and ship skills (plain checklists, self-assessed risk)."""

from __future__ import annotations

from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_add_project_scaffolds_the_fixed_sections_and_points_to_the_process_skills() -> None:
    body = _body("add-project")
    for token in ("templates/process/CLAUDE-project.md", "## Reading path", "## Must not trade off", "## Project config",
                  "docs/LEARNINGS.md", "/tentaqles:kickoff", "/tentaqles:feature", "/tentaqles:fix", "**Checks** line"):
        assert token in body, token
    assert "tq-project" not in body and "commands.test" not in body


def test_ship_rates_risk_itself_and_runs_the_reviewers_on_a_high_diff() -> None:
    body = _body("ship")
    flat = " ".join(body.split())
    for token in ("## 4. Risk (self-assessed)", "`low` or `high` yourself", "git diff --staged > <file>",
                  "`critic` always", "`security-reviewer`", "`privacy-reviewer`", "`llm-security-reviewer`",
                  "`RESULT: PASS`", "`U` row"):
        assert token in flat, token
    for forbidden in ("tq decide", "triage", "build-gate", "TQ_HUB_TASK", "tq hub", "tq pack", "dod.json",
                      "TENTAQLES_HEADLESS", "AskUserQuestion", "tq/text-pii", "--override"):
        assert forbidden not in body, forbidden
    assert "headless" not in body.lower()


def test_ship_pr_body_has_summary_risk_verification_notes() -> None:
    body = _body("ship")
    for token in ("## Summary", "## Risk", "## Verification", "## Notes", "<low | high>", "CI: pending",
                  "Definition of done:", "400", "git diff --staged --shortstat", "/tentaqles:dod", "babysit-pr"):
        assert token in body, token
    assert body.index("## Summary") < body.index("## Risk") < body.index("## Verification") < body.index("## Notes")
    assert "## Definition of done" not in body  # it is a line under Verification, not a section
    assert "--admin" not in body.replace("never `gh pr merge --admin`", "")


def test_ship_keeps_personal_data_and_secrets_out_of_the_pr_body() -> None:
    flat = " ".join(_body("ship").split())
    assert "The PR body never contains personal data or secrets" in flat
    assert "never the value" in flat and "only by its last line" in flat
    assert "tq dotenv run" in flat and "Never use `--no-verify`" in flat
