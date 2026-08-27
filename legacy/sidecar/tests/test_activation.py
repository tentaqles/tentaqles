"""Tests for the workspace activation engine."""

from __future__ import annotations

import json
from pathlib import Path

from sidecar.services.activation import (
    activate_workspace,
    capture_claude_profile,
    deactivate_workspace,
    get_active_workspace,
    toggle_workspace,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _setup_client(tmp_path: Path, name: str = "test-client") -> Path:
    """Create a client dir with .workspace-profile.json and .claude-profile.json."""
    client = tmp_path / name
    client.mkdir()
    (client / ".workspace-profile.json").write_text(
        json.dumps(
            {
                "$schema": "workspace-profile-v1",
                "client_name": name,
                "git": {"platform": "github", "host": "github.com", "account": "testuser"},
                "cloud": {"provider": "none"},
                "database": {"type": "none"},
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )
    (client / ".claude-profile.json").write_text(
        json.dumps(
            {
                "$schema": "claude-profile-v1",
                "settings": {"model": "opus", "env": {"KEY": f"val-{name}"}},
                "claude_md": f"# Global instructions for {name}",
                "mcp_servers": {"ws": {"command": "uv", "args": ["run", name]}},
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )
    return client


def _setup_claude_home(tmp_path: Path) -> Path:
    home = tmp_path / "home"
    (home / ".claude").mkdir(parents=True)
    (home / ".claude" / "settings.json").write_text(json.dumps({"model": "default"}, indent=2) + "\n", encoding="utf-8")
    return home


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_activate_workspace_writes_global_config(tmp_path: Path) -> None:
    """Activate writes settings.json, CLAUDE.md, and MCP servers into global config."""
    client = _setup_client(tmp_path)
    home = _setup_claude_home(tmp_path)
    state_dir = str(tmp_path / "state")

    result = activate_workspace(str(client), claude_home=str(home), state_dir=state_dir)

    # settings.json
    settings_path = home / ".claude" / "settings.json"
    settings = json.loads(settings_path.read_text(encoding="utf-8"))
    assert settings["model"] == "opus"
    assert settings["env"]["KEY"] == "val-test-client"

    # CLAUDE.md
    claude_md_path = home / ".claude" / "CLAUDE.md"
    assert claude_md_path.exists()
    assert "test-client" in claude_md_path.read_text(encoding="utf-8")

    # .claude.json MCP servers
    claude_json_path = home / ".claude.json"
    assert claude_json_path.exists()
    data = json.loads(claude_json_path.read_text(encoding="utf-8"))
    assert "ws" in data["mcpServers"]
    assert data["mcpServers"]["ws"]["command"] == "uv"

    assert result.workspace_path == str(client)
    assert result.client_name == "test-client"


def test_activate_workspace_creates_backup(tmp_path: Path) -> None:
    """Activate creates a backup and returns a non-None backup_id."""
    client = _setup_client(tmp_path)
    home = _setup_claude_home(tmp_path)
    state_dir = str(tmp_path / "state")

    result = activate_workspace(str(client), claude_home=str(home), state_dir=state_dir)

    assert result.backup_id is not None
    backup_dir = tmp_path / "state" / "backups" / result.backup_id
    assert backup_dir.exists()
    assert (backup_dir / "manifest.json").exists()


def test_activate_workspace_tracks_state(tmp_path: Path) -> None:
    """Activate writes .active-workspace with the workspace path."""
    client = _setup_client(tmp_path)
    home = _setup_claude_home(tmp_path)
    state_dir = tmp_path / "state"

    activate_workspace(str(client), claude_home=str(home), state_dir=str(state_dir))

    active_file = state_dir / ".active-workspace"
    assert active_file.exists()
    stored_path = active_file.read_text(encoding="utf-8").strip()
    assert stored_path == str(client)


def test_activate_workspace_auto_syncs_previous(tmp_path: Path) -> None:
    """Activating B after editing global config while A was active auto-syncs A's profile."""
    client_a = _setup_client(tmp_path, "client-a")
    client_b = _setup_client(tmp_path, "client-b")
    home = _setup_claude_home(tmp_path)
    state_dir = str(tmp_path / "state")

    # Activate A
    activate_workspace(str(client_a), claude_home=str(home), state_dir=state_dir)

    # Simulate user editing global settings while A is active
    settings_path = home / ".claude" / "settings.json"
    edited_settings = {"model": "sonnet", "edited": True}
    settings_path.write_text(json.dumps(edited_settings, indent=2) + "\n", encoding="utf-8")

    # Activate B — should auto-sync the edited settings back to A's profile
    result_b = activate_workspace(str(client_b), claude_home=str(home), state_dir=state_dir)

    assert result_b.auto_synced is True
    assert result_b.auto_sync_client == "client-a"

    # Verify A's profile was updated with the edited settings
    a_profile_path = client_a / ".claude-profile.json"
    a_profile_data = json.loads(a_profile_path.read_text(encoding="utf-8"))
    assert a_profile_data["settings"]["model"] == "sonnet"
    assert a_profile_data["settings"]["edited"] is True


def test_deactivate_workspace(tmp_path: Path) -> None:
    """Deactivate removes .active-workspace and returns the previous path."""
    client = _setup_client(tmp_path)
    home = _setup_claude_home(tmp_path)
    state_dir = tmp_path / "state"

    activate_workspace(str(client), claude_home=str(home), state_dir=str(state_dir))
    assert (state_dir / ".active-workspace").exists()

    result = deactivate_workspace(claude_home=str(home), state_dir=str(state_dir))

    assert not (state_dir / ".active-workspace").exists()
    assert result.previous_workspace == str(client)


def test_toggle_workspace(tmp_path: Path) -> None:
    """Toggle switches back to the previously active workspace."""
    client_a = _setup_client(tmp_path, "client-a")
    client_b = _setup_client(tmp_path, "client-b")
    home = _setup_claude_home(tmp_path)
    state_dir = str(tmp_path / "state")

    # Activate A, then B
    activate_workspace(str(client_a), claude_home=str(home), state_dir=state_dir)
    activate_workspace(str(client_b), claude_home=str(home), state_dir=state_dir)

    # Toggle should go back to A
    result = toggle_workspace(claude_home=str(home), state_dir=state_dir)

    assert result.workspace_path == str(client_a)
    assert result.client_name == "client-a"

    # .active-workspace should now point to A
    active_file = Path(state_dir) / ".active-workspace"
    assert active_file.read_text(encoding="utf-8").strip() == str(client_a)


def test_get_active_workspace(tmp_path: Path) -> None:
    """Returns None when no workspace is active, and ActiveWorkspace after activation."""
    home = _setup_claude_home(tmp_path)
    state_dir = str(tmp_path / "state")

    # No active workspace yet
    result_none = get_active_workspace(state_dir=state_dir)
    assert result_none is None

    client = _setup_client(tmp_path)
    activate_workspace(str(client), claude_home=str(home), state_dir=state_dir)

    active = get_active_workspace(state_dir=state_dir)
    assert active is not None
    assert active.workspace_path == str(client)
    assert active.client_name == "test-client"
    assert active.activated_at  # non-empty ISO timestamp


def test_capture_claude_profile(tmp_path: Path) -> None:
    """Capture creates .claude-profile.json in a new client dir from global state."""
    home = _setup_claude_home(tmp_path)

    # Set up a richer global config to capture
    settings_path = home / ".claude" / "settings.json"
    settings_path.write_text(
        json.dumps({"model": "claude-3-opus", "feature": "x"}, indent=2) + "\n",
        encoding="utf-8",
    )
    claude_md_path = home / ".claude" / "CLAUDE.md"
    claude_md_path.write_text("# Global CLAUDE.md\n", encoding="utf-8")
    claude_json_path = home / ".claude.json"
    claude_json_path.write_text(
        json.dumps({"mcpServers": {"my-server": {"command": "npx", "args": ["my-server"]}}}, indent=2) + "\n",
        encoding="utf-8",
    )

    # New client dir with no .claude-profile.json yet
    new_client = tmp_path / "new-client"
    new_client.mkdir()

    profile = capture_claude_profile(str(new_client), claude_home=str(home))

    # Profile returned has correct data
    assert profile.settings["model"] == "claude-3-opus"
    assert profile.claude_md == "# Global CLAUDE.md\n"
    assert "my-server" in profile.mcp_servers

    # File was written
    profile_file = new_client / ".claude-profile.json"
    assert profile_file.exists()
    stored = json.loads(profile_file.read_text(encoding="utf-8"))
    assert stored["settings"]["model"] == "claude-3-opus"
    assert stored["mcp_servers"]["my-server"]["command"] == "npx"
