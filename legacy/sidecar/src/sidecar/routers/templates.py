"""Templates router — client creation from template."""

import dataclasses

from fastapi import APIRouter

from sidecar.schemas import CreateClientRequest, CreateProjectRequest, PreviewClientRequest
from sidecar.services.template_service import create_client, preview_new_client

router = APIRouter()


@router.post("/preview")
def preview(req: PreviewClientRequest):
    result = preview_new_client(req.base_path, req.client_name, req.values)
    return dataclasses.asdict(result)


@router.post("/create")
def create(req: CreateClientRequest):
    path = create_client(req.base_path, req.client_name, req.values)
    return {"path": path}


@router.post("/create-project")
def templates_create_project(req: CreateProjectRequest):
    from sidecar.services.template_service import create_project

    try:
        path = create_project(
            req.client_path,
            req.project_name,
            req.description,
            req.goal,
            req.deliverables,
            req.tech_stack,
        )
        return {"ok": True, "path": path}
    except FileExistsError as e:
        return {"ok": False, "error": str(e)}
    except Exception as e:
        return {"ok": False, "error": str(e)}
