"""Activation router — activate, deactivate, toggle, and capture workspaces."""

from __future__ import annotations

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.services.activation import (
    activate_workspace,
    capture_claude_profile,
    deactivate_workspace,
    get_active_workspace,
    toggle_workspace,
)
from sidecar.services.health import detect_drift

router = APIRouter()


class ActivateRequest(BaseModel):
    workspace_path: str


class CaptureRequest(BaseModel):
    client_path: str | None = None


@router.post("/activate")
def activate(req: ActivateRequest):
    """Activate a workspace by writing its Claude profile to global config."""
    result = activate_workspace(req.workspace_path)
    return result.model_dump()


@router.post("/deactivate")
def deactivate():
    """Deactivate the currently active workspace."""
    result = deactivate_workspace()
    return result.model_dump()


@router.post("/toggle")
def toggle():
    """Toggle to the previously active workspace."""
    result = toggle_workspace()
    return result.model_dump()


@router.post("/capture")
def capture(req: CaptureRequest):
    """Capture current global Claude state into a client's .claude-profile.json."""
    client_path = req.client_path
    if client_path is None:
        active = get_active_workspace()
        if active is None:
            return {"error": "No active workspace"}
        client_path = active.workspace_path
    result = capture_claude_profile(client_path)
    return result.model_dump(by_alias=True)


@router.get("/active")
def active():
    """Return the currently active workspace, or null if none."""
    active_ws = get_active_workspace()
    if active_ws is None:
        return None
    return active_ws.model_dump()


@router.get("/drift")
def drift():
    """Detect drift between the active workspace profile and global config."""
    active_ws = get_active_workspace()
    if active_ws is None:
        return {"error": "No active workspace"}
    result = detect_drift(active_ws.workspace_path)
    return result.model_dump()
