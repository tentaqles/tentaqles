"""Tests for transcript parsing across the SessionEnd path.

Claude Code writes JSONL transcripts where each entry is
`{"type": "user"|"assistant", "message": {"role": ..., "content": [...]}}`
and tool calls live in `message.content[]` blocks of `{"type": "tool_use",
"name": ..., "input": {...}}`.

Two readers assumed a flat shape that Claude Code never emits — a top-level
`tool_name`/`tool_input`, and an entry type of `"human"`. Both silently
returned nothing: every auto session summary read "Session with no file
changes" and no pending items were ever detected. These tests pin the real
shape, and keep the legacy flat shape working.
"""

from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path

import pytest

from tentaqles.threads import _extract_human_text, detect_open_threads


SCRIPTS = Path(__file__).resolve().parent.parent / "scripts"


_SESSION_END = None


@pytest.fixture(scope="module")
def session_end():
    """Load scripts/session-end.py as a module.

    Loaded lazily inside a fixture rather than at import time: executing it
    during collection disturbs pytest's stdio capture.
    """
    global _SESSION_END
    if _SESSION_END is None:
        spec = importlib.util.spec_from_file_location("session_end_mod", SCRIPTS / "session-end.py")
        mod = importlib.util.module_from_spec(spec)
        # _path.setup_paths() rebinds sys.stdout/stderr to UTF-8 TextIOWrappers
        # on Windows. Simply restoring the originals is not enough: when the
        # wrapper is garbage collected it closes the buffer it wrapped — which
        # is pytest's capture buffer — breaking every later test in the run. So
        # detach the wrapper from that buffer before dropping it. The wrapping
        # is correct in production; it just must not leak into the test process.
        saved_out, saved_err = sys.stdout, sys.stderr
        try:
            spec.loader.exec_module(mod)
        finally:
            for stream, original in ((sys.stdout, saved_out), (sys.stderr, saved_err)):
                if stream is not original and hasattr(stream, "detach"):
                    try:
                        stream.detach()
                    except Exception:
                        pass
            sys.stdout, sys.stderr = saved_out, saved_err
        _SESSION_END = mod
    return _SESSION_END


# ---------------------------------------------------------------------------
# Fixture builders — the shapes Claude Code actually writes
# ---------------------------------------------------------------------------


def _assistant(blocks: list[dict], ts: str = "2026-01-01T00:00:00Z") -> dict:
    return {"type": "assistant", "timestamp": ts,
            "message": {"role": "assistant", "content": blocks}}


def _user(blocks, ts: str = "2026-01-01T00:00:00Z") -> dict:
    return {"type": "user", "timestamp": ts, "message": {"role": "user", "content": blocks}}


def _tool_use(name: str, tool_input: dict) -> dict:
    return {"type": "tool_use", "name": name, "input": tool_input}


def _write(tmp_path: Path, entries: list[dict], name: str = "t.jsonl") -> str:
    p = tmp_path / name
    p.write_text("\n".join(json.dumps(e) for e in entries), encoding="utf-8")
    return str(p)


@pytest.fixture()
def nested_transcript(tmp_path: Path) -> str:
    return _write(tmp_path, [
        _user([{"type": "text", "text": "Please refactor the auth module and fix the token bug."}],
              ts="2026-01-01T00:00:00Z"),
        _assistant([
            {"type": "thinking", "thinking": "internal reasoning that must never be a summary hint"},
            {"type": "text", "text": "On it."},
            _tool_use("Read", {"file_path": "src/config.py"}),
        ]),
        _user([{"type": "tool_result", "content": "file contents here"}]),
        _assistant([
            _tool_use("Edit", {"file_path": "src/auth.py", "old_string": "a", "new_string": "b"}),
            _tool_use("Write", {"file_path": "tests/test_auth.py", "content": "..."}),
            _tool_use("Bash", {"command": "pytest tests/ -q"}),
        ]),
        _user([{"type": "text", "text": "TODO: still need to rotate the signing key before release."}],
              ts="2026-01-01T01:30:00Z"),
    ])


