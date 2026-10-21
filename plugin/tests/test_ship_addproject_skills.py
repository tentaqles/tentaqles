# plugin/tests/test_ship_addproject_skills.py
"""Content checks for the add-project and ship skill changes (spine task 13)."""

from __future__ import annotations

from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_add_project_scaffolds_the_fixed_sections_and_points_to_kickoff() -> None:
    body = _body("add-project")
    for token in ("templates/process/CLAUDE-project.md", "## Reading path", "## Must not trade off", "## Project config",
                  "docs/LEARNINGS.md", "/tentaqles:kickoff"):
        assert token in body, token


def test_ship_quotes_the_dod_record_and_warns_on_large_diffs() -> None:
    body = _body("ship")
    for token in ("dod.json", "Definition of done", "400", "git diff --staged --shortstat"):
        assert token in body, token


def test_ship_never_overrides_the_dod_and_guards_pii() -> None:
    body = " ".join(_body("ship").split())
    assert "/tentaqles:dod" in body
    assert body.count("dod <slug> --override") == 1  # only the prohibition below names it
    assert "tq/text-pii" in body
    assert "never call `AskUserQuestion`" in body
    assert "you never run `build-gate dod <slug> --override`" in body
    assert "--admin" not in body.replace("never `gh pr merge --admin`", "")
