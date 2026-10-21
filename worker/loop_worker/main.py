"""FastAPI app 工厂（HTTP API，Go 侧依赖本文件的路径与响应形态）。

| 方法 | 路径 | body | 返回 |
|---|---|---|---|
| GET | `/health` | — | `{"status":"ok","artifacts_dir":"..."}` |
| POST | `/sessions` | `{"session_id":"sess_..."}` | `{"session_id":"bsess_..."}` |
| POST | `/sessions/{id}/navigate` | `{"url":"..."}` | Observation |
| POST | `/sessions/{id}/act` | `{"action":"click"|"input","locator":{...},"postconditions":[...]}` | ActResponse |
| DELETE | `/sessions/{id}` | — | `{"closed":true}` |
| POST | `/execute` | `{"session_id":"sess_...","case":{...}}` | ExecutionResult |
| GET | `/operations` | — | Worker-owned operation metadata |
| POST | `/operations/browser_execute_case` | execute body + trace headers | OperationResult |

路径里的 `{id}` 是**浏览器会话**句柄（`bsess_...`）；body 里的 `session_id` 是**领域会话**
（`sess_...`，CONTRACT §9）。两者不是一回事：前者用完即弃，后者决定证据落哪个目录。

错误响应统一 `{"error":"<code>","detail":"<人类可读>"}`：会话不存在 404，case 非法 400
（`error` 用 `CaseInvalid.code`，与 Go 侧 `internal/contract` 的错误码逐字一致），
其余执行器故障 500。
"""

from __future__ import annotations

import logging
from collections import defaultdict
from contextlib import asynccontextmanager

from fastapi import FastAPI, Header, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.types import ASGIApp, Message, Receive, Scope, Send

from .actions import ActionFailure
from .contracts import (
    CODE_INVALID_JSON,
    ERROR_SESSION_NOT_FOUND,
    ERROR_WORKER_ERROR,
    SIGNAL_WORKER_ERROR,
    ActRequest,
    ActResponse,
    CaseInvalid,
    ExecuteRequest,
    ExecutionResult,
    NavigateRequest,
    Observation,
    OpenSessionRequest,
    OperationArtifacts,
    OperationExecuteRequest,
    OperationResult,
    StepError,
    validate_case,
)
from .evidence import artifacts_dir
from .idempotency import (
    IdempotencyCache,
    IdempotencyConflict,
    IdempotencyOverloaded,
    canonical_request_hash,
)
from .runner import run_case
from .sessions import SessionManager, SessionNotFound

log = logging.getLogger("loop_worker")

ERROR_INVALID_REQUEST = "invalid_request"
ERROR_IDEMPOTENCY_CONFLICT = "idempotency_conflict"
ERROR_OPERATION_OVERLOADED = "operation_overloaded"
ERROR_REQUEST_TOO_LARGE = "request_too_large"

DEFAULT_OPERATION_MAX_ACTIVE = 32
DEFAULT_OPERATION_MAX_REQUEST_BYTES = 4 * 1024 * 1024
_BROWSER_OPERATION_PATH = "/operations/browser_execute_case"

_BROWSER_OPERATION_METADATA = {
    "name": "browser_execute_case",
    "description": "Execute a grounded browser evaluation case",
    "input_schema": {
        "type": "object",
        "required": ["session_id", "case"],
        "properties": {
            "session_id": {"type": "string", "minLength": 1},
            "case": {"type": "object"},
        },
        "additionalProperties": False,
    },
    "fresh_context": True,
    "disposable_fixture_data": True,
    "irreversible_production_operations": "unsupported",
}


def _error(status_code: int, code: str, detail: str) -> JSONResponse:
    return JSONResponse(status_code=status_code, content={"error": code, "detail": detail})


def _describe_validation_error(exc: RequestValidationError) -> str:
    parts: list[str] = []
    for err in exc.errors()[:5]:
        loc = ".".join(str(p) for p in err.get("loc", ())) or "<body>"
        parts.append(f"{loc}: {err.get('msg')}")
    return "; ".join(parts) or "request body is invalid"


