"""Typed browser operation API and live-process idempotency behavior."""

from __future__ import annotations

import asyncio
import json
import sys
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, patch

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

import httpx  # noqa: E402

from case_builder import build_case, goto_step  # noqa: E402
from local_site import LocalSite  # noqa: E402
from pw_support import run  # noqa: E402

from loop_worker.contracts import ExecutionResult  # noqa: E402
from loop_worker.idempotency import IdempotencyCache, canonical_request_hash  # noqa: E402
from loop_worker.main import create_app  # noqa: E402


def operation_body(session_id: str = "sess_operation") -> dict[str, object]:
    url = "https://fixture.invalid/start"
    return {
        "session_id": session_id,
        "case": build_case([goto_step(0, url, "/start")], base_url=url),
    }


def operation_result() -> ExecutionResult:
    return ExecutionResult(
        execution_id="exec_operation",
        status="passed",
        started_at="2026-10-21T12:00:00Z",
        finished_at="2026-10-21T12:00:01Z",
        final_url="https://fixture.invalid/start",
        steps=[],
        error=None,
    )


def failed_operation_result() -> ExecutionResult:
    return ExecutionResult.model_validate(
        {
            "execution_id": "exec_failed",
            "status": "failed",
            "started_at": "2026-10-21T12:00:00Z",
            "finished_at": "2026-10-21T12:00:01Z",
            "final_url": "https://fixture.invalid/start",
            "steps": [
                {
                    "index": 0,
                    "action": "goto",
                    "status": "failed",
                    "started_at": "2026-10-21T12:00:00Z",
                    "duration_ms": 1,
                    "url_before": "about:blank",
                    "url_after": "https://fixture.invalid/start",
                    "conditions": [],
                    "evidence": {
                        "screenshot_path": "",
                        "console": [],
                        "network": [],
                    },
                    "error": {
                        "kind": "condition_unmet",
                        "message": "page did not become ready",
                    },
                }
            ],
            "error": None,
        }
    )


class IdempotencyCacheTest(unittest.TestCase):
    def test_simultaneous_same_key_requests_share_one_execution(self) -> None:
        run(self._simultaneous_same_key_requests_share_one_execution())

    async def _simultaneous_same_key_requests_share_one_execution(self) -> None:
        cache: IdempotencyCache[str] = IdempotencyCache(
            max_entries=2, max_active=2
        )
        started = asyncio.Event()
        release = asyncio.Event()
        calls = 0

        async def factory() -> str:
            nonlocal calls
            calls += 1
            started.set()
            await release.wait()
            return "shared"

        first = asyncio.create_task(cache.execute("op_same", "hash", factory))
        await started.wait()
        second = asyncio.create_task(cache.execute("op_same", "hash", factory))
        await asyncio.sleep(0)
        release.set()

        self.assertEqual(await asyncio.gather(first, second), ["shared", "shared"])
        self.assertEqual(calls, 1)

    def test_cancelled_waiter_does_not_cancel_shared_work(self) -> None:
        run(self._cancelled_waiter_does_not_cancel_shared_work())

    async def _cancelled_waiter_does_not_cancel_shared_work(self) -> None:
        cache: IdempotencyCache[str] = IdempotencyCache(
            max_entries=2, max_active=2
        )
        started = asyncio.Event()
        release = asyncio.Event()
        calls = 0

        async def factory() -> str:
            nonlocal calls
            calls += 1
            started.set()
            await release.wait()
            return "completed"

        cancelled = asyncio.create_task(
            cache.execute("op_shared", "hash", factory)
        )
        await started.wait()
        survivor = asyncio.create_task(
            cache.execute("op_shared", "hash", factory)
        )
        await asyncio.sleep(0)

        cancelled.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await cancelled

        release.set()
        self.assertEqual(await survivor, "completed")
        self.assertEqual(calls, 1)

    def test_canonical_hash_is_stable_across_key_order(self) -> None:
        first = {"session_id": "sess", "case": {"name": "case", "steps": []}}
        second = {"case": {"steps": [], "name": "case"}, "session_id": "sess"}

        self.assertEqual(
            canonical_request_hash(first),
            canonical_request_hash(second),
        )


