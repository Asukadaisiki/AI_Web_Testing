"""失败信号的分类必须准确：超时 ≠ 硬失败。

报告里的 kind 会直接决定"失败回灌"给出什么建议，因此把 `ERR_CONNECTION_REFUSED`
说成"步骤超时"是不能接受的（会让人去改超时，而真相是站点连不上）。
"""

from __future__ import annotations

import functools
import sys
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

# 两种发现方式（`discover -s tests` 与 `-t .`）下都能导入测试支撑模块。
for _path in (Path(__file__).resolve().parents[1], Path(__file__).resolve().parent):
    if str(_path) not in sys.path:
        sys.path.insert(0, str(_path))

import pw_support  # noqa: E402
from loop_worker import actions  # noqa: E402
from loop_worker.contracts import (  # noqa: E402
    SIGNAL_STEP_TIMEOUT,
    SIGNAL_TARGET_NOT_FOUND,
    SIGNAL_WORKER_ERROR,
)
from loop_worker.runner import run_case  # noqa: E402


class _SlowHandler(BaseHTTPRequestHandler):
    """一个故意慢的端点，用来造出真正的超时。"""

    def do_GET(self) -> None:  # noqa: N802
        time.sleep(3)
        body = b"<html><body><h1>slow</h1></body></html>"
        self.send_response(200)
        self.send_header("Content-Type", "text/html")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A002
        return


class _SlowSite:
    def __enter__(self) -> _SlowSite:
        self._server = ThreadingHTTPServer(("127.0.0.1", 0), _SlowHandler)
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        return self

    def __exit__(self, *exc_info: object) -> None:
        self._server.shutdown()
        self._server.server_close()
        self._thread.join(timeout=5)

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self._server.server_address[1]}/"


def goto_case(url: str, timeout_ms: int) -> dict:
    return {
        "case_version": "loop.case.v1",
        "name": "goto",
        "goal": "open a page",
        "base_url": url,
        "steps": [
            {
                "index": 0,
                "action": "goto",
                "intent": "open it",
                "value": url,
                "preconditions": [],
                "postconditions": [{"type": "url_contains", "value": "/", "timeout_ms": timeout_ms}],
                "timeout_ms": timeout_ms,
            }
        ],
    }


class FailureKindTest(unittest.TestCase):
    def test_helpers_carry_the_right_kind(self) -> None:
        self.assertEqual(actions.step_timeout("x").kind, SIGNAL_STEP_TIMEOUT)
        self.assertEqual(actions.worker_error("x").kind, SIGNAL_WORKER_ERROR)
        self.assertEqual(actions.target_not_found("x").kind, SIGNAL_TARGET_NOT_FOUND)

    def test_connection_refused_is_worker_error_not_timeout(self) -> None:
        async def scenario():
            async with pw_support.launched_browser() as browser:
                # 端口 1 上不会有任何东西在听：这是硬失败，不是超时。
                return await run_case(goto_case("http://127.0.0.1:1/", 3000), browser=browser, session_id="sess_test")

        result = pw_support.run(scenario())
        self.assertEqual(result.status, "failed")
        self.assertEqual(len(result.steps), 1)
        step = result.steps[0]
        self.assertIsNotNone(step.error)
        self.assertEqual(step.error.kind, SIGNAL_WORKER_ERROR, step.error.message)
        self.assertIn("failed", step.error.message)
        # 顶层 error 必须为空：用例失败不等于执行器故障。
        self.assertIsNone(result.error)

    def test_real_timeout_is_step_timeout(self) -> None:
        with _SlowSite() as site:

            async def scenario():
                async with pw_support.launched_browser() as browser:
                    return await run_case(goto_case(site.url, 400), browser=browser, session_id="sess_test")

            result = pw_support.run(scenario())

        self.assertEqual(result.status, "failed")
        self.assertEqual(result.steps[0].error.kind, SIGNAL_STEP_TIMEOUT, result.steps[0].error.message)
        self.assertIsNone(result.error)


if __name__ == "__main__":
    unittest.main()
