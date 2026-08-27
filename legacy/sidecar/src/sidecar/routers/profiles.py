"""Profiles router — save/restore workspace configuration snapshots."""

from __future__ import annotations

import json
import os
from datetime import datetime

from fastapi import APIRouter
from pydantic import BaseModel

router = APIRouter()

PROFILES_DIR = os.path.expanduser("~/.tentaqles/profiles")


class SaveProfileRequest(BaseModel):
    name: str
    workspace_path: str
    description: str = ""


class LoadProfileRequest(BaseModel):
    profile_id: str
    workspace_path: str


class DeleteProfileRequest(BaseModel):
    profile_id: str


def _ensure_profiles_dir():
    os.makedirs(PROFILES_DIR, exist_ok=True)


def _profile_path(profile_id: str) -> str:
    return os.path.join(PROFILES_DIR, f"{profile_id}.json")


def _read_workspace_config(workspace_path: str) -> dict:
    """Read current workspace config files into a snapshot."""
    snapshot: dict = {}

    # Read CLAUDE.md
    claude_md_path = os.path.join(workspace_path, "CLAUDE.md")
    if os.path.exists(claude_md_path):
        with open(claude_md_path, "r", encoding="utf-8") as f:
            snapshot["claude_md"] = f.read()

    # Read rules
    rules_dir = os.path.join(workspace_path, ".claude", "rules")
    if os.path.isdir(rules_dir):
        rules: dict[str, str] = {}
        for fname in os.listdir(rules_dir):
            if fname.endswith(".md"):
                fpath = os.path.join(rules_dir, fname)
                with open(fpath, "r", encoding="utf-8") as f:
                    rules[fname] = f.read()
        snapshot["rules"] = rules

    # Read MCP config
    mcp_path = os.path.join(workspace_path, ".mcp.json")
    if os.path.exists(mcp_path):
        with open(mcp_path, "r", encoding="utf-8") as f:
            snapshot["mcp_config"] = json.load(f)

    return snapshot


def _apply_workspace_config(workspace_path: str, snapshot: dict):
    """Apply a saved snapshot to a workspace."""
    # Write CLAUDE.md
    if "claude_md" in snapshot:
        claude_md_path = os.path.join(workspace_path, "CLAUDE.md")
        with open(claude_md_path, "w", encoding="utf-8") as f:
            f.write(snapshot["claude_md"])

    # Write rules
    if "rules" in snapshot:
        rules_dir = os.path.join(workspace_path, ".claude", "rules")
        os.makedirs(rules_dir, exist_ok=True)
        for fname, content in snapshot["rules"].items():
            fpath = os.path.join(rules_dir, fname)
            with open(fpath, "w", encoding="utf-8") as f:
                f.write(content)

    # Write MCP config
    if "mcp_config" in snapshot:
        mcp_path = os.path.join(workspace_path, ".mcp.json")
        with open(mcp_path, "w", encoding="utf-8") as f:
            json.dump(snapshot["mcp_config"], f, indent=2)


@router.post("/list")
def list_profiles():
    """List all saved profiles."""
    _ensure_profiles_dir()
    profiles = []
    for fname in os.listdir(PROFILES_DIR):
        if fname.endswith(".json"):
            fpath = os.path.join(PROFILES_DIR, fname)
            try:
                with open(fpath, "r", encoding="utf-8") as f:
                    data = json.load(f)
                profiles.append(
                    {
                        "id": fname.replace(".json", ""),
                        "name": data.get("name", ""),
                        "description": data.get("description", ""),
                        "source_path": data.get("source_path", ""),
                        "created_at": data.get("created_at", ""),
                        "rule_count": len(data.get("snapshot", {}).get("rules", {})),
                        "has_claude_md": "claude_md" in data.get("snapshot", {}),
                        "mcp_count": len(data.get("snapshot", {}).get("mcp_config", {}).get("mcpServers", {})),
                    }
                )
            except Exception:
                pass
    profiles.sort(key=lambda p: p["created_at"], reverse=True)
    return profiles


@router.post("/save")
def save_profile(req: SaveProfileRequest):
    """Save current workspace config as a profile."""
    _ensure_profiles_dir()

    snapshot = _read_workspace_config(req.workspace_path)
    now = datetime.now().isoformat()
    profile_id = f"profile_{now.replace(':', '-').replace('.', '-')}"

    data = {
        "name": req.name,
        "description": req.description,
        "source_path": req.workspace_path,
        "created_at": now,
        "snapshot": snapshot,
    }

    with open(_profile_path(profile_id), "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2)

    return {"id": profile_id, "success": True}


@router.post("/load")
def load_profile(req: LoadProfileRequest):
    """Apply a saved profile to a workspace."""
    fpath = _profile_path(req.profile_id)
    if not os.path.exists(fpath):
        return {"success": False, "error": "Profile not found"}

    with open(fpath, "r", encoding="utf-8") as f:
        data = json.load(f)

    _apply_workspace_config(req.workspace_path, data.get("snapshot", {}))
    return {"success": True}


@router.post("/delete")
def delete_profile(req: DeleteProfileRequest):
    """Delete a saved profile."""
    fpath = _profile_path(req.profile_id)
    if os.path.exists(fpath):
        os.remove(fpath)
        return {"success": True}
    return {"success": False, "error": "Profile not found"}
