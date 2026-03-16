"""Runtime settings routes."""

from __future__ import annotations

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session

from app.db import get_db_session
from app.schemas.settings import AISettingsOverviewResponse, AISettingsResponse, AISettingsUpdateRequest
from app.services.settings import get_ai_settings, get_ai_settings_overview, update_ai_settings


router = APIRouter(prefix="/settings", tags=["settings"])


@router.get("/ai", response_model=AISettingsResponse, summary="Get runtime AI settings")
def get_ai_settings_route() -> AISettingsResponse:
    return get_ai_settings()


@router.get("/ai/overview", response_model=AISettingsOverviewResponse, summary="Get AI settings overview")
def get_ai_settings_overview_route(session: Session = Depends(get_db_session)) -> AISettingsOverviewResponse:
    return get_ai_settings_overview(session)


@router.put("/ai", response_model=AISettingsResponse, summary="Update runtime AI settings")
def update_ai_settings_route(payload: AISettingsUpdateRequest) -> AISettingsResponse:
    return update_ai_settings(payload)
