"""Tests for locator stability scoring, rich element formatting, and DSL confidence."""
from __future__ import annotations

import pytest

from app.ai.page_explorer import (
    _compute_element_stability,
    _format_element_rich,
    format_elements_for_prompt,
)
from app.schemas.dsl import ClickStep, InputStep, WaitForStep, AssertTextStep, CaptureTextStep


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture()
def single_element() -> dict:
    return {
        "tag": "button",
        "text": "Submit",
        "role": "button",
        "aria_label": None,
        "placeholder": None,
        "data_testid": None,
        "css_selector": "button.submit-btn",
        "xpath": "//button[text()='Submit']",
        "visible": True,
        "enabled": True,
    }


@pytest.fixture()
def elements_with_duplicates() -> list[dict]:
    return [
        {
            "tag": "a",
            "text": "Add to cart",
            "role": None,
            "aria_label": None,
            "data_testid": None,
            "css_selector": "a.btn.btn-default",
            "xpath": "(//a[text()='Add to cart'])[1]",
            "visible": True,
            "enabled": True,
        },
        {
            "tag": "a",
            "text": "Add to cart",
            "role": None,
            "aria_label": None,
            "data_testid": None,
            "css_selector": "a.btn.btn-default",
            "xpath": "(//a[text()='Add to cart'])[2]",
            "visible": True,
            "enabled": True,
        },
        {
            "tag": "a",
            "text": "Add to cart",
            "role": None,
            "aria_label": None,
            "data_testid": "add-to-cart-3",
            "css_selector": "a[data-testid='add-to-cart-3']",
            "xpath": "//a[@data-testid='add-to-cart-3']",
            "visible": True,
            "enabled": True,
        },
    ]


# ---------------------------------------------------------------------------
# _compute_element_stability tests
# ---------------------------------------------------------------------------

class TestComputeElementStability:
    def test_unique_data_testid_gets_highest_score(self):
        element = {"tag": "button", "data_testid": "submit-btn", "text": "Submit"}
        all_elements = [element]
        assert _compute_element_stability(element, all_elements) == 0.95

    def test_duplicate_data_testid_gets_lower_score(self):
        element = {"tag": "button", "data_testid": "btn", "text": "Click"}
        all_elements = [element, {"tag": "button", "data_testid": "btn", "text": "Click"}]
        assert _compute_element_stability(element, all_elements) == 0.85

    def test_stable_id_gets_0_85(self):
        element = {"tag": "input", "id": "email", "text": ""}
        all_elements = [element]
        assert _compute_element_stability(element, all_elements) == 0.85

    def test_dynamic_id_gets_no_id_bonus(self):
        element = {"tag": "input", "id": "input-a3f8b2c1", "text": ""}
        all_elements = [element]
        # Dynamic ID should not get 0.90, should fall through
        score = _compute_element_stability(element, all_elements)
        assert score < 0.90

    def test_unique_aria_label_with_role(self):
        element = {"tag": "button", "aria_label": "Close dialog", "role": "button", "text": "X"}
        all_elements = [element]
        assert _compute_element_stability(element, all_elements) == 0.90

    def test_unique_aria_label_alone(self):
        element = {"tag": "input", "aria_label": "Search", "text": ""}
        all_elements = [element]
        assert _compute_element_stability(element, all_elements) == 0.82

    def test_unique_href_navigation(self):
        element = {"tag": "a", "href": "/products/1", "text": "View Product"}
        all_elements = [element]
        assert _compute_element_stability(element, all_elements) == 0.70

    def test_duplicate_href_navigation(self):
        element = {"tag": "a", "href": "/products", "text": "Products"}
        all_elements = [element, {"tag": "a", "href": "/products", "text": "Products"}]
        score = _compute_element_stability(element, all_elements)
        assert 0.55 <= score <= 0.65

    def test_unique_text_no_duplicates(self):
        element = {"tag": "h1", "text": "Welcome", "xpath": "//h1"}
        all_elements = [element]
        assert _compute_element_stability(element, all_elements) == 0.50

    def test_duplicate_text_with_css(self, elements_with_duplicates):
        score = _compute_element_stability(elements_with_duplicates[0], elements_with_duplicates)
        assert score == 0.30

    def test_duplicate_text_no_css(self):
        element = {"tag": "button", "text": "Delete", "xpath": "//button[text()='Delete'][1]"}
        all_elements = [element, {"tag": "button", "text": "Delete", "xpath": "//button[text()='Delete'][2]"}]
        score = _compute_element_stability(element, all_elements)
        assert score == 0.15

    def test_xpath_position_index(self):
        element = {"tag": "div", "xpath": "(//div[@class='item'])[3]", "text": ""}
        all_elements = [element]
        assert _compute_element_stability(element, all_elements) == 0.10


