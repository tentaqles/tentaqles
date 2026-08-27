"""Notifications router."""

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.services.notification_service import (
    clear_all,
    dismiss_notification,
    list_notifications,
    mark_seen,
)

router = APIRouter()


class NotificationIdRequest(BaseModel):
    notification_id: str


@router.post("/list")
def notifications_list():
    return [n.model_dump() for n in list_notifications()]


@router.post("/mark-seen")
def notifications_mark_seen(req: NotificationIdRequest):
    return {"ok": mark_seen(req.notification_id)}


@router.post("/dismiss")
def notifications_dismiss(req: NotificationIdRequest):
    return {"ok": dismiss_notification(req.notification_id)}


@router.post("/clear")
def notifications_clear():
    clear_all()
    return {"ok": True}
