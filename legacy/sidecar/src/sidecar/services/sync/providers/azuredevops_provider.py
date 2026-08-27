"""Azure DevOps work-item sync provider."""

from __future__ import annotations

import base64
import logging
from datetime import datetime

import httpx

from sidecar.services.sync.providers.base import RemoteColumn, RemoteTask, TaskProvider

logger = logging.getLogger(__name__)

# Max work items per batch GET (Azure DevOps limit is 200)
_BATCH_SIZE = 200


class AzureDevOpsProvider(TaskProvider):
    """Syncs work items from an Azure DevOps project via REST API."""

    def __init__(
        self,
        organisation: str,
        project: str,
        pat: str,
        team: str | None = None,
        assignee_filter: str | None = None,
    ):
        """
        Parameters
        ----------
        organisation:
            Azure DevOps organisation name (the ``dev.azure.com/{org}`` segment).
        project:
            Project name.
        pat:
            Personal Access Token.
        team:
            Optional team name for scoped queries.
        assignee_filter:
            Optional email or display name to filter work items by assignee.
        """
        self._org = organisation
        self._project = project
        self._team = team
        self._assignee_filter = assignee_filter
        self._base = f"https://dev.azure.com/{organisation}/{project}"
        token_bytes = base64.b64encode(f":{pat}".encode()).decode()
        self._headers = {
            "Authorization": f"Basic {token_bytes}",
            "Content-Type": "application/json",
        }

    # ------------------------------------------------------------------
    # Columns
    # ------------------------------------------------------------------

    async def fetch_columns(self) -> list[RemoteColumn]:
        url = f"{self._base}/_apis/wit/workitemtypes/Task/states?api-version=7.1"
        columns: list[RemoteColumn] = []

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.get(url, headers=self._headers)
            resp.raise_for_status()
            body = resp.json()
            for state in body.get("value", []):
                columns.append(RemoteColumn(id=state["name"], name=state["name"]))

        logger.info("AzDO: fetched %d states from %s/%s", len(columns), self._org, self._project)
        return columns

    # ------------------------------------------------------------------
    # Fetch
    # ------------------------------------------------------------------

    async def fetch_tasks(self, since: datetime | None = None) -> list[RemoteTask]:
        ids = await self._wiql_query(since)
        if not ids:
            return []

        tasks: list[RemoteTask] = []
        # Batch fetch work item details
        for i in range(0, len(ids), _BATCH_SIZE):
            batch = ids[i : i + _BATCH_SIZE]
            tasks.extend(await self._batch_get(batch))

        logger.info("AzDO: fetched %d work items from %s/%s", len(tasks), self._org, self._project)
        return tasks

    async def _wiql_query(self, since: datetime | None) -> list[int]:
        """Run a WIQL query to get work item IDs changed since *since*."""
        where_clause = "[System.TeamProject] = @project"
        if since:
            iso = since.strftime("%Y-%m-%dT%H:%M:%SZ")
            where_clause += f" AND [System.ChangedDate] >= '{iso}'"
        if self._assignee_filter:
            where_clause += f" AND [System.AssignedTo] = '{self._assignee_filter}'"

        wiql = f"SELECT [System.Id] FROM WorkItems WHERE {where_clause} ORDER BY [System.ChangedDate] DESC"

        team_segment = f"/{self._team}" if self._team else ""
        url = f"{self._base}{team_segment}/_apis/wit/wiql?api-version=7.1"

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.post(url, headers=self._headers, json={"query": wiql})
            resp.raise_for_status()
            body = resp.json()

        return [item["id"] for item in body.get("workItems", [])]

    async def _batch_get(self, ids: list[int]) -> list[RemoteTask]:
        """Batch-fetch work item details."""
        id_str = ",".join(str(i) for i in ids)
        fields = (
            "System.Id,System.Title,System.Description,System.State,"
            "System.AssignedTo,Microsoft.VSTS.Scheduling.DueDate,"
            "Microsoft.VSTS.Common.Priority,System.Tags,"
            "System.ChangedDate"
        )
        url = f"{self._base}/_apis/wit/workitems?ids={id_str}&fields={fields}&api-version=7.1"

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.get(url, headers=self._headers)
            resp.raise_for_status()
            body = resp.json()

        tasks: list[RemoteTask] = []
        for item in body.get("value", []):
            tasks.append(self._to_remote_task(item))
        return tasks

    # ------------------------------------------------------------------
    # Push
    # ------------------------------------------------------------------

    async def push_status(self, remote_id: str, status: str) -> None:
        patch = [{"op": "replace", "path": "/fields/System.State", "value": status}]
        url = f"{self._base}/_apis/wit/workitems/{remote_id}?api-version=7.1"

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.patch(
                url,
                headers={**self._headers, "Content-Type": "application/json-patch+json"},
                json=patch,
            )
            resp.raise_for_status()

        logger.info("AzDO: set work item %s state to '%s'", remote_id, status)

    async def push_update(self, remote_id: str, fields: dict) -> None:
        field_map = {
            "title": "/fields/System.Title",
            "description": "/fields/System.Description",
            "status": "/fields/System.State",
            "assignee": "/fields/System.AssignedTo",
            "due_date": "/fields/Microsoft.VSTS.Scheduling.DueDate",
            "priority": "/fields/Microsoft.VSTS.Common.Priority",
        }

        patch = []
        for key, value in fields.items():
            path = field_map.get(key)
            if path:
                patch.append({"op": "replace", "path": path, "value": value})

        if not patch:
            return

        url = f"{self._base}/_apis/wit/workitems/{remote_id}?api-version=7.1"
        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.patch(
                url,
                headers={**self._headers, "Content-Type": "application/json-patch+json"},
                json=patch,
            )
            resp.raise_for_status()

        logger.info("AzDO: updated work item %s fields %s", remote_id, list(fields.keys()))

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _to_remote_task(self, item: dict) -> RemoteTask:
        f = item.get("fields", {})
        work_item_id = item.get("id", "")

        assignee_name: str | None = None
        assigned_to = f.get("System.AssignedTo")
        if isinstance(assigned_to, dict):
            assignee_name = assigned_to.get("displayName")
        elif isinstance(assigned_to, str):
            assignee_name = assigned_to

        tags_raw = f.get("System.Tags", "")
        labels = [t.strip() for t in tags_raw.split(";") if t.strip()] if tags_raw else []

        priority_raw = f.get("Microsoft.VSTS.Common.Priority")
        priority = str(priority_raw) if priority_raw is not None else None

        modified_at: datetime | None = None
        changed = f.get("System.ChangedDate")
        if changed:
            try:
                modified_at = datetime.fromisoformat(changed.replace("Z", "+00:00"))
            except (ValueError, TypeError):
                pass

        url = f"{self._base}/_workitems/edit/{work_item_id}"

        return RemoteTask(
            remote_id=str(work_item_id),
            remote_url=url,
            status=f.get("System.State", ""),
            title=f.get("System.Title", ""),
            description=f.get("System.Description", ""),
            assignee=assignee_name,
            due_date=f.get("Microsoft.VSTS.Scheduling.DueDate"),
            priority=priority,
            labels=labels,
            modified_at=modified_at,
        )
