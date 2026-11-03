# plugin/tests/test_process_skills.py
"""Content checks for the development-process skills: plain checklists, no build gate."""

from __future__ import annotations

import re
from pathlib import Path

import pytest

SKILLS = Path(__file__).resolve().parents[1] / "skills"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'
GATE_WORDS = ("build-gate", "tq decide", "tq pack", "tq hub", "TQ_HUB_TASK", "TENTAQLES_HEADLESS", "--headless",
              "checkpoint", "lock-tests", "record-review", "/tentaqles:build", "skills/build/")

PROCESS = ["ship", "fix", "feature", "kickoff", "direction", "dod", "llm-security-review", "privacy-review",
           "classify-data", "prd", "threat-model", "supply-chain", "grill-me", "add-project"]


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def _flat(name: str) -> str:
    return " ".join(_body(name).split())


@pytest.mark.parametrize("name", PROCESS)
def test_process_skills_name_no_gate_hub_pack_or_headless(name: str) -> None:
    body = _body(name)
    for word in GATE_WORDS:
        assert word not in body, (name, word)
    assert "headless" not in body.lower(), name


REWRITTEN = ["ship", "fix", "feature", "kickoff", "direction", "dod", "llm-security-review", "privacy-review",
             "classify-data"]


@pytest.mark.parametrize("name", REWRITTEN)
def test_python_blocks_start_with_the_bootstrap_snippet(name: str) -> None:
    for block in re.findall(r"```bash\n(.*?)```", _body(name), re.S):
        if "$TENTAQLES_PY" in block:
            code = "\n".join(l for l in block.splitlines() if l.strip() and not l.lstrip().startswith("#"))
            assert code.lstrip().startswith(SNIPPET), (name, block[:80])


def test_dod_is_a_checklist_with_no_override() -> None:
    body = _body("dod")
    flat = _flat("dod")
    assert SNIPPET in body
    for item in ("`tests`", "`lint`", "`typecheck`", "`adr`", "`privacy`", "`migration`", "`changelog`", "`prd-decisions`"):
        assert item in body, item
    for token in ("**Checks** line", "docs/adr/NNNN-<slug>.md", "docs/privacy.md", "## Migration plan", "DoD <branch>: PASS | FAIL",
                  "never", "transcript", "/tentaqles:privacy-review", "/tentaqles:ship"):
        assert token in body, token
    assert "`PASS` only when no row says `MISSING`" in flat
    assert "--override" not in body and "dod.json" not in body and "AskUserQuestion" not in body
    assert "edit a test to turn the `tests` row green" in flat


def test_direction_is_read_only_and_uses_the_scorecard() -> None:
    body = _body("direction")
    for token in ("ON COURSE", "DRIFTING", "OFF COURSE", "Aligned", "Partial", "Missing", "Contradicted",
                  "Not verified", "Fix code", "Update PRD", "Decide", "prd-status.py"):
        assert token in body, token
    for forbidden in ("git commit", "Edit", "Write(", "build-slug", "AskUserQuestion"):
        assert forbidden not in body, forbidden
    assert SNIPPET in body


def test_prd_skill_interviews_and_checks_mechanically() -> None:
    body = _body("prd")
    for token in ("templates/process/prd.md", "prd-status.py", "NEEDS-DECISION", "you decide", "from existing code",
                  "planning-ready"):
        assert token in body, token
    assert SNIPPET in body
    assert "the grader's exit code decides" in body
    assert "- NEEDS-DECISION: <question>" in body and "Numbered items and bare" in body
    assert "contiguous" in body and "Any other exit code: report it" in body
    assert "AskUserQuestion" not in body


def test_fix_skill_is_reproduce_first_and_bug_shaped() -> None:
    fm_text, body = _body("fix").split("\n---\n", 1)
    desc = fm_text.lower()
    for phrase in ("stack trace", "bug", "fails", "regression"):
        assert phrase in desc, phrase
    for token in ("templates/process/fix-spec.md", "systematic", "LEARNINGS", "incident", "fix/<slug>",
                  "security-reviewer", "privacy-reviewer", "llm-security-reviewer", "spec-critic", "critic",
                  "git diff <base>...HEAD", "/tentaqles:dod", "/tentaqles:ship"):
        assert token in body, token
    flat = " ".join(body.split())
    assert "never" in body and "without a reproduction" in body
    assert "No reproduction, no fix" in flat
    assert "The fix never edits the regression test" in flat
    assert "Commit the test on its own" in flat
    assert "fix without a failing reproduction, or edit the regression test to make it pass" in flat
    assert "--hotfix" not in body and "hotfix" not in body.lower()
    assert "AskUserQuestion" not in body


