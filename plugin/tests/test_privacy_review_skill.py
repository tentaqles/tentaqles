# plugin/tests/test_privacy_review_skill.py
"""Content checks for the privacy-review skill (runs the reviewer on a diff, records nothing)."""

from __future__ import annotations

import re
from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_privacy_review_selection_rule_text() -> None:
    body = _body("privacy-review")
    for token in ("privacy-reviewer", "references/checklist.md", "docs/privacy.md", "/tentaqles:dod",
                  "e-mail, CPF, phone, address, birth date, card", "changes a retention"):
        assert token in body, token
    assert "only docs/privacy.md" in body or "documentation-only" in body
    for forbidden in ("record-review", "tq decide", "classified_data", "classified_paths", "data:", "build-gate",
                      "TENTAQLES_HEADLESS", "AskUserQuestion", "[checkpoint", "build-gate judge", "tq redact", "tq allow",
                      ".tentaqles.yaml"):
        assert forbidden not in body, forbidden
    assert "headless" not in body.lower()


def test_privacy_review_feeds_as_is_and_never_pre_redacts() -> None:
    body = _body("privacy-review")
    assert "as-is" in body and "do not pre-redact" in body
    assert "carries placeholders, never the values" in " ".join(body.split())


def test_privacy_review_reports_rows_and_routes_decisions() -> None:
    body = _body("privacy-review")
    flat = " ".join(body.split())
    assert "`RESULT: PASS`, `RESULT: FAIL` or `RESULT: BLOCKED`" in flat
    assert "## Open decisions" in body and "NEEDS-DECISION" in body
    assert "BUILT-BY-ME" in body
    assert "Never call a change privacy-reviewed in a PR body unless the reviewer returned `PASS`" in flat
    assert "git diff <a>..<b>" in body and "git diff HEAD" in body
    assert "matched a path or table of `docs/privacy.md`" in flat


def test_privacy_review_bootstrap_and_diff_file() -> None:
    body = _body("privacy-review")
    for block in re.findall(r"```bash\n(.*?)```", body, re.S):
        if "$TENTAQLES_PY" in block or "$_tqe" in block:
            assert block.lstrip().startswith(SNIPPET), block[:80]
    assert 'git -C "$root" diff --staged >' in body


GUARD = 'br=$(git -C "$root" branch --show-current); case "$br" in main|master|"") echo "on \'$br\': do not commit"; exit 1;; esac'


def test_privacy_review_commit_has_the_main_master_guard() -> None:
    body = _body("privacy-review")
    blocks = [b for b in re.findall(r"```bash\n(.*?)```", body, re.S) if " commit " in b]
    assert len(blocks) == 1
    assert GUARD in blocks[0] and blocks[0].index(GUARD) < blocks[0].index(" commit ")
    assert "git add -A" in body and "never `git add -A`" in body
    assert "never on main or master" in body and "uncommitted and say so" in body