class OperationRequestSizeLimitMiddleware:
    """Rejects oversized operation bodies before request model parsing."""

    def __init__(self, app: ASGIApp, *, max_bytes: int) -> None:
        if max_bytes < 1:
            raise ValueError("max_bytes must be at least 1")
        self._app = app
        self._max_bytes = max_bytes

    async def __call__(
        self,
        scope: Scope,
        receive: Receive,
        send: Send,
    ) -> None:
        if (
            scope["type"] != "http"
            or scope.get("method") != "POST"
            or scope.get("path") != _BROWSER_OPERATION_PATH
        ):
            await self._app(scope, receive, send)
            return

        content_length = self._content_length(scope)
        if content_length is not None and content_length > self._max_bytes:
            await self._reject(scope, receive, send)
            return

        body = bytearray()
        disconnected = False
        while True:
            message = await receive()
            if message["type"] == "http.disconnect":
                disconnected = True
                break
            if message["type"] != "http.request":
                continue
            chunk = message.get("body", b"")
            if len(body) + len(chunk) > self._max_bytes:
                await self._reject(scope, receive, send)
                return
            body.extend(chunk)
            if not message.get("more_body", False):
                break

        replayed = False

        async def replay_receive() -> Message:
            nonlocal replayed
            if not replayed:
                replayed = True
                if disconnected:
                    return {"type": "http.disconnect"}
                return {
                    "type": "http.request",
                    "body": bytes(body),
                    "more_body": False,
                }
            return await receive()

        await self._app(scope, replay_receive, send)

    @staticmethod
    def _content_length(scope: Scope) -> int | None:
        for name, value in scope.get("headers", []):
            if name.lower() != b"content-length":
                continue
            try:
                return int(value)
            except ValueError:
                return None
        return None

    async def _reject(
        self,
        scope: Scope,
        receive: Receive,
        send: Send,
    ) -> None:
        response = _error(
            413,
            ERROR_REQUEST_TOO_LARGE,
            f"request body exceeds {self._max_bytes} bytes",
        )
        await response(scope, receive, send)


def create_app(
    *,
    operation_cache_size: int = 256,
    operation_max_active: int = DEFAULT_OPERATION_MAX_ACTIVE,
    operation_max_request_bytes: int = DEFAULT_OPERATION_MAX_REQUEST_BYTES,
) -> FastAPI:
    """构造 FastAPI app；每个 app 拥有自己的会话表。"""
    manager = SessionManager()
    operation_cache: IdempotencyCache[OperationResult] = IdempotencyCache(
        max_entries=operation_cache_size,
        max_active=operation_max_active,
    )

    @asynccontextmanager
    async def lifespan(app: FastAPI):  # noqa: ARG001
        try:
            yield
        finally:
            await manager.shutdown()

    app = FastAPI(title="loop-worker", version="0.1.0", lifespan=lifespan)
    app.add_middleware(
        OperationRequestSizeLimitMiddleware,
        max_bytes=operation_max_request_bytes,
    )
    app.state.session_manager = manager
    app.state.operation_cache = operation_cache
    app.state.operation_calls = defaultdict(int)

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

    @app.exception_handler(IdempotencyConflict)
    async def _on_idempotency_conflict(
        request: Request, exc: IdempotencyConflict  # noqa: ARG001
    ) -> JSONResponse:
        return _error(409, ERROR_IDEMPOTENCY_CONFLICT, str(exc))

    @app.exception_handler(IdempotencyOverloaded)
    async def _on_idempotency_overloaded(
        request: Request, exc: IdempotencyOverloaded  # noqa: ARG001
    ) -> JSONResponse:
        return _error(503, ERROR_OPERATION_OVERLOADED, str(exc))

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

    @app.get("/operations")
    async def operations() -> dict[str, list[dict[str, object]]]:
        return {"operations": [_BROWSER_OPERATION_METADATA]}

    @app.post("/sessions")
    async def create_session(body: OpenSessionRequest) -> dict[str, str]:
        # 返回的是浏览器会话句柄；body.session_id（领域会话）记在会话上，决定证据目录。
        session = await manager.create(body.session_id)
        return {"session_id": session.browser_session_id}

    @app.post("/sessions/{session_id}/navigate")
    async def navigate(session_id: str, body: NavigateRequest) -> Observation:
        return await manager.navigate(session_id, body.url)

    @app.post("/sessions/{session_id}/act")
    async def act(session_id: str, body: ActRequest) -> ActResponse:
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

    @app.post("/operations/browser_execute_case")
    async def browser_execute_case(
        body: OperationExecuteRequest,
        idempotency_key: str = Header(alias="Idempotency-Key", min_length=1),
        run_id: str = Header(alias="X-AgentTrace-Run-ID", min_length=1),
        span_id: str = Header(alias="X-AgentTrace-Span-ID", min_length=1),
    ) -> OperationResult:
        # The trace headers are accepted only as correlation metadata. They never
        # select a callback, credentials, or browser behavior.
        del run_id, span_id
        case = validate_case(body.case)
        request_hash = canonical_request_hash(body)

        async def execute_once() -> OperationResult:
            app.state.operation_calls[idempotency_key] += 1
            browser = await manager.browser()
            result = await run_case(
                case,
                browser=browser,
                session_id=body.session_id,
                execution_id=idempotency_key,
            )
            screenshot_paths = [
                step.evidence.screenshot_path
                for step in result.steps
                if step.evidence.screenshot_path
            ]
            operation_error = None
            if result.status != "passed":
                operation_error = result.error or next(
                    (step.error for step in result.steps if step.error is not None),
                    StepError(
                        kind=SIGNAL_WORKER_ERROR,
                        message="execution failed without a structured error",
                    ),
                )
            return OperationResult(
                status="succeeded" if result.status == "passed" else "failed",
                output=result,
                artifacts=OperationArtifacts(screenshot_paths=screenshot_paths),
                error=operation_error,
            )

        return await operation_cache.execute(
            idempotency_key, request_hash, execute_once
        )

    return app


app = create_app()


__all__ = ["app", "create_app"]
