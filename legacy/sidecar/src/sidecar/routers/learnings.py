"""Learnings router — append-only feedback journal."""

from fastapi import APIRouter

from sidecar.schemas import LearningsAppendRequest, LearningsGetRequest, LearningsPromoteRequest
from sidecar.services.learnings_service import append_learning, get_learnings, promote_learning

router = APIRouter()


@router.post("/get")
def learnings_get(req: LearningsGetRequest):
    return get_learnings(req.workspace_path, req.client_path, req.skill_name)


@router.post("/append")
def learnings_append(req: LearningsAppendRequest):
    append_learning(req.workspace_path, req.skill_name, req.entry)
    return {"ok": True}


@router.post("/promote")
def learnings_promote(req: LearningsPromoteRequest):
    promote_learning(req.workspace_path, req.client_path, req.skill_name, req.entry_text, req.to_level)
    return {"ok": True}
