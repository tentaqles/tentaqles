#!/usr/bin/env python3
"""SessionEnd hook — automatically saves session context to temporal memory.

Fires on every session termination (terminal close, Ctrl+C, /exit, timeout).
Parses the conversation transcript to extract files touched, duration, and
a basic summary. Writes to the client's memory.db via MemoryStore.

This is the guaranteed baseline — runs silently with zero user interaction.
The /tentaqles:session-wrap skill adds richer context (decisions, rationale,
pending items) when the user explicitly triggers it.
"""

import os
import sys

# Bootstrap sys.path for plugin imports (tentaqles.* + bootstrapped deps)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import json
import os
import sys
import re
from datetime import datetime, timezone
from pathlib import Path

try:
    from tentaqles.privacy import redact_text
except ImportError:
    def redact_text(text):
        return text, []

try:
    from tentaqles.threads import detect_open_threads, deduplicate_pending
except ImportError:
    detect_open_threads = None
    deduplicate_pending = None



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

                # Count turns
                if entry.get("type") == "assistant":
                    turn_count += 1

                # Extract tool use
                tool_name = entry.get("tool_name", "")
                tool_input = entry.get("tool_input", {})

                if not isinstance(tool_input, dict):
                    continue

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
                if entry.get("type") == "human":
                    text = ""
                    content = entry.get("content", "")
                    if isinstance(content, str):
                        text = content
                    elif isinstance(content, list):
                        text = " ".join(
                            c.get("text", "") for c in content
                            if isinstance(c, dict) and c.get("type") == "text"
                        )
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


def main():
    # Read hook input
    try:
        raw = sys.stdin.read()
        data = json.loads(raw) if raw.strip() else {}
    except (json.JSONDecodeError, EOFError):
        data = {}

    cwd = data.get("cwd", os.getcwd())
    session_id = data.get("session_id", "unknown")
    transcript_path = data.get("transcript_path", "")
    reason = data.get("reason", "unknown")

    # Find client workspace
    try:
        from tentaqles.manifest.loader import load_manifest
        manifest = load_manifest(cwd)
        client_root = manifest.get("_client_root", cwd) if manifest else cwd
        client_name = manifest.get("client", "unknown") if manifest else "unknown"
        display_name = manifest.get("display_name", client_name) if manifest else "unknown"
    except Exception:
        client_root = cwd
        client_name = "unknown"
        display_name = "Unknown"

    # Parse transcript
    activity = {}
    if transcript_path and Path(transcript_path).exists():
        activity = parse_transcript(transcript_path)

    summary = build_summary(activity) if activity else f"Session ended ({reason})"

    # Redact the summary before storing (strips secrets/tokens/keys)
    try:
        summary, _ = redact_text(summary)
    except Exception:
        pass

    # Save to memory
    try:
        from tentaqles.memory.store import MemoryStore

        store = MemoryStore(client_root)

        # Start a retroactive session (if none was started by the preamble hook)
        try:
            store.start_session(
                tags=[reason, client_name],
                metadata={"session_id": session_id, "auto": True},
            )
        except Exception:
            pass

        # Record file touches
        if activity:
            for fp in activity.get("files_edited", []):
                try:
                    store.touch(fp, "file", "edit", weight=1.5)
                except Exception:
                    pass
            for fp in activity.get("files_created", []):
                try:
                    store.touch(fp, "file", "create", weight=2.0)
                except Exception:
                    pass
            for fp in activity.get("files_read", []):
                try:
                    store.touch(fp, "file", "read", weight=0.5)
                except Exception:
                    pass

        # Detect open threads from transcript (F4) and record as pending items
        if (
            transcript_path
            and Path(transcript_path).exists()
            and detect_open_threads is not None
            and deduplicate_pending is not None
        ):
            try:
                candidates = detect_open_threads(transcript_path)
                if candidates:
                    try:
                        existing = store.get_open_pending()
                    except Exception:
                        existing = []
                    try:
                        new_threads = deduplicate_pending(candidates, existing)
                    except Exception:
                        new_threads = candidates
                    for thread in new_threads:
                        try:
                            store.add_pending(
                                description=thread["description"],
                                priority=thread.get("priority", "medium"),
                            )
                        except Exception:
                            pass
            except Exception:
                pass  # Never crash session end because of thread detection

        # End session with summary
        store.end_session(summary, tags=[reason, client_name])

        # F7: memory consolidation
        try:
            from tentaqles.memory.consolidator import MemoryConsolidator
            MemoryConsolidator(store).maybe_compact()
        except Exception:
            pass

        # F10: workspace profile regen if stale
        try:
            from tentaqles.memory.profiler import WorkspaceProfiler
            prof = WorkspaceProfiler(store, client_root)
            if prof.is_stale():
                prof.generate()
        except Exception:
            pass

        # F11: cross-workspace pattern detection (async, >7d stale)
        try:
            import subprocess, sys as _sys, time
            from tentaqles.config import data_dir
            patterns_path = os.path.join(data_dir(), "metagraph", "patterns.json")
            stale = (not os.path.exists(patterns_path)) or (time.time() - os.path.getmtime(patterns_path) > 7 * 86400)
            if stale:
                script = os.path.join(os.path.dirname(os.path.abspath(__file__)), "pattern-cron.py")
                subprocess.Popen([_sys.executable, script], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except Exception:
            pass

        # Update meta-memory
        try:
            from tentaqles.memory.meta import MetaMemory
            meta = MetaMemory()
            active_nodes = store.get_active_nodes(limit=10)
            stats = store.stats()
            meta.update_workspace(
                client_name,
                display_name,
                str(client_root),
                summary,
                [n["node_id"] for n in active_nodes],
                session_count=stats.get("sessions", 0),
                total_touches=stats.get("touches", 0),
            )
            meta.close()
        except Exception:
            pass

        store.close()

    except Exception:
        # Memory save failed — nothing we can do in a SessionEnd hook.
        # The incremental PostToolUse captures are the fallback.
        pass


if __name__ == "__main__":
    main()
