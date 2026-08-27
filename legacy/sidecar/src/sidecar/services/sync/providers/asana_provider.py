"""Asana task sync provider."""

from __future__ import annotations

import logging
from datetime import datetime

import httpx

from sidecar.services.sync.providers.base import RemoteColumn, RemoteTask, TaskProvider

logger = logging.getLogger(__name__)

_BASE_URL = "https://app.asana.com/api/1.0"

_OPT_FIELDS = (
    "name,notes,html_notes,assignee.name,assignee.email,"
    "due_on,due_at,start_on,start_at,completed,completed_at,created_at,"
    "tags.name,memberships.section.name,memberships.section.gid,"
    "modified_at,permalink_url,"
    "custom_fields.name,custom_fields.display_value,custom_fields.type,"
    "num_subtasks,parent.name"
)


class AsanaProvider(TaskProvider):
    """Syncs tasks from an Asana project board."""

    def __init__(
        self,
        project_gid: str,
        pat: str,
        section_map: dict[str, str] | None = None,
        assignee: str | None = None,
    ):
        """
        Parameters
        ----------
        project_gid:
            The Asana project GID to sync.
        pat:
            Personal Access Token for Asana.
        section_map:
            Optional mapping of *local status name* -> *Asana section GID*.
            Used when pushing status changes back to Asana.
        assignee:
            Optional email address to filter tasks by assignee.
            When set, only tasks assigned to this user are fetched.
        """
        self._project_gid = project_gid
        self._pat = pat
        self._section_map: dict[str, str] = section_map or {}
        self._assignee = assignee
        self._headers = {"Authorization": f"Bearer {pat}"}
        self._section_gid_to_name: dict[str, str] = {}

    # ------------------------------------------------------------------
    # Columns
    # ------------------------------------------------------------------

    async def fetch_columns(self) -> list[RemoteColumn]:
        url = f"{_BASE_URL}/projects/{self._project_gid}/sections"
        params = {"opt_fields": "name"}
        columns: list[RemoteColumn] = []

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.get(url, headers=self._headers, params=params)
            resp.raise_for_status()
            body = resp.json()
            for section in body.get("data", []):
                columns.append(RemoteColumn(id=section["gid"], name=section.get("name", "")))

        logger.info("Asana: fetched %d sections from project %s", len(columns), self._project_gid)
        return columns

    # ------------------------------------------------------------------
    # Fetch
    # ------------------------------------------------------------------

    async def fetch_tasks(self, since: datetime | None = None) -> list[RemoteTask]:
        params: dict[str, str] = {"opt_fields": _OPT_FIELDS}
        if since is not None:
            params["modified_since"] = since.isoformat()

        tasks: list[RemoteTask] = []
        url = f"{_BASE_URL}/projects/{self._project_gid}/tasks"
        assignee_lower = self._assignee.lower().strip() if self._assignee else ""

        async with httpx.AsyncClient(timeout=30) as client:
            while url:
                resp = await client.get(url, headers=self._headers, params=params)
                resp.raise_for_status()
                body = resp.json()

                for item in body.get("data", []):
                    # Client-side assignee filter (Asana project tasks endpoint doesn't support server-side)
                    if assignee_lower:
                        assignee_obj = item.get("assignee") or {}
                        assignee_name = (assignee_obj.get("name") or "").lower()
                        assignee_email = (assignee_obj.get("email") or "").lower()
                        if assignee_lower not in assignee_name and assignee_lower not in assignee_email:
                            continue

                    # Build section GID -> name mapping from memberships
                    for membership in item.get("memberships", []):
                        section = membership.get("section")
                        if section and section.get("gid") and section.get("name"):
                            self._section_gid_to_name[section["gid"]] = section["name"]
                    tasks.append(self._to_remote_task(item))

                # Pagination
                next_page = body.get("next_page")
                if next_page and next_page.get("uri"):
                    url = next_page["uri"]
                    params = {}  # URI already contains query params
                else:
                    url = ""

        logger.info("Asana: fetched %d tasks from project %s", len(tasks), self._project_gid)
        return tasks

    # ------------------------------------------------------------------
    # Push
    # ------------------------------------------------------------------

    async def push_status(self, remote_id: str, status: str) -> None:
        section_gid = self._section_map.get(status)
        if not section_gid:
            logger.warning("Asana: no section mapping for status '%s', skipping push_status", status)
            return

        url = f"{_BASE_URL}/sections/{section_gid}/addTask"
        payload = {"data": {"task": remote_id}}

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.post(url, headers=self._headers, json=payload)
            resp.raise_for_status()

        logger.info("Asana: moved task %s to section %s (%s)", remote_id, section_gid, status)

    async def push_update(self, remote_id: str, fields: dict) -> None:
        url = f"{_BASE_URL}/tasks/{remote_id}"
        # Map normalised field names to Asana API fields
        data: dict = {}
        if "title" in fields:
            data["name"] = fields["title"]
        if "description" in fields:
            data["notes"] = fields["description"]
        if "due_date" in fields:
            data["due_on"] = fields["due_date"]
        if "assignee" in fields:
            data["assignee"] = fields["assignee"]

        if not data:
            return

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.put(url, headers=self._headers, json={"data": data})
            resp.raise_for_status()

        logger.info("Asana: updated task %s fields %s", remote_id, list(data.keys()))

    # ------------------------------------------------------------------
    # On-demand task details
    # ------------------------------------------------------------------

    async def fetch_task_details(self, remote_id: str) -> dict:
        """Fetch rich details for a single task: subtasks, comments, attachments."""
        async with httpx.AsyncClient(timeout=30) as client:
            # Subtasks
            subtasks_resp = await client.get(
                f"{_BASE_URL}/tasks/{remote_id}/subtasks",
                headers=self._headers,
                params={"opt_fields": "name,completed,assignee.name"},
            )
            subtasks_resp.raise_for_status()
            subtasks = [
                {
                    "name": s["name"],
                    "completed": s.get("completed", False),
                    "assignee": s.get("assignee", {}).get("name") if s.get("assignee") else None,
                }
                for s in subtasks_resp.json().get("data", [])
            ]

            # Comments/stories (filter to comments only)
            stories_resp = await client.get(
                f"{_BASE_URL}/tasks/{remote_id}/stories",
                headers=self._headers,
                params={"opt_fields": "type,text,created_by.name,created_at"},
            )
            stories_resp.raise_for_status()
            comments = [
                {
                    "text": s["text"],
                    "author": s.get("created_by", {}).get("name", ""),
                    "created_at": s.get("created_at", ""),
                }
                for s in stories_resp.json().get("data", [])
                if s.get("type") == "comment"
            ]

            # Attachments
            attach_resp = await client.get(
                f"{_BASE_URL}/tasks/{remote_id}/attachments",
                headers=self._headers,
                params={"opt_fields": "name,download_url,host,view_url"},
            )
            attach_resp.raise_for_status()
            attachments = [
                {"name": a["name"], "url": a.get("view_url") or a.get("download_url", "")}
                for a in attach_resp.json().get("data", [])
            ]

            return {"subtasks": subtasks, "comments": comments, "attachments": attachments}

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _to_remote_task(item: dict) -> RemoteTask:
        assignee_name: str | None = None
        if item.get("assignee"):
            assignee_name = item["assignee"].get("name")

        # Derive status and section GID from the first section membership (board column)
        status = ""
        section_gid = ""
        for membership in item.get("memberships", []):
            section = membership.get("section")
            if section:
                status = section.get("name", "")
                section_gid = section.get("gid", "")
                break

        labels = [t["name"] for t in item.get("tags", []) if "name" in t]

        modified_at: datetime | None = None
        if item.get("modified_at"):
            try:
                modified_at = datetime.fromisoformat(item["modified_at"].replace("Z", "+00:00"))
            except (ValueError, TypeError):
                pass

        # Parse ISO timestamps for rich fields
        completed_at: str | None = None
        if item.get("completed_at"):
            try:
                completed_at = datetime.fromisoformat(
                    item["completed_at"].replace("Z", "+00:00")
                ).isoformat()
            except (ValueError, TypeError):
                completed_at = item["completed_at"]

        created_at: str | None = None
        if item.get("created_at"):
            try:
                created_at = datetime.fromisoformat(
                    item["created_at"].replace("Z", "+00:00")
                ).isoformat()
            except (ValueError, TypeError):
                created_at = item["created_at"]

        # Map custom fields: name -> display_value
        custom_fields = {
            cf["name"]: cf.get("display_value", "")
            for cf in item.get("custom_fields", [])
            if cf.get("name")
        }

        return RemoteTask(
            remote_id=item["gid"],
            remote_url=item.get("permalink_url", ""),
            status=status,
            title=item.get("name", ""),
            description=item.get("notes", ""),
            assignee=assignee_name,
            due_date=item.get("due_on"),
            priority=None,
            labels=labels,
            modified_at=modified_at,
            start_date=item.get("start_on"),
            completed_at=completed_at,
            created_at=created_at,
            html_description=item.get("html_notes", ""),
            subtask_count=item.get("num_subtasks", 0),
            section_gid=section_gid,
            custom_fields=custom_fields,
            permalink_url=item.get("permalink_url", ""),
        )
