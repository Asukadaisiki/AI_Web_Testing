"""用真实浏览器验证 4 个页面对真实控制面是否可用（零 LLM 成本）。

前提：控制面跑在 127.0.0.1:8101（`LOOP_LLM_SCRIPT` 脚本模型），
`npm run build && npm run preview` 跑在 127.0.0.1:5174，夹具站点在 127.0.0.1:8123。

跑法：
    cd v2/worker && uv run python ../web/tests/verify_pages.py

它做三件事：
1. 走一遍页面 1（输入 → 规划 → 审批 → 执行）；
2. 打开页面 2/3/4，断言关键内容真的渲染出来了；
3. 全程收集 console 错误与 >=400 的响应，最后一起报错（页面"看着没事"但接口 404 是最常见的坑）。

任何一步失败都不会中断整个脚本：失败会连同**页面当时的文本**一起报出来，
否则只能看到一个 Playwright 超时，根本不知道页面上写了什么。
"""

from __future__ import annotations

import asyncio
import sys
from pathlib import Path
from typing import Any

WEB_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = WEB_ROOT.parent
SHOTS = REPO_ROOT / "data" / "ui-shots"

sys.path.insert(0, str(REPO_ROOT / "worker"))
from loop_worker.sessions import ensure_browsers_path  # noqa: E402

UI = "http://127.0.0.1:5174"
GOAL = "在商品列表页筛选 alpha，打开详情页并确认有 Add to cart 按钮"


async def find_run_with_candidates(page: Any) -> str | None:
    """找一个"有失败回灌候选"的历史 run，页面 4 才有东西可验。

    不能写死 run id：那是某个数据库里的偶然产物，换个 LOOP_DATA_DIR 就失效。
    """
    response = await page.request.get(f"{UI}/api/runs?limit=50")
    if not response.ok:
        return None
    runs = (await response.json()).get("runs") or []
    for run in runs:
        feedback = await page.request.get(f"{UI}/api/runs/{run['id']}/feedback")
        if not feedback.ok:
            continue
        if (await feedback.json()).get("candidates"):
            return str(run["id"])
    return None


class Failures:
    def __init__(self) -> None:
        self.console: list[str] = []
        self.http: list[str] = []
        self.checks: list[str] = []
        self.notes: list[str] = []

    def check(self, condition: bool, description: str) -> bool:
        self.checks.append(f"{'OK  ' if condition else 'FAIL'} {description}")
        return condition

    def note(self, title: str, body: str) -> None:
        trimmed = body.strip()
        if len(trimmed) > 1200:
            trimmed = trimmed[:1200] + " …[truncated]"
        self.notes.append(f"--- {title} ---\n{trimmed}")

    @property
    def failed(self) -> bool:
        return any(line.startswith("FAIL") for line in self.checks)

    def report(self) -> str:
        lines = ["", "=== checks ===", *self.checks]
        if self.console:
            lines += ["", "=== console errors ===", *self.console]
        if self.http:
            lines += ["", "=== http >= 400 ===", *self.http]
        for note in self.notes:
            lines += ["", note]
        return "\n".join(lines)


async def guard(failures: Failures, label: str, page: Any, coro: Any) -> bool:
    """跑一个断言块；失败时把页面文本留下来，绝不中断整个脚本。"""
    try:
        await coro
        return True
    except Exception as exc:  # noqa: BLE001 - 这里就是要吞掉并报告
        failures.check(False, f"{label}: {type(exc).__name__}: {str(exc).splitlines()[0]}")
        try:
            failures.note(f"{label} page text", await page.inner_text("body"))
        except Exception:
            pass
        return False


