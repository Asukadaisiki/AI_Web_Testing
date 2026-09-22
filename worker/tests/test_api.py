"""HTTP API 形状（Go 侧依赖）—— 用 httpx ASGITransport 直接打 app，不占端口。

覆盖：`/health`、会话生命周期（create → navigate → act → delete）、404 session_not_found、
`/execute` 的 400（契约非法 / body 形状非法 / 缺 session_id）与 200（跑通一个本地 case）。
"""

from __future__ import annotations

import contextlib
import sys
import unittest
from pathlib import Path
from typing import AsyncIterator

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

import httpx  # noqa: E402

from case_builder import (  # noqa: E402
    action_step,
    build_case,
    condition,
    css_locator,
    goto_step,
    target,
)
from local_site import LocalSite  # noqa: E402
from pw_support import run  # noqa: E402

from loop_worker.evidence import artifacts_dir, session_dir  # noqa: E402
from loop_worker.main import create_app  # noqa: E402

#: 领域会话 id：执行器据此把证据落到 <产物根>/<session_id>/（CONTRACT §9.2）。
SESSION = "sess_test"


@contextlib.asynccontextmanager
async def api_client() -> AsyncIterator[httpx.AsyncClient]:
    app = create_app()
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://worker.test") as client:
        try:
            yield client
        finally:
            await app.state.session_manager.shutdown()


class HealthAndErrorsTest(unittest.TestCase):
    """不需要浏览器的部分。"""

    def test_health_returns_ok(self) -> None:
        run(self._health_returns_ok())

    async def _health_returns_ok(self) -> None:
        async with api_client() as client:
            response = await client.get("/health")
            self.assertEqual(response.status_code, 200)
            body = response.json()
            self.assertEqual(body["status"], "ok")
            # 控制面靠这个字段对账证据目录：缺了它，截图 404 就查不出来。
            self.assertEqual(body["artifacts_dir"], str(artifacts_dir()))

    def test_unknown_session_is_404(self) -> None:
        run(self._unknown_session_is_404())

    async def _unknown_session_is_404(self) -> None:
        async with api_client() as client:
            for method, url, payload in (
                ("post", "/sessions/sess_missing/navigate", {"url": "http://127.0.0.1:1/"}),
                (
                    "post",
                    "/sessions/sess_missing/act",
                    {"action": "click", "locator": {"kind": "css", "css": "#x"}},
                ),
                ("delete", "/sessions/sess_missing", None),
            ):
                with self.subTest(method=method, url=url):
                    if method == "delete":
                        response = await client.delete(url)
                    else:
                        response = await getattr(client, method)(url, json=payload)
                    self.assertEqual(response.status_code, 404)
                    body = response.json()
                    self.assertEqual(body["error"], "session_not_found")
                    self.assertTrue(body["detail"])

    def test_execute_rejects_non_goto_first_with_400(self) -> None:
        run(self._execute_rejects_non_goto_first_with_400())

    async def _execute_rejects_non_goto_first_with_400(self) -> None:
        async with api_client() as client:
            case = build_case(
                [
                    action_step(
                        0,
                        "assert_text",
                        page_url="http://127.0.0.1/index.html",
                        pre=[condition("url_contains", "/index.html")],
                        post=[condition("text_visible", "Alpha")],
                        value="Alpha",
                    )
                ]
            )
            response = await client.post(
                "/execute", json={"session_id": SESSION, "case": case}
            )
            self.assertEqual(response.status_code, 400)
            body = response.json()
            self.assertEqual(body["error"], "case_not_goto_first")
            self.assertTrue(body["detail"])

    def test_execute_rejects_malformed_body_with_400(self) -> None:
        run(self._execute_rejects_malformed_body_with_400())

    async def _execute_rejects_malformed_body_with_400(self) -> None:
        async with api_client() as client:
            response = await client.post("/execute", json={"case": "not an object"})
            self.assertEqual(response.status_code, 400)
            self.assertEqual(response.json()["error"], "case_invalid_json")

    def test_execute_requires_a_session_id(self) -> None:
        run(self._execute_requires_a_session_id())

    async def _execute_requires_a_session_id(self) -> None:
        # 没有会话就没法归属产物，所以 body 里必须带 session_id（CONTRACT §9.2）。
        async with api_client() as client:
            response = await client.post("/execute", json={"case": {}})
            self.assertEqual(response.status_code, 400)
            self.assertIn("session_id", response.text)


