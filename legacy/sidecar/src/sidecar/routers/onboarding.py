"""Onboarding router — first-run setup wizard."""

from fastapi import APIRouter
from pydantic import BaseModel, Field

from sidecar.services.onboarding_service import (
    build_user_md,
    generate_mcp_snippet,
    get_packs_dir,
    get_soul_templates,
    get_suggested_base_path,
    get_tentaqles_path,
    get_user_options,
    install_skill_packs,
    scan_base_path,
    setup_mcp_for_client,
    setup_personality,
)

router = APIRouter()


class ScanRequest(BaseModel):
    base_path: str


class McpSnippetRequest(BaseModel):
    client_path: str


class McpSetupRequest(BaseModel):
    client_path: str


class PersonalitySetupRequest(BaseModel):
    level: str = "global"
    soul_content: str = ""
    user_content: str = ""
    workspace_path: str | None = None
    client_path: str | None = None


class PacksInstallRequest(BaseModel):
    pack_names: list[str]
    level: str = "global"
    workspace_path: str | None = None
    client_path: str | None = None


@router.post("/scan")
def onboarding_scan(req: ScanRequest):
    """Scan base path and discover existing workspaces."""
    return scan_base_path(req.base_path)


@router.post("/mcp-snippet")
def onboarding_mcp_snippet(req: McpSnippetRequest):
    """Generate .mcp.json snippet for a client."""
    tentaqles_path = get_tentaqles_path()
    return {
        "snippet": generate_mcp_snippet(tentaqles_path, req.client_path),
        "tentaqles_path": tentaqles_path,
    }


@router.post("/mcp-setup")
def onboarding_mcp_setup(req: McpSetupRequest):
    """Write .mcp.json to a client directory."""
    tentaqles_path = get_tentaqles_path()
    path = setup_mcp_for_client(req.client_path, tentaqles_path)
    return {"ok": True, "path": path}


@router.post("/personality")
def onboarding_personality(req: PersonalitySetupRequest):
    """Save initial personality files."""
    results = setup_personality(req.level, req.soul_content, req.user_content, req.workspace_path, req.client_path)
    return {"ok": True, "results": results}


@router.post("/install-packs")
def onboarding_install_packs(req: PacksInstallRequest):
    """Install selected skill packs."""
    packs_dir = get_packs_dir()
    results = install_skill_packs(req.pack_names, req.level, req.workspace_path, req.client_path, packs_dir)
    return {"ok": True, "results": results}


@router.get("/defaults")
def onboarding_defaults():
    """Get default templates for personality setup."""
    return {
        "soul_templates": get_soul_templates(),
        "user_options": get_user_options(),
        "tentaqles_path": get_tentaqles_path(),
        "suggested_base_path": get_suggested_base_path(),
    }


class BuildUserRequest(BaseModel):
    name: str = ""
    role: str = ""
    preferences: list[str] = Field(default_factory=list)
    working_styles: list[str] = Field(default_factory=list)
    languages: list[str] = Field(default_factory=list)


@router.post("/build-user-md")
def onboarding_build_user(req: BuildUserRequest):
    """Build user.md from structured form selections."""
    content = build_user_md(req.name, req.role, req.preferences, req.working_styles, req.languages)
    return {"content": content}
