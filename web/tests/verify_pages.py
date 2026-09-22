"""用真实浏览器验证 4 个页面对真实控制面是否可用（零 LLM 成本）。

前提：控制面跑在 127.0.0.1:8101（`LOOP_LLM_SCRIPT` 脚本模型），
`npm run build && npm run preview` 跑在 127.0.0.1:5174，夹具站点在 127.0.0.1:8123。

跑法（推荐从仓库根跑，配置由 test.config.json 提供）：
    python run_tests.py --verify
或直接：
    cd worker && uv run python ../web/tests/verify_pages.py

UI 地址、目标文本、截图目录都读 `test.config.json` 的 `verify` 段，
不再写死在这里（否则改端口就要改两处）。

它做四件事：
1. 走一遍页面 1（输入 → 规划 → 审批 → 执行）；
2. 打开页面 2/3，断言关键内容真的渲染出来了；
3. 没有现成回灌候选时，**现场造一次失败**（同会话开一轮 → 停掉自己拉起的夹具站点 →
   批准执行 → worker_error 信号），然后验证页面 4 与回灌闭环（确认候选 → 新轮次必须
   落在同一个会话，同一条候选不能确认第二次）；
4. 全程收集 console 错误与 >=400 的响应，最后一起报错（页面“看着没事”但接口 404 是最常见的坑）。

任何一步失败都不会中断整个脚本：失败会连同**页面当时的文本**一起报出来，
否则只能看到一个 Playwright 超时，根本不知道页面上写了什么。
"""

from __future__ import annotations

import asyncio
import http.client
import json
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

WEB_ROOT = Path(__file__).resolve().parents[1]
REPO_ROOT = WEB_ROOT.parent


def _load_verify_config() -> dict:
    """读 test.config.json 的 verify 段；读不到就退回默认值（脚本仍可单独跑）。"""
    path = REPO_ROOT / "test.config.json"
    if not path.is_file():
        return {}
    try:
        with path.open(encoding="utf-8") as handle:
            return json.load(handle).get("verify") or {}
    except (OSError, ValueError):
        return {}


VERIFY_CONFIG = _load_verify_config()

SHOTS = REPO_ROOT / str(VERIFY_CONFIG.get("shots", "data/ui-shots"))

sys.path.insert(0, str(REPO_ROOT / "worker"))
from loop_worker.sessions import ensure_browsers_path  # noqa: E402

UI = str(VERIFY_CONFIG.get("ui", "http://127.0.0.1:5174"))
GOAL = str(
    VERIFY_CONFIG.get(
        "goal", "在商品列表页筛选 alpha，打开详情页并确认有 Add to cart 按钮"
    )
)

# 夹具站点：脚本自己拉起（仅当端口上没有现成站点时），造失败时停掉、最后拉回来。
FIXTURE_PORT = int(VERIFY_CONFIG.get("fixture_port", 8123))
FIXTURE_URL = f"http://127.0.0.1:{FIXTURE_PORT}"
FIXTURE_SITE_DIR = REPO_ROOT / "worker" / "fixtures" / "site"


