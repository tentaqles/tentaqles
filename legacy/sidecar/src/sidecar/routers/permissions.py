"""Permissions router — scoped permission rules CRUD."""

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.schemas import ScopedRequest, WorkspaceRequest
from sidecar.services.permissions_service import (
    add_rule,
    get_merged_permissions,
    get_permissions,
    remove_rule,
    save_permissions,
)

router = APIRouter()


class PermissionsSaveRequest(ScopedRequest):
    permissions: dict


class RuleRequest(ScopedRequest):
    category: str  # "allow" | "deny" | "ask"
    rule: str


@router.post("/get")
def perms_get(req: ScopedRequest):
    return get_permissions(req.scope, req.workspace_path)


@router.post("/save")
def perms_save(req: PermissionsSaveRequest):
    save_permissions(req.scope, req.workspace_path, req.permissions)
    return {"ok": True}


@router.post("/add-rule")
def perms_add_rule(req: RuleRequest):
    add_rule(req.scope, req.workspace_path, req.category, req.rule)
    return {"ok": True}


@router.post("/remove-rule")
def perms_remove_rule(req: RuleRequest):
    removed = remove_rule(req.scope, req.workspace_path, req.category, req.rule)
    return {"removed": removed}


@router.post("/merged")
def perms_merged(req: WorkspaceRequest):
    return get_merged_permissions(req.workspace_path)
