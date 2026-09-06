"""Deterministic failure classification shared by execution, reporting, and Planning."""

from __future__ import annotations

import hashlib
import re
from dataclasses import dataclass

from app.schemas.executions import (
    AgentEventReference,
    ExecutionReport,
    FailureCategory,
    FailureSignal,
    FailureSourceReference,
    FailureStage,
    NetworkEvent,
    StepExecutionEvidence,
)

FAILURE_SIGNAL_SCHEMA_VERSION = "failure.signal.v2"


@dataclass(frozen=True)
class _Classification:
    category: FailureCategory
    stage: FailureStage
    code: str
    retryable: bool
    json_pointer: str


def categorize_failure(
    *,
    action: str | None = None,
    error_message: str | None = None,
    step: StepExecutionEvidence | None = None,
) -> FailureCategory:
    message = (error_message or "").casefold()
    if "relative goto step requires" in message or "case.base_url" in message:
        return "configuration"
    if step is not None:
        if step.locator_trace and step.locator_trace.failure_reason:
            return "locator"
        if step.action.startswith("assert_"):
            return "assertion"
        if step.action == "goto":
            return "navigation"
        if any(
            event.failure_text or (event.status is not None and event.status >= 400)
            for event in step.network_events
        ):
            return "network"
        action = step.action
    if any(token in message for token in ("locator", "not found", "no element", "selector")):
        return "locator"
    if any(token in message for token in ("assertion", "expect", "mismatch")):
        return "assertion"
    if action == "goto" or any(token in message for token in ("navigation", "page.goto")):
        return "navigation"
    if any(token in message for token in ("network", "connection", "econnrefused", "http ")):
        return "network"
    return "runner"


def build_failure_signal(
    report: ExecutionReport | None,
    error_message: str | None,
    *,
    execution_id: int | None = None,
    agent_event_reference: AgentEventReference | None = None,
) -> FailureSignal | None:
    failed_step = _first_failed_step(report)
    if failed_step is None and not error_message:
        return None

    raw_error = failed_step.error_message if failed_step and failed_step.error_message else error_message
    classification = _classify_failure(
        failed_step,
        raw_error,
    )
    category = classification.category
    title = _normalize_error_message(raw_error) or (
        f"{category}:{failed_step.action}" if failed_step else category
    )
    action = failed_step.action if failed_step else None
    fingerprint_source = "|".join([category, action or "unknown", title.casefold()])
    source_reference = None
    if execution_id is not None:
        source_reference = FailureSourceReference(
            type="execution_report" if failed_step else "execution_error",
            execution_id=execution_id,
            step_index=failed_step.step_index if failed_step else None,
            json_pointer=classification.json_pointer,
        )
    side_effect_committed = _side_effect_committed(failed_step)
    return FailureSignal(
        schema_version=FAILURE_SIGNAL_SCHEMA_VERSION if source_reference else None,
        category=category,
        fingerprint=hashlib.sha1(fingerprint_source.encode("utf-8")).hexdigest()[:16],
        title=title,
        stage=classification.stage if source_reference else None,
        code=classification.code if source_reference else None,
        retryable=(
            classification.retryable and side_effect_committed is False
            if source_reference
            else None
        ),
        side_effect_committed=side_effect_committed,
        source_reference=source_reference,
        agent_event_reference=agent_event_reference,
        step_index=failed_step.step_index if failed_step else None,
        action=action,
        target=failed_step.target if failed_step else None,
        error_message=raw_error,
        locator_failure_reason=(
            failed_step.locator_trace.failure_reason
            if failed_step and failed_step.locator_trace
            else None
        ),
        screenshot_url=failed_step.screenshot_url if failed_step else None,
    )


