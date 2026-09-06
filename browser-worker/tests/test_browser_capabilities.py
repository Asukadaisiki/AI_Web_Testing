from __future__ import annotations

import unittest

from app.application.browser.service import execute_browser_capability


class BrowserCapabilityContractTest(unittest.TestCase):
    def test_validate_page_elements_returns_grounded_candidates(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "Login",
                    "steps": [
                        {"action": "click", "target": "Login"},
                    ],
                },
                "a11y_nodes_by_state": {
                    "login": [
                        {
                            "node_id": "button-1",
                            "role": "button",
                            "name": "Login",
                            "verified_selectors": [
                                {"strategy": "css", "selector": "#login"}
                            ],
                        }
                    ]
                },
            },
        )

        self.assertTrue(result["valid"])
        self.assertEqual(result["validation_mode"], "dsl_case")
        self.assertEqual(len(result["case_digest"]), 64)
        self.assertEqual(len(result["evidence_digest"]), 64)
        self.assertEqual(result["locator_confidence"], "high")
        self.assertEqual(result["dsl_case"]["steps"][0]["match_count"], 1)
        self.assertEqual(result["dsl_case"]["steps"][0]["page_state"], "login")
        self.assertEqual(
            result["dsl_case"]["steps"][0]["candidates"][0]["strategy"],
            "verified_css",
        )

    def test_validate_page_elements_rejects_missing_target(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "Login",
                    "steps": [
                        {"action": "click", "target": "Missing"},
                    ],
                },
                "a11y_nodes_by_state": {
                    "login": [
                        {"node_id": "button-1", "role": "button", "name": "Login"},
                    ]
                },
            },
        )

        self.assertFalse(result["valid"])
        self.assertEqual(result["locator_confidence"], "low")
        self.assertEqual(len(result["warnings"]), 1)

    def test_validate_required_elements_recommends_reexplore_for_gaps(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "required_elements": [
                    {
                        "id": "submit",
                        "description": "登录按钮",
                        "keywords": ["Login", "Sign in"],
                        "roles": ["button"],
                    },
                    {
                        "id": "email",
                        "description": "邮箱输入框",
                        "keywords": ["Email"],
                        "roles": ["textbox"],
                    },
                ],
                "a11y_nodes": [
                    {"node_id": "button-1", "role": "button", "name": "Sign in"},
                ],
            },
        )

        self.assertFalse(result["valid"])
        self.assertEqual(result["missing_requirement_ids"], ["email"])
        self.assertEqual(result["recommended_action"], "re_explore")

    def test_validate_page_elements_matches_verified_css_selectors(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "Cart",
                    "steps": [
                        {"action": "goto", "value": "https://example.com"},
                        {"action": "click", "target": "button.cart"},
                        {
                            "action": "assert_url_contains",
                            "value": "/view_cart",
                        },
                    ],
                },
                "a11y_nodes_by_state": {
                    "details": [
                        {
                            "node_id": "button-1",
                            "role": "button",
                            "name": "Add to cart",
                            "verified_selectors": [
                                {"strategy": "css", "selector": "button.cart"}
                            ],
                        }
                    ]
                },
            },
        )

        self.assertTrue(result["valid"])
        self.assertEqual(result["warnings"], [])
        click_step = result["dsl_case"]["steps"][1]
        self.assertEqual(click_step["match_count"], 1)
        self.assertEqual(click_step["page_state"], "details")
        self.assertEqual(click_step["candidates"][0]["strategy"], "verified_css")

    def test_bug_136_search_selectors_require_exact_verified_evidence(self) -> None:
        nodes = {
            "products": [
                {
                    "node_id": "search-input",
                    "role": "textbox",
                    "name": "Search Product",
                    "verified_selectors": [
                        {
                            "strategy": "css",
                            "selector": "#search_product",
                            "source": "dom_verified_interactive_control",
                        }
                    ],
                },
                {
                    "node_id": "search-button",
                    "role": "button",
                    "name": "Search",
                    "verified_selectors": [
                        {
                            "strategy": "css",
                            "selector": "#submit_search",
                            "source": "dom_verified_interactive_control",
                        }
                    ],
                },
            ]
        }
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "Search",
                    "steps": [
                        {"action": "input", "target": "#search_product", "value": "Blue Top"},
                        {"action": "click", "target": "#submit_search"},
                    ],
                },
                "a11y_nodes_by_state": nodes,
            },
        )
        self.assertTrue(result["valid"])
        self.assertEqual(
            [step["match_count"] for step in result["dsl_case"]["steps"]],
            [1, 1],
        )

        forged = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "Forged search",
                    "steps": [
                        {"action": "input", "target": "#search_product_forged", "value": "Blue Top"},
                        {"action": "click", "target": "#submit_search_forged"},
                    ],
                },
                "a11y_nodes_by_state": nodes,
            },
        )
        self.assertFalse(forged["valid"])
        self.assertEqual(
            [step["match_count"] for step in forged["dsl_case"]["steps"]],
            [0, 0],
        )

    def test_validate_page_elements_rejects_unverified_composite_css(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "Cart",
                    "steps": [
                        {
                            "action": "assert_text",
                            "target": "#product-1 td.cart_price",
                            "target_strategy": "css",
                            "value": "Rs. 500",
                        }
                    ],
                },
                "a11y_nodes_by_state": {
                    "cart": [
                        {
                            "node_id": "price",
                            "role": "cell",
                            "name": "Rs. 500",
                            "verified_selectors": [],
                        }
                    ]
                },
            },
        )

        self.assertFalse(result["valid"])
        self.assertIn("composite CSS", result["warnings"][0])
        self.assertIn("verified_selectors", result["warnings"][0])

    def test_bug_131_cart_selector_passes_when_verified_in_cart_state(self) -> None:
        selector = "#product-1 td.cart_price"
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "BUG-131 replay",
                    "steps": [
                        {
                            "action": "assert_text",
                            "target": selector,
                            "target_strategy": "css",
                            "page_state": "cart",
                            "value": "Rs. 500",
                        }
                    ],
                },
                "a11y_nodes_by_state": {
                    "details": [
                        {"node_id": "price", "role": "text", "name": "Rs. 500"}
                    ],
                    "cart": [
                        {
                            "node_id": "cart-price",
                            "role": "cell",
                            "name": "Rs. 500",
                            "verified_selectors": [
                                {"strategy": "css", "selector": selector}
                            ],
                        }
                    ],
                },
            },
        )

        self.assertTrue(result["valid"])
        step = result["dsl_case"]["steps"][0]
        self.assertEqual(step["page_state"], "cart")
        self.assertEqual(step["match_count"], 1)

    def test_cross_page_anchor_requires_target_url_postcondition(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "name": "Details",
                    "steps": [
                        {
                            "action": "click",
                            "target": "View details",
                            "page_state": "products",
                        }
                    ],
                },
                "a11y_nodes_by_state": {
                    "products": [
                        {
                            "node_id": "details",
                            "role": "link",
                            "name": "View details",
                            "dom": {
                                "tag": "a",
                                "attrs": {"href": "/details/1"},
                            },
                            "verified_selectors": [
                                {
                                    "strategy": "css",
                                    "selector": "a[href='/details/1']",
                                }
                            ],
                        }
                    ]
                },
            },
        )

        self.assertFalse(result["valid"])
        self.assertIn("url_contains postcondition", result["warnings"][0])


if __name__ == "__main__":
    unittest.main()