class OperationsAPITest(unittest.TestCase):
    def test_duplicate_operation_returns_cached_result(self) -> None:
        run(self._duplicate_operation_returns_cached_result())

    async def _duplicate_operation_returns_cached_result(self) -> None:
        app = create_app()
        app.state.session_manager.browser = AsyncMock(return_value=object())
        execute = AsyncMock(return_value=operation_result())
        transport = httpx.ASGITransport(app=app)

        try:
            with patch("loop_worker.main.run_case", new=execute):
                async with httpx.AsyncClient(
                    transport=transport, base_url="http://worker.test"
                ) as client:
                    headers = {
                        "Idempotency-Key": "op_same",
                        "X-AgentTrace-Run-ID": "run_same",
                        "X-AgentTrace-Span-ID": "span_same",
                    }
                    first = await client.post(
                        "/operations/browser_execute_case",
                        headers=headers,
                        json=operation_body(),
                    )
                    second = await client.post(
                        "/operations/browser_execute_case",
                        headers=headers,
                        json=operation_body(),
                    )
        finally:
            await app.state.session_manager.shutdown()

        self.assertEqual(first.status_code, 200, first.text)
        self.assertEqual(second.status_code, 200, second.text)
        self.assertEqual(first.json(), second.json())
        self.assertEqual(app.state.operation_calls["op_same"], 1)
        self.assertEqual(execute.await_count, 1)
        self.assertEqual(first.json()["status"], "succeeded")
        self.assertEqual(first.json()["output"]["execution_id"], "exec_operation")

    def test_distinct_active_operation_overload_is_typed(self) -> None:
        run(self._distinct_active_operation_overload_is_typed())

    async def _distinct_active_operation_overload_is_typed(self) -> None:
        app = create_app(operation_max_active=1)
        app.state.session_manager.browser = AsyncMock(return_value=object())
        started = asyncio.Event()
        release = asyncio.Event()
        calls = 0

        async def execute(*args: object, **kwargs: object) -> ExecutionResult:
            del args, kwargs
            nonlocal calls
            calls += 1
            started.set()
            await release.wait()
            return operation_result()

        transport = httpx.ASGITransport(app=app)
        try:
            with patch("loop_worker.main.run_case", new=execute):
                async with httpx.AsyncClient(
                    transport=transport, base_url="http://worker.test"
                ) as client:
                    first = asyncio.create_task(
                        client.post(
                            "/operations/browser_execute_case",
                            headers={
                                "Idempotency-Key": "op_active",
                                "X-AgentTrace-Run-ID": "run_active",
                                "X-AgentTrace-Span-ID": "span_active",
                            },
                            json=operation_body("sess_active"),
                        )
                    )
                    await started.wait()
                    overloaded = await client.post(
                        "/operations/browser_execute_case",
                        headers={
                            "Idempotency-Key": "op_overloaded",
                            "X-AgentTrace-Run-ID": "run_overloaded",
                            "X-AgentTrace-Span-ID": "span_overloaded",
                        },
                        json=operation_body("sess_overloaded"),
                    )
                    release.set()
                    completed = await first
        finally:
            release.set()
            await app.state.session_manager.shutdown()

        self.assertEqual(completed.status_code, 200, completed.text)
        self.assertEqual(overloaded.status_code, 503, overloaded.text)
        self.assertEqual(overloaded.json()["error"], "operation_overloaded")
        self.assertEqual(calls, 1)

    def test_operation_request_body_limit_returns_typed_413(self) -> None:
        run(self._operation_request_body_limit_returns_typed_413())

    async def _operation_request_body_limit_returns_typed_413(self) -> None:
        app = create_app(operation_max_request_bytes=128)
        transport = httpx.ASGITransport(app=app)
        body = json.dumps(operation_body()).encode("utf-8") + (b" " * 129)

        async def body_chunks():
            midpoint = len(body) // 2
            yield body[:midpoint]
            yield body[midpoint:]

        try:
            async with httpx.AsyncClient(
                transport=transport, base_url="http://worker.test"
            ) as client:
                response = await client.post(
                    "/operations/browser_execute_case",
                    headers={
                        "Content-Type": "application/json",
                        "Idempotency-Key": "op_oversized",
                        "X-AgentTrace-Run-ID": "run_oversized",
                        "X-AgentTrace-Span-ID": "span_oversized",
                    },
                    content=body_chunks(),
                )
        finally:
            await app.state.session_manager.shutdown()

        self.assertEqual(response.status_code, 413, response.text)
        self.assertEqual(response.json()["error"], "request_too_large")

    def test_failed_execution_promotes_step_error_to_operation_error(self) -> None:
        run(self._failed_execution_promotes_step_error_to_operation_error())

    async def _failed_execution_promotes_step_error_to_operation_error(self) -> None:
        app = create_app()
        app.state.session_manager.browser = AsyncMock(return_value=object())
        execute = AsyncMock(return_value=failed_operation_result())
        transport = httpx.ASGITransport(app=app)
        try:
            with patch("loop_worker.main.run_case", new=execute):
                async with httpx.AsyncClient(
                    transport=transport, base_url="http://worker.test"
                ) as client:
                    response = await client.post(
                        "/operations/browser_execute_case",
                        headers={
                            "Idempotency-Key": "op_failed",
                            "X-AgentTrace-Run-ID": "run_failed",
                            "X-AgentTrace-Span-ID": "span_failed",
                        },
                        json=operation_body(),
                    )
        finally:
            await app.state.session_manager.shutdown()

        self.assertEqual(response.status_code, 200, response.text)
        self.assertEqual(response.json()["status"], "failed")
        self.assertEqual(
            response.json()["error"]["kind"],
            "condition_unmet",
        )

    def test_completed_operation_cache_is_bounded(self) -> None:
        run(self._completed_operation_cache_is_bounded())

    async def _completed_operation_cache_is_bounded(self) -> None:
        app = create_app(operation_cache_size=1)
        app.state.session_manager.browser = AsyncMock(return_value=object())
        execute = AsyncMock(return_value=operation_result())
        transport = httpx.ASGITransport(app=app)

        try:
            with patch("loop_worker.main.run_case", new=execute):
                async with httpx.AsyncClient(
                    transport=transport, base_url="http://worker.test"
                ) as client:
                    for operation_id, session_id in (
                        ("op_first", "sess_first"),
                        ("op_second", "sess_second"),
                        ("op_first", "sess_first"),
                    ):
                        response = await client.post(
                            "/operations/browser_execute_case",
                            headers={
                                "Idempotency-Key": operation_id,
                                "X-AgentTrace-Run-ID": "run_bounded",
                                "X-AgentTrace-Span-ID": "span_bounded",
                            },
                            json=operation_body(session_id),
                        )
                        self.assertEqual(response.status_code, 200, response.text)
        finally:
            await app.state.session_manager.shutdown()

        self.assertEqual(app.state.operation_calls["op_first"], 2)
        self.assertEqual(app.state.operation_calls["op_second"], 1)
        self.assertEqual(execute.await_count, 3)

    def test_reusing_operation_key_with_different_body_conflicts(self) -> None:
        run(self._reusing_operation_key_with_different_body_conflicts())

    async def _reusing_operation_key_with_different_body_conflicts(self) -> None:
        app = create_app()
        app.state.session_manager.browser = AsyncMock(return_value=object())
        execute = AsyncMock(return_value=operation_result())
        transport = httpx.ASGITransport(app=app)

        try:
            with patch("loop_worker.main.run_case", new=execute):
                async with httpx.AsyncClient(
                    transport=transport, base_url="http://worker.test"
                ) as client:
                    headers = {
                        "Idempotency-Key": "op_conflict",
                        "X-AgentTrace-Run-ID": "run_conflict",
                        "X-AgentTrace-Span-ID": "span_conflict",
                    }
                    first = await client.post(
                        "/operations/browser_execute_case",
                        headers=headers,
                        json=operation_body("sess_first"),
                    )
                    second = await client.post(
                        "/operations/browser_execute_case",
                        headers=headers,
                        json=operation_body("sess_second"),
                    )
        finally:
            await app.state.session_manager.shutdown()

        self.assertEqual(first.status_code, 200, first.text)
        self.assertEqual(second.status_code, 409, second.text)
        self.assertEqual(second.json()["error"], "idempotency_conflict")
        self.assertEqual(app.state.operation_calls["op_conflict"], 1)
        self.assertEqual(execute.await_count, 1)

    def test_operation_metadata_declares_browser_safety_boundary(self) -> None:
        run(self._operation_metadata_declares_browser_safety_boundary())

    async def _operation_metadata_declares_browser_safety_boundary(self) -> None:
        app = create_app()
        transport = httpx.ASGITransport(app=app)
        try:
            async with httpx.AsyncClient(
                transport=transport, base_url="http://worker.test"
            ) as client:
                response = await client.get("/operations")
        finally:
            await app.state.session_manager.shutdown()

        self.assertEqual(response.status_code, 200, response.text)
        operation = response.json()["operations"][0]
        self.assertEqual(operation["name"], "browser_execute_case")
        self.assertTrue(operation["fresh_context"])
        self.assertTrue(operation["disposable_fixture_data"])
        self.assertEqual(
            operation["irreversible_production_operations"], "unsupported"
        )

    def test_operation_route_executes_real_playwright_adapter(self) -> None:
        run(self._operation_route_executes_real_playwright_adapter())

    async def _operation_route_executes_real_playwright_adapter(self) -> None:
        with LocalSite() as site:
            app = create_app()
            transport = httpx.ASGITransport(app=app)
            body = {
                "session_id": "sess_real_adapter",
                "case": build_case(
                    [goto_step(0, site.url("index.html"), "/index.html")],
                    base_url=site.base_url,
                ),
            }
            try:
                async with httpx.AsyncClient(
                    transport=transport, base_url="http://worker.test"
                ) as client:
                    response = await client.post(
                        "/operations/browser_execute_case",
                        headers={
                            "Idempotency-Key": "op_real_adapter",
                            "X-AgentTrace-Run-ID": "run_real_adapter",
                            "X-AgentTrace-Span-ID": "span_real_adapter",
                        },
                        json=body,
                    )
            finally:
                await app.state.session_manager.shutdown()

        self.assertEqual(response.status_code, 200, response.text)
        result = response.json()
        self.assertEqual(result["status"], "succeeded", result)
        self.assertEqual(result["output"]["status"], "passed", result)
        self.assertEqual(result["output"]["execution_id"], "op_real_adapter")
        self.assertTrue(result["artifacts"]["screenshot_paths"], result)
        self.assertIsNone(result["error"])


if __name__ == "__main__":
    unittest.main()
