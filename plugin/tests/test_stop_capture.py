"""Tests for scripts/stop-capture.py: decision prompt + context-size nudge.

The hook runs as a real subprocess, exactly as Claude Code runs it, with
CLAUDE_PLUGIN_DATA pointed at tmp so markers never touch the real data dir.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
HOOK = PLUGIN_ROOT / "scripts" / "stop-capture.py"


def _env(tmp_path: Path, **extra: str) -> dict:
    env = dict(os.environ)
    env["CLAUDE_PLUGIN_ROOT"] = str(PLUGIN_ROOT)
    env["CLAUDE_PLUGIN_DATA"] = str(tmp_path / "data")
    env["PYTHONPATH"] = str(PLUGIN_ROOT)
    env.pop("TENTAQLES_CONTEXT_NUDGE_TOKENS", None)
    env.pop("TENTAQLES_HEADLESS_CHILD", None)
    env.update(extra)
    return env


def _run(tmp_path: Path, payload: dict, **extra: str) -> dict:
    proc = subprocess.run(
        [sys.executable, str(HOOK)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        timeout=60,
        env=_env(tmp_path, **extra),
    )
    assert proc.returncode == 0, proc.stderr
    out = proc.stdout.strip()
    return json.loads(out) if out else {}


def _assistant(text: str = "", tool: bool = False, usage: dict | None = None) -> dict:
    content: list = []
    if text:
        content.append({"type": "text", "text": text})
    if tool:
        content.append({"type": "tool_use", "id": "t", "name": "Bash", "input": {"command": "ls"}})
    msg: dict = {"role": "assistant", "content": content}
    if usage is not None:
        msg["usage"] = usage
    return {"type": "assistant", "message": msg}


def _tool_result(text: str) -> dict:
    return {
        "type": "user",
        "message": {
            "role": "user",
            "content": [{"type": "tool_result", "tool_use_id": "t", "content": text}],
        },
    }


def _transcript(tmp_path: Path, entries: list[dict], name: str = "t.jsonl") -> Path:
    p = tmp_path / name
    p.write_text("\n".join(json.dumps(e) for e in entries) + "\n", encoding="utf-8")
    return p


def _busy(prose: str, n_tools: int = 15) -> list[dict]:
    entries = [{"type": "user", "message": {"role": "user", "content": "please fix the build"}}]
    entries += [_assistant(tool=True) for _ in range(n_tools)]
    entries.append(_assistant(prose))
    return entries


def _payload(tmp_path: Path, transcript: Path, sid: str = "s1", **kw) -> dict:
    d = {
        "session_id": sid,
        "transcript_path": str(transcript),
        "cwd": str(tmp_path),
        "hook_event_name": "Stop",
        "stop_hook_active": False,
    }
    d.update(kw)
    return d


# --- decision prompt ---------------------------------------------------------


def test_blocks_once_with_absolute_plugin_root(tmp_path: Path) -> None:
    t = _transcript(tmp_path, _busy("We went with SQLite instead of Postgres for the cache."))
    out = _run(tmp_path, _payload(tmp_path, t))
    assert out.get("decision") == "block"
    reason = out["reason"]
    assert "$CLAUDE_PLUGIN_ROOT" not in reason
    root = PLUGIN_ROOT.resolve().as_posix()
    assert 'bash "' + root + '/scripts/tq_run.sh" memory-bridge.py' in reason
    assert "\\" not in root

    # Second stop in the same session: never asked again.
    assert "decision" not in _run(tmp_path, _payload(tmp_path, t))


def test_embedded_command_is_a_valid_bridge_payload(tmp_path: Path) -> None:
    t = _transcript(tmp_path, _busy("Decidimos usar Redis em vez de Memcached."))
    reason = _run(tmp_path, _payload(tmp_path, t))["reason"]
    line = next(l for l in reason.splitlines() if "memory-bridge.py" in l)
    quoted = line.split("echo ", 1)[1].split(" | bash", 1)[0]
    assert quoted.startswith("'") and quoted.endswith("'")
    data = json.loads(quoted[1:-1])
    assert data["event"] == "decision"
    assert set(data["data"]) == {"chosen", "rationale", "confidence"}


def test_never_blocks_when_stop_hook_active(tmp_path: Path) -> None:
    t = _transcript(tmp_path, _busy("We chose option A over option B because of the trade-off."))
    out = _run(tmp_path, _payload(tmp_path, t, stop_hook_active=True))
    assert "decision" not in out
    # And the active stop did not consume the one prompt.
    assert _run(tmp_path, _payload(tmp_path, t)).get("decision") == "block"


def test_no_block_without_a_decision_signal(tmp_path: Path) -> None:
    t = _transcript(tmp_path, _busy("Done. All tests pass and the build is green."))
    assert "decision" not in _run(tmp_path, _payload(tmp_path, t))


def test_signal_inside_tool_output_does_not_count(tmp_path: Path) -> None:
    entries = _busy("Done. Build is green.")
    entries.insert(3, _tool_result("diff: use foo instead of bar; option A vs option B"))
    t = _transcript(tmp_path, entries)
    assert "decision" not in _run(tmp_path, _payload(tmp_path, t))


def test_no_block_for_a_short_session(tmp_path: Path) -> None:
    t = _transcript(tmp_path, _busy("We decided to keep the old API.", n_tools=3))
    assert "decision" not in _run(tmp_path, _payload(tmp_path, t))


def test_marker_is_per_session(tmp_path: Path) -> None:
    t = _transcript(tmp_path, _busy("I opted for the simpler approach rather than a rewrite."))
    assert _run(tmp_path, _payload(tmp_path, t, sid="a")).get("decision") == "block"
    assert _run(tmp_path, _payload(tmp_path, t, sid="b")).get("decision") == "block"
    assert (tmp_path / "data" / "stop-capture" / "a.done").exists()


def test_garbage_input_fails_open(tmp_path: Path) -> None:
    proc = subprocess.run(
        [sys.executable, str(HOOK)],
        input="not json",
        capture_output=True,
        text=True,
        timeout=60,
        env=_env(tmp_path),
    )
    assert proc.returncode == 0
    assert proc.stdout.strip() == ""


# --- context-size nudge -------------------------------------------------------


def _big(tokens: int) -> dict:
    return {
        "input_tokens": 10,
        "cache_read_input_tokens": tokens - 1010,
        "cache_creation_input_tokens": 1000,
        "output_tokens": 50,
    }


def test_nudge_once_above_threshold_without_blocking(tmp_path: Path) -> None:
    t = _transcript(
        tmp_path,
        [
            _assistant("early", usage=_big(500_000)),  # not the last one
            _assistant("ok", usage=_big(400_000)),
        ],
    )
    out = _run(tmp_path, _payload(tmp_path, t))
    assert "decision" not in out
    assert "systemMessage" in out
    assert "~400k" in out["systemMessage"]
    assert "/compact" in out["systemMessage"]
    # One time per session.
    assert "systemMessage" not in _run(tmp_path, _payload(tmp_path, t))


def test_no_nudge_below_threshold(tmp_path: Path) -> None:
    t = _transcript(tmp_path, [_assistant("ok", usage=_big(200_000))])
    assert "systemMessage" not in _run(tmp_path, _payload(tmp_path, t))


def test_nudge_threshold_env_override(tmp_path: Path) -> None:
    t = _transcript(tmp_path, [_assistant("ok", usage=_big(200_000))])
    out = _run(tmp_path, _payload(tmp_path, t), TENTAQLES_CONTEXT_NUDGE_TOKENS="100000")
    assert "systemMessage" in out


def test_nudge_shown_even_when_stop_hook_active(tmp_path: Path) -> None:
    t = _transcript(tmp_path, [_assistant("ok", usage=_big(400_000))])
    out = _run(tmp_path, _payload(tmp_path, t, stop_hook_active=True))
    assert "systemMessage" in out and "decision" not in out


def test_headless_child_is_ignored(tmp_path: Path) -> None:
    t = _transcript(tmp_path, _busy("We chose X instead of Y.") + [_assistant("x", usage=_big(900_000))])
    assert _run(tmp_path, _payload(tmp_path, t), TENTAQLES_HEADLESS_CHILD="1") == {}
