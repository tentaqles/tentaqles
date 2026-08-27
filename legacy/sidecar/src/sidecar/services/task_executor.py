"""Task executor — spawns Claude Code sessions for delegated tasks."""

from __future__ import annotations

import asyncio
import json
import logging
import time
from datetime import datetime

from sidecar.services.task_service import get_task, move_task, update_task

logger = logging.getLogger(__name__)

# Track running executions: task_id -> asyncio.Task
_running: dict[str, asyncio.Task] = {}


def _build_task_prompt(task: dict) -> str:
    """Build a rich Claude prompt from task content and context."""
    parts = [f"# Task: {task['title']}"]

    if task.get("client_name"):
        parts.append(f"\n## Client: {task['client_name']}")
    if task.get("project_path"):
        parts.append(f"## Project: {task['project_path']}")

    if task.get("description"):
        parts.append(f"\n## Description\n{task['description']}")

    if task.get("checklist"):
        parts.append("\n## Checklist")
        for item in task["checklist"]:
            status = "x" if item.get("done") else " "
            parts.append(f"- [{status}] {item['text']}")

    if task.get("labels"):
        parts.append(f"\n## Labels: {', '.join(task['labels'])}")

    parts.append("\n## Instructions")
    parts.append("Complete this task thoroughly. Read the project's CLAUDE.md and any .claude/rules/ for context.")
    parts.append("Commit your changes when done.")

    return "\n".join(parts)


async def _execute_task(
    task_id: str,
    workspace_path: str,
    model: str = "sonnet",
) -> None:
    """Run Claude Code for a task and update on completion."""
    task = get_task(task_id)
    prompt = _build_task_prompt(task)

    start = time.monotonic()
    started_at = datetime.now().isoformat(timespec="seconds")

    # Mark as running
    execution = {
        "state": "running",
        "started_at": started_at,
        "finished_at": None,
        "output": "",
        "error": None,
        "duration_ms": 0,
        "cost_usd": 0,
        "input_tokens": 0,
        "output_tokens": 0,
        "session_id": None,
    }
    update_task(task_id, execution=execution)

    try:
        cmd = [
            "claude",
            "-p",
            "--dangerously-skip-permissions",
            "--output-format", "json",
            "--model", model,
            prompt,
        ]

        logger.info("Executing task %s: claude -p in %s", task_id, workspace_path)

        proc = await asyncio.create_subprocess_exec(
            *cmd,
            cwd=workspace_path,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        stdout, stderr = await asyncio.wait_for(proc.communicate(), timeout=600)  # 10 min timeout

        duration = (time.monotonic() - start) * 1000
        finished_at = datetime.now().isoformat(timespec="seconds")

        output_text = stdout.decode("utf-8", errors="replace")[:20000] if stdout else ""
        error_text = stderr.decode("utf-8", errors="replace")[:5000] if stderr else None

        # Parse JSON output for token/cost info
        cost_usd = 0.0
        input_tokens = 0
        output_tokens = 0
        session_id = None
        result_text = output_text

        try:
            data = json.loads(output_text)
            if isinstance(data, dict):
                cost_usd = data.get("cost_usd", 0.0)
                input_tokens = data.get("input_tokens", 0)
                output_tokens = data.get("output_tokens", 0)
                session_id = data.get("session_id")
                if "result" in data:
                    result_text = data["result"]
        except (json.JSONDecodeError, ValueError):
            pass

        state = "success" if proc.returncode == 0 else "error"
        execution = {
            "state": state,
            "started_at": started_at,
            "finished_at": finished_at,
            "output": result_text,
            "error": error_text if state == "error" else None,
            "duration_ms": duration,
            "cost_usd": cost_usd,
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
            "session_id": session_id,
        }

    except TimeoutError:
        duration = (time.monotonic() - start) * 1000
        execution = {
            "state": "error",
            "started_at": started_at,
            "finished_at": datetime.now().isoformat(timespec="seconds"),
            "output": "",
            "error": "Execution timed out after 10 minutes",
            "duration_ms": duration,
            "cost_usd": 0,
            "input_tokens": 0,
            "output_tokens": 0,
            "session_id": None,
        }
    except Exception as e:
        duration = (time.monotonic() - start) * 1000
        execution = {
            "state": "error",
            "started_at": started_at,
            "finished_at": datetime.now().isoformat(timespec="seconds"),
            "output": "",
            "error": str(e),
            "duration_ms": duration,
            "cost_usd": 0,
            "input_tokens": 0,
            "output_tokens": 0,
            "session_id": None,
        }

    # Update task with execution result
    update_task(task_id, execution=execution)

    # Auto-move to review on success
    if execution["state"] == "success":
        move_task(task_id, "in_review")
        logger.info("Task %s completed, moved to in_review", task_id)

    # Clean up tracking
    _running.pop(task_id, None)
    logger.info("Task %s execution finished: %s (%.0fms)", task_id, execution["state"], execution["duration_ms"])


def execute_task(
    task_id: str,
    workspace_path: str,
    model: str = "sonnet",
) -> str:
    """Fire a Claude Code session for a task. Returns immediately."""
    if task_id in _running:
        return f"Task {task_id} is already executing"

    task = asyncio.create_task(
        _execute_task(task_id, workspace_path, model)
    )
    _running[task_id] = task
    return f"Execution started for task {task_id}"


def get_task_execution_status(task_id: str) -> str:
    """Check if a task is currently executing."""
    if task_id in _running:
        t = _running[task_id]
        return "running" if not t.done() else "finished"
    return "idle"


def cancel_task_execution(task_id: str) -> bool:
    """Cancel a running task execution."""
    if task_id in _running:
        t = _running[task_id]
        if not t.done():
            t.cancel()
            _running.pop(task_id, None)
            return True
    return False
