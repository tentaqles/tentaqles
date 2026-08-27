"""Tests for sidecar.services.health — drift detection and workspace health checks."""

from __future__ import annotations

import json
from pathlib import Path

from sidecar.services.health import (
    check_workspace_health,
    compute_config_hash,
    detect_drift,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _write_json(path: Path, data: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2), encoding="utf-8")


def _setup_minimal_workspace(tmp_path: Path) -> Path:
    """Create the minimum files for a workspace that produces a valid profile."""
    ws = tmp_path / "workspace"
    ws.mkdir()

    profile = {
        "$schema": "workspace-profile-v1",
        "client_name": "test-client",
        "client_description": "Test",
        "git": {"platform": "github", "host": "github.com"},
        "cloud": {"provider": "none"},
        "database": {"type": "none"},
    }
    _write_json(ws / ".workspace-profile.json", profile)
    return ws


# ---------------------------------------------------------------------------
# Drift detection tests
# ---------------------------------------------------------------------------


class TestComputeConfigHash:
    def test_compute_config_hash_deterministic(self):
        """Same inputs always produce the same hash."""
        settings = {"theme": "dark", "fontSize": 14}
        claude_md = "# My rules"
        mcp_servers = {"workspace-context": {"command": "uv"}}

        h1 = compute_config_hash(settings, claude_md, mcp_servers)
        h2 = compute_config_hash(settings, claude_md, mcp_servers)
        assert h1 == h2
        assert len(h1) == 32  # MD5 hex digest length

    def test_compute_config_hash_different_settings(self):
        """Different settings produce different hashes."""
        base = {"theme": "dark"}
        changed = {"theme": "light"}
        claude_md = "# rules"
        mcp: dict = {}

        h1 = compute_config_hash(base, claude_md, mcp)
        h2 = compute_config_hash(changed, claude_md, mcp)
        assert h1 != h2

    def test_compute_config_hash_different_claude_md(self):
        """Different claude_md values produce different hashes."""
        settings: dict = {}
        mcp: dict = {}

        h1 = compute_config_hash(settings, "# original", mcp)
        h2 = compute_config_hash(settings, "# changed", mcp)
        assert h1 != h2

    def test_compute_config_hash_different_mcp(self):
        """Different mcp_servers produce different hashes."""
        settings: dict = {}
        claude_md = None

        h1 = compute_config_hash(settings, claude_md, {"server-a": {}})
        h2 = compute_config_hash(settings, claude_md, {"server-b": {}})
        assert h1 != h2


