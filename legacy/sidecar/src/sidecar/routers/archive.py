"""Archive router — archive and restore projects."""

from fastapi import APIRouter

from sidecar.schemas import ArchiveProjectRequest, ListArchivedRequest, RestoreProjectRequest
from sidecar.services.archive_service import archive_project, list_archived, restore_project

router = APIRouter()


@router.post("/archive")
def do_archive(req: ArchiveProjectRequest):
    return archive_project(req.project_path, req.client_path)


@router.post("/restore")
def do_restore(req: RestoreProjectRequest):
    return restore_project(req.project_path, req.client_path)


@router.post("/list")
def do_list(req: ListArchivedRequest):
    return list_archived(req.client_path)
