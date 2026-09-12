"""FastAPI application entrypoint."""

import os
from contextlib import asynccontextmanager
from pathlib import Path

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from browser_worker.capabilities.browser_capabilities import (
    shutdown_browser_capabilities,
)
from browser_worker.runtime.config import get_settings
from browser_worker.runtime.idempotency import IdempotencyMiddleware
from browser_worker.runtime.logging_config import get_uvicorn_log_config, setup_logging
from browser_worker.runtime.paths import PROJECT_ROOT
from browser_worker.runtime.rate_limit import RateLimitMiddleware
from browser_worker.runtime.request_logging import RequestLoggingMiddleware
from browser_worker.server.router import build_api_router
from browser_worker.server.routes.artifacts import router as artifacts_router

ARTIFACTS_DIR = PROJECT_ROOT / "artifacts"


@asynccontextmanager
async def lifespan(_app: FastAPI):
    del _app
    yield
    shutdown_browser_capabilities()


def create_app() -> FastAPI:
    setup_logging()
    settings = get_settings()
    ARTIFACTS_DIR.mkdir(parents=True, exist_ok=True)
    STORAGE_STATES_DIR = Path(settings.storage_state_dir)
    STORAGE_STATES_DIR.mkdir(parents=True, exist_ok=True)
    app = FastAPI(
        title=settings.app_name,
        version=settings.app_version,
        debug=settings.debug,
        lifespan=lifespan,
    )
    app.state.artifacts_dir = ARTIFACTS_DIR
    app.state.storage_states_dir = STORAGE_STATES_DIR
    app.add_middleware(RequestLoggingMiddleware)
    app.add_middleware(
        RateLimitMiddleware,
        max_requests=settings.rate_limit_max_requests,
        window_seconds=settings.rate_limit_window_seconds,
    )
    app.add_middleware(IdempotencyMiddleware)
    app.add_middleware(
        CORSMiddleware,
        allow_origins=settings.cors_allow_origins,
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )
    app.include_router(build_api_router())
    app.include_router(artifacts_router)

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
    uvicorn.run(
        "browser_worker.server.main:create_app",
        host=host,
        port=port,
        reload=True,
        factory=True,
        log_config=get_uvicorn_log_config(),
    )


if __name__ == "__main__":
    main()
