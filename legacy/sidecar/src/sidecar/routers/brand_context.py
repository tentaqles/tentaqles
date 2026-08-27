"""Brand context router — voice profile, positioning, ICP, samples, assets."""

from fastapi import APIRouter

from sidecar.schemas import BrandContextGetRequest, BrandContextSaveRequest
from sidecar.services.brand_context_service import (
    get_brand_context,
    list_brand_overrides,
    save_brand_context,
)

router = APIRouter()


@router.post("/get")
def brand_context_get(req: BrandContextGetRequest):
    result = get_brand_context(req.workspace_path, req.client_path)
    return result.model_dump()


@router.post("/save")
def brand_context_save(req: BrandContextSaveRequest):
    path = save_brand_context(req.filename, req.level, req.content, req.workspace_path, req.client_path)
    return {"ok": True, "path": path}


@router.post("/list-overrides")
def brand_context_overrides(req: BrandContextGetRequest):
    return list_brand_overrides(req.workspace_path, req.client_path)
