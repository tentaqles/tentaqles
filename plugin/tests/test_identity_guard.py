"""Canonical fallback-guard suite for identity-guard.py + tq_hook.sh."""

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
GUARD = ROOT / "scripts" / "identity-guard.py"
HOOK = ROOT / "scripts" / "tq_hook.sh"
CASES = json.loads((ROOT / "tests" / "fixtures" / "guard_cases.json").read_text())["cases"]

def _find_bash():
    """Prefer Git Bash over WSL's bash.exe (which cannot see Windows paths)."""
    for cand in (
        os.environ.get("BASH"),
        r"C:\Program Files\Git\bin\bash.exe",
        r"C:\Program Files (x86)\Git\bin\bash.exe",
    ):
        if cand and os.path.exists(cand):
            return cand
    found = shutil.which("bash")
    if found and "system32" in found.lower():
        return None  # WSL bash cannot run these scripts
    return found


BASH = _find_bash()


def run_guard(command: str, cwd: str = None) -> subprocess.CompletedProcess:
    payload = {"cwd": cwd or os.getcwd(), "tool_name": "Bash", "tool_input": {"command": command}}
    env = dict(os.environ, TQ_FALLBACK="1")
    return subprocess.run(
        [sys.executable, str(GUARD)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        env=env,
    )


def hook_env(tmp_path, **extra):
    """PATH keeping bash (and its coreutils) resolvable, plus tmp_path."""
    path = os.path.dirname(BASH) + os.pathsep + str(tmp_path)
    env = dict(os.environ, PATH=path, TENTAQLES_PY=sys.executable)
    env.update(extra)
    return env


def test_suite_has_20_cases():
    assert len(CASES) == 20


@pytest.mark.parametrize("case", CASES, ids=[c["name"] for c in CASES])
def test_fallback_semantics(case):
    p = run_guard(case["command"])
    want = case["expect"]["fallback"]["block"]
    assert (p.returncode == 2) == want, p.stderr
    if want:
        assert p.stderr.startswith("BLOCKED:")


def test_string_tool_input():
    payload = {"cwd": os.getcwd(), "tool_input": json.dumps({"command": "git push"})}
    p = subprocess.run(
        [sys.executable, str(GUARD)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        env=dict(os.environ, TQ_FALLBACK="1"),
    )
    assert p.returncode == 2


def test_malformed_stdin_allows():
    p = subprocess.run(
        [sys.executable, str(GUARD)], input="not json", capture_output=True, text=True
    )
    assert p.returncode == 0


def test_guard_does_not_import_tentaqles():
    src = GUARD.read_text()
    assert "import subprocess" not in src
    assert "from _path" not in src
    assert "from tentaqles" not in src
    assert "import tentaqles" not in src


@pytest.mark.skipif(BASH is None, reason="needs bash")
def test_tq_hook_falls_back_when_tq_missing(tmp_path):
    env = hook_env(
        tmp_path,
        TQ_BIN=str(tmp_path / "nope"),
        HOME=str(tmp_path),
        LOCALAPPDATA=str(tmp_path),
        USERPROFILE=str(tmp_path),
        CLAUDE_PLUGIN_ROOT=str(ROOT),
    )
    payload = json.dumps({"cwd": str(tmp_path), "tool_input": {"command": "gh pr list"}})
    p = subprocess.run(
        [BASH, str(HOOK), "pre-tool-use"],
        input=payload,
        capture_output=True,
        text=True,
        env=env,
    )
    assert p.returncode == 2 and "tq is not installed" in p.stderr


@pytest.mark.skipif(BASH is None, reason="needs bash")
def test_tq_hook_session_start_fallback_message(tmp_path):
    env = hook_env(
        tmp_path,
        TQ_BIN=str(tmp_path / "nope"),
        HOME=str(tmp_path),
        LOCALAPPDATA=str(tmp_path),
        USERPROFILE=str(tmp_path),
        CLAUDE_PLUGIN_ROOT=str(ROOT),
    )
    p = subprocess.run(
        [BASH, str(HOOK), "session-start"],
        input="{}",
        capture_output=True,
        text=True,
        env=env,
    )
    assert p.returncode == 0
    assert "fallback mode" in p.stdout


@pytest.mark.skipif(BASH is None, reason="needs bash")
def test_tq_hook_execs_tq_when_present(tmp_path):
    fake = tmp_path / "tq"
    fake.write_text('#!/bin/sh\necho "argv:$*"; cat; exit 0\n')
    fake.chmod(0o755)
    env = hook_env(tmp_path, TQ_BIN=str(fake), CLAUDE_PLUGIN_ROOT=str(ROOT))
    p = subprocess.run(
        [BASH, str(HOOK), "session-start"],
        input="{}",
        capture_output=True,
        text=True,
        env=env,
    )
    assert p.returncode == 0
    assert "argv:claude-hook session-start" in p.stdout
    assert "{}" in p.stdout
