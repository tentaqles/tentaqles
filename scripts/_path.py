"""Shared sys.path bootstrap for all Tentaqles plugin scripts.

Every script in this directory should start with:

    from _path import setup_paths
    setup_paths()

This adds:
1. ${CLAUDE_PLUGIN_ROOT} to sys.path (so `tentaqles.*` is importable)
2. ${CLAUDE_PLUGIN_DATA}/lib to sys.path (so bootstrap-installed deps work)
3. Configures UTF-8 encoding on Windows
"""

from __future__ import annotations

import io
import os
import sys
from pathlib import Path


def setup_paths() -> tuple[str, str]:
    """Set up sys.path for plugin scripts. Returns (plugin_root, plugin_data)."""

    # UTF-8 encoding on Windows
    if sys.platform == "win32":
        try:
            sys.stdout = io.TextIOWrapper(
                sys.stdout.buffer, encoding="utf-8", errors="replace"
            )
            sys.stderr = io.TextIOWrapper(
                sys.stderr.buffer, encoding="utf-8", errors="replace"
            )
        except Exception:
            pass

    # Find plugin root: this file lives at ${CLAUDE_PLUGIN_ROOT}/scripts/_path.py
    plugin_root = os.environ.get(
        "CLAUDE_PLUGIN_ROOT",
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    )

    # Find plugin data dir (where bootstrap installs pip deps)
    plugin_data = os.environ.get("CLAUDE_PLUGIN_DATA", "")
    if not plugin_data:
        # Platform-appropriate fallback for standalone use
        if sys.platform == "darwin":
            plugin_data = str(Path.home() / "Library" / "Application Support" / "tentaqles")
        elif sys.platform != "win32":
            xdg = os.environ.get("XDG_DATA_HOME", "")
            base = Path(xdg) if xdg else Path.home() / ".local" / "share"
            plugin_data = str(base / "tentaqles")
        else:
            plugin_data = str(Path.home() / ".tentaqles")

    # Add plugin data lib dir FIRST (bootstrap-installed deps take priority)
    lib_dir = os.path.join(plugin_data, "lib")
    if os.path.isdir(lib_dir):
        sys.path.insert(0, lib_dir)

    # Then plugin root (so `tentaqles.*` package resolves)
    sys.path.insert(0, plugin_root)

    return plugin_root, plugin_data
