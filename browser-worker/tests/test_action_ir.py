from __future__ import annotations

from copy import deepcopy
import hashlib
import json
from pathlib import Path
import unittest

from pydantic import ValidationError

from browser_worker.exploration.locator_preflight import apply_preflight_to_dsl_by_state
from browser_worker.contracts.action_ir import (
    EXECUTABLE_CANDIDATE_STRATEGIES,
    validate_research_dsl,
)
from browser_worker.contracts.dsl import DSLCase, load_canonical_dsl


FIXTURE_PATH = (
    Path(__file__).parents[2] / "testdata" / "dsl_research_v1_contract.json"
)


def _draft_click() -> dict:
    return {
        "profile": "research-v1",
        "name": "Checkout",
        "steps": [
            {
                "action": "click",
                "intent": "Submit the order",
                "target": "Pay button",
                "page_state": "checkout",
                "preconditions": [
                    {"type": "element_visible", "value": "Pay button"}
                ],
                "postconditions": [
                    {"type": "text_visible", "value": "Receipt"}
                ],
                "idempotency": "non_idempotent",
                "side_effect": "external_state",
            }
        ],
    }


class ResearchActionIRContractTest(unittest.TestCase):
    def test_go_canonical_v2_bytes_and_sha_are_authoritative(self) -> None:
        fixture = json.loads(FIXTURE_PATH.read_text())

        case, payload = load_canonical_dsl(
            fixture["canonical_json"],
            fixture["sha256"],
            fixture["canonical_version"],
        )

        self.assertEqual(
            hashlib.sha256(fixture["canonical_json"].encode()).hexdigest(),
            fixture["sha256"],
        )
        self.assertEqual(case.model_dump(mode="json"), payload)
        self.assertEqual(case.profile, "research-v1")
        self.assertEqual(case.steps[1].candidates[0].strategy, "verified_css")

    def test_research_models_reject_missing_and_unknown_fields(self) -> None:
        mutations = []

        missing_intent = _draft_click()
        missing_intent["steps"][0].pop("intent")
        mutations.append(missing_intent)

        unknown_case = _draft_click()
        unknown_case["unexpected"] = True
        mutations.append(unknown_case)

        unknown_step = _draft_click()
        unknown_step["steps"][0]["unexpected"] = True
        mutations.append(unknown_step)

        unknown_condition = _draft_click()
        unknown_condition["steps"][0]["preconditions"][0]["unexpected"] = True
        mutations.append(unknown_condition)

        unknown_action = _draft_click()
        unknown_action["steps"][0]["action"] = "eval"
        mutations.append(unknown_action)

        padded_action = _draft_click()
        padded_action["steps"][0]["action"] = " click "
        mutations.append(padded_action)

        for payload in mutations:
            with self.subTest(payload=payload), self.assertRaises(ValidationError):
                validate_research_dsl(payload, phase="draft")

    def test_executable_rejects_unknown_or_unverified_candidates(self) -> None:
        draft = _draft_click()
        step = draft["steps"][0]
        step["locator_confidence"] = "high"
        step["candidates"] = [
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
        ]

        unknown = deepcopy(draft)
        unknown["steps"][0]["candidates"][0]["unexpected"] = True
        unverified = deepcopy(draft)
        unverified["steps"][0]["candidates"][0]["pre_features"] = {
            "source": "a11y_role_fuzzy"
        }
        unsupported = deepcopy(draft)
        unsupported["steps"][0]["candidates"][0]["strategy"] = (
            "invented_selector"
        )

        for payload in (unknown, unverified, unsupported):
            with self.subTest(payload=payload), self.assertRaises(ValidationError):
                validate_research_dsl(payload)

    def test_runtime_text_verification_steps_do_not_require_candidates(self) -> None:
        payload = {
            "profile": "research-v1",
            "name": "Verify runtime text",
            "steps": [
                {
                    "action": "wait_for",
                    "intent": "Wait for price text",
                    "target": "Rs. 400",
                    "timeout_ms": 5000,
                    "page_state": "detail",
                    "preconditions": [],
                    "postconditions": [],
                    "idempotency": "idempotent",
                    "side_effect": "none",
                    "locator_confidence": "medium",
                    "candidates": [],
                },
                {
                    "action": "assert_text",
                    "intent": "Assert detail text",
                    "target": "body",
                    "value": "Rs. 400",
                    "page_state": "detail",
                    "preconditions": [],
                    "postconditions": [],
                    "idempotency": "idempotent",
                    "side_effect": "none",
                    "locator_confidence": "medium",
                    "candidates": [],
                },
            ],
        }

        validate_research_dsl(payload)

    def test_goto_allows_empty_preconditions_but_requires_postconditions(self) -> None:
        payload = {
            "profile": "research-v1",
            "name": "Open checkout",
            "steps": [
                {
                    "action": "goto",
                    "intent": "Open checkout",
                    "target": "Checkout page",
                    "value": "/checkout",
                    "preconditions": [],
                    "postconditions": [
                        {"type": "url_contains", "value": "/checkout"}
                    ],
                    "idempotency": "idempotent",
                    "side_effect": "browser_state",
                }
            ],
        }

        validate_research_dsl(payload, phase="draft")
        payload["steps"][0]["postconditions"][0]["timeout_ms"] = 3000.5
        validate_research_dsl(payload, phase="draft")
        payload["steps"][0]["postconditions"] = []
        with self.assertRaises(ValidationError):
            validate_research_dsl(payload, phase="draft")

    def test_research_preflight_preserves_declared_semantics(self) -> None:
        draft = _draft_click()
        original = deepcopy(draft)

        result = apply_preflight_to_dsl_by_state(
            deepcopy(draft),
            {
                "checkout": [
                    {
                        "node_id": "pay",
                        "role": "button",
                        "name": "Pay button",
                        "verified_selectors": [
                            {"strategy": "css", "selector": "#pay"}
                        ],
                    }
                ]
            },
        )

        self.assertEqual(result["profile"], "research-v1")
        self.assertEqual(
            {
                key: value
                for key, value in result["steps"][0].items()
                if key not in {"candidates", "locator_confidence"}
            },
            original["steps"][0],
        )
        self.assertNotIn("match_count", result["steps"][0])
        self.assertEqual(
            result["steps"][0]["candidates"][0]["strategy"],
            "verified_css",
        )
        strategies = {
            candidate["strategy"]
            for candidate in result["steps"][0]["candidates"]
        }
        self.assertNotIn("role_fuzzy", strategies)
        self.assertTrue(strategies <= EXECUTABLE_CANDIDATE_STRATEGIES)
        self.assertEqual(result["_preflight"]["locator_confidence"], "high")
        validate_research_dsl(
            {
                key: value
                for key, value in result.items()
                if key != "_preflight"
            }
        )

    def test_research_preflight_accepts_verified_selector_without_name(self) -> None:
        draft = {
            "profile": "research-v1",
            "name": "Set quantity",
            "steps": [
                {
                    "action": "input",
                    "intent": "Set the quantity field",
                    "target": "#quantity",
                    "target_strategy": "css",
                    "page_state": "detail",
                    "value": "1",
                    "preconditions": [
                        {"type": "element_visible", "value": "#quantity"}
                    ],
                    "postconditions": [
                        {"type": "value_changed", "value": "1"}
                    ],
                    "idempotency": "idempotent",
                    "side_effect": "browser_state",
                }
            ],
        }

        result = apply_preflight_to_dsl_by_state(
            deepcopy(draft),
            {
                "detail": [
                    {
                        "node_id": "quantity",
                        "role": "spinbutton",
                        "dom": {"tag": "input", "attrs": {"id": "quantity"}},
                        "verified_selectors": [
                            {
                                "strategy": "css",
                                "selector": "#quantity",
                                "source": "a11y_backend_dom_node",
                            }
                        ],
                    }
                ]
            },
        )

        step = result["steps"][0]
        self.assertEqual(step["locator_confidence"], "high")
        self.assertEqual(step["candidates"][0]["strategy"], "verified_css")
        self.assertEqual(step["candidates"][0]["selector"], "#quantity")
        self.assertEqual(
            step["candidates"][0]["pre_features"]["source"],
            "a11y_backend_dom_node",
        )
        validate_research_dsl(
            {
                key: value
                for key, value in result.items()
                if key != "_preflight"
            }
        )

    def test_legacy_scope_parser_matches_runtime_role_scope_syntax(self) -> None:
        draft = {
            "profile": "research-v1",
            "name": "Edit one row",
            "steps": [
                {
                    "action": "click",
                    "intent": "Edit Alice",
                    "target": 'button="Edit" inside "Alice"',
                    "page_state": "table",
                    "preconditions": [
                        {"type": "element_visible", "value": "Alice"}
                    ],
                    "postconditions": [{"type": "dom_changed"}],
                    "idempotency": "idempotent",
                    "side_effect": "browser_state",
                }
            ],
        }
        result = apply_preflight_to_dsl_by_state(
            draft,
            {
                "table": [
                    {
                        "node_id": "row-alice",
                        "role": "product",
                        "name": "Alice",
                    },
                    {
                        "node_id": "alice-name",
                        "parent_id": "row-alice",
                        "role": "text",
                        "name": "Alice",
                    },
                    {
                        "node_id": "alice-edit",
                        "parent_id": "row-alice",
                        "role": "button",
                        "name": "Edit",
                    },
                ]
            },
        )
        self.assertEqual(result["_preflight"]["locator_confidence"], "high")
        self.assertEqual(
            result["steps"][0]["candidates"][0]["strategy"],
            "a11y_scoped_role_exact",
        )

    def test_legacy_case_still_ignores_extra_fields(self) -> None:
        case = DSLCase.model_validate(
            {
                "name": "legacy",
                "unknown_case_field": True,
                "steps": [
                    {
                        "action": "click",
                        "target": "Login",
                        "unknown_step_field": True,
                    }
                ],
            }
        )

        self.assertNotIn("unknown_case_field", case.model_dump())
        self.assertNotIn("unknown_step_field", case.model_dump()["steps"][0])


if __name__ == "__main__":
    unittest.main()
