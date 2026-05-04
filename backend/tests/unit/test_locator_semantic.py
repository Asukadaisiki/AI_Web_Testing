"""Tests for semantic locator resolution."""

from __future__ import annotations

import pytest

from app.locators import LocatorResolutionError, resolve_semantic_locator


class FakeNodeLocator:
    def __init__(self, payload: dict) -> None:
        self.payload = payload

    def evaluate(self, _script: str):
        return self.payload

    def get_by_text(self, text: str, exact: bool = True):
        key = f"chained:{self.payload.get('_css', '')}:text:{text}:{exact}"
        return FakeLocatorCollection(self.payload.get("_chained_map", {}).get(key, []))


class FakeLocatorCollection:
    def __init__(self, payloads: list[dict]) -> None:
        self.payloads = payloads

    def count(self) -> int:
        return len(self.payloads)

    def nth(self, index: int) -> FakeNodeLocator:
        return FakeNodeLocator(self.payloads[index])

    def get_by_text(self, text: str, exact: bool = True):
        """Support chained selector: locator(css).get_by_text(text)."""
        results = []
        for p in self.payloads:
            chained_map = p.get("_chained_map", {})
            key = f"chained:{p.get('_css', '')}:text:{text}:{exact}"
            results.extend(chained_map.get(key, []))
        return FakeLocatorCollection(results)


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

    def get_by_text(self, target: str, exact: bool = False):
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


def test_resolve_semantic_locator_returns_trace_with_candidates() -> None:
    page = FakePage(
        {
            "role:button:登录按钮:True": [_candidate(preview_text="登录", visible=True, enabled=True)],
        }
    )

    resolved = resolve_semantic_locator(page, "登录按钮", require_visible=True, require_enabled=True)

    assert resolved.strategy == "button_role"
    assert resolved.trace.match_strategy == "button_role"
    assert resolved.trace.selection_reason is not None
    assert resolved.trace.selected_candidate is not None
    assert resolved.trace.selected_candidate.preview_text == "登录"
    assert resolved.trace.selected_candidate.score > 0
    assert "exact-button-role-match" in resolved.trace.selected_candidate.matched_rules
    assert resolved.trace.selected_candidate.rejected_reasons == []


def test_resolve_semantic_locator_reports_visibility_failure() -> None:
    page = FakePage(
        {
            "role:button:提交按钮:True": [_candidate(preview_text="提交", visible=False, enabled=True)],
        }
    )

    with pytest.raises(LocatorResolutionError) as exc_info:
        resolve_semantic_locator(page, "提交按钮", require_visible=True, require_enabled=True)

    assert exc_info.value.trace is not None
    assert exc_info.value.trace.failure_reason == "Locator candidates matched target but none are visible."
    assert exc_info.value.trace.candidates[0].enabled is True
    assert exc_info.value.trace.candidates[0].rejected_reasons == ["element-not-visible"]


def test_resolve_semantic_locator_prefers_highest_scoring_candidate() -> None:
    page = FakePage(
        {
            "text:登录:True": [_candidate(preview_text="登录", visible=True, enabled=True)],
            "role:button:登录:True": [_candidate(preview_text="登录", visible=True, enabled=True)],
        }
    )

    resolved = resolve_semantic_locator(page, "登录", require_visible=True, require_enabled=True)

    assert resolved.strategy == "button_role"
    assert resolved.trace.selected_candidate is not None
    assert resolved.trace.selected_candidate.score >= resolved.trace.candidates[-1].score


def test_resolve_semantic_locator_records_disabled_rejection() -> None:
    page = FakePage(
        {
            "label:用户名:True": [_candidate(preview_text="用户名", visible=True, enabled=False, role="input")],
        }
    )

    with pytest.raises(LocatorResolutionError) as exc_info:
        resolve_semantic_locator(page, "用户名", prefer_input=True, require_visible=True, require_enabled=True)

    assert exc_info.value.trace is not None
    assert exc_info.value.trace.failure_reason == "Locator candidates matched target but none are enabled."
    assert exc_info.value.trace.candidates[0].rejected_reasons == ["element-not-enabled"]


