#!/usr/bin/env python3
"""Write <scratch-folder>/.claude/settings.json so that ten hook events run capture.sh.

Usage: python setup.py <scratch-folder> [--mcp]

With --mcp it also writes <scratch-folder>/.mcp.json with the made-up server
mcp_fail_server.py (named "capture") and turns on that folder's project MCP
servers, so its three tools can be called in the session.

If the settings file already exists, this prints one line and exits 1
without changing anything.
"""

import json
import os
import sys

EVENTS = [
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
]


def forward(path):
    """Absolute path with forward slashes, so it works in Git Bash on Windows."""
    return os.path.abspath(path).replace("\\", "/")


def main(argv):
    args = argv[1:]
    mcp = "--mcp" in args
    args = [a for a in args if a != "--mcp"]
    if len(args) != 1:
        print("usage: python setup.py <scratch-folder> [--mcp]", file=sys.stderr)
        return 2

    scratch = os.path.abspath(args[0])
    settings_path = os.path.join(scratch, ".claude", "settings.json")
    if os.path.exists(settings_path):
        print(f"Settings file already exists, nothing changed: {forward(settings_path)}")
        return 1

    here = os.path.dirname(os.path.abspath(__file__))
    capture_sh = forward(os.path.join(here, "capture.sh"))
    reduce_py = forward(os.path.join(here, "reduce.py"))
    capture_dir = forward(os.path.join(scratch, ".capture"))

    os.makedirs(os.path.dirname(settings_path), exist_ok=True)
    os.makedirs(os.path.join(scratch, ".capture"), exist_ok=True)

    hooks = {}
    for event in EVENTS:
        command = f'bash "{capture_sh}" {event} "{capture_dir}"'
        hooks[event] = [
            {"hooks": [{"type": "command", "command": command, "timeout": 5}]}
        ]

    settings = {"hooks": hooks}
    if mcp:
        settings["enableAllProjectMcpServers"] = True
        server = forward(os.path.join(here, "mcp_fail_server.py"))
        mcp_path = os.path.join(scratch, ".mcp.json")
        with open(mcp_path, "w", encoding="utf-8", newline="\n") as fh:
            json.dump({"mcpServers": {"capture": {"command": sys.executable.replace("\\", "/"),
                                                  "args": ["-I", server]}}}, fh, indent=2)
            fh.write("\n")
        print(f"Wrote the made-up MCP server: {forward(mcp_path)}")

    with open(settings_path, "w", encoding="utf-8", newline="\n") as fh:
        json.dump(settings, fh, indent=2)
        fh.write("\n")

    print(f"Wrote hook settings: {forward(settings_path)}")
    print(f"Captures will land in: {capture_dir}")
    print("Next:")
    print(f"  1. Start Claude Code in {forward(scratch)} and run the steps in the README.")
    print(f"  2. python {reduce_py} {capture_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