def test_fix_skill_never_pastes_reports_with_personal_data() -> None:
    body = _body("fix")
    flat = " ".join(body.split())
    assert "the report **verbatim**" not in body and "Captures the report verbatim" not in body
    assert "personal data replaced by placeholders (`<customer>`, `<email>`)" in flat
    assert "never pasted verbatim" in flat
    assert "the slug comes from the symptom, never from a name, e-mail or id" in flat


def test_feature_skill_locates_the_fr_and_interviews_first() -> None:
    body = _body("feature")
    for token in ("prd-status.py", "FR-", "grill-me", "templates/process/spec-slice.md", "NEEDS-DECISION",
                  "/tentaqles:dod", "/tentaqles:ship", "legacy", "docs/adr/", "git diff <base>...HEAD"):
        assert token in body, token
    assert SNIPPET in body
    flat = " ".join(body.split())
    assert "`prd_exit` 0 or 3 (a PRD exists): continue" in flat and "touch this feature" in flat
    assert "keeps the strict planning-ready gate" in flat and "Any other exit code: report it" in flat
    assert "A feature with no FR is first a PRD change, never code" in flat
    assert "`**Acceptance checks**` heading" in flat and "never guess" in flat
    assert "never commit on `main` or `master`" in flat and "`git switch -c feat/<slug>`" in flat
    assert '"The feature branch" below means this branch.' in flat
    assert "names this FR or its goal" in flat and "treat it as touching" in flat
    assert "never write a placeholder as a command" in flat and "leave no `<...>`" in flat
    assert "**Checks** line" in flat and "tq-project" not in body
    for crew in ("spec-critic", "security-reviewer", "privacy-reviewer", "llm-security-reviewer", "critic"):
        assert crew in body, crew
    assert "breaker" not in body
    assert "derived_from: no PRD: legacy repository, see kickoff later" in flat
    assert "write code for a feature that has no FR in `docs/prd.md`, or no committed spec slice" in flat
    assert "Status: draft" in flat and "amend the FR" in flat and "Status: implemented" in flat
    assert "Tests first, then the code" in flat and "see it fail" in flat
    assert "mark an acceptance check done without a test you ran and saw fail first" in flat
    assert "**Human stop:**" in body and body.count("**Human stop:**") == 1
    assert "AskUserQuestion" not in body


def test_feature_fix_and_ship_hand_the_reviewers_the_diff_and_the_spec() -> None:
    for name in ("feature", "fix", "ship"):
        flat = _flat(name)
        assert "`CLAUDE.md`, `docs/LEARNINGS.md` and `docs/threat-model.md`" in flat, name
        assert "`RESULT: PASS`" in flat, name
        assert "`U` row" in flat, name


def test_llm_security_review_feeds_the_reviewer_and_records_nothing() -> None:
    body = _body("llm-security-review")
    flat = _flat("llm-security-review")
    for token in ("llm-security-reviewer", "references/checklist.md", "MCP", "hooks", "tentaqles itself",
                  "RESULT: PASS", "tool and MCP definitions", "model-call site", "--range", "docs/privacy.md"):
        assert token in body, token
    assert SNIPPET in body
    assert "Nothing is recorded by this skill" in flat
    assert "hooks must fail closed and never widen permissions" in flat
    assert "prompts.allow" not in body and "manifest" not in body and "triage" not in body
    assert "judge" not in body.lower()


def test_classify_data_writes_the_record_not_the_manifest() -> None:
    body = _body("classify-data")
    flat = _flat("classify-data")
    for token in ("docs/privacy.md", "templates/process/privacy.md", "references/data-dictionary.md", "Faker",
                  "legal basis", "DSAR", "incident-playbook.md", "templates/process/incident-playbook.md",
                  "## Open decisions", "NEEDS-DECISION", "shorter clock", "`authority: none`",
                  "Which countries are the data subjects in", "Who receives an incident notice",
                  "Never invent a contact", "pii", "sensitive", "financial", "children", "secrets"):
        assert token in body, token
    assert SNIPPET in body
    assert "at least one" in flat and "`path` or `table`" in flat
    assert "never a credential" in flat
    assert "Never list `docs/privacy.md` itself" in flat
    assert "Production rows never go into fixtures" in flat
    for forbidden in (".tentaqles.yaml", "data:", "tq allow", "classified_paths", "tq-project", "tq doctor",
                      "privacy-manifest-proposal", "AskUserQuestion"):
        assert forbidden not in body, forbidden
    assert "git add -A" not in body and "git add ." not in body
    assert "stage only the files this\nrun changed, named explicitly" in body
