# plugin/tests/test_process_skills.py
"""Content checks for the development-process skills (spine)."""

from __future__ import annotations

import re
from pathlib import Path

import pytest

SKILLS = Path(__file__).resolve().parents[1] / "skills"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_build_runs_a_crew_and_records_every_verdict() -> None:
    body = _body("build")
    for token in ("record-review", "--reviewer build-critic --result", "spec-critic", "build-gate dod", "build-gate judge",
                  "BUILT-BY-ME", "workflow", "build-gate ask", "build-gate state", "## Headless mode", "TENTAQLES_HEADLESS"):
        assert token in body, token
    assert "git add -A" not in body and "git add ." not in body


def test_build_reference_lists_new_cli() -> None:
    ref = (SKILLS / "build" / "reference.md").read_text(encoding="utf-8")
    for token in ("build-gate record-review", "build-gate judge", "build-gate dod", "build-gate ask", "build-gate answer", "build-gate state", "reviews.jsonl", "dod.json", "decisions.jsonl", "events.jsonl", "TQ_BUILD_TOKEN"):
        assert token in ref, token


def test_build_dod_override_is_human_only() -> None:
    body = _body("build")
    ref = (SKILLS / "build" / "reference.md").read_text(encoding="utf-8")
    assert "--override" in ref
    assert 'dod <slug> --override "<their words>"' not in body
    assert "you never run `build-gate dod <slug> --override`" in body
    assert 'ask the human to run `build-gate dod <slug> --override "<their reason>"` in their own terminal' in body
    assert '--kind question --text "override:' in body and "--options override,stop" in body
    assert "build-gate answer <slug> <id> --choice override" in body
    assert "AskUserQuestion` is never called" in body
    assert "build-gate ask" in body.split("## Headless mode", 1)[1]


def test_build_decision_records_are_headless_only() -> None:
    body = _body("build")
    assert "In interactive mode you write no decision records" in body
    assert "you also write the `ask` record" not in body
    assert "First write the record" not in body
    assert body.count("--kind plan-approval") >= 4  # headless block, split, Request changes, re-approval rule
    assert "Log it as an H<n> line" not in body and "H<n> line in `docs/LEARNINGS.md`" in body


def test_dod_skill_runs_the_gate_and_explains_override() -> None:
    body = _body("dod")
    assert SNIPPET in body and "build-gate.py" in body and "dod" in body
    assert "--override" in body and "LEARNINGS" in body
    assert "never" in body.lower() and "transcript" in body.lower()
    assert "You never run `build-gate dod <slug> --override`" in body
    assert '--kind question --text "override:' in body and "--options override,stop" in body
    assert "the human runs the override" in body and "human runs it" not in body
    assert "blocks once" in body
    assert "AskUserQuestion` is never called" in body and "build-gate state <slug> --json" in body
    assert 'dod <slug> --override "<their words' not in body.replace("<their reason>", "")


def test_direction_is_read_only_and_uses_the_scorecard() -> None:
    body = _body("direction")
    for token in ("ON COURSE", "DRIFTING", "OFF COURSE", "Aligned", "Partial", "Missing", "Contradicted",
                  "Not verified", "Fix code", "Update PRD", "Decide"):
        assert token in body, token
    for forbidden in ("git commit", "Edit", "Write("):
        assert forbidden not in body, forbidden
    assert "AskUserQuestion` is never called" in body
    assert "report the verdict as your final output and end the turn" in body
    assert "only when the caller passed the slug of an active build run" in body


def test_prd_skill_interviews_and_checks_mechanically() -> None:
    body = _body("prd")
    for token in ("templates/process/prd.md", "prd-status.py", "NEEDS-DECISION", "you decide", "from existing code",
                  "planning-ready"):
        assert token in body, token
    assert SNIPPET in body
    assert "AskUserQuestion` is never called" in body
    assert "report what it needs as its final output and end the turn" in body
    assert "only when the caller passed the slug of an active build run" in body
    assert "the grader's exit code decides" in body
    assert "- NEEDS-DECISION: <question>" in body and "Numbered items and bare" in body
    assert "contiguous" in body and "Any other exit code: report it" in body
