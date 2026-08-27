"""FastMCP server creation and tool registration."""

from __future__ import annotations

from mcp.server.fastmcp import FastMCP

from sidecar.mcp_server.tools.activation_tools import register_activation_tools
from sidecar.mcp_server.tools.board_tools import register_board_tools
from sidecar.mcp_server.tools.read_tools import register_read_tools
from sidecar.mcp_server.tools.write_tools import register_write_tools

# Module-level variable set by __main__.py before server starts
_workspace_path: str | None = None


def get_workspace_path() -> str:
    """Get the configured workspace path (set via --workspace CLI arg)."""
    if _workspace_path is None:
        raise RuntimeError("Workspace path not set. Use --workspace argument.")
    return _workspace_path


def create_server() -> FastMCP:
    """Create and configure the FastMCP server instance."""
    mcp = FastMCP(
        "workspace-context",
        instructions=INSTRUCTIONS,
    )

    register_read_tools(mcp, get_workspace_path)
    register_write_tools(mcp, get_workspace_path)
    register_activation_tools(mcp, get_workspace_path)
    register_board_tools(mcp, get_workspace_path)

    return mcp


INSTRUCTIONS = """\
Workspace-aware MCP server for multi-client environments.

## Read Tools
- get_workspace_context: Full client profile (identity, cloud, database, tech stack, rules)
- verify_identity: Check git/Azure/GitHub identity against expected values
- get_sql_dialect_rules: SQL dialect rules for this workspace
- get_learned_patterns: Error patterns registry (filterable by category)
- list_workspaces: All configured client workspaces
- get_workspace_rules: All .claude/rules/*.md content
- get_development_rules: CLI command safety policies (blocked/confirm/allowed)
- get_active_skills: All enabled skills with hierarchy resolution (global > client > project)
- get_skill_context: Full SKILL.md methodology, context needs, dependencies, and reference files
- get_service_registry: External API keys, what they enable, and fallback behaviour

## Write Tools
- add_learned_pattern: Append new error pattern to central registry
- propagate_config: Trigger config propagation scripts
- update_workspace_rule: Update a specific .claude/rules/*.md file

## Usage
1. Call get_workspace_context at session start to understand the current client
2. Call verify_identity before any git/cloud operations
3. Call get_sql_dialect_rules before writing SQL
4. Consult get_learned_patterns when encountering errors
8. get_active_skills() — List all enabled skills for the current workspace with hierarchy resolution
   (global > client > project). Call during session startup.
9. get_skill_context(skill_name) — Get the full SKILL.md methodology, context needs, dependencies,
   and reference files for a specific skill. Call when invoking a skill.
10. discover_knowledge(query, tags?) — Search the knowledge graph across all levels. Returns matching entries.
11. pull_knowledge(id) — Get full content of a knowledge entry by ID.
12. get_personality() — Get effective soul.md + user.md for current workspace with hierarchy resolution.
13. get_brand_context() — Get brand context files (voice-profile.md, positioning.md, icp.md, samples.md, assets.md) resolved through the hierarchy. Skills use these to personalize output.
14. contribute_knowledge(title, content, tags, type) — Add a new knowledge entry to the project knowledge graph.
15. get_session_memory() — Read assembled daily session logs from read-up chain. Call at session start.
16. get_learnings(skill_name?) — Read learnings journal from read-up chain. Filter by skill.
17. save_session_memory(section, content) — Update goal/deliverables/decisions/open_threads in today's session.
18. append_learning(skill_name, entry) — Add dated entry to learnings journal.
19. session_wrap_up(knowledge_title?, knowledge_content?, knowledge_tags?, learning_skill?, learning_entry?,
    open_threads?) — End-of-session wrap-up. Optionally contribute knowledge, append learnings, and save
    open threads in one call.
20. get_service_registry() — Check which external APIs are available, what keys they need, and what
    the fallback is if a key is missing. Call before invoking any external API.

## Board Tools (Vibe Kanban)
- board_list() — List all kanban boards for the current workspace
- board_get(board_name) — Full board view with columns and cards
- board_create(board_name) — Create new board with default columns
- board_add_card(board_name, title, description?, column?, priority?) — Add card to a board
- board_move_card(board_name, card_title, target_column) — Move card between columns
- board_complete_card(board_name, card_title) — Move card to Done column
- board_delegate_card(board_name, card_title, model?) — Delegate card to Claude Code
  (assigns to claude, moves to In Progress, fires autonomous execution with --dangerously-skip-permissions)
"""
