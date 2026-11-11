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
OWN_MCP = "mcp__capture__"
MARKERS = ("CAPTURE-FAIL-MARKER", "CAPTURE-OK")
OWN_TOOL = re.compile(r"[A-Za-z0-9_.:/\-]{1,60}")
# Feature 010: the header forms of a Codex shell result (Trail-code src/trace/codex-exec.ts).
CODEX_FORMS = (
    ("Process exited with code N", re.compile(r"(?m)^Process exited with code -?[0-9]+")),
    ("Exit code: N", re.compile(r"(?m)^Exit code: -?[0-9]+")),
    ("Process running with session ID N", re.compile(r"(?m)^Process running with session ID [0-9]+")),
    ("a line that is exactly Output:", re.compile(r"(?m)^Output:\r?$")),
    ("Wall time line", re.compile(r"(?m)^Wall time: ")),
    ("Chunk ID line", re.compile(r"(?m)^Chunk ID: ")),
)


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
        # The made-up server of mcp_fail_server.py is ours, so its tool names may print, in
        # whatever form the host writes them (Claude Code: mcp__capture__<tool>).
        if "capture" in value and OWN_TOOL.fullmatch(value):
            return value
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
    # Feature 008: how a command that went to the background is told apart. Counts only, never text.
    responses = [p["tool_response"] for p in payloads if "tool_response" in p]
    if responses:
        def response_text(r):
            if isinstance(r, str):
                return r
            if isinstance(r, dict):
                return " ".join(str(v) for v in r.values() if isinstance(v, str))
            return ""
        bg = sum(1 for r in responses if "in background" in response_text(r).lower())
        lines.append(f"- tool_response says it went to the background: {bg} of {len(responses)}")
        task = sum(
            1 for r in responses
            if isinstance(r, dict) and isinstance(r.get("backgroundTaskId"), str) and r["backgroundTaskId"].strip()
        )
        lines.append(f"- tool_response has a non-empty backgroundTaskId: {task} of {len(responses)}")
    # Feature 010: how a Codex shell result states its exit. Counts only, never text.
    texts = [response_text_any(p["tool_response"]) for p in payloads if "tool_response" in p]
    texts = [t for t in texts if t]
    if texts:
        for label, pattern in CODEX_FORMS:
            hits = sum(1 for t in texts if pattern.search(t))
            if hits:
                lines.append(f"- tool_response text has {label}: {hits} of {len(texts)}")
        js = 0
        for t in texts:
            if t.lstrip().startswith("{"):
                try:
                    obj = json.loads(t)
                except (ValueError, RecursionError):
                    continue
                meta = obj.get("metadata") if isinstance(obj, dict) else None
                if isinstance(meta, dict) and isinstance(meta.get("exit_code"), int):
                    js += 1
        if js:
            lines.append(f"- tool_response text is JSON with a whole-number metadata.exit_code: {js} of {len(texts)}")
    exit_fields = sum(
        1 for p in payloads
        if isinstance(p.get("tool_response"), dict)
        and any(k in p["tool_response"] for k in ("exit_code", "exitCode", "exit"))
    )
    if exit_fields:
        lines.append(f"- tool_response object has an exit_code, exitCode or exit field: {exit_fields}")
    # Feature 009: where an MCP tool's error lands. Counts only, never text.
    own = [p for p in payloads if "capture" in str(p.get(TOOL_KEY, ""))]
    if own:
        flagged = sum(
            1 for p in own
            if isinstance(p.get("tool_response"), dict)
            and (p["tool_response"].get("isError") is True or p["tool_response"].get("is_error") is True)
        )
        lines.append(f"- tool_response is an object with isError or is_error true: {flagged} of {len(own)}")
        for marker in MARKERS:
            for field in ("tool_response", "error"):
                n = sum(1 for p in own if field in p and marker in json.dumps(p[field]))
                if n:
                    lines.append(f"- {field} holds {marker}: {n} of {len(own)}")
    for key in sorted(TEXT_KEYS + BOOL_KEYS):
        values = Counter(safe_value(key, p[key]) for p in payloads if key in p)
        if values:
            parts = ", ".join(f"{v} x{values[v]}" for v in sorted(values))
            lines.append(f"- {key}: {parts}")
    return lines


