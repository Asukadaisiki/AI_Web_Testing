"""Playwright-backed execution runner for stored DSL cases."""

from __future__ import annotations

from pathlib import Path
from urllib.parse import urljoin

from app.locators import LocatorResolutionError, resolve_semantic_locator
from app.schemas.dsl import DSLCase
from app.schemas.executions import StepExecutionEvidence


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
        try:
            for index, step in enumerate(case.steps):
                try:
                    resolved_by = None
                    if step.action == "goto":
                        page.goto(_resolve_url(step.value, base_url), wait_until="domcontentloaded")
                    elif step.action == "click":
                        resolved = resolve_semantic_locator(page, step.target)
                        resolved_by = resolved.strategy
                        resolved.locator.click()
                    elif step.action == "input":
                        resolved = resolve_semantic_locator(page, step.target, prefer_input=True)
                        resolved_by = resolved.strategy
                        resolved.locator.fill(step.value)
                    elif step.action == "wait_for":
                        resolved = resolve_semantic_locator(page, step.target)
                        resolved_by = resolved.strategy
                        resolved.locator.wait_for(state="visible", timeout=step.timeout_ms)
                    elif step.action == "assert_text":
                        resolved = resolve_semantic_locator(page, step.target)
                        resolved_by = resolved.strategy
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
                            resolved_by=resolved_by,
                            url=page.url or None,
                            screenshot_path=_take_step_screenshot(page, artifact_dir, index),
                        )
                    )
                except (LocatorResolutionError, PlaywrightTimeoutError, RunnerExecutionError, AssertionError) as exc:
                    step_results.append(
                        StepExecutionEvidence(
                            step_index=index,
                            action=step.action,
                            target=getattr(step, "target", None),
                            value=getattr(step, "value", None),
                            status="failed",
                            url=page.url or None,
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
        raise RunnerExecutionError("Relative goto step requires base_url or EXECUTION_BASE_URL.")
    return urljoin(base_url.rstrip("/") + "/", value.lstrip("/"))


def _take_step_screenshot(page, artifact_dir: Path, step_index: int) -> str | None:
    screenshot_path = artifact_dir / f"step-{step_index + 1:02d}.png"
    try:
        page.screenshot(path=str(screenshot_path), full_page=True)
    except Exception:
        return None
    return str(screenshot_path.relative_to(Path(__file__).resolve().parents[2]))
