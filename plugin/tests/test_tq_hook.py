"""tq_hook.sh, the bridge from Claude Code to `tq claude-hook`, fails closed.

Claude Code runs a call whose hook exits with anything but 2, or whose hook it killed. So
pre-tool-use answers 0 or 2 and nothing else, whatever goes wrong with tq.
"""

import json
import os
import re
import shutil
import subprocess
import time
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[1]
HOOK = ROOT / "scripts" / "tq_hook.sh"
HOOKS_JSON = ROOT / "hooks" / "hooks.json"


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
        return None
    return found


BASH = _find_bash()
needs_bash = pytest.mark.skipif(BASH is None, reason="needs bash")


def env_with(tmp_path, plugin_root=None, **extra):
    """bash and its coreutils, plus tmp_path, on PATH; the home is tmp_path, so no real tq is found."""
    env = {k: v for k, v in os.environ.items() if k not in ("TQ_BIN", "TQ_ENABLED")}
    env.update(
        PATH=os.pathsep.join([os.path.dirname(BASH), str(tmp_path)]),
        HOME=str(tmp_path),
        USERPROFILE=str(tmp_path),
        LOCALAPPDATA=str(tmp_path),
        CLAUDE_PLUGIN_ROOT=str(plugin_root or ROOT),
    )
    env.update(extra)
    return env


def fake_tq(tmp_path, body):
    fake = tmp_path / "tq"
    fake.write_text("#!/bin/sh\n" + body + "\n")
    fake.chmod(0o755)
    return fake


def run(event, env, payload='{"tool_name":"Bash"}', timeout=30):
    return subprocess.run(
        [BASH, str(HOOK), event], input=payload, capture_output=True, text=True, env=env, timeout=timeout
    )


@needs_bash
@pytest.mark.parametrize("status", [1, 3, 134, 137])
def test_tq_hook_turns_a_tq_without_decision_into_a_deny(tmp_path, status):
    fake_tq(tmp_path, "cat >/dev/null; exit %d" % status)
    p = run("pre-tool-use", env_with(tmp_path))
    assert p.returncode == 2
    assert "BLOCKED [tq/guard-error]" in p.stderr


@needs_bash
def test_tq_hook_stops_a_hanging_tq_and_denies(tmp_path):
    """Stopped before the harness timeout, since Claude Code runs a call whose hook it killed. The
    fake reads stdin first, so the payload reaches the background tq."""
    fake_tq(tmp_path, "cat >/dev/null; exec sleep 30")
    start = time.monotonic()
    p = run("pre-tool-use", env_with(tmp_path, TQ_HOOK_WRAPPER_DEADLINE="2"))
    elapsed = time.monotonic() - start
    assert p.returncode == 2, (p.returncode, p.stderr)
    assert "BLOCKED [tq/guard-error]" in p.stderr
    assert elapsed < 10, elapsed


def test_tq_hook_deadline_is_under_the_hook_timeout():
    """The deadline, plus the second its kill takes, stays under the timeout in hooks.json, and an
    override can only lower it."""
    src = HOOK.read_text(encoding="utf-8")
    deadline = int(re.search(r"^_tq_deadline=(\d+)$", src, re.M).group(1))
    hooks = json.loads(HOOKS_JSON.read_text(encoding="utf-8"))
    timeouts = [h["timeout"] for g in hooks["hooks"]["PreToolUse"] for h in g["hooks"] if "tq_hook.sh" in h["command"]]
    assert timeouts and deadline + 1 + 5 <= min(timeouts), (deadline, timeouts)
    assert '-lt "$_tq_deadline"' in src


@needs_bash
@pytest.mark.parametrize("status", [0, 2])
def test_tq_hook_passes_a_decision_through(tmp_path, status):
    fake_tq(tmp_path, "cat >/dev/null; exit %d" % status)
    p = run("pre-tool-use", env_with(tmp_path))
    assert p.returncode == status
    assert "guard-error" not in p.stderr


@needs_bash
@pytest.mark.parametrize("tool", ["Bash", "PowerShell", "Read", "Write", "mcp__x__y", ""])
def test_tq_hook_denies_every_tool_when_tq_is_missing(tmp_path, tool):
    """With no tq nothing can judge a call, so every tool is denied. This is stricter than the old
    Python fallback, which let file tools through."""
    p = run("pre-tool-use", env_with(tmp_path), payload=json.dumps({"tool_name": tool}))
    assert p.returncode == 2
    assert "BLOCKED [tq/guard-error]" in p.stderr and "tq is not installed" in p.stderr


