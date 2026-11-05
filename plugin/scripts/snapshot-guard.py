#!/usr/bin/env python3
"""PreToolUse hook — snapshot before any Write to a workspace manifest.

Reads the Claude Code hook payload from stdin (a JSON object whose
``tool_input.file_path`` names the file; the bare ``file_path`` of an older
shape is accepted too), checks whether the Write targets a manifest, and if
so captures a snapshot of the manifest as it is before the write.

A manifest is either the old-layout ``<root>/.tentaqles.yaml``, the
name-only marker of the protected layout (ADR-0002), or the protected copy
``$TQ_HOME/identities/<name>/manifest.yaml``. In every case the snapshot
holds the manifest tq actually reads (the protected copy for a marked
workspace) and is stored under the workspace root.

Always exits 0 so the Write tool proceeds regardless of errors here.
"""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths

setup_paths()

ROOT_FILE = ".tentaqles.yaml"


def _payload_file_path(payload: dict) -> str:
    """The written file: ``tool_input.file_path`` (the hook shape), else the
    top-level ``file_path`` (the older shape)."""
    tool_input = payload.get("tool_input")
    if isinstance(tool_input, dict):
        fp = tool_input.get("file_path")
        if isinstance(fp, str) and fp:
            return fp
    fp = payload.get("file_path")
    return fp if isinstance(fp, str) else ""


def _read_yaml(path: Path) -> dict:
    try:
        import yaml  # type: ignore

        if path.is_file():
            with open(path, "r", encoding="utf-8") as f:
                loaded = yaml.safe_load(f)
            if isinstance(loaded, dict):
                return loaded
    except Exception:
        pass
    return {}


def _public(manifest: dict) -> dict:
    """The manifest without the loader's bookkeeping keys."""
    return {k: v for k, v in manifest.items() if not str(k).startswith("_")}


def _target(file_path: str, cwd: str | None) -> tuple[str | None, dict]:
    """The workspace root to store the snapshot under and the manifest to
    snapshot, or (None, {}) when the write is not a manifest write."""
    from tentaqles.manifest import loader

    p = Path(file_path)
    if p.name == ROOT_FILE:
        root = p.resolve().parent
        manifest = loader.load_manifest(root)
        if manifest is None:
            # Not loadable as a manifest (an unknown schema, say): keep the
            # raw root file so the snapshot still records what was there.
            return str(root), _read_yaml(p)
        return str(root), _public(manifest)
    if loader.is_protected_manifest_path(p):
        name = p.parent.name
        root = loader.workspace_root_for(name, cwd=cwd)
        if root is None:
            return None, {}
        return str(root), _read_yaml(p)
    return None, {}


def main() -> None:
    try:
        raw = sys.stdin.read()
        if not raw.strip():
            sys.exit(0)

        payload = json.loads(raw)
        if not isinstance(payload, dict):
            sys.exit(0)

        file_path = _payload_file_path(payload)
        if not file_path:
            sys.exit(0)

        cwd = payload.get("cwd")
        workspace_root, manifest = _target(file_path, cwd if isinstance(cwd, str) else None)
        if workspace_root is None:
            sys.exit(0)

        from tentaqles.snapshots import SnapshotManager

        mgr = SnapshotManager(workspace_root)
        mgr.capture(reason="manifest-edit", manifest_data=manifest)

    except SystemExit:
        raise
    except Exception:
        pass  # Never block the Write tool

    sys.exit(0)


if __name__ == "__main__":
    main()
