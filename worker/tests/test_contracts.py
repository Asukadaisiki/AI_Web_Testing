"""契约形状与条件阶段表（CONTRACT §2 / §2.2）—— 纯逻辑，不碰浏览器。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

WORKER_ROOT = Path(__file__).resolve().parents[1]
TESTS_DIR = Path(__file__).resolve().parent
for _path in (str(WORKER_ROOT), str(TESTS_DIR)):
    if _path not in sys.path:
        sys.path.insert(0, _path)

from case_builder import action_step, build_case, condition, goto_step, role_locator  # noqa: E402
from loop_worker.contracts import (  # noqa: E402
    ALL_CONDITION_TYPES,
    DEFAULT_CONDITION_TIMEOUT_MS,
    DEFAULT_STEP_TIMEOUT_MS,
    POST_ALLOWED,
    PRE_ALLOWED,
    CaseInvalid,
    validate_case,
)

PAGE_URL = "http://127.0.0.1/index.html"


def goto_only(**overrides: object) -> dict:
    step = goto_step(0, PAGE_URL, "/index.html")
    step.update(overrides)
    return build_case([step])


class ConditionPhaseTableTest(unittest.TestCase):
    """条件阶段表是 CONTRACT §2.2 的唯一权威，这里逐字比对。"""

    def test_pre_allowed_is_state_facts_only(self) -> None:
        self.assertEqual(
            PRE_ALLOWED,
            frozenset({"url_contains", "text_visible", "text_gone", "element_state"}),
        )

    def test_post_allowed_contains_all_condition_types(self) -> None:
        self.assertEqual(
            POST_ALLOWED,
            frozenset(
                {
                    "url_contains",
                    "text_visible",
                    "text_gone",
                    "url_changes",
                    "value_equals",
                    "element_state",
                    "attribute_equals",
                    "count_equals",
                }
            ),
        )

    def test_all_condition_types_order(self) -> None:
        self.assertEqual(
            ALL_CONDITION_TYPES,
            (
                "url_contains",
                "text_visible",
                "text_gone",
                "url_changes",
                "value_equals",
                "element_state",
                "attribute_equals",
                "count_equals",
            ),
        )

    def test_transition_conditions_are_rejected_as_preconditions(self) -> None:
        for type_ in ("url_changes", "value_equals", "attribute_equals", "count_equals"):
            with self.subTest(type=type_):
                case = build_case(
                    [
                        goto_step(0, PAGE_URL, "/index.html"),
                        action_step(
                            1,
                            "click",
                            locator=role_locator("button", "Filter"),
                            page_url=PAGE_URL,
                            pre=[condition(type_, "anything")],
                            post=[condition("text_visible", "Results ready")],
                        ),
                    ]
                )
                with self.assertRaises(CaseInvalid) as caught:
                    validate_case(case)
                self.assertEqual(caught.exception.code, "condition_phase")

    def test_all_five_conditions_are_accepted_as_postconditions(self) -> None:
        for type_ in ALL_CONDITION_TYPES:
            with self.subTest(type=type_):
                case = build_case(
                    [
                        goto_step(0, PAGE_URL, "/index.html"),
                        action_step(
                            1,
                            "click",
                            locator=role_locator("button", "Filter"),
                            page_url=PAGE_URL,
                            pre=[condition("url_contains", "/index.html")],
                            post=[condition(type_, "anything")],
                        ),
                    ]
                )
                validated = validate_case(case)
                self.assertEqual(validated.steps[1].postconditions[0].type, type_)

    def test_unknown_condition_type_is_rejected(self) -> None:
        case = build_case(
            [
                goto_step(
                    0,
                    PAGE_URL,
                    "/index.html",
                    # element_visible 不在表里：未在表中即为非法
                )
            ]
        )
        case["steps"][0]["postconditions"] = [condition("element_visible", "#item")]
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "condition_unknown_type")


class CaseShapeTest(unittest.TestCase):
    def test_valid_case_is_accepted_and_normalized(self) -> None:
        case = build_case(
            [
                goto_step(0, PAGE_URL, "/index.html"),
                action_step(
                    1,
                    "input",
                    locator=role_locator("textbox", "Search items"),
                    page_url=PAGE_URL,
                    pre=[condition("url_contains", "/index.html")],
                    post=[condition("value_equals", "alpha")],
                    value="alpha",
                ),
                action_step(
                    2,
                    "assert_text",
                    page_url=PAGE_URL,
                    pre=[condition("url_contains", "/index.html")],
                    post=[condition("text_visible", "Alpha")],
                    value="Alpha",
                ),
            ]
        )
        validated = validate_case(case)
        self.assertEqual([step.index for step in validated.steps], [0, 1, 2])
        self.assertEqual(validated.steps[1].action, "input")

    def test_default_timeouts(self) -> None:
        case = build_case(
            [
                {
                    "index": 0,
                    "action": "goto",
                    "intent": "open",
                    "value": PAGE_URL,
                    "preconditions": [],
                    "postconditions": [{"type": "url_contains", "value": "/index.html"}],
                }
            ]
        )
        validated = validate_case(case)
        self.assertEqual(validated.steps[0].timeout_ms, DEFAULT_STEP_TIMEOUT_MS)
        self.assertEqual(
            validated.steps[0].postconditions[0].timeout_ms, DEFAULT_CONDITION_TIMEOUT_MS
        )

    def test_first_step_must_be_goto(self) -> None:
        case = build_case(
            [
                action_step(
                    0,
                    "click",
                    locator=role_locator("button", "Filter"),
                    page_url=PAGE_URL,
                    pre=[condition("url_contains", "/index.html")],
                    post=[condition("text_visible", "Results ready")],
                )
            ]
        )
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "case_not_goto_first")
        self.assertIn("pre-navigate", caught.exception.detail)

    def test_goto_forbids_preconditions(self) -> None:
        case = goto_only(preconditions=[condition("url_contains", "/index.html")])
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "goto_precondition_forbidden")

    def test_goto_value_must_be_absolute(self) -> None:
        case = goto_only(value="/index.html")
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "goto_value_not_absolute")

    def test_goto_requires_a_postcondition(self) -> None:
        case = goto_only(postconditions=[])
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "step_missing_postcondition")

    def test_click_requires_target_and_precondition(self) -> None:
        case = build_case(
            [
                goto_step(0, PAGE_URL, "/index.html"),
                action_step(
                    1,
                    "click",
                    page_url=PAGE_URL,
                    pre=[condition("url_contains", "/index.html")],
                    post=[condition("text_visible", "Results ready")],
                ),
            ]
        )
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "step_missing_target")

        case = build_case(
            [
                goto_step(0, PAGE_URL, "/index.html"),
                action_step(
                    1,
                    "click",
                    locator=role_locator("button", "Filter"),
                    page_url=PAGE_URL,
                    pre=[],
                    post=[condition("text_visible", "Results ready")],
                ),
            ]
        )
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "step_missing_precondition")

    def test_input_requires_value_but_empty_string_is_allowed(self) -> None:
        base = build_case(
            [
                goto_step(0, PAGE_URL, "/index.html"),
                action_step(
                    1,
                    "input",
                    locator=role_locator("textbox", "Search items"),
                    page_url=PAGE_URL,
                    pre=[condition("url_contains", "/index.html")],
                    post=[condition("value_equals", "x")],
                ),
            ]
        )
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(base)
        self.assertEqual(caught.exception.code, "step_missing_value")

        base["steps"][1]["value"] = ""
        validated = validate_case(base)
        self.assertEqual(validated.steps[1].value, "")

    def test_click_must_not_carry_value(self) -> None:
        case = build_case(
            [
                goto_step(0, PAGE_URL, "/index.html"),
                action_step(
                    1,
                    "click",
                    locator=role_locator("button", "Filter"),
                    page_url=PAGE_URL,
                    pre=[condition("url_contains", "/index.html")],
                    post=[condition("text_visible", "Results ready")],
                    value="nope",
                ),
            ]
        )
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "step_unexpected_value")

    def test_assert_text_must_not_carry_target(self) -> None:
        case = build_case(
            [
                goto_step(0, PAGE_URL, "/index.html"),
                action_step(
                    1,
                    "assert_text",
                    locator=role_locator("button", "Filter"),
                    page_url=PAGE_URL,
                    pre=[condition("url_contains", "/index.html")],
                    post=[condition("text_visible", "Alpha")],
                    value="Alpha",
                ),
            ]
        )
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "step_unexpected_target")

    def test_unknown_action_is_rejected(self) -> None:
        case = build_case(
            [
                goto_step(0, PAGE_URL, "/index.html"),
                {
                    "index": 1,
                    "action": "drag",
                    "intent": "hover",
                    "preconditions": [condition("url_contains", "/index.html")],
                    "postconditions": [condition("text_visible", "Alpha")],
                },
            ]
        )
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "step_unknown_action")

    def test_condition_requires_non_empty_value(self) -> None:
        case = goto_only(
            postconditions=[condition("text_visible", "")],
        )
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "condition_missing_value")

    def test_ungrounded_target_is_rejected(self) -> None:
        case = build_case(
            [
                goto_step(0, PAGE_URL, "/index.html"),
                {
                    "index": 1,
                    "action": "click",
                    "intent": "click",
                    "target": {
                        "hint": "Filter",
                        "locator": role_locator("button", "Filter"),
                        "grounding": {
                            "observation_id": "",
                            "page_state_id": "",
                            "candidate_id": "",
                            "page_url": "",
                        },
                    },
                    "preconditions": [condition("url_contains", "/index.html")],
                    "postconditions": [condition("text_visible", "Results ready")],
                },
            ]
        )
        with self.assertRaises(CaseInvalid) as caught:
            validate_case(case)
        self.assertEqual(caught.exception.code, "step_target_ungrounded")

    def test_case_invalid_exposes_code_and_detail(self) -> None:
        error = CaseInvalid("some_code", "some detail")
        self.assertEqual(error.code, "some_code")
        self.assertEqual(error.detail, "some detail")


if __name__ == "__main__":
    unittest.main()
