"""Scheduler router — manages scheduled Claude Code jobs."""

from fastapi import APIRouter

from sidecar.schemas import (
    JobCreateRequest,
    JobHistoryRequest,
    JobIdRequest,
    JobListRequest,
    JobUpdateRequest,
)
from sidecar.services.scheduler_service import (
    create_job,
    delete_job,
    get_job_history,
    list_jobs,
    start_scheduler,
    stop_scheduler,
    trigger_job,
    update_job,
)

router = APIRouter()


@router.post("/list")
def scheduler_list(req: JobListRequest):
    jobs = list_jobs(req.workspace_path)
    return [j.model_dump() for j in jobs]


@router.post("/create")
def scheduler_create(req: JobCreateRequest):
    job = create_job(req.workspace_path, req.name, req.schedule, req.prompt, req.model, req.enabled)
    return job.model_dump()


@router.post("/update")
def scheduler_update(req: JobUpdateRequest):
    kwargs = {k: v for k, v in req.model_dump().items() if k not in ("workspace_path", "job_id") and v is not None}
    job = update_job(req.workspace_path, req.job_id, **kwargs)
    return job.model_dump()


@router.post("/delete")
def scheduler_delete(req: JobIdRequest):
    delete_job(req.workspace_path, req.job_id)
    return {"ok": True}


@router.post("/trigger")
async def scheduler_trigger(req: JobIdRequest):
    msg = trigger_job(req.workspace_path, req.job_id)
    return {"ok": True, "message": msg}


@router.post("/start")
async def scheduler_start(req: JobListRequest):
    count = start_scheduler([req.workspace_path])
    return {"ok": True, "scheduled": count}


@router.post("/stop")
async def scheduler_stop():
    stop_scheduler()
    return {"ok": True}


@router.post("/history")
def scheduler_history(req: JobHistoryRequest):
    return get_job_history(req.workspace_path, req.job_id, req.limit)
