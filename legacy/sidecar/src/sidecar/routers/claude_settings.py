"""Claude Settings router — settings.json management (excluding hooks/permissions)."""

from fastapi import APIRouter
from pydantic import BaseModel

from sidecar.schemas import ScopedRequest
from sidecar.services.claude_settings_service import (
    get_claude_settings,
    get_setting_value,
    save_claude_settings,
    set_setting_value,
)

router = APIRouter()


class SettingsSaveRequest(ScopedRequest):
    settings: dict


class SettingValueRequest(ScopedRequest):
    key: str


class SettingValueSetRequest(SettingValueRequest):
    value: object


@router.post("/get")
def settings_get(req: ScopedRequest):
    return get_claude_settings(req.scope, req.workspace_path)


@router.post("/save")
def settings_save(req: SettingsSaveRequest):
    save_claude_settings(req.scope, req.workspace_path, req.settings)
    return {"ok": True}


@router.post("/get-value")
def settings_get_value(req: SettingValueRequest):
    return {"value": get_setting_value(req.scope, req.workspace_path, req.key)}


@router.post("/set-value")
def settings_set_value(req: SettingValueSetRequest):
    set_setting_value(req.scope, req.workspace_path, req.key, req.value)
    return {"ok": True}