# ---------------------------------------------------------------------------
# parse_transcript
# ---------------------------------------------------------------------------


def test_parse_transcript_reads_nested_tool_calls(session_end, nested_transcript: str) -> None:
    a = session_end.parse_transcript(nested_transcript)
    assert a["files_edited"] == ["src/auth.py"]
    assert a["files_created"] == ["tests/test_auth.py"]
    assert a["files_read"] == ["src/config.py"]
    assert "pytest tests/ -q" in a["commands_run"]


def test_parse_transcript_summary_reflects_real_work(session_end, nested_transcript: str) -> None:
    """The regression that made every stored summary useless."""
    summary = session_end.build_summary(session_end.parse_transcript(nested_transcript))
    assert "no file changes" not in summary
    assert "auth.py" in summary


def test_parse_transcript_hints_come_from_user_text_only(session_end, nested_transcript: str) -> None:
    a = session_end.parse_transcript(nested_transcript)
    assert a["summary_hints"], "expected the user's own message as a summary hint"
    joined = " ".join(a["summary_hints"])
    assert "refactor the auth module" in joined
    # Tool results and assistant thinking are not things the user said.
    assert "file contents here" not in joined
    assert "internal reasoning" not in joined


def test_parse_transcript_counts_turns_and_duration(session_end, nested_transcript: str) -> None:
    a = session_end.parse_transcript(nested_transcript)
    assert a["turn_count"] == 2
    assert a["duration_s"] == 5400  # 00:00 → 01:30


def test_parse_transcript_still_reads_legacy_flat_shape(session_end, tmp_path: Path) -> None:
    """Older/synthetic transcripts using the flat shape must keep working."""
    path = _write(tmp_path, [
        {"type": "assistant", "tool_name": "Edit", "tool_input": {"file_path": "legacy.py"}},
        {"type": "assistant", "tool_name": "Bash", "tool_input": {"command": "ls -la"}},
    ])
    a = session_end.parse_transcript(path)
    assert a["files_edited"] == ["legacy.py"]
    assert "ls -la" in a["commands_run"]


def test_parse_transcript_tolerates_garbage(session_end, tmp_path: Path) -> None:
    path = tmp_path / "junk.jsonl"
    path.write_text(
        "not json\n"
        + json.dumps({"type": "assistant", "message": {"content": "plain string"}}) + "\n"
        + json.dumps({"type": "assistant", "message": {"content": [None, 42, {"type": "tool_use"}]}}) + "\n"
        + json.dumps({"type": "user", "message": None}) + "\n",
        encoding="utf-8",
    )
    a = session_end.parse_transcript(str(path))  # must not raise
    assert a["files_edited"] == []


# ---------------------------------------------------------------------------
# threads._extract_human_text / detect_open_threads
# ---------------------------------------------------------------------------


def test_extract_human_text_reads_nested_user_messages(nested_transcript: str) -> None:
    texts = [t for _, t in _extract_human_text(nested_transcript)]
    assert any("refactor the auth module" in t for t in texts)
    assert any("rotate the signing key" in t for t in texts)
    # tool_result blocks are transcript plumbing, not something the human typed
    assert not any("file contents here" in t for t in texts)


def test_detect_open_threads_finds_nested_todo(nested_transcript: str) -> None:
    threads = detect_open_threads(nested_transcript)
    assert threads, "expected the TODO in the user's nested message to be detected"
    assert any("signing key" in t["description"] for t in threads)


def test_extract_human_text_still_reads_legacy_flat_shape(tmp_path: Path) -> None:
    path = _write(tmp_path, [{"type": "human", "content": "TODO: legacy flat shape still works"}])
    texts = [t for _, t in _extract_human_text(path)]
    assert texts == ["TODO: legacy flat shape still works"]
