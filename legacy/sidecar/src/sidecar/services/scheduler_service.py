"""Scheduler service — manages scheduled Claude Code jobs."""

from __future__ import annotations

import asyncio
import json
import logging
import re
import subprocess
import time
import uuid
from datetime import date, datetime
from pathlib import Path

from sidecar.models import ScheduledJob
from sidecar.parsers import read_file_content

logger = logging.getLogger(__name__)

_tasks: dict[str, asyncio.Task] = {}  # job_id -> timer task


def _jobs_dir(workspace_path: str) -> Path:
    return Path(workspace_path) / "jobs"


def _history_dir(workspace_path: str, job_id: str) -> Path:
    return Path(workspace_path) / "jobs" / "history" / job_id


def create_job(
    workspace_path: str,
    name: str,
    schedule: str,
    prompt: str,
    model: str = "sonnet",
    enabled: bool = True,
) -> ScheduledJob:
    """Create a new scheduled job."""
    job = ScheduledJob(
        id=f"job-{uuid.uuid4().hex[:8]}",
        name=name,
        schedule=schedule,
        workspace=workspace_path,
        prompt=prompt,
        model=model,
        enabled=enabled,
        created=date.today().isoformat(),
    )
    jobs_dir = _jobs_dir(workspace_path)
    jobs_dir.mkdir(parents=True, exist_ok=True)
    path = jobs_dir / f"{job.id}.json"
    path.write_text(job.model_dump_json(indent=2), encoding="utf-8")
    return job


def update_job(workspace_path: str, job_id: str, **kwargs) -> ScheduledJob:
    """Update a job's fields."""
    path = _jobs_dir(workspace_path) / f"{job_id}.json"
    content = read_file_content(str(path))
    if content is None:
        raise ValueError(f"Job not found: {job_id}")

    data = json.loads(content)
    for k, v in kwargs.items():
        if v is not None and k in data:
            data[k] = v

    job = ScheduledJob(**data)
    path.write_text(job.model_dump_json(indent=2), encoding="utf-8")
    return job


def delete_job(workspace_path: str, job_id: str) -> None:
    """Delete a job definition."""
    path = _jobs_dir(workspace_path) / f"{job_id}.json"
    if path.exists():
        path.unlink()


def list_jobs(workspace_path: str) -> list[ScheduledJob]:
    """List all jobs for a workspace."""
    jobs_dir = _jobs_dir(workspace_path)
    if not jobs_dir.is_dir():
        return []

    results: list[ScheduledJob] = []
    for f in sorted(jobs_dir.glob("*.json")):
        if f.parent.name == "history":
            continue
        try:
            data = json.loads(f.read_text(encoding="utf-8"))
            results.append(ScheduledJob(**data))
        except (json.JSONDecodeError, ValueError) as e:
            logger.warning(f"Failed to load job {f}: {e}")
    return results


async def _execute_job(job: ScheduledJob) -> None:
    """Execute a job by spawning claude CLI subprocess."""
    timestamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    start = time.monotonic()

    try:
        # Build claude CLI command
        cmd = ["claude", "-p", "--output-format", "json", "--model", job.model, job.prompt]

        result = subprocess.run(
            cmd,
            cwd=job.workspace,
            capture_output=True,
            text=True,
            timeout=300,  # 5 minute timeout
        )

        duration = (time.monotonic() - start) * 1000
        output = result.stdout[:10000] if result.stdout else ""
        error = result.stderr[:2000] if result.returncode != 0 else None
        status = "success" if result.returncode == 0 else "error"

        # Parse token usage from JSON output if possible
        cost_usd = 0.0
        input_tokens = 0
        output_tokens = 0
        try:
            data = json.loads(result.stdout)
            if isinstance(data, dict):
                cost_usd = data.get("cost_usd", 0.0)
                input_tokens = data.get("input_tokens", 0)
                output_tokens = data.get("output_tokens", 0)
                # Extract text content
                if "result" in data:
                    output = data["result"]
        except (json.JSONDecodeError, ValueError):
            pass

    except subprocess.TimeoutExpired:
        duration = (time.monotonic() - start) * 1000
        output = ""
        error = "Job timed out after 5 minutes"
        status = "error"
        cost_usd = 0.0
        input_tokens = 0
        output_tokens = 0
    except Exception as e:
        duration = (time.monotonic() - start) * 1000
        output = ""
        error = str(e)
        status = "error"
        cost_usd = 0.0
        input_tokens = 0
        output_tokens = 0

    # Update job status
    job.last_run = timestamp
    job.last_status = status
    job_path = _jobs_dir(job.workspace) / f"{job.id}.json"
    if job_path.exists():
        job_path.write_text(job.model_dump_json(indent=2), encoding="utf-8")

    # Save to history
    hist_dir = _history_dir(job.workspace, job.id)
    hist_dir.mkdir(parents=True, exist_ok=True)
    run_result = {
        "job_id": job.id,
        "timestamp": timestamp,
        "status": status,
        "output": output,
        "error": error,
        "duration_ms": duration,
        "cost_usd": cost_usd,
        "input_tokens": input_tokens,
        "output_tokens": output_tokens,
    }
    (hist_dir / f"{timestamp}.json").write_text(json.dumps(run_result, indent=2), encoding="utf-8")

    logger.info(f"Job {job.id} ({job.name}) completed: {status} in {duration:.0f}ms")


