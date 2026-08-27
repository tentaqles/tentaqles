"""Write MCP tools for workspace configuration management."""

from __future__ import annotations

from datetime import date
from pathlib import Path

from mcp.server.fastmcp import FastMCP

from sidecar.mcp_server.config import (
    LEARNED_PATTERNS_PATH,
    PROPAGATE_ALL_SCRIPT,
    REPOS_ROOT,
)
from sidecar.mcp_server.discovery import _get_known_clients
from sidecar.mcp_server.utils import run_powershell_script
from sidecar.parsers import parse_learned_patterns, read_file_content


def register_write_tools(mcp: FastMCP, get_workspace_path: callable) -> None:
    """Register all write tools with the MCP server."""

    def _find_client_path(workspace_path: str) -> str | None:
        """Walk up from workspace to find a client directory (has .workspace-profile.json)."""
        current = Path(workspace_path)
        for parent in [current] + list(current.parents):
            if (parent / ".workspace-profile.json").exists():
                return str(parent)
            if (parent / ".tentaqles.json").exists():
                return str(parent)
        return None

    @mcp.tool()
    async def add_learned_pattern(
        title: str,
        category: str,
        description: str,
        wrong_example: str,
        right_example: str,
        prevention: str | None = None,
    ) -> str:
        """Append a new error pattern to the central learned-patterns registry.

        Args:
            title: Descriptive title including the error type.
            category: One of: powershell, identity, sql-dialect, package-manager,
                      path-format, git, azure.
            description: What went wrong and why.
            wrong_example: Code that triggers the error (will be in a code block).
            right_example: Correct approach (will be in a code block).
            prevention: How to avoid this in the future (optional).
        """
        valid_categories = [
            "powershell",
            "identity",
            "sql-dialect",
            "package-manager",
            "path-format",
            "git",
            "azure",
        ]
        if category not in valid_categories:
            return f"Invalid category '{category}'. Must be one of: {', '.join(valid_categories)}"

        # Parse existing patterns to get the next number
        existing = parse_learned_patterns(LEARNED_PATTERNS_PATH)
        next_number = max((p.number for p in existing), default=0) + 1

        today = date.today().isoformat()

        # Format the new entry
        entry = f"""
## Pattern {next_number}: {title}

**Category**: {category}

**Date Discovered**: {today}

**Description**: {description}

**Wrong Example**:
```
{wrong_example}
```

**Right Example**:
```
{right_example}
```
"""
        if prevention:
            entry += f"""**Prevention**: {prevention}

"""
        entry += """**Applied To**:
- This registry entry.

---
"""

        # Read the file and insert before "## Notes for Future Entries"
        content = read_file_content(LEARNED_PATTERNS_PATH)
        if content is None:
            return f"Error: Cannot read {LEARNED_PATTERNS_PATH}"

        marker = "## Notes for Future Entries"
        if marker in content:
            parts = content.split(marker, 1)
            new_content = parts[0].rstrip() + "\n\n---\n" + entry + "\n" + marker + parts[1]
        else:
            new_content = content.rstrip() + "\n\n---\n" + entry

        Path(LEARNED_PATTERNS_PATH).write_text(new_content, encoding="utf-8")

        return f"Pattern #{next_number} '{title}' added to learned-patterns.md."

    @mcp.tool()
    async def propagate_config(client: str | None = None) -> str:
        """Trigger configuration propagation scripts.

        Propagates .claude/, CLAUDE.md, and other config files
        from the client root to all sub-repos within that client.

        Args:
            client: Client name to propagate for (e.g., 'booster').
                    If omitted, runs propagate-all-clients.ps1 for all clients.
        """
        if client is None:
            # Propagate all clients
            stdout, stderr, rc = await run_powershell_script(PROPAGATE_ALL_SCRIPT)
            if rc != 0:
                return f"Propagation failed (exit {rc}):\n{stderr}"
            return f"All clients propagated.\n{stdout}"

        known = _get_known_clients()
        if client not in known:
            return f"Unknown client '{client}'. Known: {', '.join(known)}"

        # Find the client's propagation script
        client_path = Path(REPOS_ROOT) / client
        script_candidates = [
            client_path / ".scripts" / "propagate-ai-config.ps1",
            client_path / "propagate-ai-config.ps1",
        ]

        script_path = None
        for candidate in script_candidates:
            if candidate.exists():
                script_path = str(candidate)
                break

        if script_path is None:
            return f"No propagation script found for {client}."

        stdout, stderr, rc = await run_powershell_script(script_path)
        if rc != 0:
            return f"Propagation failed for {client} (exit {rc}):\n{stderr}"
        return f"Config propagated for {client}.\n{stdout}"

    @mcp.tool()
    async def update_workspace_rule(
        rule_file: str,
        new_content: str,
    ) -> str:
        """Update a specific .claude/rules/*.md file in the current workspace.

        Args:
            rule_file: Filename within .claude/rules/ (e.g., 'identity.md', 'snowflake.md').
                       Must be a .md file with no path separators.
            new_content: The new content for the rules file.
        """
        ws_path = get_workspace_path()

        # Validate filename — no path traversal
        if "/" in rule_file or "\\" in rule_file or ".." in rule_file:
            return "Error: rule_file must be a simple filename (no path separators or '..')."

        if not rule_file.endswith(".md"):
            return "Error: rule_file must end with .md."

        # Validate the file exists (update only, not create)
        rules_dir = Path(ws_path) / ".claude" / "rules"
        target = rules_dir / rule_file

        if not target.exists():
            existing = [f.name for f in rules_dir.glob("*.md")] if rules_dir.is_dir() else []
            return f"Error: '{rule_file}' not found. Existing files: {', '.join(existing) or 'none'}"

        target.write_text(new_content, encoding="utf-8")
        return f"Updated {rule_file} in {ws_path}/.claude/rules/."

    @mcp.tool()
    async def contribute_knowledge(title: str, content: str, tags: str, type: str = "learning") -> str:
        """Add a new knowledge entry to the project-level knowledge graph.

        Args:
            title: Descriptive title for the knowledge entry.
            content: Markdown body content.
            tags: Comma-separated tags (e.g., "python,api-design").
            type: One of: learning, decision, pattern, solution.
        """
        from sidecar.services.knowledge_service import contribute_knowledge as _contribute

        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        tag_list = [t.strip() for t in tags.split(",") if t.strip()]
        entry = _contribute(
            title=title,
            content=content,
            tags=tag_list,
            type=type,
            workspace_path=ws_path,
            client_path=client_path,
        )
        return entry.model_dump_json(indent=2)

    @mcp.tool()
    async def save_session_memory(section: str, content: str) -> str:
        """Update a section in the current session's memory block.

        Args:
            section: One of: goal, deliverables, decisions, open_threads
            content: The content to write.
        """
        from sidecar.services.session_memory_service import save_memory

        ws_path = get_workspace_path()
        valid = ["goal", "deliverables", "decisions", "open_threads"]
        if section not in valid:
            return f"Invalid section. Must be one of: {', '.join(valid)}"
        save_memory(ws_path, section, content)
        return f"Updated '{section}' in today's session memory."

    @mcp.tool()
    async def append_learning(skill_name: str, entry: str) -> str:
        """Add a dated entry to the learnings journal.

        Args:
            skill_name: The skill section (e.g., 'mkt-brand-voice').
                       Use 'general/what-works' or 'general/what-doesnt' for cross-skill.
            entry: The learning text. Date is added automatically.
        """
        from sidecar.services.learnings_service import append_learning as _append

        ws_path = get_workspace_path()
        _append(ws_path, skill_name, entry)
        return f"Learning appended to '{skill_name}' section."

    @mcp.tool()
    async def session_wrap_up(
        knowledge_title: str | None = None,
        knowledge_content: str | None = None,
        knowledge_tags: str | None = None,
        learning_skill: str | None = None,
        learning_entry: str | None = None,
        open_threads: str | None = None,
    ) -> str:
        """Wrap up the current session by optionally contributing knowledge,
        appending learnings, and saving open threads.

        Call this when the session is ending. All parameters are optional —
        include only what's relevant from this session.

        Args:
            knowledge_title: Title for a new knowledge entry (if something was learned).
            knowledge_content: Body content for the knowledge entry.
            knowledge_tags: Comma-separated tags for the knowledge entry.
            learning_skill: Skill name for the learning (e.g., 'mkt-brand-voice' or 'general/what-works').
            learning_entry: The learning text to append.
            open_threads: Open threads to save in session memory for next session.
        """
        results = []
        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)

        # Contribute knowledge if provided
        if knowledge_title and knowledge_content:
            from sidecar.services.knowledge_service import contribute_knowledge as _contribute

            tag_list = [t.strip() for t in knowledge_tags.split(",") if t.strip()] if knowledge_tags else []
            _contribute(
                title=knowledge_title,
                content=knowledge_content,
                tags=tag_list,
                workspace_path=ws_path,
                client_path=client_path,
            )
            results.append(f"Knowledge contributed: {knowledge_title}")

        # Append learning if provided
        if learning_skill and learning_entry:
            from sidecar.services.learnings_service import append_learning

            append_learning(ws_path, learning_skill, learning_entry)
            results.append(f"Learning appended to {learning_skill}")

        # Save open threads if provided
        if open_threads:
            from sidecar.services.session_memory_service import save_memory

            save_memory(ws_path, "open_threads", open_threads)
            results.append("Open threads saved")

        if not results:
            return "No wrap-up actions taken (all parameters were empty)."
        return "Session wrap-up complete: " + "; ".join(results)
