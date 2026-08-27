"""Chat router — SSE streaming via ClaudeSession."""

from __future__ import annotations

import asyncio
import json

from fastapi import APIRouter
from sse_starlette.sse import EventSourceResponse

from sidecar.schemas import ChatSendRequest, SessionKeyRequest
from sidecar.services.claude_service import AVAILABLE_MODELS, ClaudeSession

router = APIRouter()

# Active sessions keyed by "workspace_path:task_id_or_global:session_id_or_new"
_active_sessions: dict[str, ClaudeSession] = {}


@router.get("/models")
def get_models():
    return AVAILABLE_MODELS


@router.post("/send")
async def send_message(req: ChatSendRequest):
    """Start a chat and stream responses via SSE."""
    session_key = f"{req.workspace_path}:{req.task_id or 'global'}:{req.session_id or 'new'}"

    if session_key not in _active_sessions:
        _active_sessions[session_key] = ClaudeSession(
            workspace_path=req.workspace_path,
            model=req.model,
            resume_session_id=req.session_id,
            permission_mode=req.permission_mode,
        )

    session = _active_sessions[session_key]

    queue: asyncio.Queue = asyncio.Queue()

    async def on_text_delta(text: str):
        await queue.put({"type": "text_delta", "text": text})

    async def on_tool_use(block):
        await queue.put(
            {
                "type": "tool_use",
                "tool_name": block.tool_name,
                "tool_id": block.tool_id,
                "tool_input": block.tool_input,
            }
        )

    async def on_tool_result(block):
        await queue.put(
            {
                "type": "tool_result",
                "tool_result": block.tool_result,
                "is_error": block.is_error,
                "text": block.text,
            }
        )

    async def on_system_init(info):
        await queue.put(
            {
                "type": "system_init",
                "session_id": info.session_id,
                "tools": info.tools,
                "mcp_servers": info.mcp_servers,
            }
        )

    async def on_message_complete(msg):
        await queue.put(
            {
                "type": "message_complete",
                "model": msg.model,
                "cost_usd": msg.cost_usd,
                "duration_ms": msg.duration_ms,
                "input_tokens": msg.input_tokens,
                "output_tokens": msg.output_tokens,
                "cache_read_tokens": msg.cache_read_tokens,
                "cache_creation_tokens": msg.cache_creation_tokens,
                "session_id": msg.session_id,
            }
        )

    async def on_error(err: str):
        await queue.put({"type": "error", "message": err})

    async def run_session():
        try:
            await session.send_message(
                req.prompt,
                on_text_delta=on_text_delta,
                on_tool_use=on_tool_use,
                on_tool_result=on_tool_result,
                on_message_complete=on_message_complete,
                on_system_init=on_system_init,
                on_error=on_error,
            )
        except Exception as e:
            await queue.put({"type": "error", "message": str(e)})
        finally:
            await queue.put(None)  # Always terminate the SSE stream

    asyncio.create_task(run_session())

    async def event_generator():
        while True:
            event = await queue.get()
            if event is None:
                break
            yield {"event": event.get("type", "message"), "data": json.dumps(event)}

    return EventSourceResponse(event_generator())


@router.post("/stop")
def stop_session(req: SessionKeyRequest):
    session = _active_sessions.get(req.session_key)
    if session:
        session.stop()
        return {"ok": True}
    return {"ok": False, "error": "session not found"}


@router.post("/reset")
def reset_session(req: SessionKeyRequest):
    session = _active_sessions.pop(req.session_key, None)
    if session:
        session.reset()
    return {"ok": True}
