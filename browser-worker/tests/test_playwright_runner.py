from __future__ import annotations

import unittest
from pathlib import Path
from unittest.mock import patch

from playwright.sync_api import TimeoutError as PlaywrightTimeoutError

from browser_worker.contracts.action_ir import validate_research_dsl
from browser_worker.contracts.action_ir_v2 import validate_research_v2_dsl
from browser_worker.contracts.dsl import DSLCase
from browser_worker.contracts.executions import StepExecutionEvidence
from browser_worker.runners.click_preprocessor import ClickPrecheckResult
from browser_worker.runners.playwright_runner import (
    RunnerExecutionError,
    StepStreamEvent,
    _execute_non_target_step,
    _execute_step_with_candidates,
    execute_case_with_playwright,
)


class _Locator:
    def __init__(
        self,
        *,
        tag: str,
        href: str = "",
        click_errors: list[Exception] | None = None,
    ) -> None:
        self.tag = tag
        self.href = href
        self.click_errors = list(click_errors or [])
        self.click_calls = 0

    def count(self) -> int:
        return 1

    def is_visible(self) -> bool:
        return True

    def is_enabled(self) -> bool:
        return True

    def click(self, **_kwargs) -> None:
        self.click_calls += 1
        if self.click_errors:
            raise self.click_errors.pop(0)

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

    def wait_for_timeout(self, _timeout_ms: int) -> None:
        pass

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
    def test_research_v2_evidence_preserves_full_locator_lineage(self) -> None:
        page = _Page(_Locator(tag="button"))
        case = validate_research_v2_dsl(
            {
                "profile": "research-v2",
                "name": "Submit form",
                "input_contract": [],
                "output_contract": [],
                "plan_binding": {
                    "plan_id": "plan-1",
                    "version": 1,
                    "sha256": "a" * 64,
                },
                "observation_bindings": [
                    {
                        "binding_id": "binding-1",
                        "binding_sha256": "b" * 64,
                        "probe_id": "probe-1",
                        "observation_id": "obs-1",
                        "observation_sha256": "c" * 64,
                        "page_state_id": "products",
                    }
                ],
                "steps": [
                    {
                        "plan_step_id": "submit",
                        "action": "click",
                        "intent": "Submit form",
                        "target_binding_id": "binding-1",
                        "probe_id": "probe-1",
                        "observation_id": "obs-1",
                        "observation_sha256": "c" * 64,
                        "page_state_id": "products",
                        "selected_candidate_id": "candidate-planned",
                        "semantic_target": "Submit",
                        "locator_candidates": [
                            {
                                "candidate_id": "candidate-planned",
                                "element_ref": "products:7",
                                "context_path": {
                                    "frames": [],
                                    "shadow_hosts": [],
                                },
                                "locator": {
                                    "kind": "role",
                                    "role": "button",
                                    "name": "Submit",
                                    "exact": True,
                                },
                                "provenance": "a11y_exact",
                                "observed_count": 1,
                                "visible": True,
                                "enabled": True,
                                "score": 0.95,
                            }
                        ],
                        "preconditions": [
                            {
                                "type": "url_contains",
                                "value": "/products",
                                "timeout_ms": 3000,
                            }
                        ],
                        "postconditions": [
                            {
                                "type": "url_contains",
                                "value": "/products",
                                "timeout_ms": 3000,
                            }
                        ],
                        "idempotency": "idempotent",
                        "side_effect": "browser_state",
                    }
                ],
            }
        )

        with patch(
            "browser_worker.runners.playwright_runner.click_with_precheck",
            return_value=ClickPrecheckResult(succeeded=True),
        ):
            evidence = _execute_step_with_candidates(page, case.steps[0], 0)

        self.assertEqual(evidence.plan_step_id, "submit")
        self.assertEqual(evidence.target_binding_id, "binding-1")
        self.assertEqual(evidence.probe_id, "probe-1")
        self.assertEqual(evidence.observation_id, "obs-1")
        self.assertEqual(evidence.page_state_id, "products")
        self.assertEqual(evidence.planned_candidate_id, "candidate-planned")
        self.assertEqual(evidence.candidate_id, "candidate-planned")
        self.assertEqual(evidence.element_ref, "products:7")
        self.assertEqual(
            evidence.locator_trace.selected_candidate.candidate_id,
            "candidate-planned",
        )

        page._locator.count = lambda: 0
        with self.assertRaises(RunnerExecutionError) as raised:
            _execute_step_with_candidates(page, case.steps[0], 0)
        failed = raised.exception.step_evidence
        self.assertIsNotNone(failed)
        assert failed is not None
        self.assertIsNotNone(failed.locator_trace, failed.model_dump(mode="json"))
        self.assertEqual(failed.planned_candidate_id, "candidate-planned")
        self.assertIsNone(failed.candidate_id)
        self.assertEqual(
            failed.locator_trace.candidates[0].candidate_id,
            "candidate-planned",
        )
        self.assertEqual(
            failed.locator_trace.candidates[0].runtime_count,
            0,
        )
        self.assertIn(
            "runtime_count_0",
            failed.locator_trace.candidates[0].rejected_reasons,
        )

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
            "browser_worker.runners.playwright_runner.execute_case_with_playwright_streaming",
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

        def click(_page, _locator, **_kwargs):
            nonlocal click_calls
            click_calls += 1
            page.url = "https://example.test/products#interstitial"
            return ClickPrecheckResult(succeeded=True)

        with patch(
            "browser_worker.runners.playwright_runner.click_with_precheck",
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

        def click(_page, _locator, **kwargs):
            nonlocal click_calls
            self.assertTrue(kwargs["allow_recovery"])
            click_calls += 1
            return ClickPrecheckResult(succeeded=True)

        with (
            patch(
                "browser_worker.runners.playwright_runner.click_with_precheck",
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

    def test_research_risky_click_errors_dispatch_locator_once(self) -> None:
        scenarios = [
            (
                "non-idempotent interception",
                "non_idempotent",
                "browser_state",
                PlaywrightTimeoutError(
                    "<div>Loading</div> intercepts pointer events"
                ),
            ),
            (
                "external-state timeout",
                "idempotent",
                "external_state",
                PlaywrightTimeoutError("element is not visible"),
            ),
            (
                "unknown-side-effect interception",
                "idempotent",
                "unknown",
                PlaywrightTimeoutError(
                    "<div>Loading</div> intercepts pointer events"
                ),
            ),
        ]

        for name, idempotency, side_effect, click_error in scenarios:
            with self.subTest(name=name):
                locator = _Locator(
                    tag="button",
                    click_errors=[click_error],
                )
                page = _Page(locator)
                case = validate_research_dsl(
                    {
                        "profile": "research-v1",
                        "name": "purchase",
                        "steps": [
                            {
                                "action": "click",
                                "intent": "Submit the order",
                                "target": "Pay",
                                "preconditions": [
                                    {
                                        "type": "url_contains",
                                        "value": "/products",
                                    }
                                ],
                                "postconditions": [
                                    {
                                        "type": "url_contains",
                                        "value": "/receipt",
                                    }
                                ],
                                "idempotency": idempotency,
                                "side_effect": side_effect,
                                "locator_confidence": "high",
                                "candidates": [
                                    {
                                        "strategy": "verified_css",
                                        "selector": "#pay",
                                        "semantic_value": "Pay",
                                        "pre_score": 1,
                                        "pre_features": {
                                            "verified": True,
                                            "source": "a11y_backend_dom_node",
                                        },
                                    }
                                ],
                            }
                        ],
                    }
                )

                with self.assertRaises(RunnerExecutionError) as raised:
                    _execute_step_with_candidates(page, case.steps[0], 0)

                self.assertEqual(locator.click_calls, 1)
                evidence = raised.exception.step_evidence
                self.assertEqual(evidence.dsl_profile, "research-v1")
                self.assertEqual(evidence.intent, "Submit the order")
                self.assertEqual(evidence.idempotency, idempotency)
                self.assertEqual(evidence.declared_side_effect, side_effect)
                self.assertEqual(evidence.action_outcome.status, "unknown")
                self.assertEqual(
                    evidence.action_outcome.side_effect_state,
                    "unknown",
                )

    def test_research_idempotent_click_keeps_recovery(self) -> None:
        locator = _Locator(
            tag="button",
            click_errors=[
                PlaywrightTimeoutError(
                    "<div>Loading</div> intercepts pointer events"
                )
            ],
        )
        page = _Page(locator)
        case = validate_research_dsl(
            {
                "profile": "research-v1",
                "name": "open details",
                "steps": [
                    {
                        "action": "click",
                        "intent": "Open product details",
                        "target": "Details",
                        "preconditions": [
                            {"type": "url_contains", "value": "/products"}
                        ],
                        "postconditions": [
                            {"type": "url_contains", "value": "/products"}
                        ],
                        "idempotency": "idempotent",
                        "side_effect": "browser_state",
                        "locator_confidence": "high",
                        "candidates": [
                            {
                                "strategy": "verified_css",
                                "selector": "#details",
                                "semantic_value": "Details",
                                "pre_score": 1,
                                "pre_features": {
                                    "verified": True,
                                    "source": "a11y_backend_dom_node",
                                },
                            }
                        ],
                    }
                ],
            }
        )

        evidence = _execute_step_with_candidates(page, case.steps[0], 0)

        self.assertEqual(locator.click_calls, 2)
        self.assertEqual(evidence.click_recovery, "wait")
        self.assertEqual(evidence.action_outcome.status, "succeeded")

    def test_research_precondition_failure_has_zero_dispatches(self) -> None:
        locator = _Locator(tag="button")
        page = _Page(locator)
        case = validate_research_dsl(
            {
                "profile": "research-v1",
                "name": "purchase",
                "steps": [
                    {
                        "action": "click",
                        "intent": "Submit the order",
                        "target": "Pay",
                        "preconditions": [
                            {"type": "url_contains", "value": "/checkout"}
                        ],
                        "postconditions": [
                            {"type": "url_contains", "value": "/receipt"}
                        ],
                        "idempotency": "non_idempotent",
                        "side_effect": "external_state",
                        "locator_confidence": "high",
                        "candidates": [
                            {
                                "strategy": "verified_css",
                                "selector": "#pay",
                                "semantic_value": "Pay",
                                "pre_score": 1,
                                "pre_features": {
                                    "verified": True,
                                    "source": "a11y_backend_dom_node",
                                },
                            }
                        ],
                    }
                ],
            }
        )

        with (
            patch("browser_worker.runners.playwright_runner.click_with_precheck") as click,
            self.assertRaises(RunnerExecutionError) as raised,
        ):
            _execute_step_with_candidates(page, case.steps[0], 0)

        click.assert_not_called()
        self.assertEqual(
            raised.exception.step_evidence.action_outcome.side_effect_state,
            "not_committed",
        )

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
                "browser_worker.runners.playwright_runner.click_with_precheck",
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
            patch("browser_worker.runners.playwright_runner.click_with_precheck") as click,
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

        def click(_page, _locator, **_kwargs):
            page.emit("response", response)
            return ClickPrecheckResult(succeeded=True)

        with patch(
            "browser_worker.runners.playwright_runner.click_with_precheck",
            side_effect=click,
        ):
            evidence = _execute_step_with_candidates(page, case.steps[0], 0)

        self.assertEqual(evidence.condition_results[0].status, "passed")
        self.assertEqual(evidence.network_events[0].event_type, "response")
        self.assertEqual(page.listeners["response"], [])


if __name__ == "__main__":
    unittest.main()
