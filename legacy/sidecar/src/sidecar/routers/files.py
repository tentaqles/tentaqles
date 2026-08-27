"""File browser router — workspace file listing for @-mentions."""

from pathlib import Path

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.schemas import FileBrowseRequest
from sidecar.services.file_browser import list_workspace_files

router = APIRouter()


@router.post("/browse")
def browse(req: FileBrowseRequest):
    return list_workspace_files(req.workspace_path, req.query, req.max_results)


class FileReadRequest(BaseModel):
    file_path: str


@router.post("/read")
def read_file(req: FileReadRequest):
    """Read a file's text content. Returns the content as a string."""
    path = Path(req.file_path)
    if not path.exists():
        return {"ok": False, "error": "File not found", "content": ""}
    if not path.is_file():
        return {"ok": False, "error": "Not a file", "content": ""}

    # Safety: limit to 1MB
    size = path.stat().st_size
    if size > 1_048_576:
        return {"ok": False, "error": f"File too large ({size} bytes, max 1MB)", "content": ""}

    try:
        content = path.read_text(encoding="utf-8", errors="replace")
        return {"ok": True, "content": content}
    except Exception as e:
        return {"ok": False, "error": str(e), "content": ""}
