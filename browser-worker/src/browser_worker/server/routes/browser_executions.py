"""Internal stateless browser execution route."""

from __future__ import annotations

from fastapi import APIRouter, HTTPException

from browser_worker.capabilities.browser_execution import execute_browser_case
from browser_worker.contracts.browser_executions import (
    BrowserExecutionRequest,
    BrowserExecutionResponse,
)

router = APIRouter(prefix="/internal/browser-executions", tags=["internal-browser"])


@router.post("", response_model=BrowserExecutionResponse)
def invoke_browser_execution(
    payload: BrowserExecutionRequest,
) -> BrowserExecutionResponse:
    try:
        return execute_browser_case(payload)
    except ValueError as exc:
        raise HTTPException(status_code=422, detail=str(exc)) from exc
