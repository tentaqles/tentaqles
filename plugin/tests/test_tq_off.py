"""tq_hook.sh and tq_run.sh have no off switch: they fail closed on a bare box whatever the environment says.

The off launchers (claude-off, codex-off, `tq run --off`) switch the plugin
off through Claude's --settings and clean the identity environment; no
variable marks the launch, so nothing here exits early. TQ_ENABLED=0 keeps
its one meaning, shell activation only: the hook still fails closed under
it. The box below has neither tq nor Python: TQ_BIN points nowhere and every
python name on PATH is a shim that fails.
"""

import json
import os
import shutil
import subprocess
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
    env = {k: v for k, v in os.environ.items() if k not in ("TENTAQLES_PY", "TQ_ENABLED")}
    env.update(
        # PATH is replaced, not prepended, on purpose (a deliberate exception
        # to "prepend fakes"): the test proves the scripts never find a tq or
        # a Python, so no real one may be reachable. A prepended PATH would
        # still reach the developer's tq and python3. Only the shims and
        # bash's own directory stay.
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


@pytest.mark.parametrize("extra", [{}, {"TQ_ENABLED": "0"}])
def test_tq_hook_fails_closed_on_a_bare_box(tmp_path, extra):
    """No tq, no Python: the pre-tool-use hook blocks, with or without the kill switch."""
    p = subprocess.run([BASH, str(HOOK), "pre-tool-use"], input=PAYLOAD, capture_output=True, text=True,
                       env=_bare_env(tmp_path, **extra), timeout=30)
    assert p.returncode == 2


def test_tq_hook_unknown_event_exits_two(tmp_path):
    p = subprocess.run([BASH, str(HOOK), "bogus"], input="", capture_output=True, text=True,
                       env=_bare_env(tmp_path), timeout=30)
    assert p.returncode == 2 and "unknown event" in p.stderr


@pytest.mark.parametrize("extra", [{}, {"TQ_ENABLED": "0"}])
def test_tq_run_cannot_run_on_a_bare_box(tmp_path, extra):
    p = subprocess.run([BASH, str(RUN), "memory-bridge.py", "session-start"], input="", capture_output=True,
                       text=True, env=_bare_env(tmp_path, **extra), timeout=60)
    assert p.returncode != 0 or p.stderr != ""


def test_tq_bin_is_not_honoured(tmp_path):
    """TQ_BIN never picks the binary (PRD v2 §8.3): an agent can set a variable, and the tq that
    enforces must not be its choice. A fake tq named only by TQ_BIN never runs; with no tq on PATH
    the hook falls back as on a bare box."""
    elsewhere = tmp_path / "elsewhere"
    elsewhere.mkdir()
    fake = elsewhere / "tq"
    fake.write_text('#!/bin/sh\necho "argv:$*"; exit 0\n')
    fake.chmod(0o755)
    env = {k: v for k, v in os.environ.items() if k not in ("TENTAQLES_PY", "TQ_ENABLED")}
    env.update(
        PATH=os.path.dirname(BASH),  # bash and its coreutils only: no tq anywhere on PATH
        TQ_BIN=str(fake),
        HOME=str(tmp_path),
        USERPROFILE=str(tmp_path),
        LOCALAPPDATA=str(tmp_path),
        CLAUDE_PLUGIN_ROOT=str(ROOT),
    )
    p = subprocess.run([BASH, str(HOOK), "session-start"], input="{}", capture_output=True, text=True,
                       env=env, timeout=30)
    assert p.returncode == 0
    assert "argv:" not in p.stdout, "the TQ_BIN binary ran"
    assert "tq is not installed" in p.stdout
    assert "TQ_BIN" not in HOOK.read_text(encoding="utf-8")


def test_tq_hook_never_runs_a_tq_from_the_plugin_folder(tmp_path):
    """The plugin ships no binary: a tq planted in $CLAUDE_PLUGIN_ROOT/bin is never run.
    With no tq on PATH or in the install dirs, the hook falls back as on a bare box."""
    root = tmp_path / "plugin"
    for name in ("tq", "tq.exe"):
        planted = root / "bin" / name
        planted.parent.mkdir(parents=True, exist_ok=True)
        planted.write_text('#!/bin/sh\necho "planted-ran $*"\nexit 0\n')
        planted.chmod(0o755)
    env = {k: v for k, v in os.environ.items() if k not in ("TENTAQLES_PY", "TQ_ENABLED", "TQ_BIN")}
    env.update(
        PATH=os.path.dirname(BASH),  # bash and its coreutils only: no tq anywhere on PATH
        HOME=str(tmp_path),
        USERPROFILE=str(tmp_path),
        LOCALAPPDATA=str(tmp_path),
        CLAUDE_PLUGIN_ROOT=str(root),
    )
    p = subprocess.run([BASH, str(HOOK), "session-start"], input="{}", capture_output=True, text=True,
                       env=env, timeout=30)
    assert p.returncode == 0
    assert "planted-ran" not in p.stdout + p.stderr, "a tq from the plugin folder ran"
    assert "tq is not installed" in p.stdout
    assert "CLAUDE_PLUGIN_ROOT/bin" not in HOOK.read_text(encoding="utf-8")


def test_scripts_have_no_off_switch():
    """Neither script exits early on an environment variable (the off launcher switches the plugin off instead)."""
    for script in (HOOK, RUN):
        text = script.read_text(encoding="utf-8")
        assert "exit 0 ;; esac" not in text, script
