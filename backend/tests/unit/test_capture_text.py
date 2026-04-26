"""Tests for the capture_text DSL action and runtime context."""

import pytest

from app.schemas.dsl import (
    CaptureTextStep,
    DSLCase,
    DSLCaseInputContract,
    DSLStep,
)
from app.runners.playwright_runner import _substitute_variables


# ---------------------------------------------------------------------------
# CaptureTextStep schema validation
# ---------------------------------------------------------------------------


class TestCaptureTextStepSchema:
    def test_valid_capture_text_step(self):
        step = CaptureTextStep(action="capture_text", target="Product Price", context_key="product_price")
        assert step.action == "capture_text"
        assert step.target == "Product Price"
        assert step.context_key == "product_price"
        assert step.target_strategy is None

    def test_valid_with_strategy(self):
        step = CaptureTextStep(action="capture_text", target="#price", context_key="price", target_strategy="css")
        assert step.target_strategy == "css"

    def test_missing_context_key_rejected(self):
        with pytest.raises(Exception):
            CaptureTextStep(action="capture_text", target="Price")

    def test_invalid_context_key_pattern_rejected(self):
        with pytest.raises(Exception):
            CaptureTextStep(action="capture_text", target="Price", context_key="123abc")

    def test_invalid_context_key_with_spaces_rejected(self):
        with pytest.raises(Exception):
            CaptureTextStep(action="capture_text", target="Price", context_key="my key")

    def test_underscore_prefix_allowed(self):
        step = CaptureTextStep(action="capture_text", target="Price", context_key="_price")
        assert step.context_key == "_price"


# ---------------------------------------------------------------------------
# DSLStep union includes CaptureTextStep
# ---------------------------------------------------------------------------


class TestDSLStepUnion:
    def test_capture_text_in_union(self):
        step_data = {"action": "capture_text", "target": "Price", "context_key": "item_price"}
        from pydantic import TypeAdapter
        adapter = TypeAdapter(DSLStep)
        step = adapter.validate_python(step_data)
        assert isinstance(step, CaptureTextStep)
        assert step.context_key == "item_price"


# ---------------------------------------------------------------------------
# DSLCase round-trip with capture_text
# ---------------------------------------------------------------------------


class TestDSLCaseWithCaptureText:
    def test_case_with_capture_then_assert(self):
        case = DSLCase(
            name="Price consistency",
            base_url="https://example.com",
            steps=[
                {"action": "goto", "value": "/product/1"},
                {"action": "capture_text", "target": "Price", "context_key": "detail_price"},
                {"action": "click", "target": "Add to cart"},
                {"action": "click", "target": "View Cart"},
                {"action": "assert_text", "target": "Cart Price", "value": "${detail_price}"},
            ],
        )
        assert len(case.steps) == 5
        assert isinstance(case.steps[1], CaptureTextStep)
        assert case.steps[1].context_key == "detail_price"
        assert case.steps[4].value == "${detail_price}"

    def test_case_serialization_round_trip(self):
        case = DSLCase(
            name="Capture test",
            steps=[
                {"action": "capture_text", "target": "Title", "context_key": "page_title"},
            ],
        )
        data = case.model_dump()
        restored = DSLCase.model_validate(data)
        assert isinstance(restored.steps[0], CaptureTextStep)
        assert restored.steps[0].context_key == "page_title"


# ---------------------------------------------------------------------------
# _substitute_variables with runtime context
# ---------------------------------------------------------------------------


class TestSubstitutionWithRuntimeContext:
    def test_runtime_context_resolves(self):
        result = _substitute_variables("${detail_price}", {"detail_price": "Rs. 500"})
        assert result == "Rs. 500"

    def test_runtime_context_merged_with_input_values(self):
        input_values = {"login_email": "user@test.com"}
        runtime_context = {"detail_price": "Rs. 500"}
        combined = dict(input_values)
        combined.update(runtime_context)

        assert _substitute_variables("${login_email}", combined) == "user@test.com"
        assert _substitute_variables("${detail_price}", combined) == "Rs. 500"

    def test_runtime_context_overrides_input_values(self):
        """Runtime capture takes precedence over initial input values."""
        input_values = {"price": "100"}
        runtime_context = {"price": "200"}
        combined = dict(input_values)
        combined.update(runtime_context)
        assert _substitute_variables("${price}", combined) == "200"

    def test_unresolved_variable_kept_as_is(self):
        result = _substitute_variables("${unknown_var}", {"other": "value"})
        assert result == "${unknown_var}"
