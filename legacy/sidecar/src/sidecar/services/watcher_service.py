"""Filesystem watcher service — monitors base_path for directory changes."""

from __future__ import annotations

import asyncio
import json
import logging
import time
from pathlib import Path

from watchfiles import Change, awatch

logger = logging.getLogger(__name__)

_DEBOUNCE_SECONDS = 1.0


class FileWatcherService:
    """Singleton that watches a base_path and fans out SSE events to subscribers."""

    def __init__(self) -> None:
        self._base_path: str | None = None
        self._stop_event: asyncio.Event = asyncio.Event()
        self._task: asyncio.Task | None = None
        self._subscribers: list[asyncio.Queue[dict | None]] = []
        self._lock = asyncio.Lock()
        self._last_emit_time: float = 0.0

    @property
    def is_watching(self) -> bool:
        return self._task is not None and not self._task.done()

    @property
    def base_path(self) -> str | None:
        return self._base_path

    async def start(self, base_path: str) -> None:
        """Start watching a base_path. Stops any existing watcher first."""
        async with self._lock:
            await self._stop_internal()
            self._base_path = base_path
            self._stop_event = asyncio.Event()
            self._task = asyncio.create_task(self._watch_loop())
            logger.info("Watcher started for %s", base_path)

    async def stop(self) -> None:
        """Stop the watcher gracefully."""
        async with self._lock:
            await self._stop_internal()

    async def _stop_internal(self) -> None:
        if self._task and not self._task.done():
            self._stop_event.set()
            try:
                await asyncio.wait_for(self._task, timeout=5.0)
            except asyncio.TimeoutError:
                self._task.cancel()
            self._task = None
            logger.info("Watcher stopped")

    def subscribe(self) -> asyncio.Queue[dict | None]:
        """Create a new subscriber queue."""
        queue: asyncio.Queue[dict | None] = asyncio.Queue(maxsize=64)
        self._subscribers.append(queue)
        return queue

    def unsubscribe(self, queue: asyncio.Queue) -> None:
        """Remove a subscriber queue."""
        try:
            self._subscribers.remove(queue)
        except ValueError:
            pass

    async def _broadcast(self, event: dict) -> None:
        """Send event to all subscriber queues."""
        dead: list[asyncio.Queue] = []
        for q in self._subscribers:
            try:
                q.put_nowait(event)
            except asyncio.QueueFull:
                dead.append(q)
        for q in dead:
            self._subscribers.remove(q)

    async def _watch_loop(self) -> None:
        """Core watch loop using watchfiles.awatch."""
        base = Path(self._base_path)
        if not base.is_dir():
            logger.warning("Watch path does not exist: %s", self._base_path)
            return

        try:
            async for changes in awatch(
                base,
                stop_event=self._stop_event,
                recursive=True,
                step=500,
                rust_timeout=5000,
            ):
                relevant = self._filter_changes(base, changes)
                if not relevant:
                    continue

                # Auto-create .workspace-profile.json for new client folders
                for change in relevant:
                    if change["depth"] == 1 and change["change"] == "added":
                        self._ensure_workspace_profile(Path(change["path"]))

                now = time.monotonic()
                if now - self._last_emit_time < _DEBOUNCE_SECONDS:
                    continue
                self._last_emit_time = now

                event = {
                    "type": "hierarchy_changed",
                    "changes": relevant,
                    "base_path": str(base),
                    "timestamp": time.time(),
                }
                await self._broadcast(event)
                logger.debug("Broadcast hierarchy_changed: %d changes", len(relevant))

        except asyncio.CancelledError:
            pass
        except Exception:
            logger.exception("Watcher loop error")

    def _filter_changes(self, base: Path, changes: set[tuple[Change, str]]) -> list[dict]:
        """Keep only directory creations/deletions at depth 1 (client) or 2 (project)."""
        results = []
        for change_type, path_str in changes:
            if change_type == Change.modified:
                continue

            path = Path(path_str)
            try:
                rel = path.relative_to(base)
            except ValueError:
                continue

            depth = len(rel.parts)
            if depth not in (1, 2):
                continue

            # For existing paths, check if it's a directory
            if path.exists() and not path.is_dir():
                continue
            # For deleted paths, skip if it looks like a file (has extension)
            if not path.exists() and "." in path.name:
                continue

            results.append(
                {
                    "change": change_type.name.lower(),
                    "path": path_str,
                    "name": path.name,
                    "depth": depth,
                }
            )

        return results

    @staticmethod
    def _ensure_workspace_profile(directory: Path) -> None:
        """Create a minimal .workspace-profile.json if one doesn't exist."""
        profile_path = directory / ".workspace-profile.json"
        if profile_path.exists():
            return
        if not directory.is_dir():
            return

        profile = {
            "$schema": "workspace-profile-v1",
            "client_name": directory.name,
            "client_description": "",
            "git": {"platform": "github", "host": "github.com", "account": None},
            "cloud": {"provider": "none", "subscription_name": None, "subscription_id": None},
            "database": {"type": "none", "dialect": None, "connection_details": None},
            "language": "en",
            "tech_stack": [],
            "special_rules": [],
        }
        try:
            profile_path.write_text(
                json.dumps(profile, indent=2, ensure_ascii=False) + "\n",
                encoding="utf-8",
            )
            logger.info("Created workspace profile for %s", directory.name)
        except OSError:
            logger.warning("Failed to create workspace profile for %s", directory.name)


# Module-level singleton
watcher = FileWatcherService()
