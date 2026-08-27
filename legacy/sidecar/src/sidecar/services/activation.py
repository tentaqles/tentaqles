"""Workspace activation engine: activate, deactivate, toggle, capture."""

from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path

from sidecar.models import ActivationResult, ActiveWorkspace, ClaudeProfile, DeactivationResult
from sidecar.parsers import parse_claude_profile, read_file_content, write_claude_profile
from sidecar.services.health import _read_global_state, detect_drift
from sidecar.services.safe_io import FileTransaction, atomic_write

_DEFAULT_STATE_DIR = Path.home() / ".tentaqles"


def _resolve_state_dir(state_dir: str | None) -> Path:
    return Path(state_dir) if state_dir is not None else _DEFAULT_STATE_DIR


def _resolve_claude_home(claude_home: str | None) -> Path:
    return Path(claude_home) if claude_home is not None else Path.home()


def _resolve_client_name(workspace_path: str | Path) -> str:
    """Read .workspace-profile.json for client_name, fallback to directory name."""
    wp = Path(workspace_path)
    profile_path = wp / ".workspace-profile.json"
    content = read_file_content(str(profile_path))
    if content is not None:
        try:
            data = json.loads(content)
            name = data.get("client_name", "")
            if name:
                return name
        except (json.JSONDecodeError, AttributeError):
            pass
    return wp.name


def _write_mcp_servers(claude_json_path: str | Path, mcp_servers: dict) -> str:
    """Merge MCP servers into existing .claude.json, preserving other fields.

    Returns the full JSON string to write.
    """
    path = Path(claude_json_path)
    existing: dict = {}
    if path.exists():
        try:
            raw = path.read_text(encoding="utf-8")
            data = json.loads(raw)
            if isinstance(data, dict):
                existing = data
        except (json.JSONDecodeError, OSError):
            existing = {}

    existing["mcpServers"] = mcp_servers
    return json.dumps(existing, indent=2, ensure_ascii=False) + "\n"


def activate_workspace(
    workspace_path: str | Path,
    claude_home: str | None = None,
    state_dir: str | None = None,
) -> ActivationResult:
    """Activate a workspace by writing its Claude profile to global config.

    9-step transaction:
    1. Load .claude-profile.json
    2. Read current global state
    3. Detect and auto-sync drift from previous workspace
    4. Atomically write settings.json, CLAUDE.md, .claude.json
    5. Identity verification (best-effort)
    6. Write .previous-workspace and .active-workspace state files
    7. Return ActivationResult
    """
    wp = Path(workspace_path)
    home = _resolve_claude_home(claude_home)
    sd = _resolve_state_dir(state_dir)
    sd.mkdir(parents=True, exist_ok=True)

    # Step 1 — load .claude-profile.json (raises FileNotFoundError if missing)
    profile_path = wp / ".claude-profile.json"
    if not profile_path.exists():
        raise FileNotFoundError(f".claude-profile.json not found at {profile_path}")
    profile = parse_claude_profile(str(profile_path))
    if profile is None:
        raise FileNotFoundError(f"Failed to parse .claude-profile.json at {profile_path}")

    # Step 3 — detect and auto-sync drift from the previously active workspace
    auto_synced = False
    auto_sync_client: str | None = None
    active_ws_file = sd / ".active-workspace"
    warnings: list[str] = []

    if active_ws_file.exists():
        try:
            old_path_str = active_ws_file.read_text(encoding="utf-8").strip()
            old_path = Path(old_path_str)
            if old_path_str != str(wp) and old_path != wp:
                old_profile_path = old_path / ".claude-profile.json"
                if old_profile_path.exists():
                    drift = detect_drift(str(old_path), claude_home)
                    if drift.has_drift:
                        live_settings, live_claude_md, live_mcp_servers = _read_global_state(claude_home)
                        synced_profile = ClaudeProfile(
                            settings=live_settings,
                            claude_md=live_claude_md,
                            mcp_servers=live_mcp_servers,
                        )
                        write_claude_profile(str(old_profile_path), synced_profile)
                        auto_synced = True
                        auto_sync_client = _resolve_client_name(old_path)
        except Exception as exc:
            warnings.append(f"Auto-sync check failed: {exc}")

    # Step 4 — atomically write global config files
    settings_path = home / ".claude" / "settings.json"
    claude_md_path = home / ".claude" / "CLAUDE.md"
    claude_json_path = home / ".claude.json"

    settings_json = json.dumps(profile.settings, indent=2, ensure_ascii=False) + "\n"
    mcp_json = _write_mcp_servers(claude_json_path, profile.mcp_servers)

    with FileTransaction(f"activate-{wp.name}", backup_dir=sd / "backups") as tx:
        tx.write(settings_path, settings_json)
        if profile.claude_md is not None:
            tx.write(claude_md_path, profile.claude_md)
        else:
            tx.delete(claude_md_path)
        tx.write(claude_json_path, mcp_json)

    backup_id = tx.backup_id

    # Step 5 — identity verification (best-effort)
    identity_verified = False
    identity_warnings: list[str] = []
    try:
        wp_profile_path = wp / ".workspace-profile.json"
        content = read_file_content(str(wp_profile_path))
        if content is not None:
            data = json.loads(content)
            git_config = data.get("git", {})
            account = git_config.get("account")
            if account:
                identity_verified = True
            else:
                identity_warnings.append("git account not set in .workspace-profile.json")
        else:
            identity_warnings.append(".workspace-profile.json not found")
    except Exception as exc:
        identity_warnings.append(f"Identity check error: {exc}")

    # Step 6 — write state files
    prev_ws_file = sd / ".previous-workspace"
    try:
        if active_ws_file.exists():
            old_active = active_ws_file.read_text(encoding="utf-8").strip()
            atomic_write(prev_ws_file, old_active)
    except Exception as exc:
        warnings.append(f"Could not write .previous-workspace: {exc}")

    atomic_write(active_ws_file, str(wp))

    # Step 7 — return result
    client_name = _resolve_client_name(wp)
    return ActivationResult(
        workspace_path=str(wp),
        client_name=client_name,
        auto_synced=auto_synced,
        auto_sync_client=auto_sync_client,
        identity_verified=identity_verified,
        identity_warnings=identity_warnings,
        warnings=warnings,
        backup_id=backup_id,
    )


