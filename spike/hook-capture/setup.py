#!/usr/bin/env python3
"""Write <scratch-folder>/.claude/settings.json so that ten hook events run capture.sh.

Usage: python setup.py <scratch-folder>

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
    if len(argv) != 2:
        print("usage: python setup.py <scratch-folder>", file=sys.stderr)
        return 2

    scratch = os.path.abspath(argv[1])
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

    with open(settings_path, "w", encoding="utf-8", newline="\n") as fh:
        json.dump({"hooks": hooks}, fh, indent=2)
        fh.write("\n")

    print(f"Wrote hook settings: {forward(settings_path)}")
    print(f"Captures will land in: {capture_dir}")
    print("Next:")
    print(f"  1. Start Claude Code in {forward(scratch)} and run the steps in the README.")
    print(f"  2. python {reduce_py} {capture_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