@needs_bash
def test_tq_hook_denies_when_tq_cannot_execute(tmp_path):
    """A resolved but unrunnable tq (exit 126/127) denies."""
    fake = tmp_path / "tq"
    fake.write_text("not-an-executable\n")
    fake.chmod(0o755)
    p = run("pre-tool-use", env_with(tmp_path))
    assert p.returncode == 2
    assert "tq is not installed" in p.stderr


@needs_bash
def test_tq_hook_session_start_says_tq_is_missing(tmp_path):
    p = run("session-start", env_with(tmp_path), payload="{}")
    assert p.returncode == 0
    assert "tq is not installed" in p.stdout


@needs_bash
@pytest.mark.parametrize("event", ["bogus", "stop", "prompt-submit"])
def test_tq_hook_unknown_event_blocks(tmp_path, event):
    """A misconfigured hook cannot silently turn enforcement off, and tq is not started."""
    fake_tq(tmp_path, "echo ran")
    p = run(event, env_with(tmp_path), payload="{}")
    assert p.returncode == 2
    assert "unknown event" in p.stderr
    assert "ran" not in p.stdout


@needs_bash
def test_tq_hook_execs_tq_when_present(tmp_path):
    fake_tq(tmp_path, 'echo "argv:$*"; cat; exit 0')
    p = run("session-start", env_with(tmp_path), payload="{}")
    assert p.returncode == 0
    assert "argv:claude-hook session-start" in p.stdout
    assert "{}" in p.stdout


@needs_bash
def test_tq_bin_is_not_honoured(tmp_path):
    """No environment variable picks the enforcing binary: an agent can set a variable."""
    elsewhere = tmp_path / "elsewhere"
    elsewhere.mkdir()
    fake = elsewhere / "tq"
    fake.write_text('#!/bin/sh\necho "argv:$*"; exit 0\n')
    fake.chmod(0o755)
    env = env_with(tmp_path, TQ_BIN=str(fake), PATH=os.path.dirname(BASH))
    p = run("session-start", env, payload="{}")
    assert p.returncode == 0
    assert "argv:" not in p.stdout, "the TQ_BIN binary ran"
    assert "tq is not installed" in p.stdout
    assert "TQ_BIN" not in HOOK.read_text(encoding="utf-8")


@needs_bash
def test_tq_hook_never_runs_a_tq_from_the_plugin_folder(tmp_path):
    """The plugin ships no binary: a tq planted in the plugin folder is never run."""
    root = tmp_path / "plugin"
    for name in ("tq", "tq.exe"):
        planted = root / "bin" / name
        planted.parent.mkdir(parents=True, exist_ok=True)
        planted.write_text('#!/bin/sh\necho "planted-ran $*"\nexit 0\n')
        planted.chmod(0o755)
    env = env_with(tmp_path, plugin_root=root, PATH=os.path.dirname(BASH))
    p = run("session-start", env, payload="{}")
    assert p.returncode == 0
    assert "planted-ran" not in p.stdout + p.stderr, "a tq from the plugin folder ran"
    assert "tq is not installed" in p.stdout


def test_hooks_json_pretooluse_matcher_covers_the_shell_tools_only():
    hooks = json.loads(HOOKS_JSON.read_text(encoding="utf-8"))
    entry = next(e for e in hooks["hooks"]["PreToolUse"] if "tq_hook.sh" in e["hooks"][0]["command"])
    rx = re.compile(entry["matcher"])
    for tool in ("Bash", "PowerShell"):
        assert rx.search(tool), tool
    for tool in ("Read", "Edit", "Write", "Agent", "mcp__x__y", "BashX", "TodoWrite"):
        assert not rx.search(tool), tool


def test_the_plugin_holds_only_the_guard_hook():
    assert not (ROOT / "skills").exists()
    hooks = json.loads(HOOKS_JSON.read_text(encoding="utf-8"))["hooks"]
    assert sorted(hooks) == ["PreToolUse", "SessionStart"]
    for group in hooks.values():
        for g in group:
            for h in g["hooks"]:
                assert "tq_hook.sh" in h["command"]
                assert h["timeout"]
    assert not list(ROOT.rglob("*.py")) or all("tests" in p.parts for p in ROOT.rglob("*.py"))
