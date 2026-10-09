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


def hook_env(tmp_path, with_python=True, **extra):
    """PATH keeping bash (and its coreutils) resolvable, plus tmp_path.

    with_python=True also puts the running interpreter's directory on PATH so
    the fallback guard can be launched; with_python=False simulates a box with
    no Python at all.
    """
    parts = [os.path.dirname(BASH)]
    if with_python:
        parts.append(os.path.dirname(sys.executable))
    parts.append(str(tmp_path))
    env = dict(os.environ, PATH=os.pathsep.join(parts))
    env.pop("TENTAQLES_PY", None)
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
def test_tq_hook_blocks_when_no_python(tmp_path):
    """No tq and no interpreter -> fail CLOSED, not open.

    Rather than trying to scrub every real interpreter off PATH (fragile: on
    Linux CI runners bash and the system python3 live in the same directory,
    e.g. /usr/bin, so dropping python's own dir from PATH doesn't remove it),
    shadow every name tq_hook.sh probes (python3, python, py) with executable
    shims that always fail. The shims sit first on PATH, so `command -v`
    finds them before any real interpreter; each one exits nonzero on the
    `-c "import sys"` probe tq_hook.sh runs to validate a candidate, so every
    candidate is rejected and the hook falls through to its fail-closed path
    regardless of what is actually installed on the box.
    """
    shims = tmp_path / "shims"
    shims.mkdir()
    shim_body = "#!/bin/sh" + chr(10) + "exit 1" + chr(10)
    for name in ("python3", "python", "py"):
        shim = shims / name
        shim.write_text(shim_body)
        shim.chmod(0o755)

    env = hook_env(
        tmp_path,
        with_python=False,
        TQ_BIN=str(tmp_path / "nope"),
        HOME=str(tmp_path),
        LOCALAPPDATA=str(tmp_path),
        USERPROFILE=str(tmp_path),
        CLAUDE_PLUGIN_ROOT=str(ROOT),
    )
    env["PATH"] = os.pathsep.join(
        [str(shims), os.path.dirname(BASH), os.path.dirname(sys.executable)]
    )
    env.pop("TENTAQLES_PY", None)
    payload = json.dumps({"cwd": str(tmp_path), "tool_input": {"command": "ls"}})
    p = subprocess.run(
        [BASH, str(HOOK), "pre-tool-use"],
        input=payload,
        capture_output=True,
        text=True,
        env=env,
    )
    assert p.returncode == 2
    assert "no python interpreter was found" in p.stderr


def _no_python_env(tmp_path):
    shims = tmp_path / "shims"
    shims.mkdir()
    for name in ("python3", "python", "py"):
        shim = shims / name
        shim.write_text("#!/bin/sh" + chr(10) + "exit 1" + chr(10))
        shim.chmod(0o755)
    env = hook_env(
        tmp_path,
        with_python=False,
        TQ_BIN=str(tmp_path / "nope"),
        HOME=str(tmp_path),
        LOCALAPPDATA=str(tmp_path),
        USERPROFILE=str(tmp_path),
        CLAUDE_PLUGIN_ROOT=str(ROOT),
    )
    env["PATH"] = os.pathsep.join(
        [str(shims), os.path.dirname(BASH), os.path.dirname(sys.executable)]
    )
    env.pop("TENTAQLES_PY", None)
    return env


@pytest.mark.skipif(BASH is None, reason="needs bash")
@pytest.mark.parametrize(
    "tool,expected",
    [("Read", 0), ("mcp__n8n__get_workflow_details", 0), ("PowerShell", 2), ("Bash", 2)],
)
def test_tq_hook_no_python_only_blocks_shell_tools(tmp_path, tool, expected):
    """The widened matcher must not turn a missing tq+python into blocking reads."""
    env = _no_python_env(tmp_path)
    payload = json.dumps(
        {"tool_name": tool, "cwd": str(tmp_path), "tool_input": {"command": "ls", "file_path": "x"}}
    )
    p = subprocess.run(
        [BASH, str(HOOK), "pre-tool-use"], input=payload, capture_output=True, text=True, env=env
    )
    assert p.returncode == expected, p.stderr


def test_hooks_json_pretooluse_matcher_covers_shell_file_and_mcp_tools():
    import re

    hooks = json.loads((ROOT / "hooks" / "hooks.json").read_text(encoding="utf-8"))
    entry = next(
        e for e in hooks["hooks"]["PreToolUse"] if "tq_hook.sh" in e["hooks"][0]["command"]
    )
    rx = re.compile(entry["matcher"])
    # Agent launches reach tq for subagent model routing (tq decide).
    for tool in ("Bash", "PowerShell", "Read", "Edit", "Write", "MultiEdit", "Grep", "Agent", "mcp__n8n__x"):
        assert rx.search(tool), tool
    for tool in ("Glob", "TodoWrite", "WebFetch", "AgentX"):
        assert not rx.search(tool), tool


