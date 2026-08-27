"""Knowledge graph router — workspace file scanning and file content reading."""

from __future__ import annotations

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

from sidecar.services.knowledge_graph_service import read_file_content, scan_workspace

router = APIRouter()


# ------------------------------------------------------------------
# Request schemas
# ------------------------------------------------------------------


class ScanRequest(BaseModel):
    client_path: str


class FileRequest(BaseModel):
    client_path: str
    file_path: str


# ------------------------------------------------------------------
# Endpoints
# ------------------------------------------------------------------


@router.post("/scan")
def graph_scan(req: ScanRequest):
    """Scan a workspace and return the knowledge graph (nodes + edges)."""
    return scan_workspace(req.client_path)


@router.post("/file")
def graph_file(req: FileRequest):
    """Read a single file's content for the graph detail panel."""
    try:
        return read_file_content(req.client_path, req.file_path)
    except FileNotFoundError as e:
        raise HTTPException(status_code=404, detail=str(e)) from None
    except ValueError as e:
        raise HTTPException(status_code=400, detail=str(e)) from None
