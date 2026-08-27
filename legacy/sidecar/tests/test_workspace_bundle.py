"""Tests for workspace bundle export/import service."""

from __future__ import annotations

import json
from pathlib import Path

from sidecar.services.workspace_bundle import (
    export_workspace,
    export_workspace_to_file,
    import_workspace,
)


def _setup_exportable_client(tmp_path: Path) -> Path:
    """Create a client directory with known workspace files for testing."""
    client = tmp_path / "acme"
    client.mkdir()

    # .workspace-profile.json
    workspace_profile = {
        "$schema": "workspace-profile-v1",
        "client_name": "acme",
        "client_description": "Acme Corp",
        "git": {"platform": "github", "host": "github.com", "account": "acme"},
        "cloud": {"provider": "none"},
        "database": {"engine": "none"},
    }
    (client / ".workspace-profile.json").write_text(
        json.dumps(workspace_profile, indent=2) + "\n", encoding="utf-8"
    )

    # .claude-profile.json with API_KEY in env
    claude_profile = {
        "$schema": "claude-profile-v1",
        "settings": {
            "env": {
                "API_KEY": "sk-secret-123",
                "OTHER_VAR": "some-value",
            }
        },
        "claude_md": None,
        "mcp_servers": {},
    }
    (client / ".claude-profile.json").write_text(
        json.dumps(claude_profile, indent=2) + "\n", encoding="utf-8"
    )

    # .tentaqles.json
    tentaqles = {
        "$schema": "tentaqles-v1",
        "toggles": {"rules": {}, "mcps": {}, "hooks": {}, "skills": {}, "commands": {}},
        "propagation_excludes": [],
        "last_propagated_at": None,
    }
    (client / ".tentaqles.json").write_text(
        json.dumps(tentaqles, indent=2) + "\n", encoding="utf-8"
    )

    # .claude/rules/identity.md
    rules_dir = client / ".claude" / "rules"
    rules_dir.mkdir(parents=True)
    (rules_dir / "identity.md").write_text("# Identity Rule\nAlways use renato@tentaql.com.\n", encoding="utf-8")

    # CLAUDE.md
    (client / "CLAUDE.md").write_text("# Acme CLAUDE.md\nProject overview here.\n", encoding="utf-8")

    # brand_context/voice-profile.md
    brand_dir = client / "brand_context"
    brand_dir.mkdir()
    (brand_dir / "voice-profile.md").write_text("# Voice Profile\nProfessional tone.\n", encoding="utf-8")

    return client


# ---------------------------------------------------------------------------
# 1. Export strips secrets by default
# ---------------------------------------------------------------------------


def test_export_strips_secrets_by_default(tmp_path: Path) -> None:
    client = _setup_exportable_client(tmp_path)
    bundle = export_workspace(str(client))

    env = bundle["claude_profile"]["settings"]["env"]
    assert all(v == "<REDACTED>" for v in env.values()), (
        f"Expected all env values to be '<REDACTED>', got: {env}"
    )
    # Keys must be preserved
    assert "API_KEY" in env
    assert "OTHER_VAR" in env


# ---------------------------------------------------------------------------
# 2. Export includes secrets when opted in
# ---------------------------------------------------------------------------


def test_export_includes_secrets_when_opted_in(tmp_path: Path) -> None:
    client = _setup_exportable_client(tmp_path)
    bundle = export_workspace(str(client), include_secrets=True)

    env = bundle["claude_profile"]["settings"]["env"]
    assert env["API_KEY"] == "sk-secret-123"
    assert env["OTHER_VAR"] == "some-value"


# ---------------------------------------------------------------------------
# 3. Export includes rules and CLAUDE.md
# ---------------------------------------------------------------------------


def test_export_includes_rules_and_claude_md(tmp_path: Path) -> None:
    client = _setup_exportable_client(tmp_path)
    bundle = export_workspace(str(client))

    assert "identity.md" in bundle["rules"], "Expected identity.md in rules"
    assert "Identity Rule" in bundle["rules"]["identity.md"]

    assert bundle["claude_md"] is not None, "Expected claude_md to be present"
    assert "Acme CLAUDE.md" in bundle["claude_md"]


# ---------------------------------------------------------------------------
# 4. Import creates files
# ---------------------------------------------------------------------------


def test_import_creates_files(tmp_path: Path) -> None:
    client = _setup_exportable_client(tmp_path)
    bundle_path = tmp_path / "bundle.json"
    export_workspace_to_file(str(client), str(bundle_path))

    target = tmp_path / "restored"
    target.mkdir()

    result = import_workspace(str(bundle_path), str(target))

    assert (target / ".workspace-profile.json").exists(), ".workspace-profile.json not created"
    assert (target / "CLAUDE.md").exists(), "CLAUDE.md not created"
    assert result.client_name == "acme"
    assert len(result.files_written) > 0


# ---------------------------------------------------------------------------
# 5. Import skips existing files when merge=False (default)
# ---------------------------------------------------------------------------


def test_import_skip_existing(tmp_path: Path) -> None:
    client = _setup_exportable_client(tmp_path)
    bundle_path = tmp_path / "bundle.json"
    export_workspace_to_file(str(client), str(bundle_path))

    target = tmp_path / "partial"
    target.mkdir()

    # Pre-create CLAUDE.md with different content
    original_content = "# Pre-existing CLAUDE.md\n"
    (target / "CLAUDE.md").write_text(original_content, encoding="utf-8")

    result = import_workspace(str(bundle_path), str(target), merge=False)

    # CLAUDE.md should be skipped, not overwritten
    assert "CLAUDE.md" in result.files_skipped, (
        f"Expected CLAUDE.md in files_skipped, got skipped={result.files_skipped}"
    )
    assert (target / "CLAUDE.md").read_text(encoding="utf-8") == original_content, (
        "CLAUDE.md should NOT have been overwritten"
    )


# ---------------------------------------------------------------------------
# 6. Round-trip: export → import → re-export matches original
# ---------------------------------------------------------------------------


def test_import_round_trip(tmp_path: Path) -> None:
    client = _setup_exportable_client(tmp_path)

    # First export (with secrets so comparison is meaningful)
    bundle1 = export_workspace(str(client), include_secrets=True)
    bundle_path = tmp_path / "bundle.json"
    export_workspace_to_file(str(client), str(bundle_path), include_secrets=True)

    # Import into a fresh directory
    target = tmp_path / "round_trip"
    target.mkdir()
    import_workspace(str(bundle_path), str(target))

    # Re-export from the imported directory
    bundle2 = export_workspace(str(target), include_secrets=True)

    # Rules and CLAUDE.md must match exactly
    assert bundle1["rules"] == bundle2["rules"], (
        f"Rules mismatch after round-trip.\nOriginal: {bundle1['rules']}\nRestored: {bundle2['rules']}"
    )
    assert bundle1["claude_md"] == bundle2["claude_md"], (
        "CLAUDE.md content changed after round-trip"
    )
