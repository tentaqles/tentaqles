#!/usr/bin/env python3
"""Tentaqles bootstrap — installs Python dependencies on first run.

Runs on SessionStart BEFORE session-preamble. Idempotent: checks if a
sentinel file exists and bails immediately on subsequent runs. Installs
dependencies into ${CLAUDE_PLUGIN_DATA}/lib so they're isolated from the
user's global Python environment.

Never blocks session start. A pip install (fastembed alone is a large
download) used to run inside the hook with a 600s timeout, so a first session
could sit frozen for minutes. When deps are missing the hook now hands the
install to a detached copy of this script (`--worker`, via
_detach.spawn_detached), prints one line saying so, and returns. A lock file
(`.bootstrap.lock` in the data dir) keeps two sessions from installing at
once; a lock older than LOCK_STALE_SECONDS is treated as abandoned.

The worker logs to ${CLAUDE_PLUGIN_DATA}/bootstrap.log, including the manual
install command if pip fails.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from pathlib import Path

def _utf8_stdio() -> None:
    """Ensure UTF-8 stdout/stderr on Windows (only when run as a script)."""
    if sys.platform != "win32":
        return
    try:
        import io
        if hasattr(sys.stdout, "buffer"):
            sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
            sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")
    except Exception:
        pass


# Installed for real users on first run. fastembed is included deliberately
# even though it is optional at runtime (it lives in the `embeddings` extra in
# pyproject.toml): bootstrap is where the plugin can afford to fetch it once,
# so semantic search works out of the box.
REQUIRED_DEPS = [
    "pyyaml",
    "pathspec",
    "fastembed",
    "numpy",
]

# Core packages that must be importable for the plugin to work at all.
# fastembed is not among them: every write path degrades to a NULL embedding
# without it (pinned by tests/test_embeddings_optional.py).
CORE_IMPORTS = ["yaml", "pathspec", "numpy"]

LOCK_NAME = ".bootstrap.lock"
LOG_NAME = "bootstrap.log"

# A worker holds the lock for at most pip's own timeout (600s) plus slack;
# anything older belongs to a worker that died without cleaning up.
LOCK_STALE_SECONDS = 20 * 60

# One notice for "just started" and "already running": when the hook goes
# through tq_run.sh, tq_env.sh has usually started the worker a moment earlier.
NOTICE = (
    "Tentaqles: installing Python dependencies in the background (first run); "
    "memory features switch on once it finishes, usually within a few minutes."
)
NOTICE_STARTED = NOTICE
NOTICE_RUNNING = NOTICE


def _log(msg: str) -> None:
    """Write a log line to stderr (won't pollute hook stdout)."""
    print(f"[tentaqles bootstrap] {msg}", file=sys.stderr)


def _check_core_available(lib_dir: Path) -> bool:
    """Return True if all core deps are importable from either site-packages or lib_dir."""
    if lib_dir.is_dir():
        sys.path.insert(0, str(lib_dir))
    for mod in CORE_IMPORTS:
        try:
            __import__(mod)
        except ImportError:
            return False
    return True


def _resolve_executable() -> str:
    """Resolve the real Python executable, bypassing Windows Store stubs."""
    exe = sys.executable
    if sys.platform == "win32" and "WindowsApps" in exe:
        # Windows Store Python uses an app-execution alias stub that breaks pip --target.
        # Try the py launcher which resolves to the real interpreter.
        import shutil
        py = shutil.which("py")
        if py:
            import subprocess as _sp
            try:
                real = _sp.run([py, "-3", "-c", "import sys; print(sys.executable)"],
                               capture_output=True, text=True, timeout=10).stdout.strip()
                if real and Path(real).is_file():
                    return real
            except Exception:
                pass
    return exe


def _run_pip_install(target_dir: Path, packages: list[str]) -> bool:
    """Install packages into target_dir using pip --target. Returns True on success."""
    target_dir.mkdir(parents=True, exist_ok=True)
    exe = _resolve_executable()
    cmd = [
        exe,
        "-m", "pip", "install",
        "--quiet",
        "--disable-pip-version-check",
        # --upgrade lets pip replace packages already in target_dir; without
        # it a lib built for another Python (wrong compiled ABI) never heals.
        "--upgrade",
        "--target", str(target_dir),
        *packages,
    ]
    try:
        r = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=600,  # 10 minutes for heavy deps like fastembed
        )
        if r.returncode != 0:
            _log(f"pip install failed: {r.stderr[:500]}")
            return False
        return True
    except subprocess.TimeoutExpired:
        _log("pip install timed out after 600s")
        return False
    except (OSError, FileNotFoundError) as e:
        _log(f"pip not available: {e}")
        return False


