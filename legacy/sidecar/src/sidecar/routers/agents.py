"""Agents router — project-scoped agent CRUD."""

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.schemas import WorkspaceRequest
from sidecar.services.agents_service import (
    delete_agent,
    get_agent,
    list_agents,
    save_agent,
)

router = APIRouter()


class AgentFileRequest(WorkspaceRequest):
    filename: str


class AgentSaveRequest(AgentFileRequest):
    content: str


@router.post("/list")
def agents_list(req: WorkspaceRequest):
    return list_agents(req.workspace_path)


@router.post("/get")
def agents_get(req: AgentFileRequest):
    result = get_agent(req.workspace_path, req.filename)
    if result is None:
        return {"error": "Agent not found"}
    return result


@router.post("/save")
def agents_save(req: AgentSaveRequest):
    save_agent(req.workspace_path, req.filename, req.content)
    return {"ok": True}


@router.post("/delete")
def agents_delete(req: AgentFileRequest):
    return {"deleted": delete_agent(req.workspace_path, req.filename)}
