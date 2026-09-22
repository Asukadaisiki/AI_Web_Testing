"""动作实现：goto / click / input（CONTRACT §2.1 的 5 个 action 里真正操作页面的 3 个）。

`assert_text` / `assert_url` 不改动页面 —— 它们由 postconditions（`text_visible` / `url_contains`）
承担断言，runner 对这两个 action 不做页面操作。
"""

from __future__ import annotations

from playwright.async_api import Locator, Page
from playwright.async_api import Error as PlaywrightError
from playwright.async_api import TimeoutError as PlaywrightTimeoutError

from .contracts import (
    SIGNAL_STEP_TIMEOUT,
    SIGNAL_TARGET_NOT_FOUND,
    SIGNAL_WORKER_ERROR,
    LocatorSpec,
)
from .locators import LocatorResolutionError, to_playwright_locator

#: 动作后等待页面稳定的上限（load / networkidle）
STABLE_LOAD_TIMEOUT_MS = 3000
STABLE_NETWORKIDLE_TIMEOUT_MS = 2000
#: 稳定后额外让出的一小段时间，等同步 JS 的 DOM 变更落地
SETTLE_MS = 50


class ActionFailure(Exception):
    """动作失败，`kind` 是 CONTRACT §4.1 的失败信号 kind。"""

    def __init__(self, kind: str, detail: str) -> None:
        super().__init__(detail)
        self.kind = kind
        self.detail = detail

    @property
    def error(self) -> str:
        return f"{self.kind}: {self.detail}"


def target_not_found(detail: str) -> ActionFailure:
    return ActionFailure(SIGNAL_TARGET_NOT_FOUND, detail)


def step_timeout(detail: str) -> ActionFailure:
    return ActionFailure(SIGNAL_STEP_TIMEOUT, detail)


def worker_error(detail: str) -> ActionFailure:
    """硬失败（不是超时）：例如导航被拒、页面崩了。

    这类失败必须报 `worker_error` 而不是 `step_timeout`，否则报告会说"步骤超时"，
    而真相是"目标站点根本连不上"——回灌候选也会因此给出错误的建议。
    """
    return ActionFailure(SIGNAL_WORKER_ERROR, detail)


async def wait_for_stable(page: Page) -> None:
    """动作后等页面稳定：load → networkidle → 一小段让出时间。全部容错。"""
    try:
        await page.wait_for_load_state("load", timeout=STABLE_LOAD_TIMEOUT_MS)
    except Exception:
        pass
    try:
        await page.wait_for_load_state("networkidle", timeout=STABLE_NETWORKIDLE_TIMEOUT_MS)
    except Exception:
        pass
    try:
        await page.wait_for_timeout(SETTLE_MS)
    except Exception:
        pass


async def goto(page: Page, url: str, timeout_ms: int) -> None:
    """导航到绝对 URL。"""
    try:
        await page.goto(url, wait_until="domcontentloaded", timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"goto {url!r} timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"goto {url!r} failed: {exc}") from exc
    await wait_for_stable(page)


async def resolve_target(page: Page, spec: LocatorSpec, timeout_ms: int) -> Locator:
    """解析并验证本步目标：必须**唯一命中且可见**。

    执行期只使用 case 里记录的那一条定位器，不再重新推导（CONTRACT §3.1）。
    """
    try:
        locator = to_playwright_locator(page, spec)
    except LocatorResolutionError as exc:
        raise target_not_found(str(exc)) from exc

    try:
        await locator.wait_for(state="visible", timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        count = await _safe_count(locator)
        raise target_not_found(
            f"{spec.describe()} matched {count} element(s) within {timeout_ms}ms"
        ) from exc
    except PlaywrightError as exc:
        # strict mode violation 等
        raise target_not_found(f"{spec.describe()} could not be resolved: {exc}") from exc

    count = await _safe_count(locator)
    if count != 1:
        raise target_not_found(f"{spec.describe()} matched {count} element(s), expected exactly 1")
    return locator


async def click_target(page: Page, locator: Locator, timeout_ms: int) -> None:
    try:
        await locator.click(timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"click timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"click failed: {exc}") from exc
    await wait_for_stable(page)


async def fill_target(page: Page, locator: Locator, value: str, timeout_ms: int) -> None:
    """`input` 动作：用 fill 语义写入（先清空再输入），因此 value 可以为空串。"""
    try:
        await locator.fill(value, timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"input timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"input failed: {exc}") from exc
    await wait_for_stable(page)


async def _safe_count(locator: Locator) -> int:
    try:
        return await locator.count()
    except Exception:
        return 0


__all__ = [
    "ActionFailure",
    "click_target",
    "fill_target",
    "goto",
    "resolve_target",
    "step_timeout",
    "target_not_found",
    "wait_for_stable",
    "worker_error",
]
