from __future__ import annotations

import asyncio
import json
import socket
import threading
import time
import unittest
from urllib.request import Request, urlopen
from unittest.mock import patch

from fastapi import FastAPI
import uvicorn

from app.ai.page_explorer import BrowserSessionManager
from app.api.router import build_api_router
from app.api.routes import browser_capabilities as capability_routes
from app.application.browser.service import (
    _BrowserCapabilityRuntime,
    shutdown_browser_capabilities,
)
from app.db import get_db_session


class _FakePage:
    def __init__(self, calls: list[tuple[str, int, str]]) -> None:
        self.url = "about:blank"
        self._calls = calls

    def evaluate(self, _expression: str) -> int:
        del _expression
        self._record("evaluate")
        return 1

    def goto(self, url: str, **_kwargs: object) -> None:
        del _kwargs
        self._record("goto")
        self.url = url

    def wait_for_load_state(self, _state: str, **_kwargs: object) -> None:
        del _state, _kwargs
        self._record("wait_for_load_state")

    def _record(self, operation: str) -> None:
        self._calls.append(
            (operation, threading.get_ident(), threading.current_thread().name)
        )


class _FakeContext:
    def __init__(self, page: _FakePage) -> None:
        self._page = page

    def new_page(self) -> _FakePage:
        return self._page

    def close(self) -> None:
        self._page._record("context_close")


class _FakeBrowser:
    def __init__(self, page: _FakePage) -> None:
        self._page = page

    def new_context(self, **_kwargs: object) -> _FakeContext:
        del _kwargs
        return _FakeContext(self._page)

    def close(self) -> None:
        self._page._record("browser_close")


class _FakeChromium:
    def __init__(self, page: _FakePage) -> None:
        self._page = page

    def launch(self, **_kwargs: object) -> _FakeBrowser:
        del _kwargs
        return _FakeBrowser(self._page)


class _FakePlaywright:
    def __init__(self, page: _FakePage) -> None:
        self.chromium = _FakeChromium(page)


class _FakePlaywrightContext:
    def __init__(self, calls: list[tuple[str, int, str]]) -> None:
        self._calls = calls
        self._page = _FakePage(calls)

    def __enter__(self) -> _FakePlaywright:
        try:
            asyncio.get_running_loop()
        except RuntimeError:
            pass
        else:
            raise AssertionError("Sync Playwright started inside a running asyncio loop")
        self._page._record("sync_playwright_enter")
        return _FakePlaywright(self._page)

    def __exit__(self, *_args: object) -> None:
        del _args
        return None


class BrowserCapabilityHTTPTest(unittest.TestCase):
    def setUp(self) -> None:
        shutdown_browser_capabilities()
        BrowserSessionManager._sessions.clear()
        self.calls: list[tuple[str, int, str]] = []

        app = FastAPI()
        app.include_router(build_api_router())
        app.dependency_overrides[get_db_session] = lambda: None

        self.validate_patch = patch.object(
            capability_routes,
            "validate_capability_context",
            return_value=None,
        )
        self.playwright_patch = patch(
            "app.ai.page_explorer.sync_playwright",
            side_effect=lambda: _FakePlaywrightContext(self.calls),
        )
        self.nodes_patch = patch(
            "app.ai.page_explorer.collect_a11y_nodes",
            return_value=[],
        )
        self.service_nodes_patch = patch(
            "app.application.browser.service.collect_a11y_nodes",
            return_value=[],
        )
        self.validate_patch.start()
        self.playwright_patch.start()
        self.nodes_patch.start()
        self.service_nodes_patch.start()

        sock = socket.socket()
        sock.bind(("127.0.0.1", 0))
        self.port = sock.getsockname()[1]
        sock.close()
        config = uvicorn.Config(
            app,
            host="127.0.0.1",
            port=self.port,
            log_level="critical",
        )
        self.server = uvicorn.Server(config)
        self.server_thread = threading.Thread(target=self.server.run, daemon=True)
        self.server_thread.start()
        deadline = time.monotonic() + 5
        while not self.server.started and time.monotonic() < deadline:
            time.sleep(0.01)
        self.assertTrue(self.server.started)

    def tearDown(self) -> None:
        self.server.should_exit = True
        self.server_thread.join(timeout=5)
        shutdown_browser_capabilities()
        self.service_nodes_patch.stop()
        self.nodes_patch.stop()
        self.playwright_patch.stop()
        self.validate_patch.stop()

    def test_explore_routes_keep_sync_playwright_on_dedicated_thread(self) -> None:
        page_response = self._post(
            "explore_page",
            {"url": "http://local.test/page"},
        )
        flow_response = self._post(
            "explore_flow",
            {
                "base_url": "http://local.test",
                "steps": [{"url": "/flow"}],
            },
        )

        self.assertEqual(page_response["result"]["url"], "http://local.test/page")
        self.assertEqual(flow_response["result"]["total_pages"], 1)
        self.assertTrue(self.calls)
        thread_ids = {thread_id for _, thread_id, _ in self.calls}
        self.assertEqual(len(thread_ids), 1)
        self.assertTrue(
            all(name.startswith("browser-capability") for _, _, name in self.calls)
        )
        self.assertIn("sync_playwright_enter", [name for name, _, _ in self.calls])

    def test_three_http_sessions_share_runtime_and_close_on_owner_thread(self) -> None:
        for conversation_id in ("201", "202", "203"):
            response = self._post(
                "explore_page",
                {"url": f"http://local.test/{conversation_id}"},
                conversation_id=conversation_id,
            )
            self.assertEqual(
                response["result"]["url"],
                f"http://local.test/{conversation_id}",
            )
            _BrowserCapabilityRuntime.run(
                lambda session_id=int(conversation_id): (
                    BrowserSessionManager.close_session(session_id)
                )
            )

        operation_names = [name for name, _, _ in self.calls]
        self.assertEqual(operation_names.count("sync_playwright_enter"), 1)
        self.assertEqual(operation_names.count("context_close"), 3)
        self.assertEqual(
            len({thread_id for _, thread_id, _ in self.calls}),
            1,
        )

    def _post(
        self,
        capability: str,
        arguments: dict[str, object],
        *,
        conversation_id: str = "101",
    ) -> dict:
        payload = json.dumps(
            {
                "actor_user_id": 1,
                "project_id": 1,
                "conversation_id": conversation_id,
                "arguments": arguments,
            }
        ).encode()
        request = Request(
            f"http://127.0.0.1:{self.port}/api/v1/internal/browser-capabilities/{capability}",
            data=payload,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urlopen(request, timeout=5) as response:
            return json.load(response)


if __name__ == "__main__":
    unittest.main()
