"""FastAPI application entrypoint."""

import os
from pathlib import Path

from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles
import uvicorn

from app.api.router import build_api_router
from app.core.config import get_settings
from app.db import verify_database_connection


ARTIFACTS_DIR = Path(__file__).resolve().parents[1] / "artifacts"


def create_app() -> FastAPI:
    settings = get_settings()
    verify_database_connection()
    ARTIFACTS_DIR.mkdir(parents=True, exist_ok=True)
    app = FastAPI(
        title=settings.app_name,
        version=settings.app_version,
        debug=settings.debug,
    )
    app.include_router(build_api_router())
    app.mount("/artifacts", StaticFiles(directory=ARTIFACTS_DIR), name="artifacts")

    @app.get("/", tags=["meta"], summary="Service metadata")
    def read_root() -> dict[str, str]:
        return {
            "name": settings.app_name,
            "environment": settings.app_env,
            "docs_url": "/docs",
        }

    return app

def main() -> None:
    host = os.getenv("APP_HOST", "127.0.0.1")
    port = int(os.getenv("APP_PORT", "8000"))
    uvicorn.run("app.main:create_app", host=host, port=port, reload=True, factory=True)


if __name__ == "__main__":
    main()
