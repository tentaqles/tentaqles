"""Reconciliation and health check router."""

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.services.reconciliation_service import reconcile_workspace
from sidecar.services.service_registry import get_service_registry, get_services_for_skill

router = APIRouter()


class ReconcileRequest(BaseModel):
    workspace_path: str
    client_path: str | None = None


class SkillServicesRequest(BaseModel):
    skill_name: str
    workspace_path: str | None = None


@router.post("/check")
def reconciliation_check(req: ReconcileRequest):
    return reconcile_workspace(req.workspace_path, req.client_path)


@router.post("/services")
def service_registry_list(req: ReconcileRequest):
    return get_service_registry(req.workspace_path)


@router.post("/services-for-skill")
def services_for_skill(req: SkillServicesRequest):
    return get_services_for_skill(req.skill_name, req.workspace_path)
