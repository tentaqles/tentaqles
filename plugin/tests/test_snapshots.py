"""Tests for tentaqles.snapshots.manager — Feature 9: Time-Travel Snapshots."""

from __future__ import annotations

import hashlib
import json
import time
from pathlib import Path

import pytest

from tentaqles.snapshots.manager import SnapshotManager, _dict_diff


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def workspace(tmp_path: Path) -> Path:
    """A minimal fake workspace directory."""
    (tmp_path / ".tentaqles.yaml").write_text(
        "schema: tentaqles-client-v1\nclient: test\n", encoding="utf-8"
    )
    return tmp_path


@pytest.fixture
def mgr(workspace: Path) -> SnapshotManager:
    return SnapshotManager(workspace, max_snapshots=5)


MANIFEST_A = {"schema": "tentaqles-client-v1", "client": "acme", "email": "a@acme.com"}
MANIFEST_B = {"schema": "tentaqles-client-v1", "client": "acme", "email": "b@acme.com", "extra": "new"}


# ---------------------------------------------------------------------------
# capture
# ---------------------------------------------------------------------------


def test_capture_writes_valid_json(mgr: SnapshotManager, workspace: Path) -> None:
    path = mgr.capture(reason="manual", manifest_data=MANIFEST_A)
    assert path.exists()
    data = json.loads(path.read_text(encoding="utf-8"))
    assert data["schema"] == "tentaqles-snapshot-v1"
    assert data["reason"] == "manual"
    assert data["manifest"] == MANIFEST_A
    assert "captured_at" in data
    assert "manifest_hash" in data


def test_capture_hash_matches_manifest(mgr: SnapshotManager) -> None:
    path = mgr.capture(reason="test", manifest_data=MANIFEST_A)
    data = json.loads(path.read_text(encoding="utf-8"))
    expected_hash = hashlib.sha256(
        json.dumps(MANIFEST_A, sort_keys=True).encode()
    ).hexdigest()
    assert data["manifest_hash"] == expected_hash


def test_capture_stores_context_data(mgr: SnapshotManager) -> None:
    ctx = {"sessions": 10, "open_pending": 2}
    path = mgr.capture(reason="test", manifest_data=MANIFEST_A, context_data=ctx)
    data = json.loads(path.read_text(encoding="utf-8"))
    assert data["context"] == ctx


def test_capture_creates_snapshots_dir(tmp_path: Path) -> None:
    workspace = tmp_path / "ws"
    workspace.mkdir()
    mgr = SnapshotManager(workspace)
    snap_dir = workspace / ".claude" / "snapshots"
    assert not snap_dir.exists()
    mgr.capture(reason="init", manifest_data={})
    assert snap_dir.exists()


def test_capture_prunes_after_write(tmp_path: Path) -> None:
    """Capture should prune so that only max_snapshots remain."""
    workspace = tmp_path / "ws"
    workspace.mkdir()
    mgr = SnapshotManager(workspace, max_snapshots=3)
    for i in range(5):
        mgr.capture(reason=f"step-{i}", manifest_data={"i": i})
        time.sleep(0.01)  # ensure distinct filenames
    snaps = list((workspace / ".claude" / "snapshots").glob("*.json"))
    assert len(snaps) == 3


# ---------------------------------------------------------------------------
# list_snapshots
# ---------------------------------------------------------------------------


def test_list_returns_newest_first(mgr: SnapshotManager) -> None:
    for i in range(3):
        mgr.capture(reason=f"r{i}", manifest_data={"i": i})
        time.sleep(0.01)
    snapshots = mgr.list_snapshots()
    assert len(snapshots) == 3
    # Verify newest-first ordering by captured_at
    timestamps = [s["captured_at"] for s in snapshots]
    assert timestamps == sorted(timestamps, reverse=True)


