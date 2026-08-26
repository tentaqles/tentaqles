#!/usr/bin/env python3
"""Tentaqles bootstrap — installs Python dependencies on first run.

Runs on SessionStart BEFORE session-preamble. Idempotent: checks if a
sentinel file exists and bails immediately on subsequent runs. Installs
dependencies into ${CLAUDE_PLUGIN_DATA}/lib so they're isolated from the
user's global Python environment.

On failure, prints a warning to stderr explaining the manual install
command. Never blocks session start.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

# Ensure UTF-8 stdout/stderr on Windows
if sys.platform == "win32":
    try:
        import io
        if hasattr(sys.stdout, "buffer"):
            sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding="utf-8", errors="replace")
            sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding="utf-8", errors="replace")
    except Exception:
        pass


REQUIRED_DEPS = [
    "pyyaml",
    "pathspec",
    "fastembed",
    "numpy",
]

# Core packages that must be importable for the plugin to work at all.
CORE_IMPORTS = ["yaml", "pathspec", "numpy"]
# fastembed is heavy — check separately and fail more gracefully
OPTIONAL_IMPORTS = ["fastembed"]


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


def main() -> None:
    # Read hook payload (we don't actually use it, just consume stdin)
    try:
        sys.stdin.read()
    except Exception:
        pass

    plugin_data = os.environ.get("CLAUDE_PLUGIN_DATA", "")
    if not plugin_data:
        # Fall back to ~/.tentaqles when running outside the plugin harness
        plugin_data = str(Path.home() / ".tentaqles")

    lib_dir = Path(plugin_data) / "lib"
    sentinel = Path(plugin_data) / ".bootstrap-complete"

    # Fast path: sentinel exists and core imports work
    if sentinel.is_file() and _check_core_available(lib_dir):
        return

    # Check if core deps are already available from system Python
    if _check_core_available(lib_dir):
        sentinel.parent.mkdir(parents=True, exist_ok=True)
        sentinel.write_text("system", encoding="utf-8")
        return

    # Need to install
    _log(f"installing dependencies into {lib_dir} (first run, may take a few minutes)")
    ok = _run_pip_install(lib_dir, REQUIRED_DEPS)
    if ok:
        sentinel.parent.mkdir(parents=True, exist_ok=True)
        sentinel.write_text("installed", encoding="utf-8")
        _log("bootstrap complete")
    else:
        _log("")
        _log("Automatic install failed. Please run manually:")
        _log(f"  pip install --target \"{lib_dir}\" pyyaml pathspec fastembed numpy")
        _log("")
        _log("Or install globally:")
        _log("  pip install pyyaml pathspec fastembed numpy")


if __name__ == "__main__":
    main()
