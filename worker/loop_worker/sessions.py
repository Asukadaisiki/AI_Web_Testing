"""内存会话管理（HTTP `/sessions*` 的后端）。

- 一个进程共享一个 chromium 浏览器实例（懒启动）；
- 每个会话一个独立的 BrowserContext + Page，起始是空页面（about:blank）；
- 会话里的 console / network 由 `EvidenceCollector` 挂在 page 上采集；
- `/navigate` 与 `/act` 都返回**同一条观测路径**产出的 Observation。
"""

from __future__ import annotations

import asyncio
import os
from pathlib import Path

from playwright.async_api import Browser, BrowserContext, Page
from playwright.async_api import Error as PlaywrightError
from playwright.async_api import async_playwright

from .actions import (
    check_target,
    click_target,
    dismiss_dialog,
    fill_target,
    goto,
    hover_target,
    resolve_target,
    scroll_into_view_target,
    select_target,
    uncheck_target,
    upload_file_target,
)
from .contracts import (
    DEFAULT_STEP_TIMEOUT_MS,
    SIGNAL_CONDITION_UNMET,
    ActRequest,
    ActResponse,
    Observation,
    StepError,
)
from .conditions import evaluate_postcondition_list, unmet_summary
from .evidence import EvidenceCollector, capture_screenshot
from .observer import new_id, observe_page

#: 本机 Playwright 浏览器的默认位置；只在环境变量未设置时兜底（见 NOTES）
DEFAULT_BROWSERS_PATH = r"D:\PlaywrightBrowsers"


class SessionNotFound(Exception):
    """会话不存在（HTTP 404 session_not_found）。"""

    def __init__(self, session_id: str) -> None:
        super().__init__(f"session {session_id!r} does not exist")
        self.session_id = session_id


def ensure_browsers_path() -> None:
    """保证 PLAYWRIGHT_BROWSERS_PATH 有值：环境变量优先，否则用本机默认目录。"""
    if os.environ.get("PLAYWRIGHT_BROWSERS_PATH"):
        return
    if Path(DEFAULT_BROWSERS_PATH).is_dir():
        os.environ["PLAYWRIGHT_BROWSERS_PATH"] = DEFAULT_BROWSERS_PATH


class WorkerSession:
    """一个浏览器会话 = 一个 BrowserContext + 一个 Page + 一个证据采集器。

    `browser_session_id` 是这个临时句柄自己的 id（`bsess_...`）；
    `session_id` 是它服务的**领域会话**（`sess_...`），决定证据落哪个目录（CONTRACT §9）。
    """

    def __init__(
        self,
        browser_session_id: str,
        session_id: str,
        context: BrowserContext,
        page: Page,
    ) -> None:
        self.browser_session_id = browser_session_id
        self.session_id = session_id
        self.context = context
        self.page = page
        self.evidence = EvidenceCollector(page)
        self.closed = False

    async def close(self) -> None:
        if self.closed:
            return
        self.closed = True
        try:
            await self.context.close()
        except Exception:
            pass


