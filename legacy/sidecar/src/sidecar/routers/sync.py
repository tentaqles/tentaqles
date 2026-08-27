"""Sync router — bidirectional task sync with Asana, GitHub Projects, Azure DevOps."""

from __future__ import annotations

from fastapi import APIRouter
from pydantic import BaseModel, Field

from sidecar.services.sync.engine import engine

router = APIRouter()


# ------------------------------------------------------------------
# Request schemas
# ------------------------------------------------------------------


class SyncPullRequest(BaseModel):
    client_slug: str


class SyncPushRequest(BaseModel):
    client_slug: str


class ConflictResolveRequest(BaseModel):
    conflict_id: str
    resolution: str  # 'local' | 'remote' | 'merged'


class ProviderConfigureRequest(BaseModel):
    client_slug: str
    provider_type: str  # 'asana' | 'github' | 'azuredevops' | 'trello'
    config: dict = Field(default_factory=dict)
    credential: str = ""
    poll_interval: int = 60
    sync_direction: str = "bidirectional"  # 'pull_only' | 'push_only' | 'bidirectional'


class EnqueuePushRequest(BaseModel):
    client_slug: str
    remote_id: str
    action: str  # 'status' | 'update'
    payload: dict = Field(default_factory=dict)


class TaskDetailsRequest(BaseModel):
    client_slug: str
    remote_id: str


class FetchColumnsRequest(BaseModel):
    provider_type: str  # 'asana' | 'github' | 'azuredevops' | 'trello'
    config: dict = Field(default_factory=dict)
    credential: str = ""


# ------------------------------------------------------------------
# Endpoints
# ------------------------------------------------------------------


@router.get("/status")
def sync_status():
    """Get sync status for all configured clients."""
    return engine.get_status()


@router.get("/provider/{slug}")
def sync_get_provider(slug: str):
    """Get a saved provider's config (without credential)."""
    row = engine._db.get_provider(slug)
    if not row:
        return {"ok": False, "error": f"No provider configured for '{slug}'"}
    import json
    config = json.loads(row["config"]) if isinstance(row["config"], str) else row["config"]
    return {
        "ok": True,
        "slug": row["slug"],
        "type": row["type"],
        "config": config,
        "poll_interval": row.get("poll_interval", 60),
        "sync_direction": row.get("sync_direction", "bidirectional"),
        "last_synced": row.get("last_synced"),
        "enabled": bool(row.get("enabled", 1)),
    }


@router.get("/tasks")
def sync_tasks(client: str | None = None):
    """Get all synced tasks, optionally filtered by client slug."""
    if client:
        return engine.get_tasks_for_client(client)
    return engine.get_all_tasks()


@router.get("/conflicts")
def sync_conflicts():
    """Get unresolved conflicts."""
    return engine.get_conflicts()


@router.post("/pull")
async def sync_pull(req: SyncPullRequest):
    """Trigger a pull sync for one client, or all if slug is 'all'."""
    if req.client_slug == "all":
        await engine.run_all_syncs()
        return {"ok": True, "synced": "all"}
    result = await engine.sync_client(req.client_slug)
    return result


@router.post("/push")
async def sync_push(req: SyncPushRequest):
    """Process the push queue for a client (or all if the queue has items)."""
    await engine.process_push_queue()
    return {"ok": True}


@router.post("/resolve")
def sync_resolve(req: ConflictResolveRequest):
    """Resolve a sync conflict."""
    engine.resolve_conflict(req.conflict_id, req.resolution)
    return {"ok": True}


@router.post("/configure")
def sync_configure(req: ProviderConfigureRequest):
    """Configure a sync provider for a client."""
    result = engine.configure_provider(
        slug=req.client_slug,
        provider_type=req.provider_type,
        config=req.config,
        credential=req.credential,
        poll_interval=req.poll_interval,
        sync_direction=req.sync_direction,
    )
    return result


@router.post("/fetch-columns")
async def sync_fetch_columns(req: FetchColumnsRequest):
    """Fetch remote columns/statuses from a provider (for column mapping UI)."""
    from sidecar.services.sync.engine import _build_provider

    try:
        provider = _build_provider(req.provider_type, req.config, req.credential)
        columns = await provider.fetch_columns()
        return {
            "ok": True,
            "columns": [{"id": c.id, "name": c.name} for c in columns],
        }
    except Exception as exc:
        return {"ok": False, "error": str(exc), "columns": []}


@router.post("/enqueue-push")
def sync_enqueue_push(req: EnqueuePushRequest):
    """Queue a push to the remote provider (e.g., status change from card move)."""
    push_id = engine._db.enqueue_push(req.client_slug, req.remote_id, req.action, req.payload)
    return {"ok": True, "push_id": push_id}


@router.post("/task-details")
async def sync_task_details(req: TaskDetailsRequest):
    """Fetch rich details for a synced task (subtasks, comments, attachments)."""
    provider = engine._providers.get(req.client_slug)
    if not provider:
        return {"ok": False, "error": f"No provider for '{req.client_slug}'"}
    try:
        details = await provider.fetch_task_details(req.remote_id)
        return {"ok": True, **details}
    except Exception as exc:
        return {"ok": False, "error": str(exc)}
