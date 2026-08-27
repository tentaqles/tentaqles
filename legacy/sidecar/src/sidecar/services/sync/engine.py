"""Sync engine — orchestrates bidirectional task sync with external providers."""

from __future__ import annotations

import asyncio
import json
import logging
from datetime import datetime, timezone

from sidecar.services.sync.database import SyncDatabase
from sidecar.services.sync.providers.asana_provider import AsanaProvider
from sidecar.services.sync.providers.azuredevops_provider import AzureDevOpsProvider
from sidecar.services.sync.providers.base import RemoteTask, TaskProvider
from sidecar.services.sync.providers.github_provider import GitHubProjectsProvider

logger = logging.getLogger(__name__)

# Retry backoff multiplier (seconds) per attempt
_BACKOFF_BASE = 15


class SyncEngine:
    """Main sync orchestrator.

    Polls enabled providers on a schedule (via APScheduler), reconciles
    remote tasks with the local ``task_sync`` table, detects conflicts,
    and processes a push queue for outbound changes.
    """

    def __init__(self, db: SyncDatabase | None = None):
        self._db = db or SyncDatabase()
        self._providers: dict[str, TaskProvider] = {}
        self._scheduler: object | None = None  # APScheduler instance
        self._running = False

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    def start(self) -> None:
        """Initialise the database and start the APScheduler background loop."""
        if self._running:
            return

        self._db.initialize()
        self._rebuild_providers()

        try:
            from apscheduler.schedulers.asyncio import AsyncIOScheduler

            scheduler = AsyncIOScheduler()

            # Per-client sync jobs based on each provider's poll_interval
            for row in self._db.list_providers(enabled_only=True):
                slug = row["slug"]
                interval = row.get("poll_interval", 60) or 60
                scheduler.add_job(
                    self.sync_client,
                    "interval",
                    seconds=interval,
                    id=f"sync_{slug}",
                    replace_existing=True,
                    args=[slug],
                )
                logger.info("Scheduled sync for '%s' every %ds", slug, interval)

            # Push queue processor (shared, fast interval)
            scheduler.add_job(self.process_push_queue, "interval", seconds=5, id="push_queue", replace_existing=True)
            scheduler.start()
            self._scheduler = scheduler
        except ImportError:
            logger.warning("apscheduler not installed — sync engine will only run on-demand")
            self._scheduler = None

        self._running = True
        logger.info("SyncEngine started (providers: %s)", list(self._providers.keys()))

    def _reschedule_client_job(self, slug: str, poll_interval: int) -> None:
        """Add or update the per-client scheduler job."""
        if self._scheduler is None:
            return
        try:
            self._scheduler.add_job(  # type: ignore[union-attr]
                self.sync_client,
                "interval",
                seconds=poll_interval,
                id=f"sync_{slug}",
                replace_existing=True,
                args=[slug],
            )
            logger.info("Rescheduled sync for '%s' every %ds", slug, poll_interval)
        except Exception:
            logger.exception("Failed to reschedule sync job for '%s'", slug)

    def stop(self) -> None:
        """Shut down the scheduler and close the database."""
        if self._scheduler is not None:
            try:
                self._scheduler.shutdown(wait=False)  # type: ignore[union-attr]
            except Exception:
                pass
            self._scheduler = None

        self._db.close()
        self._running = False
        logger.info("SyncEngine stopped")

    # ------------------------------------------------------------------
    # Provider management
    # ------------------------------------------------------------------

    def _rebuild_providers(self) -> None:
        """Build provider instances from the database configuration."""
        self._providers.clear()
        for row in self._db.list_providers(enabled_only=True):
            slug = row["slug"]
            ptype = row["type"]
            config = json.loads(row["config"]) if isinstance(row["config"], str) else row["config"]
            cred = row["credential_ref"]

            try:
                provider = _build_provider(ptype, config, cred)
                self._providers[slug] = provider
            except Exception:
                logger.exception("Failed to build provider for '%s'", slug)

    # ------------------------------------------------------------------
    # Sync: pull
    # ------------------------------------------------------------------

    async def run_all_syncs(self) -> None:
        """Poll every enabled provider in parallel."""
        if not self._providers:
            self._rebuild_providers()
        if not self._providers:
            return

        tasks = [self.sync_client(slug) for slug in list(self._providers.keys())]
        results = await asyncio.gather(*tasks, return_exceptions=True)
        for slug, result in zip(self._providers.keys(), results):
            if isinstance(result, Exception):
                logger.error("Sync failed for '%s': %s", slug, result)

    async def sync_client(self, slug: str) -> dict:
        """Run a sync for one client, respecting sync_direction."""
        provider = self._providers.get(slug)
        if provider is None:
            self._rebuild_providers()
            provider = self._providers.get(slug)
        if provider is None:
            return {"error": f"No provider configured for '{slug}'"}

        prov_row = self._db.get_provider(slug)
        direction = (prov_row or {}).get("sync_direction", "bidirectional")

        stats = {"tasks_fetched": 0, "tasks_created": 0, "tasks_updated": 0, "conflicts": 0}
        log_id = self._db.start_sync_log(slug)

        try:
            # Pull phase — skip if push_only
            if direction != "push_only":
                since: datetime | None = None
                if prov_row and prov_row.get("last_synced"):
                    try:
                        since = datetime.fromisoformat(prov_row["last_synced"])
                    except (ValueError, TypeError):
                        pass

                remote_tasks = await provider.fetch_tasks(since=since)
                stats["tasks_fetched"] = len(remote_tasks)

                for rt in remote_tasks:
                    self._reconcile_task(slug, rt, stats)

            # Push phase — process queue if not pull_only
            if direction != "pull_only":
                await self._process_push_queue_for_client(slug)

            self._db.update_last_synced(slug, datetime.now(timezone.utc).isoformat())
            self._db.finish_sync_log(log_id, status="success", **stats)
            logger.info("Sync '%s' (%s) complete: %s", slug, direction, stats)
        except Exception as exc:
            self._db.finish_sync_log(log_id, status="error", error=str(exc), **stats)
            logger.exception("Sync '%s' failed", slug)
            raise

        return stats

    async def _process_push_queue_for_client(self, slug: str) -> None:
        """Process push queue items for a single client."""
        pending = self._db.get_pending_pushes()
        for item in pending:
            if item["client_slug"] != slug:
                continue
            provider = self._providers.get(slug)
            if provider is None:
                continue
            action = item["action"]
            payload = json.loads(item["payload"]) if isinstance(item["payload"], str) else item["payload"]
            push_id = item["id"]
            remote_id = item["remote_id"]
            try:
                if action == "status":
                    await provider.push_status(remote_id, payload.get("status", ""))
                elif action == "update":
                    await provider.push_update(remote_id, payload)
                else:
                    continue
                self._db.push_succeeded(push_id)
                self._db.clear_dirty(slug, remote_id)
            except Exception as exc:
                backoff = _BACKOFF_BASE * (2 ** item["retries"])
                self._db.push_failed(push_id, str(exc), backoff_seconds=backoff)

    def _reconcile_task(self, slug: str, rt: RemoteTask, stats: dict) -> None:
        """Reconcile a single remote task against the local store."""
        existing = self._db.get_task(slug, rt.remote_id)
        remote_mod = rt.modified_at.isoformat() if rt.modified_at else None

        extra = {
            "html_description": rt.html_description,
            "start_date": rt.start_date,
            "completed_at": rt.completed_at,
            "created_at": rt.created_at,
            "subtask_count": rt.subtask_count,
            "section_gid": rt.section_gid,
            "custom_fields": rt.custom_fields,
            "permalink_url": rt.permalink_url,
        }

        if existing is None:
            # New remote task
            self._db.upsert_task(
                slug, rt.remote_id,
                remote_url=rt.remote_url, status=rt.status, title=rt.title,
                description=rt.description, assignee=rt.assignee, due_date=rt.due_date,
                priority=rt.priority, labels=rt.labels, remote_modified=remote_mod,
                extra_data=extra,
            )
            stats["tasks_created"] += 1
            return

        # Existing task — provider is source of truth, remote wins on conflict
        if existing["dirty"]:
            # Remote wins — accept remote version and clear dirty flag
            self._db.upsert_task(
                slug, rt.remote_id,
                remote_url=rt.remote_url, status=rt.status, title=rt.title,
                description=rt.description, assignee=rt.assignee, due_date=rt.due_date,
                priority=rt.priority, labels=rt.labels, remote_modified=remote_mod,
                extra_data=extra,
            )
            self._db.clear_dirty(slug, rt.remote_id)
            stats["tasks_updated"] += 1
            return

        # No conflict — update local with remote data
        self._db.upsert_task(
            slug, rt.remote_id,
            remote_url=rt.remote_url, status=rt.status, title=rt.title,
            description=rt.description, assignee=rt.assignee, due_date=rt.due_date,
            priority=rt.priority, labels=rt.labels, remote_modified=remote_mod,
            extra_data=extra,
        )
        stats["tasks_updated"] += 1

    # ------------------------------------------------------------------
    # Push queue
    # ------------------------------------------------------------------

    async def process_push_queue(self) -> None:
        """Process all due outbound pushes with retry."""
        pending = self._db.get_pending_pushes()
        if not pending:
            return

        for item in pending:
            slug = item["client_slug"]
            provider = self._providers.get(slug)
            if provider is None:
                continue

            action = item["action"]
            payload = json.loads(item["payload"]) if isinstance(item["payload"], str) else item["payload"]
            push_id = item["id"]
            remote_id = item["remote_id"]

            try:
                if action == "status":
                    await provider.push_status(remote_id, payload.get("status", ""))
                elif action == "update":
                    await provider.push_update(remote_id, payload)
                else:
                    logger.warning("Unknown push action '%s' for push %s", action, push_id)
                    continue

                self._db.push_succeeded(push_id)
                self._db.clear_dirty(slug, remote_id)
                logger.info("Push %s succeeded for %s/%s", push_id, slug, remote_id)

            except Exception as exc:
                backoff = _BACKOFF_BASE * (2 ** item["retries"])
                self._db.push_failed(push_id, str(exc), backoff_seconds=backoff)
                logger.warning("Push %s failed (attempt %d): %s", push_id, item["retries"] + 1, exc)

    # ------------------------------------------------------------------
    # Query helpers
    # ------------------------------------------------------------------

    def get_status(self) -> dict:
        """Return sync status summary for all configured clients."""
        providers = self._db.list_providers(enabled_only=False)
        conflicts = self._db.get_conflicts(resolved=False)
        pending_pushes = self._db.get_pending_pushes()
        return {
            "running": self._running,
            "providers": providers,
            "unresolved_conflicts": len(conflicts),
            "pending_pushes": len(pending_pushes),
        }

    def get_tasks_for_client(self, slug: str) -> list[dict]:
        return self._db.get_tasks_for_client(slug)

    def get_all_tasks(self) -> list[dict]:
        return self._db.get_all_tasks()

    def get_conflicts(self) -> list[dict]:
        return self._db.get_conflicts(resolved=False)

    def resolve_conflict(self, conflict_id: str, resolution: str) -> None:
        """Resolve a conflict.

        *resolution* is one of ``'local'``, ``'remote'``, or ``'merged'``.
        When ``'remote'`` is chosen the local dirty data is discarded and a
        fresh pull will overwrite on next sync.  When ``'local'`` is chosen
        the dirty changes are pushed via the push queue.
        """
        self._db.resolve_conflict(conflict_id, resolution)

        if resolution == "local":
            # Re-enqueue a push for the winning local version
            row = self._db.conn.execute(
                "SELECT cl.client_slug, cl.remote_id, cl.local_snapshot "
                "FROM conflict_log cl WHERE cl.id = ?", (conflict_id,)
            ).fetchone()
            if row:
                snapshot = json.loads(row["local_snapshot"]) if isinstance(row["local_snapshot"], str) else row["local_snapshot"]
                self._db.enqueue_push(row["client_slug"], row["remote_id"], "update", snapshot)

    # ------------------------------------------------------------------
    # Configuration (called from the router)
    # ------------------------------------------------------------------

    def configure_provider(
        self,
        slug: str,
        provider_type: str,
        config: dict,
        credential: str = "",
        poll_interval: int = 60,
        sync_direction: str = "bidirectional",
    ) -> dict:
        # If credential is empty, preserve the existing one (user didn't re-enter it)
        if not credential:
            existing = self._db.get_provider(slug)
            if existing:
                credential = existing.get("credential_ref", "")
        result = self._db.upsert_provider(slug, provider_type, config, credential, poll_interval, sync_direction)
        self._rebuild_providers()
        # Reschedule per-client job with updated interval
        self._reschedule_client_job(slug, poll_interval)
        return result