# ---------------------------------------------------------------------------
# _format_element_rich tests
# ---------------------------------------------------------------------------

class TestFormatElementRich:
    def test_full_attributes(self):
        element = {
            "tag": "a",
            "data_testid": "view-product-1",
            "href": "/product_details/1",
            "text": "View Product",
            "css_selector": "a.productinfo",
            "xpath": "//a[text()='View Product']",
            "rect": {"x": 100.5, "y": 200.3, "width": 80.0, "height": 24.0},
            "enabled": True,
        }
        result = _format_element_rich(element, 0.85)
        assert "a" in result
        assert "[data-testid='view-product-1']" in result
        assert "[href='/product_details/1']" in result
        assert "stable=0.85" in result
        assert "css=" in result
        assert "xpath=" in result
        assert "rect=" in result

    def test_minimal_attributes(self):
        element = {"tag": "button", "text": "Click Me"}
        result = _format_element_rich(element, 0.40)
        assert "button" in result
        assert "[text='Click Me']" in result
        assert "stable=0.40" in result

    def test_disabled_element(self):
        element = {"tag": "button", "text": "Submit", "enabled": False}
        result = _format_element_rich(element, 0.30)
        assert "disabled" in result

    def test_long_text_truncated(self):
        element = {"tag": "p", "text": "A" * 200}
        result = _format_element_rich(element, 0.50)
        assert "..." in result
        assert len(result) < 400  # Reasonable length


# ---------------------------------------------------------------------------
# format_elements_for_prompt tests
# ---------------------------------------------------------------------------

class TestFormatElementsForPrompt:
    def test_filters_invisible(self):
        elements = [
            {"tag": "button", "text": "Visible", "visible": True},
            {"tag": "button", "text": "Hidden", "visible": False},
        ]
        result = format_elements_for_prompt(elements)
        assert "Visible" in result
        assert "Hidden" not in result

    def test_multiple_elements(self, elements_with_duplicates):
        result = format_elements_for_prompt(elements_with_duplicates)
        lines = result.strip().split("\n")
        assert len(lines) == 3
        # Third element has data-testid, should have highest stability
        assert "stable=0.95" in lines[2]
        # First two should have lower stability
        assert "stable=" in lines[0]
        assert "stable=" in lines[1]

    def test_empty_elements(self):
        result = format_elements_for_prompt([])
        assert result == ""


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


# ---------------------------------------------------------------------------
# Integration: stability score produces reasonable formatting for real-world case
# ---------------------------------------------------------------------------

class TestRealWorldScenario:
    def test_brand_filter_cart_elements(self):
        """Simulate the Automation Exercise product page with multiple 'Add to cart' buttons."""
        elements = [
            {
                "tag": "a",
                "text": "Add to cart",
                "href": None,
                "data_testid": None,
                "css_selector": "a.btn.btn-default.add-to-cart",
                "xpath": "(//a[contains(text(),'Add to cart')])[1]",
                "visible": True, "enabled": True,
            },
            {
                "tag": "a",
                "text": "Add to cart",
                "href": None,
                "data_testid": None,
                "css_selector": "a.btn.btn-default.add-to-cart",
                "xpath": "(//a[contains(text(),'Add to cart')])[2]",
                "visible": True, "enabled": True,
            },
            {
                "tag": "a",
                "text": "View Product",
                "href": "/product_details/2",
                "data_testid": None,
                "css_selector": "a.productinfo",
                "xpath": "//a[contains(@href,'/product_details/2')]",
                "visible": True, "enabled": True,
            },
        ]

        result = format_elements_for_prompt(elements)
        lines = result.strip().split("\n")
        assert len(lines) == 3

        # "Add to cart" elements should have low stability (duplicates + xpath position)
        add_to_cart_lines = [l for l in lines if "Add to cart" in l]
        assert len(add_to_cart_lines) == 2
        for line in add_to_cart_lines:
            # Should contain stability score and indicate low confidence
            assert "stable=" in line

        # "View Product" should have higher stability (unique href)
        view_product_line = [l for l in lines if "View Product" in l][0]
        assert "stable=0.70" in view_product_line