def trigger_job(workspace_path: str, job_id: str) -> str:
    """Trigger immediate execution of a job."""
    path = _jobs_dir(workspace_path) / f"{job_id}.json"
    content = read_file_content(str(path))
    if content is None:
        return f"Job not found: {job_id}"

    try:
        job = ScheduledJob(**json.loads(content))
    except (json.JSONDecodeError, ValueError) as e:
        return f"Invalid job: {e}"

    asyncio.create_task(_execute_job(job))
    return f"Job {job_id} triggered for execution"


async def _schedule_loop(job: ScheduledJob) -> None:
    """Run a job on its schedule interval."""
    interval = parse_interval_seconds(job.schedule) or parse_cron_next_seconds(job.schedule) or 3600
    if interval == 3600 and not parse_interval_seconds(job.schedule) and not parse_cron_next_seconds(job.schedule):
        logger.warning(f"Unrecognized schedule '{job.schedule}' for job {job.id}, defaulting to 1h")

    logger.info(f"Scheduled job {job.id} ({job.name}) every {interval}s")
    while True:
        await asyncio.sleep(interval)
        # Re-read job from disk to check if still enabled
        path = _jobs_dir(job.workspace) / f"{job.id}.json"
        content = read_file_content(str(path))
        if content is None:
            logger.info(f"Job {job.id} deleted, stopping schedule loop")
            break
        try:
            current_job = ScheduledJob(**json.loads(content))
        except (json.JSONDecodeError, ValueError):
            break
        if not current_job.enabled:
            continue
        await _execute_job(current_job)


def start_scheduler(workspace_paths: list[str]) -> int:
    """Start schedule loops for all enabled jobs across workspaces. Returns count."""
    count = 0
    for ws in workspace_paths:
        for job in list_jobs(ws):
            if job.enabled and job.id not in _tasks:
                _tasks[job.id] = asyncio.create_task(_schedule_loop(job))
                count += 1
    return count


def stop_scheduler() -> None:
    """Cancel all running schedule loops."""
    for task in _tasks.values():
        if not task.done():
            task.cancel()
    _tasks.clear()


def get_job_history(workspace_path: str, job_id: str, limit: int = 20) -> list[dict]:
    """Get run history for a job."""
    hist_dir = _history_dir(workspace_path, job_id)
    if not hist_dir.is_dir():
        return []

    results: list[dict] = []
    for f in sorted(hist_dir.glob("*.json"), reverse=True)[:limit]:
        try:
            results.append(json.loads(f.read_text(encoding="utf-8")))
        except (json.JSONDecodeError, ValueError):
            pass
    return results


def parse_interval_seconds(schedule: str) -> int | None:
    """Parse simple interval strings like 'every 30m', 'every 2h', 'every 1d'."""
    m = re.match(r"every\s+(\d+)\s*([mhd])", schedule, re.IGNORECASE)
    if not m:
        return None
    value, unit = int(m.group(1)), m.group(2).lower()
    multipliers = {"m": 60, "h": 3600, "d": 86400}
    return value * multipliers.get(unit)


def parse_cron_next_seconds(schedule: str) -> int | None:
    """Parse cron-like expressions and return seconds until next run.

    Supports:
    - Standard 5-field cron: "0 9 * * 1-5" (min hour dom month dow)
    - Simple shortcuts: "@hourly", "@daily", "@weekly"
    """
    shortcuts = {
        "@hourly": 3600,
        "@daily": 86400,
        "@weekly": 604800,
    }
    if schedule in shortcuts:
        return shortcuts[schedule]

    # Basic cron parsing: extract minute and hour, compute interval
    parts = schedule.strip().split()
    if len(parts) != 5:
        return None

    minute, hour = parts[0], parts[1]

    # If specific hour and minute, run daily
    if hour != "*" and minute != "*":
        return 86400  # Once a day
    # If specific minute, hourly
    elif hour == "*" and minute != "*":
        return 3600  # Once an hour
    # Every minute
    elif hour == "*" and minute == "*":
        return 60

    return 3600  # Default fallback
