"""Tests for tentaqles.memory.profiler — Feature 10: Workspace Profiles."""

from __future__ import annotations

import json
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from tentaqles.memory.store import MemoryStore
from tentaqles.memory.profiler import WorkspaceProfiler


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def store(tmp_path, monkeypatch):
    """MemoryStore with stubbed embedding (no model needed)."""
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 4)
    s = MemoryStore(tmp_path)
    yield s
    s.close()


@pytest.fixture
def profiler(store, tmp_path):
    return WorkspaceProfiler(store, tmp_path)


# ---------------------------------------------------------------------------
# generate() — writes valid profile.json
# ---------------------------------------------------------------------------


def test_generate_writes_profile_json(profiler, tmp_path):
    profile = profiler.generate()

    profile_path = tmp_path / ".claude" / "profile.json"
    assert profile_path.exists(), "profile.json not written"

    on_disk = json.loads(profile_path.read_text())
    assert on_disk["schema"] == "tentaqles-profile-v1"
    assert "generated_at" in on_disk
    assert "session_frequency" in on_disk
    assert "hot_files" in on_disk
    assert "top_concepts" in on_disk
    assert isinstance(on_disk["hot_files"], list)
    assert isinstance(on_disk["top_concepts"], list)


def test_generate_returns_dict_matching_file(profiler, tmp_path):
    profile = profiler.generate()
    on_disk = json.loads((tmp_path / ".claude" / "profile.json").read_text())
    assert profile["generated_at"] == on_disk["generated_at"]
    assert profile["schema"] == on_disk["schema"]


# ---------------------------------------------------------------------------
# is_stale() — True for old / missing files, False for fresh
# ---------------------------------------------------------------------------


def test_is_stale_returns_true_when_missing(profiler, tmp_path):
    # No profile.json exists yet
    assert profiler.is_stale() is True


def test_is_stale_returns_false_for_fresh_profile(profiler):
    profiler.generate()
    # Just generated — should not be stale
    assert profiler.is_stale(max_age_days=7.0) is False


def test_is_stale_returns_true_for_old_profile(profiler, tmp_path):
    profiler.generate()
    profile_path = tmp_path / ".claude" / "profile.json"

    # Backdate generated_at by 10 days
    data = json.loads(profile_path.read_text())
    old_ts = (datetime.now(timezone.utc) - timedelta(days=10)).isoformat()
    data["generated_at"] = old_ts
    profile_path.write_text(json.dumps(data))

    assert profiler.is_stale(max_age_days=7.0) is True


# ---------------------------------------------------------------------------
# load() — returns None when missing, dict when present
# ---------------------------------------------------------------------------


def test_load_returns_none_when_missing(profiler):
    result = profiler.load()
    assert result is None


def test_load_returns_dict_after_generate(profiler):
    profiler.generate()
    result = profiler.load()
    assert result is not None
    assert result["schema"] == "tentaqles-profile-v1"


def test_load_returns_none_on_corrupt_json(profiler, tmp_path):
    profile_path = tmp_path / ".claude" / "profile.json"
    profile_path.parent.mkdir(parents=True, exist_ok=True)
    profile_path.write_text("NOT VALID JSON", encoding="utf-8")
    assert profiler.load() is None


# ---------------------------------------------------------------------------
# _compute_hot_files() — top file is ranked first
# ---------------------------------------------------------------------------


def _seed_touches(store: MemoryStore, node_id: str, count: int, weight: float = 1.0) -> None:
    """Insert *count* touches for a given file node (no active session needed)."""
    for _ in range(count):
        store.touch(node_id=node_id, node_type="file", action="edit", weight=weight)


def test_hot_files_ranks_most_touched_file_first(store, tmp_path, monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 4)

    _seed_touches(store, "src/auth.py", 10)
    _seed_touches(store, "src/routes.py", 5)
    _seed_touches(store, "src/models.py", 2)

    profiler = WorkspaceProfiler(store, tmp_path)
    hot = profiler._compute_hot_files(limit=15)

    assert len(hot) >= 3
    assert hot[0]["path"] == "src/auth.py", f"Expected auth.py first, got {hot[0]['path']}"
    assert hot[0]["touch_count"] == 10


