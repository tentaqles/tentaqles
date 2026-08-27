"""Toggle router — enable/disable rules, MCPs, hooks."""

from fastapi import APIRouter

from sidecar.schemas import IsEnabledRequest, ToggleRequest, WorkspaceRequest
from sidecar.services.toggle_service import (
    get_manager_state,
    is_enabled,
    toggle_hook,
    toggle_mcp,
    toggle_rule,
    toggle_skill,
)

router = APIRouter()


@router.post("/state")
def get_state(req: WorkspaceRequest):
    state = get_manager_state(req.workspace_path)
    return state.model_dump(by_alias=True)


@router.post("/rule/toggle")
def toggle_rule_endpoint(req: ToggleRequest):
    toggle_rule(req.workspace_path, req.name, req.enabled)
    return {"ok": True}


@router.post("/mcp/toggle")
def toggle_mcp_endpoint(req: ToggleRequest):
    toggle_mcp(req.workspace_path, req.name, req.enabled)
    return {"ok": True}


@router.post("/hook/toggle")
def toggle_hook_endpoint(req: ToggleRequest):
    toggle_hook(req.workspace_path, req.name, req.enabled)
    return {"ok": True}


@router.post("/skill/toggle")
def toggle_skill_endpoint(req: ToggleRequest):
    toggle_skill(req.workspace_path, req.name, req.enabled)
    return {"ok": True}


@router.post("/is-enabled")
def is_enabled_endpoint(req: IsEnabledRequest):
    return {"enabled": is_enabled(req.workspace_path, req.config_type, req.name)}
