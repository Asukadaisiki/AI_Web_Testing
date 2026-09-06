from __future__ import annotations

import unittest
from unittest.mock import MagicMock, patch

from app.ai.page_explorer import (
    BrowserSessionManager,
    _collect_flow_a11y,
    _collect_dom_interactive_supplement,
    _deduplicate_explore_results,
    _filter_a11y_nodes,
    _is_business_candidate,
    _same_document_url,
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
        self.calls.append(("goto", url))
        self.url = url

    def wait_for_load_state(self, *_args: object, **_kwargs: object) -> None:
        return None

    def wait_for_timeout(self, timeout: int) -> None:
        self.calls.append(("wait_for_timeout", timeout))

    def get_by_text(self, text: str, *, exact: bool = False) -> _FlowLocator:
        self.calls.append(("get_by_text", text, exact))
        return _FlowLocator(self.calls)

    def locator(self, selector: str) -> _FlowLocator:
        self.calls.append(("locator", selector))
        return _FlowLocator(self.calls)


class _DOMPage:
    def __init__(self, counts: dict[str, int]) -> None:
        self.counts = counts

    def locator(self, selector: str):
        page = self

        class Locator:
            def count(self) -> int:
                return page.counts.get(selector, 0)

        return Locator()

    def get_by_test_id(self, value: str):
        return self.locator(f"testid={value}")


class _DOMClient:
    def __init__(self, payloads: list[dict[str, object]]) -> None:
        self.payloads = payloads

    def send(self, method: str, params: dict[str, object]) -> dict[str, object]:
        if method == "DOM.performSearch":
            return {"searchId": "search-1", "resultCount": len(self.payloads)}
        if method == "DOM.getDocument":
            return {"root": {"nodeId": 1}}
        if method == "DOM.getSearchResults":
            return {"nodeIds": list(range(1, len(self.payloads) + 1))}
        if method == "DOM.describeNode":
            node_id = int(params["nodeId"])
            return {"node": {"backendNodeId": 100 + node_id}}
        if method == "DOM.resolveNode":
            return {"object": {"objectId": f"object-{params['nodeId']}"}}
        if method == "Runtime.callFunctionOn":
            object_id = str(params["objectId"])
            return {"result": {"value": self.payloads[int(object_id.split("-")[1]) - 1]}}
        if method in {
            "Runtime.releaseObject",
            "DOM.discardSearchResults",
            "DOM.enable",
            "DOM.disable",
        }:
            return {}
        raise AssertionError(f"unexpected CDP method: {method}")


class PageExplorerA11yFilterTest(unittest.TestCase):
    def test_same_document_url_ignores_only_fragment(self) -> None:
        self.assertTrue(
            _same_document_url(
                "https://example.test/product?id=1#modal",
                "https://example.test/product?id=1#details",
            )
        )
        self.assertFalse(
            _same_document_url(
                "https://example.test/product?id=1",
                "https://example.test/product?id=2",
            )
        )
        self.assertFalse(
            _same_document_url(
                "https://example.test/product",
                "https://example.test/product/",
            )
        )
        self.assertFalse(
            _same_document_url(
                "https://example.test/product",
                "https://example.test/other",
            )
        )

    def test_deduplication_preserves_query_and_path_identity(self) -> None:
        result = _deduplicate_explore_results(
            [
                {
                    "url": "https://example.test/Product?sku=blue#one",
                    "status": "success",
                    "revision": 1,
                },
                {
                    "url": "https://example.test/Product?sku=blue#two",
                    "status": "success",
                    "revision": 2,
                },
                {
                    "url": "https://example.test/Product?sku=red",
                    "status": "success",
                    "revision": 3,
                },
                {
                    "url": "https://example.test/product?sku=blue",
                    "status": "success",
                    "revision": 4,
                },
                {
                    "url": "https://example.test/Product/?sku=blue",
                    "status": "success",
                    "revision": 5,
                },
            ]
        )

        self.assertEqual(len(result), 4)
        self.assertEqual(
            next(
                entry
                for entry in result
                if entry["url"] == "https://example.test/Product?sku=blue#two"
            )["revision"],
            2,
        )

    def test_flow_skips_same_document_navigation_but_keeps_query_and_path(self) -> None:
        page = _FlowPage()
        with (
            patch.object(
                BrowserSessionManager,
                "get_or_create_context",
                return_value=(object(), page),
            ),
            patch(
                "app.ai.page_explorer.collect_a11y_nodes",
                return_value=[],
            ),
        ):
            _collect_flow_a11y(
                [
                    {"url": "https://example.test/current#first"},
                    {"url": "https://example.test/current#second"},
                    {"url": "https://example.test/current?variant=2"},
                    {"url": "https://example.test/other"},
                ],
                session_id=7,
            )

        self.assertEqual(
            [
                ("goto", "https://example.test/current?variant=2"),
                ("goto", "https://example.test/other"),
            ],
            [call for call in page.calls if call[0] == "goto"],
        )

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

    def test_managed_flow_exits_playwright_once(self) -> None:
        page = _FlowPage()
        pw = MagicMock()
        browser = pw.__enter__.return_value.chromium.launch.return_value
        context = browser.new_context.return_value
        context.new_page.return_value = page

        with (
            patch("app.ai.page_explorer.sync_playwright", return_value=pw),
            patch("app.ai.page_explorer.collect_a11y_nodes", return_value=[]),
        ):
            _collect_flow_a11y([{"url": page.url}])

        context.close.assert_called_once_with()
        browser.close.assert_called_once_with()
        pw.__exit__.assert_called_once_with(None, None, None)

    def test_managed_flow_exits_playwright_once_when_collection_and_cleanup_fail(
        self,
    ) -> None:
        page = _FlowPage()
        pw = MagicMock()
        browser = pw.__enter__.return_value.chromium.launch.return_value
        context = browser.new_context.return_value
        context.new_page.return_value = page
        context.close.side_effect = RuntimeError("context close failed")
        browser.close.side_effect = RuntimeError("browser close failed")
        pw.__exit__.side_effect = RuntimeError("playwright exit failed")

        with (
            patch("app.ai.page_explorer.sync_playwright", return_value=pw),
            patch(
                "app.ai.page_explorer.collect_a11y_nodes",
                side_effect=RuntimeError("collection failed"),
            ),
        ):
            result = _collect_flow_a11y([{"url": page.url}])

        self.assertEqual(result[0]["status"], "error")
        context.close.assert_called_once_with()
        browser.close.assert_called_once_with()
        pw.__exit__.assert_called_once_with(None, None, None)

    def test_shared_session_does_not_exit_playwright(self) -> None:
        page = _FlowPage()
        context = MagicMock()
        pw = MagicMock()

        with (
            patch.object(
                BrowserSessionManager,
                "get_or_create_context",
                return_value=(context, page),
            ),
            patch("app.ai.page_explorer.sync_playwright", return_value=pw),
            patch("app.ai.page_explorer.collect_a11y_nodes", return_value=[]),
        ):
            _collect_flow_a11y([{"url": page.url}], session_id=7)

        context.close.assert_not_called()
        pw.__exit__.assert_not_called()

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
            patch(
                "app.ai.page_explorer.collect_a11y_nodes",
                return_value=[{"node_id": "current", "role": "heading", "name": "Cart"}],
            ),
        ):
            result = _collect_flow_a11y(
                [{"actions": [{"action": "click", "target": "Missing"}]}],
                session_id=7,
            )

        self.assertEqual(result[-1]["status"], "error")
        self.assertEqual(result[-1]["failure"]["code"], "flow_action_failed")
        self.assertEqual(result[-1]["failure"]["action"], "click")

    def test_wait_for_text_prefix_uses_requested_timeout(self) -> None:
        page = _FlowPage()

        _wait_for_flow_target(page, "text=View Cart", 8123)

        self.assertIn(("get_by_text", "View Cart", True), page.calls)
        self.assertIn(("wait_for", {"state": "visible", "timeout": 8123}), page.calls)

    def test_wait_for_css_forms_use_locator(self) -> None:
        for target, expected in (
            ("#search_product", "#search_product"),
            (".search-form", ".search-form"),
            ("css=button.search", "button.search"),
        ):
            with self.subTest(target=target):
                page = _FlowPage()
                _wait_for_flow_target(page, target, 900)
                self.assertIn(("locator", expected), page.calls)
                self.assertIn(
                    ("wait_for", {"state": "visible", "timeout": 900}),
                    page.calls,
                )

    def test_dom_supplement_adds_only_unique_verified_controls(self) -> None:
        base = {
            "connected": True,
            "visible": True,
            "enabled": True,
            "advertising_context": False,
            "third_party_frame": False,
            "textContent": "",
        }
        payloads = [
            {**base, "tag": "input", "attrs": {
                "id": "search_product", "placeholder": "Search Product", "value": "secret"
            }},
            {**base, "tag": "button", "attrs": {
                "id": "submit_search", "type": "button"
            }, "textContent": "Search"},
            {**base, "tag": "button", "attrs": {"id": "hidden"}, "visible": False},
            {**base, "tag": "button", "attrs": {"id": "disabled"}, "enabled": False},
            {**base, "tag": "button", "attrs": {"name": "duplicate"}},
            {**base, "tag": "button", "attrs": {}, "textContent": "No stable attribute"},
            {**base, "tag": "button", "attrs": {"id": "advert"}, "advertising_context": True},
            {**base, "tag": "input", "attrs": {"id": "password", "type": "password"}},
        ]
        page = _DOMPage(
            {
                "#search_product": 1,
                'input[placeholder="Search Product"]': 1,
                "#submit_search": 1,
                "button[type=\"button\"]": 4,
                'button[name="duplicate"]': 2,
            }
        )

        nodes = _collect_dom_interactive_supplement(
            page,
            _DOMClient(payloads),
            page_state="products",
            existing_nodes=[],
        )

        self.assertEqual(
            {"#search_product", "#submit_search"},
            {
                selector["selector"]
                for node in nodes
                for selector in node["verified_selectors"]
                if selector["selector"].startswith("#")
            },
        )
        self.assertTrue(
            all(node["source"] == "dom_verified_interactive_control" for node in nodes)
        )
        self.assertTrue(
            all(
                selector["source"] == "dom_verified_interactive_control"
                for node in nodes
                for selector in node["verified_selectors"]
            )
        )
        self.assertTrue(all("value" not in node["dom"]["attrs"] for node in nodes))
        self.assertNotIn("password", {node["dom"]["attrs"].get("id") for node in nodes})

    def test_dom_supplement_deduplicates_against_ax_backend_and_selector(self) -> None:
        payloads = [
            {
                "tag": "button",
                "attrs": {"id": "same-backend"},
                "connected": True,
                "visible": True,
                "enabled": True,
                "advertising_context": False,
                "third_party_frame": False,
                "textContent": "Same",
            },
            {
                "tag": "button",
                "attrs": {"id": "same-selector"},
                "connected": True,
                "visible": True,
                "enabled": True,
                "advertising_context": False,
                "third_party_frame": False,
                "textContent": "Same selector",
            },
        ]
        existing = [
            {
                "backend_dom_node_id": 101,
                "verified_selectors": [
                    {"strategy": "css", "selector": "#same-selector"}
                ],
            }
        ]
        nodes = _collect_dom_interactive_supplement(
            _DOMPage({"#same-selector": 1}),
            _DOMClient(payloads),
            page_state="S0",
            existing_nodes=existing,
        )
        self.assertEqual(nodes, [])

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
            patch(
                "app.ai.page_explorer.collect_a11y_nodes",
                return_value=[{"node_id": "current", "role": "link", "name": "Details"}],
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

        self.assertEqual(result[-1]["status"], "error")
        self.assertIn(
            "did not reach expected anchor destination",
            result[-1]["failure"]["message"],
        )

    def test_action_snapshots_keep_cross_page_state_ownership(self) -> None:
        page = _FlowPage()

        def click(_page, _locator):
            page.url = "https://example.test/expected"
            return ClickPrecheckResult(succeeded=True)

        def collect(_page, *, page_state: str, **_kwargs: object):
            return [
                {
                    "node_id": page.url,
                    "role": "button",
                    "name": page.url,
                    "page_state": page_state,
                    "verified_selectors": [
                        {"strategy": "css", "selector": f"#{page_state}"}
                    ],
                }
            ]

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
            patch("app.ai.page_explorer.collect_a11y_nodes", side_effect=collect),
        ):
            result = _collect_flow_a11y(
                [{"actions": [{"action": "click", "target": "Details"}]}],
                session_id=7,
            )

        by_url = {entry["url"]: entry for entry in result}
        before = by_url["https://example.test/current"]
        after = by_url["https://example.test/expected"]
        self.assertNotEqual(before["page_state"], after["page_state"])
        self.assertEqual(before["actions"][0]["phase"], "before")
        self.assertEqual(after["actions"][0]["phase"], "after")
        self.assertEqual(before["actions"][0]["action"], "click")
        self.assertEqual(before["actions"][0]["target"], "Details")
        self.assertEqual(after["actions"][0]["action"], "click")
        self.assertEqual(after["actions"][0]["target"], "Details")
        self.assertTrue(
            all(node["page_state"] == before["page_state"] for node in before["a11y_nodes"])
        )
        self.assertTrue(
            all(node["page_state"] == after["page_state"] for node in after["a11y_nodes"])
        )
        self.assertEqual(before["actions"][0]["page_state"], before["page_state"])
        self.assertEqual(after["actions"][0]["page_state"], after["page_state"])

    def test_same_url_action_keeps_pre_and_post_evidence_in_latest_revision(self) -> None:
        page = _FlowPage()
        snapshots = iter(("before-node", "after-node"))

        def collect(_page, *, page_state: str, **_kwargs: object):
            return [
                {
                    "node_id": next(snapshots),
                    "role": "button",
                    "name": "Ready",
                    "page_state": page_state,
                    "verified_selectors": [{"strategy": "css", "selector": "#ready"}],
                }
            ]

        with (
            patch.object(
                BrowserSessionManager,
                "get_or_create_context",
                return_value=(object(), page),
            ),
            patch("app.ai.page_explorer.collect_a11y_nodes", side_effect=collect),
        ):
            result = _collect_flow_a11y(
                [{"actions": [{"action": "wait_for", "target": "Ready"}]}],
                session_id=7,
            )

        self.assertEqual(len(result), 1)
        before_action = next(
            action
            for action in result[0]["actions"]
            if action.get("phase") == "before"
        )
        after_action = next(
            action
            for action in result[0]["actions"]
            if action.get("phase") == "after"
        )
        self.assertEqual(
            before_action["target_evidence"][0]["node_id"],
            "before-node",
        )
        self.assertEqual(
            after_action["target_evidence"][0]["node_id"],
            "after-node",
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
