"""Tests for scripts/snapshot-guard.py — PreToolUse hook."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _run_guard(
    stdin_payload: dict | str,
    workspace: Path | None = None,
    env: dict | None = None,
    home: Path | None = None,
) -> subprocess.CompletedProcess:
    """Invoke snapshot-guard.py with the given stdin payload.

    The guard runs under the test's own agent marker (the suite runs inside
    Claude Code), so it ignores TQ_HOME and reads ~/.tentaqles: HOME and
    USERPROFILE are pointed at ``home`` (an empty temp dir by default) so no
    run reads the developer's real state folder.

    Returns the CompletedProcess so callers can inspect returncode / stdout / stderr.
    """
    scripts_dir = Path(__file__).resolve().parent.parent / "scripts"
    guard_script = scripts_dir / "snapshot-guard.py"

    if isinstance(stdin_payload, dict):
        stdin_text = json.dumps(stdin_payload)
    else:
        stdin_text = stdin_payload

    if home is None:
        home = Path(tempfile.mkdtemp(prefix="tq-guard-home-"))
    run_env = dict(os.environ)
    run_env["HOME"] = str(home)
    run_env["USERPROFILE"] = str(home)
    run_env["TQ_HOME"] = ""
    # The guard is a hook: it always runs under an agent marker.
    run_env["CLAUDECODE"] = "1"
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


def _protected_workspace(tmp_path: Path, email: str = "before@acme.test") -> tuple[Path, Path, Path]:
    """A marked workspace root, the home whose .tentaqles holds the state, and
    the protected manifest."""
    home = tmp_path / "home"
    tq_home = home / ".tentaqles"
    root = tmp_path / "base" / "acme"
    root.mkdir(parents=True)
    (root / ".tentaqles.yaml").write_text(
        "# marker\nschema: tentaqles-marker-v1\nclient: acme\n", encoding="utf-8"
    )
    pp = tq_home / "identities" / "acme" / "manifest.yaml"
    pp.parent.mkdir(parents=True)
    pp.write_text(
        f"schema: tentaqles-client-v2\nclient: acme\ngit:\n  email: {email}\n",
        encoding="utf-8",
    )
    # The hook resolves through the registry, as tq does.
    (tq_home / "config.yaml").write_text(f"bases:\n  - {root.parent}\n", encoding="utf-8")
    return root, home, pp


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
    root, home, _ = _protected_workspace(tmp_path)
    payload = {
        "tool_name": "Write",
        "cwd": str(root),
        "tool_input": {"file_path": str(root / ".tentaqles.yaml")},
    }
    result = _run_guard(payload, home=home)
    assert result.returncode == 0
    snaps = _snapshots(root)
    assert len(snaps) == 1
    assert snaps[0]["manifest"]["client"] == "acme"
    assert snaps[0]["manifest"]["git"]["email"] == "before@acme.test"
    assert "schema" in snaps[0]["manifest"] and snaps[0]["manifest"]["schema"] == "tentaqles-client-v2"


def test_guard_protected_manifest_write_snapshots_under_the_workspace(tmp_path: Path) -> None:
    """A Write to ~/.tentaqles/identities/<name>/manifest.yaml is a manifest
    write; the snapshot lands under the workspace root the marker names."""
    root, home, pp = _protected_workspace(tmp_path)
    payload = {"tool_name": "Write", "cwd": str(root / "src"), "tool_input": {"file_path": str(pp)}}
    result = _run_guard(payload, home=home)
    assert result.returncode == 0
    snaps = _snapshots(root)
    assert len(snaps) == 1
    assert snaps[0]["manifest"]["git"]["email"] == "before@acme.test"
    assert not (home / ".tentaqles" / "identities" / "acme" / ".claude").exists()


def test_guard_protected_manifest_write_finds_the_root_through_the_registry(tmp_path: Path) -> None:
    root, home, pp = _protected_workspace(tmp_path)
    (home / ".tentaqles" / "config.yaml").write_text(f"bases:\n  - {root.parent}\n", encoding="utf-8")
    payload = {"tool_name": "Write", "cwd": str(tmp_path), "tool_input": {"file_path": str(pp)}}
    env = {"TQ_WS": "", "TQ_WS_ROOT": ""}
    assert _run_guard(payload, env=env, home=home).returncode == 0
    assert len(_snapshots(root)) == 1


def test_guard_protected_manifest_write_without_a_root_is_a_noop(tmp_path: Path) -> None:
    home = tmp_path / "home"
    pp = home / ".tentaqles" / "identities" / "ghost" / "manifest.yaml"
    pp.parent.mkdir(parents=True)
    pp.write_text("schema: tentaqles-client-v2\nclient: ghost\n", encoding="utf-8")
    payload = {"tool_name": "Write", "cwd": str(tmp_path), "tool_input": {"file_path": str(pp)}}
    env = {"TQ_WS": "", "TQ_WS_ROOT": ""}
    assert _run_guard(payload, env=env, home=home).returncode == 0
    assert not list(tmp_path.rglob("snapshots"))


def test_guard_ignores_tq_home_under_an_agent_marker(tmp_path: Path) -> None:
    """A TQ_HOME the harness environment carries (an agent-writable settings
    env block can set it) must not point the guard at a forged state folder:
    under an agent marker the real ~/.tentaqles is read."""
    root, home, _ = _protected_workspace(tmp_path, email="real@acme.test")
    forged = tmp_path / "forged"
    fp = forged / "identities" / "acme" / "manifest.yaml"
    fp.parent.mkdir(parents=True)
    fp.write_text("schema: tentaqles-client-v2\nclient: acme\ngit:\n  email: forged@acme.test\n", encoding="utf-8")
    payload = {"tool_name": "Write", "cwd": str(root), "tool_input": {"file_path": str(root / ".tentaqles.yaml")}}
    result = _run_guard(payload, env={"TQ_HOME": str(forged), "CLAUDECODE": "1"}, home=home)
    assert result.returncode == 0
    snaps = _snapshots(root)
    assert len(snaps) == 1
    assert snaps[0]["manifest"]["git"]["email"] == "real@acme.test"


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
