"""Health and drift detection service for workspace configuration."""

from __future__ import annotations

import hashlib
import json
from datetime import UTC, datetime
from pathlib import Path

from sidecar.models import ClaudeProfile, DriftReport, HealthCheck, HealthReport
from sidecar.parsers import parse_claude_profile


def compute_config_hash(settings: dict, claude_md: str | None, mcp_servers: dict) -> str:
    """Compute an MD5 hash over the three config components.

    Each component is JSON-serialised with sort_keys=True before hashing so
    the result is deterministic regardless of key insertion order.
    """
    h = hashlib.md5()
    h.update(json.dumps(settings, sort_keys=True).encode())
    h.update(json.dumps(claude_md, sort_keys=True).encode())
    h.update(json.dumps(mcp_servers, sort_keys=True).encode())
    return h.hexdigest()


def _read_global_state(claude_home: str | None = None) -> tuple[dict, str | None, dict]:
    """Read live global Claude Code config files from disk.

    Returns (settings, claude_md, mcp_servers).
    All reads are tolerant of missing or malformed files.
    """
    home = Path(claude_home) if claude_home is not None else Path.home()

    # settings.json
    settings: dict = {}
    settings_path = home / ".claude" / "settings.json"
    try:
        raw = settings_path.read_text(encoding="utf-8")
        settings = json.loads(raw)
        if not isinstance(settings, dict):
            settings = {}
    except (FileNotFoundError, OSError, json.JSONDecodeError):
        settings = {}

    # CLAUDE.md
    claude_md: str | None = None
    claude_md_path = home / ".claude" / "CLAUDE.md"
    try:
        claude_md = claude_md_path.read_text(encoding="utf-8")
    except (FileNotFoundError, OSError):
        claude_md = None

    # .claude.json — extract mcpServers field
    mcp_servers: dict = {}
    claude_json_path = home / ".claude.json"
    try:
        raw = claude_json_path.read_text(encoding="utf-8")
        data = json.loads(raw)
        if isinstance(data, dict):
            mcp_servers = data.get("mcpServers", {})
            if not isinstance(mcp_servers, dict):
                mcp_servers = {}
    except (FileNotFoundError, OSError, json.JSONDecodeError):
        mcp_servers = {}

    return settings, claude_md, mcp_servers


def detect_drift(client_path: str, claude_home: str | None = None) -> DriftReport:
    """Compare live global config against stored .claude-profile.json.

    Returns a DriftReport describing which components (if any) have changed.
    If no profile is found all flags are set to True with empty hashes.
    """
    profile_path = str(Path(client_path) / ".claude-profile.json")
    profile: ClaudeProfile | None = parse_claude_profile(profile_path)

    # Read live state
    live_settings, live_claude_md, live_mcp_servers = _read_global_state(claude_home)
    active_hash = compute_config_hash(live_settings, live_claude_md, live_mcp_servers)

    if profile is None:
        return DriftReport(
            has_drift=True,
            settings_changed=True,
            claude_md_changed=True,
            mcp_servers_changed=True,
            active_hash=active_hash,
            profile_hash="",
        )

    profile_hash = compute_config_hash(profile.settings, profile.claude_md, profile.mcp_servers)

    # Component-level comparison
    settings_changed = (
        compute_config_hash(live_settings, None, {}) != compute_config_hash(profile.settings, None, {})
    )
    claude_md_changed = (
        compute_config_hash({}, live_claude_md, {}) != compute_config_hash({}, profile.claude_md, {})
    )
    mcp_servers_changed = (
        compute_config_hash({}, None, live_mcp_servers) != compute_config_hash({}, None, profile.mcp_servers)
    )

    has_drift = settings_changed or claude_md_changed or mcp_servers_changed

    return DriftReport(
        has_drift=has_drift,
        settings_changed=settings_changed,
        claude_md_changed=claude_md_changed,
        mcp_servers_changed=mcp_servers_changed,
        active_hash=active_hash,
        profile_hash=profile_hash,
    )


