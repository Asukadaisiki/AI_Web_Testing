"""条件评估（阶段感知）—— CONTRACT §2.2。

阶段语义：

- `pre`：只能是状态事实（`url_contains` / `text_visible` / `text_gone`），**动作前快照即刻评估**，不轮询；
- `post`：动作后**带超时轮询**评估；`url_changes` / `value_equals` 只能在 post。

`value_equals` 的语义是**本步 target 元素的 value 等于给定值**（不是"值变了"）。
"""

from __future__ import annotations

import asyncio
import time

from playwright.async_api import Locator, Page

from .contracts import (
    Condition,
    ConditionResult,
    Step,
    condition_phase_allowed,
    utc_now_iso,
)

#: 轮询间隔
POLL_INTERVAL_MS = 100

#: text_visible / text_gone 最多检查多少个候选元素
MAX_TEXT_MATCHES_TO_CHECK = 80


class ConditionPhaseError(Exception):
    """条件被放到了不允许的阶段（CONTRACT §2.2 的条件阶段表）。"""


async def _text_visible(page: Page, value: str) -> tuple[bool, str | None]:
    """页面上是否存在**可见的**、文本包含 value 的元素。"""
    if not value:
        return False, "condition value is empty; no element can be matched"
    try:
        locator = page.get_by_text(value, exact=False)
        count = await locator.count()
    except Exception as exc:  # 页面已销毁等
        return False, f"text lookup failed: {exc}"
    if count == 0:
        return False, f"no element contains text {value!r}"
    limit = min(count, MAX_TEXT_MATCHES_TO_CHECK)
    for i in range(limit):
        try:
            if await locator.nth(i).is_visible():
                return True, None
        except Exception:
            continue
    return False, f"{count} element(s) contain text {value!r} but none is visible"


async def evaluate_condition(
    page: Page,
    condition: Condition,
    *,
    phase: str,
    url_before: str | None = None,
    target_locator: Locator | None = None,
) -> ConditionResult:
    """评估一次（不做轮询），返回带 detail 的结果。"""
    if not condition_phase_allowed(condition.type, phase):
        raise ConditionPhaseError(
            f"condition type {condition.type!r} is not allowed in the {phase} phase"
        )

    satisfied = False
    detail: str | None = None

    if condition.type == "url_contains":
        current = page.url
        satisfied = condition.value in current
        detail = None if satisfied else f"current url {current!r} does not contain {condition.value!r}"
    elif condition.type == "text_visible":
        satisfied, detail = await _text_visible(page, condition.value)
    elif condition.type == "text_gone":
        visible, _ = await _text_visible(page, condition.value)
        satisfied = not visible
        detail = None if satisfied else f"text {condition.value!r} is still visible"
    elif condition.type == "url_changes":
        current = page.url
        satisfied = url_before is not None and current != url_before
        detail = None if satisfied else f"url did not change from {url_before!r} (still {current!r})"
    elif condition.type == "value_equals":
        if target_locator is None:
            satisfied = False
            detail = "condition value_equals requires a step target, but this step has none"
        else:
            try:
                actual = await target_locator.input_value(timeout=1000)
            except Exception as exc:
                satisfied = False
                detail = f"could not read target value: {exc}"
            else:
                satisfied = actual == condition.value
                detail = None if satisfied else f"target value is {actual!r}, expected {condition.value!r}"
    elif condition.type == "element_state":
        if target_locator is None:
            satisfied = False
            detail = "condition element_state requires a step target"
        else:
            try:
                state = condition.value.strip().lower()
                if state == "visible":
                    satisfied = await target_locator.is_visible()
                elif state in ("hidden", "not_visible"):
                    satisfied = not await target_locator.is_visible()
                elif state == "enabled":
                    satisfied = await target_locator.is_enabled()
                elif state == "disabled":
                    satisfied = not await target_locator.is_enabled()
                elif state == "checked":
                    satisfied = await target_locator.is_checked()
                elif state == "unchecked":
                    satisfied = not await target_locator.is_checked()
                else:
                    detail = "element_state must be visible, hidden, enabled, disabled, checked, or unchecked"
                if detail is None and not satisfied:
                    detail = f"target state is not {condition.value!r}"
            except Exception as exc:
                satisfied = False
                detail = f"could not read target state: {exc}"
    elif condition.type == "attribute_equals":
        if target_locator is None:
            satisfied = False
            detail = "condition attribute_equals requires a step target"
        else:
            if "=" not in condition.value:
                satisfied = False
                detail = "attribute_equals value must be attr=value"
            else:
                attr, expected = condition.value.split("=", 1)
                attr = attr.strip()
                expected = expected.strip()
                try:
                    actual = await target_locator.get_attribute(attr, timeout=1000)
                except Exception as exc:
                    satisfied = False
                    detail = f"could not read target attribute {attr!r}: {exc}"
                else:
                    satisfied = actual == expected
                    detail = None if satisfied else f"target attribute {attr!r} is {actual!r}, expected {expected!r}"
    elif condition.type == "count_equals":
        if target_locator is None:
            satisfied = False
            detail = "condition count_equals requires a step target"
        else:
            try:
                expected = int(condition.value)
            except ValueError:
                satisfied = False
                detail = "count_equals value must be an integer"
            else:
                try:
                    actual = await target_locator.count()
                except Exception as exc:
                    satisfied = False
                    detail = f"could not count target locator: {exc}"
                else:
                    satisfied = actual == expected
                    detail = None if satisfied else f"target count is {actual}, expected {expected}"
    else:  # pragma: no cover - pydantic 已限制取值
        raise ConditionPhaseError(f"unknown condition type: {condition.type!r}")

    return ConditionResult(
        phase="pre" if phase == "pre" else "post",
        type=condition.type,
        value=condition.value,
        satisfied=satisfied,
        detail=detail,
    )


