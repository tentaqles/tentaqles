"""Tests for the shadow Jev memory gates (tentaqles.memory.jev_gate).

Every tq call is mocked: no network, no binary. The gates must only spawn a
detached worker from the hooks, honour TENTAQLES_JEV_MEMORY=0, keep content
off command lines and out of the log, and never change what the hooks do.
"""

from __future__ import annotations

import importlib.util
import io
import json
import os
import sqlite3
import subprocess
import sys
import textwrap
from pathlib import Path

import pytest

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
SCRIPTS = PLUGIN_ROOT / "scripts"
if str(SCRIPTS) not in sys.path:
    sys.path.insert(0, str(SCRIPTS))

from tentaqles.memory import jev_gate  # noqa: E402
from tentaqles.memory.store import MemoryStore  # noqa: E402

SECRET_WORD = "zebra-canary-7781"  # stands in for content that must not leak


def _load_script(name: str, monkeypatch):
    # Scripts call _path.setup_paths(), which rewraps stdout/stderr on
    # Windows and would break pytest's capture: the path is already set up.
    import _path

    monkeypatch.setattr(_path, "setup_paths", lambda: (str(PLUGIN_ROOT), ""))
    spec = importlib.util.spec_from_file_location(name.replace("-", "_"), SCRIPTS / name)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


@pytest.fixture(autouse=True)
def _clean_env(monkeypatch):
    monkeypatch.delenv("TENTAQLES_JEV_MEMORY", raising=False)
    monkeypatch.delenv("TENTAQLES_HEADLESS_CHILD", raising=False)


def _workspace(tmp_path: Path, jev: bool = True) -> Path:
    body = "schema: tentaqles-client-v2\nclient: acme\n"
    if jev:
        body += "decision:\n  backend: typesafe\n  mode: shadow\n"
    (tmp_path / ".tentaqles.yaml").write_text(body, encoding="utf-8")
    MemoryStore(tmp_path).close()  # creates .claude/memory.db
    return tmp_path


class FakeTQ:
    """Stands in for subprocess.run on the tq binary."""

    def __init__(self, answers=None, returncode=0, stderr="", raise_timeout=False):
        self.answers = answers
        self.returncode = returncode
        self.stderr = stderr
        self.raise_timeout = raise_timeout
        self.calls: list[list[str]] = []
        self.states: list[dict] = []

    def __call__(self, argv, **kwargs):
        self.calls.append(list(argv))
        assert kwargs.get("timeout"), "every tq call must be bounded"
        if self.raise_timeout:
            raise subprocess.TimeoutExpired(argv, kwargs["timeout"])
        if argv[1:3] == ["decide", "ask"]:
            path = argv[argv.index("--state-file") + 1]
            self.states.append(json.loads(Path(path).read_text(encoding="utf-8")))
            if self.returncode:
                return subprocess.CompletedProcess(argv, self.returncode, "", self.stderr)
            qids = [a.split("=", 1)[0] for i, a in enumerate(argv) if i and argv[i - 1] == "--q"]
            ans = self.answers(qids) if callable(self.answers) else self.answers
            return subprocess.CompletedProcess(argv, 0, json.dumps(ans), "")
        return subprocess.CompletedProcess(argv, 0, "", "")

    def asks(self):
        return [c for c in self.calls if c[1:3] == ["decide", "ask"]]

    def logs(self):
        return [c for c in self.calls if c[1:3] == ["decide", "log"]]


@pytest.fixture
def fake_tq(monkeypatch):
    def install(**kw):
        fake = FakeTQ(**kw)
        monkeypatch.setattr(jev_gate.subprocess, "run", fake)
        return fake

    return install


# --- switches -------------------------------------------------------------


@pytest.mark.parametrize("value", ["0", "false", "off", "NO"])
def test_opt_out_env_disables(monkeypatch, value):
    monkeypatch.setenv("TENTAQLES_JEV_MEMORY", value)
    assert not jev_gate.enabled()
    assert not jev_gate.should_gate({"decision": {"backend": "typesafe"}})


def test_headless_child_disables(monkeypatch):
    monkeypatch.setenv("TENTAQLES_HEADLESS_CHILD", "1")
    assert not jev_gate.enabled()


@pytest.mark.parametrize(
    "manifest,want",
    [
        (None, False),
        ({}, False),
        ({"decision": {"backend": "off"}}, False),
        ({"decision": "typesafe"}, False),
        ({"decision": {"backend": "typesafe"}}, True),
    ],
)
def test_manifest_precheck(manifest, want):
    assert jev_gate.should_gate(manifest) is want


# --- ask / log ------------------------------------------------------------


def test_ask_keeps_content_off_the_command_line(fake_tq, tmp_path):
    fake = fake_tq(answers={"worth": {"noul": 0.8}})
    probs, err = jev_gate.ask_noul(str(tmp_path), {"text": SECRET_WORD}, {"worth": "Q?"}, purpose="memory-capture", tq="tq")
    assert probs == {"worth": 0.8} and err == ""
    argv = fake.asks()[0]
    assert SECRET_WORD not in " ".join(argv)
    assert fake.states[0] == {"text": SECRET_WORD}
    assert "--json" in argv and argv[argv.index("--purpose") + 1] == "memory-capture"
    # The temp state file is gone.
    assert not Path(argv[argv.index("--state-file") + 1]).exists()