class TestCompoundCssSelector:
    """BUG-049: 复合 CSS 选择器（如 button[type='submit']）应被识别为 CSS 策略。"""

    def test_tag_with_attribute_selector(self):
        """button[type='submit'] 应被解析为 CSS，而非文本匹配。"""
        page = FakePage({
            "locator:button[type='submit']": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "button[type='submit']")
        assert result.strategy == "css"

    def test_tag_child_selector(self):
        """'form button' 应被解析为 CSS。"""
        page = FakePage({
            "locator:form button": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "form button")
        assert result.strategy == "css"

    def test_tag_with_class_selector(self):
        """'div.container' 应被解析为 CSS。"""
        page = FakePage({
            "locator:div.container": [_candidate(preview_text="content", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "div.container")
        assert result.strategy == "css"

    def test_tag_direct_child_selector(self):
        """'form > button' 应被解析为 CSS。"""
        page = FakePage({
            "locator:form > button": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "form > button")
        assert result.strategy == "css"

    def test_plain_text_not_treated_as_css(self):
        """'Login' 不应被解析为 CSS。"""
        page = FakePage({
            "text:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "Login")
        assert result.strategy != "css"

    def test_single_tag_not_treated_as_css(self):
        """'button'（裸标签名）不应被解析为 css，但应被识别为 css_tag。"""
        page = FakePage({
            "locator:button": [_candidate(preview_text="Click Me", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "button")
        assert result.strategy == "css_tag"


class TestBareHtmlTagRecognition:
    """裸 HTML 标签名应被识别为 css_tag 策略。"""

    def test_body_tag_recognized(self):
        page = FakePage({
            "locator:body": [_candidate(preview_text="Page content", visible=True, enabled=True, role="body")],
        })
        result = resolve_semantic_locator(page, "body")
        assert result.strategy == "css_tag"
        assert result.trace.match_strategy == "css_tag"

    def test_form_tag_recognized(self):
        page = FakePage({
            "locator:form": [_candidate(preview_text="Login Form", visible=True, enabled=True, role="form")],
        })
        result = resolve_semantic_locator(page, "form")
        assert result.strategy == "css_tag"

    def test_non_tag_word_not_treated_as_tag(self):
        """非标签名单词不应走 css_tag。"""
        page = FakePage({
            "text:foobar:True": [_candidate(preview_text="foobar", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "foobar")
        assert result.strategy != "css_tag"

    def test_tag_not_in_set_falls_through(self):
        """不在 _HTML_TAG_NAMES 集合中的裸词应走语义匹配。"""
        page = FakePage({
            "text:customtag:True": [_candidate(preview_text="customtag", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "customtag")
        assert result.strategy != "css_tag"


class TestTargetStrategyOverride:
    """target_strategy 参数应绕过启发式，直接使用指定策略。"""

    def test_target_strategy_css(self):
        """target_strategy='css' 应直接将 target 当作 CSS 选择器。"""
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

    def test_target_strategy_tag(self):
        page = FakePage({
            "locator:body": [_candidate(preview_text="Page", visible=True, enabled=True, role="body")],
        })
        result = resolve_semantic_locator(page, "body", target_strategy="tag")
        assert result.strategy == "css_tag"

    def test_target_strategy_semantic_falls_through(self):
        """target_strategy='semantic' 应走正常启发式路径。"""
        page = FakePage({
            "text:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "Login", target_strategy="semantic")
        assert result.strategy == "text"

    def test_target_strategy_none_uses_heuristic(self):
        """target_strategy=None 应使用默认启发式。"""
        page = FakePage({
            "text:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "Login", target_strategy=None)
        assert result.strategy == "text"

    def test_target_strategy_unknown_falls_through_to_semantic(self):
        """Unknown target_strategy falls through to semantic scan, which still fails with no candidates."""
        page = FakePage({})
        with pytest.raises(LocatorResolutionError):
            resolve_semantic_locator(page, "anything", target_strategy="unknown_strategy")

    def test_target_strategy_element_id(self):
        page = FakePage({
            "locator:#my-field": [_candidate(preview_text="Field", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "my-field", target_strategy="element_id")
        assert result.strategy == "element_id"

    def test_target_strategy_preference_fallback_to_semantic(self):
        """When hinted strategy finds 0 matches, semantic scan should be tried as fallback."""
        page = FakePage({
            # CSS strategy will find nothing for "Login" (no locator:Login key)
            # But semantic scan will find a text match
            "text:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "Login", target_strategy="css")
        assert result.strategy == "text"

    def test_target_strategy_css_zero_matches_falls_through(self):
        """target_strategy='css' with 0 CSS matches should fall through to semantic scan."""
        page = FakePage({
            "role:button:Submit:True": [_candidate(preview_text="Submit", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "Submit", target_strategy="css")
        assert result.strategy == "button_role"


class TestChainedSelector:
    """Playwright-style chained selectors: '.class text=Value'."""

    @staticmethod
    def _chained_page():
        """FakePage where .productinfo locator chains to get_by_text('View Product')."""
        inner = _candidate(preview_text="View Product", visible=True, enabled=True, role="a")
        inner["_css"] = ".productinfo"
        inner["_chained_map"] = {
            "chained:.productinfo:text:View Product:True": [inner],
        }
        return FakePage({
            "locator:.productinfo": [inner],
        })

    def test_dot_class_text_equals_value(self):
        """'.productinfo text=View Product' → chained_css_text strategy."""
        page = self._chained_page()
        result = resolve_semantic_locator(page, ".productinfo text=View Product")
        assert result.strategy == "chained_css_text"

    def test_dot_class_text_quoted_value(self):
        """'.productinfo text='View Product'' → chained_css_text strategy."""
        page = self._chained_page()
        result = resolve_semantic_locator(page, ".productinfo text='View Product'")
        assert result.strategy == "chained_css_text"

    def test_dot_class_double_arrow_text(self):
        """'.productinfo >> text=View Product' → chained_css_text strategy."""
        page = self._chained_page()
        result = resolve_semantic_locator(page, ".productinfo >> text=View Product")
        assert result.strategy == "chained_css_text"

    def test_hash_id_text_value(self):
        """'#submit-btn text=Go' → chained_css_text strategy."""
        inner = _candidate(preview_text="Go", visible=True, enabled=True)
        inner["_css"] = "#submit-btn"
        inner["_chained_map"] = {
            "chained:#submit-btn:text:Go:True": [inner],
        }
        page = FakePage({
            "locator:#submit-btn": [inner],
        })
        result = resolve_semantic_locator(page, "#submit-btn text=Go")
        assert result.strategy == "chained_css_text"

    def test_tag_class_text_value(self):
        """'div.productinfo text=View Product' → chained_css_text."""
        inner = _candidate(preview_text="View Product", visible=True, enabled=True, role="a")
        inner["_css"] = "div.productinfo"
        inner["_chained_map"] = {
            "chained:div.productinfo:text:View Product:True": [inner],
        }
        page = FakePage({
            "locator:div.productinfo": [inner],
        })
        result = resolve_semantic_locator(page, "div.productinfo text=View Product")
        assert result.strategy == "chained_css_text"

    def test_plain_class_without_text_not_chained(self):
        """'.productinfo' alone should be plain CSS, not chained."""
        page = FakePage({
            "locator:.productinfo": [_candidate(preview_text="Item", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, ".productinfo")
        assert result.strategy == "css"

    def test_chained_score_higher_than_fuzzy(self):
        """Chained selector score (110) should be higher than fuzzy matches."""
        page = self._chained_page()
        result = resolve_semantic_locator(page, ".productinfo text='View Product'")
        assert result.trace.selected_candidate is not None
        assert result.trace.selected_candidate.score >= 110


class TestLinkRoleStrategy:
    """Tests for link_role and menuitem_role strategies (Phase 1: ARIA role expansion)."""

    def test_link_role_preferred_over_text(self):
        """link_role (score 85) should outrank text_fuzzy (score 50) for <a> tags."""
        page = FakePage(
            {
                "role:link:Login:True": [
                    _candidate(preview_text="Signup / Login", visible=True, enabled=True, role="link")
                ],
                "text:Login:False": [
                    _candidate(preview_text="Signup / Login", visible=True, enabled=True, role="link")
                ],
            }
        )
        resolved = resolve_semantic_locator(page, "Login", require_visible=True, require_enabled=True)
        assert resolved.strategy == "link_role"

    def test_menuitem_role_matches_target(self):
        """menuitem_role exact match should be selected when available."""
        page = FakePage(
            {
                "role:menuitem:Profile:True": [
                    _candidate(preview_text="Profile", visible=True, enabled=True, role="menuitem")
                ],
                "text:Profile:True": [
                    _candidate(preview_text="Profile", visible=True, enabled=True, role="menuitem")
                ],
            }
        )
        resolved = resolve_semantic_locator(page, "Profile", require_visible=True, require_enabled=True)
        assert resolved.strategy == "menuitem_role"

    def test_role_strategies_skip_when_no_match(self):
        """When link_role/menuitem_role have 0 matches, should fall to text_fuzzy."""
        page = FakePage(
            {
                "text:Logout:False": [
                    _candidate(preview_text="Logout", visible=True, enabled=True, role="link")
                ],
            }
        )
        resolved = resolve_semantic_locator(page, "Logout", require_visible=True, require_enabled=True)
        assert resolved.strategy == "text_fuzzy"
