"""Semantic-fact extraction through a headless `claude -p` call.

Every test mocks the subprocess (or puts a fake `claude` on PATH); nothing
here ever calls a real model.
"""

from __future__ import annotations

import json
import os
import sqlite3
import subprocess
import sys
import textwrap
import time
from pathlib import Path

import pytest

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(PLUGIN_ROOT))

from tentaqles.memory import llm  # noqa: E402
from tentaqles.memory.consolidator import MemoryConsolidator  # noqa: E402
from tentaqles.memory.store import MemoryStore  # noqa: E402

# Built at runtime so no secret-looking literal sits in the source.
FAKE_AWS_ID = "AKIA" + "Q" * 16

FACTS = "- The project stores its cache in SQLite, not Redis.\n- Deploys go through the release workflow only."


class _Proc:
    def __init__(self, stdout: bytes, returncode: int = 0):
        self.stdout = stdout
        self.stderr = b""
        self.returncode = returncode


def _ok(result: str = FACTS, **extra) -> _Proc:
    body = {"type": "result", "subtype": "success", "is_error": False, "result": result}
    body.update(extra)
    return _Proc(json.dumps(body).encode("utf-8"))


@pytest.fixture
def fake_run(monkeypatch):
    calls: list[dict] = []
    state = {"proc": _ok(), "exc": None}

    def run(argv, **kwargs):
        calls.append({"argv": argv, **kwargs})
        if state["exc"] is not None:
            raise state["exc"]
        return state["proc"]

    monkeypatch.setattr(llm.subprocess, "run", run)
    monkeypatch.setattr(llm, "find_claude", lambda: "/fake/bin/claude")
    monkeypatch.delenv("TENTAQLES_SEMANTIC_FACTS", raising=False)
    monkeypatch.delenv("TENTAQLES_HEADLESS_CHILD", raising=False)
    return calls, state


def test_cli_call_shape_identity_and_redaction(fake_run, monkeypatch):
    calls, _ = fake_run
    monkeypatch.setenv("CLAUDE_CONFIG_DIR", "/home/me/.tentaqles/identities/acme/claude")
    fn = llm.get_semantic_llm()
    assert fn is not None
    out = fn("summaries mention key " + FAKE_AWS_ID + " somewhere")
    assert out == FACTS

    (call,) = calls
    argv = call["argv"]
    assert argv[0] == "/fake/bin/claude"
    assert argv[argv.index("--model") + 1] == "haiku"
    # The child runs untrusted transcript text: no tools, no MCP, no prompts.
    assert argv[argv.index("--tools") + 1] == ""
    assert argv[argv.index("--permission-mode") + 1] == "dontAsk"
    assert "--strict-mcp-config" in argv and "--disable-slash-commands" in argv
    assert call["timeout"] == 60
    sent = call["input"].decode("utf-8")
    assert FAKE_AWS_ID not in sent and "REDACTED" in sent
    env = call["env"]
    assert env["CLAUDE_CONFIG_DIR"] == "/home/me/.tentaqles/identities/acme/claude"
    assert env["TENTAQLES_HEADLESS_CHILD"] == "1"
    assert env["TENTAQLES_SEMANTIC_FACTS"] == "0"


@pytest.mark.parametrize("value", ["0", "false", "off", "no"])
def test_opt_out(fake_run, monkeypatch, value):
    monkeypatch.setenv("TENTAQLES_SEMANTIC_FACTS", value)
    assert llm.get_semantic_llm() is None


def test_disabled_inside_headless_child(fake_run, monkeypatch):
    monkeypatch.setenv("TENTAQLES_HEADLESS_CHILD", "1")
    assert llm.get_semantic_llm() is None


def test_missing_binary_means_no_llm(monkeypatch):
    monkeypatch.delenv("TENTAQLES_SEMANTIC_FACTS", raising=False)
    monkeypatch.delenv("TENTAQLES_HEADLESS_CHILD", raising=False)
    monkeypatch.setattr(llm, "find_claude", lambda: None)
    assert llm.get_semantic_llm() is None
    assert llm.claude_cli_complete("x") == ""


@pytest.mark.parametrize(
    "proc,exc",
    [
        (_Proc(b"", returncode=1), None),
        (_Proc(b"not json"), None),
        (_ok(is_error=True), None),
        (None, subprocess.TimeoutExpired(cmd="claude", timeout=60)),
        (None, FileNotFoundError("claude")),
        (None, PermissionError("denied")),
    ],
)
def test_failures_degrade_to_empty(fake_run, proc, exc):
    _, state = fake_run
    state["proc"], state["exc"] = proc, exc
    assert llm.get_semantic_llm()("prompt") == ""


def test_legacy_list_output_is_parsed(fake_run):
    _, state = fake_run
    state["proc"] = _Proc(
        json.dumps([{"type": "system"}, {"type": "result", "is_error": False, "result": "ok"}]).encode()
    )
    assert llm.get_semantic_llm()("p") == "ok"


def _store_with_sessions(root: Path, n: int) -> MemoryStore:
    store = MemoryStore(root)
    for i in range(n):
        store.start_session(tags=["t"])
        store.end_session(f"Session {i}: moved the cache to SQLite and documented why.")
    return store


