"""The session preamble's hub line (spec A §13.3): a read-only SQLite count bounded to 300 ms."""

from __future__ import annotations

import importlib.util
import json
import os
import sqlite3
import subprocess
import sys
import textwrap
import time
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
SCRIPT = PLUGIN_ROOT / "scripts" / "session-preamble.py"


def _workspace(root: Path) -> Path:
    root.mkdir(parents=True, exist_ok=True)
    (root / ".tentaqles.yaml").write_text(textwrap.dedent("""\
        schema: tentaqles-client-v2
        client: acme
        display_name: Acme Corp
        language: en
        """), encoding="utf-8")
    return root


def _hub_db(home: Path, ws: str, pending: int, answered: int = 0) -> Path:
    db = home / "hub" / ws / "hub.db"
    db.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(db)
    conn.execute("CREATE TABLE decisions (id TEXT PRIMARY KEY, status TEXT NOT NULL)")
    rows = [(f"d{i}", "pending") for i in range(pending)] + [(f"a{i}", "answered") for i in range(answered)]
    conn.executemany("INSERT INTO decisions(id, status) VALUES (?, ?)", rows)
    conn.commit()
    conn.close()
    return db


def _run(cwd: Path, env_extra: dict, *args: str) -> str:
    env = dict(os.environ)
    env.update(env_extra)
    extra = [p for p in sys.path if p and ("site-packages" in p or p == str(PLUGIN_ROOT))]
    env["PYTHONPATH"] = os.pathsep.join([str(PLUGIN_ROOT), *extra])
    proc = subprocess.run([sys.executable, str(SCRIPT), *args], input=json.dumps({"cwd": str(cwd)}),
                          capture_output=True, text=True, timeout=60, env=env)
    assert proc.returncode == 0, proc.stderr
    return proc.stdout


def test_preamble_counts_pending_decisions(tmp_path):
    ws = _workspace(tmp_path / "work" / "acme")
    home = tmp_path / "tqhome"
    _hub_db(home, "acme", pending=2, answered=1)
    out = _run(ws, {"TQ_HOME": str(home), "TQ_WS": "acme"}, "--memory-only")
    assert "2 decisions waiting on you (tq hub open)" in out
    full = _run(ws, {"TQ_HOME": str(home), "TQ_WS": "acme"})
    assert "2 decisions waiting on you (tq hub open)" in full


def test_preamble_singular_none_and_no_database(tmp_path):
    ws = _workspace(tmp_path / "work" / "acme")
    home = tmp_path / "tqhome"
    assert "waiting on you" not in _run(ws, {"TQ_HOME": str(home), "TQ_WS": "acme"}, "--memory-only")
    _hub_db(home, "acme", pending=1)
    assert "1 decision waiting on you (tq hub open)" in _run(ws, {"TQ_HOME": str(home), "TQ_WS": "acme"}, "--memory-only")
    other = tmp_path / "tqhome2"
    _hub_db(other, "acme", pending=0, answered=3)
    assert "waiting on you" not in _run(ws, {"TQ_HOME": str(other), "TQ_WS": "acme"}, "--memory-only")


def _load_module(monkeypatch):
    # The script calls _path.setup_paths(), which rewraps stdout/stderr on Windows and
    # would break pytest's capture: the path is already set up in a test.
    monkeypatch.syspath_prepend(str(PLUGIN_ROOT / "scripts"))
    import _path

    monkeypatch.setattr(_path, "setup_paths", lambda: (str(PLUGIN_ROOT), ""))
    spec = importlib.util.spec_from_file_location("session_preamble_under_test", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def test_hub_read_is_bounded_to_300_ms(tmp_path, monkeypatch):
    home = tmp_path / "tqhome"
    db = _hub_db(home, "acme", pending=4)
    monkeypatch.setenv("TQ_HOME", str(home))
    monkeypatch.setenv("TQ_WS", "acme")
    mod = _load_module(monkeypatch)
    assert mod._hub_context({}) == "4 decisions waiting on you (tq hub open)"
    holder = sqlite3.connect(db, isolation_level=None)
    holder.execute("BEGIN EXCLUSIVE")  # a writer holds the database: readers must give up, not wait
    try:
        t0 = time.monotonic()
        line = mod._hub_context({})
        elapsed = time.monotonic() - t0
    finally:
        holder.execute("ROLLBACK")
        holder.close()
    assert line == "" and elapsed < 1.0, (line, elapsed)


def test_workspace_name_falls_back_to_the_client_root(tmp_path, monkeypatch):
    home = tmp_path / "tqhome"
    _hub_db(home, "acme", pending=1)
    monkeypatch.setenv("TQ_HOME", str(home))
    monkeypatch.delenv("TQ_WS", raising=False)
    mod = _load_module(monkeypatch)
    assert mod._hub_db_path({"client_root": str(tmp_path / "work" / "acme")}) == home / "hub" / "acme" / "hub.db"
    assert mod._hub_db_path({"client_root": str(tmp_path / "work" / "..")}) is None
