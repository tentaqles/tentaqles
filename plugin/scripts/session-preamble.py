#!/usr/bin/env python3
"""
SessionStart hook bridge — generates memory/temporal context for Claude Code.

Reads JSON from stdin (cwd, session_id) and prints workspace context.

Identity enforcement (git/gh/az/doctl) is owned by the Go CLI
(`tq claude-hook session-start`); this script NEVER mutates any identity.

With --memory-only (how the hook invokes it) only the memory/temporal
context is printed. Without the flag the client header + preflight
warnings are printed too, for direct/legacy invocation.
"""

import os
import sys

# Bootstrap sys.path for plugin imports (tentaqles.* + bootstrapped deps)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import json
import subprocess
from pathlib import Path


FALLBACK_CONTEXT = "Tentaqles plugin active. No client manifest found for this workspace."


def _run(cmd: str, cwd: str | None = None, timeout: int = 5) -> tuple[int, str]:
    """Run a read-only shell command, return (exit_code, combined_output)."""
    try:
        r = subprocess.run(
            cmd, shell=True, capture_output=True, text=True, timeout=timeout, cwd=cwd
        )
        return r.returncode, (r.stdout + r.stderr).strip()
    except subprocess.TimeoutExpired:
        return 124, "<timeout>"
    except OSError as e:
        return 1, str(e)


def _get_temporal_context(cwd: str, context: dict) -> str:
    """Attempt to load temporal context from the memory store."""
    try:
        client_root = context.get("client_root", "")
        if not client_root:
            return ""

        db_path = Path(client_root) / ".claude" / "memory.db"
        if not db_path.is_file():
            return ""

        from tentaqles.memory.store import MemoryStore

        store = MemoryStore(client_root)
        summary = store.get_context_summary()

        # F7: Semantic facts
        try:
            facts = store.get_semantic_facts(limit=5)
            if facts:
                facts_lines = ["## Semantic facts"] + [f"- {f['fact']}" for f in facts]
                summary = summary + "\n\n" + "\n".join(facts_lines) if summary else "\n".join(facts_lines)
        except Exception:
            pass

        # F10: Workspace profile
        try:
            from tentaqles.memory.profiler import WorkspaceProfiler
            prof_manager = WorkspaceProfiler(store, client_root)
            profile = prof_manager.load()
            if profile is not None and not prof_manager.is_stale():
                profile_lines = ["## Workspace profile"]
                if profile.get("summary_sentence"):
                    profile_lines.append(profile["summary_sentence"])
                hot = profile.get("hot_files", [])[:3]
                if hot:
                    profile_lines.append("Hot files: " + ", ".join(h["path"] for h in hot))
                concepts = profile.get("top_concepts", [])[:3]
                if concepts:
                    profile_lines.append("Concepts: " + ", ".join(c.get("label", "") for c in concepts if c.get("label")))
                summary = summary + "\n\n" + "\n".join(profile_lines) if summary else "\n".join(profile_lines)
            else:
                # Profile missing or stale — regenerate inline with 5s timeout
                import threading

                def _regen():
                    try:
                        from tentaqles.memory.profiler import WorkspaceProfiler as _WP
                        _WP(store, client_root).generate()
                    except Exception:
                        pass

                t = threading.Thread(target=_regen, daemon=True)
                t.start()
                t.join(timeout=5)
        except Exception:
            pass

        # F12: Incoming signals
        try:
            manifest_signals = context.get("signals", {})
            if manifest_signals.get("enabled"):
                client_name = context.get("client", "unknown")
                from tentaqles.memory.signals import SignalBus
                bus = SignalBus()
                signals = bus.read_pending(client_name)
                if signals:
                    sig_lines = ["## Incoming signals"]
                    for sig in signals:
                        sig_lines.append(
                            f"- [{sig['event_type']}] from {sig['from_workspace']}: {sig['message']}"
                        )
                        try:
                            bus.acknowledge(sig["id"], client_name)
                        except Exception:
                            pass
                    summary = summary + "\n\n" + "\n".join(sig_lines) if summary else "\n".join(sig_lines)
        except Exception:
            pass

        store.close()
        return summary
    except Exception:
        return ""


def main() -> None:
    memory_only = "--memory-only" in sys.argv[1:]

    try:
        raw = sys.stdin.read()
    except Exception:
        raw = "{}"

    try:
        payload = json.loads(raw) if raw.strip() else {}
    except (json.JSONDecodeError, TypeError):
        payload = {}

    cwd = payload.get("cwd", os.getcwd())

    try:
        from tentaqles.manifest.loader import (
            format_context_summary,
            get_client_context,
            load_manifest,
            run_preflight_checks,
        )

        manifest = load_manifest(cwd)
        context = get_client_context(cwd)

        if context.get("client", "unknown") == "unknown":
            if not memory_only:
                print(FALLBACK_CONTEXT)
            return

        if memory_only:
            temporal = _get_temporal_context(cwd, context)
            if temporal:
                print(temporal)
            return

        try:
            checks = run_preflight_checks(manifest or context, session_cwd=cwd)
        except TypeError:
            # Older loader without session_cwd arg — fall back
            checks = run_preflight_checks(manifest or context)

        # Suppress git preflight warnings when not in a git repo (can't run
        # git ops there anyway).
        in_git_repo, _ = _run("git rev-parse --git-dir", cwd=cwd)
        if in_git_repo != 0:
            checks = [c for c in checks if c.get("section") != "git"]

        summary = format_context_summary(context, checks)

        temporal = _get_temporal_context(cwd, context)
        if temporal:
            summary += "\n\n" + temporal

        print(summary)

    except ImportError:
        if not memory_only:
            print(FALLBACK_CONTEXT)
    except Exception:
        if not memory_only:
            print(FALLBACK_CONTEXT)


if __name__ == "__main__":
    main()
