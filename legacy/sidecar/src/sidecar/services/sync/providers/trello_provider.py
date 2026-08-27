"""Trello task sync provider."""

from __future__ import annotations

import logging
from datetime import datetime

import httpx

from sidecar.services.sync.providers.base import RemoteColumn, RemoteTask, TaskProvider

logger = logging.getLogger(__name__)

_BASE_URL = "https://api.trello.com/1"


class TrelloProvider(TaskProvider):
    """Syncs cards from a Trello board via REST API."""

    def __init__(self, board_id: str, api_key: str, token: str, member_filter: str | None = None):
        """
        Parameters
        ----------
        board_id:
            The Trello board ID (short or full).
        api_key:
            Trello API key.
        token:
            Trello member token.
        member_filter:
            Optional full name to filter cards by member (client-side).
        """
        self._board_id = board_id
        self._auth_params = {"key": api_key, "token": token}
        self._member_filter = member_filter.lower() if member_filter else None
        # Lazily populated mapping of list ID -> list name
        self._list_names: dict[str, str] = {}

    # ------------------------------------------------------------------
    # Columns
    # ------------------------------------------------------------------

    async def fetch_columns(self) -> list[RemoteColumn]:
        url = f"{_BASE_URL}/boards/{self._board_id}/lists"
        params = {**self._auth_params, "fields": "name"}
        columns: list[RemoteColumn] = []

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.get(url, params=params)
            resp.raise_for_status()
            body = resp.json()
            for lst in body:
                columns.append(RemoteColumn(id=lst["id"], name=lst.get("name", "")))
                self._list_names[lst["id"]] = lst.get("name", "")

        logger.info("Trello: fetched %d lists from board %s", len(columns), self._board_id)
        return columns

    # ------------------------------------------------------------------
    # Fetch
    # ------------------------------------------------------------------

    async def fetch_tasks(self, since: datetime | None = None) -> list[RemoteTask]:
        # Ensure list names are populated
        if not self._list_names:
            await self.fetch_columns()

        url = f"{_BASE_URL}/boards/{self._board_id}/cards"
        params = {
            **self._auth_params,
            "fields": "name,desc,idList,idLabels,due,dateLastActivity,shortUrl,idMembers",
            "members": "true",
            "member_fields": "fullName",
        }

        tasks: list[RemoteTask] = []

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.get(url, params=params)
            resp.raise_for_status()
            cards = resp.json()

            for card in cards:
                task = self._to_remote_task(card)
                # Client-side date filter
                if since and task.modified_at and task.modified_at < since:
                    continue
                # Client-side member filter
                if self._member_filter and (
                    not task.assignee or self._member_filter != task.assignee.lower()
                ):
                    continue
                tasks.append(task)

        logger.info("Trello: fetched %d cards from board %s", len(tasks), self._board_id)
        return tasks

    # ------------------------------------------------------------------
    # Push
    # ------------------------------------------------------------------

    async def push_status(self, remote_id: str, status: str) -> None:
        # Status maps to a list in Trello — find the list ID by name
        target_list_id: str | None = None
        for list_id, list_name in self._list_names.items():
            if list_name.lower().strip() == status.lower().strip():
                target_list_id = list_id
                break

        if not target_list_id:
            logger.warning(
                "Trello: no list matching status '%s', available: %s",
                status,
                list(self._list_names.values()),
            )
            return

        url = f"{_BASE_URL}/cards/{remote_id}"
        params = {**self._auth_params}

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.put(url, params=params, json={"idList": target_list_id})
            resp.raise_for_status()

        logger.info("Trello: moved card %s to list %s (%s)", remote_id, target_list_id, status)

    async def push_update(self, remote_id: str, fields: dict) -> None:
        field_map = {
            "title": "name",
            "description": "desc",
            "due_date": "due",
        }

        data: dict = {}
        for key, value in fields.items():
            trello_key = field_map.get(key)
            if trello_key:
                data[trello_key] = value

        # Handle status separately
        if "status" in fields:
            await self.push_status(remote_id, fields["status"])

        if not data:
            return

        url = f"{_BASE_URL}/cards/{remote_id}"
        params = {**self._auth_params}

        async with httpx.AsyncClient(timeout=30) as client:
            resp = await client.put(url, params=params, json=data)
            resp.raise_for_status()

        logger.info("Trello: updated card %s fields %s", remote_id, list(data.keys()))

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _to_remote_task(self, card: dict) -> RemoteTask:
        # Derive status from list name
        list_id = card.get("idList", "")
        status = self._list_names.get(list_id, "")

        # Assignee: first member
        assignee: str | None = None
        members = card.get("members", [])
        if members:
            assignee = members[0].get("fullName")

        # Labels
        labels = [lb.get("name", "") for lb in card.get("labels", []) if lb.get("name")]

        # Modified at
        modified_at: datetime | None = None
        activity = card.get("dateLastActivity")
        if activity:
            try:
                modified_at = datetime.fromisoformat(activity.replace("Z", "+00:00"))
            except (ValueError, TypeError):
                pass

        return RemoteTask(
            remote_id=card["id"],
            remote_url=card.get("shortUrl", ""),
            status=status,
            title=card.get("name", ""),
            description=card.get("desc", ""),
            assignee=assignee,
            due_date=card.get("due"),
            priority=None,
            labels=labels,
            modified_at=modified_at,
        )
