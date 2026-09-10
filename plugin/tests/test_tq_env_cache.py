"""Tests for scripts/tq_env.sh interpreter/dependency caching.

Claude Code budgets *all* SessionEnd hooks with a single AbortSignal whose
default is a 1500ms floor (`CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` overrides
it). The per-hook `timeout` declared in a plugin's hooks.json does **not** raise
that budget. tq_env.sh therefore may not spend the budget re-discovering the
Python interpreter on every hook invocation: two extra interpreter spawns cost
~600ms of the 1500ms, which pushed session-end.py past the deadline and left
session rows half-written (started, never ended).

These tests pin the caching that keeps the hook inside the budget.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest


ENV_SH = Path(__file__).resolve().parent.parent / "scripts" / "tq_env.sh"
PLUGIN_ROOT = ENV_SH.parent.parent

bash = shutil.which("bash")
pytestmark = pytest.mark.skipif(bash is None, reason="bash not available")


def _make_counting_shims(bindir: Path, counter: Path) -> None:
    """Put py/python3/python shims on PATH that log every invocation.

    Each shim appends a line to `counter`, then delegates to the real
    interpreter so tq_env.sh still gets truthful answers.
    """
    bindir.mkdir(parents=True, exist_ok=True)
    real = sys.executable.replace("\\", "/")
    for name in ("py", "python3", "python"):
        shim = bindir / name
        shim.write_text(
            "#!/usr/bin/env bash\n"
            f'echo "{name}" >> "{counter.as_posix()}"\n'
            # drop a leading "-3" (the Windows py launcher selector)
            'if [ "$1" = "-3" ]; then shift; fi\n'
            # Report *this shim* as sys.executable so that the dependency
            # check — which invokes the resolved interpreter directly — is
            # counted too, not just the initial probe.
            'if [ "$1" = "-c" ] && [ "$2" = "import sys; print(sys.executable)" ]; then\n'
            f'  echo "{shim.as_posix()}"; exit 0\n'
            "fi\n"
            f'exec "{real}" "$@"\n',
            encoding="utf-8",
        )
        shim.chmod(0o755)


def _source_env_sh(tmp_path: Path, bindir: Path, data_dir: Path) -> subprocess.CompletedProcess:
    """Run a fresh shell that sources tq_env.sh, as a hook invocation would."""
    env = dict(os.environ)
    # Isolate: only the shims are discoverable, and the cache lives in tmp.
    env["PATH"] = f"{bindir.as_posix()}:/usr/bin:/bin"
    env["CLAUDE_PLUGIN_ROOT"] = str(PLUGIN_ROOT)
    env["CLAUDE_PLUGIN_DATA"] = str(data_dir)
    # Must not leak a pre-resolved interpreter between runs — each hook
    # invocation is a brand new process with none of this set.
    env.pop("TENTAQLES_PY", None)
    return subprocess.run(
        [bash, "-c", f'. "{ENV_SH.as_posix()}"; echo "PY=$TENTAQLES_PY"'],
        capture_output=True,
        text=True,
        env=env,
        cwd=str(tmp_path),
        timeout=120,
    )


def _count(counter: Path) -> int:
    if not counter.exists():
        return 0
    return len([ln for ln in counter.read_text(encoding="utf-8").splitlines() if ln.strip()])


def test_warm_cache_spawns_no_interpreter(tmp_path: Path) -> None:
    """The second invocation must not spawn Python at all.

    This is the regression guard for the SessionEnd budget: a warm run has to
    be effectively free, otherwise interpreter discovery eats the 1500ms.
    """
    bindir = tmp_path / "bin"
    data_dir = tmp_path / "data"
    data_dir.mkdir()
    counter = tmp_path / "spawns.log"
    _make_counting_shims(bindir, counter)

    cold = _source_env_sh(tmp_path, bindir, data_dir)
    assert "PY=" in cold.stdout, f"cold run failed: {cold.stdout!r} {cold.stderr!r}"
    cold_spawns = _count(counter)
    assert cold_spawns >= 1, "cold run should have probed for an interpreter"

    counter.write_text("", encoding="utf-8")
    warm = _source_env_sh(tmp_path, bindir, data_dir)
    assert "PY=" in warm.stdout, f"warm run failed: {warm.stdout!r} {warm.stderr!r}"
    assert _count(warm_counter := counter) == 0, (
        f"warm run spawned {_count(warm_counter)} interpreter(s); expected 0. "
        "Every spawn costs ~300ms of the 1500ms SessionEnd budget."
    )


def test_warm_cache_resolves_same_interpreter(tmp_path: Path) -> None:
    """Caching must not change which interpreter is selected."""
    bindir = tmp_path / "bin"
    data_dir = tmp_path / "data"
    data_dir.mkdir()
    counter = tmp_path / "spawns.log"
    _make_counting_shims(bindir, counter)

    cold = _source_env_sh(tmp_path, bindir, data_dir)
    warm = _source_env_sh(tmp_path, bindir, data_dir)
    assert cold.stdout.strip() == warm.stdout.strip()
    assert warm.stdout.strip() not in ("PY=", "PY=python3")


def test_dep_stamp_invalidated_when_lib_dir_disappears(tmp_path: Path) -> None:
    """Deleting the bootstrapped lib dir must re-trigger the dependency check.

    Skipping the check on a stale stamp would strand the plugin with missing
    deps and no self-healing, which is what bootstrap.py exists to prevent.
    """
    bindir = tmp_path / "bin"
    data_dir = tmp_path / "data"
    lib = data_dir / "lib"
    lib.mkdir(parents=True)
    counter = tmp_path / "spawns.log"
    _make_counting_shims(bindir, counter)

    _source_env_sh(tmp_path, bindir, data_dir)
    stamp = data_dir / ".deps-ok"
    assert stamp.exists(), "expected a dependency stamp after a successful check"
    stamped = stamp.read_text(encoding="utf-8").strip()
    assert stamped.endswith("|lib"), f"stamp should record the lib-dir state, got {stamped!r}"

    # Warm run with lib still present: no re-check.
    counter.write_text("", encoding="utf-8")
    _source_env_sh(tmp_path, bindir, data_dir)
    assert _count(counter) == 0

    # Remove the lib dir — the stamp must no longer be trusted.
    shutil.rmtree(lib)
    counter.write_text("", encoding="utf-8")
    _source_env_sh(tmp_path, bindir, data_dir)
    assert _count(counter) >= 1, (
        "dependency check was skipped even though the lib dir was deleted"
    )


def test_stale_cache_is_rediscovered(tmp_path: Path) -> None:
    """A cached interpreter that no longer exists must not be trusted."""
    bindir = tmp_path / "bin"
    data_dir = tmp_path / "data"
    data_dir.mkdir()
    counter = tmp_path / "spawns.log"
    _make_counting_shims(bindir, counter)

    _source_env_sh(tmp_path, bindir, data_dir)

    # Poison every cache file with a path that does not exist.
    poisoned = 0
    for cache in data_dir.iterdir():
        if cache.is_file():
            cache.write_text("/nonexistent/python\n", encoding="utf-8")
            poisoned += 1
    assert poisoned, "expected tq_env.sh to have written a cache file"

    counter.write_text("", encoding="utf-8")
    recovered = _source_env_sh(tmp_path, bindir, data_dir)
    assert "/nonexistent/python" not in recovered.stdout, (
        "stale cache was trusted instead of re-probing"
    )
    assert _count(counter) >= 1, "stale cache should have triggered a re-probe"
