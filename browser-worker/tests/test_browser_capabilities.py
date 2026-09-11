from __future__ import annotations

import unittest
from unittest.mock import MagicMock, patch

from browser_worker.capabilities.browser_capabilities import (
    _BrowserCapabilityRuntime,
    execute_browser_capability,
)
from browser_worker.contracts.browser_capabilities import ExploreFlowArguments


class BrowserCapabilityContractTest(unittest.TestCase):
    def test_wait_for_accepts_structured_semantic_value_condition(self) -> None:
        parsed = ExploreFlowArguments.model_validate(
            {
                "steps": [
                    {
                        "actions": [
                            {
                                "action": "wait_for",
                                "plan_step_id": "quantity",
                                "locator": {
                                    "kind": "role",
                                    "role": "spinbutton",
                                },
                                "condition": {
                                    "type": "value_equals",
                                    "expected": "1",
                                },
                            }
                        ]
                    }
                ]
            }
        )

        action = parsed.steps[0].actions[0]
        self.assertIsNotNone(action.locator)
        self.assertIsNotNone(action.condition)
        assert action.locator is not None
        assert action.condition is not None
        self.assertEqual(action.plan_step_id, "quantity")
        self.assertEqual(action.locator.kind, "role")
        self.assertEqual(action.condition.type, "value_equals")

    def test_structured_exploration_locator_rejects_css_and_click(self) -> None:
        invalid_actions = [
            {
                "action": "wait_for",
                "locator": {"kind": "css", "value": "#quantity"},
            },
            {
                "action": "click",
                "locator": {"kind": "role", "role": "button", "name": "Save"},
            },
        ]
        for action in invalid_actions:
            with self.subTest(action=action), self.assertRaises(ValueError):
                ExploreFlowArguments.model_validate(
                    {"steps": [{"actions": [action]}]}
                )

    def test_same_project_sessions_apply_independent_context_policies(self) -> None:
        flow_arguments = {
            "base_url": "http://local.test",
            "probe_id": "probe-flow",
            "steps": [{"url": "/flow"}],
        }

        with (
            patch.object(
                _BrowserCapabilityRuntime,
                "run",
                side_effect=lambda operation: operation(),
            ),
            patch(
                "browser_worker.capabilities.browser_capabilities._storage_state_path",
                return_value="/tmp/project-7-state.json",
            ),
            patch(
                "browser_worker.capabilities.browser_capabilities._collect_flow_a11y",
                return_value=[],
            ) as collect_flow,
        ):
            normal = execute_browser_capability(
                None,
                capability="explore_flow",
                project_id=7,
                conversation_id="101",
                context={"clean_context": False},
                arguments=flow_arguments,
            )
            clean = execute_browser_capability(
                None,
                capability="explore_flow",
                project_id=7,
                conversation_id="102",
                context={"clean_context": True},
                arguments=flow_arguments,
            )

        self.assertEqual(
            collect_flow.call_args_list[0].kwargs["storage_state_path"],
            "/tmp/project-7-state.json",
        )
        self.assertIsNone(
            collect_flow.call_args_list[1].kwargs["storage_state_path"]
        )
        self.assertEqual(
            [call.kwargs["session_id"] for call in collect_flow.call_args_list],
            [101, 102],
        )
        self.assertEqual(
            [
                call.kwargs["isolated_context"]
                for call in collect_flow.call_args_list
            ],
            [True, True],
        )
        self.assertEqual(
            [call.kwargs["probe_id"] for call in collect_flow.call_args_list],
            ["probe-flow", "probe-flow"],
        )
        self.assertEqual(normal["probe_id"], "probe-flow")
        self.assertEqual(clean["probe_id"], "probe-flow")
        self.assertEqual(
            normal["context_evidence"],
            {
                "version": "v1",
                "clean_context_requested": False,
                "storage_state_loaded": True,
                "planning_session_id": 101,
                "execution_scope": "isolated_probe",
                "state_persisted": False,
            },
        )
        self.assertEqual(
            clean["context_evidence"],
            {
                "version": "v1",
                "clean_context_requested": True,
                "storage_state_loaded": False,
                "planning_session_id": 102,
                "execution_scope": "isolated_probe",
                "state_persisted": False,
            },
        )

    def test_validate_page_elements_does_not_read_browser_context(self) -> None:
        result = execute_browser_capability(
            None,
            capability="validate_page_elements",
            project_id=7,
            conversation_id="101",
            context={"clean_context": True},
            arguments={
                "required_elements": [
                    {
                        "id": "submit",
                        "description": "submit button",
                        "keywords": ["Submit"],
                    }
                ],
                "a11y_nodes": [
                    {
                        "node_id": "button-1",
                        "role": "button",
                        "name": "Submit",
                    }
                ],
            },
        )

        self.assertTrue(result["valid"])

    def test_explore_page_returns_clean_context_evidence(self) -> None:
        page = MagicMock()
        page.url = "http://local.test/page"

        with (
            patch.object(
                _BrowserCapabilityRuntime,
                "run",
                side_effect=lambda operation: operation(),
            ),
            patch(
                "browser_worker.capabilities.browser_capabilities._storage_state_path",
            ) as storage_state_path,
            patch(
                "browser_worker.capabilities.browser_capabilities."
                "BrowserSessionManager.get_or_create_context",
                return_value=(MagicMock(), page),
            ) as get_context,
            patch(
                "browser_worker.capabilities.browser_capabilities.collect_a11y_nodes",
                return_value=[],
            ),
        ):
            result = execute_browser_capability(
                None,
                capability="explore_page",
                project_id=7,
                conversation_id="103",
                context={"clean_context": True},
                arguments={
                    "url": "http://local.test/page",
                    "probe_id": "probe-1",
                },
            )

        storage_state_path.assert_not_called()
        get_context.assert_called_once_with(103, storage_state_path=None)
        self.assertEqual(
            result["context_evidence"],
            {
                "version": "v1",
                "clean_context_requested": True,
                "storage_state_loaded": False,
                "planning_session_id": 103,
            },
        )
        self.assertEqual(result["observation_v2"]["probe_id"], "probe-1")

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

    def test_validate_page_elements_allows_runtime_text_wait_without_node(self) -> None:
        result = execute_browser_capability(
            None,  # type: ignore[arg-type]
            capability="validate_page_elements",
            project_id=1,
            conversation_id="1",
            arguments={
                "dsl_case": {
                    "profile": "research-v1",
                    "name": "Runtime text",
                    "steps": [
                        {
                            "action": "wait_for",
                            "intent": "Wait for price text verified at runtime",
                            "target": "Rs. 400",
                            "timeout_ms": 5000,
                            "page_state": "detail",
                            "preconditions": [],
                            "postconditions": [],
                            "idempotency": "idempotent",
                            "side_effect": "none",
                        }
                    ],
                },
                "a11y_nodes_by_state": {
                    "detail": [
                        {
                            "node_id": "title",
                            "role": "heading",
                            "name": "Men Tshirt",
                        }
                    ]
                },
            },
        )

        self.assertTrue(result["valid"])
        self.assertEqual(result["warnings"], [])
        step = result["dsl_case"]["steps"][0]
        self.assertNotIn("match_count", step)
        self.assertEqual(step["locator_confidence"], "medium")
        self.assertEqual(step["candidates"], [])

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
