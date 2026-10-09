"""bootstrap.py must never run pip inside the SessionStart hook.

Missing deps -> take a lock, start a detached `--worker`, print one line and
return. The worker installs and releases the lock. A second session while the
lock is live starts nothing.
"""

from __future__ import annotations

import importlib.util
import json
import os
import time
from pathlib import Path

import pytest

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
SCRIPT = PLUGIN_ROOT / "scripts" / "bootstrap.py"
HOOKS = PLUGIN_ROOT / "hooks" / "hooks.json"


@pytest.fixture
def bootstrap(tmp_path, monkeypatch):
    spec = importlib.util.spec_from_file_location("tq_bootstrap_under_test", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(tmp_path / "data"))
    monkeypatch.setattr(mod, "_check_core_available", lambda lib_dir: False)
    calls = {"spawn": 0, "pip": 0}

    def fake_spawn():
        calls["spawn"] += 1

    def fake_pip(target, packages):
        calls["pip"] += 1
        return True

    monkeypatch.setattr(mod, "_spawn_worker", fake_spawn)
    monkeypatch.setattr(mod, "_run_pip_install", fake_pip)
    mod.calls = calls
    return mod


def test_hook_spawns_worker_and_never_installs_inline(bootstrap, tmp_path):
    start = time.monotonic()
    notice = bootstrap.run_hook()
    assert time.monotonic() - start < 5
    assert bootstrap.calls == {"spawn": 1, "pip": 0}
    assert notice and "\n" not in notice and "background" in notice
    assert (tmp_path / "data" / bootstrap.LOCK_NAME).exists()


def test_second_session_does_not_spawn_while_locked(bootstrap):
    bootstrap.run_hook()
    notice = bootstrap.run_hook()
    assert bootstrap.calls["spawn"] == 1
    assert notice  # still tells the user what is going on


def test_stale_lock_is_taken_over(bootstrap, tmp_path):
    lock = tmp_path / "data" / bootstrap.LOCK_NAME
    lock.parent.mkdir(parents=True)
    lock.write_text("{}", encoding="utf-8")
    old = time.time() - bootstrap.LOCK_STALE_SECONDS - 60
    os.utime(lock, (old, old))
    bootstrap.run_hook()
    assert bootstrap.calls["spawn"] == 1


def test_spawn_failure_releases_lock(bootstrap, tmp_path, monkeypatch):
    def boom():
        raise OSError("no")

    monkeypatch.setattr(bootstrap, "_spawn_worker", boom)
    assert bootstrap.run_hook() == ""
    assert not (tmp_path / "data" / bootstrap.LOCK_NAME).exists()


def test_worker_installs_writes_sentinel_and_releases_lock(bootstrap, tmp_path, monkeypatch):
    bootstrap.run_hook()
    # Keep stderr on the test's stream rather than the log file redirect.
    monkeypatch.setattr(bootstrap, "_redirect_log", lambda d: None)
    assert bootstrap.run_worker() is True
    data = tmp_path / "data"
    assert bootstrap.calls["pip"] == 1
    assert (data / ".bootstrap-complete").read_text(encoding="utf-8") == "installed"
    assert not (data / bootstrap.LOCK_NAME).exists()


def test_worker_releases_lock_on_failure(bootstrap, tmp_path, monkeypatch):
    bootstrap.run_hook()
    monkeypatch.setattr(bootstrap, "_redirect_log", lambda d: None)
    monkeypatch.setattr(bootstrap, "_run_pip_install", lambda t, p: False)
    assert bootstrap.run_worker() is False
    assert not (tmp_path / "data" / bootstrap.LOCK_NAME).exists()
    assert not (tmp_path / "data" / ".bootstrap-complete").exists()


def test_deps_present_is_a_silent_no_op(bootstrap, monkeypatch):
    monkeypatch.setattr(bootstrap, "_check_core_available", lambda lib_dir: True)
    assert bootstrap.run_hook() == ""
    assert bootstrap.calls["spawn"] == 0


def test_spawn_worker_uses_detach_helper(monkeypatch):
    """The real _spawn_worker hands `bootstrap.py --worker` to _detach."""
    spec = importlib.util.spec_from_file_location("tq_bootstrap_spawn", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    monkeypatch.syspath_prepend(str(SCRIPT.parent))
    import _detach

    seen = {}
    monkeypatch.setattr(
        _detach, "spawn_detached", lambda argv, stdin_data=b"": seen.setdefault("argv", argv)
    )
    mod._spawn_worker()
    assert seen["argv"][-1] == "--worker"
    assert Path(seen["argv"][-2]).name == "bootstrap.py"


def test_hooks_json_bootstrap_timeout_is_short():
    hooks = json.loads(HOOKS.read_text(encoding="utf-8"))["hooks"]
    entries = [
        h for g in hooks["SessionStart"] for h in g["hooks"] if "bootstrap.py" in h["command"]
    ]
    assert entries and all(h.get("timeout", 600) <= 60 for h in entries)
