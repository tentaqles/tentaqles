# plugin/tests/test_classify_data_skill.py
"""Content checks for the classify-data skill."""

from __future__ import annotations

from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_classify_data_requires_paths_or_tables() -> None:
    body = _body("classify-data")
    for token in ("data:", "classes:", "paths:", "table:", "retention_default", "incident:", "authority", "tq allow",
                  "classified_paths", "templates/process/privacy.md", "references/data-dictionary.md", "Faker",
                  "tq doctor", "legal basis", "DSAR"):
        assert token in body, token
    assert "at least one" in body and ("path" in body and "table" in body)
    assert "never" in body and "credential" in body


def test_classify_data_rulings() -> None:
    body = _body("classify-data")
    for token in ("incident-playbook.md", "templates/process/incident-playbook.md",  # F5
                  "## Open decisions", "NEEDS-DECISION",  # F6
                  "TENTAQLES_HEADLESS", "AskUserQuestion", "build-gate ask",  # F17
                  "MODEL CALL", "never STORES personal data unmasked",  # F20
                  "shorter clock", "data.regions",  # F4
                  "schema or migration",  # F21
                  "Never add",  # F22
                  "tq-project", "_tqe="):  # bootstrap, mirror
        assert token in body, token
    assert "git add -A" not in body and "git add ." not in body


def test_classify_data_fix_round_1() -> None:
    body = _body("classify-data")
    for token in ("Which countries are the data subjects in", "Who receives an incident notice",
                  "never invent a contact", "do not write the block",
                  "pii, sensitive, financial, children and secrets",
                  "JSON array", "create the key when it is missing",
                  "`incident.authority: none`",
                  "stage only the files this\nrun changed, named explicitly"):
        assert token in body, token