def test_consolidator_records_facts_with_cli_llm(fake_run, tmp_path):
    store = _store_with_sessions(tmp_path, 10)
    result = MemoryConsolidator(store, llm_fn=llm.get_semantic_llm()).maybe_compact()
    assert result["compacted"] and result["facts_added"] == 2
    facts = [f["fact"] for f in store.get_semantic_facts(limit=10)]
    assert any("SQLite" in f for f in facts)
    store.close()


def test_consolidator_survives_cli_failure(fake_run, tmp_path):
    _, state = fake_run
    state["exc"] = subprocess.TimeoutExpired(cmd="claude", timeout=60)
    store = _store_with_sessions(tmp_path, 10)
    result = MemoryConsolidator(store, llm_fn=llm.get_semantic_llm()).maybe_compact()
    assert result["facts_added"] == 0
    store.close()


# --- wiring: the detached session-end worker passes the model ----------------


def _fake_claude(bindir: Path, log: Path) -> None:
    bindir.mkdir()
    impl = bindir / "fake_claude.py"
    impl.write_text(
        textwrap.dedent(
            f"""\
            import json, os, sys
            prompt = sys.stdin.read()
            with open({str(log)!r}, "a", encoding="utf-8") as fh:
                fh.write(json.dumps({{"argv": sys.argv[1:], "prompt": prompt,
                    "config": os.environ.get("CLAUDE_CONFIG_DIR", "")}}) + "\\n")
            print(json.dumps({{"type": "result", "is_error": False,
                "result": {FACTS!r}}}))
            """
        ),
        encoding="utf-8",
    )
    if sys.platform == "win32":
        (bindir / "claude.cmd").write_text(
            f'@"{sys.executable}" "{impl}" %*\r\n', encoding="utf-8"
        )
    else:
        sh = bindir / "claude"
        sh.write_text(f'#!/bin/sh\nexec "{sys.executable}" "{impl}" "$@"\n', encoding="utf-8")
        sh.chmod(0o755)


def _facts(db: Path) -> list[str]:
    if not db.exists():
        return []
    try:
        conn = sqlite3.connect(f"file:{db.as_posix()}?mode=ro", uri=True)
        try:
            return [r[0] for r in conn.execute("SELECT fact FROM semantic_memories")]
        finally:
            conn.close()
    except sqlite3.Error:
        return []


def test_session_end_worker_extracts_facts_via_cli(tmp_path):
    ws = tmp_path / "ws"
    ws.mkdir()
    (ws / ".tentaqles.yaml").write_text(
        "schema: tentaqles-client-v2\nclient: acme\n", encoding="utf-8"
    )
    _store_with_sessions(ws, 9).close()  # the worker ends the 10th

    log = tmp_path / "claude-calls.jsonl"
    _fake_claude(tmp_path / "bin", log)
    env = dict(os.environ)
    env["PATH"] = str(tmp_path / "bin") + os.pathsep + env.get("PATH", "")
    env["CLAUDE_PLUGIN_ROOT"] = str(PLUGIN_ROOT)
    env["CLAUDE_PLUGIN_DATA"] = str(tmp_path / "data")
    env["TENTAQLES_DATA_DIR"] = str(tmp_path / "data")
    env["PYTHONPATH"] = str(PLUGIN_ROOT)
    env["CLAUDE_CONFIG_DIR"] = str(tmp_path / "identity-config")
    env.pop("TENTAQLES_SEMANTIC_FACTS", None)
    env.pop("TENTAQLES_HEADLESS_CHILD", None)

    payload = json.dumps(
        {"session_id": "s-10", "cwd": str(ws), "transcript_path": "", "reason": "exit"}
    )
    proc = subprocess.run(
        [sys.executable, str(PLUGIN_ROOT / "scripts" / "session-end.py"), "--worker", str(ws)],
        input=payload.encode("utf-8"),
        capture_output=True,
        timeout=120,
        env=env,
    )
    assert proc.returncode == 0, proc.stderr.decode(errors="replace")

    facts = _facts(ws / ".claude" / "memory.db")
    assert any("SQLite" in f for f in facts), facts
    (call,) = [json.loads(l) for l in log.read_text(encoding="utf-8").splitlines()]
    argv = call["argv"]
    assert argv[0] == "-p" and argv[argv.index("--model") + 1] == "haiku"
    assert argv[argv.index("--tools") + 1] == "" and "--strict-mcp-config" in argv
    assert call["config"] == str(tmp_path / "identity-config")


def test_session_end_hook_skips_headless_child(tmp_path):
    ws = tmp_path / "ws"
    ws.mkdir()
    env = dict(os.environ)
    env["PYTHONPATH"] = str(PLUGIN_ROOT)
    env["CLAUDE_PLUGIN_DATA"] = str(tmp_path / "data")
    env["TENTAQLES_HEADLESS_CHILD"] = "1"
    payload = json.dumps({"session_id": "x", "cwd": str(ws), "transcript_path": ""})
    proc = subprocess.run(
        [sys.executable, str(PLUGIN_ROOT / "scripts" / "session-end.py"), "--worker", str(ws)],
        input=payload.encode("utf-8"),
        capture_output=True,
        timeout=60,
        env=env,
    )
    assert proc.returncode == 0
    time.sleep(0.2)
    assert not (ws / ".claude" / "memory.db").exists()
