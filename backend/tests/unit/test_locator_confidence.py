"""Tests for locator stability scoring, rich element formatting, and DSL confidence."""
from __future__ import annotations

import pytest

from app.schemas.dsl import ClickStep, InputStep, WaitForStep, AssertTextStep, CaptureTextStep



# ---------------------------------------------------------------------------
# DSL Schema locator_confidence field tests
# ---------------------------------------------------------------------------

class TestDslLocatorConfidence:
    def test_click_step_accepts_confidence(self):
        step = ClickStep(action="click", target="Submit", locator_confidence="high")
        assert step.locator_confidence == "high"

    def test_click_step_confidence_defaults_none(self):
        step = ClickStep(action="click", target="Submit")
        assert step.locator_confidence is None

    def test_input_step_accepts_confidence(self):
        step = InputStep(action="input", target="Email", value="test@test.com", locator_confidence="medium")
        assert step.locator_confidence == "medium"

    def test_wait_for_step_accepts_confidence(self):
        step = WaitForStep(action="wait_for", target="Loaded", locator_confidence="low")
        assert step.locator_confidence == "low"

    def test_assert_text_step_accepts_confidence(self):
        step = AssertTextStep(action="assert_text", target="Title", value="Hello", locator_confidence="high")
        assert step.locator_confidence == "high"

    def test_capture_text_step_accepts_confidence(self):
        step = CaptureTextStep(action="capture_text", target="Price", context_key="price", locator_confidence="low")
        assert step.locator_confidence == "low"

    def test_invalid_confidence_rejected(self):
        with pytest.raises(Exception):
            ClickStep(action="click", target="Submit", locator_confidence="invalid")


class TestA11yPreflightProductActions:
    def test_repeated_bare_add_to_cart_is_low_confidence(self):
        from app.ai.locator_preflight import apply_preflight_to_dsl

        dsl = {"steps": [{"action": "click", "target": "Add to cart"}]}
        a11y_nodes = [
            {"role": "link", "name": "Add to cart"},
            {"role": "link", "name": "Add to cart"},
        ]

        result = apply_preflight_to_dsl(dsl, a11y_nodes)

        assert result["steps"][0]["locator_confidence"] == "low"
        assert "repeated product action" in result["_preflight"]["warnings"][0]

    def test_verified_a11y_candidate_is_added_to_step_candidates(self):
        from app.ai.locator_preflight import apply_preflight_to_dsl

        dsl = {"steps": [{"action": "click", "target": "Add to cart"}]}
        a11y_nodes = [
            {
                "role": "link",
                "name": "Add to cart",
                "verified_selectors": [
                    {
                        "strategy": "css",
                        "selector": "a[data-product-id=\"1\"]:visible",
                        "source": "a11y_backend_dom_node",
                    }
                ],
            },
        ]

        result = apply_preflight_to_dsl(dsl, a11y_nodes)

        assert result["steps"][0]["locator_confidence"] == "high"
        assert result["steps"][0]["match_count"] == 1
        assert result["steps"][0]["candidates"][0]["strategy"] == "verified_css"
        assert result["steps"][0]["candidates"][0]["selector"] == "a[data-product-id=\"1\"]:visible"
