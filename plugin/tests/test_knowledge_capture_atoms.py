"""knowledge-capture.py turns Bash results into fix atoms.

Claude Code hands PostToolUse hooks the tool result as ``tool_response`` (for
Bash: a dict with stdout/stderr); the hook also still accepts the older flat
``tool_output`` string. A failing Bash command is remembered; the next
successful command sharing an executable with it becomes a fix atom.
"""

from __future__ import annotations

import importlib.util
import io
import json
import sqlite3
import sys
from pathlib import Path

import pytest

from tentaqles.memory.store import MemoryStore

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
SCRIPTS = PLUGIN_ROOT / "scripts"
sys.path.insert(0, str(SCRIPTS))


def _load_capture(monkeypatch):
    import _path

    monkeypatch.setattr(_path, "setup_paths", lambda: (str(PLUGIN_ROOT), ""))
    spec = importlib.util.spec_from_file_location("knowledge_capture", SCRIPTS / "knowledge-capture.py")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


@pytest.fixture
def workspace(tmp_path, monkeypatch):
    (tmp_path / ".tentaqles.yaml").write_text("schema: tentaqles-client-v2\nclient: acme\n", encoding="utf-8")
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: None)
    MemoryStore(tmp_path).close()
    monkeypatch.delenv("TENTAQLES_JEV_MEMORY", raising=False)
    return tmp_path


def _run(monkeypatch, cwd: Path, command: str, response) -> None:
    mod = _load_capture(monkeypatch)
    payload = {"cwd": str(cwd), "tool_name": "Bash", "tool_input": {"command": command}}
    if isinstance(response, str):
        payload["tool_output"] = response
    else:
        payload["tool_response"] = response
    monkeypatch.setattr(sys, "stdin", io.StringIO(json.dumps(payload)))
    with pytest.raises(SystemExit):
        mod.main()


def _rows(cwd: Path, sql: str):
    db = sqlite3.connect(str(cwd / ".claude" / "memory.db"))
    try:
        return db.execute(sql).fetchall()
    finally:
        db.close()


def test_stderr_failure_then_success_writes_a_fix_atom(monkeypatch, workspace):
    _run(monkeypatch, workspace, "pytest tests/", {"stdout": "", "stderr": "ModuleNotFoundError: No module named 'yaml'"})
    assert _rows(workspace, "SELECT executable FROM tool_failures") == [("pytest",)]

    _run(monkeypatch, workspace, "pip install pyyaml && pytest tests/", {"stdout": "3 passed", "stderr": ""})

    atoms = _rows(workspace, "SELECT kind, text FROM atoms")
    assert len(atoms) == 1 and atoms[0][0] == "fix"
    assert "No module named 'yaml'" in atoms[0][1] and "pip install pyyaml" in atoms[0][1]
    assert _rows(workspace, "SELECT COUNT(*) FROM tool_failures") == [(0,)]


def test_exit_code_in_stdout_counts_as_failure(monkeypatch, workspace):
    _run(monkeypatch, workspace, "go test ./...", {"stdout": "Exit code 1\n--- FAIL: TestX\nFAIL", "stderr": ""})
    assert _rows(workspace, "SELECT error FROM tool_failures")[0][0].startswith("--- FAIL: TestX")


def test_explicit_exit_code_field_counts_as_failure(monkeypatch, workspace):
    _run(monkeypatch, workspace, "make", {"stdout": "building", "stderr": "", "exit_code": 2})
    assert _rows(workspace, "SELECT COUNT(*) FROM tool_failures") == [(1,)]


def test_clean_success_without_failure_records_nothing(monkeypatch, workspace):
    _run(monkeypatch, workspace, "ls", {"stdout": "a\nb\n", "stderr": ""})
    assert _rows(workspace, "SELECT COUNT(*) FROM tool_failures") == [(0,)]
    assert _rows(workspace, "SELECT COUNT(*) FROM atoms") == [(0,)]


def test_interrupted_command_is_not_a_failure(monkeypatch, workspace):
    _run(monkeypatch, workspace, "npm run dev", {"stdout": "", "stderr": "", "interrupted": True})
    assert _rows(workspace, "SELECT COUNT(*) FROM tool_failures") == [(0,)]


def test_legacy_tool_output_string_is_still_scanned(monkeypatch, workspace):
    _run(monkeypatch, workspace, "pytest", "Traceback (most recent call last):\n  ...\nAssertionError: boom")
    assert _rows(workspace, "SELECT executable FROM tool_failures") == [("pytest",)]


def test_tool_response_text_still_feeds_touches(monkeypatch, workspace):
    _run(monkeypatch, workspace, "echo", {"stdout": "root cause was in /src/app/parser.py", "stderr": ""})
    assert _rows(workspace, "SELECT node_id FROM touches") == [("/src/app/parser.py",)]
