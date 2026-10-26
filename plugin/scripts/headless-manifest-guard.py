#!/usr/bin/env python3
"""PreToolUse hook: a headless run never edits the client manifest.

Editing `.tentaqles.yaml` moves its trust hash, which blocks the workspace
until a human runs `tq allow`. When TENTAQLES_HEADLESS=1 and an Edit, Write or
MultiEdit targets a file named `.tentaqles.yaml`, exit 2 with a message that
points at the proposal-file path. Everything else is allowed.

Stdlib only, no network, no file access: it reads one JSON object from stdin.
Any error allows (exit 0): this guard must never stall a run.

Exit codes: 0 allow, 2 block (message on stderr).
"""

import json
import os
import sys

TOOLS = ("Edit", "Write", "MultiEdit")
MESSAGE = (
    "BLOCKED: a headless run must not edit .tentaqles.yaml (it changes the "
    "manifest trust hash and blocks the workspace until a human runs `tq allow`). "
    "Write the proposed change to docs/privacy-manifest-proposal.yaml, record a "
    "human decision (build-gate ask ... --kind question, or a NEEDS-DECISION line "
    "in docs/privacy.md), and let the human review it, apply it and run "
    "`tq allow <workspace>`."
)


def main() -> None:
    try:
        if os.environ.get("TENTAQLES_HEADLESS", "").strip() != "1":
            sys.exit(0)
        raw = sys.stdin.read()
        if not raw.strip():
            sys.exit(0)
        payload = json.loads(raw)
        if not isinstance(payload, dict):
            sys.exit(0)
        tool = payload.get("tool_name", "")
        ti = payload.get("tool_input")
        if not isinstance(ti, dict):
            ti = payload
        path = ti.get("file_path") or ti.get("path") or ""
        if not isinstance(path, str):
            sys.exit(0)
        base = path.replace("\\", "/").rsplit("/", 1)[-1]
        if base == ".tentaqles.yaml" and (not tool or tool in TOOLS):
            sys.stderr.write(MESSAGE + "\n")
            sys.exit(2)
    except SystemExit:
        raise
    except Exception:
        pass
    sys.exit(0)


if __name__ == "__main__":
    main()
