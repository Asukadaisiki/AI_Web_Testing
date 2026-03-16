"""Runtime settings routes."""

from __future__ import annotations

from fastapi import APIRouter

from app.schemas.settings import AISettingsResponse, AISettingsUpdateRequest
from app.services.settings import get_ai_settings, update_ai_settings


router = APIRouter(prefix="/settings", tags=["settings"])


@router.get("/ai", response_model=AISettingsResponse, summary="Get runtime AI settings")
def get_ai_settings_route() -> AISettingsResponse:
    return get_ai_settings()


@router.put("/ai", response_model=AISettingsResponse, summary="Update runtime AI settings")
def update_ai_settings_route(payload: AISettingsUpdateRequest) -> AISettingsResponse:
    return update_ai_settings(payload)
