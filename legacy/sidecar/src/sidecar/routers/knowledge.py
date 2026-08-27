"""Knowledge router — cross-project knowledge graph."""

from fastapi import APIRouter

from sidecar.schemas import (
    KnowledgeContributeRequest,
    KnowledgeDiscoverRequest,
    KnowledgeGraphRequest,
    KnowledgeLinkRequest,
    KnowledgePromoteRequest,
    KnowledgePullRequest,
)
from sidecar.services.knowledge_service import (
    contribute_knowledge,
    discover_knowledge,
    get_graph,
    link_knowledge,
    promote_knowledge,
    pull_knowledge,
)

router = APIRouter()


@router.post("/discover")
def knowledge_discover(req: KnowledgeDiscoverRequest):
    entries = discover_knowledge(req.query, req.tags, req.type_filter, req.workspace_path, req.client_path)
    return [e.model_dump() for e in entries]


@router.post("/pull")
def knowledge_pull(req: KnowledgePullRequest):
    entry = pull_knowledge(req.id, req.workspace_path, req.client_path)
    if entry is None:
        return {"error": "Entry not found"}
    return entry.model_dump()


@router.post("/contribute")
def knowledge_contribute(req: KnowledgeContributeRequest):
    entry = contribute_knowledge(req.title, req.content, req.tags, req.type, req.workspace_path, req.client_path)
    return entry.model_dump()


@router.post("/promote")
def knowledge_promote(req: KnowledgePromoteRequest):
    msg = promote_knowledge(req.id, req.from_level, req.to_level, req.workspace_path, req.client_path)
    return {"message": msg}


@router.post("/link")
def knowledge_link(req: KnowledgeLinkRequest):
    msg = link_knowledge(req.source_id, req.target_id, req.workspace_path, req.client_path)
    return {"message": msg}


@router.post("/graph")
def knowledge_graph(req: KnowledgeGraphRequest):
    return get_graph(req.workspace_path, req.client_path)
