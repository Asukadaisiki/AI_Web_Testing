"""Tests for a11y-based semantic locator resolution."""

from __future__ import annotations

import pytest

from app.locators import LocatorResolutionError, resolve_semantic_locator


class FakeNodeLocator:
    def __init__(self, payload: dict) -> None:
        self.payload = payload

    def evaluate(self, _script: str):
        return self.payload


class FakeLocatorCollection:
    def __init__(self, payloads: list[dict]) -> None:
        self.payloads = payloads

    def count(self) -> int:
        return len(self.payloads)

    def nth(self, index: int) -> FakeNodeLocator:
        return FakeNodeLocator(self.payloads[index])


class FakePage:
    def __init__(self, mapping: dict[str, list[dict]]) -> None:
        self.mapping = mapping

    def locator(self, target: str):
        return FakeLocatorCollection(self.mapping.get(f"locator:{target}", []))

    def get_by_label(self, target: str, exact: bool = True):
        return FakeLocatorCollection(self.mapping.get(f"label:{target}:{exact}", []))

    def get_by_placeholder(self, target: str, exact: bool = True):
        return FakeLocatorCollection(self.mapping.get(f"placeholder:{target}:{exact}", []))

    def get_by_role(self, role: str, name: str, exact: bool = True):
        return FakeLocatorCollection(self.mapping.get(f"role:{role}:{name}:{exact}", []))

    def get_by_text(self, target: str, exact: bool = True):
        return FakeLocatorCollection(self.mapping.get(f"text:{target}:{exact}", []))

    def get_by_test_id(self, target: str):
        return FakeLocatorCollection(self.mapping.get(f"testid:{target}", []))


def _candidate(*, preview_text: str, visible: bool, enabled: bool, role: str = "button") -> dict:
    return {
        "preview_text": preview_text,
        "role": role,
        "attributes": {
            "aria_label": preview_text,
            "placeholder": None,
            "data_testid": None,
        },
        "visible": visible,
        "enabled": enabled,
    }