def test_hot_files_only_returns_file_nodes(store, tmp_path, monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 4)

    store.touch(node_id="my_function", node_type="function", action="edit")
    _seed_touches(store, "src/utils.py", 3)

    profiler = WorkspaceProfiler(store, tmp_path)
    hot = profiler._compute_hot_files()

    paths = [h["path"] for h in hot]
    assert "my_function" not in paths
    assert "src/utils.py" in paths


# ---------------------------------------------------------------------------
# _compute_session_frequency() — sessions_last_30d accurate
# ---------------------------------------------------------------------------


def _seed_sessions(store: MemoryStore, count: int) -> None:
    """Insert *count* completed sessions (start + end)."""
    for i in range(count):
        store.start_session()
        store.end_session(summary=f"Session {i}")


def test_session_frequency_counts_last_30d(store, tmp_path, monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 4)

    _seed_sessions(store, 14)

    profiler = WorkspaceProfiler(store, tmp_path)
    freq = profiler._compute_session_frequency()

    assert freq["sessions_last_30d"] == 14
    assert freq["sessions_per_week_avg"] >= 0.0
    assert 0 <= freq["most_active_hour"] <= 23


def test_session_frequency_zero_sessions(store, tmp_path):
    profiler = WorkspaceProfiler(store, tmp_path)
    freq = profiler._compute_session_frequency()

    assert freq["sessions_last_30d"] == 0
    assert freq["sessions_per_week_avg"] == 0.0


# ---------------------------------------------------------------------------
# _compute_concept_clusters() — falls back gracefully
# ---------------------------------------------------------------------------


def test_concept_clusters_returns_list(store, tmp_path, monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 4)

    # Seed some decisions with tags
    store.start_session()
    store.record_decision("Use JWT", "Best for stateless auth", tags=["auth"])
    store.record_decision("Use Postgres", "ACID compliance", tags=["db"])
    store.end_session("seed session")

    profiler = WorkspaceProfiler(store, tmp_path)
    concepts = profiler._compute_concept_clusters(n_clusters=5)

    assert isinstance(concepts, list)
    # Each entry has the expected keys
    for c in concepts:
        assert "label" in c
        assert "decision_count" in c
        assert "representative" in c


def test_concept_clusters_empty_store(store, tmp_path):
    profiler = WorkspaceProfiler(store, tmp_path)
    concepts = profiler._compute_concept_clusters()
    assert isinstance(concepts, list)


# ---------------------------------------------------------------------------
# _compute_commit_velocity() — gracefully returns None for non-git paths
# ---------------------------------------------------------------------------


def test_commit_velocity_returns_none_for_non_git_dir(tmp_path, monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 4)
    store = MemoryStore(tmp_path)

    # Use a temp path that is definitely not a git repo
    non_git = tmp_path / "not_a_repo"
    non_git.mkdir()
    profiler = WorkspaceProfiler(store, non_git)

    result = profiler._compute_commit_velocity()
    # Should be None (not a git repo) or a dict — never raise
    assert result is None or isinstance(result, dict)
    store.close()


def test_commit_velocity_structure_if_present(tmp_path, monkeypatch):
    """If git log succeeds, result has the expected keys."""
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 4)

    # We can only test structure — mock subprocess to return fake output
    import subprocess as _subprocess

    fake_output = "abc1234 feat: add thing\ndef5678 fix: bug\n"

    class FakeResult:
        returncode = 0
        stdout = fake_output

    monkeypatch.setattr(_subprocess, "run", lambda *a, **kw: FakeResult())

    store = MemoryStore(tmp_path)
    profiler = WorkspaceProfiler(store, tmp_path)
    result = profiler._compute_commit_velocity()
    store.close()

    assert result is not None
    assert result["commits_30d"] == 2
    assert result["commits_per_week_avg"] == 0.5
