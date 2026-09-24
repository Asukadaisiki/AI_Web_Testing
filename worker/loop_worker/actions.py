"""动作实现：goto / click / input（CONTRACT §2.1 的 5 个 action 里真正操作页面的 3 个）。

`assert_text` / `assert_url` 不改动页面 —— 它们由 postconditions（`text_visible` / `url_contains`）
承担断言，runner 对这两个 action 不做页面操作。
"""

from __future__ import annotations

from playwright.async_api import Locator, Page
from playwright.async_api import Error as PlaywrightError
from playwright.async_api import TimeoutError as PlaywrightTimeoutError

from .blockers import NON_BYPASSABLE, attempt_recovery, blocker_for_hit, signal_for_blocker, target_hit_test
from .contracts import (
    Blocker,
    HitTest,
    RecoveryAttempt,
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

    def __init__(
        self,
        kind: str,
        detail: str,
        *,
        blocker: Blocker | None = None,
        hit_test: HitTest | None = None,
        recovery: list[RecoveryAttempt] | None = None,
    ) -> None:
        super().__init__(detail)
        self.kind = kind
        self.detail = detail
        self.blocker = blocker
        self.hit_test = hit_test
        self.recovery = recovery or []

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
    """导航到绝对 URL。

    等待条件是 `commit`，**不是 `domcontentloaded`**。

    实测（automationexercise.com，2026-09-23）：这个站点的页面在 HTTPS 下引用了
    `http://fonts.googleapis.com/...`，被浏览器按 Mixed Content 拦掉；挂起的外部资源
    把 DOMContentLoaded 拖到 20s 之后才触发。结果是"页面其实早就好了、URL 也对，
    但 goto 报 step_timeout"——干跑偶然在 20s 内过、真实执行 20.8s 就红，
    同一份 case 一会儿过一会儿不过。

    正确做法：`commit` 只等导航被提交（快且可靠），页面就绪交给**后置条件轮询**决定——
    那本来就是 v2 的就绪判据（每个条件有自己的 timeout_ms，独立于步超时）。
    步超时应当约束"动作"，不该被目标站点的外部资源绑架。

    这不掩盖真实故障：连不上仍然是 `worker_error`，内容迟迟不出现仍然是
    后置条件 `condition_unmet`——都是诚实的失败。
    """
    try:
        await page.goto(url, wait_until="commit", timeout=timeout_ms)
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


async def ensure_actionable(
    page: Page, locator: Locator, timeout_ms: int
) -> list[RecoveryAttempt]:
    """Check that a grounded target is reachable; safely recover once if blocked."""
    try:
        hit = await target_hit_test(page, locator)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"target hit-test timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"target hit-test failed: {exc}") from exc
    if not hit.covered:
        return []

    blocker = await blocker_for_hit(page, hit)
    if blocker.kind in NON_BYPASSABLE:
        raise ActionFailure(
            signal_for_blocker(blocker.kind),
            f"{blocker.kind} blocks target: {blocker.reason}",
            blocker=blocker,
            hit_test=hit,
        )

    attempt = await attempt_recovery(page, blocker, timeout_ms)
    recovery = [attempt]
    if not attempt.succeeded:
        raise ActionFailure(
            signal_for_blocker(blocker.kind),
            f"{blocker.kind} blocks target and recovery failed: {attempt.reason}",
            blocker=blocker,
            hit_test=hit,
            recovery=recovery,
        )
    await wait_for_stable(page)
    try:
        retry_hit = await target_hit_test(page, locator)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"target hit-test timed out after recovery after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"target hit-test failed after recovery: {exc}") from exc
    attempt.retried_original_action = True
    if retry_hit.covered:
        retry_blocker = await blocker_for_hit(page, retry_hit)
        raise ActionFailure(
            signal_for_blocker(retry_blocker.kind),
            f"{retry_blocker.kind} still blocks target after recovery: {retry_blocker.reason}",
            blocker=retry_blocker,
            hit_test=retry_hit,
            recovery=recovery,
        )
    return recovery


async def click_target(page: Page, locator: Locator, timeout_ms: int) -> list[RecoveryAttempt]:
    recovery = await ensure_actionable(page, locator, timeout_ms)
    try:
        await locator.click(timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"click timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"click failed: {exc}") from exc
    await wait_for_stable(page)
    return recovery


async def fill_target(
    page: Page, locator: Locator, value: str, timeout_ms: int, submit: bool = False
) -> None:
    """`input` 动作：用 fill 语义写入（先清空再输入），因此 value 可以为空串。

    `submit=True` 时填完按回车。真实站点上的搜索提交控件常常是纯图标按钮——
    可访问名与文本都只有一个不可见的私有区码位，模型无法用任何 hint 指到它，
    回车不需要指到任何控件，是表单的原生提交方式（CONTRACT §2.1）。

    **边界（实测）**：这只对"表单能被回车提交"的站点有效。若站点的提交控件是
    `type="button"` + JS（例如 automationexercise.com 的 `#submit_search`），
    回车与 `form.requestSubmit()` 都不会提交，唯一出路是能指到那个按钮本身——
    那是别名匹配面的活，不在本函数职责内。
    """
    try:
        await locator.fill(value, timeout=timeout_ms)
        if submit:
            await locator.press("Enter", timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"input timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"input failed: {exc}") from exc
    await wait_for_stable(page)


async def select_target(page: Page, locator: Locator, value: str, timeout_ms: int) -> list[RecoveryAttempt]:
    recovery = await ensure_actionable(page, locator, timeout_ms)
    try:
        await locator.select_option(label=value, timeout=timeout_ms)
    except PlaywrightError:
        try:
            await locator.select_option(value=value, timeout=timeout_ms)
        except PlaywrightTimeoutError as exc:
            raise step_timeout(f"select timed out after {timeout_ms}ms") from exc
        except PlaywrightError as exc:
            raise worker_error(f"select failed: {exc}") from exc
    await wait_for_stable(page)
    return recovery


async def check_target(page: Page, locator: Locator, timeout_ms: int) -> list[RecoveryAttempt]:
    recovery = await ensure_actionable(page, locator, timeout_ms)
    try:
        await locator.check(timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"check timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"check failed: {exc}") from exc
    await wait_for_stable(page)
    return recovery


async def uncheck_target(page: Page, locator: Locator, timeout_ms: int) -> list[RecoveryAttempt]:
    recovery = await ensure_actionable(page, locator, timeout_ms)
    try:
        await locator.uncheck(timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"uncheck timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"uncheck failed: {exc}") from exc
    await wait_for_stable(page)
    return recovery


async def scroll_into_view_target(page: Page, locator: Locator, timeout_ms: int) -> None:
    try:
        await locator.scroll_into_view_if_needed(timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"scroll_into_view timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"scroll_into_view failed: {exc}") from exc
    await wait_for_stable(page)


async def hover_target(page: Page, locator: Locator, timeout_ms: int) -> list[RecoveryAttempt]:
    recovery = await ensure_actionable(page, locator, timeout_ms)
    try:
        await locator.hover(timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"hover timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"hover failed: {exc}") from exc
    await wait_for_stable(page)
    return recovery


async def dismiss_dialog(page: Page, locator: Locator, timeout_ms: int) -> None:
    try:
        close_button = locator.get_by_role("button", name="Close")
        if await close_button.count() != 1:
            close_button = locator.locator(
                'button[aria-label*="Close" i], button[title*="Close" i], '
                'button:has-text("Close"), button:has-text("×"), button:has-text("x")'
            ).first
        await close_button.click(timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"dismiss_dialog timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"dismiss_dialog failed: {exc}") from exc
    await wait_for_stable(page)


async def upload_file_target(
    page: Page, locator: Locator, file_path: str, timeout_ms: int
) -> list[RecoveryAttempt]:
    recovery = await ensure_actionable(page, locator, timeout_ms)
    try:
        await locator.set_input_files(file_path, timeout=timeout_ms)
    except PlaywrightTimeoutError as exc:
        raise step_timeout(f"upload_file timed out after {timeout_ms}ms") from exc
    except PlaywrightError as exc:
        raise worker_error(f"upload_file failed: {exc}") from exc
    await wait_for_stable(page)
    return recovery


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
    "check_target",
    "dismiss_dialog",
    "ensure_actionable",
    "hover_target",
    "resolve_target",
    "scroll_into_view_target",
    "select_target",
    "step_timeout",
    "target_not_found",
    "uncheck_target",
    "upload_file_target",
    "wait_for_stable",
    "worker_error",
]