def _plugin_data() -> Path:
    plugin_data = os.environ.get("CLAUDE_PLUGIN_DATA", "")
    if not plugin_data:
        # Fall back to ~/.tentaqles when running outside the plugin harness
        plugin_data = str(Path.home() / ".tentaqles")
    return Path(plugin_data)


def _acquire_lock(lock: Path) -> bool:
    """Create the lock atomically. False if a live install already holds it."""
    lock.parent.mkdir(parents=True, exist_ok=True)
    for _ in range(2):
        try:
            fd = os.open(str(lock), os.O_CREAT | os.O_EXCL | os.O_WRONLY)
        except FileExistsError:
            try:
                age = time.time() - lock.stat().st_mtime
            except OSError:
                continue  # vanished between the two calls: try again
            if age < LOCK_STALE_SECONDS:
                return False
            try:
                lock.unlink()
            except OSError:
                return False
            continue
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            fh.write(json.dumps({"pid": os.getpid(), "started": time.time()}))
        return True
    return False


def _release_lock(lock: Path) -> None:
    try:
        lock.unlink()
    except OSError:
        pass


def _spawn_worker() -> None:
    """Start `bootstrap.py --worker` detached. Raises OSError on failure."""
    scripts_dir = os.path.dirname(os.path.abspath(__file__))
    if scripts_dir not in sys.path:
        sys.path.insert(0, scripts_dir)
    from _detach import spawn_detached

    spawn_detached([sys.executable, os.path.abspath(__file__), "--worker"])


def _manual_hint(lib_dir: Path) -> None:
    _log("Automatic install failed. Please run manually:")
    _log(f"  pip install --target \"{lib_dir}\" pyyaml pathspec fastembed numpy")
    _log("Or install globally:")
    _log("  pip install pyyaml pathspec fastembed numpy")


def install(plugin_data: Path) -> bool:
    """Do the actual install (worker side). Returns True on success."""
    lib_dir = plugin_data / "lib"
    sentinel = plugin_data / ".bootstrap-complete"
    _log(f"installing dependencies into {lib_dir}")
    ok = _run_pip_install(lib_dir, REQUIRED_DEPS)
    if ok:
        sentinel.parent.mkdir(parents=True, exist_ok=True)
        sentinel.write_text("installed", encoding="utf-8")
        _log("bootstrap complete")
    else:
        _manual_hint(lib_dir)
    return ok


def _redirect_log(plugin_data: Path) -> None:
    """The detached worker has no console worth writing to: log to a file."""
    try:
        plugin_data.mkdir(parents=True, exist_ok=True)
        sys.stderr = open(plugin_data / LOG_NAME, "a", encoding="utf-8")
    except OSError:
        pass


def run_worker() -> bool:
    """Detached side: install, then always release the lock."""
    plugin_data = _plugin_data()
    lock = plugin_data / LOCK_NAME
    try:
        _redirect_log(plugin_data)
        return install(plugin_data)
    finally:
        _release_lock(lock)


def run_hook() -> str:
    """Hook side: never installs inline. Returns the notice to print (or "")."""
    plugin_data = _plugin_data()
    lib_dir = plugin_data / "lib"
    sentinel = plugin_data / ".bootstrap-complete"

    # Fast path: sentinel exists and core imports work
    if sentinel.is_file() and _check_core_available(lib_dir):
        return ""

    # Check if core deps are already available from system Python
    if _check_core_available(lib_dir):
        sentinel.parent.mkdir(parents=True, exist_ok=True)
        sentinel.write_text("system", encoding="utf-8")
        return ""

    lock = plugin_data / LOCK_NAME
    if not _acquire_lock(lock):
        return NOTICE_RUNNING

    try:
        _spawn_worker()
    except Exception as exc:
        _release_lock(lock)
        _log(f"could not start the background install: {exc}")
        _manual_hint(lib_dir)
        return ""
    return NOTICE_STARTED


def main() -> None:
    if sys.argv[1:2] == ["--worker"]:
        run_worker()
        return

    # Read hook payload (we don't actually use it, just consume stdin)
    try:
        sys.stdin.read()
    except Exception:
        pass

    notice = run_hook()
    if notice:
        print(notice)


if __name__ == "__main__":
    _utf8_stdio()
    main()
