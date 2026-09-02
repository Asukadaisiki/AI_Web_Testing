"""Deterministic failure classification shared by execution, reporting, and Planning."""

from __future__ import annotations

import hashlib
import re

from app.schemas.executions import (
    ExecutionReport,
    FailureCategory,
    FailureSignal,
    StepExecutionEvidence,
)


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
) -> FailureSignal | None:
    failed_step = _first_failed_step(report)
    if failed_step is None and not error_message:
        return None

    raw_error = failed_step.error_message if failed_step and failed_step.error_message else error_message
    category = categorize_failure(
        action=failed_step.action if failed_step else None,
        error_message=raw_error,
        step=failed_step,
    )
    title = _normalize_error_message(raw_error) or (
        f"{category}:{failed_step.action}" if failed_step else category
    )
    action = failed_step.action if failed_step else None
    fingerprint_source = "|".join([category, action or "unknown", title.casefold()])
    return FailureSignal(
        category=category,
        fingerprint=hashlib.sha1(fingerprint_source.encode("utf-8")).hexdigest()[:16],
        title=title,
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


def _first_failed_step(report: ExecutionReport | None) -> StepExecutionEvidence | None:
    if report is None:
        return None
    return next((step for step in report.steps if step.status == "failed"), None)


def _normalize_error_message(error_message: str | None) -> str | None:
    if not error_message:
        return None
    normalized = re.sub(r"\s+", " ", error_message).strip()
    return normalized[:1000] or None