@pytest.mark.skipif(BASH is None, reason="needs bash")
def test_tq_hook_unknown_event_blocks(tmp_path):
    env = hook_env(tmp_path, CLAUDE_PLUGIN_ROOT=str(ROOT))
    p = subprocess.run(
        [BASH, str(HOOK), "bogus"], input="{}", capture_output=True, text=True, env=env
    )
    assert p.returncode == 2
    assert "unknown event 'bogus'" in p.stderr


@pytest.mark.skipif(BASH is None, reason="needs bash")
def test_tq_hook_falls_through_when_tq_not_executable(tmp_path):
    """A resolved-but-unrunnable tq (exit 126/127) must fall back, not pass."""
    broken = tmp_path / "tq"
    broken.write_text("not-an-executable" + chr(10))
    broken.chmod(0o755)
    env = hook_env(
        tmp_path,
        TQ_BIN=str(broken),
        HOME=str(tmp_path),
        LOCALAPPDATA=str(tmp_path),
        USERPROFILE=str(tmp_path),
        CLAUDE_PLUGIN_ROOT=str(ROOT),
    )
    payload = json.dumps({"cwd": str(tmp_path), "tool_input": {"command": "git push"}})
    p = subprocess.run(
        [BASH, str(HOOK), "pre-tool-use"],
        input=payload,
        capture_output=True,
        text=True,
        env=env,
    )
    assert p.returncode == 2
    assert "tq is not installed" in p.stderr


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


def _tq_hook_entries(event):
    hooks = json.loads((ROOT / "hooks" / "hooks.json").read_text(encoding="utf-8"))
    return [
        h
        for group in hooks["hooks"].get(event, [])
        for h in group["hooks"]
        if "tq_hook.sh" in h["command"]
    ]


@pytest.mark.parametrize(
    "event,arg,max_timeout",
    [("Stop", "stop", 10), ("UserPromptSubmit", "prompt-submit", 5)],
)
def test_hooks_json_registers_jev_hooks_with_short_timeouts(event, arg, max_timeout):
    entries = _tq_hook_entries(event)
    assert len(entries) == 1, entries
    assert entries[0]["command"].endswith("tq_hook.sh\" " + arg)
    assert 0 < entries[0]["timeout"] <= max_timeout


@pytest.mark.skipif(BASH is None, reason="needs bash")
@pytest.mark.parametrize("event", ["stop", "prompt-submit"])
def test_tq_hook_advisory_events_without_tq_do_nothing(tmp_path, event):
    """No tq installed: the Stop/UserPromptSubmit hooks exit 0 with no output."""
    env = _no_python_env(tmp_path)
    p = subprocess.run(
        [BASH, str(HOOK), event], input="{}", capture_output=True, text=True, env=env
    )
    assert p.returncode == 0, p.stderr
    assert p.stdout == ""


@pytest.mark.skipif(BASH is None, reason="needs bash")
@pytest.mark.parametrize("event", ["stop", "prompt-submit"])
def test_tq_hook_advisory_events_pass_through_and_never_fail(tmp_path, event):
    """tq runs with the event and stdin; a non-zero exit (older tq) becomes 0."""
    fake = tmp_path / "tq"
    fake.write_text('#!/bin/sh\necho "argv:$*"; cat; exit 1\n')
    fake.chmod(0o755)
    env = hook_env(tmp_path, TQ_BIN=str(fake), CLAUDE_PLUGIN_ROOT=str(ROOT))
    env.pop("TENTAQLES_HEADLESS_CHILD", None)
    p = subprocess.run(
        [BASH, str(HOOK), event], input='{"x":1}', capture_output=True, text=True, env=env
    )
    assert p.returncode == 0
    assert "argv:claude-hook " + event in p.stdout
    assert '{"x":1}' in p.stdout


@pytest.mark.skipif(BASH is None, reason="needs bash")
def test_tq_hook_stop_skips_headless_child(tmp_path):
    fake = tmp_path / "tq"
    fake.write_text('#!/bin/sh\necho ran\n')
    fake.chmod(0o755)
    env = hook_env(
        tmp_path, TQ_BIN=str(fake), CLAUDE_PLUGIN_ROOT=str(ROOT), TENTAQLES_HEADLESS_CHILD="1"
    )
    p = subprocess.run([BASH, str(HOOK), "stop"], input="{}", capture_output=True, text=True, env=env)
    assert p.returncode == 0
    assert p.stdout == ""
