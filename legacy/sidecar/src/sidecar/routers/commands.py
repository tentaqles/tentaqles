"""Commands router — scoped slash command CRUD."""

from fastapi import APIRouter

from sidecar.schemas import ScopedFileRequest, ScopedRequest, ScopedSaveFileRequest
from sidecar.services.commands_service import (
    delete_command,
    get_command,
    list_commands,
    save_command,
)

router = APIRouter()


@router.post("/list")
def commands_list(req: ScopedRequest):
    return list_commands(req.scope, req.workspace_path)


@router.post("/get")
def commands_get(req: ScopedFileRequest):
    result = get_command(req.scope, req.workspace_path, req.filename)
    if result is None:
        return {"error": "Command not found"}
    return result


@router.post("/save")
def commands_save(req: ScopedSaveFileRequest):
    save_command(req.scope, req.workspace_path, req.filename, req.content)
    return {"ok": True}


@router.post("/delete")
def commands_delete(req: ScopedFileRequest):
    return {"deleted": delete_command(req.scope, req.workspace_path, req.filename)}