async def wait_condition(
    page: Page,
    condition: Condition,
    *,
    phase: str,
    url_before: str | None = None,
    target_locator: Locator | None = None,
    timeout_ms: int | None = None,
) -> ConditionResult:
    """轮询评估直到满足或超时（post 阶段用）。"""
    deadline = time.monotonic() + (timeout_ms if timeout_ms is not None else condition.timeout_ms) / 1000.0
    result = await evaluate_condition(
        page, condition, phase=phase, url_before=url_before, target_locator=target_locator
    )
    while not result.satisfied and time.monotonic() < deadline:
        await asyncio.sleep(POLL_INTERVAL_MS / 1000.0)
        result = await evaluate_condition(
            page, condition, phase=phase, url_before=url_before, target_locator=target_locator
        )
    return result


async def evaluate_preconditions(page: Page, step: Step) -> list[ConditionResult]:
    """pre 阶段：动作前快照即刻评估，不轮询（CONTRACT §2.2）。"""
    results: list[ConditionResult] = []
    for condition in step.preconditions:
        results.append(await evaluate_condition(page, condition, phase="pre"))
    return results


async def evaluate_postconditions(
    page: Page,
    step: Step,
    *,
    url_before: str,
    target_locator: Locator | None = None,
) -> list[ConditionResult]:
    """post 阶段：动作后按各自 timeout_ms 轮询评估。"""
    results: list[ConditionResult] = []
    for condition in step.postconditions:
        results.append(
            await wait_condition(
                page,
                condition,
                phase="post",
                url_before=url_before,
                target_locator=target_locator,
            )
        )
    return results


def unmet_summary(results: list[ConditionResult]) -> str | None:
    """把未满足的条件压成一行人类可读文本（用于 StepResult.error）。"""
    unmet = [r for r in results if not r.satisfied]
    if not unmet:
        return None
    parts = [
        f"[{r.phase}] {r.type}={r.value!r}" + (f" ({r.detail})" if r.detail else "")
        for r in unmet
    ]
    return "; ".join(parts)


__all__ = [
    "POLL_INTERVAL_MS",
    "ConditionPhaseError",
    "evaluate_condition",
    "evaluate_postconditions",
    "evaluate_preconditions",
    "unmet_summary",
    "wait_condition",
]