class SessionManager:
    """进程内会话表。所有会话共用一个懒启动的 chromium。"""

    def __init__(self) -> None:
        self._playwright = None
        self._browser: Browser | None = None
        self._sessions: dict[str, WorkerSession] = {}
        self._lock = asyncio.Lock()

    # ---- 浏览器生命周期 ----

    async def browser(self) -> Browser:
        """取共享浏览器；首次调用时启动 Playwright 与 chromium。"""
        if self._browser is not None and self._browser.is_connected():
            return self._browser
        async with self._lock:
            if self._browser is not None and self._browser.is_connected():
                return self._browser
            ensure_browsers_path()
            if self._playwright is None:
                self._playwright = await async_playwright().start()
            try:
                self._browser = await self._playwright.chromium.launch(headless=True)
            except PlaywrightError as exc:
                raise RuntimeError(f"could not launch chromium: {exc}") from exc
            return self._browser

    async def shutdown(self) -> None:
        for session in list(self._sessions.values()):
            await session.close()
        self._sessions.clear()
        if self._browser is not None:
            try:
                await self._browser.close()
            except Exception:
                pass
            self._browser = None
        if self._playwright is not None:
            try:
                await self._playwright.stop()
            except Exception:
                pass
            self._playwright = None

    # ---- 会话表 ----

    async def create(self, session_id: str) -> WorkerSession:
        """开一个浏览器会话，并把它绑到领域会话上（决定证据落哪个目录）。"""
        if not session_id:
            raise ValueError("session_id is required to open a browser session")
        browser = await self.browser()
        context = await browser.new_context(viewport={"width": 1280, "height": 800})
        page = await context.new_page()
        # 兜底超时：不让任何漏传 timeout 的 Playwright 调用吃默认的 30s
        page.set_default_timeout(DEFAULT_STEP_TIMEOUT_MS)
        page.set_default_navigation_timeout(DEFAULT_STEP_TIMEOUT_MS)
        session = WorkerSession(new_id("bsess"), session_id, context, page)
        self._sessions[session.browser_session_id] = session
        return session

    def get(self, session_id: str) -> WorkerSession:
        session = self._sessions.get(session_id)
        if session is None or session.closed:
            raise SessionNotFound(session_id)
        return session

    async def close(self, session_id: str) -> bool:
        session = self.get(session_id)
        await session.close()
        self._sessions.pop(session_id, None)
        return True

    # ---- 会话上的动作 ----

    async def navigate(self, session_id: str, url: str) -> Observation:
        session = self.get(session_id)
        await goto(session.page, url, DEFAULT_STEP_TIMEOUT_MS)
        return await self.observe(session)

    async def act(self, session_id: str, request: ActRequest) -> ActResponse:
        session = self.get(session_id)
        page = session.page
        url_before = page.url
        locator = await resolve_target(page, request.locator, DEFAULT_STEP_TIMEOUT_MS)
        if request.action == "click":
            await click_target(page, locator, DEFAULT_STEP_TIMEOUT_MS)
        elif request.action == "input":
            await fill_target(
                page, locator, request.value or "", DEFAULT_STEP_TIMEOUT_MS, request.submit
            )
        elif request.action == "select":
            await select_target(page, locator, request.value or "", DEFAULT_STEP_TIMEOUT_MS)
        elif request.action == "check":
            await check_target(page, locator, DEFAULT_STEP_TIMEOUT_MS)
        elif request.action == "uncheck":
            await uncheck_target(page, locator, DEFAULT_STEP_TIMEOUT_MS)
        elif request.action == "scroll_into_view":
            await scroll_into_view_target(page, locator, DEFAULT_STEP_TIMEOUT_MS)
        elif request.action == "hover":
            await hover_target(page, locator, DEFAULT_STEP_TIMEOUT_MS)
        elif request.action == "dismiss_dialog":
            await dismiss_dialog(page, locator, DEFAULT_STEP_TIMEOUT_MS)
        elif request.action == "upload_file":
            await upload_file_target(page, locator, request.value or "", DEFAULT_STEP_TIMEOUT_MS)
        elif request.action in ("assert_element", "assert_attribute", "assert_count"):
            # Target assertions only resolve their locator; their postconditions do the evaluation.
            pass
        conditions = await evaluate_postcondition_list(
            page,
            request.postconditions,
            url_before=url_before,
            target_locator=locator,
        )
        summary = unmet_summary(conditions)
        return ActResponse(
            status="failed" if summary is not None else "passed",
            observation=await self.observe(session),
            conditions=conditions,
            error=(
                StepError(
                    kind=SIGNAL_CONDITION_UNMET,
                    message=f"postcondition unmet: {summary}",
                )
                if summary is not None
                else None
            ),
        )

    async def observe(self, session: WorkerSession) -> Observation:
        """会话观测：截图 → 采集 elements（定位器就地验证）。"""
        observation_id = new_id("obs")
        screenshot_path = await capture_screenshot(
            session.page, session.session_id, f"{observation_id}.png"
        )
        return await observe_page(
            session.page,
            screenshot_path=screenshot_path,
            observation_id=observation_id,
            browser_session_id=session.browser_session_id,
        )


__all__ = [
    "DEFAULT_BROWSERS_PATH",
    "SessionManager",
    "SessionNotFound",
    "WorkerSession",
    "ensure_browsers_path",
]