def _classify_failure(
    step: StepExecutionEvidence | None,
    error_message: str | None,
) -> _Classification:
    if step is not None:
        for result_position, result in enumerate(step.condition_results):
            if result.status == "passed":
                continue
            category: FailureCategory = "assertion"
            retryable = False
            code = f"condition.{result.phase}.{result.type}.{result.status}"
            if result.type in {"url_contains", "url_changes"}:
                category = "navigation"
                retryable = True
            elif result.type == "network_request":
                category = "network"
                network = _first_failed_network_event(step.network_events)
                code, retryable = _network_code(network)
            return _Classification(
                category=category,
                stage=result.phase,
                code=code,
                retryable=retryable,
                json_pointer=f"/steps/{step.step_index}/condition_results/{result_position}",
            )

        outcome = step.action_outcome
        if outcome.status != "succeeded":
            if step.locator_trace and step.locator_trace.failure_reason:
                return _Classification(
                    category="locator",
                    stage="locator",
                    code="locator.no_match",
                    retryable=True,
                    json_pointer=f"/steps/{step.step_index}/action_outcome",
                )
            network = _first_failed_network_event(step.network_events)
            if network is not None:
                code, retryable = _network_code(network)
                return _Classification(
                    category="network",
                    stage="network",
                    code=code,
                    retryable=retryable,
                    json_pointer=f"/steps/{step.step_index}/action_outcome",
                )
            if step.action == "goto":
                return _Classification(
                    category="navigation",
                    stage="action",
                    code=f"action.{outcome.status}",
                    retryable=True,
                    json_pointer=f"/steps/{step.step_index}/action_outcome",
                )
            if step.action.startswith("assert_"):
                return _Classification(
                    category="assertion",
                    stage="action",
                    code=f"action.{outcome.status}",
                    retryable=False,
                    json_pointer=f"/steps/{step.step_index}/action_outcome",
                )

        if step.locator_trace and step.locator_trace.failure_reason:
            return _Classification(
                category="locator",
                stage="locator",
                code="locator.no_match",
                retryable=True,
                json_pointer=f"/steps/{step.step_index}/locator_trace/failure_reason",
            )
        network_position, network = _first_failed_network_event_with_position(step.network_events)
        if network is not None:
            code, retryable = _network_code(network)
            return _Classification(
                category="network",
                stage="network",
                code=code,
                retryable=retryable,
                json_pointer=f"/steps/{step.step_index}/network_events/{network_position}",
            )

    exception = _exception_classification(step, error_message)
    if exception is not None:
        return exception

    category = categorize_failure(
        action=step.action if step else None,
        error_message=error_message,
        step=step,
    )
    stage: FailureStage = "configuration" if category == "configuration" else "runner"
    if step is not None:
        if category == "locator":
            stage = "locator"
        elif category == "navigation":
            stage = "action"
        elif category == "network":
            stage = "network"
        elif category == "assertion":
            stage = "action"
    return _Classification(
        category=category,
        stage=stage,
        code=f"fallback.{category}",
        retryable=category in {"locator", "navigation", "network"},
        json_pointer=(
            f"/steps/{step.step_index}/error_message" if step else "/error_message"
        ),
    )


def _exception_classification(
    step: StepExecutionEvidence | None,
    error_message: str | None,
) -> _Classification | None:
    match = re.match(r"\s*([A-Za-z_][\w.]*(?:Error|Exception))\s*:", error_message or "")
    if match is None:
        return None
    exception_type = match.group(1).rsplit(".", 1)[-1]
    normalized = exception_type.casefold()
    category: FailureCategory = "runner"
    stage: FailureStage = "action" if step else "runner"
    retryable = False
    if "assert" in normalized:
        category = "assertion"
    elif "navigation" in normalized:
        category = "navigation"
        retryable = True
    elif "connection" in normalized or "network" in normalized:
        category = "network"
        stage = "network"
        retryable = True
    elif "timeout" in normalized:
        retryable = True
    return _Classification(
        category=category,
        stage=stage,
        code=f"exception.{_snake_case(exception_type)}",
        retryable=retryable,
        json_pointer=(
            f"/steps/{step.step_index}/error_message" if step else "/error_message"
        ),
    )


def _first_failed_network_event(events: list[NetworkEvent]) -> NetworkEvent | None:
    return _first_failed_network_event_with_position(events)[1]


def _first_failed_network_event_with_position(
    events: list[NetworkEvent],
) -> tuple[int, NetworkEvent | None]:
    for index, event in enumerate(events):
        if event.failure_text or (event.status is not None and event.status >= 400):
            return index, event
    return -1, None


def _network_code(event: NetworkEvent | None) -> tuple[str, bool]:
    if event is None:
        return "condition.network_request.unsatisfied", True
    if event.failure_text:
        return "network.request_failed", True
    if event.status is not None and 400 <= event.status < 500:
        return "network.http_4xx", False
    if event.status is not None and event.status >= 500:
        return "network.http_5xx", True
    return "network.failure", True


def _side_effect_committed(step: StepExecutionEvidence | None) -> bool | None:
    if step is None:
        return None
    state = step.action_outcome.side_effect_state
    if state == "committed":
        return True
    if state in {"not_committed", "not_applicable"}:
        return False
    return None


def _snake_case(value: str) -> str:
    return re.sub(r"(?<!^)(?=[A-Z])", "_", value).casefold()


def _first_failed_step(report: ExecutionReport | None) -> StepExecutionEvidence | None:
    if report is None:
        return None
    return next((step for step in report.steps if step.status == "failed"), None)


def _normalize_error_message(error_message: str | None) -> str | None:
    if not error_message:
        return None
    normalized = re.sub(r"\s+", " ", error_message).strip()
    return normalized[:1000] or None
