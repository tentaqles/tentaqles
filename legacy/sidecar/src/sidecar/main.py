"""FastAPI sidecar — Python backend for Workspace Manager v2."""

from __future__ import annotations

import argparse
import os
import platform
import sys

# WMI deadlock workaround: monkey-patch before any library calls platform.system().
# On this Windows machine, WMI queries hang indefinitely, freezing aiohttp imports.
platform.system = lambda: "Windows"
platform.uname = lambda: platform.uname_result(
    system="Windows", node="", release="10", version="10.0.26200", machine="AMD64"
)

# Strip Claude Code env vars to prevent nested session detection.
for _k in list(os.environ):
    if _k.startswith("CLAUDE"):
        del os.environ[_k]
os.environ.pop("VIRTUAL_ENV", None)

import asyncio
import logging
from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from sidecar.routers import (
    activation,
    agents,
    analytics,
    archive,
    boards,
    brand_context,
    bundle,
    chat,
    claude_settings,
    claudemd,
    commands,
    config,
    diffs,
    files,
    health_check,
    hierarchy,
    hooks,
    knowledge,
    knowledge_graph,
    learnings,
    memory,
    onboarding,
    patterns,
    permissions,
    personality,
    profiles,
    propagation,
    promotion,
    reconciliation,
    scheduler,
    session_memory,
    sessions,
    settings,
    skills,
    sync,
    tasks,
    templates,
    terminal,
    toggles,
    notifications,
    watcher,
)

from sidecar.services.watcher_service import watcher as watcher_service
from sidecar.services.sync.engine import engine as sync_engine

logger = logging.getLogger(__name__)

# How long to wait (seconds) with no frontend clients before auto-exiting.
_IDLE_SHUTDOWN_GRACE = 60
# How long to wait at startup before monitoring begins (let frontend connect).
_STARTUP_GRACE = 30


async def _auto_shutdown_monitor() -> None:
    """Exit the sidecar when no frontend SSE clients are connected."""
    import time

    await asyncio.sleep(_STARTUP_GRACE)
    idle_since: float | None = None

    while True:
        await asyncio.sleep(10)
        has_clients = len(watcher_service._subscribers) > 0

        if has_clients:
            idle_since = None
            continue

        if idle_since is None:
            idle_since = time.monotonic()
            logger.info("No frontend clients connected, starting idle timer")
            continue

        elapsed = time.monotonic() - idle_since
        if elapsed >= _IDLE_SHUTDOWN_GRACE:
            logger.info("No clients for %ds, shutting down", int(elapsed))
            os._exit(0)


@asynccontextmanager
async def lifespan(app: FastAPI):
    shutdown_task = asyncio.create_task(_auto_shutdown_monitor())
    try:
        sync_engine.start()
    except Exception as e:
        logger.warning("Sync engine failed to start: %s", e)
    yield
    shutdown_task.cancel()
    sync_engine.stop()
    await watcher_service.stop()


app = FastAPI(title="Workspace Manager Sidecar", version="0.1.0", lifespan=lifespan)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(hierarchy.router, prefix="/api/hierarchy", tags=["hierarchy"])
app.include_router(config.router, prefix="/api/config", tags=["config"])
app.include_router(chat.router, prefix="/api/chat", tags=["chat"])
app.include_router(terminal.router, prefix="/api/terminal", tags=["terminal"])
app.include_router(propagation.router, prefix="/api/propagation", tags=["propagation"])
app.include_router(promotion.router, prefix="/api/promotion", tags=["promotion"])
app.include_router(toggles.router, prefix="/api/toggles", tags=["toggles"])
app.include_router(files.router, prefix="/api/files", tags=["files"])
app.include_router(templates.router, prefix="/api/templates", tags=["templates"])
app.include_router(sessions.router, prefix="/api/sessions", tags=["sessions"])
app.include_router(settings.router, prefix="/api/settings", tags=["settings"])
app.include_router(patterns.router, prefix="/api/patterns", tags=["patterns"])
app.include_router(analytics.router, prefix="/api/analytics", tags=["analytics"])
app.include_router(profiles.router, prefix="/api/profiles", tags=["profiles"])
app.include_router(memory.router, prefix="/api/memory", tags=["memory"])
app.include_router(commands.router, prefix="/api/commands", tags=["commands"])
app.include_router(hooks.router, prefix="/api/hooks", tags=["hooks"])
app.include_router(permissions.router, prefix="/api/permissions", tags=["permissions"])
app.include_router(agents.router, prefix="/api/agents", tags=["agents"])
app.include_router(claude_settings.router, prefix="/api/claude-settings", tags=["claude-settings"])
app.include_router(skills.router, prefix="/api/skills", tags=["skills"])
app.include_router(watcher.router, prefix="/api/watcher", tags=["watcher"])
app.include_router(knowledge.router, prefix="/api/knowledge", tags=["knowledge"])
app.include_router(knowledge_graph.router, prefix="/api/knowledge-graph", tags=["knowledge-graph"])
app.include_router(personality.router, prefix="/api/personality", tags=["personality"])
app.include_router(claudemd.router, prefix="/api/claudemd", tags=["claudemd"])
app.include_router(session_memory.router, prefix="/api/session-memory", tags=["session-memory"])
app.include_router(learnings.router, prefix="/api/learnings", tags=["learnings"])
app.include_router(scheduler.router, prefix="/api/scheduler", tags=["scheduler"])
app.include_router(onboarding.router, prefix="/api/onboarding", tags=["onboarding"])
app.include_router(brand_context.router, prefix="/api/brand-context", tags=["brand-context"])
app.include_router(reconciliation.router, prefix="/api/reconciliation", tags=["reconciliation"])
app.include_router(activation.router, prefix="/api/activation", tags=["activation"])
app.include_router(health_check.router, prefix="/api/health-check", tags=["health-check"])
app.include_router(bundle.router, prefix="/api/bundle", tags=["bundle"])
app.include_router(boards.router, prefix="/api/boards", tags=["boards"])
app.include_router(tasks.router, prefix="/api/tasks", tags=["tasks"])
app.include_router(diffs.router, prefix="/api/diffs", tags=["diffs"])
app.include_router(sync.router, prefix="/api/sync", tags=["sync"])
app.include_router(archive.router, prefix="/api/archive", tags=["archive"])
app.include_router(notifications.router, prefix="/api/notifications", tags=["notifications"])


@app.get("/api/health")
def health():
    return {"status": "ok"}


def _kill_port_holder(port: int) -> None:
    """Kill any existing process holding the given port (Windows only)."""
    import subprocess

    try:
        result = subprocess.run(
            ["netstat", "-ano"],
            capture_output=True,
            text=True,
            timeout=5,
        )
        for line in result.stdout.splitlines():
            if f"127.0.0.1:{port}" in line and "LISTENING" in line:
                pid = line.strip().split()[-1]
                if pid.isdigit() and int(pid) != os.getpid():
                    subprocess.run(
                        ["taskkill", "/F", "/PID", pid],
                        capture_output=True,
                        timeout=5,
                    )
                    print(f"Killed zombie process on port {port} (PID {pid})")
                    return
    except Exception as exc:
        print(f"Port cleanup warning: {exc}")


def main():
    parser = argparse.ArgumentParser(description="Workspace Manager sidecar")
    parser.add_argument("--port", type=int, default=9120)
    parser.add_argument("--host", default="127.0.0.1")
    args = parser.parse_args()

    _kill_port_holder(args.port)
    uvicorn.run(app, host=args.host, port=args.port, log_level="info")


if __name__ == "__main__":
    main()