@pytest.mark.parametrize(
    "kw,want_err",
    [
        ({"returncode": 1, "stderr": "Error: decision backend is off for acme"}, ""),
        ({"returncode": 1, "stderr": "Error: not in a trusted workspace"}, ""),
        ({"returncode": 1, "stderr": "Error: jev: HTTP 503: down"}, "Error: jev: HTTP 503: down"),
        ({"raise_timeout": True}, "tq timed out or could not start"),
        ({"answers": {"other": {"noul": 0.5}}}, "no answer for worth"),
        ({"answers": {"worth": {"noul": 1.7}}}, "no answer for worth"),
    ],
)
def test_ask_failures_are_no_opinion(fake_tq, tmp_path, kw, want_err):
    fake_tq(**kw)
    probs, err = jev_gate.ask_noul(str(tmp_path), {}, {"worth": "Q?"}, purpose="x", tq="tq")
    assert probs is None and err == want_err


def test_no_tq_binary_is_silent(monkeypatch, fake_tq, tmp_path):
    fake = fake_tq(answers={})
    monkeypatch.setattr(jev_gate, "find_tq", lambda: None)
    assert jev_gate.ask_noul(str(tmp_path), {}, {"q": "?"}, purpose="x") == (None, "")
    assert jev_gate.run_capture({"cwd": str(tmp_path), "text": "t"}) is None
    assert fake.calls == []


# --- capture gate ---------------------------------------------------------


@pytest.mark.parametrize("p,want", [(0.9, "keep"), (0.5, "keep"), (0.2, "drop")])
def test_run_capture_logs_would_decision_without_content(fake_tq, tmp_path, p, want):
    fake = fake_tq(answers={"worth": {"noul": p}})
    payload = jev_gate.capture_payload(str(tmp_path), "Bash", f"decided to use {SECRET_WORD}", 2)
    rec = jev_gate.run_capture(payload, tq="tq")
    assert rec["would"] == {"worth": want}
    log = fake.logs()[0]
    joined = " ".join(log)
    assert SECRET_WORD not in joined
    assert "--kind" in log and log[log.index("--kind") + 1] == "memory-capture"
    assert log[log.index("--mode") + 1] == "shadow"
    assert f"worth={want}" in log and "tool=Bash" in log and "paths=2" in log


def test_run_capture_quiet_when_jev_not_configured(fake_tq, tmp_path):
    fake = fake_tq(returncode=1, stderr="Error: decision backend is off for acme")
    assert jev_gate.run_capture({"cwd": str(tmp_path), "tool": "Bash", "text": "x"}, tq="tq") is None
    assert fake.logs() == []


def test_capture_payload_is_redacted_and_capped(tmp_path):
    key = "ghp_" + "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8"  # built at runtime for the commit guard
    payload = jev_gate.capture_payload(str(tmp_path), "Bash", key + " " + "x" * 10000, 0)
    assert key not in payload["text"]
    assert len(payload["text"]) <= jev_gate.MAX_CAPTURE_TEXT


# --- recall gate ----------------------------------------------------------


def _store_with_memory(root: Path, facts: int = 8, decisions: int = 2) -> MemoryStore:
    store = MemoryStore(root)
    for i in range(facts):
        store.record_semantic_fact(f"fact {i} {SECRET_WORD}", source_sessions=[])
    for i in range(decisions):
        store.record_decision(f"choice {i}", "because")
    store.end_session("worked on the parser")
    return store


def test_recall_payload_skips_when_nothing_to_rank(tmp_path):
    store = _store_with_memory(_workspace(tmp_path), facts=3, decisions=1)
    assert jev_gate.recall_payload(str(tmp_path), store) is None
    store.close()


def test_run_recall_reports_overlap(fake_tq, tmp_path):
    store = _store_with_memory(_workspace(tmp_path), facts=8, decisions=2)
    payload = jev_gate.recall_payload(str(tmp_path), store)
    store.close()
    assert sorted(payload["items"]) == sorted([f"f{i}" for i in range(8)] + ["d0", "d1"])

    # Jev prefers the facts currently ranked lowest: f7..f3.
    def answers(qids):
        return {q: {"noul": (int(q[1:]) / 10 if q.startswith("f") else 0.9)} for q in qids}

    fake = fake_tq(answers=answers)
    rec = jev_gate.run_recall(payload, tq="tq")
    assert rec["extra"]["facts_overlap"] == "2/5"  # f3, f4 in both top-5s
    assert rec["would"] == {"facts": "reorder", "decisions": "same"}
    log = " ".join(fake.logs()[0])
    assert SECRET_WORD not in log and "parser" not in log
    # The question text names item ids only; content travels in the state file.
    assert SECRET_WORD not in " ".join(fake.asks()[0])
    assert any(SECRET_WORD in v for v in fake.states[0]["items"].values())


