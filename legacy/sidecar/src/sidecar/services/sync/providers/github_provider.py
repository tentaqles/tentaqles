"""GitHub Projects v2 task sync provider (GraphQL)."""

from __future__ import annotations

import logging
from datetime import datetime

import httpx

from sidecar.services.sync.providers.base import RemoteColumn, RemoteTask, TaskProvider

logger = logging.getLogger(__name__)

_GRAPHQL_URL = "https://api.github.com/graphql"

# -----------------------------------------------------------------
# GraphQL fragments
# -----------------------------------------------------------------

_FETCH_ITEMS_QUERY = """
query($projectId: ID!, $cursor: String) {
  node(id: $projectId) {
    ... on ProjectV2 {
      items(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          updatedAt
          content {
            ... on Issue {
              title
              body
              url
              assignees(first: 5) { nodes { login } }
              labels(first: 10) { nodes { name } }
            }
            ... on DraftIssue {
              title
              body
              assignees(first: 5) { nodes { login } }
            }
            ... on PullRequest {
              title
              body
              url
              assignees(first: 5) { nodes { login } }
              labels(first: 10) { nodes { name } }
            }
          }
          fieldValues(first: 20) {
            nodes {
              ... on ProjectV2ItemFieldSingleSelectValue {
                name
                field { ... on ProjectV2SingleSelectField { name } }
              }
              ... on ProjectV2ItemFieldDateValue {
                date
                field { ... on ProjectV2Field { name } }
              }
              ... on ProjectV2ItemFieldTextValue {
                text
                field { ... on ProjectV2Field { name } }
              }
            }
          }
        }
      }
    }
  }
}
"""

_DISCOVER_STATUS_FIELD_QUERY = """
query($projectId: ID!) {
  node(id: $projectId) {
    ... on ProjectV2 {
      fields(first: 50) {
        nodes {
          ... on ProjectV2SingleSelectField {
            id
            name
            options { id name }
          }
        }
      }
    }
  }
}
"""

_UPDATE_FIELD_MUTATION = """
mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $optionId: String!) {
  updateProjectV2ItemFieldValue(input: {
    projectId: $projectId
    itemId: $itemId
    fieldId: $fieldId
    value: { singleSelectOptionId: $optionId }
  }) {
    projectV2Item { id }
  }
}
"""


