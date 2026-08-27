"""Workspace bundle export/import service.

Provides portable bundles for sharing workspace configurations across
environments, with optional secret stripping.
"""

from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path

from sidecar.models import ImportResult
from sidecar.parsers import read_file_content
from sidecar.services.safe_io import atomic_write

_BUNDLE_VERSION = "tentaqles-bundle-v1"


def export_workspace(client_path: str, include_secrets: bool = False) -> dict:
    """Build a portable bundle dict from client workspace files.

    Collects config files, rules, skills, CLAUDE.md, brand context, and
    knowledge files into a single dict. Env values are redacted by default.
    """
    base = Path(client_path)

    # --- Core JSON config files ---
    workspace_profile: dict = {}
    raw = read_file_content(str(base / ".workspace-profile.json"))
    if raw:
        try:
            workspace_profile = json.loads(raw)
        except json.JSONDecodeError:
            pass

    claude_profile: dict = {}
    raw = read_file_content(str(base / ".claude-profile.json"))
    if raw:
        try:
            claude_profile = json.loads(raw)
        except json.JSONDecodeError:
            pass

    toggle_state: dict = {}
    raw = read_file_content(str(base / ".tentaqles.json"))
    if raw:
        try:
            toggle_state = json.loads(raw)
        except json.JSONDecodeError:
            pass

    # --- Secret stripping ---
    if not include_secrets and claude_profile:
        settings = claude_profile.get("settings", {})
        env = settings.get("env", {})
        if isinstance(env, dict) and env:
            claude_profile = dict(claude_profile)
            claude_profile["settings"] = dict(settings)
            claude_profile["settings"]["env"] = dict.fromkeys(env, "<REDACTED>")

    # --- Rules ---
    rules: dict[str, str] = {}
    rules_dir = base / ".claude" / "rules"
    if rules_dir.is_dir():
        for rule_file in sorted(rules_dir.glob("*.md")):
            content = read_file_content(str(rule_file))
            if content is not None:
                rules[rule_file.name] = content

    # --- Skills ---
    skills: dict[str, dict[str, str]] = {}
    skills_dir = base / ".claude" / "skills"
    if skills_dir.is_dir():
        for skill_dir in sorted(skills_dir.iterdir()):
            if not skill_dir.is_dir():
                continue
            skill_files: dict[str, str] = {}
            for skill_file in sorted(skill_dir.rglob("*")):
                if not skill_file.is_file():
                    continue
                content = read_file_content(str(skill_file))
                if content is not None:
                    rel = str(skill_file.relative_to(skill_dir)).replace("\\", "/")
                    skill_files[rel] = content
            if skill_files:
                skills[skill_dir.name] = skill_files

    # --- CLAUDE.md ---
    claude_md: str | None = read_file_content(str(base / "CLAUDE.md"))

    # --- Brand context ---
    brand_context: dict[str, str] = {}
    brand_dir = base / "brand_context"
    if brand_dir.is_dir():
        for bc_file in sorted(brand_dir.glob("*.md")):
            content = read_file_content(str(bc_file))
            if content is not None:
                brand_context[bc_file.name] = content

    # --- Personality ---
    personality: dict[str, str] = {}
    soul_content = read_file_content(str(base / ".claude" / "soul.md"))
    if soul_content is not None:
        personality["soul.md"] = soul_content
    user_content = read_file_content(str(base / ".claude" / "user.md"))
    if user_content is not None:
        personality["user.md"] = user_content

    # --- Knowledge ---
    knowledge: list[dict[str, str]] = []
    knowledge_dir = base / ".claude" / "knowledge"
    if knowledge_dir.is_dir():
        for k_file in sorted(knowledge_dir.glob("*.md")):
            content = read_file_content(str(k_file))
            if content is not None:
                knowledge.append({"filename": k_file.name, "content": content})

    # --- Client name from profile ---
    client_name = workspace_profile.get("client_name", base.name)

    return {
        "version": _BUNDLE_VERSION,
        "exported_at": datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "client_name": client_name,
        "workspace_profile": workspace_profile,
        "claude_profile": claude_profile,
        "toggle_state": toggle_state,
        "rules": rules,
        "skills": skills,
        "claude_md": claude_md,
        "brand_context": brand_context,
        "personality": personality,
        "knowledge": knowledge,
    }