class SessionLifecycleTest(unittest.TestCase):
    """真实浏览器：create → navigate → act → delete。"""

    def test_session_observe_act_and_close(self) -> None:
        run(self._session_observe_act_and_close())

    async def _session_observe_act_and_close(self) -> None:
        with LocalSite() as site:
            index_url = site.url("index.html")
            async with api_client() as client:
                created = await client.post("/sessions", json={"session_id": SESSION})
                self.assertEqual(created.status_code, 200)
                browser_session_id = created.json()["session_id"]
                # 返回的是浏览器会话句柄，不是领域会话（CONTRACT §9.3）。
                self.assertTrue(browser_session_id.startswith("bsess_"), browser_session_id)
                session_id = browser_session_id

                navigated = await client.post(
                    f"/sessions/{session_id}/navigate", json={"url": index_url}
                )
                self.assertEqual(navigated.status_code, 200, navigated.text)
                observation = navigated.json()
                self.assertTrue(observation["observation_id"].startswith("obs_"))
                self.assertTrue(observation["page_state_id"].startswith("ps_"))
                self.assertEqual(observation["url"], index_url)
                self.assertIn("Demo Catalog", observation["title"])
                self.assertTrue(observation["screenshot_path"])
                # 观测截图必须落在本会话的目录里（CONTRACT §9.2）。
                self.assertTrue(
                    observation["screenshot_path"].startswith(f"{SESSION}/"),
                    observation["screenshot_path"],
                )
                self.assertTrue(
                    (artifacts_dir() / observation["screenshot_path"]).is_relative_to(
                        session_dir(SESSION)
                    )
                )
                self.assertEqual(observation["browser_session_id"], browser_session_id)
                self.assertTrue(observation["elements"])
                for element in observation["elements"]:
                    self.assertTrue(element["locators"])
                    for locator in element["locators"]:
                        self.assertEqual(locator["match_count"], 1, locator)

                typed = await client.post(
                    f"/sessions/{session_id}/act",
                    json={
                        "action": "input",
                        "locator": {"kind": "css", "css": "#search"},
                        "value": "alpha",
                    },
                )
                self.assertEqual(typed.status_code, 200, typed.text)
                search_element = next(
                    element
                    for element in typed.json()["elements"]
                    if element["value"] == "alpha"
                )
                self.assertEqual(search_element["tag"], "input")

                clicked = await client.post(
                    f"/sessions/{session_id}/act",
                    json={"action": "click", "locator": {"kind": "css", "css": "#filter"}},
                )
                self.assertEqual(clicked.status_code, 200, clicked.text)

                missing_target = await client.post(
                    f"/sessions/{session_id}/act",
                    json={
                        "action": "click",
                        "locator": {"kind": "css", "css": "#no-such-element"},
                    },
                )
                self.assertEqual(missing_target.status_code, 400)
                self.assertEqual(missing_target.json()["error"], "target_not_found")

                closed = await client.delete(f"/sessions/{session_id}")
                self.assertEqual(closed.status_code, 200)
                self.assertEqual(closed.json(), {"closed": True})

                after = await client.delete(f"/sessions/{session_id}")
                self.assertEqual(after.status_code, 404)
                self.assertEqual(after.json()["error"], "session_not_found")

    def test_execute_runs_a_case_over_http(self) -> None:
        run(self._execute_runs_a_case_over_http())

    async def _execute_runs_a_case_over_http(self) -> None:
        with LocalSite() as site:
            index_url = site.url("index.html")
            case = build_case(
                [
                    goto_step(0, index_url, "/index.html"),
                    action_step(
                        1,
                        "input",
                        page_url=index_url,
                        locator=css_locator("#search"),
                        pre=[condition("url_contains", "/index.html")],
                        post=[condition("value_equals", "alpha")],
                        value="alpha",
                    ),
                    action_step(
                        2,
                        "click",
                        page_url=index_url,
                        locator=css_locator("#filter"),
                        pre=[condition("url_contains", "/index.html")],
                        post=[condition("text_gone", "Beta")],
                    ),
                    action_step(
                        3,
                        "assert_text",
                        page_url=index_url,
                        pre=[condition("url_contains", "/index.html")],
                        post=[condition("text_visible", "Alpha")],
                        value="Alpha",
                    ),
                ],
                base_url=site.base_url,
            )
            async with api_client() as client:
                response = await client.post(
                    "/execute", json={"session_id": SESSION, "case": case}
                )
                self.assertEqual(response.status_code, 200, response.text)
                result = response.json()
                self.assertEqual(result["status"], "passed", result)
                self.assertEqual(len(result["steps"]), 4)
                self.assertIsNone(result["error"])
                self.assertEqual(result["steps"][0]["url_before"], "about:blank")
                self.assertTrue(result["final_url"].endswith("/index.html"))
                for step in result["steps"]:
                    self.assertEqual(step["status"], "passed")
                    self.assertTrue(step["evidence"]["screenshot_path"])
                    # 执行期每步截图也按会话分目录。
                    self.assertTrue(
                        step["evidence"]["screenshot_path"].startswith(f"{SESSION}/"),
                        step["evidence"]["screenshot_path"],
                    )
                    self.assertIsInstance(step["evidence"]["console"], list)
                    self.assertIsInstance(step["evidence"]["network"], list)
                    self.assertIsNone(step["error"])


if __name__ == "__main__":
    unittest.main()
