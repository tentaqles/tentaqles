"""CLAUDE.md auto-generation from workspace state.

Follows Claude Code best practices:
- CLAUDE.md only contains what Claude can't figure out by reading code
- Uses @path/to/import syntax for additional context
- Heavy content lives in .claude/skills/ for on-demand loading
- Personality lives in .claude/soul.md, referenced via @soul.md
"""

from __future__ import annotations

import json
from pathlib import Path

from sidecar.parsers import read_file_content


def generate_claude_md(workspace_path: str, client_path: str | None = None) -> str:
    """Generate a slim CLAUDE.md. Only include what Claude can't infer."""
    ws = Path(workspace_path)
    ws_name = ws.name
    lines: list[str] = []

    lines.append(f"# {ws_name}")
    lines.append("")

    # ── Identity — things Claude can't guess ──
    profile = _load_profile(ws)
    if profile:
        desc = profile.get("client_description", "")
        if desc:
            lines.append(desc)
            lines.append("")

        env_lines = []
        git = profile.get("git", {})
        if git.get("platform"):
            env_lines.append(f"- Git: {git['platform']} ({git.get('host', '')})")
        cloud = profile.get("cloud", {})
        if cloud.get("provider", "none") != "none":
            env_lines.append(f"- Cloud: {cloud['provider']}")
        db = profile.get("database", {})
        if db.get("type", "none") != "none":
            env_lines.append(f"- Database: {db['type']}")
        tech = profile.get("tech_stack", [])
        if tech:
            env_lines.append(f"- Stack: {', '.join(tech)}")

        if env_lines:
            lines.append("## Environment")
            lines.extend(env_lines)
            lines.append("")

    # ── Personality — reference, don't embed ──
    if (ws / ".claude" / "soul.md").exists():
        lines.append("## Personality")
        lines.append("See @.claude/soul.md for behavior guidelines and @.claude/user.md for user preferences.")
        lines.append("")

    # ── Session start — light heartbeat ──
    lines.append("## Session Start")
    lines.append("At session start, call these MCP tools:")
    lines.append("1. `get_personality()` — behavior + user preferences")
    lines.append("2. `get_session_memory()` — open threads from last session")
    lines.append("3. `verify_identity()` — confirm git/cloud identity")
    lines.append("Report open threads if any. Skip the rest — skills load on demand.")
    lines.append("")

    # ── Project brief — if exists ──
    if (ws / "brief.md").exists():
        lines.append("## Project")
        lines.append("See @brief.md for project goal, deliverables, and acceptance criteria.")
        lines.append("")

    # ── Brand context — if exists ──
    if (ws / "brand_context").is_dir() and any((ws / "brand_context").iterdir()):
        lines.append("## Brand Context")
        lines.append(
            "Brand files in `brand_context/`. "
            "Call `get_brand_context()` MCP tool when producing audience-facing content."
        )
        lines.append("")

    # ── Additional context imports ──
    imports = []
    if (ws / "context" / "learnings.md").exists():
        imports.append("- Learnings: @context/learnings.md")
    if (ws / "README.md").exists():
        imports.append("- Overview: @README.md")

    if imports:
        lines.append("## Additional Context")
        lines.extend(imports)
        lines.append("")

    return "\n".join(lines)


def _load_profile(ws: Path) -> dict | None:
    content = read_file_content(str(ws / ".workspace-profile.json"))
    if not content:
        return None
    try:
        return json.loads(content)
    except json.JSONDecodeError:
        return None


def generate_and_save(workspace_path: str, client_path: str | None = None) -> str:
    """Generate and write CLAUDE.md."""
    content = generate_claude_md(workspace_path, client_path)
    (Path(workspace_path) / "CLAUDE.md").write_text(content, encoding="utf-8")
    return content
