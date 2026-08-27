"""Sessions router — discover past Claude sessions."""

import dataclasses

from fastapi import APIRouter

from sidecar.schemas import DiscoverSessionsRequest, ListRecentSessionsRequest, DeleteSessionRequest
from sidecar.services.claude_service import list_recent_sessions
from sidecar.services.session_history import discover_sessions, delete_session, load_session_messages

router = APIRouter()


@router.post("/discover")
def discover(req: DiscoverSessionsRequest):
    entries = discover_sessions(req.workspace_path, req.limit)
    return [dataclasses.asdict(e) for e in entries]


@router.post("/recent")
def recent(req: ListRecentSessionsRequest):
    return list_recent_sessions(req.workspace_path, req.limit)


@router.post("/load-messages")
def load_messages(req: DeleteSessionRequest):
    """Load messages from a session JSONL file."""
    messages = load_session_messages(req.workspace_path, req.session_id)
    return messages


@router.post("/delete")
def delete(req: DeleteSessionRequest):
    deleted = delete_session(req.workspace_path, req.session_id)
    return {"ok": deleted}
