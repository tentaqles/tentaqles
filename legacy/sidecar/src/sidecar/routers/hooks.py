"""Hooks router — scoped hooks CRUD for settings.json."""

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.schemas import ScopedRequest
from sidecar.services.hooks_service import (
    add_hook_group,
    get_hooks,
    remove_hook_group,
    save_hooks,
    update_hook_group,
)

router = APIRouter()


class HooksSaveRequest(ScopedRequest):
    hooks: dict


class AddHookGroupRequest(ScopedRequest):
    event: str
    matcher: str | None = None
    hooks: list[dict]


class RemoveHookGroupRequest(ScopedRequest):
    event: str
    group_index: int


class UpdateHookGroupRequest(ScopedRequest):
    event: str
    group_index: int
    matcher: str | None = None
    hooks: list[dict]


@router.post("/list")
def hooks_list(req: ScopedRequest):
    return get_hooks(req.scope, req.workspace_path)


@router.post("/save")
def hooks_save(req: HooksSaveRequest):
    save_hooks(req.scope, req.workspace_path, req.hooks)
    return {"ok": True}


@router.post("/add-group")
def hooks_add_group(req: AddHookGroupRequest):
    add_hook_group(req.scope, req.workspace_path, req.event, req.matcher, req.hooks)
    return {"ok": True}


@router.post("/remove-group")
def hooks_remove_group(req: RemoveHookGroupRequest):
    removed = remove_hook_group(req.scope, req.workspace_path, req.event, req.group_index)
    return {"removed": removed}


@router.post("/update-group")
def hooks_update_group(req: UpdateHookGroupRequest):
    updated = update_hook_group(req.scope, req.workspace_path, req.event, req.group_index, req.matcher, req.hooks)
    return {"updated": updated}
