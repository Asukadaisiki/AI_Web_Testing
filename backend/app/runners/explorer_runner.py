"""Non-terminating Explorer runner: executes ALL steps and records every failure.

Unlike playwright_runner which stops at the first failure, the Explorer
continues through the entire DSL, recording failures with rich evidence.
After a step fails, it attempts page-state recovery via subsequent goto steps.
"""

from __future__ import annotations

import logging
from pathlib import Path
from threading import Event
from time import perf_counter
from typing import Generator

from app.locators import InterventionNeededError, LocatorResolutionError, resolve_with_fallback
from app.locators.corrections import CorrectionStore
from app.runners.playwright_runner import (
    ARTIFACTS_ROOT,
    RunnerCancelledError,
    RunnerExecutionError,
    _elapsed_ms,
    _resolve_url,
    _safe_dom_summary,
    _safe_page_title,
    _substitute_variables,
    _take_step_screenshot,
)
from app.schemas.dsl import DSLCase
from app.schemas.explorer_judge import ExplorerStepEvidence, ExplorationResult

logger = logging.getLogger(__name__)


class ExplorerStepEvent:
    """Yielded per step during Explorer execution."""

    __slots__ = ("type", "step_index", "action", "target", "value", "status", "duration_ms")

    def __init__(
        self,
        *,
        type: str,
        step_index: int,
        action: str,
        target: str | None = None,
        value: str | None = None,
        status: str | None = None,
        duration_ms: int | None = None,
    ) -> None:
        self.type = type
        self.step_index = step_index
        self.action = action
        self.target = target
        self.value = value
        self.status = status
        self.duration_ms = duration_ms


def run_explorer(
    *,
    case: DSLCase,
    execution_id: int,
    base_url: str | None,
    correction_store: CorrectionStore | None = None,
    input_values: dict[str, str] | None = None,
    cancel_event: Event | None = None,
) -> Generator[ExplorerStepEvent, None, ExplorationResult]:
    """Execute ALL DSL steps, recording every failure without stopping.

    After a failure, attempts recovery by executing subsequent goto steps.
    If recovery is not possible, marks remaining dependent steps as cascade_blocked.

    Yields ExplorerStepEvent per step. Returns ExplorationResult via StopIteration.value.
    """
    try:
        from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
        from playwright.sync_api import expect, sync_playwright
    except ImportError as exc:
        raise RunnerExecutionError(
            "Playwright dependency is not installed. Run `uv sync` in backend/ first."
        ) from exc

    artifact_dir = ARTIFACTS_ROOT / str(execution_id)
    artifact_dir.mkdir(parents=True, exist_ok=True)

    all_steps: list[ExplorerStepEvidence] = []
    failures: list[ExplorerStepEvidence] = []
    page_broken = False  # True after a non-goto failure breaks the page state

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = browser.new_page()

        try:
            for index, step in enumerate(case.steps):
                if cancel_event is not None and cancel_event.is_set():
                    raise RunnerCancelledError("Explorer cancelled by user.", step_results=[])

                yield ExplorerStepEvent(
                    type="step_start",
                    step_index=index,
                    action=step.action,
                    target=getattr(step, "target", None),
                    value=getattr(step, "value", None),
                )

                # If page state is broken and this step can't recover it, mark cascade_blocked
                if page_broken and step.action != "goto":
                    evidence = ExplorerStepEvidence(
                        step_index=index,
                        action=step.action,
                        target=getattr(step, "target", None),
                        value=getattr(step, "value", None),
                        status="cascade_blocked",
                        error_message="Previous step failure left page in inconsistent state; this step was skipped.",
                    )
                    all_steps.append(evidence)
                    yield ExplorerStepEvent(
                        type="step_complete",
                        step_index=index,
                        action=step.action,
                        status="cascade_blocked",
                    )
                    continue

                step_started_at = perf_counter()
                page_broken = False

                try:
                    _execute_step(
                        page, step, execution_id, base_url,
                        correction_store=correction_store,
                        input_values=input_values,
                    )

                    evidence = _collect_evidence(
                        page, index, step, "passed",
                        duration_ms=_elapsed_ms(step_started_at),
                        artifact_dir=artifact_dir,
                    )
                    all_steps.append(evidence)
                    yield ExplorerStepEvent(
                        type="step_complete",
                        step_index=index,
                        action=step.action,
                        status="passed",
                        duration_ms=evidence.duration_ms,
                    )

                except (InterventionNeededError, LocatorResolutionError,
                        PlaywrightTimeoutError, RunnerExecutionError, AssertionError) as exc:
                    error_msg = str(exc)
                    evidence = _collect_evidence(
                        page, index, step, "failed",
                        duration_ms=_elapsed_ms(step_started_at),
                        artifact_dir=artifact_dir,
                        error_message=error_msg,
                    )
                    all_steps.append(evidence)
                    failures.append(evidence)
                    page_broken = True

                    yield ExplorerStepEvent(
                        type="step_complete",
                        step_index=index,
                        action=step.action,
                        status="failed",
                        duration_ms=evidence.duration_ms,
                    )
                    logger.info("Explorer: step %d failed (%s): %s", index, step.action, error_msg)
        finally:
            browser.close()

    passed = sum(1 for s in all_steps if s.status == "passed")
    failed = sum(1 for s in all_steps if s.status == "failed")
    cascade = sum(1 for s in all_steps if s.status == "cascade_blocked")
    return ExplorationResult(
        steps=all_steps,
        failure_records=failures,
        total_steps=len(all_steps),
        passed_steps=passed,
        failed_steps=failed,
        cascade_blocked_steps=cascade,
    )


