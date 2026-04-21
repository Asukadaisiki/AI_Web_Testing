"""Tests for semantic locator resolution."""

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
        """'button'（裸标签名）不应被解析为 CSS，应走文本匹配。"""
        page = FakePage({
            "text:button:True": [_candidate(preview_text="button", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "button")
        assert result.strategy != "css"