def check_workspace_health(workspace_path: str, claude_home: str | None = None) -> HealthReport:
    """Run 8 health checks on a workspace directory.

    Returns a HealthReport with an overall status of 'healthy', 'degraded',
    or 'broken'.
    """
    wp = Path(workspace_path)
    checks: list[HealthCheck] = []

    # 1. profile_exists — .workspace-profile.json present and valid JSON (fail)
    profile_file = wp / ".workspace-profile.json"
    client_name = ""
    if profile_file.exists():
        try:
            data = json.loads(profile_file.read_text(encoding="utf-8"))
            client_name = data.get("client_name", "")
            checks.append(HealthCheck(
                name="profile_exists", status="pass",
                message=".workspace-profile.json present and valid",
            ))
        except (json.JSONDecodeError, OSError):
            checks.append(HealthCheck(
                name="profile_exists", status="fail",
                message=".workspace-profile.json invalid JSON",
            ))
    else:
        checks.append(HealthCheck(
            name="profile_exists", status="fail",
            message=".workspace-profile.json missing",
        ))

    # 2. claude_profile_exists — .claude-profile.json present (warn)
    if (wp / ".claude-profile.json").exists():
        checks.append(HealthCheck(name="claude_profile_exists", status="pass", message=".claude-profile.json present"))
    else:
        checks.append(HealthCheck(name="claude_profile_exists", status="warn", message=".claude-profile.json missing"))

    # 3. rules_dir — .claude/rules/ exists and has .md files (warn)
    rules_dir = wp / ".claude" / "rules"
    if rules_dir.exists() and any(rules_dir.glob("*.md")):
        checks.append(HealthCheck(name="rules_dir", status="pass", message=".claude/rules/ has .md files"))
    elif rules_dir.exists():
        checks.append(HealthCheck(
            name="rules_dir", status="warn",
            message=".claude/rules/ exists but has no .md files",
        ))
    else:
        checks.append(HealthCheck(name="rules_dir", status="warn", message=".claude/rules/ missing"))

    # 4. skills_dir — .claude/skills/ exists (warn)
    skills_dir = wp / ".claude" / "skills"
    if skills_dir.exists():
        checks.append(HealthCheck(name="skills_dir", status="pass", message=".claude/skills/ present"))
    else:
        checks.append(HealthCheck(name="skills_dir", status="warn", message=".claude/skills/ missing"))

    # 5. claude_md — CLAUDE.md exists and not empty (warn)
    claude_md_file = wp / "CLAUDE.md"
    if claude_md_file.exists():
        try:
            content = claude_md_file.read_text(encoding="utf-8").strip()
            if content:
                checks.append(HealthCheck(name="claude_md", status="pass", message="CLAUDE.md present and non-empty"))
            else:
                checks.append(HealthCheck(name="claude_md", status="warn", message="CLAUDE.md exists but is empty"))
        except OSError:
            checks.append(HealthCheck(name="claude_md", status="warn", message="CLAUDE.md could not be read"))
    else:
        checks.append(HealthCheck(name="claude_md", status="warn", message="CLAUDE.md missing"))

    # 6. tentaqles_state — .tentaqles.json valid JSON with schema "tentaqles-v1" (warn)
    tentaqles_file = wp / ".tentaqles.json"
    if tentaqles_file.exists():
        try:
            data = json.loads(tentaqles_file.read_text(encoding="utf-8"))
            schema = data.get("$schema", "")
            if schema == "tentaqles-v1":
                checks.append(HealthCheck(
                    name="tentaqles_state", status="pass",
                    message=".tentaqles.json valid with correct schema",
                ))
            else:
                checks.append(HealthCheck(
                    name="tentaqles_state", status="warn",
                    message=f".tentaqles.json has unexpected schema: {schema!r}",
                ))
        except (json.JSONDecodeError, OSError):
            checks.append(HealthCheck(name="tentaqles_state", status="warn", message=".tentaqles.json invalid JSON"))
    else:
        checks.append(HealthCheck(name="tentaqles_state", status="warn", message=".tentaqles.json missing"))

    # 7. knowledge_dir — .claude/knowledge/ exists (warn)
    knowledge_dir = wp / ".claude" / "knowledge"
    if knowledge_dir.exists():
        checks.append(HealthCheck(name="knowledge_dir", status="pass", message=".claude/knowledge/ present"))
    else:
        checks.append(HealthCheck(name="knowledge_dir", status="warn", message=".claude/knowledge/ missing"))

    # 8. brand_context — brand_context/voice-profile.md exists (warn)
    brand_file = wp / "brand_context" / "voice-profile.md"
    if brand_file.exists():
        checks.append(HealthCheck(
            name="brand_context", status="pass",
            message="brand_context/voice-profile.md present",
        ))
    else:
        checks.append(HealthCheck(
            name="brand_context", status="warn",
            message="brand_context/voice-profile.md missing",
        ))

    # Overall status
    statuses = {c.status for c in checks}
    if "fail" in statuses:
        overall = "broken"
    elif "warn" in statuses:
        overall = "degraded"
    else:
        overall = "healthy"

    timestamp = datetime.now(UTC).isoformat()

    return HealthReport(
        workspace_path=workspace_path,
        client_name=client_name,
        overall=overall,
        checks=checks,
        timestamp=timestamp,
    )
