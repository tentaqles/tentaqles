"""Session memory service — daily session logs with read-up chain."""

from __future__ import annotations

import re
from datetime import date, timedelta
from pathlib import Path

from sidecar.models import DailyMemory, SessionBlock
from sidecar.parsers import read_file_content


def _context_memory_dir(workspace_path: str) -> Path:
    return Path(workspace_path) / "context" / "memory"


def _global_memory_dir() -> Path:
    return Path.home() / ".tentaqles" / "context" / "memory"


def _find_client_path(workspace_path: str) -> str | None:
    current = Path(workspace_path)
    for parent in [current] + list(current.parents):
        if (parent / ".workspace-profile.json").exists():
            return str(parent)
        if (parent / ".tentaqles.json").exists():
            return str(parent)
    return None


def _parse_session_block(block_text: str, session_num: int) -> SessionBlock:
    """Parse a single ## Session N block into a SessionBlock."""
    goal = ""
    deliverables: list[str] = []
    decisions: list[str] = []
    open_threads: list[str] = []
    project = None

    current_section = None
    for line in block_text.split("\n"):
        stripped = line.strip()
        if stripped.startswith("### Goal"):
            current_section = "goal"
        elif stripped.startswith("### Project"):
            current_section = "project"
        elif stripped.startswith("### Deliverables"):
            current_section = "deliverables"
        elif stripped.startswith("### Decisions"):
            current_section = "decisions"
        elif stripped.startswith("### Open threads"):
            current_section = "open_threads"
        elif stripped.startswith("### "):
            current_section = None
        elif stripped and current_section:
            if current_section == "goal":
                goal = stripped
            elif current_section == "project":
                project = stripped
            elif current_section == "deliverables" and stripped.startswith("- "):
                deliverables.append(stripped[2:])
            elif current_section == "decisions" and stripped.startswith("- "):
                decisions.append(stripped[2:])
            elif current_section == "open_threads" and stripped.startswith("- "):
                open_threads.append(stripped[2:])

    return SessionBlock(
        session_number=session_num,
        goal=goal,
        deliverables=deliverables,
        decisions=decisions,
        open_threads=open_threads,
        project=project,
        raw_content=block_text,
    )


def _parse_memory_file(file_path: Path, level: str) -> DailyMemory | None:
    content = read_file_content(str(file_path))
    if content is None:
        return None

    # Split into session blocks by ## Session N
    blocks = re.split(r"(?=^## Session \d+)", content, flags=re.MULTILINE)
    sessions: list[SessionBlock] = []
    for block in blocks:
        match = re.match(r"^## Session (\d+)", block)
        if match:
            num = int(match.group(1))
            sessions.append(_parse_session_block(block, num))

    return DailyMemory(
        date=file_path.stem,
        level=level,
        file_path=str(file_path),
        sessions=sessions,
        raw_content=content,
    )


def get_session_memory(workspace_path: str, client_path: str | None = None) -> dict:
    """Read-up chain: project (today+yesterday) -> client (today) -> global (today)."""
    today = date.today().isoformat()
    yesterday = (date.today() - timedelta(days=1)).isoformat()

    result: dict[str, list] = {"project": [], "client": [], "global": []}

    # Project level: today + yesterday
    proj_dir = _context_memory_dir(workspace_path)
    for d in [today, yesterday]:
        f = proj_dir / f"{d}.md"
        parsed = _parse_memory_file(f, "project")
        if parsed:
            result["project"].append(parsed.model_dump())

    # Client level: today only
    cp = client_path or _find_client_path(workspace_path)
    if cp and cp != workspace_path:
        f = _context_memory_dir(cp) / f"{today}.md"
        parsed = _parse_memory_file(f, "client")
        if parsed:
            result["client"].append(parsed.model_dump())

    # Global: today only
    f = _global_memory_dir() / f"{today}.md"
    parsed = _parse_memory_file(f, "global")
    if parsed:
        result["global"].append(parsed.model_dump())

    return result


def save_memory(workspace_path: str, section: str, content: str) -> None:
    """Write to current session block at the workspace level."""
    today = date.today().isoformat()
    mem_dir = _context_memory_dir(workspace_path)
    mem_dir.mkdir(parents=True, exist_ok=True)
    file_path = mem_dir / f"{today}.md"

    if file_path.exists():
        existing = file_path.read_text(encoding="utf-8")
        # Find last session block and update section
        blocks = re.split(r"(?=^## Session \d+)", existing, flags=re.MULTILINE)
        if blocks:
            last_block = blocks[-1]
            section_header = f"### {section.replace('_', ' ').title()}"
            if section_header in last_block:
                # Replace section content
                pattern = rf"(### {re.escape(section.replace('_', ' ').title())})\n.*?(?=###|\Z)"
                replacement = f"\\1\n{content}\n\n"
                blocks[-1] = re.sub(pattern, replacement, last_block, flags=re.DOTALL)
            else:
                blocks[-1] += f"\n{section_header}\n{content}\n"
            file_path.write_text("".join(blocks), encoding="utf-8")
    else:
        # Create new file with Session 1
        template = f"""## Session 1

### Goal
{content if section == "goal" else ""}

### Deliverables
{content if section == "deliverables" else ""}

### Decisions
{content if section == "decisions" else ""}

### Open threads
{content if section == "open_threads" else ""}
"""
        file_path.write_text(template, encoding="utf-8")


def list_memory_timeline(workspace_path: str, client_path: str | None = None, days: int = 14) -> list[dict]:
    """List daily memory summaries for timeline view."""
    results: list[dict] = []
    mem_dir = _context_memory_dir(workspace_path)
    if not mem_dir.is_dir():
        return results

    for f in sorted(mem_dir.iterdir(), reverse=True):
        if f.suffix == ".md":
            parsed = _parse_memory_file(f, "project")
            if parsed:
                results.append(
                    {
                        "date": parsed.date,
                        "session_count": len(parsed.sessions),
                        "has_open_threads": any(s.open_threads for s in parsed.sessions),
                        "goals": [s.goal for s in parsed.sessions if s.goal],
                    }
                )
        if len(results) >= days:
            break

    return results
