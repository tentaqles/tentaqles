#!/usr/bin/env python3
"""PreToolUse hook — snapshot before any Write to .tentaqles.yaml.

Reads a JSON object from stdin (the Claude Code tool_input), checks whether
the Write tool is targeting a .tentaqles.yaml file, and if so captures a
snapshot of the current manifest state.

Always exits 0 so the Write tool proceeds regardless of errors here.
"""

from __future__ import annotations

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths

setup_paths()


def _find_workspace_root(file_path: str) -> str | None:
    """Walk up from file_path to find the directory containing .tentaqles.yaml."""
    from pathlib import Path

    current = Path(file_path).resolve().parent
    for _ in range(12):
        if (current / ".tentaqles.yaml").is_file():
            return str(current)
        parent = current.parent
        if parent == current:
            break
        current = parent
    return None


def main() -> None:
    try:
        raw = sys.stdin.read()
        if not raw.strip():
            sys.exit(0)

        tool_input = json.loads(raw)

        # Only act when writing a manifest file
        file_path: str = tool_input.get("file_path", "") or ""
        if not file_path.endswith(".tentaqles.yaml"):
            sys.exit(0)

        workspace_root = _find_workspace_root(file_path)
        if workspace_root is None:
            # file_path IS the manifest — treat its parent as root
            from pathlib import Path

            workspace_root = str(Path(file_path).resolve().parent)

        # Load current manifest before the write happens
        manifest: dict = {}
        try:
            import yaml  # type: ignore
            from pathlib import Path

            manifest_file = Path(workspace_root) / ".tentaqles.yaml"
            if manifest_file.is_file():
                with open(manifest_file, "r", encoding="utf-8") as f:
                    loaded = yaml.safe_load(f)
                if isinstance(loaded, dict):
                    manifest = loaded
        except Exception:
            pass

        from tentaqles.snapshots import SnapshotManager

        mgr = SnapshotManager(workspace_root)
        mgr.capture(reason="manifest-edit", manifest_data=manifest)

    except Exception:
        pass  # Never block the Write tool

    sys.exit(0)


if __name__ == "__main__":
    main()