class TestA11yRoleParsing:
    """Test parsing of role="name" format targets."""

    def test_button_role_exact_match(self):
        """button="Login" should match via a11y_role_exact."""
        page = FakePage({
            "role:button:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        resolved = resolve_semantic_locator(page, 'button="Login"', require_visible=True, require_enabled=True)
        assert resolved.strategy == "a11y_role_exact"
        assert resolved.trace.match_strategy == "a11y_role_exact"
        assert resolved.trace.selected_candidate is not None
        assert resolved.trace.selected_candidate.score > 0

    def test_link_role_exact_match(self):
        """link="Signup / Login" should match via a11y_role_exact."""
        page = FakePage({
            "role:link:Signup / Login:True": [_candidate(preview_text="Signup / Login", visible=True, enabled=True, role="link")],
        })
        resolved = resolve_semantic_locator(page, 'link="Signup / Login"', require_visible=True, require_enabled=True)
        assert resolved.strategy == "a11y_role_exact"

    def test_textbox_role_exact_match(self):
        """textbox="Email" should match via a11y_role_exact."""
        page = FakePage({
            "role:textbox:Email:True": [_candidate(preview_text="Email", visible=True, enabled=True, role="textbox")],
        })
        resolved = resolve_semantic_locator(page, 'textbox="Email"', require_visible=True, require_enabled=True)
        assert resolved.strategy == "a11y_role_exact"

    def test_heading_role_exact_match(self):
        """heading="Welcome" should match via a11y_role_exact."""
        page = FakePage({
            "role:heading:Welcome:True": [_candidate(preview_text="Welcome", visible=True, enabled=True, role="heading")],
        })
        resolved = resolve_semantic_locator(page, 'heading="Welcome"', require_visible=True, require_enabled=True)
        assert resolved.strategy == "a11y_role_exact"


class TestA11yRoleFallback:
    """Test fallback behavior when a11y role exact match fails."""

    def test_fuzzy_role_match(self):
        """When exact match fails, fuzzy should be tried."""
        page = FakePage({
            "role:button:Login:False": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        resolved = resolve_semantic_locator(page, 'button="Login"', require_visible=True, require_enabled=True)
        assert resolved.strategy == "a11y_role_fuzzy"

    def test_text_fallback(self):
        """When role match fails, text match should be tried."""
        page = FakePage({
            "text:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        resolved = resolve_semantic_locator(page, 'button="Login"', require_visible=True, require_enabled=True)
        assert resolved.strategy == "a11y_text_exact"

    def test_plain_text_target(self):
        """Plain text target (no role prefix) should use text match."""
        page = FakePage({
            "text:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        resolved = resolve_semantic_locator(page, "Login", require_visible=True, require_enabled=True)
        assert resolved.strategy == "a11y_text_exact"


class TestVisibilityAndEnabled:
    """Test visibility and enabled checks."""

    def test_visibility_failure(self):
        """Invisible elements should be rejected."""
        page = FakePage({
            "role:button:Submit:True": [_candidate(preview_text="Submit", visible=False, enabled=True)],
        })
        with pytest.raises(LocatorResolutionError) as exc_info:
            resolve_semantic_locator(page, 'button="Submit"', require_visible=True, require_enabled=True)
        assert exc_info.value.trace is not None
        assert exc_info.value.trace.failure_reason == "Locator candidates matched target but none are visible."
        assert exc_info.value.trace.candidates[0].rejected_reasons == ["element-not-visible"]

    def test_enabled_failure(self):
        """Disabled elements should be rejected when require_enabled=True."""
        page = FakePage({
            "role:textbox:Username:True": [_candidate(preview_text="Username", visible=True, enabled=False, role="textbox")],
        })
        with pytest.raises(LocatorResolutionError) as exc_info:
            resolve_semantic_locator(page, 'textbox="Username"', prefer_input=True, require_visible=True, require_enabled=True)
        assert exc_info.value.trace is not None
        assert exc_info.value.trace.failure_reason == "Locator candidates matched target but none are enabled."
        assert exc_info.value.trace.candidates[0].rejected_reasons == ["element-not-enabled"]


class TestExplicitSelectors:
    """Test explicit CSS/XPath selectors."""

    def test_css_selector(self):
        """CSS selector should work."""
        page = FakePage({
            "locator:#login-btn": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        resolved = resolve_semantic_locator(page, "#login-btn")
        assert resolved.strategy == "css"

    def test_xpath_selector(self):
        """XPath selector should work."""
        page = FakePage({
            "locator:xpath=//div[@id='main']": [_candidate(preview_text="Main", visible=True, enabled=True)],
        })
        resolved = resolve_semantic_locator(page, "//div[@id='main']")
        assert resolved.strategy == "xpath"

    def test_data_testid(self):
        """data-testid selector should work."""
        page = FakePage({
            "testid:login-btn": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        resolved = resolve_semantic_locator(page, "data-testid=login-btn")
        assert resolved.strategy == "data-testid"


class TestTargetStrategyOverride:
    """target_strategy parameter should bypass heuristics."""

    def test_target_strategy_css(self):
        page = FakePage({
            "locator:my-custom-selector": [_candidate(preview_text="Found", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "my-custom-selector", target_strategy="css")
        assert result.strategy == "css"

    def test_target_strategy_xpath(self):
        page = FakePage({
            "locator://div[@id='main']": [_candidate(preview_text="Main", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "xpath=//div[@id='main']", target_strategy="xpath")
        assert result.strategy == "xpath"

    def test_target_strategy_element_id(self):
        page = FakePage({
            "locator:#my-field": [_candidate(preview_text="Field", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "my-field", target_strategy="element_id")
        assert result.strategy == "element_id"

    def test_target_strategy_css_zero_matches_falls_through(self):
        """target_strategy='css' with 0 CSS matches should fall through to a11y scan."""
        page = FakePage({
            "role:button:Submit:True": [_candidate(preview_text="Submit", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, 'button="Submit"', target_strategy="css")
        assert result.strategy == "a11y_role_exact"


class TestScorePriority:
    """Test that higher-scored strategies are preferred."""

    def test_a11y_role_exact_higher_than_text(self):
        """a11y_role_exact (120) should outrank a11y_text_exact (80)."""
        page = FakePage({
            "role:button:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
            "text:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        resolved = resolve_semantic_locator(page, 'button="Login"', require_visible=True, require_enabled=True)
        assert resolved.strategy == "a11y_role_exact"
