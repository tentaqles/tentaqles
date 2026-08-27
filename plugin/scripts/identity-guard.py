#!/usr/bin/env python3
"""
PreToolUse hook bridge (Bash) — guards identity and blocks forbidden commands.

Reads JSON from stdin (cwd, tool_input.command), checks the command against
the client manifest's blocked commands and identity expectations.

Exit codes:
  0 — allow (pass or no manifest)
  2 — BLOCK (identity mismatch or blocked command)
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



def _run_cmd(cmd: str, timeout: int = 5) -> str:
    """Run a shell command and return stripped stdout, or empty string on failure."""
    try:
        proc = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
        return proc.stdout.strip()
    except Exception:
        return ""


def _block(message: str) -> None:
    """Print error to stderr and exit with code 2 (BLOCK)."""
    print(message, file=sys.stderr)
    sys.exit(2)


def _command_starts_with(command: str, prefix: str) -> bool:
    """Check if the command starts with a given CLI tool name."""
    # Match the tool name at word boundary: "git commit" matches "git", "github" does not
    pattern = r"(?:^|&&|\|\||;|\|)\s*" + re.escape(prefix) + r"(?:\s|$)"
    return bool(re.search(pattern, command))


def _is_cloud_command(command: str, provider: str) -> bool:
    """Check if command uses a specific cloud CLI."""
    cli_map = {
        "azure": ["az"],
        "aws": ["aws"],
        "gcp": ["gcloud", "gsutil", "bq"],
        "digitalocean": ["doctl"],
    }
    for cli in cli_map.get(provider, []):
        if _command_starts_with(command, cli):
            return True
    return False


def _get_cloud_clis() -> dict:
    """Map of cloud CLI prefixes to provider names."""
    return {
        "az": "azure",
        "aws": "aws",
        "gcloud": "gcp",
        "gsutil": "gcp",
        "bq": "gcp",
        "doctl": "digitalocean",
    }


def main() -> None:
    # Read stdin
    try:
        raw = sys.stdin.read()
    except Exception:
        raw = "{}"

    try:
        payload = json.loads(raw) if raw.strip() else {}
    except (json.JSONDecodeError, TypeError):
        payload = {}

    cwd = payload.get("cwd", os.getcwd())
    tool_input = payload.get("tool_input", {})
    if isinstance(tool_input, str):
        try:
            tool_input = json.loads(tool_input)
        except (json.JSONDecodeError, TypeError):
            tool_input = {}

    command = tool_input.get("command", "")
    if not command:
        sys.exit(0)

    # Load manifest — if missing, allow everything
    try:
        from tentaqles.manifest.loader import load_manifest
    except ImportError:
        sys.exit(0)

    manifest = load_manifest(cwd)
    if manifest is None:
        sys.exit(0)

    cloud = manifest.get("cloud", {}) or {}
    git = manifest.get("git", {}) or {}

    # --- Check blocked commands ---
    blocked: list[str] = []
    for bl in cloud.get("blocked_commands", []):
        if isinstance(bl, str):
            blocked.append(bl)
    for bl in git.get("blocked_commands", []):
        if isinstance(bl, str):
            blocked.append(bl)

    for bl_cmd in blocked:
        if _command_starts_with(command, bl_cmd) or bl_cmd in command:
            _block(
                f"BLOCKED: Command '{bl_cmd}' is blocked by client manifest.\n"
                f"  Client: {manifest.get('client', 'unknown')}"
            )

    # --- Check wrong-provider cloud commands ---
    expected_provider = cloud.get("provider", "").lower()
    if expected_provider and expected_provider != "none":
        cloud_clis = _get_cloud_clis()
        for cli_prefix, provider_name in cloud_clis.items():
            if _command_starts_with(command, cli_prefix):
                if provider_name != expected_provider:
                    _block(
                        f"BLOCKED: This workspace uses {expected_provider}, "
                        f"but you ran a {provider_name} command ('{cli_prefix}').\n"
                        f"  Client: {manifest.get('client', 'unknown')}"
                    )

    # --- Git identity check ---
    if _command_starts_with(command, "git"):
        expected_email = git.get("email", "")
        if expected_email:
            actual_email = _run_cmd("git config user.email")
            if actual_email and actual_email.lower() != expected_email.lower():
                _block(
                    f"BLOCKED: Git email mismatch.\n"
                    f"  Expected: {expected_email}\n"
                    f"  Actual:   {actual_email}\n"
                    f'  Fix: git config user.email "{expected_email}"'
                )

    # --- GitHub identity check ---
    if _command_starts_with(command, "gh"):
        expected_user = git.get("user", "")
        if expected_user:
            actual_user = _run_cmd("gh api user --jq .login")
            if actual_user and actual_user.lower() != expected_user.lower():
                _block(
                    f"BLOCKED: GitHub user mismatch.\n"
                    f"  Expected: {expected_user}\n"
                    f"  Actual:   {actual_user}\n"
                    f"  Fix: switch GitHub auth to the correct account"
                )

    # --- Cloud identity check ---
    if expected_provider and expected_provider != "none":
        if _is_cloud_command(command, expected_provider):
            preflight_cmd = cloud.get("preflight", "")
            expected_value = cloud.get("expected") or cloud.get("expected_user", "")
            if preflight_cmd and expected_value:
                actual_value = _run_cmd(preflight_cmd)
                if actual_value and expected_value not in actual_value:
                    _block(
                        f"BLOCKED: Cloud identity mismatch ({expected_provider}).\n"
                        f"  Expected: {expected_value}\n"
                        f"  Actual:   {actual_value}\n"
                        f"  Command:  {preflight_cmd}"
                    )

    # All checks passed — allow
    sys.exit(0)


if __name__ == "__main__":
    main()
