#!/usr/bin/env python3
"""
PostToolUse hook bridge (Bash|Edit|Write) — captures decisions and file touches.

Reads JSON from stdin (cwd, tool_name, tool_input, tool_output), scans for
decision patterns, and records file touches in the memory store.

Always exits 0 — never blocks the workflow.
"""

import os
import sys

# Bootstrap sys.path for plugin imports (tentaqles.* + bootstrapped deps)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import io
import json
import os
import re
import sys

try:
    from tentaqles.privacy import redact_text
except Exception:
    def redact_text(text, **kwargs):
        return (text, [])


# Patterns that indicate a decision or discovery worth capturing
DECISION_PATTERNS = [
    re.compile(r"chose\s+\S+\s+over\s+\S+", re.IGNORECASE),
    re.compile(r"decided\s+to\b", re.IGNORECASE),
    re.compile(r"switched\s+from\b", re.IGNORECASE),
    re.compile(r"fixed\s+by\b", re.IGNORECASE),
    re.compile(r"root\s+cause\b", re.IGNORECASE),
    re.compile(r"\bworkaround\b", re.IGNORECASE),
    re.compile(r"the\s+issue\s+was\b", re.IGNORECASE),
    re.compile(r"\bdiscovered\b", re.IGNORECASE),
    re.compile(r"\bturns\s+out\b", re.IGNORECASE),
]

# Pattern to extract file paths from text (Unix or Windows style)
FILE_PATH_PATTERN = re.compile(
    r"(?:[A-Za-z]:[/\\]|/)"            # Drive letter or leading slash
    r"[^\s:*?\"<>|,;()'\[\]{}]+"       # Path characters
    r"\.[a-zA-Z0-9]{1,10}"             # File extension
)


def _has_decision_pattern(text: str) -> bool:
    """Check if text contains any decision/discovery pattern."""
    for pattern in DECISION_PATTERNS:
        if pattern.search(text):
            return True
    return False


def _extract_file_paths(text: str) -> list[str]:
    """Extract plausible file paths from text."""
    matches = FILE_PATH_PATTERN.findall(text)
    # Normalize to forward slashes
    return [m.replace("\\", "/") for m in matches]


def main() -> None:
    # Read stdin
    try:
        raw = sys.stdin.read()
    except Exception:
        sys.exit(0)

    try:
        payload = json.loads(raw) if raw.strip() else {}
    except (json.JSONDecodeError, TypeError):
        sys.exit(0)

    cwd = payload.get("cwd", os.getcwd())
    tool_name = payload.get("tool_name", "")
    tool_input = payload.get("tool_input", {})
    tool_output = payload.get("tool_output", "")

    if isinstance(tool_input, str):
        try:
            tool_input = json.loads(tool_input)
        except (json.JSONDecodeError, TypeError):
            tool_input = {}

    if not isinstance(tool_input, dict):
        tool_input = {}

    if not isinstance(tool_output, str):
        tool_output = str(tool_output) if tool_output else ""

    # Load manifest to find client root
    try:
        from tentaqles.manifest.loader import load_manifest
    except ImportError:
        sys.exit(0)

    manifest = load_manifest(cwd)
    if manifest is None:
        sys.exit(0)

    client_root = manifest.get("_client_root", "")
    if not client_root:
        sys.exit(0)

    # Check if memory.db exists
    from pathlib import Path

    db_path = Path(client_root) / ".claude" / "memory.db"
    if not db_path.is_file():
        sys.exit(0)

    try:
        from tentaqles.memory.store import MemoryStore

        store = MemoryStore(str(db_path))
    except ImportError:
        sys.exit(0)
    except Exception:
        sys.exit(0)

    paths_to_touch: list[str] = []

    # For Edit/Write tools, touch the edited file
    if tool_name in ("Edit", "Write"):
        file_path = tool_input.get("file_path", "")
        if file_path:
            paths_to_touch.append(file_path.replace("\\", "/"))

    # Scan output and input for decision patterns
    combined_text = ""
    if tool_name == "Bash":
        command = tool_input.get("command", "")
        combined_text = command + "\n" + tool_output
    else:
        combined_text = tool_output

    # Redact before scanning to ensure secrets don't match decision patterns
    # or get extracted as file paths
    try:
        combined_text, _ = redact_text(combined_text)
    except Exception:
        pass

    if _has_decision_pattern(combined_text):
        # Extract file paths mentioned in the text
        mentioned_paths = _extract_file_paths(combined_text)
        paths_to_touch.extend(mentioned_paths)

    # Deduplicate and touch
    seen: set[str] = set()
    for p in paths_to_touch:
        normalized = p.lower()
        if normalized not in seen:
            seen.add(normalized)
            try:
                store.touch(p)
            except Exception:
                pass

    sys.exit(0)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Never block the workflow
        sys.exit(0)
