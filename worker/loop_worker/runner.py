"""case 执行循环（CONTRACT §4）。

- 在**全新的** BrowserContext 里跑完整个 case，不依赖任何已有会话；
- 首步必须是 goto：case 非法由 `contracts.validate_case` 拒绝，本模块把校验失败翻译成
  `status="error"` + `error.kind="case_invalid"`（HTTP 层会先校验并返回 400）；
- pre 阶段在动作前快照评估，post 阶段在动作后带超时轮询；
- 每步都产出证据：截图 + console + network + url_before/url_after。

失败即止：某个步骤失败后不再执行后续步骤（后续步骤依赖的状态已不成立），结果里只包含
已经执行过的步骤。
"""

from __future__ import annotations

import time
from typing import Any

from playwright.async_api import Browser

from . import actions
from .actions import ActionFailure
from .conditions import evaluate_postconditions, evaluate_preconditions, unmet_summary
from .contracts import (
    ACTION_CLICK,
    ACTION_GOTO,
    ACTION_INPUT,
    DEFAULT_STEP_TIMEOUT_MS,
    SIGNAL_CASE_INVALID,
    SIGNAL_CONDITION_UNMET,
    SIGNAL_WORKER_ERROR,
    Case,
    CaseInvalid,
    ConditionResult,
    Evidence,
    ExecutionResult,
    Step,
    StepError,
    StepResult,
    utc_now_iso,
    validate_case,
)
from .evidence import EvidenceCollector
from .observer import new_id


async def run_case(
    case: dict[str, Any] | Case,
    *,
    browser: Browser,
    session_id: str,
    execution_id: str | None = None,
) -> ExecutionResult:
    """执行整个 case，返回 ExecutionResult（永不抛业务异常）。

    `session_id` 是领域会话：每步截图落进 `<产物根>/<session_id>/`（CONTRACT §9.2）。
    """
    if not session_id:
        raise ValueError("session_id is required to run a case")
    execution_id = execution_id or new_id("exec")
    started_at = utc_now_iso()

    try:
        validated = validate_case(case) if isinstance(case, dict) else case
    except CaseInvalid as exc:
        return ExecutionResult(
            execution_id=execution_id,
            status="error",
            started_at=started_at,
            finished_at=utc_now_iso(),
            final_url="",
            steps=[],
            error=StepError(kind=SIGNAL_CASE_INVALID, message=f"{exc.code}: {exc.detail}"),
        )

    context = await browser.new_context(viewport={"width": 1280, "height": 800})
    page = await context.new_page()
    # 兜底超时：不让任何漏传 timeout 的 Playwright 调用吃默认的 30s
    page.set_default_timeout(DEFAULT_STEP_TIMEOUT_MS)
    page.set_default_navigation_timeout(DEFAULT_STEP_TIMEOUT_MS)
    collector = EvidenceCollector(page)
    step_results: list[StepResult] = []
    status = "passed"
    error: StepError | None = None
    final_url = page.url

    try:
        for step in validated.steps:
            result = await _run_step(page, collector, step, execution_id, session_id)
            step_results.append(result)
            final_url = result.url_after
            if result.status == "failed":
                status = "failed"
                break
    except Exception as exc:  # 执行器自身故障
        status = "error"
        error = StepError(
            kind=SIGNAL_WORKER_ERROR, message=f"{type(exc).__name__}: {exc}"
        )
        try:
            final_url = page.url
        except Exception:
            pass
    finally:
        try:
            await context.close()
        except Exception:
            pass

    return ExecutionResult(
        execution_id=execution_id,
        status=status,
        started_at=started_at,
        finished_at=utc_now_iso(),
        final_url=final_url,
        steps=step_results,
        # 顶层 error 只在 status="error"（执行器故障）时非空：失败步骤的原因在
        # steps[].error 里，避免 Go 侧把条件未满足误判成 worker_error。
        error=error,
    )


async def _run_step(
    page,  # noqa: ANN001 - Playwright Page
    collector: EvidenceCollector,
    step: Step,
    execution_id: str,
    session_id: str,
) -> StepResult:
    url_before = page.url
    started_at = utc_now_iso()
    clock = time.perf_counter()
    conditions: list[ConditionResult] = []
    status = "passed"
    error: StepError | None = None
    target_locator = None

    preconditions = await evaluate_preconditions(page, step)
    conditions.extend(preconditions)
    summary = unmet_summary(preconditions)
    if summary is not None:
        status = "failed"
        error = StepError(kind=SIGNAL_CONDITION_UNMET, message=f"precondition unmet: {summary}")

    if status == "passed":
        try:
            if step.action == ACTION_GOTO:
                await actions.goto(page, step.value or "", step.timeout_ms)
            elif step.action in (ACTION_CLICK, ACTION_INPUT):
                if step.target is None or step.target.locator is None:
                    raise ActionFailure(
                        SIGNAL_WORKER_ERROR, f"{step.action} step has no resolvable target"
                    )
                target_locator = await actions.resolve_target(
                    page, step.target.locator, step.timeout_ms
                )
                if step.action == ACTION_CLICK:
                    await actions.click_target(page, target_locator, step.timeout_ms)
                else:
                    await actions.fill_target(
                        page, target_locator, step.value or "", step.timeout_ms
                    )
            # assert_text / assert_url 不改动页面：断言由 postconditions 承担
        except ActionFailure as exc:
            status = "failed"
            error = StepError(kind=exc.kind, message=exc.detail)
        except Exception as exc:
            status = "failed"
            error = StepError(
                kind=SIGNAL_WORKER_ERROR, message=f"{type(exc).__name__}: {exc}"
            )

    if status == "passed":
        postconditions = await evaluate_postconditions(
            page, step, url_before=url_before, target_locator=target_locator
        )
        conditions.extend(postconditions)
        summary = unmet_summary(postconditions)
        if summary is not None:
            status = "failed"
            error = StepError(
                kind=SIGNAL_CONDITION_UNMET, message=f"postcondition unmet: {summary}"
            )

    screenshot_path = await collector.screenshot(
        session_id, f"{execution_id}_{step.index}.png"
    )
    snapshot = collector.snapshot()
    duration_ms = int((time.perf_counter() - clock) * 1000)

    return StepResult(
        index=step.index,
        action=step.action,
        status=status,
        started_at=started_at,
        duration_ms=duration_ms,
        url_before=url_before,
        url_after=page.url,
        conditions=conditions,
        evidence=Evidence(
            screenshot_path=screenshot_path,
            console=snapshot.console,
            network=snapshot.network,
        ),
        error=error,
    )


__all__ = ["run_case"]
