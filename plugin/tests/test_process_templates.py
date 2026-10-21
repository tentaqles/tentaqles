"""The process templates ship with the plugin and keep the shapes the parsers rely on."""

from __future__ import annotations

import json
import re
from pathlib import Path

import pytest

TEMPLATES = Path(__file__).resolve().parents[1] / "templates" / "process"
NAMES = ["prd.md", "adr-0000-template.md", "spec-slice.md", "fix-spec.md",
         "LEARNINGS.md", "CLAUDE-project.md", "review-contract.md"]


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


def test_fix_spec_summarises_the_report_without_personal_data() -> None:
    # final review I3: docs/specs/fix-<slug>.md is committed; a raw ticket or stack trace may carry PII
    text = (TEMPLATES / "fix-spec.md").read_text(encoding="utf-8")
    flat = " ".join(text.split())
    assert "exactly as received" not in text
    assert "## Report (summary, personal data replaced)" in text
    assert ("Summarise the report, stack trace or failing command, keeping its meaning, with personal data "
            "replaced by placeholders (`<customer>`, `<email>`, `<row id>`); never paste it verbatim.") in flat
    assert "Leave the original where it came from" in flat
