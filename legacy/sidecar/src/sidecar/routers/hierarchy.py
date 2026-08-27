"""Hierarchy scanning router."""

import dataclasses

from fastapi import APIRouter

from sidecar.schemas import ScanRequest
from sidecar.services.hierarchy import scan_hierarchy

router = APIRouter()


@router.post("/scan")
def scan(req: ScanRequest):
    tree = scan_hierarchy(req.base_path)
    return dataclasses.asdict(tree)
