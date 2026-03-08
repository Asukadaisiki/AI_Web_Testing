"""FastAPI application entrypoint."""

from fastapi import FastAPI

from app.api.router import build_api_router
from app.core.config import get_settings


def create_app() -> FastAPI:
    settings = get_settings()
    app = FastAPI(
        title=settings.app_name,
        version=settings.app_version,
        debug=settings.debug,
    )
    app.include_router(build_api_router())

    @app.get("/", tags=["meta"], summary="Service metadata")
    def read_root() -> dict[str, str]:
        return {
            "name": settings.app_name,
            "environment": settings.app_env,
            "docs_url": "/docs",
        }

    return app


app = create_app()
