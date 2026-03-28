"""API router assembly."""

from fastapi import APIRouter, Depends

from app.api.auth import require_authenticated_user
from app.api.routes.auth import router as auth_router
from app.api.routes.cases import router as cases_router
from app.api.routes.corrections import router as corrections_router
from app.api.routes.dsl import router as dsl_router
from app.api.routes.executions import router as executions_router
from app.api.routes.health import router as health_router
from app.api.routes.projects import router as projects_router
from app.api.routes.reports import router as reports_router
from app.api.routes.settings import router as settings_router
from app.api.routes.suites import router as suites_router
from app.core.config import get_settings


def build_api_router() -> APIRouter:
    settings = get_settings()
    api_router = APIRouter(prefix=settings.api_v1_prefix)
    api_router.include_router(health_router)
    api_router.include_router(auth_router)
    api_router.include_router(cases_router, dependencies=[Depends(require_authenticated_user)])
    api_router.include_router(corrections_router, dependencies=[Depends(require_authenticated_user)])
    api_router.include_router(suites_router, dependencies=[Depends(require_authenticated_user)])
    api_router.include_router(dsl_router, dependencies=[Depends(require_authenticated_user)])
    api_router.include_router(settings_router, dependencies=[Depends(require_authenticated_user)])
    api_router.include_router(executions_router, dependencies=[Depends(require_authenticated_user)])
    api_router.include_router(projects_router, dependencies=[Depends(require_authenticated_user)])
    api_router.include_router(reports_router, dependencies=[Depends(require_authenticated_user)])
    return api_router
