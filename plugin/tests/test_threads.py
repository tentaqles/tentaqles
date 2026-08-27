"""Tests for tentaqles.threads open-thread detection."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from tentaqles.threads import (
    _jaccard,
    deduplicate_pending,
    detect_open_threads,
)


def _write_transcript(tmp_path: Path, entries: list[dict]) -> str:
    path = tmp_path / "transcript.jsonl"
    with path.open("w", encoding="utf-8") as fh:
        for e in entries:
            fh.write(json.dumps(e) + "\n")
    return str(path)


def test_detect_todo_in_human_message(tmp_path: Path) -> None:
    path = _write_transcript(
        tmp_path,
        [
            {"type": "human", "content": "TODO: fix auth bug"},
            {"type": "assistant", "content": "Sure, I'll help."},
        ],
    )
    results = detect_open_threads(path)
    assert len(results) == 1
    assert results[0]["pattern"] == r"\bTODO\b"
    assert "TODO" in results[0]["raw_text"]


def test_ignores_tool_output_todos(tmp_path: Path) -> None:
    path = _write_transcript(
        tmp_path,
        [
            {"type": "human", "content": "please refactor this"},
            {"type": "assistant", "content": "# TODO: fix later\ndef foo(): pass"},
            {"type": "tool_result", "content": "TODO found in code"},
        ],
    )
    results = detect_open_threads(path)
    assert results == []


def test_priority_bump_urgent(tmp_path: Path) -> None:
    path = _write_transcript(
        tmp_path,
        [{"type": "human", "content": "urgent: need to fix the database connection"}],
    )
    results = detect_open_threads(path)
    assert len(results) == 1
    assert results[0]["priority"] == "critical"


def test_priority_default_medium(tmp_path: Path) -> None:
    path = _write_transcript(
        tmp_path,
        [{"type": "human", "content": "still need to refactor the auth module"}],
    )
    results = detect_open_threads(path)
    assert len(results) == 1
    assert results[0]["priority"] == "medium"


def test_empty_transcript(tmp_path: Path) -> None:
    missing = str(tmp_path / "does_not_exist.jsonl")
    assert detect_open_threads(missing) == []


def test_malformed_jsonl_skipped(tmp_path: Path) -> None:
    path = tmp_path / "transcript.jsonl"
    with path.open("w", encoding="utf-8") as fh:
        fh.write(json.dumps({"type": "human", "content": "TODO: first item"}) + "\n")
        fh.write("this is not json at all {{{\n")
        fh.write(json.dumps({"type": "human", "content": "FIXME: second item"}) + "\n")
    results = detect_open_threads(str(path))
    assert len(results) == 2
    patterns = {r["pattern"] for r in results}
    assert r"\bTODO\b" in patterns
    assert r"\bFIXME\b" in patterns


def test_deduplicate_similar() -> None:
    candidates = [{"description": "fix the auth bug in login flow"}]
    existing = [{"description": "fix the auth bug in login flow"}]
    result = deduplicate_pending(candidates, existing)
    assert result == []


def test_deduplicate_dissimilar() -> None:
    candidates = [{"description": "refactor the database connection pool"}]
    existing = [{"description": "update documentation for API endpoints"}]
    result = deduplicate_pending(candidates, existing)
    assert len(result) == 1


def test_deduplicate_empty_existing() -> None:
    candidates = [
        {"description": "task one"},
        {"description": "task two"},
    ]
    result = deduplicate_pending(candidates, [])
    assert len(result) == 2


def test_context_snippet_length(tmp_path: Path) -> None:
    long_prefix = "word " * 50  # 250 chars
    long_suffix = "blah " * 50
    content = f"{long_prefix}TODO fix this thing {long_suffix}"
    path = _write_transcript(tmp_path, [{"type": "human", "content": content}])
    results = detect_open_threads(path)
    assert len(results) == 1
    # ~200 chars window (100 before + match + 100 after), allow some slack for trim
    assert 150 <= len(results[0]["raw_text"]) <= 260


def test_multiple_patterns_same_turn_deduped(tmp_path: Path) -> None:
    # Message matches TODO, FIXME, and "still need to" (3 patterns)
    content = "TODO and FIXME — still need to wrap this up"
    path = _write_transcript(tmp_path, [{"type": "human", "content": content}])
    results = detect_open_threads(path)
    assert len(results) == 1
    # Most specific (first in pattern order) should win: TODO
    assert results[0]["pattern"] == r"\bTODO\b"


def test_redacted_description(tmp_path: Path) -> None:
    # A real API key assignment adjacent to TODO — privacy module should redact it
    content = "TODO: rotate api_key=abcdef1234567890ABCDEF before release"
    path = _write_transcript(tmp_path, [{"type": "human", "content": content}])
    results = detect_open_threads(path)
    assert len(results) == 1
    desc = results[0]["description"]
    raw = results[0]["raw_text"]
    # The raw secret value should not appear verbatim
    assert "abcdef1234567890ABCDEF" not in desc
    assert "abcdef1234567890ABCDEF" not in raw
    assert "REDACTED" in raw or "REDACTED" in desc


def test_content_as_list_format(tmp_path: Path) -> None:
    """Content as a list of {type, text} dicts should also be parsed."""
    path = _write_transcript(
        tmp_path,
        [
            {
                "type": "human",
                "content": [
                    {"type": "text", "text": "Hello there"},
                    {"type": "text", "text": "TODO: come back to this"},
                ],
            }
        ],
    )
    results = detect_open_threads(path)
    assert len(results) == 1


def test_jaccard_basic() -> None:
    assert _jaccard("foo bar baz", "foo bar baz") == 1.0
    assert _jaccard("foo bar", "baz qux") == 0.0
    # Half overlap
    sim = _jaccard("foo bar baz", "foo bar qux")
    assert 0.4 < sim < 0.6