# --- hooks: spawn only, behaviour unchanged ---------------------------------


class SpawnRecorder:
    def __init__(self):
        self.calls = []

    def __call__(self, argv, stdin_data=b""):
        self.calls.append((argv, json.loads(stdin_data)))


def _run_capture_hook(monkeypatch, cwd: Path, spawner, text: str) -> list[str]:
    mod = _load_script("knowledge-capture.py", monkeypatch)
    import _detach

    monkeypatch.setattr(_detach, "spawn_detached", spawner)
    monkeypatch.setattr(jev_gate.subprocess, "run", lambda *a, **k: pytest.fail("hook ran tq inline"))
    payload = {"cwd": str(cwd), "tool_name": "Bash", "tool_input": {"command": "echo"}, "tool_output": text}
    monkeypatch.setattr(sys, "stdin", io.StringIO(json.dumps(payload)))
    with pytest.raises(SystemExit):
        mod.main()
    db = sqlite3.connect(str(cwd / ".claude" / "memory.db"))
    try:
        return sorted(r[0] for r in db.execute("SELECT node_id FROM touches"))
    finally:
        db.close()


def test_capture_hook_records_touches(monkeypatch, tmp_path):
    ws = _workspace(tmp_path, jev=False)
    touches = _run_capture_hook(monkeypatch, ws, SpawnRecorder(), "root cause was in /src/app/parser.py")
    assert touches == ["/src/app/parser.py"]


def test_capture_hook_spawns_worker_and_changes_nothing(monkeypatch, tmp_path):
    text = "decided to switch, see /src/app/parser.py"
    off_dir, on_dir = tmp_path / "off", tmp_path / "on"
    off_dir.mkdir()
    on_dir.mkdir()

    monkeypatch.setenv("TENTAQLES_JEV_MEMORY", "0")
    off = SpawnRecorder()
    touches_off = _run_capture_hook(monkeypatch, _workspace(off_dir), off, text)
    assert off.calls == []

    monkeypatch.delenv("TENTAQLES_JEV_MEMORY")
    on = SpawnRecorder()
    touches_on = _run_capture_hook(monkeypatch, _workspace(on_dir), on, text)
    assert touches_on == touches_off == ["/src/app/parser.py"]
    assert len(on.calls) == 1
    argv, payload = on.calls[0]
    assert argv[-1].endswith("jev-memory-gate.py")
    assert payload["kind"] == "capture" and payload["tool"] == "Bash" and payload["paths"] == 1


def test_capture_hook_skips_gate_without_regex_hit_or_jev(monkeypatch, tmp_path):
    a, b = tmp_path / "a", tmp_path / "b"
    a.mkdir()
    b.mkdir()
    rec = SpawnRecorder()
    _run_capture_hook(monkeypatch, _workspace(a), rec, "nothing interesting here")
    _run_capture_hook(monkeypatch, _workspace(b, jev=False), rec, "decided to switch")
    assert rec.calls == []


def test_capture_hook_survives_spawn_failure(monkeypatch, tmp_path):
    def boom(argv, stdin_data=b""):
        raise OSError("cannot start")

    touches = _run_capture_hook(monkeypatch, _workspace(tmp_path), boom, "root cause in /x/y.py")
    assert touches == ["/x/y.py"]


def test_preamble_output_unchanged_and_gate_detached(monkeypatch, tmp_path):
    mod = _load_script("session-preamble.py", monkeypatch)
    import _detach

    ws = _workspace(tmp_path)
    _store_with_memory(ws).close()
    monkeypatch.setattr(jev_gate.subprocess, "run", lambda *a, **k: pytest.fail("hook ran tq inline"))
    ctx = {"client": "acme", "client_root": str(ws)}

    monkeypatch.setenv("TENTAQLES_JEV_MEMORY", "0")
    off = SpawnRecorder()
    monkeypatch.setattr(_detach, "spawn_detached", off)
    out_off = mod._get_temporal_context(str(ws), ctx)

    monkeypatch.delenv("TENTAQLES_JEV_MEMORY")
    on = SpawnRecorder()
    monkeypatch.setattr(_detach, "spawn_detached", on)
    out_on = mod._get_temporal_context(str(ws), ctx)

    assert out_on == out_off and "## Semantic facts" in out_on
    assert off.calls == []
    assert len(on.calls) == 1 and on.calls[0][1]["kind"] == "recall"


def test_worker_honours_opt_out(monkeypatch, fake_tq, tmp_path):
    fake = fake_tq(answers={"worth": {"noul": 0.9}})
    monkeypatch.setenv("TENTAQLES_JEV_MEMORY", "0")
    mod = _load_script("jev-memory-gate.py", monkeypatch)
    monkeypatch.setattr(sys, "stdin", io.StringIO(json.dumps({"kind": "capture", "cwd": str(tmp_path), "text": "x"})))
    mod.main()
    assert fake.calls == []
