"""Abstract base for task sync providers."""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from datetime import datetime


@dataclass
class RemoteTask:
    """Normalised representation of a task from any external provider."""

    remote_id: str
    remote_url: str = ""
    status: str = ""
    title: str = ""
    description: str = ""
    assignee: str | None = None
    due_date: str | None = None
    priority: str | None = None
    labels: list[str] = field(default_factory=list)
    modified_at: datetime | None = None
    # Rich fields
    start_date: str | None = None
    completed_at: str | None = None
    created_at: str | None = None
    html_description: str = ""
    subtask_count: int = 0
    section_gid: str = ""  # for pushing status back to correct section
    custom_fields: dict = field(default_factory=dict)  # name -> display_value
    permalink_url: str = ""  # duplicate of remote_url but explicit


@dataclass
class RemoteColumn:
    """Normalised representation of a column / status from any external provider."""

    id: str
    name: str


class TaskProvider(ABC):
    """Contract every sync provider must implement."""

    @abstractmethod
    async def fetch_columns(self) -> list[RemoteColumn]:
        """Return the available columns / statuses from the remote board."""

    @abstractmethod
    async def fetch_tasks(self, since: datetime | None = None) -> list[RemoteTask]:
        """Return tasks modified since *since* (or all tasks when ``None``)."""

    @abstractmethod
    async def push_status(self, remote_id: str, status: str) -> None:
        """Update the status / state of a remote task."""

    @abstractmethod
    async def push_update(self, remote_id: str, fields: dict) -> None:
        """Push arbitrary field changes to a remote task."""

    async def fetch_task_details(self, remote_id: str) -> dict:
        """Fetch rich task details (subtasks, comments, attachments). Override in subclass."""
        return {"subtasks": [], "comments": [], "attachments": []}
