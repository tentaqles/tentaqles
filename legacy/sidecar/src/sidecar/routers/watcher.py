"""Filesystem watcher router — SSE stream of hierarchy changes."""

from __future__ import annotations

import asyncio
import json

from fastapi import APIRouter, Query
from sse_starlette.sse import EventSourceResponse

from sidecar.services.watcher_service import watcher

router = APIRouter()


@router.get("/stream")
async def watch_stream(base_path: str = Query(...)):
    """SSE endpoint: streams hierarchy_changed events for the given base_path."""
    if not watcher.is_watching or watcher.base_path != base_path:
        await watcher.start(base_path)

    queue = watcher.subscribe()

    async def event_generator():
        try:
            yield {
                "event": "connected",
                "data": json.dumps({"type": "connected", "base_path": base_path}),
            }

            while True:
                try:
                    event = await asyncio.wait_for(queue.get(), timeout=30.0)
                except asyncio.TimeoutError:
                    yield {"event": "keepalive", "data": ""}
                    continue

                if event is None:
                    break

                yield {
                    "event": event.get("type", "message"),
                    "data": json.dumps(event),
                }
        finally:
            watcher.unsubscribe(queue)

    return EventSourceResponse(event_generator())


@router.get("/status")
async def watcher_status():
    """Check watcher status."""
    return {
        "watching": watcher.is_watching,
        "base_path": watcher.base_path,
    }
