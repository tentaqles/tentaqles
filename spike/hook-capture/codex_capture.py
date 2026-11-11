#!/usr/bin/env python3
"""Codex hook capture: append one hook payload (read from stdin) as one line to <capture-dir>/<Event>.jsonl.

Usage (written into a scratch config.toml by codex_setup.py):
    python -I codex_capture.py <EventName> <capture-dir>

It prints nothing and exits 0, except for the probes below, which a person asks for on purpose by
putting a marker word in a shell command:

    PROBE_EXIT2  the PostToolUse hook writes a line to stderr and exits 2
    PROBE_PRINT  the PostToolUse hook prints a line of plain text to stdout
    PROBE_SLOW   the PostToolUse hook starts a child process that writes the marker file
                 probe-child-<n>.txt after 15 seconds, then sleeps 10 seconds itself (its timeout is 5)

On SessionEnd it writes the payload, sleeps 5 seconds (its timeout is 3), then writes
SessionEnd-late.txt: the file exists only if Codex let the hook run past its timeout.

Once per process it also records the names (never the values) of environment variables whose
name contains CODEX, to env-names.txt.

On PostToolUse for Bash it looks for the call's tool_use_id in the session file (transcript_path)
and writes to link.jsonl only where it was found (entry types and key paths), whether a
CommandExecution item with an exit_code holds it, and whether that code is zero. If not found at
once, it looks again after 0.5 seconds.
"""

import json
import os
import re
import subprocess
import sys
import time

EVENT = re.compile(r"[A-Za-z0-9]{1,40}")


def append(path, text):
    with open(path, "a", encoding="utf-8", newline="\n") as fh:
        fh.write(text + "\n")


def command_of(payload):
    try:
        data = json.loads(payload)
    except ValueError:
        return ""
    tool_input = data.get("tool_input") if isinstance(data, dict) else None
    if isinstance(tool_input, dict) and isinstance(tool_input.get("command"), (str, list)):
        c = tool_input["command"]
        return c if isinstance(c, str) else " ".join(str(x) for x in c)
    return ""


def where_id(obj, wanted, path, found):
    """Add the dotted key path of every string equal to wanted. Key names only."""
    if isinstance(obj, dict):
        for k, v in obj.items():
            where_id(v, wanted, f"{path}.{k}" if path else str(k), found)
    elif isinstance(obj, list):
        for v in obj:
            where_id(v, wanted, path + "[]", found)
    elif obj == wanted:
        found.add(path)


def scan_transcript(path, tool_use_id):
    """What the session file holds for this call at this moment. Names, booleans and counts only."""
    out = {"places": [], "command_item": False, "exit": "none", "lines": 0}
    places = set()
    with open(path, encoding="utf-8", errors="replace") as fh:
        for line in fh:
            out["lines"] += 1
            if tool_use_id not in line:
                continue
            try:
                entry = json.loads(line)
            except ValueError:
                continue
            payload = entry.get("payload") if isinstance(entry, dict) else None
            kind = f"{entry.get('type')}/{payload.get('type') if isinstance(payload, dict) else '-'}"
            found = set()
            where_id(entry, tool_use_id, "", found)
            places.update(f"{kind}:{p}" for p in found)
            item = payload.get("item") if isinstance(payload, dict) else None
            if isinstance(item, dict) and item.get("type") == "CommandExecution" and isinstance(item.get("exit_code"), int):
                out["command_item"] = True
                out["exit"] = "zero" if item["exit_code"] == 0 else "nonzero"
    out["places"] = sorted(places)
    return out


def link_probe(payload, folder):
    """Feature 010: can the hook find this call's exit code in the session file? Writes link.jsonl."""
    try:
        data = json.loads(payload)
    except ValueError:
        return
    if not isinstance(data, dict) or data.get("tool_name") != "Bash":
        return
    tool_use_id = data.get("tool_use_id")
    transcript = data.get("transcript_path")
    record = {"has_id": isinstance(tool_use_id, str) and bool(tool_use_id),
              "has_transcript": isinstance(transcript, str) and os.path.isfile(transcript or "")}
    home = os.environ.get("CODEX_HOME", "")
    if record["has_transcript"] and home:
        record["under_codex_home"] = os.path.normcase(os.path.abspath(transcript)).startswith(
            os.path.normcase(os.path.abspath(home)) + os.sep)
    if record["has_id"] and record["has_transcript"]:
        record["at_once"] = scan_transcript(transcript, tool_use_id)
        if not record["at_once"]["command_item"]:
            time.sleep(0.5)
            record["after_500ms"] = scan_transcript(transcript, tool_use_id)
    append(os.path.join(folder, "link.jsonl"), json.dumps(record))


def main(argv):
    if len(argv) != 3 or not EVENT.fullmatch(argv[1]) or not argv[2]:
        return 0
    event, folder = argv[1], argv[2]
    os.makedirs(folder, exist_ok=True)
    payload = sys.stdin.buffer.read().decode("utf-8", "replace")
    append(os.path.join(folder, event + ".jsonl"), payload.replace("\r", " ").replace("\n", " "))

    names = sorted(k for k in os.environ if "CODEX" in k.upper())
    append(os.path.join(folder, "env-names.txt"), " ".join(names))

    if event == "SessionEnd":
        time.sleep(5)
        append(os.path.join(folder, "SessionEnd-late.txt"), "late")
        return 0
    if event != "PostToolUse":
        return 0
    link_probe(payload, folder)
    command = command_of(payload)
    if "PROBE_EXIT2" in command:
        sys.stderr.write("probe: this hook exited 2\n")
        return 2
    if "PROBE_PRINT" in command:
        sys.stdout.write("probe: this hook printed plain text\n")
        return 0
    if "PROBE_SLOW" in command:
        marker = os.path.join(folder, f"probe-child-{os.getpid()}.txt")
        child = f"import time; time.sleep(15); open({marker!r}, 'w').write('child lived')"
        flags = 0
        if os.name == "nt":
            flags = subprocess.CREATE_NEW_PROCESS_GROUP
        subprocess.Popen([sys.executable, "-I", "-c", child], creationflags=flags,
                         stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        time.sleep(10)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv))
    except Exception:
        sys.exit(0)
