"""Personality router — soul + user profile management."""

from fastapi import APIRouter

from sidecar.schemas import (
    PersonalityGetRequest,
    PersonalityOverridesRequest,
    PersonalitySaveRequest,
)
from sidecar.services.personality_service import (
    get_personality,
    list_personality_overrides,
    save_personality,
)

router = APIRouter()


@router.post("/get")
def personality_get(req: PersonalityGetRequest):
    result = get_personality(req.workspace_path, req.client_path)
    return result.model_dump()


@router.post("/save")
def personality_save(req: PersonalitySaveRequest):
    save_personality(req.file_type, req.level, req.content, req.workspace_path, req.client_path)
    return {"ok": True}


@router.post("/list-overrides")
def personality_list_overrides(req: PersonalityOverridesRequest):
    return list_personality_overrides(req.workspace_path, req.client_path)
