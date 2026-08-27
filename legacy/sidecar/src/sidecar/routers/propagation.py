"""Propagation router — push client configs to projects."""

import dataclasses

from fastapi import APIRouter

from sidecar.schemas import PropagationRequest
from sidecar.services.propagation import preview_propagation, propagate

router = APIRouter()


@router.post("/preview")
def preview(req: PropagationRequest):
    config_types = set(req.config_types) if req.config_types else None
    result = preview_propagation(req.client_path, req.base_path, config_types)
    return dataclasses.asdict(result)


@router.post("/execute")
def execute(req: PropagationRequest):
    config_types = set(req.config_types) if req.config_types else None
    result = propagate(req.client_path, req.base_path, config_types)
    return dataclasses.asdict(result)
