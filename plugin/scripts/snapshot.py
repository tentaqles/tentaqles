#!/usr/bin/env python3
"""Snapshot CLI — capture / list / restore / prune workspace snapshots.

Usage:
    python snapshot.py capture <workspace_path> <reason>
    python snapshot.py list    <workspace_path>
    python snapshot.py restore <workspace_path> <timestamp>
    python snapshot.py prune   <workspace_path>

Reads .tentaqles.yaml from the workspace path to supply manifest data
for captures.
"""

from __future__ import annotations

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths

setup_paths()

from pathlib import Path


def _load_yaml_manifest(workspace_path: Path) -> dict:
    """Load .tentaqles.yaml from workspace_path. Returns {} on failure."""
    try:
        import yaml  # type: ignore

        yaml_file = workspace_path / ".tentaqles.yaml"
        if yaml_file.is_file():
            with open(yaml_file, "r", encoding="utf-8") as f:
                data = yaml.safe_load(f)
            if isinstance(data, dict):
                return data
    except Exception:
        pass
    return {}


def cmd_capture(workspace_path: Path, reason: str) -> None:
    from tentaqles.snapshots import SnapshotManager

    manifest = _load_yaml_manifest(workspace_path)
    mgr = SnapshotManager(workspace_path)
    snap_path = mgr.capture(reason=reason, manifest_data=manifest)
    print(json.dumps({"status": "ok", "path": str(snap_path)}, ensure_ascii=False))


def cmd_list(workspace_path: Path) -> None:
    from tentaqles.snapshots import SnapshotManager

    mgr = SnapshotManager(workspace_path)
    snapshots = mgr.list_snapshots()
    print(json.dumps(snapshots, indent=2, ensure_ascii=False))


def cmd_restore(workspace_path: Path, timestamp: str) -> None:
    from tentaqles.snapshots import SnapshotManager

    mgr = SnapshotManager(workspace_path)
    try:
        manifest = mgr.restore(timestamp)
        print(json.dumps(manifest, indent=2, ensure_ascii=False))
    except FileNotFoundError as exc:
        print(json.dumps({"error": str(exc)}), file=sys.stderr)
        sys.exit(1)


def cmd_prune(workspace_path: Path) -> None:
    from tentaqles.snapshots import SnapshotManager

    mgr = SnapshotManager(workspace_path)
    deleted = mgr.prune(mgr.max_snapshots)
    print(json.dumps({"status": "ok", "deleted": deleted}, ensure_ascii=False))


def main() -> None:
    if len(sys.argv) < 3:
        print(
            "Usage: snapshot.py <capture|list|restore|prune> <workspace_path> [args...]",
            file=sys.stderr,
        )
        sys.exit(1)

    command = sys.argv[1]
    workspace_path = Path(sys.argv[2]).resolve()

    if command == "capture":
        if len(sys.argv) < 4:
            print("Usage: snapshot.py capture <workspace_path> <reason>", file=sys.stderr)
            sys.exit(1)
        reason = sys.argv[3]
        cmd_capture(workspace_path, reason)
    elif command == "list":
        cmd_list(workspace_path)
    elif command == "restore":
        if len(sys.argv) < 4:
            print("Usage: snapshot.py restore <workspace_path> <timestamp>", file=sys.stderr)
            sys.exit(1)
        timestamp = sys.argv[3]
        cmd_restore(workspace_path, timestamp)
    elif command == "prune":
        cmd_prune(workspace_path)
    else:
        print(f"Unknown command: {command!r}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
