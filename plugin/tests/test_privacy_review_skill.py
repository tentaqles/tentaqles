# plugin/tests/test_privacy_review_skill.py
"""Content checks for the privacy-review skill."""

from __future__ import annotations

from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_privacy_review_selection_rule_text() -> None:
    body = _body("privacy-review")
    for token in ("privacy-reviewer", "references/checklist.md", "classified_paths", "data:", "docs/privacy.md",
                  "record-review", "tq decide triage", "classified_data"):
        assert token in body, token
    assert "only docs/privacy.md" in body or "documentation-only" in body


def test_privacy_review_feeds_as_is_and_never_pre_redacts() -> None:
    body = _body("privacy-review")
    assert "tq redact --classes all" not in body
    assert "as-is" in body
    assert "tq redact" in body  # only for copies that leave the session


def test_privacy_review_records_and_routes_decisions() -> None:
    body = _body("privacy-review")
    assert "--reviewer privacy-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N" in body
    assert "## Open decisions" in body and "NEEDS-DECISION" in body
    assert "no build slug" in body.lower() or "standalone" in body.lower()


def test_privacy_review_headless_and_bootstrap() -> None:
    body = _body("privacy-review")
    assert "TENTAQLES_HEADLESS" in body and "build-gate ask" in body
    assert "never" in body and "AskUserQuestion" in body
    import re
    for block in re.findall(r"```bash\n(.*?)```", body, re.S):
        if "$TENTAQLES_PY" in block or "$_tqe" in block:
            assert block.lstrip().startswith(SNIPPET), block[:80]
