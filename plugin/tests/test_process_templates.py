"""The process templates ship with the plugin and keep the shapes the parsers rely on."""

from __future__ import annotations

import json
import re
from pathlib import Path

import pytest

TEMPLATES = Path(__file__).resolve().parents[1] / "templates" / "process"
NAMES = ["prd.md", "adr-0000-template.md", "spec-slice.md", "fix-spec.md",
         "LEARNINGS.md", "CLAUDE-project.md", "review-contract.md",
         "threat-model.md", "privacy.md", "incident-playbook.md"]


@pytest.mark.parametrize("name", NAMES)
def test_template_exists_and_is_not_empty(name: str) -> None:
    path = TEMPLATES / name
    assert path.is_file(), path
    assert len(path.read_text(encoding="utf-8").strip()) > 200


def test_prd_has_eleven_numbered_sections_and_fr_shape() -> None:
    text = (TEMPLATES / "prd.md").read_text(encoding="utf-8")
    heads = re.findall(r"^## (\d+)\. ", text, flags=re.MULTILINE)
    assert heads == [str(i) for i in range(1, 12)], heads
    assert "### FR-01 " in text
    assert "**Acceptance checks**" in text
    assert "NEEDS-DECISION" in text
    assert "### 3.2 Non-goals" in text


def test_claude_scaffold_has_fixed_sections_and_valid_block() -> None:
    text = (TEMPLATES / "CLAUDE-project.md").read_text(encoding="utf-8")
    for h in ("## Reading path", "## Must not trade off", "## Project config"):
        assert h in text, h
    m = re.search(r"^```json tq-project\n(.*?)\n```", text, flags=re.MULTILINE | re.DOTALL)
    assert m, "scaffold must carry a ```json tq-project fenced block"
    cfg = json.loads(m.group(1))
    assert set(cfg) >= {"commands", "architecture_paths", "changelog"}
    assert "test" in cfg["commands"]


def test_learnings_has_rules_and_log_with_row_format() -> None:
    text = (TEMPLATES / "LEARNINGS.md").read_text(encoding="utf-8")
    assert "## Rules" in text and "## Log" in text
    assert "L<n> · <date> · <slug>#<cp> · <gate> ·" in text
    assert "D<n> · <date> · <slug> · dod-override ·" in text


def test_review_contract_defines_the_verdict_grammar() -> None:
    text = (TEMPLATES / "review-contract.md").read_text(encoding="utf-8")
    for token in ("RESULT: PASS", "RESULT: FAIL", "RESULT: BLOCKED", "CANNOT JUDGE", "BUILT-BY-ME:"):
        assert token in text, token


def test_threat_model_has_four_questions_and_stride_table() -> None:
    text = (TEMPLATES / "threat-model.md").read_text(encoding="utf-8")
    for q in ("What are we building", "What can go wrong", "What will we do about it", "Did we do a good job"):
        assert q in text, q
    for col in ("Spoofing", "Tampering", "Repudiation", "Information disclosure", "Denial of service", "Elevation of privilege"):
        assert col in text, col
    assert "| Category | Threat | Mitigation | Status | Owner |" in text
    assert not re.search(r"^\| Threat \|", text, flags=re.MULTILINE)
    assert "B1 <from> -> <to>" in text and "superseded" in text
    assert "Accepted risks" in text and "expires" in text


def test_privacy_record_has_legal_bases_clocks_and_dsar() -> None:
    text = (TEMPLATES / "privacy.md").read_text(encoding="utf-8")
    for token in ("LGPD Art. 7", "LGPD Art. 11", "GDPR Art. 6", "GDPR Art. 9", "15 days", "one month",
                  "3 business days", "72 hours", "DPIA", "RIPD", "five years", "deletion job", "## Processing activities",
                  "## Open decisions", "NEEDS-DECISION"):
        assert token in text, token


def test_incident_playbook_names_both_clocks() -> None:
    text = (TEMPLATES / "incident-playbook.md").read_text(encoding="utf-8")
    for token in ("Detect", "Contain", "Assess", "Notify", "Record", "ANPD", "3 business days", "72 hours", "shorter clock",
                  "five years", "never rotate"):
        assert token in text, token
