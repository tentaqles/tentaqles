"""tq_env.sh keeps Python off the bytecode next to the plugin's sources.

The plugin folder's ``__pycache__`` is outside the signed integrity record
(``tq hooks verify`` skips it, Python rewrites it), so a ``.pyc`` planted there
must never be what runs. tq_env.sh exports ``PYTHONDONTWRITEBYTECODE=1`` and
``PYTHONPYCACHEPREFIX=<tq home>/pycache``, inside the state folder tq
protects, ``<tq home>`` resolved as tq's ``paths.Home`` does: Python then
looks for bytecode only under the prefix, and writes none anywhere.
"""

from __future__ import annotations

import importlib.util
import marshal
import os
import shutil
import struct
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
SCRIPTS = ROOT / "scripts"
MARKER = "FORGED-PYC-LOADED"


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


def _forge_path_pyc(scripts_dir: Path) -> Path:
    """Write scripts/__pycache__/_path.<tag>.pyc holding code that prints the
    marker, with the timestamp header of the real _path.py (its mtime and
    size), so a Python that reads the cache takes it for _path."""
    src = scripts_dir / "_path.py"
    st = src.stat()
    code = compile(f"print({MARKER!r})\ndef setup_paths():\n    return ('', '')\n", str(src), "exec")
    header = importlib.util.MAGIC_NUMBER + struct.pack("<III", 0, int(st.st_mtime) & 0xFFFFFFFF, st.st_size & 0xFFFFFFFF)
    pyc = scripts_dir / "__pycache__" / f"_path.{sys.implementation.cache_tag}.pyc"
    pyc.parent.mkdir(exist_ok=True)
    pyc.write_bytes(header + marshal.dumps(code))
    return pyc


@pytest.fixture
def plugin_copy(tmp_path):
    """A plugin folder with the runner, the bootstrap and _path.py, plus a
    probe script that imports _path the way every plugin script does."""
    root = tmp_path / "plugin"
    (root / "scripts").mkdir(parents=True)
    for name in ("_path.py", "tq_run.sh", "tq_env.sh"):
        shutil.copy2(SCRIPTS / name, root / "scripts" / name)
    (root / "scripts" / "probe.py").write_text(
        "import sys\nfrom _path import setup_paths\nsetup_paths()\nprint('probe-ran', sys.implementation.cache_tag)\n"
        "print('prefix', sys.pycache_prefix)\n",
        encoding="utf-8",
    )
    return root


def _same_path(a: str, b) -> bool:
    return os.path.normcase(os.path.normpath(a)) == os.path.normcase(os.path.normpath(str(b)))


def _env(tmp_path, root):
    drop = ("PYTHONDONTWRITEBYTECODE", "PYTHONPYCACHEPREFIX", "PYTHONPATH", "TENTAQLES_PY")
    env = {k: v for k, v in os.environ.items() if k not in drop}
    env.update(
        CLAUDE_PLUGIN_ROOT=str(root),
        CLAUDE_PLUGIN_DATA=str(tmp_path / "data"),
        HOME=str(tmp_path),
        USERPROFILE=str(tmp_path),
    )
    return env


def test_a_forged_pyc_next_to_the_sources_is_not_loaded(tmp_path, plugin_copy):
    scripts = plugin_copy / "scripts"
    forged = _forge_path_pyc(scripts)

    # Control: Python run directly takes the forged bytecode for _path, so
    # the forgery is one Python would load.
    direct = subprocess.run([sys.executable, str(scripts / "probe.py")], capture_output=True, text=True,
                            env=_env(tmp_path, plugin_copy), timeout=60)
    assert MARKER in direct.stdout, direct.stdout + direct.stderr

    # Through tq_run.sh (as every hook runs a script), with this interpreter.
    env = _env(tmp_path, plugin_copy)
    env["TENTAQLES_PY"] = Path(sys.executable).as_posix()
    p = subprocess.run([BASH, str(scripts / "tq_run.sh"), "probe.py"], capture_output=True, text=True, env=env, timeout=120)
    assert p.returncode == 0, p.stderr
    assert f"probe-ran {sys.implementation.cache_tag}" in p.stdout, p.stdout
    assert MARKER not in p.stdout + p.stderr
    # tq_run.sh exports CLAUDE_PLUGIN_ROOT, an agent marker: the prefix is in
    # ~/.tentaqles whatever TQ_HOME says, as the hook's tq reads it.
    prefix = next(line for line in p.stdout.splitlines() if line.startswith("prefix "))[len("prefix "):]
    assert _same_path(prefix, tmp_path / ".tentaqles" / "pycache"), prefix

    # No bytecode is written, next to the sources or under the prefix: the
    # forged file is the only one there is.
    assert sorted(tmp_path.rglob("*.pyc")) == [forged]


def _script_markers():
    """The agent markers tq_env.sh checks (tq's internal/markers list; a Go
    test pins the two together)."""
    text = (SCRIPTS / "tq_env.sh").read_text(encoding="utf-8")
    line = next(l for l in text.splitlines() if l.startswith("for _tq_m in "))
    return line[len("for _tq_m in "):].split(";")[0].split()


def _prefix_after_sourcing(tmp_path, **env_extra):
    """PYTHONPYCACHEPREFIX after sourcing tq_env.sh with TENTAQLES_PY set (the
    guard skips the rest, the exports still run), from an environment with no
    agent marker but the ones given."""
    drop = {"PYTHONPYCACHEPREFIX", "TQ_HOME", *_script_markers()}
    env = {k: v for k, v in os.environ.items() if k.upper() not in drop}
    env.update(HOME=str(tmp_path), USERPROFILE=str(tmp_path), TENTAQLES_PY=Path(sys.executable).as_posix())
    env.update(env_extra)
    script = '. "$1"; printf "%s|%s" "$PYTHONPYCACHEPREFIX" "$PYTHONDONTWRITEBYTECODE"'
    p = subprocess.run([BASH, "-c", script, "probe", str(SCRIPTS / "tq_env.sh")], capture_output=True, text=True, env=env, timeout=60)
    assert p.returncode == 0, p.stderr
    prefix, nowrite = p.stdout.split("|")
    assert nowrite == "1"
    return prefix


def test_the_prefix_is_in_the_tq_home_as_paths_home_resolves_it(tmp_path):
    """<tq home>/pycache on every OS: TQ_HOME when no agent marker is set,
    ~/.tentaqles when TQ_HOME is unset or a marker is set."""
    other = tmp_path / "other-home"
    assert _same_path(_prefix_after_sourcing(tmp_path, TQ_HOME=str(other)), other / "pycache")
    assert _same_path(_prefix_after_sourcing(tmp_path), tmp_path / ".tentaqles" / "pycache")
    assert len(_script_markers()) == 11
    for marker in _script_markers():
        got = _prefix_after_sourcing(tmp_path, TQ_HOME=str(other), **{marker: "1"})
        assert _same_path(got, tmp_path / ".tentaqles" / "pycache"), (marker, got)
