"""Health check router — workspace health diagnostics."""

from __future__ import annotations

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.services.health import check_workspace_health

router = APIRouter()


class HealthRequest(BaseModel):
    workspace_path: str


@router.post("/workspace")
def workspace_health(req: HealthRequest):
    """Run health checks on a workspace directory."""
    result = check_workspace_health(req.workspace_path)
    return result.model_dump()
