"""CLAUDE.md generation router."""

from fastapi import APIRouter

from sidecar.schemas import ClaudeMdGenerateRequest
from sidecar.services.claudemd_generator import generate_and_save, generate_claude_md

router = APIRouter()


@router.post("/generate")
def claudemd_generate(req: ClaudeMdGenerateRequest):
    content = generate_claude_md(req.workspace_path, req.client_path)
    return {"content": content}


@router.post("/generate-and-save")
def claudemd_generate_and_save(req: ClaudeMdGenerateRequest):
    content = generate_and_save(req.workspace_path, req.client_path)
    return {"content": content, "saved_to": f"{req.workspace_path}/CLAUDE.md"}
