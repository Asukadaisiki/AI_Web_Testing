from __future__ import annotations

import unittest
from unittest.mock import patch

from app.runners.click_preprocessor import ClickPrecheckResult
from app.runners.playwright_runner import (
    RunnerExecutionError,
    _execute_step_with_candidates,
)
from app.schemas.dsl import DSLCase


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


class PlaywrightRunnerNavigationFallbackTest(unittest.TestCase):
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
            self.assertRaises(RunnerExecutionError),
        ):
            _execute_step_with_candidates(page, case.steps[0], 0)

        self.assertEqual(click_calls, 1)
        self.assertEqual(page.goto_calls, [])


if __name__ == "__main__":
    unittest.main()
