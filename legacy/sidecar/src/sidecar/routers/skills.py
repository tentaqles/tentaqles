"""Skills router — hierarchical skill management."""

from fastapi import APIRouter

from sidecar.schemas import (
    PackInstallRequest,
    SkillGetRequest,
    SkillInstallRequest,
    SkillPromoteRequest,
    SkillRemoveRequest,
    SkillsDiscoverRequest,
)
from sidecar.services.skills_service import (
    discover_skills,
    get_skill,
    get_skill_context,
    install_pack,
    install_skill,
    list_packs,
    promote_skill,
    remove_skill,
)

router = APIRouter()


@router.post("/discover")
def skills_discover(req: SkillsDiscoverRequest):
    """Discover all skills with hierarchy resolution."""
    skills = discover_skills(req.workspace_path, req.client_path)
    return [s.model_dump() for s in skills]


@router.post("/get")
def skills_get(req: SkillGetRequest):
    """Get a single skill by name."""
    result = get_skill(req.workspace_path, req.skill_name, req.client_path)
    if result is None:
        return {"error": "Skill not found"}
    return result.model_dump()


@router.post("/context")
def skills_context(req: SkillGetRequest):
    """Get full skill content for invocation."""
    result = get_skill_context(req.workspace_path, req.skill_name, req.client_path)
    if result is None:
        return {"error": "Skill not found"}
    return result


@router.post("/install")
def skills_install(req: SkillInstallRequest):
    """Install a skill at a specific level."""
    msg = install_skill(req.level, req.skill_name, req.source_path, req.workspace_path, req.client_path)
    return {"message": msg}


@router.post("/remove")
def skills_remove(req: SkillRemoveRequest):
    """Remove a skill from a specific level."""
    msg = remove_skill(req.level, req.skill_name, req.workspace_path, req.client_path)
    return {"message": msg}


@router.post("/promote")
def skills_promote(req: SkillPromoteRequest):
    """Promote a skill from one level to a higher one."""
    msg = promote_skill(req.skill_name, req.from_level, req.to_level, req.workspace_path, req.client_path)
    return {"message": msg}


@router.post("/packs/list")
def packs_list():
    """List available skill packs."""
    packs = list_packs()
    return [p.model_dump() for p in packs]


@router.post("/packs/install")
def packs_install(req: PackInstallRequest):
    """Install all skills from a pack."""
    results = install_pack(req.pack_name, req.level, req.workspace_path, req.client_path)
    return {"results": results}
