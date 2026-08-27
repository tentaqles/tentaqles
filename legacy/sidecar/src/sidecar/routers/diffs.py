"""Diffs router — git diff and status for ChangesPanel."""

from fastapi import APIRouter

from sidecar.schemas import DiffRequest, DiffStatusRequest
from sidecar.services.diff_service import get_diff, get_status

router = APIRouter()


@router.post("/diff")
def diffs_diff(req: DiffRequest):
    return get_diff(req.workspace_path, req.staged, req.ref)


@router.post("/status")
def diffs_status(req: DiffStatusRequest):
    return get_status(req.workspace_path)
