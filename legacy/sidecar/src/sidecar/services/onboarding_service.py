"""Onboarding service — first-run setup wizard backend."""

from __future__ import annotations

import json
from pathlib import Path

from sidecar.parsers import read_file_content
from sidecar.services.personality_service import save_personality
from sidecar.services.skills_service import install_pack


def scan_base_path(base_path: str) -> dict:
    """Scan a base path and report what was found.

    Returns:
        {
            "valid": bool,
            "companies": [{"name": str, "path": str, "has_profile": bool, "project_count": int}],
            "total_projects": int,
            "suggestions": [str]
        }
    """
    path = Path(base_path)
    if not path.is_dir():
        return {"valid": False, "companies": [], "total_projects": 0, "suggestions": ["Directory does not exist"]}

    clients = []
    total_projects = 0

    for child in sorted(path.iterdir()):
        if not child.is_dir() or child.name.startswith("."):
            continue

        has_profile = (child / ".workspace-profile.json").exists()
        has_gitconfig = any(child.glob(".gitconfig-*"))

        if has_profile or has_gitconfig:
            # Count project subdirectories
            projects = [
                p
                for p in child.iterdir()
                if p.is_dir()
                and not p.name.startswith(".")
                and p.name not in {"node_modules", "__pycache__", ".git", "venv", ".venv"}
            ]
            clients.append(
                {
                    "name": child.name,
                    "path": str(child),
                    "has_profile": has_profile,
                    "has_gitconfig": has_gitconfig,
                    "project_count": len(projects),
                }
            )
            total_projects += len(projects)

    suggestions = []
    if not clients:
        suggestions.append("No client workspaces found. You can create one using the New Client wizard.")
        # Check if there are directories that look like they could be clients
        potential = [c for c in path.iterdir() if c.is_dir() and not c.name.startswith(".") and (c / ".git").exists()]
        if potential:
            suggestions.append(
                f"Found {len(potential)} directories with git repos. "
                "These might be client directories that need workspace profiles."
            )

    return {
        "valid": True,
        "clients": clients,
        "total_projects": total_projects,
        "suggestions": suggestions,
    }


def generate_mcp_snippet(tentaqles_path: str, client_path: str) -> dict:
    """Generate the .mcp.json snippet for connecting a client workspace.

    Returns the JSON object that should go in .mcp.json.
    """
    return {
        "mcpServers": {
            "workspace-context": {
                "command": "uv",
                "args": [
                    "run",
                    "--project",
                    str(Path(tentaqles_path) / "sidecar"),
                    "workspace-mcp",
                    "--workspace",
                    client_path,
                ],
            }
        }
    }


def setup_mcp_for_client(client_path: str, tentaqles_path: str) -> str:
    """Write .mcp.json to a client directory with the workspace-context server.

    Merges with existing .mcp.json if present.
    """
    mcp_path = Path(client_path) / ".mcp.json"
    existing = {}

    content = read_file_content(str(mcp_path))
    if content:
        try:
            existing = json.loads(content)
        except json.JSONDecodeError:
            pass

    snippet = generate_mcp_snippet(tentaqles_path, client_path)
    servers = existing.get("mcpServers", {})
    servers["workspace-context"] = snippet["mcpServers"]["workspace-context"]
    existing["mcpServers"] = servers

    mcp_path.write_text(json.dumps(existing, indent=2) + "\n", encoding="utf-8")
    return str(mcp_path)