# ------------------------------------------------------------------
# Provider factory
# ------------------------------------------------------------------

def _build_provider(ptype: str, config: dict, credential: str) -> TaskProvider:
    """Instantiate a concrete provider from its type string and config."""
    if ptype == "asana":
        return AsanaProvider(
            project_gid=config["project_gid"],
            pat=credential,
            section_map=config.get("section_map"),
            assignee=config.get("assignee_email"),
        )
    if ptype == "github":
        return GitHubProjectsProvider(
            project_node_id=config["project_node_id"],
            pat=credential,
            assignee_filter=config.get("assignee_email"),
        )
    if ptype == "azuredevops":
        return AzureDevOpsProvider(
            organisation=config["organisation"],
            project=config["project"],
            pat=credential,
            team=config.get("team"),
            assignee_filter=config.get("assignee_email"),
        )
    if ptype == "trello":
        from sidecar.services.sync.providers.trello_provider import TrelloProvider

        return TrelloProvider(
            board_id=config["board_id"],
            api_key=config["api_key"],
            token=credential,
            member_filter=config.get("assignee_email"),
        )
    raise ValueError(f"Unknown provider type: {ptype}")


# ------------------------------------------------------------------
# Module-level singleton
# ------------------------------------------------------------------

engine = SyncEngine()
