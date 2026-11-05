"""tq_env.sh keeps Python off the bytecode next to the plugin's sources.

The plugin folder's ``__pycache__`` is outside the signed integrity record
(``tq hooks verify`` skips it, Python rewrites it), so a ``.pyc`` planted there
must never be what runs. tq_env.sh exports ``PYTHONDONTWRITEBYTECODE=1`` and
``PYTHONPYCACHEPREFIX`` (under the plugin data dir): Python then looks for
bytecode only under the prefix, and writes none anywhere.
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
        "import sys\nfrom _path import setup_paths\nsetup_paths()\nprint('probe-ran', sys.implementation.cache_tag)\n",
        encoding="utf-8",
    )
    return root


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

    # No bytecode is written, next to the sources or under the prefix.
    assert sorted(x.name for x in forged.parent.iterdir()) == [forged.name]
    assert not list((tmp_path / "data").rglob("*.pyc"))


def test_tq_env_exports_outside_its_guard():
    """The exports run even when TENTAQLES_PY arrives set (the guard skips the rest)."""
    text = (SCRIPTS / "tq_env.sh").read_text(encoding="utf-8")
    tail = text[text.index("fi  # end guard"):]
    assert "export PYTHONDONTWRITEBYTECODE=1" in tail
    assert 'export PYTHONPYCACHEPREFIX="${CLAUDE_PLUGIN_DATA:-$HOME/.cache}/tentaqles-pycache"' in tail
