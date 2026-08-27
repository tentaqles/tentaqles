"""Session memory router — daily session logs."""

from fastapi import APIRouter

from sidecar.schemas import MemoryTimelineRequest, SessionMemoryGetRequest, SessionMemorySaveRequest
from sidecar.services.session_memory_service import get_session_memory, list_memory_timeline, save_memory

router = APIRouter()


@router.post("/get")
def session_memory_get(req: SessionMemoryGetRequest):
    return get_session_memory(req.workspace_path, req.client_path)


@router.post("/save")
def session_memory_save(req: SessionMemorySaveRequest):
    save_memory(req.workspace_path, req.section, req.content)
    return {"ok": True}


@router.post("/timeline")
def session_memory_timeline(req: MemoryTimelineRequest):
    return list_memory_timeline(req.workspace_path, req.client_path, req.days)
