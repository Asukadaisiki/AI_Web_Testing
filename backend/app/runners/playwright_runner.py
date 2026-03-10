"""Playwright-backed execution runner for stored DSL cases."""

from __future__ import annotations

from pathlib import Path
from time import perf_counter
from urllib.parse import urljoin

from app.locators import LocatorResolutionError, resolve_semantic_locator
from app.schemas.dsl import DSLCase
from app.schemas.executions import (
    ConsoleEvent,
    DOMSummary,
    NetworkEvent,
    StepExecutionEvidence,
    ViewportSnapshot,
)


ARTIFACTS_ROOT = Path(__file__).resolve().parents[2] / "artifacts" / "executions"


class RunnerExecutionError(RuntimeError):
    """Raised when a case cannot be executed successfully."""

    def __init__(
        self,
        message: str,
        *,
        step_results: list[StepExecutionEvidence] | None = None,
    ) -> None:
        super().__init__(message)
        self.step_results = step_results or []


def execute_case_with_playwright(
    *,
    case: DSLCase,
    execution_id: int,
    base_url: str | None,
) -> list[StepExecutionEvidence]:
    try:
        from playwright.sync_api import TimeoutError as PlaywrightTimeoutError
        from playwright.sync_api import expect, sync_playwright
    except ImportError as exc:
        raise RunnerExecutionError(
            "Playwright dependency is not installed. Run `uv sync` in backend/ first."
        ) from exc

    artifact_dir = ARTIFACTS_ROOT / str(execution_id)
    artifact_dir.mkdir(parents=True, exist_ok=True)

    step_results: list[StepExecutionEvidence] = []
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        page = browser.new_page()
        console_buffer: list[ConsoleEvent] = []
        network_buffer: list[NetworkEvent] = []
        page.on("console", lambda message: _capture_console_event(message, console_buffer))
        page.on("requestfailed", lambda request: _capture_request_failed(request, network_buffer))
        page.on("response", lambda response: _capture_error_response(response, network_buffer))

        try:
            for index, step in enumerate(case.steps):
                step_started_at = perf_counter()
                console_index = len(console_buffer)
                network_index = len(network_buffer)
                resolved = None
                locator_trace = None

                try:
                    resolved_by = None
                    if step.action == "goto":
                        page.goto(_resolve_url(step.value, base_url), wait_until="domcontentloaded")
                    elif step.action == "click":
                        resolved = resolve_semantic_locator(
                            page,
                            step.target,
                            require_visible=True,
                            require_enabled=True,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        resolved.locator.click()
                    elif step.action == "input":
                        resolved = resolve_semantic_locator(
                            page,
                            step.target,
                            prefer_input=True,
                            require_visible=True,
                            require_enabled=True,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        resolved.locator.fill(step.value)
                    elif step.action == "wait_for":
                        resolved = resolve_semantic_locator(
                            page,
                            step.target,
                            require_visible=False,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        resolved.locator.wait_for(state="visible", timeout=step.timeout_ms)
                    elif step.action == "assert_text":
                        resolved = resolve_semantic_locator(
                            page,
                            step.target,
                            require_visible=False,
                        )
                        resolved_by = resolved.strategy
                        locator_trace = resolved.trace
                        expect(resolved.locator).to_contain_text(step.value)
                    elif step.action == "assert_url_contains":
                        if step.value not in page.url:
                            raise RunnerExecutionError(
                                f"URL assertion failed, expected fragment: {step.value}"
                            )
                    else:
                        raise RunnerExecutionError(f"Unsupported action: {step.action}")

                    step_results.append(
                        StepExecutionEvidence(
                            step_index=index,
                            action=step.action,
                            target=getattr(step, "target", None),
                            value=getattr(step, "value", None),
                            status="passed",
                            duration_ms=_elapsed_ms(step_started_at),
                            resolved_by=resolved_by,
                            locator_trace=locator_trace,
                            url=page.url or None,
                            page_title=_safe_page_title(page),
                            viewport=_safe_viewport(page),
                            dom_summary=_safe_dom_summary(page),
                            console_events=console_buffer[console_index:],
                            network_events=network_buffer[network_index:],
                            screenshot_path=_take_step_screenshot(page, artifact_dir, index),
                        )
                    )
                except (LocatorResolutionError, PlaywrightTimeoutError, RunnerExecutionError, AssertionError) as exc:
                    if isinstance(exc, LocatorResolutionError):
                        locator_trace = exc.trace
                    elif resolved is not None:
                        locator_trace = resolved.trace

                    step_results.append(
                        StepExecutionEvidence(
                            step_index=index,
                            action=step.action,
                            target=getattr(step, "target", None),
                            value=getattr(step, "value", None),
                            status="failed",
                            duration_ms=_elapsed_ms(step_started_at),
                            resolved_by=resolved.strategy if resolved is not None else None,
                            locator_trace=locator_trace,
                            url=page.url or None,
                            page_title=_safe_page_title(page),
                            viewport=_safe_viewport(page),
                            dom_summary=_safe_dom_summary(page),
                            console_events=console_buffer[console_index:],
                            network_events=network_buffer[network_index:],
                            screenshot_path=_take_step_screenshot(page, artifact_dir, index),
                            error_message=str(exc),
                        )
                    )
                    raise RunnerExecutionError(str(exc), step_results=step_results) from exc
        finally:
            browser.close()

    return step_results


def _resolve_url(value: str, base_url: str | None) -> str:
    if value.startswith(("http://", "https://")):
        return value
    if not base_url:
        raise RunnerExecutionError("Relative goto step requires case.base_url or execution request base_url.")
    return urljoin(base_url.rstrip("/") + "/", value.lstrip("/"))


def _take_step_screenshot(page, artifact_dir: Path, step_index: int) -> str | None:
    screenshot_path = artifact_dir / f"step-{step_index + 1:02d}.png"
    try:
        page.screenshot(path=str(screenshot_path), full_page=True)
    except Exception:
        return None
    return str(screenshot_path.relative_to(Path(__file__).resolve().parents[2]))


def _elapsed_ms(started_at: float) -> int:
    return max(0, int((perf_counter() - started_at) * 1000))


def _safe_page_title(page) -> str | None:
    try:
        title = page.title()
    except Exception:
        return None
    return title or None


def _safe_viewport(page) -> ViewportSnapshot | None:
    try:
        viewport = page.viewport_size
    except Exception:
        return None
    if not viewport:
        return None
    return ViewportSnapshot(width=viewport.get("width", 0), height=viewport.get("height", 0))


def _safe_dom_summary(page) -> DOMSummary | None:
    try:
        payload = page.evaluate(
            """
            () => {
              const text = (document.body?.innerText || "").replace(/\\s+/g, " ").trim().slice(0, 240);
              return {
                text_preview: text || null,
                button_count: document.querySelectorAll("button").length,
                input_count: document.querySelectorAll("input, textarea, select").length,
                link_count: document.querySelectorAll("a").length,
              };
            }
            """
        )
    except Exception:
        return None
    return DOMSummary.model_validate(payload)


def _capture_console_event(message, console_buffer: list[ConsoleEvent]) -> None:
    level = message.type
    if level not in {"error", "warning"}:
        return
    location = message.location or {}
    console_buffer.append(
        ConsoleEvent(
            level=level,
            text=message.text,
            source_url=location.get("url"),
            line_number=location.get("lineNumber"),
        )
    )


def _capture_request_failed(request, network_buffer: list[NetworkEvent]) -> None:
    failure = request.failure or {}
    network_buffer.append(
        NetworkEvent(
            url=request.url,
            method=request.method,
            resource_type=request.resource_type,
            failure_text=failure.get("errorText"),
        )
    )


def _capture_error_response(response, network_buffer: list[NetworkEvent]) -> None:
    if response.status < 400:
        return
    request = response.request
    network_buffer.append(
        NetworkEvent(
            url=response.url,
            method=request.method,
            status=response.status,
            resource_type=request.resource_type,
        )
    )
