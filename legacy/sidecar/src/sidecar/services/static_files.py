"""Static file serving for production mode.

When Next.js is built with `output: 'export'`, the output is placed in
out/. This module mounts those files so FastAPI can serve both the API
and the UI from a single process (web-only mode without Electron).
"""

from __future__ import annotations

import logging
from pathlib import Path

from fastapi import FastAPI
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles

logger = logging.getLogger(__name__)

# Static files directory — Next.js static export output.
# In a PyInstaller bundle, this would be bundled alongside the sidecar.
_STATIC_DIR = Path(__file__).parent.parent / "static"


def mount_static_files(app: FastAPI) -> None:
    """Mount Next.js static export if the directory exists.

    In dev mode, the static dir won't exist (Next.js dev server serves the frontend).
    In production web mode, it contains the full Next.js static export.
    In Electron mode, Electron loads the files directly via file:// protocol.
    """
    if not _STATIC_DIR.is_dir():
        logger.info("No static/ directory found — running in API-only mode (dev)")
        return

    logger.info("Serving static UI from %s", _STATIC_DIR)

    # Catch-all for SPA: any non-API, non-file route returns index.html
    @app.get("/{path:path}")
    async def spa_catch_all(path: str):
        # If the file exists in static dir, serve it directly
        file_path = _STATIC_DIR / path
        if file_path.is_file():
            return FileResponse(file_path)
        # Otherwise serve index.html for SPA client-side routing
        index = _STATIC_DIR / "index.html"
        if index.exists():
            return FileResponse(index)
        return {"error": "not found"}

    # Mount static assets (JS, CSS, images) — must come after catch-all
    app.mount("/", StaticFiles(directory=str(_STATIC_DIR), html=True), name="static")