def test_list_returns_required_keys(mgr: SnapshotManager) -> None:
    mgr.capture(reason="manual", manifest_data=MANIFEST_A)
    snap = mgr.list_snapshots()[0]
    assert {"path", "captured_at", "reason", "manifest_hash"} <= snap.keys()


def test_list_empty_when_no_snapshots(workspace: Path) -> None:
    mgr = SnapshotManager(workspace)
    assert mgr.list_snapshots() == []


# ---------------------------------------------------------------------------
# restore
# ---------------------------------------------------------------------------


def test_restore_returns_correct_manifest(mgr: SnapshotManager) -> None:
    path = mgr.capture(reason="test", manifest_data=MANIFEST_A)
    stem = path.stem
    result = mgr.restore(stem)
    assert result == MANIFEST_A


def test_restore_prefix_match(mgr: SnapshotManager) -> None:
    path = mgr.capture(reason="test", manifest_data=MANIFEST_A)
    prefix = path.stem[:16]  # e.g. "2026-04-12T14-30"
    result = mgr.restore(prefix)
    assert result == MANIFEST_A


def test_restore_raises_for_unknown_identifier(mgr: SnapshotManager) -> None:
    with pytest.raises(FileNotFoundError):
        mgr.restore("9999-no-such-snapshot")


def test_restore_round_trip_integrity(mgr: SnapshotManager) -> None:
    """Round-trip: capture then restore should yield identical dict."""
    original = {"schema": "tentaqles-client-v1", "client": "round-trip", "nested": {"a": 1}}
    path = mgr.capture(reason="round-trip", manifest_data=original)
    restored = mgr.restore(path.stem)
    assert restored == original


# ---------------------------------------------------------------------------
# prune
# ---------------------------------------------------------------------------


def test_prune_keeps_exactly_n(workspace: Path) -> None:
    mgr = SnapshotManager(workspace, max_snapshots=10)
    for i in range(7):
        mgr.capture(reason=f"r{i}", manifest_data={"i": i})
        time.sleep(0.01)
    deleted = mgr.prune(keep_last=4)
    assert deleted == 3
    remaining = list((workspace / ".claude" / "snapshots").glob("*.json"))
    assert len(remaining) == 4


def test_prune_noop_when_under_limit(mgr: SnapshotManager) -> None:
    mgr.capture(reason="only", manifest_data=MANIFEST_A)
    deleted = mgr.prune(keep_last=5)
    assert deleted == 0


def test_prune_returns_zero_no_dir(tmp_path: Path) -> None:
    mgr = SnapshotManager(tmp_path / "empty_ws", max_snapshots=5)
    assert mgr.prune() == 0


# ---------------------------------------------------------------------------
# diff
# ---------------------------------------------------------------------------


def test_diff_added_removed_changed(mgr: SnapshotManager) -> None:
    path_a = mgr.capture(reason="before", manifest_data=MANIFEST_A)
    time.sleep(0.01)
    path_b = mgr.capture(reason="after", manifest_data=MANIFEST_B)
    result = mgr.diff(path_a.stem, path_b.stem)

    assert "extra" in result["added"]
    assert result["added"]["extra"] == "new"
    assert result["changed"]["email"] == ("a@acme.com", "b@acme.com")
    assert result["removed"] == {}


def test_diff_no_changes_same_manifest(mgr: SnapshotManager) -> None:
    path_a = mgr.capture(reason="a", manifest_data=MANIFEST_A)
    time.sleep(0.01)
    path_b = mgr.capture(reason="b", manifest_data=MANIFEST_A)
    result = mgr.diff(path_a.stem, path_b.stem)
    assert result == {"added": {}, "removed": {}, "changed": {}}


def test_dict_diff_unit() -> None:
    old = {"a": 1, "b": 2, "c": 3}
    new = {"b": 99, "c": 3, "d": 4}
    result = _dict_diff(old, new)
    assert result["removed"] == {"a": 1}
    assert result["added"] == {"d": 4}
    assert result["changed"] == {"b": (2, 99)}
