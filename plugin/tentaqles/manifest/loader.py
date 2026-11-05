"""
Tentaqles client manifest loader.

Discovers and loads the client manifest for a working directory, providing
client context for Claude Code sessions.

Two layouts exist (ADR-0002). In the protected layout the workspace root
holds a name-only marker, ``.tentaqles.yaml`` with ``schema:
tentaqles-marker-v1`` and ``client: <name>``, and the manifest lives at
``$TQ_HOME/identities/<name>/manifest.yaml`` (``~/.tentaqles`` when
``TQ_HOME`` is unset). In the old layout the full manifest sits at
``<root>/.tentaqles.yaml``. The loader walks up from the working directory
to the root file, then reads the protected copy when there is one; the
marker's ``client`` must equal the folder name, or the folder is unknown.
"""

from __future__ import annotations

import os
import subprocess
from pathlib import Path
from typing import Any

import yaml

MARKER_SCHEMA = "tentaqles-marker-v1"
MANIFEST_SCHEMAS = ("tentaqles-client-v1", "tentaqles-client-v2")
ROOT_FILE = ".tentaqles.yaml"
PROTECTED_FILE = "manifest.yaml"


def tq_home() -> Path:
    """``$TQ_HOME`` or ``~/.tentaqles``, as tq resolves it."""
    env = os.environ.get("TQ_HOME")
    return Path(env) if env else Path.home() / ".tentaqles"


def protected_manifest_path(name: str) -> Path:
    """The protected manifest of workspace ``name``."""
    return tq_home() / "identities" / name / PROTECTED_FILE


def is_protected_manifest_path(path: str | Path) -> bool:
    """Whether ``path`` is some workspace's protected manifest."""
    p = Path(path)
    if p.name != PROTECTED_FILE:
        return False
    try:
        return p.parent.parent.resolve() == (tq_home() / "identities").resolve()
    except OSError:
        return False


def _find_root_file(cwd: str | Path) -> Path | None:
    """Walk up from cwd looking for .tentaqles.yaml. Stop at root or after 10 levels."""
    current = Path(cwd).resolve()
    for _ in range(10):
        candidate = current / ROOT_FILE
        if candidate.is_file():
            return candidate
        parent = current.parent
        if parent == current:
            break
        current = parent
    return None


def _same_name(a: str, b: str) -> bool:
    """Folder names compare case-insensitively on Windows, as tq does."""
    if os.name == "nt":
        return a.casefold() == b.casefold()
    return a == b


def _read_yaml(path: Path) -> dict | None:
    try:
        with open(path, "r", encoding="utf-8") as f:
            data = yaml.safe_load(f)
    except (OSError, yaml.YAMLError):
        return None
    return data if isinstance(data, dict) else None


def _resolve_manifest(root_file: Path) -> Path | None:
    """The file to load for a root file: the protected copy or the root file itself.

    A marker whose client is not the folder name, or whose protected copy is
    missing, resolves to nothing: the folder is unknown, never a guess.
    """
    head = _read_yaml(root_file)
    if head is None:
        return None
    schema = head.get("schema")
    name = root_file.parent.name
    if schema == MARKER_SCHEMA:
        client = head.get("client")
        if not isinstance(client, str) or not client or not _same_name(client, name):
            return None
        pp = protected_manifest_path(name)
        return pp.resolve() if pp.is_file() else None
    if schema in MANIFEST_SCHEMAS:
        pp = protected_manifest_path(name)
        if pp.is_file():
            return pp.resolve()
    return root_file


def find_manifest(cwd: str | Path) -> Path | None:
    """Locate the manifest file read for cwd: the protected copy for a marked
    root, the root file itself in the old layout, None when there is none."""
    root_file = _find_root_file(cwd)
    if root_file is None:
        return None
    return _resolve_manifest(root_file)


def load_manifest(cwd: str | Path) -> dict | None:
    """Locate and parse the manifest, validating schema version.

    ``_manifest_path`` is the file actually loaded; ``_client_root`` is the
    folder that holds the root file (marker or manifest).
    """
    root_file = _find_root_file(cwd)
    if root_file is None:
        return None
    path = _resolve_manifest(root_file)
    if path is None:
        return None
    data = _read_yaml(path)
    if data is None:
        return None
    if data.get("schema") not in MANIFEST_SCHEMAS:
        return None
    data["_manifest_path"] = str(path)
    data["_client_root"] = str(root_file.parent)
    return data


def workspace_root_for(name: str, cwd: str | Path | None = None) -> Path | None:
    """The identity root of workspace ``name``: the folder whose marker (or
    manifest) names it. Tries, in order, the cwd walk-up, the identity
    shell's own ``TQ_WS``/``TQ_WS_ROOT``, then every ``<base>/<name>`` of the
    bases registered in ``$TQ_HOME/config.yaml``."""
    if cwd is not None:
        data = load_manifest(cwd)
        if data and _same_name(str(data.get("client", "")), name):
            return Path(data["_client_root"]).resolve()
    if _same_name(os.environ.get("TQ_WS", ""), name):
        ws_root = os.environ.get("TQ_WS_ROOT", "")
        if ws_root and (Path(ws_root) / ROOT_FILE).is_file():
            return Path(ws_root).resolve()
    cfg = _read_yaml(tq_home() / "config.yaml") or {}
    bases = cfg.get("bases") or []
    if not isinstance(bases, list):
        return None
    for base in bases:
        if not isinstance(base, str):
            continue
        root = Path(base) / name
        if (root / ROOT_FILE).is_file():
            return root.resolve()
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
                    f"⚠ Git email mismatch: current={actual}, expected={expected}"
                    f'\n  → Fix: git config user.email "{expected}"'
                )
            elif section == "cloud":
                warnings.append(
                    f"⚠ Cloud check failed: expected={expected}, actual={actual}"
                    f"\n  → Command: {cmd}"
                )
            else:
                warnings.append(
                    f"⚠ {section} check failed: expected={expected}, actual={actual}"
                    f"\n  → Command: {cmd}"
                )

    if warnings:
        lines.append("")
        lines.extend(warnings)

    return "\n".join(lines)
