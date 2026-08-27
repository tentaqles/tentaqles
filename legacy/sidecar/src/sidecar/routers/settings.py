"""Settings router — global app settings."""

from fastapi import APIRouter

from sidecar.models import GlobalManagerState
from sidecar.schemas import GetSettingsRequest, SaveSettingsRequest
from sidecar.services.toggle_service import get_global_state, save_global_state

router = APIRouter()


@router.post("/get")
def get_settings(req: GetSettingsRequest):
    return get_global_state(req.base_path).model_dump(by_alias=True)


@router.post("/save")
def save_settings(req: SaveSettingsRequest):
    state = GlobalManagerState.model_validate(req.state)
    save_global_state(req.base_path, state)
    return {"ok": True}
