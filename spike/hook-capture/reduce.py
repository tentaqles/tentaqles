#!/usr/bin/env python3
"""Reduce hook captures to a Markdown report of field names, JSON types and counts.

Usage: python reduce.py <capture-dir>

The report never prints a payload value. The only values it prints come from
safe_value(), which keeps a short fixed vocabulary.
"""

import json
import os
import re
import sys
from collections import Counter, defaultdict

TEXT_KEYS = ("hook_event_name", "source", "reason", "permission_mode", "notification_type", "matcher")
BOOL_KEYS = ("is_interrupt", "stop_hook_active")
TOOL_KEY = "tool_name"
NESTED_KEYS = ("tool_input", "tool_response")
TYPE_ORDER = ("string", "number", "boolean", "null", "object", "array")
EXPECTED_EVENTS = (
    "SessionStart",
    "UserPromptSubmit",
    "PreToolUse",
    "PermissionRequest",
    "PermissionDenied",
    "PostToolUse",
    "PostToolUseFailure",
    "Notification",
    "Stop",
    "SessionEnd",
)

SAFE_TEXT = re.compile(r"[A-Za-z0-9_\- \[\]]{0,40}")
SAFE_KEY = re.compile(r"[A-Za-z0-9_.\-]{1,40}")
EVENT_NAME = re.compile(r"[A-Za-z0-9_\-]{1,40}")
EXIT_LINE = re.compile(r"Exit code [0-9]+")


def _safe_text(value):
    if isinstance(value, str) and SAFE_TEXT.fullmatch(value):
        return value
    return "<other>"


def safe_key(name):
    """A key name that is safe to print: a short identifier. Anything else is data."""
    if isinstance(name, str) and SAFE_KEY.fullmatch(name):
        return name
    return "<other key>"


def safe_value(key, value):
    """The only text of a payload value that this report may print.

    Returns None for a key whose values are never printed.
    """
    if key == TOOL_KEY:
        if not isinstance(value, str):
            return "<other>"
        if value.startswith("mcp__"):
            return "mcp__*"
        return _safe_text(value)
    if key in TEXT_KEYS:
        return _safe_text(value)
    if key in BOOL_KEYS:
        if isinstance(value, bool):
            return "true" if value else "false"
        return "<other>"
    return None


def json_type(value):
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, (int, float)):
        return "number"
    if isinstance(value, str):
        return "string"
    if isinstance(value, dict):
        return "object"
    return "array"


def type_text(types):
    return "/".join(t for t in TYPE_ORDER if t in types)


def key_stats(objects):
    """(objects having each printed key name, JSON types seen per printed key name).

    Every unsafe key prints as "<other key>", so that name counts each object once
    if it has at least one unsafe key.
    """
    seen = Counter()
    types = defaultdict(set)
    for obj in objects:
        names = set()
        for key, value in obj.items():
            name = safe_key(key)
            names.add(name)
            types[name].add(json_type(value))
        seen.update(names)
    return seen, types


def is_json_object(text):
    try:
        return isinstance(json.loads(text), dict)
    except (ValueError, RecursionError):
        return False


def tool_group(payload):
    if TOOL_KEY not in payload:
        return "(no tool)"
    return safe_value(TOOL_KEY, payload[TOOL_KEY])


def field_rows(payloads, mcp):
    """(field, types, present in) rows for one tool group.

    mcp: the group is an MCP tool. Its tool_response is an outside server's answer,
    so only the top-level tool_response row is kept and its keys are not listed.
    """
    total = len(payloads)
    seen, types = key_stats(payloads)
    rows = [(name, type_text(types[name]), f"{seen[name]} of {total}") for name in sorted(seen)]

    for parent in NESTED_KEYS:
        if mcp and parent == "tool_response":
            continue
        objects = [p[parent] for p in payloads if isinstance(p.get(parent), dict)]
        strings = [p[parent] for p in payloads if isinstance(p.get(parent), str)]
        nested_seen, nested_types = key_stats(objects)
        for name in sorted(nested_seen):
            rows.append(
                (f"{parent}.{name}", type_text(nested_types[name]), f"{nested_seen[name]} of {total}")
            )
        if strings:
            holds = sum(1 for s in strings if is_json_object(s))
            rows.append((f"{parent} (string)", "string", f"holds JSON object: {holds} of {len(strings)}"))
    return rows


