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

Four rules keep that from being obnoxious:

  * once per session, tracked by a marker file keyed by session_id in the
    plugin data dir, so a conversation cannot be nagged twice however long it
    runs;
  * never when stop_hook_active is set, which is Claude Code telling us this
    stop already came from a hook -- ignoring that flag is how a stop hook
    becomes an infinite loop;
  * only when the session actually did something, measured in tool calls;
  * only when the conversation shows a real choice between alternatives
    ("instead of", "trade-off", "decidimos", ...). A session that merely did
    work has nothing to record, and blocking it just burns a turn.

The instruction embeds the plugin's absolute path. `$CLAUDE_PLUGIN_ROOT` is
expanded in hooks.json but is not set inside the Bash tool the model uses, so
a command written with it failed with exit 127 every time.

The same hook also watches context size: when the last assistant turn's input
(prompt + cache read + cache creation) passes TENTAQLES_CONTEXT_NUDGE_TOKENS
(350k by default) it shows the user a one-time, non-blocking `systemMessage`
suggesting /compact or the session-wrap skill.

Any error at all lets the stop proceed. A memory feature must never be the
reason someone cannot end their session.
"""

from __future__ import annotations

import json
import os
import re
import sys
import time
from pathlib import Path

from _path import setup_paths

setup_paths()

# Below this many tool calls a session has not done enough to have decided
# anything worth keeping, and the prompt would be pure noise.
MIN_TOOL_CALLS = 12

# Default context size (tokens) above which the user is nudged once.
DEFAULT_NUDGE_TOKENS = 350_000

# How much of the transcript's tail to read when looking for the last usage.
_TAIL_BYTES = 1024 * 1024

# Usage fields that together make up the context the model was sent.
_USAGE_FIELDS = (
    "input_tokens",
    "cache_read_input_tokens",
    "cache_creation_input_tokens",
)

# Markers older than this are pruned whenever a new one is written.
_MARKER_TTL_SECONDS = 30 * 86400

# Phrases that indicate a choice between alternatives was discussed. Matched
# against conversational text only (user prompts and assistant prose), never
# against tool input/output, where "vs" and "option" are everywhere.
DECISION_SIGNALS = re.compile(
    r"\b(?:"
    r"instead\s+of|rather\s+than|chose|decided|decid(?:e|ing)\s+(?:to|on|against)"
    r"|trade-?offs?|went\s+with|opted|option\s+[a-c1-3]|versus|vs"
    # Portuguese
    r"|em\s+vez\s+de|ao\s+inv[eé]s\s+de|decidimos|decidi|optei|optamos"
    r"|escolhi|escolhemos"
    r")(?![\w])",
    re.IGNORECASE,
)

# Cheap pre-filter: lines that could carry conversational text at all.
_CONVERSATION_LINE = re.compile(r'"type"\s*:\s*"(?:user|assistant)"')


def _fail_open(msg: str = "") -> None:
    """Allow the stop. Never block on our own bugs."""
    if msg:
        print(msg, file=sys.stderr)
    sys.exit(0)


def plugin_root() -> str:
    """Absolute plugin root, resolved from this file, with forward slashes."""
    return Path(__file__).resolve().parent.parent.as_posix()


def _marker_dir() -> Path:
    try:
        from tentaqles.config import data_dir

        base = data_dir()
    except Exception:
        base = Path(os.environ.get("CLAUDE_PLUGIN_DATA") or Path.home() / ".tentaqles")
    return base / "stop-capture"


def _safe_id(session_id: str) -> str:
    return re.sub(r"[^A-Za-z0-9._-]", "_", session_id)[:128]


def _marker(session_id: str, kind: str) -> Path:
    return _marker_dir() / ("%s.%s" % (_safe_id(session_id), kind))


def _write_marker(path: Path) -> bool:
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("", encoding="utf-8")
    except OSError:
        return False
    # Opportunistic pruning so the directory does not grow forever.
    try:
        cutoff = time.time() - _MARKER_TTL_SECONDS
        for old in path.parent.iterdir():
            try:
                if old.stat().st_mtime < cutoff:
                    old.unlink()
            except OSError:
                pass
    except OSError:
        pass
    return True


def _message_text(entry: dict) -> str:
    """Conversational text of a transcript entry: prompts and prose only."""
    if entry.get("isMeta"):
        return ""
    if entry.get("type") not in ("user", "assistant"):
        return ""
    msg = entry.get("message")
    if not isinstance(msg, dict):
        return ""
    content = msg.get("content")
    if isinstance(content, str):
        return content
    parts = []
    if isinstance(content, list):
        for block in content:
            if isinstance(block, dict) and block.get("type") == "text":
                parts.append(str(block.get("text") or ""))
    return "\n".join(parts)


def scan_transcript(transcript_path: str) -> tuple[int, bool]:
    """Return (tool_calls, has_decision_signal) for the whole transcript."""
    p = Path(transcript_path) if transcript_path else None
    if p is None or not p.is_file():
        return 0, False
    tool_calls = 0
    signal = False
    try:
        with open(p, "r", encoding="utf-8", errors="replace") as fh:
            for line in fh:
                if '"tool_use"' in line:
                    tool_calls += 1
                if signal:
                    continue
                if not _CONVERSATION_LINE.search(line) or not DECISION_SIGNALS.search(line):
                    continue
                try:
                    entry = json.loads(line)
                except ValueError:
                    continue
                if isinstance(entry, dict) and DECISION_SIGNALS.search(_message_text(entry)):
                    signal = True
    except OSError:
        return 0, False
    return tool_calls, signal


def last_context_tokens(transcript_path: str) -> int:
    """Input-side tokens of the last assistant message that reports usage."""
    p = Path(transcript_path) if transcript_path else None
    if p is None or not p.is_file():
        return 0
    try:
        size = p.stat().st_size
        with open(p, "rb") as fh:
            if size > _TAIL_BYTES:
                fh.seek(size - _TAIL_BYTES)
                fh.readline()  # drop the partial first line
            lines = fh.read().decode("utf-8", errors="replace").splitlines()
    except OSError:
        return 0
    for line in reversed(lines):
        if '"usage"' not in line or '"assistant"' not in line:
            continue
        try:
            entry = json.loads(line)
        except ValueError:
            continue
        if not isinstance(entry, dict) or entry.get("type") != "assistant":
            continue
        msg = entry.get("message")
        usage = msg.get("usage") if isinstance(msg, dict) else None
        if not isinstance(usage, dict):
            continue
        total = 0
        for field in _USAGE_FIELDS:
            try:
                total += int(usage.get(field) or 0)
            except (TypeError, ValueError):
                pass
        return total
    return 0


def nudge_threshold() -> int:
    """TENTAQLES_CONTEXT_NUDGE_TOKENS, or the default. 0 or less disables."""
    raw = os.environ.get("TENTAQLES_CONTEXT_NUDGE_TOKENS", "").strip()
    if not raw:
        return DEFAULT_NUDGE_TOKENS
    try:
        return int(raw)
    except ValueError:
        return DEFAULT_NUDGE_TOKENS


def context_nudge(session_id: str, transcript_path: str) -> str:
    """One-time user-facing notice when the session's context is very large."""
    threshold = nudge_threshold()
    if threshold <= 0:
        return ""
    marker = _marker(session_id, "nudge")
    if marker.exists():
        return ""
    used = last_context_tokens(transcript_path)
    if used <= threshold:
        return ""
    if not _write_marker(marker):
        return ""
    return (
        "Tentaqles: this session's context is ~%dk tokens. Long sessions get slow "
        "and expensive -- consider /compact, or wrap up with the session-wrap skill "
        "(/tentaqles:session-wrap) and start a fresh session." % (used // 1000)
    )


def decision_prompt(client: str, cwd: str) -> str:
    root = plugin_root()
    payload = json.dumps(
        {
            "cwd": Path(cwd).as_posix() if cwd else ".",
            "event": "decision",
            "data": {
                "chosen": "<what was decided>",
                "rationale": "<why, including what it was chosen over>",
                "confidence": "high",
            },
        }
    )
    # Single-quoted for bash; a literal ' inside is closed, escaped, reopened.
    quoted = "'" + payload.replace("'", "'\\''") + "'"
    return (
        "Before finishing: record what was DECIDED in this session for "
        + client
        + ", while you still have the context.\n\n"
        "For each real decision -- a choice between alternatives that someone "
        "would need the reasoning for later, not a task you completed -- run "
        "(with the Bash tool):\n\n"
        "  echo " + quoted + ' | bash "' + root + '/scripts/tq_run.sh" memory-bridge.py\n\n'
        "Skip trivia and skip work that merely got done. If nothing was truly "
        "decided, record nothing and simply say so. Then finish your reply as "
        "normal -- you will not be asked again this session."
    )


def should_prompt_decisions(session_id: str, transcript: str) -> bool:
    if _marker(session_id, "done").exists():
        return False
    tool_calls, signal = scan_transcript(transcript)
    return tool_calls >= MIN_TOOL_CALLS and signal


def main() -> None:
    try:
        payload = json.load(sys.stdin)
    except Exception:
        _fail_open()
    if not isinstance(payload, dict):
        _fail_open()

    # A headless `claude -p` started by the plugin itself (semantic facts).
    if os.environ.get("TENTAQLES_HEADLESS_CHILD") == "1":
        _fail_open()

    session_id = str(payload.get("session_id") or "").strip()
    transcript = str(payload.get("transcript_path") or "")
    cwd = str(payload.get("cwd") or ".")
    if not session_id:
        _fail_open()

    out: dict = {}

    try:
        msg = context_nudge(session_id, transcript)
        if msg:
            out["systemMessage"] = msg
    except Exception:
        pass

    # Claude Code sets this when the stop is itself the result of a stop hook.
    # Without this check the block below would re-fire forever.
    block = False
    if not payload.get("stop_hook_active"):
        try:
            block = should_prompt_decisions(session_id, transcript)
        except Exception:
            block = False

    if block:
        client = "this workspace"
        try:
            from tentaqles.manifest.loader import load_manifest

            manifest = load_manifest(cwd)
            if manifest:
                client = manifest.get("client", client) or client
        except Exception:
            pass
        # Write the marker BEFORE blocking. If anything downstream fails, the
        # worst case is one missed capture; the alternative -- writing it
        # after -- risks asking again on the next stop, which is the behaviour
        # most likely to make someone disable the hook entirely.
        if _write_marker(_marker(session_id, "done")):
            out["decision"] = "block"
            out["reason"] = decision_prompt(client, cwd)

    if out:
        print(json.dumps(out))
    sys.exit(0)


if __name__ == "__main__":
    try:
        main()
    except SystemExit:
        raise
    except Exception as exc:  # pragma: no cover - belt and braces
        _fail_open("stop-capture: %s" % exc)
