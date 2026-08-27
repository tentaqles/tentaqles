"""Tasks router — standalone task management."""

from fastapi import APIRouter

from sidecar.schemas import (
    TaskCancelRequest,
    TaskChecklistToggleRequest,
    TaskCommentRequest,
    TaskCreateRequest,
    TaskDeleteRequest,
    TaskExecuteRequest,
    TaskGetRequest,
    TaskListRequest,
    TaskMoveRequest,
    TaskUpdateRequest,
    UnifiedCard,
    UnifiedFeedRequest,
)
from sidecar.services.task_executor import cancel_task_execution, execute_task
from sidecar.services.task_service import (
    add_comment,
    create_task,
    delete_task,
    get_task,
    list_tasks,
    move_task,
    toggle_checklist_item,
    update_task,
)

router = APIRouter()

# Map common remote status names to local column IDs
_STATUS_MAP = {
    "backlog": "backlog",
    "to do": "todo",
    "todo": "todo",
    "to-do": "todo",
    "in progress": "in_progress",
    "in-progress": "in_progress",
    "doing": "in_progress",
    "in review": "in_review",
    "in-review": "in_review",
    "review": "in_review",
    "blocked": "blocked",
    "blocked/on hold": "blocked",
    "on hold": "blocked",
    "done": "done",
    "complete": "done",
    "completed": "done",
    "closed": "done",
    "archive": "archive",
    "archived": "archive",
}


def _normalize_status(raw: str) -> str:
    """Normalize a remote status string to a local column ID."""
    key = raw.strip().lower()
    return _STATUS_MAP.get(key, key.replace(" ", "_"))


@router.post("/list")
def tasks_list(req: TaskListRequest):
    return list_tasks(req.status, req.priority, req.label, req.workspace_path)


@router.post("/get")
def tasks_get(req: TaskGetRequest):
    return get_task(req.task_id)


@router.post("/create")
def tasks_create(req: TaskCreateRequest):
    return create_task(
        req.title,
        req.description,
        req.status,
        req.priority,
        req.labels,
        req.checklist,
        req.assignee,
        req.due_date,
        req.workspace_path,
        req.column,
        req.position,
        req.client_name,
        req.project_path,
    )


@router.post("/update")
def tasks_update(req: TaskUpdateRequest):
    exclude = {"task_id"}
    kwargs = {k: v for k, v in req.model_dump().items() if k not in exclude and v is not None}
    return update_task(req.task_id, **kwargs)


@router.post("/move")
def tasks_move(req: TaskMoveRequest):
    return move_task(req.task_id, req.column, req.position)


@router.post("/delete")
def tasks_delete(req: TaskDeleteRequest):
    delete_task(req.task_id)
    return {"ok": True}


@router.post("/comment")
def tasks_comment(req: TaskCommentRequest):
    return add_comment(req.task_id, req.author, req.text)


@router.post("/checklist-toggle")
def tasks_checklist_toggle(req: TaskChecklistToggleRequest):
    return toggle_checklist_item(req.task_id, req.item_index)


@router.post("/unified")
def tasks_unified(req: UnifiedFeedRequest):
    """Merge native tasks + board cards into a unified feed."""
    # Get native tasks
    native = list_tasks(workspace_path=req.workspace_path)
    unified = []

    for t in native:
        card = UnifiedCard(
            id=t["id"],
            display_id=f"TSK-{t['id'][:4].upper()}",
            title=t["title"],
            description=t.get("description"),
            status=t.get("column", t.get("status", "backlog")),
            priority=t.get("priority"),
            source="native",
            workspace_path=t.get("workspace_path"),
            labels=t.get("labels", []),
            assignee=t.get("assignee"),
        ).model_dump()
        # Pass through execution, client_name, project_path if present
        if "execution" in t:
            card["execution"] = t["execution"]
        if "client_name" in t:
            card["client_name"] = t["client_name"]
        if "project_path" in t:
            card["project_path"] = t["project_path"]
        unified.append(card)

    # Try to get board cards if board service is available
    try:
        from sidecar.services.board_service import list_boards
        boards = list_boards(scope="all", client_path=req.client_path)
        for board in boards:
            for card in board.get("cards", []):
                unified.append(UnifiedCard(
                    id=card.get("id", ""),
                    display_id=card.get("id", "")[:8],
                    title=card.get("title", ""),
                    description=card.get("description"),
                    status=card.get("status", card.get("column", "backlog")),
                    priority=card.get("priority"),
                    source="board",
                    workspace_path=card.get("workspace_path"),
                    labels=card.get("labels", []),
                    assignee=card.get("assignee"),
                ).model_dump())
    except Exception:
        pass  # Board service may not be available

    # Include synced tasks from external providers (Asana, GitHub, etc.)
    try:
        import json  # noqa: I001

        from sidecar.services.sync.engine import engine as sync_engine

        synced = sync_engine.get_all_tasks()
        for t in synced:
            labels_raw = t.get("labels", "[]")
            labels = json.loads(labels_raw) if isinstance(labels_raw, str) else (labels_raw or [])
            # Parse extra_data JSON
            extra_raw = t.get("extra_data", "{}")
            extra = json.loads(extra_raw) if isinstance(extra_raw, str) else (extra_raw or {})
            client_slug = t.get("client_slug", "")
            unified.append(UnifiedCard(
                id=t.get("id", ""),
                display_id=f"SYNC-{t.get('remote_id', '')[:6].upper()}",
                title=t.get("title", ""),
                description=t.get("description"),
                status=_normalize_status(t.get("status", "backlog")),
                priority=t.get("priority"),
                source=f"sync:{client_slug}",
                workspace_path=None,
                labels=labels,
                assignee=t.get("assignee"),
                due_date=t.get("due_date"),
                start_date=extra.get("start_date"),
                completed_at=extra.get("completed_at"),
                created_at=extra.get("created_at") or t.get("created_at"),
                remote_url=t.get("remote_url"),
                subtask_count=extra.get("subtask_count", 0),
                custom_fields=extra.get("custom_fields", {}),
                sync_provider=client_slug.split(":")[0] if ":" in client_slug else None,
                sync_remote_id=t.get("remote_id"),
                client_name=client_slug,
            ).model_dump())
    except Exception:
        pass  # Sync engine may not be initialized

    return unified


@router.post("/execute")
async def tasks_execute(req: TaskExecuteRequest):
    """Delegate task to Claude Code — assigns to 'claude', moves to in_progress, fires execution."""
    # Set assignee to claude and move to in_progress
    update_task(req.task_id, assignee="claude")
    move_task(req.task_id, "in_progress")

    # Fire execution
    result = execute_task(req.task_id, req.workspace_path, req.model)

    task = get_task(req.task_id)
    return task


@router.post("/cancel-execution")
async def tasks_cancel_execution(req: TaskCancelRequest):
    """Cancel a running task execution."""
    cancelled = cancel_task_execution(req.task_id)
    task = get_task(req.task_id)
    return {"ok": cancelled, "task": task}
