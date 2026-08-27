"""Memory router — scoped CLAUDE.md read/write."""

from fastapi import APIRouter

from sidecar.schemas import ScopedContentRequest, ScopedRequest, WorkspaceRequest
from sidecar.services.memory_service import get_all_memory, get_memory, save_memory

router = APIRouter()


@router.post("/get")
def memory_get(req: ScopedRequest):
    return get_memory(req.scope, req.workspace_path)


@router.post("/save")
def memory_save(req: ScopedContentRequest):
    save_memory(req.scope, req.workspace_path, req.content)
    return {"ok": True}


@router.post("/all")
def memory_all(req: WorkspaceRequest):
    return get_all_memory(req.workspace_path)
