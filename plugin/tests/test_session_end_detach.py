"""SessionEnd must return immediately and do its saving in a detached worker.

Claude Code aborts every SessionEnd hook still running when one shared budget
expires: 1500ms by default. A plugin's `timeout` in hooks.json does not raise
it. The save itself (transcript parse, embedding, consolidation) has no upper
bound, so no amount of launcher tuning keeps it inside that budget. The hook
therefore hands the payload to a detached process and exits; the save lands a
moment later, after Claude Code has already gone.
"""

from __future__ import annotations

import json
import os
import sqlite3
import subprocess
import sys
import time
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
SCRIPTS = PLUGIN_ROOT / "scripts"
HOOK = SCRIPTS / "session-end.py"


def _wait_for(predicate, timeout: float):
    """Poll until predicate() returns something truthy, or give up."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = predicate()
        if result:
            return result
        time.sleep(0.1)
    return predicate()


def _hook_env(tmp_path: Path) -> dict:
    env = dict(os.environ)
    # The suite may itself run inside a Claude Code session whose environment
    # points at an *installed* plugin — pin both to this checkout and to tmp.
    env["CLAUDE_PLUGIN_ROOT"] = str(PLUGIN_ROOT)
    env["CLAUDE_PLUGIN_DATA"] = str(tmp_path / "data")
    env["PYTHONPATH"] = str(PLUGIN_ROOT)
    return env


def _ended_session(db: Path, session_id: str):
    """The ended session row recorded for session_id, if it exists yet."""
    if not db.exists():
        return None
    try:
        conn = sqlite3.connect(f"file:{db.as_posix()}?mode=ro", uri=True)
        try:
            return conn.execute(
                "SELECT summary FROM sessions WHERE metadata LIKE ? AND ended_at IS NOT NULL",
                (f"%{session_id}%",),
            ).fetchone()
        finally:
            conn.close()
    except sqlite3.Error:
        return None  # schema not created yet, or mid-write


def _payload(workspace: Path, session_id: str) -> bytes:
    return json.dumps(
        {
            "session_id": session_id,
            "cwd": str(workspace),
            "transcript_path": "",
            "reason": "prompt_input_exit",
            "hook_event_name": "SessionEnd",
        }
    ).encode("utf-8")


def test_spawn_detached_returns_while_the_child_is_still_working(tmp_path: Path) -> None:
    """The caller must be able to exit — pipes included — before the child ends.

    Run through a real parent process with captured output, exactly as Claude
    Code runs a hook: if the child inherited the parent's stdout/stderr, the
    capture would not see EOF until the child finished, and the "hook" would
    not be over until then either.
    """
    marker = tmp_path / "child-finished.txt"
    child = tmp_path / "child.py"
    child.write_text(
        "import sys, time\n"
        "data = sys.stdin.buffer.read()\n"
        "time.sleep(3)\n"
        "open(sys.argv[1], 'wb').write(data)\n",
        encoding="utf-8",
    )
    parent = tmp_path / "parent.py"
    parent.write_text(
        "import sys\n"
        f"sys.path.insert(0, {str(SCRIPTS)!r})\n"
        "from _detach import spawn_detached\n"
        f"spawn_detached([sys.executable, {str(child)!r}, {str(marker)!r}], stdin_data=b'payload-from-hook')\n"
        "print('parent done')\n",
        encoding="utf-8",
    )

    done = subprocess.run([sys.executable, str(parent)], capture_output=True, timeout=120)

    assert done.returncode == 0, done.stderr.decode(errors="replace")
    assert b"parent done" in done.stdout
    assert not marker.exists(), "the parent only returned once the child had finished"
    assert _wait_for(marker.exists, timeout=60), "the child died with its parent"
    assert marker.read_bytes() == b"payload-from-hook"


def test_hook_returns_while_store_is_busy_and_save_lands_afterwards(tmp_path: Path) -> None:
    """The hook's own lifetime must not be what the save depends on.

    The store is write-locked for as long as the hook runs. A hook that saves
    inline waits on that lock, gives up, and the session is never recorded. A
    hook that hands off returns regardless, and the worker records the session
    once the lock is gone.
    """
    workspace = tmp_path / "ws"
    db = workspace / ".claude" / "memory.db"
    db.parent.mkdir(parents=True)
    session_id = "detach-test-session"

    lock = sqlite3.connect(str(db), isolation_level=None)
    lock.execute("BEGIN IMMEDIATE")
    try:
        done = subprocess.run(
            [sys.executable, str(HOOK)],
            input=_payload(workspace, session_id),
            capture_output=True,
            env=_hook_env(tmp_path),
            cwd=str(tmp_path),
            timeout=120,
        )
    finally:
        lock.execute("ROLLBACK")
        lock.close()

    assert done.returncode == 0, done.stderr.decode(errors="replace")
    assert done.stdout == b"" and done.stderr == b"", (done.stdout, done.stderr)
    row = _wait_for(lambda: _ended_session(db, session_id), timeout=45)
    assert row, "the session was never recorded after the hook returned"
    assert "prompt_input_exit" in row[0]


def test_hook_saves_inline_when_the_worker_cannot_be_started(tmp_path: Path) -> None:
    """Losing the hand-off must not lose the session: fall back to saving inline."""
    workspace = tmp_path / "ws"
    workspace.mkdir()
    db = workspace / ".claude" / "memory.db"
    session_id = "inline-fallback-session"
    driver = tmp_path / "driver.py"
    driver.write_text(
        "import importlib.util, sys\n"
        f"sys.path.insert(0, {str(SCRIPTS)!r})\n"
        "import _detach\n"
        "def refuse(*args, **kwargs):\n"
        "    raise OSError('process table full')\n"
        "_detach.spawn_detached = refuse\n"
        f"spec = importlib.util.spec_from_file_location('session_end_mod', {str(HOOK)!r})\n"
        "mod = importlib.util.module_from_spec(spec)\n"
        "spec.loader.exec_module(mod)\n"
        "mod.run_hook(sys.stdin.buffer.read())\n",
        encoding="utf-8",
    )

    done = subprocess.run(
        [sys.executable, str(driver)],
        input=_payload(workspace, session_id),
        capture_output=True,
        env=_hook_env(tmp_path),
        cwd=str(tmp_path),
        timeout=300,
    )

    assert done.returncode == 0, done.stderr.decode(errors="replace")
    assert _ended_session(db, session_id), "the inline fallback did not record the session"