def export_workspace_to_file(
    client_path: str, output_path: str, include_secrets: bool = False
) -> None:
    """Export workspace bundle to a JSON file using atomic_write."""
    bundle = export_workspace(client_path, include_secrets=include_secrets)
    atomic_write(output_path, json.dumps(bundle, indent=2, ensure_ascii=False) + "\n")


def import_workspace(bundle_path: str, target_path: str, merge: bool = False) -> ImportResult:
    """Import a workspace bundle into target_path.

    Args:
        bundle_path: Path to the bundle JSON file.
        target_path: Destination client directory.
        merge: If True, overwrite existing files. If False (default), skip them.

    Returns:
        ImportResult with counts of files written, skipped, and merged.

    Raises:
        ValueError: If bundle version does not match expected version.
    """
    raw = Path(bundle_path).read_text(encoding="utf-8")
    bundle: dict = json.loads(raw)

    version = bundle.get("version")
    if version != _BUNDLE_VERSION:
        raise ValueError(
            f"Unsupported bundle version: {version!r}. Expected {_BUNDLE_VERSION!r}."
        )

    client_name: str = bundle.get("client_name", "")
    base = Path(target_path)

    files_written: list[str] = []
    files_skipped: list[str] = []
    files_merged: list[str] = []
    warnings: list[str] = []

    def _write_file(rel_path: str, content: str) -> None:
        full_path = base / rel_path
        if full_path.exists():
            if merge:
                atomic_write(full_path, content)
                files_merged.append(rel_path)
            else:
                files_skipped.append(rel_path)
        else:
            atomic_write(full_path, content)
            files_written.append(rel_path)

    # --- workspace-profile.json ---
    wp = bundle.get("workspace_profile")
    if wp:
        _write_file(".workspace-profile.json", json.dumps(wp, indent=2, ensure_ascii=False) + "\n")

    # --- claude-profile.json ---
    cp = bundle.get("claude_profile")
    if cp:
        _write_file(".claude-profile.json", json.dumps(cp, indent=2, ensure_ascii=False) + "\n")

    # --- tentaqles.json ---
    ts = bundle.get("toggle_state")
    if ts:
        _write_file(".tentaqles.json", json.dumps(ts, indent=2, ensure_ascii=False) + "\n")

    # --- Rules ---
    for filename, content in bundle.get("rules", {}).items():
        _write_file(f".claude/rules/{filename}", content)

    # --- Skills ---
    for skill_name, skill_files in bundle.get("skills", {}).items():
        for rel_file, content in skill_files.items():
            _write_file(f".claude/skills/{skill_name}/{rel_file}", content)

    # --- CLAUDE.md ---
    claude_md = bundle.get("claude_md")
    if claude_md is not None:
        _write_file("CLAUDE.md", claude_md)

    # --- Brand context ---
    for filename, content in bundle.get("brand_context", {}).items():
        _write_file(f"brand_context/{filename}", content)

    # --- Personality ---
    for filename, content in bundle.get("personality", {}).items():
        _write_file(f".claude/{filename}", content)

    # --- Knowledge ---
    for entry in bundle.get("knowledge", []):
        filename = entry.get("filename", "")
        content = entry.get("content", "")
        if filename:
            _write_file(f".claude/knowledge/{filename}", content)

    return ImportResult(
        client_name=client_name,
        target_path=target_path,
        files_written=files_written,
        files_skipped=files_skipped,
        files_merged=files_merged,
        warnings=warnings,
    )
