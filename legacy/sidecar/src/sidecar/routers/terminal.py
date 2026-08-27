"""Terminal router — WebSocket PTY bridge."""

from __future__ import annotations

import asyncio
import uuid

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from sidecar.schemas import TerminalCreateRequest, TerminalDestroyRequest
from sidecar.services.terminal_service import TerminalSession, create_session, destroy_session

router = APIRouter()

_terminal_sessions: dict[str, TerminalSession] = {}


@router.post("/create")
def create_terminal(req: TerminalCreateRequest):
    session = create_session(
        req.workspace_path,
        launch_command=req.launch_command,
        shell=req.shell,
        rows=req.rows,
        cols=req.cols,
    )
    session_id = uuid.uuid4().hex[:12]
    _terminal_sessions[session_id] = session
    return {"session_id": session_id, "label": session.label}


@router.post("/destroy")
def destroy_terminal(req: TerminalDestroyRequest):
    session = _terminal_sessions.pop(req.session_id, None)
    if session:
        destroy_session(session)
        return {"ok": True}
    return {"ok": False}


@router.websocket("/ws/{session_id}")
async def terminal_ws(websocket: WebSocket, session_id: str):
    await websocket.accept()
    session = _terminal_sessions.get(session_id)
    if not session:
        await websocket.close(code=4004, reason="Session not found")
        return

    async def read_loop():
        loop = asyncio.get_event_loop()
        while session.is_alive():
            try:
                data = await loop.run_in_executor(None, session.read, 4096)
                if data:
                    await websocket.send_text(data)
            except EOFError:
                break
            except Exception:
                break

    read_task = asyncio.create_task(read_loop())

    try:
        while True:
            data = await websocket.receive_text()
            session.write(data)
    except WebSocketDisconnect:
        pass
    finally:
        read_task.cancel()
