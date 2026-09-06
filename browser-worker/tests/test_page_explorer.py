from __future__ import annotations

import unittest
from unittest.mock import patch

from app.ai.page_explorer import (
    BrowserSessionManager,
    _collect_flow_a11y,
    _deduplicate_explore_results,
    _filter_a11y_nodes,
    _is_business_candidate,
    _wait_for_flow_target,
)
from app.runners.click_preprocessor import ClickPrecheckResult


class _PartiallyInitializedPlaywright:
    def __init__(self) -> None:
        self.exit_called = False

    def __enter__(self):
        raise RuntimeError("startup failed before connection initialization")

    def __exit__(self, *_args: object) -> None:
        del _args
        self.exit_called = True
        raise AttributeError("_connection")


class _FlowLocator:
    def __init__(self, calls: list[tuple[object, ...]]) -> None:
        self.first = self
        self._calls = calls

    def count(self) -> int:
        return 1

    def wait_for(self, **kwargs: object) -> None:
        self._calls.append(("wait_for", kwargs))

    def evaluate(self, _script: str):
        return {"tag": "a", "href": "/expected", "download": False}


class _FlowPage:
    def __init__(self) -> None:
        self.url = "https://example.test/current"
        self.calls: list[tuple[object, ...]] = []

    def goto(self, url: str, **_kwargs: object) -> None:
        self.url = url

    def wait_for_load_state(self, *_args: object, **_kwargs: object) -> None:
        return None

    def wait_for_timeout(self, timeout: int) -> None:
        self.calls.append(("wait_for_timeout", timeout))

    def get_by_text(self, text: str, *, exact: bool = False) -> _FlowLocator:
        self.calls.append(("get_by_text", text, exact))
        return _FlowLocator(self.calls)


class PageExplorerA11yFilterTest(unittest.TestCase):
    def test_filter_excludes_non_targetable_document_nodes(self) -> None:
        nodes = [
            {
                "nodeId": "root",
                "role": {"value": "RootWebArea"},
                "name": {"value": "Example Domain"},
            },
            {
                "nodeId": "heading",
                "role": {"value": "heading"},
                "name": {"value": "Example Domain"},
            },
            {
                "nodeId": "text",
                "role": {"value": "StaticText"},
                "name": {"value": "Example Domain"},
            },
        ]

        filtered = _filter_a11y_nodes(nodes)

        self.assertEqual(["heading"], [node["nodeId"] for node in filtered])

    def test_partial_playwright_startup_does_not_call_exit(self) -> None:
        manager = _PartiallyInitializedPlaywright()
        BrowserSessionManager._sessions.clear()
        BrowserSessionManager._runtime_pw = None
        BrowserSessionManager._runtime_playwright = None
        BrowserSessionManager._runtime_browser = None

        with patch(
            "app.ai.page_explorer.sync_playwright",
            return_value=manager,
        ):
            with self.assertRaisesRegex(RuntimeError, "startup failed"):
                BrowserSessionManager.get_or_create_context(301)

        self.assertFalse(manager.exit_called)

    def test_empty_actions_collect_current_page(self) -> None:
        page = _FlowPage()
        with (
            patch.object(
                BrowserSessionManager,
                "get_or_create_context",
                return_value=(object(), page),
            ),
            patch(
                "app.ai.page_explorer.collect_a11y_nodes",
                return_value=[{"node_id": "current", "role": "heading", "name": "Cart"}],
            ),
        ):
            result = _collect_flow_a11y(
                [{"url": "https://example.test/cart", "actions": []}],
                session_id=7,
            )

        self.assertEqual(len(result), 1)
        self.assertEqual(result[0]["status"], "success")
        self.assertEqual(result[0]["element_count"], 1)
        self.assertEqual(result[0]["revision"], 1)

    def test_missing_click_returns_structured_failure(self) -> None:
        page = _FlowPage()
        with (
            patch.object(
                BrowserSessionManager,
                "get_or_create_context",
                return_value=(object(), page),
            ),
            patch(
                "app.ai.page_explorer._resolve_flow_action_locator",
                return_value=None,
            ),
        ):
            result = _collect_flow_a11y(
                [{"actions": [{"action": "click", "target": "Missing"}]}],
                session_id=7,
            )

        self.assertEqual(result[0]["status"], "error")
        self.assertEqual(result[0]["failure"]["code"], "flow_action_failed")
        self.assertEqual(result[0]["failure"]["action"], "click")

    def test_wait_for_text_prefix_uses_requested_timeout(self) -> None:
        page = _FlowPage()

        _wait_for_flow_target(page, "text=View Cart", 8123)

        self.assertIn(("get_by_text", "View Cart", True), page.calls)
        self.assertIn(("wait_for", {"state": "visible", "timeout": 8123}), page.calls)

    def test_deduplicate_keeps_latest_successful_revision_and_failure(self) -> None:
        result = _deduplicate_explore_results(
            [
                {
                    "url": "https://example.test/cart",
                    "status": "success",
                    "revision": 1,
                    "actions": [{"action_description": "old", "a11y_nodes": []}],
                },
                {
                    "url": "https://example.test/cart",
                    "status": "success",
                    "revision": 2,
                    "actions": [],
                    "a11y_nodes": [{"name": "latest"}],
                },
                {
                    "url": "https://example.test/cart",
                    "status": "error",
                    "revision": 3,
                    "failure": {"code": "flow_action_failed"},
                },
            ]
        )

        self.assertEqual(len(result), 2)
        self.assertEqual(result[0]["revision"], 2)
        self.assertEqual(result[1]["failure"]["code"], "flow_action_failed")

    def test_hash_only_interstitial_does_not_satisfy_cross_page_anchor(self) -> None:
        page = _FlowPage()

        def click(_page, _locator):
            page.url = "https://example.test/current#interstitial"
            return ClickPrecheckResult(succeeded=True)

        with (
            patch.object(
                BrowserSessionManager,
                "get_or_create_context",
                return_value=(object(), page),
            ),
            patch(
                "app.ai.page_explorer._resolve_flow_action_locator",
                return_value=_FlowLocator(page.calls),
            ),
            patch(
                "app.runners.click_preprocessor.click_with_precheck",
                side_effect=click,
            ),
        ):
            result = _collect_flow_a11y(
                [
                    {
                        "actions": [
                            {
                                "action": "click",
                                "target": "Details",
                                "timeout_ms": 100,
                            }
                        ]
                    }
                ],
                session_id=7,
            )

        self.assertEqual(result[0]["status"], "error")
        self.assertIn(
            "did not reach expected anchor destination",
            result[0]["failure"]["message"],
        )

    def test_advertising_context_is_not_a_business_candidate(self) -> None:
        self.assertFalse(
            _is_business_candidate(
                {
                    "role": "link",
                    "name": "Continue",
                    "dom": {"advertising_context": True},
                }
            )
        )
        self.assertFalse(
            _is_business_candidate(
                {
                    "role": "button",
                    "name": "Open",
                    "dom": {"third_party_frame": True},
                }
            )
        )
        self.assertTrue(
            _is_business_candidate(
                {
                    "role": "link",
                    "name": "Product details",
                    "dom": {"advertising_context": False},
                }
            )
        )


if __name__ == "__main__":
    unittest.main()
