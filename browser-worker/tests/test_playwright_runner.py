from __future__ import annotations

import unittest
from pathlib import Path
from unittest.mock import patch

from app.runners.click_preprocessor import ClickPrecheckResult
from app.runners.playwright_runner import (
    RunnerExecutionError,
    StepStreamEvent,
    _execute_non_target_step,
    _execute_step_with_candidates,
    execute_case_with_playwright,
)
from app.schemas.dsl import DSLCase
from app.schemas.executions import StepExecutionEvidence


class _Locator:
    def __init__(self, *, tag: str, href: str = "") -> None:
        self.tag = tag
        self.href = href

    def count(self) -> int:
        return 1

    def evaluate(self, script: str):
        if "getAttribute('href')" in script:
            return {"tag": self.tag, "href": self.href, "download": False}
        if "tagName.toLowerCase" in script:
            return self.tag
        return ""

    def all(self) -> list[object]:
        return []

    def inner_text(self) -> str:
        return ""


class _Page:
    def __init__(self, locator: _Locator) -> None:
        self.url = "https://example.test/products"
        self._locator = locator
        self.goto_calls: list[str] = []
        self.viewport_size = {"width": 1280, "height": 720}
        self.listeners: dict[str, list] = {}

    def locator(self, _selector: str) -> _Locator:
        return self._locator

    def get_by_role(self, *_args, **_kwargs) -> _Locator:
        return self._locator

    def evaluate(self, script: str):
        if "button_count" in script:
            return {
                "text_preview": None,
                "button_count": 0,
                "input_count": 0,
                "link_count": 1,
            }
        return ""

    def goto(self, url: str, **_kwargs) -> None:
        self.goto_calls.append(url)
        self.url = url

    def title(self) -> str:
        return "Example"

    def on(self, event_name: str, callback) -> None:
        self.listeners.setdefault(event_name, []).append(callback)

    def remove_listener(self, event_name: str, callback) -> None:
        self.listeners[event_name].remove(callback)

    def emit(self, event_name: str, event) -> None:
        for callback in list(self.listeners.get(event_name, [])):
            callback(event)


