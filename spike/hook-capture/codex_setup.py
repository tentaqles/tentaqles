#!/usr/bin/env python3
"""Write a scratch CODEX_HOME whose config.toml runs codex_capture.py for every Codex hook event.

Usage: python codex_setup.py <scratch-folder>

It writes <scratch-folder>/codex-home/config.toml and makes <scratch-folder>/.capture. With
--mcp it also registers the made-up MCP server mcp_fail_server.py as "capture". It never reads
or copies a login: the person logs in to the scratch home themselves. If the config file already
exists, it prints one line and exits 1 without changing anything.

The event list and the hook syntax are from the Codex hooks page (learn.chatgpt.com/docs/hooks.md,
read 2026-11-10). SessionEnd and Interrupt allow a timeout of at most 3 seconds.
"""

import os
import sys

EVENTS = [
    "SessionStart", "SessionEnd", "SubagentStart", "PreToolUse", "PermissionRequest", "PostToolUse",
    "PreModelRequest", "PostModelRequest", "PreCompact", "PostCompact", "UserPromptSubmit",
    "SubagentStop", "Stop", "Interrupt",
]
SHORT = {"SessionEnd": 3, "Interrupt": 3}


def forward(path):
    return os.path.abspath(path).replace("\\", "/")


def toml_string(s):
    return "'" + s + "'" if "'" not in s else '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'


def word(path):
    """A path as one command word: bare when it has no blank, else in double quotes."""
    return f'"{path}"' if " " in path else path


def main(argv):
    args = argv[1:]
    mcp = "--mcp" in args
    args = [a for a in args if a != "--mcp"]
    if len(args) != 1:
        print("usage: python codex_setup.py <scratch-folder> [--mcp]", file=sys.stderr)
        return 2
    scratch = os.path.abspath(args[0])
    home = os.path.join(scratch, "codex-home")
    config = os.path.join(home, "config.toml")
    if os.path.exists(config):
        print(f"Config already exists, nothing changed: {forward(config)}")
        return 1
    here = os.path.dirname(os.path.abspath(__file__))
    python = forward(sys.executable)
    capture = forward(os.path.join(here, "codex_capture.py"))
    capture_dir = forward(os.path.join(scratch, ".capture"))
    os.makedirs(home, exist_ok=True)
    os.makedirs(capture_dir, exist_ok=True)

    lines = ["# Scratch Codex config for the hook capture (spike/hook-capture). Delete with the folder.", ""]
    for event in EVENTS:
        command = f"{word(python)} -I {word(capture)} {event} {word(capture_dir)}"
        lines += [
            f"[[hooks.{event}]]",
            "",
            f"[[hooks.{event}.hooks]]",
            'type = "command"',
            f"command = {toml_string(command)}",
            f"timeout = {SHORT.get(event, 5)}",
            "",
        ]
    if mcp:
        server = forward(os.path.join(here, "mcp_fail_server.py"))
        lines += [
            "[mcp_servers.capture]",
            f"command = {toml_string(python)}",
            f"args = [{toml_string('-I')}, {toml_string(server)}]",
            "",
        ]
    with open(config, "w", encoding="utf-8", newline="\n") as fh:
        fh.write("\n".join(lines))

    print(f"Wrote the scratch Codex config: {forward(config)}")
    print(f"Captures will land in: {capture_dir}")
    print(f"Next: set CODEX_HOME to {forward(home)}, log in there, and run the steps you were given.")
    print(f"Then: python {forward(os.path.join(here, 'reduce.py'))} {capture_dir}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