def setup_personality(
    level: str,
    soul_content: str,
    user_content: str,
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> dict:
    """Set up initial personality files."""
    results = {}
    if soul_content.strip():
        save_personality("soul", level, soul_content, workspace_path, client_path)
        results["soul"] = f"Saved soul.md at {level} level"
    if user_content.strip():
        save_personality("user", level, user_content, workspace_path, client_path)
        results["user"] = f"Saved user.md at {level} level"
    return results


def install_skill_packs(
    pack_names: list[str],
    level: str = "global",
    workspace_path: str | None = None,
    client_path: str | None = None,
    packs_dir: str | None = None,
) -> list[str]:
    """Install selected skill packs."""
    all_results: list[str] = []
    for pack_name in pack_names:
        results = install_pack(pack_name, level, workspace_path, client_path, packs_dir)
        all_results.extend(results)
    return all_results


def get_tentaqles_path() -> str:
    """Get the path to the Tentaqles installation (for MCP snippet generation)."""
    # Walk up from this file to find the project root
    current = Path(__file__).resolve()
    # services/ -> sidecar/ -> src/ -> sidecar/ -> (root)
    for parent in current.parents:
        if (parent / "package.json").exists() and (parent / "sidecar").is_dir():
            return str(parent)
    return str(current.parent.parent.parent.parent)


def get_packs_dir() -> str:
    """Get the packs directory — works in both dev and PyInstaller bundle."""
    import sys

    # PyInstaller bundle: packs are extracted to _MEIPASS/packs/
    if hasattr(sys, "_MEIPASS"):
        bundled = Path(sys._MEIPASS) / "packs"
        if bundled.is_dir():
            return str(bundled)

    # Development: packs/ is at the project root
    tentaqles_path = get_tentaqles_path()
    dev_packs = Path(tentaqles_path) / "packs"
    if dev_packs.is_dir():
        return str(dev_packs)

    # Fallback: next to executable
    exe_dir = Path(sys.executable).parent
    near_exe = exe_dir / "packs"
    if near_exe.is_dir():
        return str(near_exe)

    return str(dev_packs)  # Return dev path even if it doesn't exist


def get_soul_templates() -> list[dict]:
    """Return pre-built soul.md personality templates."""
    return [
        {
            "id": "engineer",
            "name": "Engineering Partner",
            "description": (
                "Direct, technical, code-first. Verifies before suggesting. Prefers reading code over guessing."
            ),
            "content": """# Soul — Engineering Partner

## Core Behavior
- Be direct and technical. No fluff, no pleasantries.
- Always verify assumptions against the codebase before suggesting changes.
- Prefer reading code over guessing at behavior.
- When uncertain, say so clearly. Don't hedge with "I think".
- Show code, don't describe it. Implementations over explanations.

## Working Style
- Follow existing patterns in the codebase. Don't reinvent.
- Make the smallest change that solves the problem.
- Flag security concerns immediately.
- Suggest tests for any non-trivial change.

## Boundaries
- Never commit without explicit approval.
- Don't refactor code outside the current task scope.
- Don't add features that weren't requested.
""",
        },
        {
            "id": "mentor",
            "name": "Patient Mentor",
            "description": (
                "Explains reasoning, teaches patterns, suggests learning resources. Great for learning new codebases."
            ),
            "content": """# Soul — Patient Mentor

## Core Behavior
- Explain the reasoning behind suggestions, not just the solution.
- When introducing a new concept, provide context and analogies.
- Offer 2-3 approaches with trade-offs when applicable.
- Link to relevant documentation when helpful.
- Be encouraging but honest about mistakes.

## Working Style
- Start with the simplest approach, explain when complexity is needed.
- Point out patterns and principles as you work.
- Ask clarifying questions before making assumptions.
- After completing a task, summarize what was done and why.

## Boundaries
- Never make the user feel bad for not knowing something.
- Don't over-explain things the user clearly already knows.
- Balance teaching with actually getting work done.
""",
        },
        {
            "id": "strategist",
            "name": "Creative Strategist",
            "description": "Opinionated, resourceful, anticipates needs. Thinks like an owner, not an employee.",
            "content": """# Soul — Creative Strategist

## Core Behavior
- Have opinions. Recommend with reasoning, not neutrally.
- Be resourceful — check context and files before asking questions.
- Anticipate needs. Flag gaps and opportunities proactively.
- Think like an owner, not an employee. Challenge bad ideas respectfully.
- Move fast. Don't over-plan when action will reveal the answer.

## Working Style
- Propose bold solutions, then refine based on feedback.
- Connect dots across domains. Marketing insight can improve code architecture.
- When stuck, change the approach rather than push harder.
- Prototype first, polish later.

## Boundaries
- Own mistakes openly. Say so and fix, don't hedge.
- Never silently fall back to generic output when specialized skills exist.
- Don't wait for permission on obvious improvements.
""",
        },
        {
            "id": "minimalist",
            "name": "Terse Operator",
            "description": (
                "Minimal words, maximum action. Does the task, reports results. No explanations unless asked."
            ),
            "content": """# Soul — Terse Operator

## Core Behavior
- Do the task. Report results. Minimal words.
- No preamble, no summaries, no trailing explanations.
- If the answer is code, show only code.
- Ask questions only when truly blocked.

## Working Style
- One commit per logical change.
- Fix it, don't discuss it.
- If something is broken, fix it along the way.

## Boundaries
- Don't explain unless asked.
- Don't suggest improvements unless they're blocking the task.
""",
        },
    ]


def get_user_options() -> dict:
    """Return structured options for building user.md via form."""
    return {
        "roles": [
            "Senior Software Engineer",
            "Full-Stack Developer",
            "Frontend Developer",
            "Backend Developer",
            "DevOps / Platform Engineer",
            "Data Scientist / ML Engineer",
            "Mobile Developer",
            "Engineering Manager",
            "Technical Lead",
            "Student / Learning Developer",
            "Solo Founder / Indie Hacker",
            "QA / Test Engineer",
        ],
        "preferences": [
            "Concise communication — bullet points over paragraphs",
            "Show code, don't describe it",
            "Prefer 2-3 options over one recommendation",
            "Explain reasoning behind suggestions",
            "Minimal output — just the essentials",
            "Include tests with every implementation",
            "Always suggest the simplest approach first",
            "Challenge my ideas when you disagree",
            "Follow existing code patterns strictly",
        ],
        "working_styles": [
            "Move fast, iterate later",
            "Plan thoroughly, execute once",
            "Pair-programming style — think out loud together",
            "Autonomous — take initiative, ask less",
            "Review-oriented — show me before committing",
            "TDD — tests first, implementation second",
            "Prototype first, refine later",
        ],
        "languages": [
            "Python",
            "TypeScript / JavaScript",
            "Rust",
            "Go",
            "Java",
            "C# / .NET",
            "C / C++",
            "Ruby",
            "Swift",
            "Kotlin",
            "PHP",
            "Elixir",
            "Scala",
            "Shell / Bash",
            "SQL",
        ],
    }


def build_user_md(
    role: str,
    preferences: list[str],
    working_styles: list[str],
    languages: list[str],
    name: str = "",
) -> str:
    """Build user.md from structured selections."""
    prefs = "\n".join(f"- {p}" for p in preferences) if preferences else "- (none selected)"
    styles = "\n".join(f"- {s}" for s in working_styles) if working_styles else "- (none selected)"
    langs = ", ".join(languages) if languages else "(none selected)"

    return f"""# User Profile

## About
- Name: {name}
- Role: {role}

## Preferences
{prefs}

## Working Style
{styles}

## Languages
{langs}
"""


def get_suggested_base_path() -> str:
    """Suggest a default base path for new users."""
    home = Path.home()
    tentaqles_dir = home / "Tentaqles"
    # Check common dev directories
    for candidate in [
        home / "Tentaqles",
        home / "repos",
        home / "projects",
        Path("D:/repos"),
        Path("C:/repos"),
    ]:
        if candidate.is_dir():
            return str(candidate)
    # Default: create ~/Tentaqles
    return str(tentaqles_dir)
