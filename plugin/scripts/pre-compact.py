#!/usr/bin/env python3
"""Print the workspace state block that should survive context compaction.

No longer registered as a PreCompact hook: Claude Code does not hand a
PreCompact hook's stdout to the model, so the block never reached it. The
SessionStart hook (session-preamble.py) now emits the same block when
`source == "compact"`, which is injected into the post-compaction context.

Kept as a manual/debug entry point:

    echo '{"cwd": "."}' | bash scripts/tq_run.sh pre-compact.py
"""

import os
import sys

from _path import setup_paths
setup_paths()

import json


def main() -> None:
    try:
        raw = sys.stdin.read()
    except Exception:
        raw = "{}"
    try:
        payload = json.loads(raw) if raw.strip() else {}
    except (json.JSONDecodeError, TypeError):
        payload = {}

    cwd = payload.get("cwd", os.getcwd())
    try:
        from tentaqles.memory.compact_context import build_compact_block

        block = build_compact_block(cwd, header="# Tentaqles context preservation (PreCompact)")
    except Exception:
        block = ""
    if block:
        print(block)


if __name__ == "__main__":
    main()
