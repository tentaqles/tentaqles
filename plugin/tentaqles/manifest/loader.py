"""
Tentaqles client manifest loader.

Discovers and loads .tentaqles.yaml files by walking up from the current
working directory, providing client context for Claude Code sessions.
"""

from __future__ import annotations

import subprocess
from pathlib import Path
from typing import Any

import yaml


def find_manifest(cwd: str | Path) -> Path | None:
    """Walk up from cwd looking for .tentaqles.yaml. Stop at root or after 10 levels."""
    current = Path(cwd).resolve()
    for _ in range(10):
        candidate = current / ".tentaqles.yaml"
        if candidate.is_file():
            return candidate
        parent = current.parent
        if parent == current:
            break
        current = parent
    return None


def load_manifest(cwd: str | Path) -> dict | None:
    """Locate and parse .tentaqles.yaml, validating schema version."""
    path = find_manifest(cwd)
    if path is None:
        return None
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = yaml.safe_load(f)
        if not isinstance(data, dict):
            return None
        if data.get("schema") not in ("tentaqles-client-v1", "tentaqles-client-v2"):
            return None
        data["_manifest_path"] = str(path)
        data["_client_root"] = str(path.parent)
        return data
    except (OSError, yaml.YAMLError):
        return None


def _extract_section(manifest: dict, key: str) -> dict:
    """Safely extract a section dict from the manifest."""
    val = manifest.get(key)
    return dict(val) if isinstance(val, dict) else {}


def get_client_context(cwd: str | Path) -> dict:
    """
    Build a rich client context dict from the manifest.

    Returns an empty context (client "unknown") when no manifest is found.
    """
    manifest = load_manifest(cwd)

    if manifest is not None:
        signals_raw = manifest.get("signals") or {}
        return {
            "client": manifest.get("client", "unknown"),
            "display_name": manifest.get("display_name", manifest.get("client", "unknown")),
            "language": manifest.get("language", "en"),
            "manifest_path": manifest.get("_manifest_path", ""),
            "client_root": manifest.get("_client_root", ""),
            "cloud": _extract_section(manifest, "cloud"),
            "database": _extract_section(manifest, "database"),
            "git": _extract_section(manifest, "git"),
            "project_management": _extract_section(manifest, "project_management"),
            "stack": manifest.get("stack", []),
            "signals": {
                "enabled": bool(signals_raw.get("enabled", False)),
                "subscribe_to": signals_raw.get("subscribe_to", []),
            },
        }

    empty: dict[str, Any] = {
        "client": "unknown",
        "display_name": "Unknown",
        "language": "en",
        "manifest_path": "",
        "client_root": "",
        "cloud": {},
        "database": {},
        "git": {},
        "project_management": {},
        "stack": [],
        "signals": {"enabled": False, "subscribe_to": []},
    }
    return empty


def run_preflight_checks(manifest_or_context: dict) -> list[dict]:
    """
    Run preflight commands defined in manifest sections (cloud, git, etc.)
    and compare output against expected values.

    Accepts either a raw manifest dict or a get_client_context() result.
    The manifest format uses flat fields:
        cloud:
          preflight: "az account show --query name -o tsv"
          expected: "PPU"
    """
    results: list[dict] = []
    sections_to_check = ["cloud", "git"]

    for section_name in sections_to_check:
        section = manifest_or_context.get(section_name)
        if not isinstance(section, dict):
            continue

        command = section.get("preflight")
        expected = section.get("expected") or section.get("expected_user")

        # Skip if no preflight command or it's not a string
        if not command or not isinstance(command, str):
            continue

        try:
            proc = subprocess.run(
                command,
                shell=True,
                capture_output=True,
                text=True,
                timeout=5,
            )
            actual = proc.stdout.strip()
        except subprocess.TimeoutExpired:
            actual = "<timeout>"
        except OSError:
            actual = "<error>"

        # For checks with multi-line output, check if expected appears anywhere
        if expected:
            passed = expected in actual
        else:
            passed = True

        results.append({
            "section": section_name,
            "check": command,
            "expected": str(expected) if expected is not None else "",
            "actual": actual,
            "passed": passed,
        })

    return results


def format_context_summary(context: dict, checks: list[dict]) -> str:
    """
    Generate a human-readable context summary (~300 tokens) for Claude Code
    session injection.
    """
    lines: list[str] = []

    display = context.get("display_name", context.get("client", "Unknown"))
    lang = context.get("language", "en")
    lines.append(f"Client: {display} ({lang})")

    cloud = context.get("cloud", {})
    if cloud and cloud.get("provider") and cloud["provider"] != "none":
        provider = cloud.get("provider", "")
        sub_name = cloud.get("subscription_name", "")
        cloud_str = provider
        if sub_name:
            cloud_str += f" ({sub_name} subscription)"
        lines.append(f"Cloud: {cloud_str}")

    db = context.get("database", {})
    if db and db.get("provider") and db["provider"] != "none":
        provider = db.get("provider", "")
        dialect = db.get("dialect", "")
        access = db.get("access", "")
        mcp = db.get("mcp_server", "")
        parts = [provider]
        if dialect and dialect != provider:
            parts.append(f"(dialect: {dialect})")
        if access:
            parts.append(f"via {access}")
        if mcp and access == "mcp":
            parts.append(f"[{mcp}]")
        lines.append(f"Database: {' '.join(parts)}")

    git = context.get("git", {})
    if git:
        host = git.get("host", git.get("provider", ""))
        user = git.get("user", "")
        email = git.get("email", "")
        git_parts = [host]
        if user:
            git_parts.append(f"as {user}")
        if email:
            git_parts.append(f"({email})")
        git_str = " ".join(p for p in git_parts if p)
        if git_str:
            lines.append(f"Git: {git_str}")

    pm = context.get("project_management", {})
    if pm and pm.get("provider") and pm["provider"] != "none":
        lines.append(f"PM: {pm['provider']}")

    stack = context.get("stack", [])
    if stack:
        lines.append(f"Stack: {', '.join(stack)}")

    # Append warnings from preflight checks
    warnings: list[str] = []
    for check in checks:
        if not check.get("passed", True):
            section = check.get("section", "")
            expected = check.get("expected", "")
            actual = check.get("actual", "")
            cmd = check.get("check", "")

            if section == "git" and "email" in cmd:
                warnings.append(
                    f"\u26a0 Git email mismatch: current={actual}, expected={expected}"
                    f'\n  \u2192 Fix: git config user.email "{expected}"'
                )
            elif section == "cloud":
                warnings.append(
                    f"\u26a0 Cloud check failed: expected={expected}, actual={actual}"
                    f"\n  \u2192 Command: {cmd}"
                )
            else:
                warnings.append(
                    f"\u26a0 {section} check failed: expected={expected}, actual={actual}"
                    f"\n  \u2192 Command: {cmd}"
                )

    if warnings:
        lines.append("")
        lines.extend(warnings)

    return "\n".join(lines)