def deactivate_workspace(
    claude_home: str | None = None,
    state_dir: str | None = None,
) -> DeactivationResult:
    """Deactivate the currently active workspace.

    Reads the active workspace, detects and auto-syncs any drift, then removes
    the .active-workspace state file.
    """
    sd = _resolve_state_dir(state_dir)
    active_ws_file = sd / ".active-workspace"

    previous_workspace: str | None = None
    auto_synced = False

    if not active_ws_file.exists():
        return DeactivationResult(previous_workspace=None, auto_synced=False)

    try:
        active_path_str = active_ws_file.read_text(encoding="utf-8").strip()
        previous_workspace = active_path_str
        active_path = Path(active_path_str)

        if (active_path / ".claude-profile.json").exists():
            drift = detect_drift(str(active_path), claude_home)
            if drift.has_drift:
                live_settings, live_claude_md, live_mcp_servers = _read_global_state(claude_home)
                synced_profile = ClaudeProfile(
                    settings=live_settings,
                    claude_md=live_claude_md,
                    mcp_servers=live_mcp_servers,
                )
                write_claude_profile(str(active_path / ".claude-profile.json"), synced_profile)
                auto_synced = True
    except Exception:
        pass

    try:
        active_ws_file.unlink()
    except Exception:
        pass

    return DeactivationResult(previous_workspace=previous_workspace, auto_synced=auto_synced)


def toggle_workspace(
    claude_home: str | None = None,
    state_dir: str | None = None,
) -> ActivationResult:
    """Activate the previously active workspace (toggle between two workspaces).

    Raises FileNotFoundError if no .previous-workspace is recorded.
    """
    sd = _resolve_state_dir(state_dir)
    prev_ws_file = sd / ".previous-workspace"

    if not prev_ws_file.exists():
        raise FileNotFoundError(".previous-workspace not found — no previous workspace to toggle to")

    prev_path = prev_ws_file.read_text(encoding="utf-8").strip()
    return activate_workspace(prev_path, claude_home=claude_home, state_dir=state_dir)


def capture_claude_profile(
    client_path: str | Path,
    claude_home: str | None = None,
) -> ClaudeProfile:
    """Capture the current global Claude Code state into a client's .claude-profile.json.

    Reads live global state and writes it to {client_path}/.claude-profile.json.
    Returns the created ClaudeProfile.
    """
    cp = Path(client_path)
    live_settings, live_claude_md, live_mcp_servers = _read_global_state(claude_home)

    profile = ClaudeProfile(
        settings=live_settings,
        claude_md=live_claude_md,
        mcp_servers=live_mcp_servers,
    )

    write_claude_profile(str(cp / ".claude-profile.json"), profile)
    return profile


def get_active_workspace(state_dir: str | None = None) -> ActiveWorkspace | None:
    """Return info about the currently active workspace, or None if none is active."""
    sd = _resolve_state_dir(state_dir)
    active_ws_file = sd / ".active-workspace"

    if not active_ws_file.exists():
        return None

    try:
        workspace_path_str = active_ws_file.read_text(encoding="utf-8").strip()
        workspace_path = Path(workspace_path_str)
        client_name = _resolve_client_name(workspace_path)
        mtime = active_ws_file.stat().st_mtime
        activated_at = datetime.fromtimestamp(mtime, tz=UTC).isoformat()

        return ActiveWorkspace(
            workspace_path=workspace_path_str,
            client_name=client_name,
            activated_at=activated_at,
        )
    except Exception:
        return None