async def main() -> int:
    ensure_browsers_path()
    from playwright.async_api import async_playwright

    SHOTS.mkdir(parents=True, exist_ok=True)
    failures = Failures()

    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch(headless=True)
        context = await browser.new_context(viewport={"width": 1440, "height": 960})
        page = await context.new_page()

        page.on(
            "console",
            lambda message: failures.console.append(f"{message.type}: {message.text}")
            if message.type == "error"
            else None,
        )
        page.on(
            "response",
            lambda response: failures.http.append(f"{response.status} {response.url}")
            if response.status >= 400
            else None,
        )

        # ---- 页面 1：输入 / 会话 ----
        await page.goto(UI, wait_until="domcontentloaded")
        await guard(
            failures,
            "page1 renders the goal form",
            page,
            page.wait_for_selector("h2:has-text('输入目标')", timeout=15000),
        )
        await page.screenshot(path=str(SHOTS / "1-input.png"))

        await guard(
            failures,
            "page1 submits a goal and follows the new run",
            page,
            _submit(page),
        )
        await guard(
            failures,
            "page1 reaches awaiting_approval over SSE",
            page,
            page.wait_for_selector("h2:has-text('待审批')", timeout=90000),
        )
        if await guard(
            failures,
            "page1 timeline has events",
            page,
            page.wait_for_selector("ul.timeline li", timeout=15000),
        ):
            count = await page.locator("ul.timeline li").count()
            failures.check(count > 3, f"page1 timeline event count > 3 (got {count})")
        if await guard(
            failures,
            "page1 shows the planned case steps",
            page,
            page.wait_for_selector("table tbody tr", timeout=15000),
        ):
            rows = await page.locator("table tbody tr").count()
            failures.check(rows > 0, f"page1 case step rows > 0 (got {rows})")
        await page.screenshot(path=str(SHOTS / "1-session-awaiting-approval.png"), full_page=True)

        await page.click("button.primary:has-text('批准并执行')")
        # StatusBadge 显示的是后端原样状态值（契约取值），不是中文标签。
        # 必须限定在"run 状态"卡片里：左侧 run 列表也有 completed 徽标，
        # 直接匹配 span.badge 会命中别的历史 run，于是还没执行完就往下走了。
        await guard(
            failures,
            "page1 approves and the run completes",
            page,
            page.wait_for_selector(
                "section.card:has(h2:has-text('run 状态')) span.badge:has-text('completed')",
                timeout=120000,
            ),
        )
        await page.screenshot(path=str(SHOTS / "1-session-completed.png"), full_page=True)

        run_id = page.url.split("run=")[1].split("&")[0]
        failures.check(run_id.startswith("run_"), f"page1 url carries the run id ({run_id})")

        # ---- 页面 2：执行详情 ----
        await page.goto(f"{UI}/executions/{run_id}", wait_until="domcontentloaded")
        await guard(
            failures,
            "page2 renders the execution detail",
            page,
            page.wait_for_selector("h2:has-text('执行详情')", timeout=15000),
        )
        if await guard(
            failures,
            "page2 lists the executed steps",
            page,
            page.wait_for_selector("h2:has-text('步骤')", timeout=20000),
        ):
            body = await page.inner_text("body")
            failures.check("步骤（7）" in body, "page2 shows 7 executed steps")
            # 截图/证据默认折叠，必须先展开一步才会渲染出来。
            await page.click("table.table tbody tr:first-child button.link-button")
            await page.wait_for_selector("h4:has-text('截图')", timeout=10000)
            shot_links = await page.locator("a[href*='artifacts']").count()
            failures.check(shot_links > 0, f"page2 links step screenshots ({shot_links})")
            img = page.locator("img.shot").first
            failures.check(await img.count() > 0, "page2 renders the screenshot image")
        await page.screenshot(path=str(SHOTS / "2-execution.png"), full_page=True)

        # ---- 页面 3：报告 ----
        await page.goto(f"{UI}/runs/{run_id}/report", wait_until="domcontentloaded")
        await guard(
            failures,
            "page3 renders the report",
            page,
            page.wait_for_selector("h2:has-text('报告')", timeout=15000),
        )
        if await guard(
            failures,
            "page3 renders the failure-signal section",
            page,
            page.wait_for_selector("h2:has-text('失败信号')", timeout=15000),
        ):
            body = await page.inner_text("body")
            failures.check("无失败信号" in body, "page3 shows no failure signals for a passing run")
        await page.screenshot(path=str(SHOTS / "3-report.png"), full_page=True)

        # ---- 页面 4：错误注入 ----
        failed_run = await find_run_with_candidates(page)
        if not failures.check(
            failed_run is not None,
            "found a run with feedback candidates to verify page4",
        ):
            failures.note(
                "page4 skipped",
                "数据库里没有带回灌候选的 run。先造一次失败：规划出 case → 停掉站点 → 批准执行。",
            )
        else:
            await page.goto(f"{UI}/runs/{failed_run}/injection", wait_until="domcontentloaded")
            await guard(
                failures,
                "page4 renders the injection page",
                page,
                page.wait_for_selector("h2:has-text('错误注入')", timeout=15000),
            )
            if await guard(
                failures,
                "page4 lists the feedback candidates",
                page,
                page.wait_for_selector("h2:has-text('失败回灌候选')", timeout=15000),
            ):
                await page.wait_for_timeout(1500)
                body = await page.inner_text("body")
                failures.check(
                    "worker_error" in body,
                    "page4 shows the worker_error signal of the failed run",
                )
                failures.check(
                    "失败回灌候选（1）" in body, "page4 lists exactly one feedback candidate"
                )
            await page.screenshot(path=str(SHOTS / "4-injection.png"), full_page=True)

        await browser.close()

    print(failures.report())
    print(f"\nscreenshots -> {SHOTS}")
    if failures.console or failures.http:
        print("\nRESULT: FAILED (browser reported errors)")
        return 1
    if failures.failed:
        print("\nRESULT: FAILED (a check did not hold)")
        return 1
    print("\nRESULT: OK")
    return 0


async def _submit(page: Any) -> None:
    await page.fill("textarea.textarea", GOAL)
    await page.click("button.primary:has-text('开始')")
    await page.wait_for_url("**/?run=*", timeout=20000)


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
