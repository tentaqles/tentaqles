"""The workspace state block that has to survive context compaction.

Claude Code does not pass a PreCompact hook's output to the model, so a block
printed there was silently dropped. The same block is now emitted by the
SessionStart hook when `source == "compact"` -- that output *is* injected into
the fresh, post-compaction context.
"""

from __future__ import annotations

from pathlib import Path

DEFAULT_TOKEN_BUDGET = 600

HEADER = "# Tentaqles context (restored after compaction)"


def build_compact_block(cwd: str, header: str = HEADER) -> str:
    """Return the redacted re-injection block for the workspace at cwd.

    Empty string when there is no manifest-backed memory or anything fails:
    a hook printing nothing is always a valid no-op.
    """
    manifest = None
    client_root = cwd
    budget = DEFAULT_TOKEN_BUDGET
    try:
        from tentaqles.manifest.loader import load_manifest

        manifest = load_manifest(cwd)
        if manifest:
            client_root = manifest.get("_client_root", cwd)
            budget = int(manifest.get("precompact_token_budget", DEFAULT_TOKEN_BUDGET))
    except Exception:
        pass

    # Never create a memory.db as a side effect of reading one.
    if not manifest:
        return ""
    try:
        if not (Path(client_root) / ".claude" / "memory.db").is_file():
            return ""
    except Exception:
        return ""

    block = ""
    try:
        from tentaqles.memory.store import MemoryStore

        store = MemoryStore(client_root)
        try:
            if hasattr(store, "get_compact_context"):
                block = store.get_compact_context(budget)
            else:
                block = store.get_context_summary()
        finally:
            store.close()
    except Exception:
        block = ""

    if not block or "(no prior memory recorded)" in block:
        return ""

    try:
        from tentaqles.privacy import redact_text

        block, _ = redact_text(block)
    except Exception:
        pass

    client = manifest.get("client", "unknown")
    display = manifest.get("display_name", client)
    lines = [header, f"_Workspace: {display} ({client})_"]
    return "\n".join(lines) + "\n\n" + block
