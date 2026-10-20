#!/usr/bin/env python3
"""
PostToolUse hook bridge (Bash|Edit|Write) — captures decisions and file touches.

Reads JSON from stdin (cwd, tool_name, tool_input, tool_response -- the older
flat tool_output string is still accepted), scans for decision patterns,
records file touches, and turns a failed Bash command followed by a successful
one with the same program into a fix atom.

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


# Lines that mark a shell result as failed when no exit code is available.
ERROR_LINE_RE = re.compile(
    r"^(?:Traceback \(most recent call last\)|[A-Za-z_.]*(?:Error|Exception)\b.*:|error\b|fatal:|FAIL\b|--- FAIL|"
    r"panic:|.*command not found|.*No such file or directory|.*Permission denied|npm ERR!|ERROR\b)",
    re.IGNORECASE,
)
EXIT_CODE_RE = re.compile(r"^Exit code (\d+)\s*$", re.MULTILINE)


def _response_text(resp) -> str:
    """Flatten a tool result (dict, list or string) to text for scanning."""
    if resp is None:
        return ""
    if isinstance(resp, str):
        return resp
    if isinstance(resp, dict):
        parts = []
        for key in ("stdout", "stderr", "output", "content", "error"):
            v = resp.get(key)
            if isinstance(v, str) and v:
                parts.append(v)
            elif isinstance(v, (list, dict)):
                parts.append(_response_text(v))
        return "\n".join(parts)
    if isinstance(resp, list):
        return "\n".join(_response_text(x) for x in resp)
    return str(resp)


def _first_error_line(text: str) -> str:
    for line in text.splitlines():
        line = line.strip()
        if line and ERROR_LINE_RE.match(line):
            return line[:400]
    return ""


def classify_bash_result(resp) -> tuple[str, str]:
    """("failure", excerpt) | ("success", "") | ("unknown", "") for a Bash result.

    An exit code decides when present (a field, or the "Exit code N" line the
    tool prints). Otherwise stderr/stdout lines that look like errors do. An
    interrupted command is neither.
    """
    if isinstance(resp, dict):
        if resp.get("interrupted"):
            return "unknown", ""
        for key in ("exit_code", "exitCode", "returncode", "code"):
            v = resp.get(key)
            if isinstance(v, int) and not isinstance(v, bool):
                if v == 0:
                    return "success", ""
                return "failure", _first_error_line(_response_text(resp)) or f"exit code {v}"
    text = _response_text(resp)
    m = EXIT_CODE_RE.search(text)
    if m:
        code = int(m.group(1))
        body = (text[:m.start()] + text[m.end():]).strip()
        if code == 0:
            return "success", ""
        return "failure", _first_error_line(body) or f"exit code {code}"
    line = _first_error_line(text)
    return ("failure", line) if line else ("success", "")


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
    tool_response = payload.get("tool_response", payload.get("tool_output", ""))
    tool_output = _response_text(tool_response)

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

        # MemoryStore takes the workspace root (it opens .claude/memory.db
        # itself); passing the db file made it mkdir under a file and fail.
        store = MemoryStore(client_root)
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
        # Error -> fix atoms: remember a failure, pair the next matching success.
        try:
            verdict, excerpt = classify_bash_result(tool_response)
            if verdict == "failure":
                store.record_tool_failure(command, excerpt)
            elif verdict == "success":
                store.record_tool_success(command)
        except Exception:
            pass
    else:
        combined_text = tool_output

    # Redact before scanning to ensure secrets don't match decision patterns
    # or get extracted as file paths
    try:
        combined_text, _ = redact_text(combined_text)
    except Exception:
        pass

    decision_hit = _has_decision_pattern(combined_text)
    mentioned_paths: list[str] = []
    if decision_hit:
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

    if decision_hit:
        _shadow_capture_gate(manifest, cwd, tool_name, combined_text, len(mentioned_paths))

    sys.exit(0)


def _shadow_capture_gate(manifest, cwd: str, tool_name: str, text: str, n_paths: int) -> bool:
    """Ask Jev, in a detached worker, whether the regex hit was worth keeping.

    Shadow only: the touches above already happened and are never undone.
    Returns True when a worker was started.
    """
    try:
        from tentaqles.memory import jev_gate

        if not jev_gate.should_gate(manifest):
            return False
        from _detach import spawn_detached

        return jev_gate.spawn(jev_gate.capture_payload(cwd, tool_name, text, n_paths), spawn_detached)
    except Exception:
        return False


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Never block the workflow
        sys.exit(0)
