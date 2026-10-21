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


def test_privacy_review_fix_round_1_rules() -> None:
    body = _body("privacy-review")
    assert "do not pre-redact" in body
    assert "remind the user to run `tq allow <workspace>`" in body
    assert "tq allow`)" not in body
    assert "[checkpoint <slug>#<id> review]" in body
    assert "BUILT-BY-ME" in body
    assert "git add -A" in body and "never `git add -A`" in body
    assert "git diff <a>..<b>" in body and "git diff HEAD" in body
    assert "matched" in body and "classified_paths" in body
    assert "judge flow" in body


GUARD = 'br=$(git -C "$root" branch --show-current); case "$br" in main|master|"") echo "on \'$br\': do not commit"; exit 1;; esac'


def test_privacy_review_commit_has_the_main_master_guard() -> None:
    import re
    body = _body("privacy-review")
    blocks = [b for b in re.findall(r"```bash\n(.*?)```", body, re.S) if " commit " in b]
    assert len(blocks) == 1
    assert GUARD in blocks[0] and blocks[0].index(GUARD) < blocks[0].index(" commit ")
    assert "never on main or master" in body and "uncommitted and say so" in body
