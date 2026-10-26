#!/usr/bin/env python3
"""PreToolUse hook: a headless run never edits the client manifest.

Editing `.tentaqles.yaml` moves its trust hash, which blocks the workspace
until a human runs `tq allow`. When TENTAQLES_HEADLESS=1, exit 2 with a message
that points at the proposal-file path when:

- an Edit, Write, MultiEdit or NotebookEdit targets the manifest. The name is
  compared the way Windows resolves it: case-insensitively, without trailing
  dots and spaces or an NTFS stream suffix (`::$DATA`), and through the real
  path, so an 8.3 short name (`TENTAQ~1.YAM`) of an existing file matches too;
- a Bash command names the manifest together with something that writes
  (a redirect, sed -i, tee, mv, cp, rm, an interpreter, PowerShell writers).
  Reading it (cat, grep) is allowed.

The runner also re-checks trust before every round, so this guard is the early,
cheap stop, not the only one.

Stdlib only, no network; the only file access is resolving the target path.
Any error allows (exit 0): this guard must never stall a run.

Exit codes: 0 allow, 2 block (message on stderr).
"""

import json
import os
import re
import sys

MANIFEST = ".tentaqles.yaml"
FILE_TOOLS = ("Edit", "Write", "MultiEdit", "NotebookEdit")
MESSAGE = (
    "BLOCKED: a headless run must not edit .tentaqles.yaml (it changes the "
    "manifest trust hash and blocks the workspace until a human runs `tq allow`). "
    "Write the proposed change to docs/privacy-manifest-proposal.yaml, record a "
    "human decision (build-gate ask ... --kind question, or a NEEDS-DECISION line "
    "in docs/privacy.md), and let the human review it, apply it and run "
    "`tq allow <workspace>`."
)
# A Bash command that names the manifest and does one of these writes it, or may.
WRITES = re.compile(
    r"(?<![<>&0-9])>{1,2}|\bsed\b[^|;&]*\s-i|\btee\b|\bmv\b|\bcp\b|\brm\b|\bdd\b|\btruncate\b"
    r"|\bpython[0-9.]*\b|\bpy\b|\bperl\b|\bruby\b|\bnode\b|\bed\b|\byq\b|\binstall\b"
    r"|\bgit\s+(checkout|restore|apply|stash|reset)\b"
    r"|set-content|add-content|out-file|new-item|move-item|copy-item|remove-item|\[io\.file\]",
    re.I,
)


def _name(path: str) -> str:
    base = path.replace("\\", "/").rsplit("/", 1)[-1]
    base = base.split(":", 1)[0] if ":" in base else base  # NTFS stream suffix
    return base.rstrip(" .").casefold()


def is_manifest(path: str) -> bool:
    if _name(path) == MANIFEST:
        return True
    try:  # an 8.3 short name or a link resolves to the real name
        return _name(os.path.realpath(path)) == MANIFEST
    except Exception:
        return False


def bash_writes_manifest(command: str) -> bool:
    low = command.casefold()
    if "tentaqles.yaml" not in low and "tentaq~" not in low:
        return False
    return bool(WRITES.search(command))


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
        if tool == "Bash":
            cmd = ti.get("command") or ""
            if isinstance(cmd, str) and bash_writes_manifest(cmd):
                sys.stderr.write(MESSAGE + "\n")
                sys.exit(2)
            sys.exit(0)
        path = ti.get("file_path") or ti.get("notebook_path") or ti.get("path") or ""
        if not isinstance(path, str) or not path:
            sys.exit(0)
        if (not tool or tool in FILE_TOOLS) and is_manifest(path):
            sys.stderr.write(MESSAGE + "\n")
            sys.exit(2)
    except SystemExit:
        raise
    except Exception:
        pass
    sys.exit(0)


if __name__ == "__main__":
    main()