def extra_lines(payloads):
    """Counts-only lines under a group's table, plus the allowed-value lines."""
    lines = []
    total = len(payloads)
    errors = [p["error"] for p in payloads if isinstance(p.get("error"), str)]
    if errors:
        exit_count = sum(1 for e in errors if EXIT_LINE.fullmatch(e.split("\n", 1)[0]))
        empty = sum(1 for e in errors if e == "")
        lengths = [len(e) for e in errors]
        lines.append(
            f"- error (string in {len(errors)} of {total} payloads): first line is "
            f"`Exit code N` in {exit_count}; empty in {empty}; "
            f"length in characters: smallest {min(lengths)}, largest {max(lengths)}"
        )
    for key in sorted(TEXT_KEYS + BOOL_KEYS):
        values = Counter(safe_value(key, p[key]) for p in payloads if key in p)
        if values:
            parts = ", ".join(f"{v} x{values[v]}" for v in sorted(values))
            lines.append(f"- {key}: {parts}")
    return lines


def load_events(capture_dir):
    """Return (number of files read, {event label: {"payloads": [...], "unparsed": n}})."""
    events = {}
    files = 0
    for name in sorted(os.listdir(capture_dir)):
        path = os.path.join(capture_dir, name)
        if not name.endswith(".jsonl") or not os.path.isfile(path):
            continue
        stem = name[: -len(".jsonl")]
        event = stem if EVENT_NAME.fullmatch(stem) else "<other>"
        bucket = events.setdefault(event, {"payloads": [], "unparsed": 0})
        files += 1
        try:
            with open(path, encoding="utf-8", errors="replace") as fh:
                for line in fh:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        payload = json.loads(line)
                    except (ValueError, RecursionError):
                        payload = None
                    if isinstance(payload, dict):
                        bucket["payloads"].append(payload)
                    else:
                        bucket["unparsed"] += 1
        except OSError:
            print("could not read one capture file", file=sys.stderr)
    return files, events


def summary_line(event, payloads):
    groups = Counter(tool_group(p) for p in payloads)
    tools = ", ".join(f"{g} x{groups[g]}" for g in sorted(groups)) or "none"
    return f"- {event}: {len(payloads)} payloads; tools: {tools}"


def render(files, events):
    total = sum(len(e["payloads"]) for e in events.values())
    out = ["# Hook capture report", "", f"Files: {files}; payloads: {total}"]

    for event in sorted(events):
        bucket = events[event]
        payloads = bucket["payloads"]
        out += ["", f"## {event}", "", f"Payloads: {len(payloads)}"]
        if bucket["unparsed"]:
            out.append(f"Lines that did not parse as a JSON object: {bucket['unparsed']}")

        groups = defaultdict(list)
        for payload in payloads:
            groups[tool_group(payload)].append(payload)

        for group in sorted(groups):
            group_payloads = groups[group]
            out += [
                "",
                f"### {group} ({len(group_payloads)} payloads)",
                "",
                "| field | JSON type(s) | present in |",
                "|---|---|---|",
            ]
            for field, types, present in field_rows(group_payloads, group.startswith("mcp__")):
                out.append(f"| {field} | {types} | {present} |")
            extra = extra_lines(group_payloads)
            if extra:
                out += [""] + extra

    names = list(EXPECTED_EVENTS) + sorted(e for e in events if e not in EXPECTED_EVENTS)
    summary = [summary_line(name, events[name]["payloads"] if name in events else []) for name in names]
    out += ["", "## Summary for the builders", ""] + summary
    return "\n".join(out) + "\n"


def main(argv):
    if len(argv) != 2:
        print("usage: python reduce.py <capture-dir>", file=sys.stderr)
        return 2
    capture_dir = argv[1]
    if not os.path.isdir(capture_dir):
        print("capture folder not found", file=sys.stderr)
        return 1
    files, events = load_events(capture_dir)
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    sys.stdout.write(render(files, events))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
