"""Bundle router — export and import workspace configuration bundles."""

from __future__ import annotations

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.services.workspace_bundle import export_workspace, import_workspace

router = APIRouter()


class ExportRequest(BaseModel):
    client_path: str
    include_secrets: bool = False


class ImportRequest(BaseModel):
    bundle_path: str
    target_path: str
    merge: bool = False


@router.post("/export")
def export(req: ExportRequest):
    """Export a workspace configuration as a portable bundle."""
    return export_workspace(req.client_path, req.include_secrets)


@router.post("/import")
def import_bundle(req: ImportRequest):
    """Import a workspace bundle into a target directory."""
    result = import_workspace(req.bundle_path, req.target_path, req.merge)
    return result.model_dump()
