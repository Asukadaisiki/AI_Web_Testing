"""FastAPI app 工厂（HTTP API，Go 侧依赖本文件的路径与响应形态）。

| 方法 | 路径 | body | 返回 |
|---|---|---|---|
| GET | `/health` | — | `{"status":"ok","artifacts_dir":"..."}` |
| POST | `/sessions` | `{"session_id":"sess_..."}` | `{"session_id":"bsess_..."}` |
| POST | `/sessions/{id}/navigate` | `{"url":"..."}` | Observation |
| POST | `/sessions/{id}/act` | `{"action":"click"|"input","locator":{...},"value":"..."}` | Observation |
| DELETE | `/sessions/{id}` | — | `{"closed":true}` |
| POST | `/execute` | `{"session_id":"sess_...","case":{...}}` | ExecutionResult |

路径里的 `{id}` 是**浏览器会话**句柄（`bsess_...`）；body 里的 `session_id` 是**领域会话**
（`sess_...`，CONTRACT §9）。两者不是一回事：前者用完即弃，后者决定证据落哪个目录。

错误响应统一 `{"error":"<code>","detail":"<人类可读>"}`：会话不存在 404，case 非法 400
（`error` 用 `CaseInvalid.code`，与 Go 侧 `internal/contract` 的错误码逐字一致），
其余执行器故障 500。
"""

from __future__ import annotations

import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse

from .actions import ActionFailure
from .contracts import (
    CODE_INVALID_JSON,
    ERROR_SESSION_NOT_FOUND,
    ERROR_WORKER_ERROR,
    ActRequest,
    CaseInvalid,
    ExecuteRequest,
    ExecutionResult,
    NavigateRequest,
    Observation,
    OpenSessionRequest,
    validate_case,
)
from .evidence import artifacts_dir
from .runner import run_case
from .sessions import SessionManager, SessionNotFound

log = logging.getLogger("loop_worker")

ERROR_INVALID_REQUEST = "invalid_request"


def _error(status_code: int, code: str, detail: str) -> JSONResponse:
    return JSONResponse(status_code=status_code, content={"error": code, "detail": detail})


def _describe_validation_error(exc: RequestValidationError) -> str:
    parts: list[str] = []
    for err in exc.errors()[:5]:
        loc = ".".join(str(p) for p in err.get("loc", ())) or "<body>"
        parts.append(f"{loc}: {err.get('msg')}")
    return "; ".join(parts) or "request body is invalid"


def create_app() -> FastAPI:
    """构造 FastAPI app；每个 app 拥有自己的会话表。"""
    manager = SessionManager()

    @asynccontextmanager
    async def lifespan(app: FastAPI):  # noqa: ARG001
        try:
            yield
        finally:
            await manager.shutdown()

    app = FastAPI(title="loop-worker", version="0.1.0", lifespan=lifespan)
    app.state.session_manager = manager

    # ---- 统一错误信封 ----

    @app.exception_handler(SessionNotFound)
    async def _on_session_not_found(request: Request, exc: SessionNotFound) -> JSONResponse:  # noqa: ARG001
        return _error(404, ERROR_SESSION_NOT_FOUND, str(exc))

    @app.exception_handler(CaseInvalid)
    async def _on_case_invalid(request: Request, exc: CaseInvalid) -> JSONResponse:  # noqa: ARG001
        return _error(400, exc.code, exc.detail)

    @app.exception_handler(ActionFailure)
    async def _on_action_failure(request: Request, exc: ActionFailure) -> JSONResponse:  # noqa: ARG001
        return _error(400, exc.kind, exc.detail)

    @app.exception_handler(RequestValidationError)
    async def _on_bad_body(request: Request, exc: RequestValidationError) -> JSONResponse:
        code = CODE_INVALID_JSON if request.url.path == "/execute" else ERROR_INVALID_REQUEST
        return _error(400, code, _describe_validation_error(exc))

    @app.exception_handler(Exception)
    async def _on_unhandled(request: Request, exc: Exception) -> JSONResponse:  # noqa: ARG001
        log.exception("unhandled worker error")
        return _error(500, ERROR_WORKER_ERROR, f"{type(exc).__name__}: {exc}")

    # ---- 路由 ----

    @app.get("/health")
    async def health() -> dict[str, str]:
        # artifacts_dir 是给控制面对账用的：证据图片由控制面的 /artifacts/ 提供，
        # 两边目录不一致时图片会静默 404，只有把路径报出去才查得出来。
        return {"status": "ok", "artifacts_dir": str(artifacts_dir())}

    @app.post("/sessions")
    async def create_session(body: OpenSessionRequest) -> dict[str, str]:
        # 返回的是浏览器会话句柄；body.session_id（领域会话）记在会话上，决定证据目录。
        session = await manager.create(body.session_id)
        return {"session_id": session.browser_session_id}

    @app.post("/sessions/{session_id}/navigate")
    async def navigate(session_id: str, body: NavigateRequest) -> Observation:
        return await manager.navigate(session_id, body.url)

    @app.post("/sessions/{session_id}/act")
    async def act(session_id: str, body: ActRequest) -> Observation:
        return await manager.act(session_id, body)

    @app.delete("/sessions/{session_id}")
    async def delete_session(session_id: str) -> dict[str, bool]:
        await manager.close(session_id)
        return {"closed": True}

    @app.post("/execute")
    async def execute(body: ExecuteRequest) -> ExecutionResult:
        # 先校验：契约非法一律 400，且不浪费一次浏览器启动
        case = validate_case(body.case)
        browser = await manager.browser()
        return await run_case(case, browser=browser, session_id=body.session_id)

    return app


app = create_app()


__all__ = ["app", "create_app"]
