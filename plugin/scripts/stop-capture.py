#!/usr/bin/env python3
"""Stop hook: ask the model to record this session's decisions, once.

Sessions and pending items were already captured automatically. Decisions were
not: they were written only when the session-wrap skill ran, which needs
somebody to remember to say "done" before closing the terminal. On this machine
that produced nineteen days of sessions with every file touch logged and not
one reason recorded.

A regex over the transcript cannot fix that -- decisions.py deliberately
captures only what is explicitly labelled a decision, because anything looser
stored fragments. The only thing in the loop that can tell a decision from a
passing remark is the model, and it is still holding the session when it
stops. So this blocks the stop exactly once and asks it to write them down.

Three rules keep that from being obnoxious:

  * once per session, tracked by a marker file, so a conversation cannot be
    nagged twice however long it runs;
  * never when stop_hook_active is set, which is Claude Code telling us this
    stop already came from a hook -- ignoring that flag is how a stop hook
    becomes an infinite loop;
  * only when the session actually did something, measured in tool calls.

Any error at all lets the stop proceed. A memory feature must never be the
reason someone cannot end their session.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

from _path import setup_paths

setup_paths()

# Below this many tool calls a session has not done enough to have decided
# anything worth keeping, and the prompt would be pure noise.
MIN_TOOL_CALLS = 12


def _fail_open(msg: str = "") -> None:
    """Allow the stop. Never block on our own bugs."""
    if msg:
        print(msg, file=sys.stderr)
    sys.exit(0)


def _tool_calls(transcript_path: str) -> int:
    p = Path(transcript_path)
    if not p.exists():
        return 0
    n = 0
    try:
        with open(p, "r", encoding="utf-8", errors="replace") as fh:
            for line in fh:
                if '"tool_use"' in line:
                    n += 1
    except OSError:
        return 0
    return n


def main() -> None:
    try:
        payload = json.load(sys.stdin)
    except Exception:
        _fail_open()

    # Claude Code sets this when the stop is itself the result of a stop hook.
    # Without this check the block below would re-fire forever.
    if payload.get("stop_hook_active"):
        _fail_open()

    session_id = str(payload.get("session_id") or "").strip()
    transcript = str(payload.get("transcript_path") or "")
    cwd = str(payload.get("cwd") or ".")
    if not session_id:
        _fail_open()

    try:
        from tentaqles.manifest.loader import load_manifest

        manifest = load_manifest(cwd)
        client_root = manifest.get("_client_root", cwd) if manifest else cwd
        client = manifest.get("client", "this workspace") if manifest else "this workspace"
    except Exception:
        _fail_open()

    marker_dir = Path(client_root) / ".claude" / "stop-capture"
    marker = marker_dir / (session_id + ".done")
    if marker.exists():
        _fail_open()

    if _tool_calls(transcript) < MIN_TOOL_CALLS:
        _fail_open()

    # Write the marker BEFORE blocking. If anything downstream fails, the worst
    # case is one missed capture; the alternative -- writing it after -- risks
    # asking again on the next stop, which is the behaviour most likely to make
    # someone disable the hook entirely.
    try:
        marker_dir.mkdir(parents=True, exist_ok=True)
        marker.write_text("", encoding="utf-8")
    except OSError:
        _fail_open()

    reason = (
        "Before finishing: record what was DECIDED in this session for "
        + client
        + ", while you still have the context.\n\n"
        "For each real decision -- a choice between alternatives that someone "
        "would need the reasoning for later, not a task you completed -- run:\n\n"
        '  echo \'{"cwd": "' + cwd.replace("\\", "\\\\") + '", "event": "decision", '
        '"data": {"chosen": "<what was decided>", "rationale": "<why, including '
        'what it was chosen over>", "confidence": "high"}}\' | '
        'bash "$CLAUDE_PLUGIN_ROOT/scripts/tq_run.sh" memory-bridge.py\n\n'
        "Skip trivia and skip work that merely got done. If nothing was truly "
        "decided, record nothing and simply say so. Then finish your reply as "
        "normal -- you will not be asked again this session."
    )

    print(json.dumps({"decision": "block", "reason": reason}))
    sys.exit(0)


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:  # pragma: no cover - belt and braces
        _fail_open("stop-capture: %s" % exc)
