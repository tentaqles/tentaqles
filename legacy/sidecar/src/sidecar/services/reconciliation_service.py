"""Reconciliation service — detect drift between disk state and registered state."""

from __future__ import annotations

from pathlib import Path

from sidecar.services.skills_service import discover_skills


def reconcile_workspace(workspace_path: str, client_path: str | None = None) -> dict:
    """Check workspace health and detect drift.

    Returns a report of what's present, missing, or unexpected.
    """
    ws = Path(workspace_path)
    report = {
        "workspace": workspace_path,
        "checks": [],
        "warnings": [],
        "ok": True,
    }

    # Check essential directories
    essential_dirs = [
        (".claude/rules", "Rules directory"),
        (".claude/skills", "Skills directory"),
        ("context/memory", "Session memory directory"),
    ]
    for rel_path, label in essential_dirs:
        exists = (ws / rel_path).is_dir()
        report["checks"].append({"name": label, "path": rel_path, "exists": exists})
        if not exists:
            report["warnings"].append(f"Missing: {label} ({rel_path})")

    # Check essential files
    essential_files = [
        (".workspace-profile.json", "Workspace profile"),
        ("CLAUDE.md", "CLAUDE.md"),
        (".tentaqles.json", "Toggle state"),
        ("context/learnings.md", "Learnings journal"),
    ]
    for rel_path, label in essential_files:
        exists = (ws / rel_path).is_file()
        report["checks"].append({"name": label, "path": rel_path, "exists": exists})
        if not exists:
            report["warnings"].append(f"Missing: {label} ({rel_path})")

    # Check skills consistency
    skills = discover_skills(workspace_path, client_path)
    for skill in skills:
        skill_path = Path(skill.source_path)
        skill_md = skill_path / "SKILL.md"
        if not skill_md.exists():
            report["warnings"].append(f"Skill '{skill.name}' directory exists but missing SKILL.md")

    # Check CLAUDE.md staleness (should contain heartbeat)
    claude_md = ws / "CLAUDE.md"
    if claude_md.exists():
        content = claude_md.read_text(encoding="utf-8", errors="replace")
        if "get_personality" not in content:
            report["warnings"].append("CLAUDE.md may be outdated — missing heartbeat MCP tools. Regenerate it.")

    # Check .mcp.json exists
    mcp_json = ws / ".mcp.json"
    if not mcp_json.exists():
        report["warnings"].append("No .mcp.json — MCP server not configured for this workspace")

    # Check git identity
    gitconfigs = list(ws.glob(".gitconfig-*"))
    if not gitconfigs:
        report["warnings"].append("No .gitconfig-* file — git identity not configured")

    report["ok"] = len(report["warnings"]) == 0
    return report
