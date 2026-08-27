"""Notification service — in-app notifications for card changes."""

from __future__ import annotations

import json
import logging
import uuid
from datetime import datetime
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, Field

logger = logging.getLogger(__name__)

NOTIFICATIONS_FILE = Path.home() / ".tentaqles" / "notifications.json"


class Notification(BaseModel):
    id: str = ""
    type: Literal["comment_added", "status_changed", "assignee_changed", "priority_changed", "card_deleted"] = "status_changed"
    card_id: str = ""
    board_id: str = ""
    actor: str = ""
    message: str = ""
    created: str = ""
    seen: bool = False
    dismissed: bool = False


def _read_all() -> list[Notification]:
    if not NOTIFICATIONS_FILE.exists():
        return []
    try:
        data = json.loads(NOTIFICATIONS_FILE.read_text(encoding="utf-8"))
        return [Notification(**n) for n in data]
    except (json.JSONDecodeError, ValueError):
        return []


def _write_all(notifications: list[Notification]) -> None:
    NOTIFICATIONS_FILE.parent.mkdir(parents=True, exist_ok=True)
    NOTIFICATIONS_FILE.write_text(
        json.dumps([n.model_dump() for n in notifications], indent=2),
        encoding="utf-8",
    )


def create_notification(
    type: str,
    card_id: str,
    board_id: str,
    actor: str,
    message: str,
) -> Notification:
    n = Notification(
        id=f"notif-{uuid.uuid4().hex[:8]}",
        type=type,
        card_id=card_id,
        board_id=board_id,
        actor=actor,
        message=message,
        created=datetime.now().isoformat(timespec="seconds"),
    )
    all_notifs = _read_all()
    all_notifs.insert(0, n)
    # Keep max 200 notifications
    all_notifs = all_notifs[:200]
    _write_all(all_notifs)
    return n


def list_notifications(include_dismissed: bool = False) -> list[Notification]:
    all_notifs = _read_all()
    if not include_dismissed:
        all_notifs = [n for n in all_notifs if not n.dismissed]
    return all_notifs


def mark_seen(notification_id: str) -> bool:
    all_notifs = _read_all()
    for n in all_notifs:
        if n.id == notification_id:
            n.seen = True
            _write_all(all_notifs)
            return True
    return False


def dismiss_notification(notification_id: str) -> bool:
    all_notifs = _read_all()
    for n in all_notifs:
        if n.id == notification_id:
            n.dismissed = True
            _write_all(all_notifs)
            return True
    return False


def clear_all() -> None:
    if NOTIFICATIONS_FILE.exists():
        NOTIFICATIONS_FILE.unlink()
