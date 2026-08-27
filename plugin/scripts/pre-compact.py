#!/usr/bin/env python3
"""PreCompact hook — re-injects critical workspace state before context compaction.

Fires when Claude Code is about to auto-compact the conversation. Reads the
session's cwd from stdin JSON, loads the client manifest and memory store,
and prints a compact re-injection block to stdout.

Everything written to stdout is treated as additional context that Claude
should preserve through compaction.
"""

import os
import sys

# Bootstrap sys.path for plugin imports (tentaqles.* + bootstrapped deps)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import json
from pathlib import Path


FALLBACK = ""  # Empty on failure — never crash the compaction flow


def main() -> None:
    # 1. Read hook payload from stdin
    try:
        raw = sys.stdin.read()
    except Exception:
        raw = "{}"
    try:
        payload = json.loads(raw) if raw.strip() else {}
    except (json.JSONDecodeError, TypeError):
        payload = {}

    cwd = payload.get("cwd", os.getcwd())

    # 2. Load manifest to find client root + configured token budget
    manifest = None
    client_root = cwd
    token_budget = 600

    try:
        from tentaqles.manifest.loader import load_manifest
        manifest = load_manifest(cwd)
        if manifest:
            client_root = manifest.get("_client_root", cwd)
            # Allow per-client override via manifest
            token_budget = int(manifest.get("precompact_token_budget", 600))
    except Exception:
        pass

    # 3. Load memory store and build compact context
    block = ""
    try:
        from tentaqles.memory.store import MemoryStore
        store = MemoryStore(client_root)
        # Use the new method added by the store.py agent.
        # If not yet available, fall back to get_context_summary.
        if hasattr(store, "get_compact_context"):
            block = store.get_compact_context(max_tokens=token_budget)
        else:
            block = store.get_context_summary()
        store.close()
    except Exception:
        block = ""

    # 4. Redact through privacy filter
    if block:
        try:
            from tentaqles.privacy import redact_text
            block, _ = redact_text(block)
        except Exception:
            pass

    # 5. Add a header so the injected block is self-labeling in the compaction summary
    if block:
        header = "# Tentaqles context preservation (PreCompact)\n"
        # Include manifest summary if available
        if manifest:
            client = manifest.get("client", "unknown")
            display = manifest.get("display_name", client)
            header += f"_Workspace: {display} ({client})_\n\n"
        print(header + block)
    else:
        # Never output anything on failure — an empty PreCompact hook is a valid no-op
        print(FALLBACK, end="")


if __name__ == "__main__":
    main()
