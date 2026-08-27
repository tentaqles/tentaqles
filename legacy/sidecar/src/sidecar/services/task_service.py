"""Task service — standalone task CRUD with JSON storage in ~/.tentaqles/tasks/."""

from __future__ import annotations

import json
import logging
import uuid
from datetime import datetime
from pathlib import Path

from sidecar.services.safe_io import atomic_write

logger = logging.getLogger(__name__)

_TASKS_DIR = Path.home() / ".tentaqles" / "tasks"


def _uid() -> str:
    return f"task-{uuid.uuid4().hex[:8]}"


def _now() -> str:
    return datetime.now().isoformat(timespec="seconds")


def _task_path(task_id: str) -> Path:
    return _TASKS_DIR / f"{task_id}.json"


def _read_task(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def _write_task(path: Path, task: dict) -> None:
    atomic_write(path, json.dumps(task, indent=2))


# --- CRUD ---


def list_tasks(
    status: str | None = None,
    priority: str | None = None,
    label: str | None = None,
    workspace_path: str | None = None,
) -> list[dict]:
    """List all tasks, optionally filtered."""
    if not _TASKS_DIR.is_dir():
        return []

    results: list[dict] = []
    for f in sorted(_TASKS_DIR.glob("*.json")):
        try:
            task = _read_task(f)
        except (json.JSONDecodeError, ValueError) as e:
            logger.warning("Failed to load task %s: %s", f, e)
            continue

        if status and task.get("status") != status:
            continue
        if priority and task.get("priority") != priority:
            continue
        if label and label not in task.get("labels", []):
            continue
        if workspace_path and task.get("workspace_path") != workspace_path:
            continue

        results.append(task)

    return sorted(results, key=lambda t: t.get("position", 0))


def get_task(task_id: str) -> dict:
    """Get a single task by ID."""
    path = _task_path(task_id)
    if not path.exists():
        raise ValueError(f"Task not found: {task_id}")
    return _read_task(path)


def create_task(
    title: str,
    description: str = "",
    status: str = "backlog",
    priority: str = "medium",
    labels: list[str] | None = None,
    checklist: list[dict] | None = None,
    assignee: str | None = None,
    due_date: str | None = None,
    workspace_path: str | None = None,
    column: str = "backlog",
    position: int = 0,
    client_name: str | None = None,
    project_path: str | None = None,
) -> dict:
    """Create a new task."""
    now = _now()
    task = {
        "id": _uid(),
        "title": title,
        "description": description,
        "status": status,
        "priority": priority,
        "labels": labels or [],
        "checklist": checklist or [],
        "comments": [],
        "assignee": assignee,
        "due_date": due_date,
        "workspace_path": workspace_path,
        "column": column,
        "position": position,
        "client_name": client_name,
        "project_path": project_path,
        "execution": {
            "state": "idle",
            "started_at": None,
            "finished_at": None,
            "output": "",
            "error": None,
            "duration_ms": 0,
            "cost_usd": 0,
            "input_tokens": 0,
            "output_tokens": 0,
            "session_id": None,
        },
        "created": now,
        "updated": now,
    }

    _write_task(_task_path(task["id"]), task)
    return task


ALLOWED_NEW_FIELDS = {"client_name", "project_path", "execution"}


def update_task(task_id: str, **kwargs) -> dict:
    """Update task fields. Returns updated task."""
    path = _task_path(task_id)
    if not path.exists():
        raise ValueError(f"Task not found: {task_id}")

    task = _read_task(path)

    for key, value in kwargs.items():
        if value is not None and (key in task or key in ALLOWED_NEW_FIELDS):
            task[key] = value

    task["updated"] = _now()
    _write_task(path, task)
    return task


def move_task(task_id: str, column: str, position: int = 0) -> dict:
    """Move a task to a different column/position."""
    path = _task_path(task_id)
    if not path.exists():
        raise ValueError(f"Task not found: {task_id}")

    task = _read_task(path)
    task["column"] = column
    task["position"] = position
    task["status"] = column  # status mirrors column name
    task["updated"] = _now()
    _write_task(path, task)
    return task


def delete_task(task_id: str) -> None:
    """Delete a task."""
    path = _task_path(task_id)
    if path.exists():
        path.unlink()


def add_comment(task_id: str, author: str, text: str) -> dict:
    """Add a comment to a task."""
    path = _task_path(task_id)
    if not path.exists():
        raise ValueError(f"Task not found: {task_id}")

    task = _read_task(path)
    comment = {
        "id": f"cmt-{uuid.uuid4().hex[:8]}",
        "author": author,
        "text": text,
        "created": _now(),
    }
    task.setdefault("comments", []).append(comment)
    task["updated"] = _now()
    _write_task(path, task)
    return task


def toggle_checklist_item(task_id: str, item_index: int) -> dict:
    """Toggle a checklist item's completed state."""
    path = _task_path(task_id)
    if not path.exists():
        raise ValueError(f"Task not found: {task_id}")

    task = _read_task(path)
    checklist = task.get("checklist", [])
    if 0 <= item_index < len(checklist):
        checklist[item_index]["done"] = not checklist[item_index].get("done", False)
    task["updated"] = _now()
    _write_task(path, task)
    return task