class PlaywrightRunnerNavigationFallbackTest(unittest.TestCase):
    def test_failed_read_only_action_has_explicit_failed_outcome(self) -> None:
        page = _Page(_Locator(tag="body"))
        case = DSLCase.model_validate(
            {
                "name": "assert",
                "steps": [
                    {
                        "action": "assert_url_contains",
                        "value": "/missing",
                    }
                ],
            }
        )

        with self.assertRaises(RunnerExecutionError) as raised:
            _execute_non_target_step(
                page,
                case.steps[0],
                0,
                base_url=None,
                artifact_dir=Path("."),
                input_values={},
            )

        evidence = raised.exception.step_evidence
        self.assertEqual(evidence.action_outcome.status, "failed")
        self.assertEqual(evidence.action_outcome.side_effect_state, "not_applicable")

    def test_sync_consumes_the_same_streaming_evidence(self) -> None:
        case = DSLCase.model_validate(
            {
                "name": "same path",
                "steps": [{"action": "goto", "value": "https://example.test"}],
            }
        )
        expected = [
            StepExecutionEvidence(
                step_index=0,
                action="goto",
                status="passed",
                action_outcome={
                    "status": "succeeded",
                    "side_effect_state": "committed",
                },
            )
        ]

        def stream(**_kwargs):
            yield StepStreamEvent(
                type="step_start",
                step_index=0,
                action="goto",
            )
            return expected

        with patch(
            "app.runners.playwright_runner.execute_case_with_playwright_streaming",
            side_effect=stream,
        ):
            actual = execute_case_with_playwright(
                case=case,
                execution_id=1,
                base_url=None,
            )

        self.assertEqual(
            [item.model_dump(mode="json") for item in actual],
            [item.model_dump(mode="json") for item in expected],
        )

    def test_verified_anchor_clicks_once_then_gotos_once(self) -> None:
        locator = _Locator(tag="a", href="/details/1")
        page = _Page(locator)
        case = DSLCase.model_validate(
            {
                "name": "anchor",
                "steps": [
                    {
                        "action": "click",
                        "target": "Details",
                        "candidates": [
                            {
                                "strategy": "css",
                                "selector": "a[href='/details/1']",
                                "pre_score": 1,
                                "pre_features": {
                                    "verified_href": "/details/1",
                                },
                            }
                        ],
                        "postconditions": [
                            {
                                "type": "url_contains",
                                "value": "/details/1",
                                "timeout_ms": 100,
                            }
                        ],
                    }
                ],
            }
        )
        click_calls = 0

        def click(_page, _locator):
            nonlocal click_calls
            click_calls += 1
            page.url = "https://example.test/products#interstitial"
            return ClickPrecheckResult(succeeded=True)

        with patch(
            "app.runners.playwright_runner.click_with_precheck",
            side_effect=click,
        ):
            evidence = _execute_step_with_candidates(page, case.steps[0], 0)

        self.assertEqual(click_calls, 1)
        self.assertEqual(page.goto_calls, ["https://example.test/details/1"])
        self.assertEqual(evidence.click_recovery, "href_navigation_fallback")
        self.assertEqual(evidence.url, "https://example.test/details/1")

    def test_button_is_not_replayed_or_navigated_after_failed_postcondition(self) -> None:
        locator = _Locator(tag="button")
        page = _Page(locator)
        case = DSLCase.model_validate(
            {
                "name": "button",
                "steps": [
                    {
                        "action": "click",
                        "target": "Submit",
                        "candidates": [
                            {
                                "strategy": "role",
                                "selector": "button",
                                "semantic_value": "Submit",
                                "pre_score": 1,
                            },
                            {
                                "strategy": "text",
                                "selector": "Submit",
                                "pre_score": 0.5,
                            },
                        ],
                        "postconditions": [
                            {
                                "type": "url_contains",
                                "value": "/done",
                                "timeout_ms": 100,
                            }
                        ],
                    }
                ],
            }
        )
        click_calls = 0

        def click(_page, _locator):
            nonlocal click_calls
            click_calls += 1
            return ClickPrecheckResult(succeeded=True)

        with (
            patch(
                "app.runners.playwright_runner.click_with_precheck",
                side_effect=click,
            ),
            self.assertRaises(RunnerExecutionError) as raised,
        ):
            _execute_step_with_candidates(page, case.steps[0], 0)

        self.assertEqual(click_calls, 1)
        self.assertEqual(page.goto_calls, [])
        evidence = raised.exception.step_evidence
        self.assertIsNotNone(evidence)
        self.assertEqual(evidence.action_outcome.status, "succeeded")
        self.assertEqual(evidence.action_outcome.side_effect_state, "committed")
        self.assertEqual(evidence.condition_results[0].phase, "postcondition")
        self.assertEqual(evidence.condition_results[0].status, "failed")

    def test_click_exception_is_unknown_and_does_not_switch_candidate(self) -> None:
        locator = _Locator(tag="button")
        page = _Page(locator)
        case = DSLCase.model_validate(
            {
                "name": "uncertain click",
                "steps": [
                    {
                        "action": "click",
                        "target": "Submit",
                        "candidates": [
                            {
                                "strategy": "role",
                                "selector": "button",
                                "semantic_value": "Submit",
                                "pre_score": 1,
                            },
                            {
                                "strategy": "text",
                                "selector": "Submit",
                                "pre_score": 0.5,
                            },
                        ],
                    }
                ],
            }
        )

        with (
            patch(
                "app.runners.playwright_runner.click_with_precheck",
                side_effect=RuntimeError("connection lost after dispatch"),
            ) as click,
            self.assertRaises(RunnerExecutionError) as raised,
        ):
            _execute_step_with_candidates(page, case.steps[0], 0)

        self.assertEqual(click.call_count, 1)
        evidence = raised.exception.step_evidence
        self.assertEqual(evidence.action_outcome.status, "unknown")
        self.assertEqual(evidence.action_outcome.side_effect_state, "unknown")

    def test_failed_precondition_never_dispatches_click(self) -> None:
        locator = _Locator(tag="button")
        page = _Page(locator)
        case = DSLCase.model_validate(
            {
                "name": "precondition",
                "steps": [
                    {
                        "action": "click",
                        "target": "Add to cart",
                        "candidates": [
                            {
                                "strategy": "role",
                                "selector": "button",
                                "semantic_value": "Add to cart",
                                "pre_score": 1,
                            }
                        ],
                        "preconditions": [
                            {
                                "type": "url_contains",
                                "value": "/details/1",
                                "timeout_ms": 100,
                            }
                        ],
                    }
                ],
            }
        )

        with (
            patch("app.runners.playwright_runner.click_with_precheck") as click,
            self.assertRaises(RunnerExecutionError) as raised,
        ):
            _execute_step_with_candidates(page, case.steps[0], 0)

        click.assert_not_called()
        evidence = raised.exception.step_evidence
        self.assertEqual(evidence.action_outcome.status, "not_executed")
        self.assertEqual(evidence.action_outcome.side_effect_state, "not_committed")
        self.assertEqual(evidence.condition_results[0].phase, "precondition")

    def test_network_postcondition_uses_response_from_current_step(self) -> None:
        locator = _Locator(tag="button")
        page = _Page(locator)
        case = DSLCase.model_validate(
            {
                "name": "network",
                "steps": [
                    {
                        "action": "click",
                        "target": "Add to cart",
                        "candidates": [
                            {
                                "strategy": "role",
                                "selector": "button",
                                "semantic_value": "Add to cart",
                                "pre_score": 1,
                            }
                        ],
                        "postconditions": [
                            {
                                "type": "network_request",
                                "value": "/api/cart",
                                "method": "POST",
                                "status": 201,
                                "timeout_ms": 100,
                            }
                        ],
                    }
                ],
            }
        )

        request = type(
            "Request",
            (),
            {
                "url": "https://example.test/api/cart",
                "method": "POST",
                "resource_type": "fetch",
            },
        )()
        response = type(
            "Response",
            (),
            {
                "url": request.url,
                "status": 201,
                "request": request,
            },
        )()

        def click(_page, _locator):
            page.emit("response", response)
            return ClickPrecheckResult(succeeded=True)

        with patch(
            "app.runners.playwright_runner.click_with_precheck",
            side_effect=click,
        ):
            evidence = _execute_step_with_candidates(page, case.steps[0], 0)

        self.assertEqual(evidence.condition_results[0].status, "passed")
        self.assertEqual(evidence.network_events[0].event_type, "response")
        self.assertEqual(page.listeners["response"], [])


if __name__ == "__main__":
    unittest.main()
