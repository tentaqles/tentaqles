"""Atoms: an error paired with the command that fixed it.

The first thing anyone asks a project memory is "I saw this error before, what
did I do?". Decisions do not hold that; a session summary rarely does. So the
store keeps the last failing shell commands of a session and, when a later
command with the same executable succeeds, writes one *fix atom*:
"<error> -- fixed by: <command>". Atoms are searchable like everything else
and the newest few are shown at session start.
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone

import pytest

from tentaqles.memory.store import MemoryStore


@pytest.fixture
def store(tmp_path, monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: None)

    def _no_backend(self):
        raise RuntimeError("fastembed is not installed")

    monkeypatch.setattr(MemoryStore, "_get_emb", _no_backend)
    s = MemoryStore(tmp_path)
    s.start_session()
    yield s
    s.close()


def _atoms(store):
    return store._conn.execute("SELECT kind, text, command, error FROM atoms ORDER BY created_at").fetchall()


def test_success_after_a_matching_failure_records_a_fix_atom(store):
    store.record_tool_failure("pytest tests/test_x.py", "ModuleNotFoundError: No module named 'yaml'")

    aid = store.record_tool_success("pip install pyyaml && pytest tests/test_x.py")

    assert aid
    (atom,) = _atoms(store)
    kind, text, command, error = atom
    assert kind == "fix"
    assert "ModuleNotFoundError: No module named 'yaml'" in text
    assert "pip install pyyaml && pytest tests/test_x.py" in text
    assert command == "pip install pyyaml && pytest tests/test_x.py"
    assert error.startswith("ModuleNotFoundError")


def test_the_failure_is_consumed_by_its_fix(store):
    store.record_tool_failure("go test ./...", "FAIL: TestX (0.00s)")
    assert store.record_tool_success("go test ./...")
    assert store.record_tool_success("go test ./...") is None
    assert len(_atoms(store)) == 1


def test_success_without_a_prior_failure_records_nothing(store):
    assert store.record_tool_success("ls -la") is None
    assert _atoms(store) == []


def test_unrelated_success_leaves_the_failure_open(store):
    store.record_tool_failure("go test ./...", "FAIL: TestX (0.00s)")

    assert store.record_tool_success("ls -la") is None
    assert store.record_tool_success("git status") is None
    assert store.record_tool_success("cd internal && go test ./...")

    assert len(_atoms(store)) == 1


def test_executable_matching_ignores_env_prefixes_and_cd(store):
    store.record_tool_failure("FOO=1 python -m pytest", "AssertionError: boom")

    assert store.record_tool_success("cd plugin && FOO=2 python -m pytest -q")


def test_failures_older_than_an_hour_are_not_paired(store):
    store.record_tool_failure("npm test", "Error: Cannot find module 'left-pad'")
    stale = (datetime.now(timezone.utc) - timedelta(hours=2)).isoformat()
    store._conn.execute("UPDATE tool_failures SET created_at=?", (stale,))
    store._conn.commit()

    assert store.record_tool_success("npm test") is None
    assert _atoms(store) == []
    assert store._conn.execute("SELECT COUNT(*) FROM tool_failures").fetchone()[0] == 0, "stale failures are pruned"


def test_only_the_latest_failure_of_an_executable_is_kept(store):
    store.record_tool_failure("pytest", "AssertionError: first")
    store.record_tool_failure("pytest", "AssertionError: second")

    store.record_tool_success("pytest")

    (atom,) = _atoms(store)
    assert "second" in atom[1] and "first" not in atom[1]


def test_error_text_is_redacted_and_bounded(store):
    long_error = "api_key=sk_abcdef1234567890XYZ leaked\n" + "x" * 2000
    store.record_tool_failure("curl https://api.example.com", long_error)

    store.record_tool_success("curl https://api.example.com -H auth")

    (atom,) = _atoms(store)
    assert "sk_abcdef1234567890XYZ" not in atom[1]
    assert "[REDACTED" in atom[1]
    assert len(atom[3]) <= 400


def test_fix_atoms_are_searchable(store):
    store.record_tool_failure("pytest", "ModuleNotFoundError: No module named 'yaml'")
    store.record_tool_success("pip install pyyaml && pytest")

    results = store.search_memory("ModuleNotFoundError yaml")

    assert results and results[0]["type"] == "fix"
    assert "pip install pyyaml" in results[0]["text"]


def test_recent_fixes_are_listed(store):
    store.record_tool_failure("pytest", "AssertionError: a")
    store.record_tool_success("pytest -k a")
    store.record_tool_failure("go build ./...", "undefined: foo")
    store.record_tool_success("go build ./...")

    fixes = store.get_recent_fixes(limit=5)

    assert [f["error"][:9] for f in fixes] == ["undefined", "Assertion"]
    assert set(fixes[0]) >= {"id", "created_at", "error", "command", "text"}


def test_recent_fixes_appear_in_the_context_summary(store):
    store.record_tool_failure("pytest", "ModuleNotFoundError: No module named 'yaml'")
    store.record_tool_success("pip install pyyaml && pytest")

    summary = store.get_context_summary()

    assert "## Recent fixes" in summary
    assert "No module named 'yaml'" in summary
    assert "pip install pyyaml" in summary


def test_context_summary_without_fixes_has_no_section(store):
    assert "Recent fixes" not in store.get_context_summary()