def response_text_any(r):
    """The text of a tool_response: itself when text, the text values of a list of parts, or an
    object's content, output or stdout read the same way. Used for counts only."""
    if isinstance(r, str):
        return r
    if isinstance(r, list):
        return "\n".join(x["text"] for x in r if isinstance(x, dict) and isinstance(x.get("text"), str))
    if isinstance(r, dict):
        for key in ("content", "output", "stdout"):
            if key in r:
                return response_text_any(r[key])
    return ""


def codex_lines(capture_dir):
    """Counts from the files codex_capture.py writes besides the payloads. Names, never values."""
    lines = []
    names = set()
    env = os.path.join(capture_dir, "env-names.txt")
    if os.path.isfile(env):
        with open(env, encoding="utf-8", errors="replace") as fh:
            for line in fh:
                names.update(n for n in line.split() if SAFE_KEY.fullmatch(n))
        lines.append("- environment variable names with CODEX seen by the hooks: " + (", ".join(sorted(names)) or "none"))
    files = os.listdir(capture_dir)
    probes = sum(1 for f in files if f.startswith("probe-child-"))
    lines.append(f"- PROBE_SLOW child marker files (the child outlived its hook): {probes}")
    late = os.path.isfile(os.path.join(capture_dir, "SessionEnd-late.txt"))
    lines.append(f"- SessionEnd-late.txt exists (the hook ran past its 3 s timeout): {'yes' if late else 'no'}")
    lines += link_lines(os.path.join(capture_dir, "link.jsonl"))
    return lines


PLACE = re.compile(r"[A-Za-z0-9_./:\[\]-]{1,120}")


def link_lines(path):
    """Feature 010: the session-file lookup from codex_capture.py's link probe. Counts and key paths only."""
    if not os.path.isfile(path):
        return []
    records = []
    with open(path, encoding="utf-8", errors="replace") as fh:
        for line in fh:
            try:
                r = json.loads(line)
            except ValueError:
                continue
            if isinstance(r, dict):
                records.append(r)
    n = len(records)
    lines = [f"- link probe: {n} Bash PostToolUse calls"]
    for key, label in (("has_id", "had a tool_use_id"), ("has_transcript", "had a transcript_path that exists"),
                       ("under_codex_home", "had the transcript under CODEX_HOME")):
        lines.append(f"  - {label}: {sum(1 for r in records if r.get(key) is True)} of {n}")
    for when in ("at_once", "after_500ms"):
        scans = [r[when] for r in records if isinstance(r.get(when), dict)]
        if not scans:
            continue
        found = sum(1 for s in scans if s.get("command_item") is True)
        lines.append(f"  - {when}: a CommandExecution item with this id and an exit_code: {found} of {len(scans)}")
        exits = Counter(str(s.get("exit")) for s in scans if s.get("command_item") is True)
        if exits:
            lines.append(f"  - {when}: its exit_code: " + ", ".join(f"{k} x{v}" for k, v in sorted(exits.items())))
        places = Counter(
            p for s in scans for p in s.get("places", []) if isinstance(p, str) and PLACE.fullmatch(p))
        for p, c in sorted(places.items()):
            lines.append(f"  - {when}: the id appears at {p}: {c}")
    return lines


def load_events(capture_dir):
    """Return (number of files read, {event label: {"payloads": [...], "unparsed": n}})."""
    events = {}
    files = 0
    for name in sorted(os.listdir(capture_dir)):
        path = os.path.join(capture_dir, name)
        if not name.endswith(".jsonl") or name == "link.jsonl" or not os.path.isfile(path):
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
            outside = group.startswith("mcp__") and not group.startswith(OWN_MCP)
            for field, types, present in field_rows(group_payloads, outside):
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
    if os.path.isfile(os.path.join(capture_dir, "env-names.txt")):
        sys.stdout.write("\n## Codex probes\n\n" + "\n".join(codex_lines(capture_dir)) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
