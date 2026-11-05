"""Tests for scripts/snapshot-guard.py — PreToolUse hook."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _run_guard(
    stdin_payload: dict | str, workspace: Path | None = None, env: dict | None = None
) -> subprocess.CompletedProcess:
    """Invoke snapshot-guard.py with the given stdin payload.

    Returns the CompletedProcess so callers can inspect returncode / stdout / stderr.
    """
    scripts_dir = Path(__file__).resolve().parent.parent / "scripts"
    guard_script = scripts_dir / "snapshot-guard.py"

    if isinstance(stdin_payload, dict):
        stdin_text = json.dumps(stdin_payload)
    else:
        stdin_text = stdin_payload

    run_env = dict(os.environ)
    if env:
        run_env.update(env)
    return subprocess.run(
        [sys.executable, str(guard_script)],
        input=stdin_text,
        capture_output=True,
        text=True,
        env=run_env,
    )


def _snapshots(root: Path) -> list[dict]:
    snap_dir = root / ".claude" / "snapshots"
    if not snap_dir.exists():
        return []
    return [json.loads(p.read_text(encoding="utf-8")) for p in sorted(snap_dir.glob("*.json"))]


def _protected_workspace(tmp_path: Path) -> tuple[Path, Path, Path]:
    """A marked workspace root, its TQ_HOME and the protected manifest."""
    tq_home = tmp_path / "tqhome"
    root = tmp_path / "base" / "acme"
    root.mkdir(parents=True)
    (root / ".tentaqles.yaml").write_text(
        "# marker\nschema: tentaqles-marker-v1\nclient: acme\n", encoding="utf-8"
    )
    pp = tq_home / "identities" / "acme" / "manifest.yaml"
    pp.parent.mkdir(parents=True)
    pp.write_text(
        "schema: tentaqles-client-v2\nclient: acme\ngit:\n  email: before@acme.test\n",
        encoding="utf-8",
    )
    return root, tq_home, pp


# ---------------------------------------------------------------------------
# Tests: the hook payload names the file as tool_input.file_path
# ---------------------------------------------------------------------------


def test_guard_reads_tool_input_file_path(tmp_path: Path) -> None:
    """Claude Code puts the Write's arguments under tool_input; the guard must
    read file_path there (the top-level spelling is the older fallback)."""
    manifest_path = tmp_path / ".tentaqles.yaml"
    manifest_path.write_text("schema: tentaqles-client-v1\nclient: shaped\n", encoding="utf-8")
    payload = {
        "tool_name": "Write",
        "cwd": str(tmp_path),
        "tool_input": {"file_path": str(manifest_path), "content": "schema: x\n"},
    }
    result = _run_guard(payload)
    assert result.returncode == 0
    snaps = _snapshots(tmp_path)
    assert len(snaps) == 1
    assert snaps[0]["reason"] == "manifest-edit"
    assert snaps[0]["manifest"]["client"] == "shaped"
    assert not any(k.startswith("_") for k in snaps[0]["manifest"])


def test_guard_tool_input_wins_over_top_level(tmp_path: Path) -> None:
    manifest_path = tmp_path / ".tentaqles.yaml"
    manifest_path.write_text("schema: tentaqles-client-v1\nclient: inner\n", encoding="utf-8")
    payload = {
        "tool_name": "Write",
        "file_path": str(tmp_path / "README.md"),
        "tool_input": {"file_path": str(manifest_path)},
    }
    assert _run_guard(payload).returncode == 0
    assert [s["manifest"]["client"] for s in _snapshots(tmp_path)] == ["inner"]


def test_guard_tool_input_non_manifest_is_a_noop(tmp_path: Path) -> None:
    payload = {"tool_name": "Write", "tool_input": {"file_path": str(tmp_path / "src" / "main.py")}}
    assert _run_guard(payload).returncode == 0
    assert _snapshots(tmp_path) == []


# ---------------------------------------------------------------------------
# Tests: the protected layout (ADR-0002)
# ---------------------------------------------------------------------------


def test_guard_marker_write_snapshots_the_protected_manifest(tmp_path: Path) -> None:
    """A Write to the marker snapshots the manifest tq reads, under the root."""
    root, tq_home, _ = _protected_workspace(tmp_path)
    payload = {
        "tool_name": "Write",
        "cwd": str(root),
        "tool_input": {"file_path": str(root / ".tentaqles.yaml")},
    }
    result = _run_guard(payload, env={"TQ_HOME": str(tq_home)})
    assert result.returncode == 0
    snaps = _snapshots(root)
    assert len(snaps) == 1
    assert snaps[0]["manifest"]["client"] == "acme"
    assert snaps[0]["manifest"]["git"]["email"] == "before@acme.test"
    assert "schema" in snaps[0]["manifest"] and snaps[0]["manifest"]["schema"] == "tentaqles-client-v2"


def test_guard_protected_manifest_write_snapshots_under_the_workspace(tmp_path: Path) -> None:
    """A Write to $TQ_HOME/identities/<name>/manifest.yaml is a manifest write;
    the snapshot lands under the workspace root the marker names."""
    root, tq_home, pp = _protected_workspace(tmp_path)
    payload = {"tool_name": "Write", "cwd": str(root / "src"), "tool_input": {"file_path": str(pp)}}
    result = _run_guard(payload, env={"TQ_HOME": str(tq_home)})
    assert result.returncode == 0
    snaps = _snapshots(root)
    assert len(snaps) == 1
    assert snaps[0]["manifest"]["git"]["email"] == "before@acme.test"
    assert not (tq_home / "identities" / "acme" / ".claude").exists()


def test_guard_protected_manifest_write_finds_the_root_through_the_registry(tmp_path: Path) -> None:
    root, tq_home, pp = _protected_workspace(tmp_path)
    (tq_home / "config.yaml").write_text(f"bases:\n  - {root.parent}\n", encoding="utf-8")
    payload = {"tool_name": "Write", "cwd": str(tmp_path), "tool_input": {"file_path": str(pp)}}
    env = {"TQ_HOME": str(tq_home), "TQ_WS": "", "TQ_WS_ROOT": ""}
    assert _run_guard(payload, env=env).returncode == 0
    assert len(_snapshots(root)) == 1


def test_guard_protected_manifest_write_without_a_root_is_a_noop(tmp_path: Path) -> None:
    tq_home = tmp_path / "tqhome"
    pp = tq_home / "identities" / "ghost" / "manifest.yaml"
    pp.parent.mkdir(parents=True)
    pp.write_text("schema: tentaqles-client-v2\nclient: ghost\n", encoding="utf-8")
    payload = {"tool_name": "Write", "cwd": str(tmp_path), "tool_input": {"file_path": str(pp)}}
    env = {"TQ_HOME": str(tq_home), "TQ_WS": "", "TQ_WS_ROOT": ""}
    assert _run_guard(payload, env=env).returncode == 0
    assert not list(tmp_path.rglob("snapshots"))


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
