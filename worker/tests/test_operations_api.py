"""Typed browser operation API and live-process idempotency behavior."""

from __future__ import annotations

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
from pw_support import run  # noqa: E402

from loop_worker.contracts import ExecutionResult  # noqa: E402
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


if __name__ == "__main__":
    unittest.main()