class TestDetectDrift:
    def _write_global_state(
        self,
        tmp_path: Path,
        settings: dict,
        claude_md: str | None,
        mcp_servers: dict,
    ) -> None:
        """Write fake global Claude Code config under tmp_path."""
        claude_dir = tmp_path / ".claude"
        claude_dir.mkdir(parents=True, exist_ok=True)

        (claude_dir / "settings.json").write_text(json.dumps(settings), encoding="utf-8")

        if claude_md is not None:
            (claude_dir / "CLAUDE.md").write_text(claude_md, encoding="utf-8")

        claude_json_data = {"mcpServers": mcp_servers}
        (tmp_path / ".claude.json").write_text(json.dumps(claude_json_data), encoding="utf-8")

    def _write_client_profile(
        self,
        client_dir: Path,
        settings: dict,
        claude_md: str | None,
        mcp_servers: dict,
    ) -> None:
        client_dir.mkdir(parents=True, exist_ok=True)
        profile = {
            "$schema": "claude-profile-v1",
            "settings": settings,
            "claude_md": claude_md,
            "mcp_servers": mcp_servers,
        }
        (client_dir / ".claude-profile.json").write_text(json.dumps(profile), encoding="utf-8")

    def test_detect_drift_no_drift(self, tmp_path: Path):
        """When live config matches the profile exactly, no drift is reported."""
        settings = {"theme": "dark"}
        claude_md = "# rules"
        mcp_servers = {"ws": {"command": "uv"}}

        self._write_global_state(tmp_path, settings, claude_md, mcp_servers)

        client_dir = tmp_path / "client"
        self._write_client_profile(client_dir, settings, claude_md, mcp_servers)

        report = detect_drift(str(client_dir), claude_home=str(tmp_path))

        assert report.has_drift is False
        assert report.settings_changed is False
        assert report.claude_md_changed is False
        assert report.mcp_servers_changed is False
        assert report.active_hash != ""
        assert report.profile_hash != ""
        assert report.active_hash == report.profile_hash

    def test_detect_drift_settings_changed(self, tmp_path: Path):
        """Changed settings are detected; other components remain unchanged."""
        original_settings = {"theme": "dark"}
        new_settings = {"theme": "light", "fontSize": 16}
        claude_md = "# rules"
        mcp_servers: dict = {}

        # Live state has new_settings
        self._write_global_state(tmp_path, new_settings, claude_md, mcp_servers)

        # Profile was stored with original_settings
        client_dir = tmp_path / "client"
        self._write_client_profile(client_dir, original_settings, claude_md, mcp_servers)

        report = detect_drift(str(client_dir), claude_home=str(tmp_path))

        assert report.has_drift is True
        assert report.settings_changed is True
        assert report.claude_md_changed is False
        assert report.mcp_servers_changed is False
        assert report.active_hash != report.profile_hash


# ---------------------------------------------------------------------------
# Health check tests
# ---------------------------------------------------------------------------


class TestCheckWorkspaceHealth:
    def test_health_check_healthy(self, tmp_path: Path):
        """A workspace with all optional files present should be healthy or degraded."""
        ws = _setup_minimal_workspace(tmp_path)

        # Add all optional files/dirs so nothing warns
        _write_json(ws / ".claude-profile.json", {"$schema": "claude-profile-v1", "settings": {}, "mcp_servers": {}})

        rules_dir = ws / ".claude" / "rules"
        rules_dir.mkdir(parents=True)
        (rules_dir / "context.md").write_text("# Context rules", encoding="utf-8")

        skills_dir = ws / ".claude" / "skills"
        skills_dir.mkdir(parents=True)

        (ws / "CLAUDE.md").write_text("# Project guidance", encoding="utf-8")

        _write_json(ws / ".tentaqles.json", {"$schema": "tentaqles-v1"})

        knowledge_dir = ws / ".claude" / "knowledge"
        knowledge_dir.mkdir(parents=True)

        brand_dir = ws / "brand_context"
        brand_dir.mkdir()
        (brand_dir / "voice-profile.md").write_text("# Voice", encoding="utf-8")

        report = check_workspace_health(str(ws))

        assert report.overall in ("healthy", "degraded")
        assert report.workspace_path == str(ws)

        # profile_exists must pass since we have the file
        profile_check = next(c for c in report.checks if c.name == "profile_exists")
        assert profile_check.status == "pass"

    def test_health_check_broken_no_profile(self, tmp_path: Path):
        """An empty directory produces a broken report with profile_exists=fail."""
        ws = tmp_path / "empty_workspace"
        ws.mkdir()

        report = check_workspace_health(str(ws))

        assert report.overall == "broken"

        profile_check = next(c for c in report.checks if c.name == "profile_exists")
        assert profile_check.status == "fail"

    def test_health_check_warn_no_claude_md(self, tmp_path: Path):
        """A workspace without CLAUDE.md should have a warning on that check."""
        ws = _setup_minimal_workspace(tmp_path)
        # Deliberately do NOT create CLAUDE.md

        report = check_workspace_health(str(ws))

        claude_md_check = next(c for c in report.checks if c.name == "claude_md")
        assert claude_md_check.status == "warn"

        # Overall should be degraded (profile passes, everything else warns)
        assert report.overall == "degraded"
