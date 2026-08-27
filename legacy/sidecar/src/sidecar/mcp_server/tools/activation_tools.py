"""Activation and health MCP tools for workspace lifecycle management."""

from __future__ import annotations

from mcp.server.fastmcp import FastMCP

from sidecar.services.activation import (
    activate_workspace,
    capture_claude_profile,
    deactivate_workspace,
    get_active_workspace,
    toggle_workspace,
)
from sidecar.services.health import check_workspace_health, detect_drift


def register_activation_tools(mcp: FastMCP, get_workspace_path: callable) -> None:
    """Register activation and health MCP tools."""

    @mcp.tool()
    async def activate_workspace_tool(workspace_path: str) -> str:
        """Activate a workspace: switch Claude Code global settings to match the client's .claude-profile.json.

        Creates backup before switching, auto-syncs drift from previous workspace.
        """
        try:
            result = activate_workspace(workspace_path)
            parts = [f"Activated workspace: {result.client_name}"]
            if result.auto_synced:
                parts.append(f"Auto-synced drift from {result.auto_sync_client}")
            if result.warnings:
                parts.append(f"Warnings: {', '.join(result.warnings)}")
            return "\n".join(parts)
        except Exception as e:
            return f"Error activating workspace: {e}"

    @mcp.tool()
    async def deactivate_workspace_tool() -> str:
        """Deactivate the current workspace. Auto-syncs any config drift back to the profile before clearing."""
        try:
            result = deactivate_workspace()
            msg = f"Deactivated workspace: {result.previous_workspace or 'none'}"
            if result.auto_synced:
                msg += " (drift auto-synced)"
            return msg
        except Exception as e:
            return f"Error deactivating workspace: {e}"

    @mcp.tool()
    async def toggle_workspace_tool() -> str:
        """Quick-switch to the previously active workspace (like 'cd -' for workspaces)."""
        try:
            result = toggle_workspace()
            return f"Toggled to workspace: {result.client_name} ({result.workspace_path})"
        except Exception as e:
            return f"Error toggling workspace: {e}"

    @mcp.tool()
    async def capture_claude_profile_tool(client_path: str | None = None) -> str:
        """Snapshot the current global Claude Code config into a client's .claude-profile.json."""
        try:
            path = client_path
            if path is None:
                active = get_active_workspace()
                if active is None:
                    return "Error: No active workspace and no client_path provided"
                path = active.workspace_path
            profile = capture_claude_profile(path)
            model = profile.settings.get("model", "default")
            mcp_count = len(profile.mcp_servers)
            return f"Captured Claude profile to {path}/.claude-profile.json (model: {model}, {mcp_count} MCP servers)"
        except Exception as e:
            return f"Error capturing profile: {e}"

    @mcp.tool()
    async def get_active_workspace_tool() -> str:
        """Get the currently active workspace (path, client name, activation time)."""
        active = get_active_workspace()
        if active is None:
            return "No workspace is currently active."
        return f"Active workspace: {active.client_name} ({active.workspace_path}), activated at {active.activated_at}"

    @mcp.tool()
    async def check_workspace_health_tool(workspace_path: str | None = None) -> str:
        """Run health checks on a workspace. Validates profile, rules, skills, CLAUDE.md, and more."""
        try:
            path = workspace_path
            if path is None:
                active = get_active_workspace()
                if active is None:
                    path = get_workspace_path()
                else:
                    path = active.workspace_path
            report = check_workspace_health(path)
            lines = [f"Health: {report.overall.upper()} ({report.client_name})"]
            for check in report.checks:
                icon = {"pass": "OK", "warn": "!!", "fail": "XX"}[check.status]
                lines.append(f"  [{icon}] {check.name}: {check.message}")
            return "\n".join(lines)
        except Exception as e:
            return f"Error checking health: {e}"

    @mcp.tool()
    async def detect_drift_tool() -> str:
        """Check if live global Claude Code config has drifted from the active workspace's stored profile."""
        try:
            active = get_active_workspace()
            if active is None:
                return "No active workspace — cannot detect drift."
            report = detect_drift(active.workspace_path)
            if not report.has_drift:
                return "No drift detected — global config matches stored profile."
            parts = ["Drift detected:"]
            if report.settings_changed:
                parts.append("  - settings.json changed")
            if report.claude_md_changed:
                parts.append("  - CLAUDE.md changed")
            if report.mcp_servers_changed:
                parts.append("  - MCP servers changed")
            return "\n".join(parts)
        except Exception as e:
            return f"Error detecting drift: {e}"