class GitHubProjectsProvider(TaskProvider):
    """Syncs items from a GitHub Projects v2 board via GraphQL."""

    def __init__(self, project_node_id: str, pat: str, assignee_filter: str | None = None):
        """
        Parameters
        ----------
        project_node_id:
            The global node ID of the GitHub ProjectV2 (starts with ``PVT_``).
        pat:
            GitHub Personal Access Token with ``project`` scope.
        assignee_filter:
            Optional GitHub username to filter tasks by assignee (client-side).
        """
        self._project_id = project_node_id
        self._pat = pat
        self._assignee_filter = assignee_filter.lower() if assignee_filter else None
        self._headers = {
            "Authorization": f"Bearer {pat}",
            "Content-Type": "application/json",
        }
        # Lazily discovered field/option IDs
        self._status_field_id: str | None = None
        self._status_options: dict[str, str] = {}  # option name -> option id

    # ------------------------------------------------------------------
    # Internal GraphQL helper
    # ------------------------------------------------------------------

    async def _gql(self, client: httpx.AsyncClient, query: str, variables: dict) -> dict:
        resp = await client.post(
            _GRAPHQL_URL,
            headers=self._headers,
            json={"query": query, "variables": variables},
        )
        resp.raise_for_status()
        body = resp.json()
        if body.get("errors"):
            logger.error("GitHub GraphQL errors: %s", body["errors"])
        return body.get("data", {})

    # ------------------------------------------------------------------
    # Discover status field
    # ------------------------------------------------------------------

    async def _ensure_status_field(self, client: httpx.AsyncClient) -> None:
        if self._status_field_id:
            return

        data = await self._gql(client, _DISCOVER_STATUS_FIELD_QUERY, {"projectId": self._project_id})
        fields = data.get("node", {}).get("fields", {}).get("nodes", [])
        for f in fields:
            if f.get("name", "").lower() == "status":
                self._status_field_id = f["id"]
                self._status_options = {opt["name"]: opt["id"] for opt in f.get("options", [])}
                logger.info(
                    "GitHub: discovered Status field %s with options %s",
                    self._status_field_id,
                    list(self._status_options.keys()),
                )
                return

        logger.warning("GitHub: no 'Status' single-select field found on project %s", self._project_id)

    # ------------------------------------------------------------------
    # Columns
    # ------------------------------------------------------------------

    async def fetch_columns(self) -> list[RemoteColumn]:
        columns: list[RemoteColumn] = []
        async with httpx.AsyncClient(timeout=30) as client:
            await self._ensure_status_field(client)
            for name, option_id in self._status_options.items():
                columns.append(RemoteColumn(id=option_id, name=name))

        logger.info("GitHub: fetched %d status options from project %s", len(columns), self._project_id)
        return columns

    # ------------------------------------------------------------------
    # Fetch
    # ------------------------------------------------------------------

    async def fetch_tasks(self, since: datetime | None = None) -> list[RemoteTask]:
        tasks: list[RemoteTask] = []
        cursor: str | None = None

        async with httpx.AsyncClient(timeout=30) as client:
            while True:
                variables: dict = {"projectId": self._project_id, "cursor": cursor}
                data = await self._gql(client, _FETCH_ITEMS_QUERY, variables)

                items_data = data.get("node", {}).get("items", {})
                nodes = items_data.get("nodes", [])

                for node in nodes:
                    task = self._to_remote_task(node)
                    # Client-side date filter (GraphQL doesn't natively support modified_since)
                    if since and task.modified_at and task.modified_at < since:
                        continue
                    # Client-side assignee filter
                    if self._assignee_filter and (
                        not task.assignee or self._assignee_filter != task.assignee.lower()
                    ):
                        continue
                    tasks.append(task)

                page_info = items_data.get("pageInfo", {})
                if page_info.get("hasNextPage"):
                    cursor = page_info.get("endCursor")
                else:
                    break

        logger.info("GitHub: fetched %d items from project %s", len(tasks), self._project_id)
        return tasks

    # ------------------------------------------------------------------
    # Push
    # ------------------------------------------------------------------

    async def push_status(self, remote_id: str, status: str) -> None:
        async with httpx.AsyncClient(timeout=30) as client:
            await self._ensure_status_field(client)

            if not self._status_field_id:
                logger.warning("GitHub: cannot push status — Status field not discovered")
                return

            option_id = self._status_options.get(status)
            if not option_id:
                logger.warning("GitHub: unknown status option '%s', available: %s", status, list(self._status_options.keys()))
                return

            await self._gql(client, _UPDATE_FIELD_MUTATION, {
                "projectId": self._project_id,
                "itemId": remote_id,
                "fieldId": self._status_field_id,
                "optionId": option_id,
            })

        logger.info("GitHub: updated item %s status to '%s'", remote_id, status)

    async def push_update(self, remote_id: str, fields: dict) -> None:
        # GitHub Projects v2 has limited field mutation support.
        # For now we only handle status changes via push_status.
        if "status" in fields:
            await self.push_status(remote_id, fields["status"])

        if any(k != "status" for k in fields):
            logger.info(
                "GitHub: push_update ignoring non-status fields %s (not yet supported)",
                [k for k in fields if k != "status"],
            )

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _to_remote_task(node: dict) -> RemoteTask:
        content = node.get("content", {}) or {}
        field_values = node.get("fieldValues", {}).get("nodes", [])

        # Extract status and due_date from field values
        status = ""
        due_date: str | None = None
        for fv in field_values:
            field_name = ""
            field_obj = fv.get("field", {})
            if field_obj:
                field_name = field_obj.get("name", "").lower()
            if field_name == "status" and "name" in fv:
                status = fv["name"]
            elif field_name == "due date" and "date" in fv:
                due_date = fv["date"]

        assignees = [a["login"] for a in content.get("assignees", {}).get("nodes", []) if "login" in a]
        labels = [lb["name"] for lb in content.get("labels", {}).get("nodes", []) if "name" in lb]

        modified_at: datetime | None = None
        if node.get("updatedAt"):
            try:
                modified_at = datetime.fromisoformat(node["updatedAt"].replace("Z", "+00:00"))
            except (ValueError, TypeError):
                pass

        return RemoteTask(
            remote_id=node["id"],
            remote_url=content.get("url", ""),
            status=status,
            title=content.get("title", ""),
            description=content.get("body", ""),
            assignee=assignees[0] if assignees else None,
            due_date=due_date,
            priority=None,
            labels=labels,
            modified_at=modified_at,
        )
