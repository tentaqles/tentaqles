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
    assert "AskUserQuestion` is never called" in body
    assert "build-gate ask" in body.split("## Headless mode", 1)[1]
