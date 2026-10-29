"""tq_hook.sh and tq_run.sh exit 0 at once under TQ_MODE=off (spec E §8.4); TQ_ENABLED=0 is shell activation only.

They must do so before resolving tq or Python, so the fail-closed "no tq, no
Python" exit 2 never fires in off. The box below has neither: TQ_BIN points
nowhere and every python name on PATH is a shim that fails.
"""

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
HOOK = ROOT / "scripts" / "tq_hook.sh"
RUN = ROOT / "scripts" / "tq_run.sh"


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
pytestmark = pytest.mark.skipif(BASH is None, reason="needs bash")


def _bare_env(tmp_path, **extra):
    """No tq (TQ_BIN points nowhere) and no working Python on PATH."""
    shims = tmp_path / "shims"
    shims.mkdir()
    for name in ("python3", "python", "py"):
        shim = shims / name
        shim.write_text("#!/bin/sh" + chr(10) + "echo shim-ran >&2" + chr(10) + "exit 1" + chr(10))
        shim.chmod(0o755)
    env = {k: v for k, v in os.environ.items() if k not in ("TENTAQLES_PY", "TQ_MODE", "TQ_ENABLED")}
    env.update(
        # PATH is replaced, not prepended, on purpose (a deliberate exception
        # to "prepend fakes"): the test proves the scripts exit before they
        # look for tq or Python, so no real tq or Python may be reachable. A
        # prepended PATH would still reach the developer's tq and python3.
        # Only the shims and bash's own directory stay.
        PATH=os.pathsep.join([str(shims), os.path.dirname(BASH)]),
        TQ_BIN=str(tmp_path / "nope"),
        HOME=str(tmp_path),
        USERPROFILE=str(tmp_path),
        LOCALAPPDATA=str(tmp_path),
        CLAUDE_PLUGIN_ROOT=str(ROOT),
        CLAUDE_PLUGIN_DATA=str(tmp_path / "data"),
    )
    env.update(extra)
    return env


PAYLOAD = json.dumps({"cwd": ".", "tool_name": "Bash", "tool_input": {"command": "cat .env"}})


@pytest.mark.parametrize("var,val", [("TQ_MODE", "off")])
@pytest.mark.parametrize("event", ["pre-tool-use", "session-start", "stop", "prompt-submit"])
def test_tq_hook_exits_zero_when_off(tmp_path, var, val, event):
    p = subprocess.run([BASH, str(HOOK), event], input=PAYLOAD, capture_output=True, text=True,
                       env=_bare_env(tmp_path, **{var: val}), timeout=30)
    assert p.returncode == 0, p.stderr
    assert p.stdout == "" and p.stderr == ""


@pytest.mark.parametrize("var,val", [("TQ_MODE", "off")])
def test_tq_hook_off_even_for_an_unknown_event(tmp_path, var, val):
    p = subprocess.run([BASH, str(HOOK), "bogus"], input="", capture_output=True, text=True,
                       env=_bare_env(tmp_path, **{var: val}), timeout=30)
    assert p.returncode == 0 and p.stderr == ""


def test_tq_hook_still_fails_closed_when_on(tmp_path):
    """Control: the same bare box without off blocks the pre-tool-use call."""
    p = subprocess.run([BASH, str(HOOK), "pre-tool-use"], input=PAYLOAD, capture_output=True, text=True,
                       env=_bare_env(tmp_path), timeout=30)
    assert p.returncode == 2


@pytest.mark.parametrize("var,val", [("TQ_MODE", "off")])
def test_tq_run_exits_zero_when_off(tmp_path, var, val):
    p = subprocess.run([BASH, str(RUN), "memory-bridge.py", "session-start"], input="", capture_output=True,
                       text=True, env=_bare_env(tmp_path, **{var: val}), timeout=30)
    assert p.returncode == 0, p.stderr
    assert p.stdout == "" and p.stderr == ""


def test_tq_run_control_without_off(tmp_path):
    """Control: without off, the bare box cannot run the script."""
    p = subprocess.run([BASH, str(RUN), "memory-bridge.py", "session-start"], input="", capture_output=True,
                       text=True, env=_bare_env(tmp_path), timeout=60)
    assert p.returncode != 0 or p.stderr != ""


@pytest.mark.parametrize("var,val", [("TQ_ENABLED", "0"), ("TQ_MODE", "OFF"), ("TQ_MODE", " off")])
def test_tq_hook_runs_when_not_exactly_off(tmp_path, var, val):
    """TQ_ENABLED=0 only skips shell activation, and off is exactly `off`: the hook still fails closed."""
    p = subprocess.run([BASH, str(HOOK), "pre-tool-use"], input=PAYLOAD, capture_output=True, text=True,
                       env=_bare_env(tmp_path, **{var: val}), timeout=30)
    assert p.returncode == 2


@pytest.mark.parametrize("var,val", [("TQ_ENABLED", "0"), ("TQ_MODE", "OFF")])
def test_tq_run_runs_when_not_exactly_off(tmp_path, var, val):
    p = subprocess.run([BASH, str(RUN), "memory-bridge.py", "session-start"], input="", capture_output=True,
                       text=True, env=_bare_env(tmp_path, **{var: val}), timeout=60)
    assert p.returncode != 0 or p.stderr != ""
