"""Tests for scripts/snapshot-guard.py — PreToolUse hook."""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _run_guard(stdin_payload: dict | str, workspace: Path | None = None) -> subprocess.CompletedProcess:
    """Invoke snapshot-guard.py with the given stdin payload.

    Returns the CompletedProcess so callers can inspect returncode / stdout / stderr.
    """
    scripts_dir = Path(__file__).resolve().parent.parent / "scripts"
    guard_script = scripts_dir / "snapshot-guard.py"

    if isinstance(stdin_payload, dict):
        stdin_text = json.dumps(stdin_payload)
    else:
        stdin_text = stdin_payload

    return subprocess.run(
        [sys.executable, str(guard_script)],
        input=stdin_text,
        capture_output=True,
        text=True,
    )


# ---------------------------------------------------------------------------
# Tests: always exits 0
# ---------------------------------------------------------------------------


def test_guard_exits_0_non_manifest_path(tmp_path: Path) -> None:
    payload = {"tool_name": "Write", "file_path": str(tmp_path / "src" / "main.py")}
    result = _run_guard(payload)
    assert result.returncode == 0


def test_guard_exits_0_manifest_path(tmp_path: Path) -> None:
    """Even when processing a manifest write, guard must exit 0."""
    # Create a fake workspace with a .tentaqles.yaml
    manifest_path = tmp_path / ".tentaqles.yaml"
    manifest_path.write_text(
        "schema: tentaqles-client-v1\nclient: test\n", encoding="utf-8"
    )
    payload = {"tool_name": "Write", "file_path": str(manifest_path)}
    result = _run_guard(payload)
    assert result.returncode == 0


def test_guard_exits_0_empty_stdin() -> None:
    result = _run_guard("")
    assert result.returncode == 0


def test_guard_exits_0_malformed_json() -> None:
    result = _run_guard("not valid json {{")
    assert result.returncode == 0


# ---------------------------------------------------------------------------
# Tests: snapshot written vs. not written
# ---------------------------------------------------------------------------


def test_guard_no_snapshot_for_non_manifest(tmp_path: Path) -> None:
    """Writing a non-manifest file must not create any snapshot."""
    payload = {"tool_name": "Write", "file_path": str(tmp_path / "README.md")}
    _run_guard(payload, workspace=tmp_path)
    snap_dir = tmp_path / ".claude" / "snapshots"
    assert not snap_dir.exists() or not list(snap_dir.glob("*.json"))


def test_guard_writes_snapshot_for_manifest_file(tmp_path: Path) -> None:
    """Writing .tentaqles.yaml must produce a snapshot JSON file."""
    manifest_path = tmp_path / ".tentaqles.yaml"
    manifest_path.write_text(
        "schema: tentaqles-client-v1\nclient: guarded\n", encoding="utf-8"
    )
    payload = {"tool_name": "Write", "file_path": str(manifest_path)}
    result = _run_guard(payload)
    assert result.returncode == 0

    snap_dir = tmp_path / ".claude" / "snapshots"
    snapshots = list(snap_dir.glob("*.json"))
    assert len(snapshots) == 1

    data = json.loads(snapshots[0].read_text(encoding="utf-8"))
    assert data["reason"] == "manifest-edit"
    assert data["schema"] == "tentaqles-snapshot-v1"


def test_guard_snapshot_contains_manifest_data(tmp_path: Path) -> None:
    """Snapshot captured by guard should include the pre-write manifest content."""
    manifest_path = tmp_path / ".tentaqles.yaml"
    manifest_path.write_text(
        "schema: tentaqles-client-v1\nclient: before-edit\nemail: old@test.com\n",
        encoding="utf-8",
    )
    payload = {"tool_name": "Write", "file_path": str(manifest_path)}
    _run_guard(payload)

    snap_dir = tmp_path / ".claude" / "snapshots"
    snap_data = json.loads(list(snap_dir.glob("*.json"))[0].read_text(encoding="utf-8"))
    assert snap_data["manifest"].get("client") == "before-edit"
    assert snap_data["manifest"].get("email") == "old@test.com"
