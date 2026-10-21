# plugin/tests/test_security_skills.py
"""Content checks for the security, privacy and LLM-security skills."""

from __future__ import annotations

from pathlib import Path

import pytest

SKILLS = Path(__file__).resolve().parents[1] / "skills"


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


CHECKLISTS = {
    "auth-security/references/asvs-checklist.md": ("S1", "ASVS", "V6", "V8", "A03", "A10"),
    "privacy-review/references/checklist.md": ("P1", "LGPD Art. 6", "Art. 18", "Art. 37", "Art. 46", "GDPR Art. 5", "Art. 25", "Art. 30", "Art. 32"),
    "llm-security-review/references/checklist.md": ("L1", "LLM01", "LLM02", "LLM06", "ASI01", "ASI", "MCP", "memory"),
    "supply-chain/references/checklist.md": ("C1", "SLSA", "SBOM", "permissions:", "lockfile", "Scorecard", "secret scanning"),
}

ROW_IDS = {
    "auth-security/references/asvs-checklist.md": [f"S{i}" for i in range(1, 13)],
    "privacy-review/references/checklist.md": [f"P{i}" for i in range(1, 12)],
    "llm-security-review/references/checklist.md": [f"L{i}" for i in range(1, 13)],
    "supply-chain/references/checklist.md": [f"C{i}" for i in range(1, 11)],
}


@pytest.mark.parametrize("rel,tokens", CHECKLISTS.items(), ids=lambda x: x if isinstance(x, str) else "")
def test_checklists_exist_with_stable_ids(rel: str, tokens: tuple) -> None:
    path = SKILLS / rel
    assert path.is_file(), path
    text = path.read_text(encoding="utf-8")
    for tok in tokens:
        assert tok in text, (rel, tok)


@pytest.mark.parametrize("rel,ids", ROW_IDS.items(), ids=list(ROW_IDS))
def test_checklist_rows_present_once(rel: str, ids: list) -> None:
    text = (SKILLS / rel).read_text(encoding="utf-8")
    for rid in ids:
        assert text.count(f"| {rid} |") == 1, (rel, rid)
