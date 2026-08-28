#!/usr/bin/env python3
"""
Fallback identity guard (PreToolUse / Bash).

This script only runs when the `tq` binary is NOT installed: `tq_hook.sh`
resolves `tq` first and exec's `tq claude-hook pre-tool-use`, and only falls
back here (with TQ_FALLBACK=1) when no binary is found. It is deliberately
fail-closed and dependency-free: it does not read manifests, does not shell
out to git/gh, and does not import `tentaqles.*`.

Rule: block any remote mutation — `git push|fetch|pull|clone` in ANY chained
git segment, any `gh` invocation, any cloud CLI (az aws gcloud gsutil bq
doctl). Everything else is allowed.

Exit codes:
  0 — allow
  2 — BLOCK (message on stderr, starts with "BLOCKED: ")
"""

import json
import re
import sys

INSTALL_URL = "https://github.com/tentaqles/tentaqles#install"

# Mirrors internal/guard/guard.go sepPattern: classic separators, line breaks,
# and command-substitution/grouping openers.
_SEP = r"&&|&|\|\||;|\||\n|\r|\$\(|`|\(|\)|\{|\}"
_SEP_RE = re.compile(_SEP)

CLOUD_CLIS = ("az", "aws", "gcloud", "gsutil", "bq", "doctl")
REMOTE_GIT_SUBS = ("push", "fetch", "pull", "clone")


def _command_starts_with(command: str, prefix: str) -> bool:
    """Check if the command starts with a given CLI tool name."""
    # Match the tool name at word boundary: "git commit" matches "git", "github" does not
    pattern = r"(?:^|" + _SEP + r")\s*" + re.escape(prefix.strip()) + r"(?:\s|$)"
    return bool(re.search(pattern, command))


def _git_subcommands(command: str):
    """Yield the sub-word of every git invocation in command (mirrors gitSegments)."""
    for seg in _SEP_RE.split(command):
        fields = seg.split()
        if not fields or fields[0] != "git":
            continue
        i = 1
        while i < len(fields):
            arg = fields[i]
            if arg in ("-C", "-c"):
                i += 2
                continue
            if arg.startswith("-"):
                i += 1
                continue
            yield arg
            break


def _is_remote_mutation(command: str) -> bool:
    if _command_starts_with(command, "gh"):
        return True
    for cli in CLOUD_CLIS:
        if _command_starts_with(command, cli):
            return True
    for sub in _git_subcommands(command):
        if sub in REMOTE_GIT_SUBS:
            return True
    return False


def main() -> None:
    try:
        raw = sys.stdin.read()
    except Exception:
        raw = "{}"

    try:
        payload = json.loads(raw) if raw.strip() else {}
    except (json.JSONDecodeError, TypeError, ValueError):
        payload = {}
    if not isinstance(payload, dict):
        payload = {}

    tool_input = payload.get("tool_input", {})
    if isinstance(tool_input, str):
        try:
            tool_input = json.loads(tool_input)
        except (json.JSONDecodeError, TypeError, ValueError):
            tool_input = {}
    if not isinstance(tool_input, dict):
        tool_input = {}

    command = tool_input.get("command", "")
    if not command or not isinstance(command, str):
        sys.exit(0)

    if _is_remote_mutation(command):
        print(
            f"BLOCKED: tq is not installed; refusing '{command}' without a "
            f"verified identity. Install tq: {INSTALL_URL}",
            file=sys.stderr,
        )
        sys.exit(2)

    sys.exit(0)


if __name__ == "__main__":
    main()
