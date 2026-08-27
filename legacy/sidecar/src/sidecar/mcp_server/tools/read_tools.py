"""Read-only MCP tools for workspace context."""

from __future__ import annotations

import json
from pathlib import Path

from mcp.server.fastmcp import FastMCP

from sidecar.mcp_server.config import LEARNED_PATTERNS_PATH
from sidecar.mcp_server.discovery import (
    discover_sql_dialect_rules,
    discover_workspace_rules,
    list_all_workspaces,
    resolve_workspace,
)
from sidecar.mcp_server.utils import get_az_subscription_id, get_gh_account, get_git_email
from sidecar.models import IdentityVerification, VerificationResult
from sidecar.parsers import parse_development_rules, parse_learned_patterns, read_file_content


def register_read_tools(mcp: FastMCP, get_workspace_path: callable) -> None:
    """Register all read-only tools with the MCP server."""

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
    async def get_workspace_context() -> str:
        """Return the full client profile for the current workspace.

        Includes: client name, description, git identity, cloud provider,
        database config, tech stack, language, active MCP servers, special rules,
        and all workspace rules content.
        """
        ws_path = get_workspace_path()
        config = resolve_workspace(ws_path)
        return config.model_dump_json(indent=2)

    @mcp.tool()
    async def verify_identity() -> str:
        """Check current git email, Azure subscription, and GitHub account
        against expected values for this workspace.

        Runs actual CLI commands (git config, az account show, gh auth status)
        and compares results against the workspace's expected identity.
        Returns pass/fail for each check with expected vs actual values.
        """
        ws_path = get_workspace_path()
        config = resolve_workspace(ws_path)

        # Git email check
        actual_email = await get_git_email(cwd=ws_path)
        git_result = VerificationResult(
            check="git_email",
            passed=actual_email == config.git.email,
            expected=config.git.email,
            actual=actual_email,
            command="git config user.email",
        )

        # Azure subscription check (only for Azure companies)
        az_result = None
        if config.cloud.provider == "azure" and config.cloud.subscription_id:
            actual_sub = await get_az_subscription_id()
            az_result = VerificationResult(
                check="azure_subscription",
                passed=actual_sub == config.cloud.subscription_id,
                expected=config.cloud.subscription_id,
                actual=actual_sub or "not logged in or az CLI not installed",
                command="az account show --query id -o tsv",
            )

        # GitHub account check (only for GitHub companies)
        gh_result = None
        if config.git.platform == "github" and config.git.account:
            actual_account = await get_gh_account()
            gh_result = VerificationResult(
                check="github_account",
                passed=actual_account == config.git.account,
                expected=config.git.account,
                actual=actual_account or "not logged in or gh CLI not installed",
                command="gh auth status",
            )

        verification = IdentityVerification(
            git_email=git_result,
            azure_subscription=az_result,
            github_account=gh_result,
        )
        return verification.model_dump_json(indent=2, exclude_none=True)

    @mcp.tool()
    async def get_sql_dialect_rules() -> str:
        """Return the SQL dialect rules for the current workspace.

        Reads the .claude/rules/{dialect}.md file (snowflake.md, postgres.md,
        or databricks.md) and returns its full content.
        """
        ws_path = get_workspace_path()
        rules = discover_sql_dialect_rules(ws_path)
        if rules is None:
            config = resolve_workspace(ws_path)
            return f"No SQL dialect rules configured for {config.client_name}."
        return rules

    @mcp.tool()
    async def get_learned_patterns(category: str | None = None) -> str:
        """Return error patterns from the central learned-patterns registry.

        Args:
            category: Optional filter. One of: powershell, identity, sql-dialect,
                      package-manager, path-format, git, azure. If omitted, returns all.
        """
        patterns = parse_learned_patterns(LEARNED_PATTERNS_PATH)

        if category:
            patterns = [p for p in patterns if p.category == category]

        if not patterns:
            return f"No patterns found{f' for category: {category}' if category else ''}."

        return f"[{', '.join(p.model_dump_json() for p in patterns)}]"

    @mcp.tool()
    async def list_workspaces() -> str:
        """List all configured client workspaces with summary info.

        Returns a JSON array of workspace summaries including client name,
        git email, platform, cloud provider, database type, and special rules.
        """
        summaries = list_all_workspaces()
        return f"[{', '.join(s.model_dump_json() for s in summaries)}]"

    @mcp.tool()
    async def get_workspace_rules() -> str:
        """Return all .claude/rules/*.md content for the current workspace.

        Returns a JSON object mapping filename to file content.
        """
        import json

        ws_path = get_workspace_path()
        rules = discover_workspace_rules(ws_path)

        if not rules:
            return "No workspace rules found in .claude/rules/."

        return json.dumps(rules, indent=2)

    @mcp.tool()
    async def get_development_rules() -> str:
        """Return blocked/confirm/allowed CLI command policies for this workspace.

        Parses the '## Development Rules' section from CLAUDE.md and returns
        structured lists of blocked, confirm-first, and allowed commands.
        """
        ws_path = get_workspace_path()
        claude_md = read_file_content(f"{ws_path}/CLAUDE.md")

        if not claude_md:
            return "No CLAUDE.md found for this workspace."

        dev_rules = parse_development_rules(claude_md)
        if dev_rules is None:
            return "No Development Rules section found in CLAUDE.md."

        return dev_rules.model_dump_json(indent=2)

    @mcp.tool()
    async def get_active_skills() -> str:
        """Return all enabled skills for the current workspace with hierarchy resolution.

        Skills are resolved across three levels: global (~/.tentaqles/skills/),
        client (.claude/skills/ at client level), and project (.claude/skills/
        at project level). Most specific level wins for same-named skills.
        Toggle state from .tentaqles.json is applied.

        Call this during session initialization (heartbeat) to know what
        skills are available.
        """
        from sidecar.services.skills_service import discover_skills

        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        skills = discover_skills(ws_path, client_path)
        enabled_skills = [s for s in skills if s.enabled]
        return json.dumps(
            [
                {
                    "name": s.name,
                    "description": s.description,
                    "category": s.category,
                    "level": s.level,
                }
                for s in enabled_skills
            ],
            indent=2,
        )

    @mcp.tool()
    async def get_skill_context(skill_name: str) -> str:
        """Return the full SKILL.md content and context matrix for a specific skill.

        Call this when you need to invoke a skill — it returns the methodology,
        context needs, dependencies, and reference files.

        Args:
            skill_name: The folder name of the skill (e.g., 'mkt-brand-voice')
        """
        from sidecar.services.skills_service import get_skill_context as _get_context

        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        result = _get_context(ws_path, skill_name, client_path)
        if result is None:
            return json.dumps({"error": f"Skill not found: {skill_name}"})
        return json.dumps(result, indent=2)

    @mcp.tool()
    async def discover_knowledge(query: str = "", tags: str | None = None) -> str:
        """Search the knowledge graph across all levels.

        Args:
            query: Text search against title, body, and tags.
            tags: Comma-separated tag filter (e.g., "python,api-design").
        """
        from sidecar.services.knowledge_service import discover_knowledge as _discover

        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        tag_list = [t.strip() for t in tags.split(",") if t.strip()] if tags else None
        results = _discover(query=query, tags=tag_list, workspace_path=ws_path, client_path=client_path)
        return json.dumps([e.model_dump() for e in results[:20]], indent=2)

    @mcp.tool()
    async def pull_knowledge(id: str) -> str:
        """Get full content of a knowledge entry by ID.

        Args:
            id: The knowledge entry ID (from discover_knowledge results).
        """
        from sidecar.services.knowledge_service import pull_knowledge as _pull

        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        entry = _pull(id, workspace_path=ws_path, client_path=client_path)
        if entry is None:
            return json.dumps({"error": f"Knowledge entry not found: {id}"})
        return entry.model_dump_json(indent=2)

    @mcp.tool()
    async def get_personality() -> str:
        """Get the effective personality (soul.md + user.md) for the current workspace.

        Returns soul.md (agent behavior) and user.md (user preferences) from the
        most specific level that defines them. Hierarchy: project > client > global.
        """
        from sidecar.services.personality_service import get_personality as _get

        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        result = _get(workspace_path=ws_path, client_path=client_path)
        return result.model_dump_json(indent=2)

    @mcp.tool()
    async def get_brand_context() -> str:
        """Get the brand context files for the current workspace.

        Returns voice-profile.md, positioning.md, icp.md, samples.md, and assets.md
        resolved through the hierarchy (project > client > global).
        Skills use these to personalize output with brand voice and positioning.
        """
        from sidecar.services.brand_context_service import get_brand_context as _get

        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        result = _get(workspace_path=ws_path, client_path=client_path)
        return result.model_dump_json(indent=2)

    @mcp.tool()
    async def get_session_memory() -> str:
        """Return assembled session memory from read-up chain (project -> client -> global).

        Reads today + yesterday at project level, today at client/global.
        Call at session start to restore context and see open threads.
        """
        from sidecar.services.session_memory_service import get_session_memory as _get

        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        result = _get(ws_path, client_path)
        return json.dumps(result, indent=2)

    @mcp.tool()
    async def get_learnings(skill_name: str | None = None) -> str:
        """Return learnings from read-up chain (project -> client -> global).

        Args:
            skill_name: Filter to a specific skill section. If omitted, returns all.
        """
        from sidecar.services.learnings_service import get_learnings as _get

        ws_path = get_workspace_path()
        client_path = _find_client_path(ws_path)
        return json.dumps(_get(ws_path, client_path, skill_name), indent=2)

    @mcp.tool()
    async def get_service_registry() -> str:
        """Get the external service registry — API keys, what they enable, and fallbacks.

        Use this before calling any external API to check if the key is configured
        and what the fallback is if it's missing.
        """
        from sidecar.services.service_registry import get_service_registry as _get

        ws_path = get_workspace_path()
        result = _get(ws_path)
        return json.dumps(result, indent=2)
