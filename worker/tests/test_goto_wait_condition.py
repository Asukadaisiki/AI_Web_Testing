"""`goto` 的等待条件：`commit`，不是 `domcontentloaded`。

来自真实站点的教训（automationexercise.com，2026-09-23）：该站在 HTTPS 下引用
`http://fonts.googleapis.com/...`，被浏览器按 Mixed Content 拦掉，挂起的外部资源把
DOMContentLoaded 拖到 20s 之后。于是"页面其实早就好了、URL 也对，但 goto 报
step_timeout"——干跑偶然在 20s 内过、真实执行 20.8s 就红，同一份 case 一会儿过一会儿不过。

这个测试自带一个**故意不响应**的静态资源服务，所以它不会空过：
先证明夹具真的会挂（`load` 在 2s 内到不了），再证明 `goto` 照样成功。
如果把 `commit` 改回 `domcontentloaded`，这个测试立刻变红。
"""

from __future__ import annotations

import sys
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from pw_support import launched_browser, run  # noqa: E402

from loop_worker import actions  # noqa: E402

#: 挂起资源的响应延迟，取得比任何断言超时都长
HANG_SECONDS = 30

PAGE = """<!doctype html>
<html><head>
<link rel="stylesheet" href="/hang.css">
<script>document.title = "script ran";</script>
</head><body><h1>content is here</h1></body></html>
"""


class _Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler 的接口
        if self.path.startswith("/hang"):
            # 接受连接但永不响应：模拟被挂起/被拦的外部资源。
            time.sleep(HANG_SECONDS)
            return
        body = PAGE.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args: object) -> None:
        """静音：测试输出不该被 HTTP 日志淹没。"""


class HangingSite:
    """一个总是把一个子资源挂起的站点。"""

    def __init__(self) -> None:
        self._server = ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)

    def __enter__(self) -> "HangingSite":
        self._thread.start()
        return self

    def __exit__(self, *exc: object) -> None:
        self._server.shutdown()
        self._server.server_close()

    @property
    def page_url(self) -> str:
        host, port = self._server.server_address[:2]
        return f"http://{host}:{port}/page.html"


class GotoWaitConditionTest(unittest.TestCase):
    def test_goto_succeeds_while_a_subresource_is_still_hanging(self) -> None:
        run(self._goto_succeeds_while_a_subresource_is_still_hanging())

    async def _goto_succeeds_while_a_subresource_is_still_hanging(self) -> None:
        with HangingSite() as site:
            async with launched_browser() as browser:
                page = await browser.new_page()

                # 步超时给得比挂起时间短得多：旧实现（等 domcontentloaded）必然红。
                await actions.goto(page, site.page_url, timeout_ms=5000)

                # 自证夹具有效：DOMContentLoaded 确实还没到。
                # 样式表后面的经典脚本必须等样式表加载完才执行，DOMContentLoaded 又等
                # 那个脚本——这正是真实站点被挂起的外部样式表拖死的机制。
                # 没有这一步，测试可能因为"资源其实没挂"而空过，那它就不再是回归测试。
                with self.assertRaises(Exception):
                    await page.wait_for_load_state("domcontentloaded", timeout=2000)

                # 注意这里**不**断言 body 已经存在。`commit` 的语义就是"导航已提交"，
                # 此时 DOM 可能只解析到 <head>——内容就绪是**后置条件轮询**的职责
                # （每个条件有自己的 timeout_ms），不是 goto 的职责。
                # 真实站点上这一点已被验证：同一个 case 连跑 3 轮，goto 全过，
                # 后一步 assert_text 的 text_visible 也都等到了内容。


if __name__ == "__main__":
    unittest.main()
