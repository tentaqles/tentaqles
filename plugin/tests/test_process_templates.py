"""The process templates ship with the plugin and keep the shapes the parsers rely on."""

from __future__ import annotations

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


def test_claude_scaffold_has_fixed_sections_and_a_checks_line() -> None:
    text = (TEMPLATES / "CLAUDE-project.md").read_text(encoding="utf-8")
    for h in ("## Reading path", "## Must not trade off", "## Project config"):
        assert h in text, h
    assert "```json tq-project" not in text, "the tq-project block is gone (plugin 0.16.0)"
    assert re.search(r"^- Checks: test `[^`]+`; lint `[^`]+`; typecheck `[^`]+`$", text, flags=re.MULTILINE)
    assert "- Architecture paths" in text and "docs/adr/" in text
    assert "- Changelog:" in text
    assert "breaker" not in text and "classified_paths" not in text and "manifest" not in text


def test_learnings_has_rules_and_log_with_row_format() -> None:
    text = (TEMPLATES / "LEARNINGS.md").read_text(encoding="utf-8")
    assert "## Rules" in text and "## Log" in text
    assert "L<n> · <date> · <slug> · failure:" in text
    assert "H<n> · <date> · <slug> · sent back ·" in text
    assert "dod-override" not in text and "<gate>" not in text and "stop 2" not in text


def test_review_contract_defines_the_verdict_grammar() -> None:
    text = (TEMPLATES / "review-contract.md").read_text(encoding="utf-8")
    for token in ("RESULT: PASS", "RESULT: FAIL", "RESULT: BLOCKED", "CANNOT JUDGE", "BUILT-BY-ME:"):
        assert token in text, token
    assert "build-gate" not in text and "orchestrator" not in text


@pytest.mark.parametrize("name", NAMES)
def test_templates_carry_no_gate_or_manifest_data_wording(name: str) -> None:
    text = (TEMPLATES / name).read_text(encoding="utf-8")
    for forbidden in ("build-gate", "lock-tests", "manifest `data:`", "data.incident", "data.regions", "tq redact",
                      "checkpoint", "breaker"):
        assert forbidden not in text, (name, forbidden)


def test_threat_model_has_four_questions_and_stride_table() -> None:
    text = (TEMPLATES / "threat-model.md").read_text(encoding="utf-8")
    for q in ("What are we building", "What can go wrong", "What will we do about it", "Did we do a good job"):
        assert q in text, q
    for col in ("Spoofing", "Tampering", "Repudiation", "Information disclosure", "Denial of service", "Elevation of privilege"):
        assert col in text, col
    assert "| Category | Threat | Mitigation | Status | Owner |" in text
    assert not re.search(r"^\| Threat \|", text, flags=re.MULTILINE)
    assert "B1 <from> -> <to>" in text and "superseded" in text
    assert "Document status" in text and "Row status" in text and "file:line or test name" in text
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
                  "five years", "never rotate",
                  "never personal data or secrets", "Processors", "notifying affected subjects"):
        assert token in text, token
def test_fix_spec_summarises_the_report_without_personal_data() -> None:
    # final review I3: docs/specs/fix-<slug>.md is committed; a raw ticket or stack trace may carry PII
    text = (TEMPLATES / "fix-spec.md").read_text(encoding="utf-8")
    flat = " ".join(text.split())
    assert "exactly as received" not in text
    assert "## Report (summary, personal data replaced)" in text
    assert ("Summarise the report, stack trace or failing command, keeping its meaning, with personal data "
            "replaced by placeholders (`<customer>`, `<email>`, `<row id>`); never paste it verbatim.") in flat
    assert "Leave the original where it came from" in flat
    assert "the fix never changes it" in flat and "locked" not in text.lower()
