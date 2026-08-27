#!/usr/bin/env python3
"""
SessionStart hook bridge — generates client context preamble for Claude Code.

Reads JSON from stdin (cwd, session_id), loads client manifest,
AUTO-SWITCHES identity (gh, git includeIf, az, doctl) to match the
manifest, runs preflight checks, and outputs combined context to stdout.
"""

import os
import sys

# Bootstrap sys.path for plugin imports (tentaqles.* + bootstrapped deps)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import io
import json
import os
import re
import subprocess
import sys
from pathlib import Path


FALLBACK_CONTEXT = "Tentaqles plugin active. No client manifest found for this workspace."


def _run(cmd: str, cwd: str | None = None, timeout: int = 5) -> tuple[int, str]:
    """Run a shell command, return (exit_code, combined_output)."""
    try:
        r = subprocess.run(
            cmd, shell=True, capture_output=True, text=True, timeout=timeout, cwd=cwd
        )
        return r.returncode, (r.stdout + r.stderr).strip()
    except subprocess.TimeoutExpired:
        return 124, "<timeout>"
    except OSError as e:
        return 1, str(e)


def _setup_git_includeif(
    client_root: str, expected_email: str, expected_name: str | None
) -> bool:
    """Configure git conditional include so all repos under client_root get this identity.

    Idempotent: returns True only if a change was made.
    """
    norm_root = client_root.replace("\\", "/").rstrip("/") + "/"
    gitconfig_path = Path(client_root) / ".gitconfig-tentaqles"
    gitconfig_path_str = str(gitconfig_path).replace("\\", "/")

    desired = f"[user]\n    email = {expected_email}\n"
    if expected_name:
        desired += f"    name = {expected_name}\n"

    current_content = ""
    if gitconfig_path.exists():
        try:
            current_content = gitconfig_path.read_text(encoding="utf-8")
        except OSError:
            pass

    changed = False
    if current_content.strip() != desired.strip():
        try:
            gitconfig_path.write_text(desired, encoding="utf-8")
            changed = True
        except OSError:
            return False

    include_key = f"includeIf.gitdir:{norm_root}.path"
    code, current = _run(f'git config --global --get "{include_key}"')
    if code != 0 or current.strip() != gitconfig_path_str:
        _run(f'git config --global "{include_key}" "{gitconfig_path_str}"')
        changed = True

    return changed


def auto_switch_identity(manifest: dict, cwd: str) -> list[str]:
    """Switch gh, git, az, doctl to match the manifest. Returns list of actions."""
    actions: list[str] = []
    git = manifest.get("git", {}) or {}
    cloud = manifest.get("cloud", {}) or {}
    client_root = manifest.get("_client_root", cwd) or cwd

    # F9: capture snapshot before identity switch
    try:
        from tentaqles.snapshots.manager import SnapshotManager
        SnapshotManager(client_root).capture(
            reason="auto-switch",
            manifest_data=manifest,
        )
    except Exception:
        pass

    # --- Git identity via includeIf (works for any sub-repo under client_root) ---
    expected_email = git.get("email")
    expected_name = manifest.get("display_name")
    if expected_email and client_root:
        try:
            if _setup_git_includeif(client_root, expected_email, expected_name):
                actions.append(f"git.includeIf: configured for {client_root}")
        except Exception:
            pass

    # --- GitHub CLI auto-switch ---
    # For github, ALWAYS prefer git.user (the username). Reject email-looking
    # values since a common mistake is to put the email in expected_user.
    if git.get("provider") == "github":
        expected_user = git.get("user") or git.get("expected_user")
        if expected_user and "@" in str(expected_user):
            expected_user = None
    else:
        expected_user = None

    if expected_user:
        code, active_out = _run("gh auth status --active 2>&1")
        active_user = None
        if code == 0:
            m = re.search(r"account\s+(\S+)", active_out)
            if m:
                active_user = m.group(1).strip()

        if active_user != expected_user:
            code_all, all_out = _run("gh auth status 2>&1")
            known = re.findall(r"account\s+(\S+)", all_out) if code_all == 0 else []
            if expected_user in known:
                code_sw, out_sw = _run(f"gh auth switch --user {expected_user}")
                if code_sw == 0:
                    actions.append(f"gh: {active_user or '<none>'} -> {expected_user}")
                else:
                    actions.append(
                        f"gh: FAILED to switch to {expected_user}: {out_sw[:100]}"
                    )
            else:
                actions.append(
                    f"gh: {expected_user} not authenticated (active={active_user or 'none'}). "
                    f"Run: gh auth login"
                )

    # --- Azure CLI subscription switch ---
    if cloud.get("provider") == "azure":
        expected_sub = cloud.get("expected") or cloud.get("subscription_name")
        if expected_sub:
            code, current = _run('az account show --query name -o tsv')
            if code == 0 and current != expected_sub:
                code2, _ = _run(f'az account set --subscription "{expected_sub}"')
                if code2 == 0:
                    actions.append(f"az: subscription -> {expected_sub}")
                else:
                    actions.append(f"az: FAILED to switch to {expected_sub}")

    # --- DigitalOcean context switch ---
    if cloud.get("provider") == "digitalocean":
        expected_ctx = cloud.get("context")
        if expected_ctx:
            code, current = _run("doctl auth list")
            if expected_ctx in current:
                _run(f"doctl auth switch --context {expected_ctx}")
                actions.append(f"doctl: context -> {expected_ctx}")
            else:
                actions.append(
                    f"doctl: {expected_ctx} not authenticated. Run: doctl auth init"
                )

    return actions


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
                regen_result = {}

                def _regen():
                    try:
                        from tentaqles.memory.profiler import WorkspaceProfiler as _WP
                        _WP(store, client_root).generate()
                        regen_result["done"] = True
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
            print(FALLBACK_CONTEXT)
            return

        # Auto-switch identity before running preflight
        switch_actions: list[str] = []
        if manifest:
            try:
                switch_actions = auto_switch_identity(manifest, cwd)
            except Exception as e:
                switch_actions = [f"auto-switch failed: {e}"]

        # Run preflight from the actual session cwd (so git includeIf applies)
        try:
            checks = run_preflight_checks(manifest or context, session_cwd=cwd)
        except TypeError:
            # Older loader without session_cwd arg — fall back
            checks = run_preflight_checks(manifest or context)

        # Suppress git preflight warnings when not in a git repo (can't run
        # git ops there anyway, and includeIf applies once user enters a sub-repo)
        in_git_repo, _ = _run("git rev-parse --git-dir", cwd=cwd)
        if in_git_repo != 0:
            checks = [c for c in checks if c.get("section") != "git"]

        summary = format_context_summary(context, checks)

        if switch_actions:
            summary += "\n\nAuto-switch:"
            for a in switch_actions:
                summary += f"\n  * {a}"

        temporal = _get_temporal_context(cwd, context)
        if temporal:
            summary += "\n\n" + temporal

        print(summary)

    except ImportError:
        print(FALLBACK_CONTEXT)
    except Exception:
        print(FALLBACK_CONTEXT)


if __name__ == "__main__":
    main()
