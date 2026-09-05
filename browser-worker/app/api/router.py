"""API router assembly."""

from fastapi import APIRouter

from app.api.routes.browser_capabilities import router as browser_capabilities_router
from app.api.routes.health import router as health_router
from app.core.config import get_settings


def build_api_router() -> APIRouter:
    settings = get_settings()
    api_router = APIRouter(prefix=settings.api_v1_prefix)
    api_router.include_router(health_router)
    api_router.include_router(browser_capabilities_router)
    return api_router
