"""Stateless browser execution boundary used by the Go control plane."""

from __future__ import annotations

from typing import cast

from app.reporters import build_execution_report
from app.runners import RunnerExecutionError, RunnerInterventionError
from app.runners.playwright_runner import RunnerCancelledError, execute_case_with_playwright
from app.schemas.action_ir import case_dsl_profile
from app.schemas.dsl import DSLCase, GotoStep, validate_dsl_case
from app.schemas.executions import StepExecutionEvidence
from app.schemas.browser_executions import (
    BrowserExecutionRequest,
    BrowserExecutionResponse,
)
from app.services.failure_signals import build_failure_signal


def execute_browser_case(payload: BrowserExecutionRequest) -> BrowserExecutionResponse:
    normalized_case = validate_dsl_case(payload.dsl_case)
    dsl_profile = case_dsl_profile(normalized_case)
    effective_base_url = payload.base_url or getattr(normalized_case, "base_url", None)
    missing_base_url_error = _build_missing_base_url_error(
        normalized_case,
        effective_base_url,
    )
    input_values = _merged_input_values(normalized_case, payload.input_values)

    status = "failed"
    error_message = None
    step_results: list[StepExecutionEvidence] = []
    try:
        if missing_base_url_error is not None:
            step_results = [missing_base_url_error]
            error_message = missing_base_url_error.error_message
        else:
            step_results = execute_case_with_playwright(
                case=normalized_case,
                execution_id=payload.execution_id,
                base_url=effective_base_url,
                correction_store=None,
                input_values=input_values,
            )
            status = (
                "failed"
                if any(step.status == "failed" for step in step_results)
                else "passed"
            )
    except RunnerInterventionError as exc:
        status = "needs_intervention"
        error_message = str(exc)
        step_results = exc.step_results
    except RunnerCancelledError as exc:
        status = "cancelled"
        error_message = "Execution cancelled by control plane."
        step_results = exc.step_results
    except RunnerExecutionError as exc:
        status = "failed"
        error_message = str(exc)
        step_results = exc.step_results
    except Exception as exc:
        status = "failed"
        error_message = f"{type(exc).__name__}: {exc}"

    normalized_steps = _with_action_ir_metadata(
        [_with_artifact_url(step) for step in step_results],
        normalized_case,
        dsl_profile,
    )
    report = build_execution_report(
        status=status if status != "needs_intervention" else "failed",
        steps=normalized_steps,
        dsl_profile=dsl_profile,
    )
    failure_signal = None
    if status != "cancelled":
        signal = build_failure_signal(
            report,
            error_message,
            execution_id=payload.execution_id,
        )
        failure_signal = signal.model_dump(mode="json") if signal else None
    return BrowserExecutionResponse(
        status=status,
        error_message=error_message,
        report=report.model_dump(mode="json"),
        failure_signal=failure_signal,
    )


def _merged_input_values(case: object, input_values: dict[str, str]) -> dict[str, str]:
    merged: dict[str, str] = {}
    for contract in getattr(case, "input_contract", []):
        value = getattr(contract, "value", None)
        if value is not None:
            merged[getattr(contract, "context_key")] = value
    merged.update(input_values)
    return merged


def _with_action_ir_metadata(
    steps: list[StepExecutionEvidence],
    case: object,
    dsl_profile: str,
) -> list[StepExecutionEvidence]:
    case_steps = getattr(case, "steps", [])
    result: list[StepExecutionEvidence] = []
    for evidence in steps:
        declared_step = (
            case_steps[evidence.step_index]
            if evidence.step_index < len(case_steps)
            else None
        )
        updates = {
            "dsl_profile": dsl_profile,
            "intent": getattr(declared_step, "intent", None),
            "idempotency": getattr(declared_step, "idempotency", None),
            "declared_side_effect": getattr(declared_step, "side_effect", None),
        }
        result.append(evidence.model_copy(update=updates))
    return result


def _with_artifact_url(step: StepExecutionEvidence) -> StepExecutionEvidence:
    updates: dict[str, str] = {}
    if not step.screenshot_url and step.screenshot_path:
        normalized = step.screenshot_path.replace("\\", "/").lstrip("/")
        if normalized.startswith("artifacts/"):
            updates["screenshot_url"] = f"/{normalized}"
    if not step.dom_snapshot_url and step.dom_snapshot_path:
        normalized = step.dom_snapshot_path.replace("\\", "/").lstrip("/")
        if normalized.startswith("artifacts/"):
            updates["dom_snapshot_url"] = f"/{normalized}"
    return step.model_copy(update=updates) if updates else step


def _build_missing_base_url_error(
    case: DSLCase,
    base_url: str | None,
) -> StepExecutionEvidence | None:
    if base_url:
        return None
    for index, step in enumerate(case.steps):
        if step.action != "goto":
            continue
        goto_step = cast(GotoStep, step)
        if goto_step.value.startswith(("http://", "https://")):
            continue
        return StepExecutionEvidence(
            step_index=index,
            action="goto",
            value=goto_step.value,
            status="failed",
            duration_ms=0,
            error_message="Relative goto step requires case.base_url or execution request base_url.",
        )
    return None