def _execute_step(page, step, execution_id: int, base_url: str | None, *,
                   correction_store: CorrectionStore | None = None,
                   input_values: dict[str, str] | None = None) -> None:
    """Execute a single DSL step. Raises on failure."""
    from playwright.sync_api import expect

    if step.action == "goto":
        page.goto(_resolve_url(step.value, base_url), wait_until="domcontentloaded")
    elif step.action == "click":
        resolved = resolve_with_fallback(
            page, step.target,
            target_strategy=step.target_strategy,
            correction_store=correction_store,
            execution_id=execution_id,
            require_visible=True, require_enabled=True,
        )
        if resolved.click_coordinates is not None:
            page.mouse.click(*resolved.click_coordinates)
        else:
            resolved.locator.click()
    elif step.action == "input":
        resolved = resolve_with_fallback(
            page, step.target,
            target_strategy=step.target_strategy,
            correction_store=correction_store,
            execution_id=execution_id,
            prefer_input=True, require_visible=True, require_enabled=True,
        )
        input_value = _substitute_variables(step.value, input_values)
        if resolved.click_coordinates is not None:
            page.mouse.click(*resolved.click_coordinates)
            page.keyboard.type(input_value)
        else:
            resolved.locator.fill(input_value)
    elif step.action == "wait_for":
        resolved = resolve_with_fallback(
            page, step.target,
            target_strategy=step.target_strategy,
            correction_store=correction_store,
            execution_id=execution_id,
            require_visible=False,
        )
        resolved.locator.wait_for(state="visible", timeout=step.timeout_ms)
    elif step.action == "assert_text":
        resolved = resolve_with_fallback(
            page, step.target,
            target_strategy=step.target_strategy,
            correction_store=correction_store,
            execution_id=execution_id,
            require_visible=False,
        )
        expect(resolved.locator).to_contain_text(_substitute_variables(step.value, input_values))
    elif step.action == "assert_url_contains":
        if _substitute_variables(step.value, input_values) not in page.url:
            raise RunnerExecutionError(
                f"URL assertion failed, expected fragment: {_substitute_variables(step.value, input_values)}"
            )
    else:
        raise RunnerExecutionError(f"Unsupported action: {step.action}")


def _collect_evidence(
    page,
    step_index: int,
    step,
    status: str,
    *,
    duration_ms: int,
    artifact_dir: Path,
    error_message: str | None = None,
) -> ExplorerStepEvidence:
    """Collect step evidence for Explorer output."""
    console_errors: list[str] = []
    network_errors: list[str] = []
    dom_summary = None
    screenshot_path = None
    url = None
    page_title = None

    try:
        url = page.url or None
    except Exception:
        pass
    try:
        page_title = _safe_page_title(page)
    except Exception:
        pass
    try:
        dom_raw = _safe_dom_summary(page)
        if dom_raw:
            dom_summary = dom_raw.model_dump()
    except Exception:
        pass
    try:
        screenshot_path = _take_step_screenshot(page, artifact_dir, step_index)
    except Exception:
        pass

    return ExplorerStepEvidence(
        step_index=step_index,
        action=step.action,
        target=getattr(step, "target", None),
        value=getattr(step, "value", None),
        status=status,
        duration_ms=duration_ms,
        error_message=error_message,
        screenshot_path=screenshot_path,
        dom_summary=dom_summary,
        console_errors=console_errors,
        network_errors=network_errors,
        url=url,
        page_title=page_title,
    )
