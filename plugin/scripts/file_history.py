#!/usr/bin/env python3
"""File history query — show all Tentaqles records about a file.

Usage: python file_history.py <path>

Resolves the path to an absolute form, then looks up the node_id in the
memory store. If not found at the absolute path, tries the as-provided form
(paths may have been stored relative). Outputs formatted history to stdout.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import json
from pathlib import Path


def resolve_candidate_ids(path_arg: str, cwd: str) -> list[str]:
    """Return candidate node_id strings to try, in priority order.

    Files may be stored as:
    - absolute Windows paths: C:/repos/acme/src/auth.py
    - absolute Unix-style paths: /c/repos/acme/src/auth.py
    - relative paths: src/auth.py
    - as-provided value from user

    Try absolute first (most common), then as-provided, then relative to cwd.
    """
    candidates = []
    # As-provided
    candidates.append(path_arg)
    # Absolute resolved
    try:
        abs_path = str(Path(path_arg).resolve())
        if abs_path not in candidates:
            candidates.append(abs_path)
        # Normalize separators
        normalized = abs_path.replace("\\", "/")
        if normalized not in candidates:
            candidates.append(normalized)
    except Exception:
        pass
    # Relative to cwd (stripped)
    try:
        rel = os.path.relpath(path_arg, cwd) if os.path.isabs(path_arg) else path_arg
        if rel not in candidates:
            candidates.append(rel)
    except Exception:
        pass
    return candidates


def format_history(node_id: str, data: dict) -> str:
    """Format enriched history dict as human-readable markdown."""
    lines = [f"# File history: {node_id}", ""]

    touches = data.get("touches", [])
    if not touches:
        lines.append("_No touches recorded for this file._")
        return "\n".join(lines)

    lines.append(f"## Touches ({len(touches)})")
    lines.append("")
    for t in touches[:30]:  # cap at 30 most recent
        date = t.get("touched_at", "?")[:10]
        action = t.get("action", "?")
        weight = t.get("weight", "?")
        summary = t.get("session_summary") or "(no summary)"
        summary = summary[:100] + ("..." if len(summary) > 100 else "")
        lines.append(f"- **{date}** — `{action}` (weight {weight})")
        lines.append(f"  _session_: {summary}")
    lines.append("")

    decisions = data.get("related_decisions", [])
    if decisions:
        lines.append(f"## Related decisions ({len(decisions)})")
        lines.append("")
        for d in decisions[:10]:
            date = d.get("created_at", "?")[:10]
            chosen = d.get("chosen", "?")
            rationale = (d.get("rationale", "") or "")[:120]
            conf = d.get("confidence", "?")
            lines.append(f"- **{date}** [{conf}] {chosen}")
            if rationale:
                lines.append(f"  _why_: {rationale}")

    return "\n".join(lines)


def main() -> None:
    if len(sys.argv) < 2:
        print("Usage: file_history.py <path>", file=sys.stderr)
        sys.exit(1)

    path_arg = sys.argv[1]
    cwd = os.getcwd()

    try:
        from tentaqles.manifest.loader import load_manifest
        from tentaqles.memory.store import MemoryStore
    except ImportError as e:
        print(f"[tentaqles] memory modules not available: {e}", file=sys.stderr)
        sys.exit(0)

    try:
        manifest = load_manifest(cwd)
        client_root = manifest.get("_client_root", cwd) if manifest else cwd

        store = MemoryStore(client_root)

        candidates = resolve_candidate_ids(path_arg, cwd)
        data = None
        found_id = None

        for candidate in candidates:
            # Prefer the enriched method if available
            if hasattr(store, "get_node_history_enriched"):
                result = store.get_node_history_enriched(candidate, limit=50)
                if result and result.get("touches"):
                    data = result
                    found_id = candidate
                    break
            else:
                # Fallback to basic method
                touches = store.get_node_history(candidate, limit=50)
                if touches:
                    data = {"touches": touches, "related_decisions": [], "node_id": candidate}
                    found_id = candidate
                    break

        try:
            store.close()
        except Exception:
            pass

        if not data:
            print(f"# File history: {path_arg}\n\n_No history found for this file._")
            print(f"\nTried: {', '.join(candidates)}")
            return

        print(format_history(found_id, data))
    except Exception as e:
        print(f"[tentaqles] file_history error: {e}", file=sys.stderr)
        sys.exit(0)


if __name__ == "__main__":
    main()