class OwnedFixtureSite:
    """脚本自持的夹具站点进程；8123 已有站点时保持 `owned=False` 不去动它。"""

    def __init__(self) -> None:
        self.process: subprocess.Popen[bytes] | None = None

    @property
    def owned(self) -> bool:
        return self.process is not None

    def start(self) -> None:
        self.process = subprocess.Popen(
            [
                sys.executable,
                "-m",
                "http.server",
                str(FIXTURE_PORT),
                "--directory",
                str(FIXTURE_SITE_DIR),
            ],
            cwd=str(REPO_ROOT / "worker"),
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

    def stop(self) -> None:
        if self.process is None:
            return
        self.process.terminate()
        try:
            self.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.process.kill()
        self.process = None


async def wait_fixture(page: Any, want_up: bool, timeout_s: float = 15) -> bool:
    """等夹具站点起来/停下；超时返回当前实际状态。"""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        try:
            response = await page.request.get(f"{FIXTURE_URL}/", timeout=1500)
            up = response.ok
        except Exception:  # noqa: BLE001 - 连不上就是 down
            up = False
        if up == want_up:
            return True
        await page.wait_for_timeout(300)
    return False


async def wait_run_status(page: Any, run_id: str, wanted: set[str], timeout_s: float = 60) -> str | None:
    """轮询 run 状态直到进入 wanted 之一；超时返回 None。"""
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        response = await page.request.get(f"{UI}/api/runs/{run_id}")
        if response.ok:
            status = str((await response.json()).get("status") or "")
            if status in wanted:
                return status
        await page.wait_for_timeout(500)
    return None


async def manufacture_failed_run(
    page: Any, failures: "Failures", fixture: OwnedFixtureSite, session_id: str, parent_run_id: str
) -> bool:
    """现场造一次执行失败：同会话开一轮 → 停站点 → 批准 → 等 worker_error 信号。

    成功后数据库里就有带回灌候选的 run，页面 4 与回灌闭环验证才有东西可验。
    只在夹具站点由本脚本拉起时才停（外部进程不能随便杀）；结束前把站点拉回来。
    """
    if not fixture.owned:
        failures.note(
            "manufacture skipped",
            "夹具站点不是本脚本拉起的，无法安全停掉。"
            "请空出 127.0.0.1:8123 让脚本自持站点，或手工造一次失败。",
        )
        return False
    created = await page.request.post(
        f"{UI}/api/runs",
        data={"session_id": session_id, "input": GOAL, "parent_run_id": parent_run_id},
    )
    if not failures.check(created.ok, f"manufacture: create run ({created.status})"):
        return False
    run_id = str((await created.json()).get("run_id") or "")
    if not failures.check(run_id.startswith("run_"), f"manufacture: got run id ({run_id})"):
        return False
    status = await wait_run_status(page, run_id, {"awaiting_approval", "failed"})
    if not failures.check(
        status == "awaiting_approval", f"manufacture: planning reached {status}"
    ):
        return False

    fixture.stop()
    try:
        if not await wait_fixture(page, want_up=False):
            failures.check(False, "manufacture: fixture site did not stop")
            return False
        await page.request.post(f"{UI}/api/runs/{run_id}/approve", data={})
        status = await wait_run_status(page, run_id, {"completed", "failed"})
        if not failures.check(status is not None, f"manufacture: run settled (status={status})"):
            return False
        report = await page.request.get(f"{UI}/api/runs/{run_id}/report")
        signals = (await report.json()).get("signals") or [] if report.ok else []
        failures.check(
            any(signal.get("kind") == "worker_error" for signal in signals),
            "manufacture: worker_error signal produced",
        )
    finally:
        fixture.start()
        await wait_fixture(page, want_up=True)
    return True


async def find_run_with_candidates(page: Any) -> dict[str, Any] | None:
    """找一个"有未用过回灌候选"的历史 run，页面 4 与回灌闭环验证才有东西可用。

    不能写死 run id：那是某个数据库里的偶然产物，换个 LOOP_DATA_DIR 就失效。
    返回 run id、会话 id 与第一条 pending 候选；没有就返回 None。
    """
    response = await page.request.get(f"{UI}/api/runs?limit=50")
    if not response.ok:
        return None
    runs = (await response.json()).get("runs") or []
    for run in runs:
        feedback = await page.request.get(f"{UI}/api/runs/{run['id']}/feedback")
        if not feedback.ok:
            continue
        pending = [
            item
            for item in (await feedback.json()).get("candidates") or []
            if item.get("status") == "pending"
        ]
        if pending:
            return {
                "id": str(run["id"]),
                "session_id": str(run.get("session_id") or ""),
                "candidate_id": pending[0]["id"],
                "proposed_input": str(pending[0].get("proposed_input") or ""),
            }
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
    fixture = OwnedFixtureSite()

    # 夹具站点：8123 上没有现成站点时自己拉起（造失败时需要能停掉它）。
    def fixture_is_up_sync() -> bool:
        try:
            conn = http.client.HTTPConnection("127.0.0.1", FIXTURE_PORT, timeout=1)
            conn.request("GET", "/")
            conn.getresponse().read()
            conn.close()
            return True
        except OSError:
            return False

    if not fixture_is_up_sync():
        fixture.start()
        for _ in range(50):
            if fixture_is_up_sync():
                break
            time.sleep(0.2)

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

        # 侧边列的是会话，不是 run（CONTRACT §9）。
        failures.check(
            await page.locator("aside.sidebar .run-list-head:has-text('会话列表')").count() > 0,
            "the sidebar lists sessions, not runs",
        )

        await guard(
            failures,
            "page1 submits a goal and follows the new run",
            page,
            _submit(page),
        )
        await guard(
            failures,
            "page1 renders the session card",
            page,
            page.wait_for_selector("h2:has-text('会话')", timeout=15000),
        )
        if await guard(
            failures,
            "page1 lists the rounds of the session",
            page,
            page.wait_for_selector("h2:has-text('轮次') + table.table tbody tr", timeout=15000),
        ):
            rounds = await page.locator("h2:has-text('轮次') + table.table tbody tr").count()
            failures.check(rounds >= 1, f"page1 shows the session rounds (got {rounds})")
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
                "section.card:has(h2:has-text('当前轮次')) span.badge:has-text('completed')",
                timeout=120000,
            ),
        )
        await page.screenshot(path=str(SHOTS / "1-session-completed.png"), full_page=True)

        # URL 形如 /?session=sess_x&run=run_y：两个 id 都要在，且必须指向同一个会话。
        query = dict(
            part.split("=", 1) for part in page.url.split("?", 1)[1].split("&") if "=" in part
        )
        run_id = query.get("run", "")
        session_id = query.get("session", "")
        failures.check(run_id.startswith("run_"), f"page1 url carries the run id ({run_id})")
        failures.check(
            session_id.startswith("sess_"), f"page1 url carries the session id ({session_id})"
        )
        detail = await page.request.get(f"{UI}/api/sessions/{session_id}")
        if failures.check(detail.ok, f"the session in the url exists ({detail.status})"):
            session_body = await detail.json()
            failures.check(
                any(item["id"] == run_id for item in session_body.get("runs") or []),
                "the run in the url belongs to the session in the url",
            )
            failures.check(
                int(session_body.get("run_count") or 0) >= 1,
                f"the session reports its rounds (got {session_body.get('run_count')})",
            )

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

        # ---- 页面 4：错误注入（没有现成候选时，现场造一次失败）----
        failed_run = await find_run_with_candidates(page)
        if failed_run is None:
            failures.check(
                await manufacture_failed_run(page, failures, fixture, session_id, run_id),
                "manufactured a failed run for page4",
            )
            failed_run = await find_run_with_candidates(page)
        if not failures.check(
            failed_run is not None,
            "found a run with feedback candidates to verify page4",
        ):
            failures.note(
                "page4 skipped",
                "数据库里没有带回灌候选的 run，且现场制造失败未成功。"
                "确认夹具站点可由本脚本管理（127.0.0.1:8123 空闲或可复用）。",
            )
        else:
            assert failed_run is not None  # 给类型检查器；上一行已保证非 None
            await page.goto(f"{UI}/runs/{failed_run['id']}/injection", wait_until="domcontentloaded")
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
            await page.screenshot(path=str(SHOTS / "4-injection.png"), full_page=True)

            # ---- 回灌闭环：确认候选 → 新一轮必须落在同一个会话里（CONTRACT §9.1）----
            confirmed = await page.request.post(
                f"{UI}/api/runs/{failed_run['id']}/feedback/confirm",
                data={
                    "candidate_id": failed_run["candidate_id"],
                    "input": failed_run["proposed_input"],
                },
            )
            if failures.check(confirmed.ok, f"feedback confirm accepted ({confirmed.status})"):
                created = await confirmed.json()
                new_run_id = str(created.get("run_id") or "")
                failures.check(
                    new_run_id.startswith("run_"), f"confirm returns the new run id ({new_run_id})"
                )
                if failed_run["session_id"]:
                    failures.check(
                        created.get("session_id") == failed_run["session_id"],
                        "the feedback run stays in the same session",
                    )
                new_response = await page.request.get(f"{UI}/api/runs/{new_run_id}")
                if failures.check(new_response.ok, f"the feedback run exists ({new_response.status})"):
                    new_body = await new_response.json()
                    failures.check(
                        new_body.get("parent_run_id") == failed_run["id"],
                        "the feedback run points back at the failed run",
                    )
                # 同一条候选不能确认第二次（一条候选只能开一轮）。
                replay = await page.request.post(
                    f"{UI}/api/runs/{failed_run['id']}/feedback/confirm",
                    data={
                        "candidate_id": failed_run["candidate_id"],
                        "input": failed_run["proposed_input"],
                    },
                )
                failures.check(
                    replay.status == 409,
                    f"confirming the same candidate again is rejected ({replay.status})",
                )

        await browser.close()

    # 自己拉起的夹具站点由自己收尾；外部站点不动。
    if fixture.owned:
        fixture.stop()

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
    # 提交目标 = 建会话（含第 1 轮），URL 同时带上 session 与 run（CONTRACT §9.1）。
    await page.wait_for_url("**/?session=*", timeout=20000)
    await page.wait_for_url("**&run=*", timeout=20000)


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
