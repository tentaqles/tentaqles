"""Reading Claude Code JSONL transcripts.

Claude Code writes one JSON object per line, shaped like::

    {"type": "user"|"assistant",
     "timestamp": "...",
     "message": {"role": "...", "content": [ ...blocks... ]}}

Tool calls are *content blocks* inside ``message.content``::

    {"type": "tool_use", "name": "Edit", "input": {"file_path": ...}}

Readers that assumed a flat shape — a top-level ``tool_name``/``tool_input``,
or an entry ``type`` of ``"human"`` — silently matched nothing on every real
transcript. Centralising the shape here keeps that from being rediscovered
independently (and wrongly) in each caller.

The legacy flat shape is still accepted, so older or synthetic transcripts
keep working.
"""

from __future__ import annotations

import json
import re
from datetime import datetime
from pathlib import Path
from typing import Any, Iterator


def entry_role(entry: dict) -> str:
    """Return "user", "assistant", or "" for a transcript entry.

    Accepts the legacy ``"human"`` entry type as "user".
    """
    if not isinstance(entry, dict):
        return ""
    t = entry.get("type")
    if t in ("user", "human"):
        return "user"
    if t == "assistant":
        return "assistant"
    # Entries that carry the role only on the message body.
    msg = entry.get("message")
    if isinstance(msg, dict):
        role = msg.get("role")
        if role in ("user", "human"):
            return "user"
        if role == "assistant":
            return "assistant"
    return ""


def _content(entry: dict) -> Any:
    """Return the entry's content, nested or flat."""
    msg = entry.get("message")
    if isinstance(msg, dict) and msg.get("content") is not None:
        return msg.get("content")
    return entry.get("content")


def entry_text(entry: dict) -> str:
    """Return the human-readable text of an entry.

    Only ``text`` blocks count. ``tool_result`` blocks are transcript plumbing
    (they carry tool output, not something anyone typed) and ``thinking``
    blocks are the model's private reasoning — including either would poison
    summaries and open-thread detection.
    """
    if not isinstance(entry, dict):
        return ""
    content = _content(entry)
    if isinstance(content, str):
        return content
    if not isinstance(content, list):
        return ""

    parts: list[str] = []
    for block in content:
        if isinstance(block, str):
            parts.append(block)
            continue
        if not isinstance(block, dict):
            continue
        btype = block.get("type")
        text = block.get("text")
        # Typed text blocks, plus untyped {"text": ...} from the legacy shape.
        if isinstance(text, str) and (btype == "text" or btype is None):
            parts.append(text)
    return "\n".join(p for p in parts if p)


def iter_tool_uses(entry: dict) -> Iterator[tuple[str, dict]]:
    """Yield ``(tool_name, tool_input)`` for every tool call in an entry."""
    if not isinstance(entry, dict):
        return

    # Legacy flat shape: one tool call per entry.
    flat_name = entry.get("tool_name")
    if isinstance(flat_name, str) and flat_name:
        flat_input = entry.get("tool_input")
        yield flat_name, flat_input if isinstance(flat_input, dict) else {}
        return

    content = _content(entry)
    if not isinstance(content, list):
        return
    for block in content:
        if not isinstance(block, dict) or block.get("type") != "tool_use":
            continue
        name = block.get("name")
        if not isinstance(name, str) or not name:
            continue
        tool_input = block.get("input")
        yield name, tool_input if isinstance(tool_input, dict) else {}


def parse_transcript(transcript_path: str) -> dict:
    """Parse the JSONL transcript to extract session activity.

    Returns:
        {
            "files_edited": ["src/auth.py", ...],
            "files_read": ["src/config.py", ...],
            "files_created": ["tests/test_auth.py", ...],
            "commands_run": ["git status", ...],
            "duration_s": 1234,
            "turn_count": 15,
            "summary_hints": ["Fixed auth bug", ...]
        }
    """
    files_edited = set()
    files_read = set()
    files_created = set()
    commands_run = []
    timestamps = []
    summary_hints = []
    turn_count = 0

    try:
        with open(transcript_path, "r", encoding="utf-8", errors="replace") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    entry = json.loads(line)
                except json.JSONDecodeError:
                    continue

                # Track timestamps for duration
                ts = entry.get("timestamp")
                if ts:
                    timestamps.append(ts)

                role = entry_role(entry)

                # Count turns
                if role == "assistant":
                    turn_count += 1

                # Extract tool use. Claude Code nests these in
                # message.content[] as tool_use blocks — a single entry can
                # carry several.
                for tool_name, tool_input in iter_tool_uses(entry):
                    if tool_name == "Edit":
                        fp = tool_input.get("file_path", "")
                        if fp:
                            files_edited.add(fp)

                    elif tool_name == "Write":
                        fp = tool_input.get("file_path", "")
                        if fp:
                            files_created.add(fp)

                    elif tool_name == "Read":
                        fp = tool_input.get("file_path", "")
                        if fp:
                            files_read.add(fp)

                    elif tool_name == "Bash":
                        cmd = tool_input.get("command", "")
                        if cmd and len(cmd) < 200:
                            commands_run.append(cmd)

                # Look for user messages that hint at what was accomplished
                if role == "user":
                    text = entry_text(entry)
                    # Capture the first substantive user message as a hint
                    if text and len(text) > 20 and len(summary_hints) < 3:
                        # Skip common non-substantive messages
                        if not re.match(r"^(yes|no|ok|sure|thanks|done|y|n)\b", text.lower()):
                            summary_hints.append(text[:150])

    except (OSError, PermissionError):
        pass

    # Calculate duration
    duration_s = 0
    if len(timestamps) >= 2:
        try:
            first = datetime.fromisoformat(timestamps[0].replace("Z", "+00:00"))
            last = datetime.fromisoformat(timestamps[-1].replace("Z", "+00:00"))
            duration_s = int((last - first).total_seconds())
        except (ValueError, TypeError):
            pass

    return {
        "files_edited": sorted(files_edited),
        "files_read": sorted(files_read - files_edited - files_created),
        "files_created": sorted(files_created),
        "commands_run": commands_run[-10:],  # last 10
        "duration_s": duration_s,
        "turn_count": turn_count,
        "summary_hints": summary_hints,
    }


def build_summary(activity: dict) -> str:
    """Build a concise session summary from parsed activity."""
    parts = []

    edited = activity["files_edited"]
    created = activity["files_created"]
    total_files = len(edited) + len(created)

    if total_files > 0:
        file_names = [Path(f).name for f in (edited + created)[:5]]
        parts.append(f"Worked on {total_files} file(s): {', '.join(file_names)}")

    if activity["summary_hints"]:
        # Use the first user message as context
        hint = activity["summary_hints"][0]
        if len(hint) > 100:
            hint = hint[:97] + "..."
        parts.append(f"Context: {hint}")

    if not parts:
        parts.append("Session with no file changes")

    dur = activity["duration_s"]
    if dur > 60:
        parts.append(f"Duration: {dur // 60}m")

    return ". ".join(parts)
