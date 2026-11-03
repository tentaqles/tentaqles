# plugin/tests/test_classify_data_skill.py
"""Content checks for the classify-data skill (writes docs/privacy.md, never the manifest)."""

from __future__ import annotations

from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_classify_data_requires_paths_or_tables() -> None:
    body = _body("classify-data")
    for token in ("docs/privacy.md", "templates/process/privacy.md", "references/data-dictionary.md", "Faker",
                  "legal basis", "DSAR", "retention", "authority", "`public`", "`internal`", "`pii`", "`sensitive`",
                  "`financial`", "`children`", "`secrets`", "schema.table", "_tqe="):
        assert token in body, token
    flat = " ".join(body.split())
    assert "at least one" in flat and "`path` or `table`" in flat
    assert "never" in body and "credential" in body
    assert "never writes one" in flat


def test_classify_data_rulings() -> None:
    body = _body("classify-data")
    flat = " ".join(body.split())
    for token in ("incident-playbook.md", "templates/process/incident-playbook.md",
                  "## Open decisions", "NEEDS-DECISION",
                  "shorter clock", "`authority: none`",
                  "schema or migration files",
                  "Never list `docs/privacy.md` itself"):
        assert token in flat, token
    assert "git add -A" not in body and "git add ." not in body
    for forbidden in (".tentaqles.yaml", "data:", "tq allow", "classified_paths", "tq-project", "tq doctor",
                      "privacy-manifest-proposal", "build-gate", "TENTAQLES_HEADLESS", "AskUserQuestion",
                      "tq/pii-fixture-dump", "prompts.allow", "MODEL CALL"):
        assert forbidden not in body, forbidden
    assert "headless" not in body.lower()


def test_classify_data_questions_and_commit() -> None:
    body = _body("classify-data")
    flat = " ".join(body.split())
    for token in ("Which countries are the data subjects in", "Who receives an incident notice",
                  "Never invent a contact", "do not write the class without one",
                  "Production rows never go into fixtures",
                  "stage only the files this run changed, named explicitly",
                  "docs(privacy): data classification and record of processing"):
        assert token in flat, token
