"""Tests for AI visual locator helpers."""

from __future__ import annotations

from app.core.config import get_settings
from app.locators.ai_visual import AILocateResult, _normalize_bbox, locate_element_by_vision
from app.locators.fallback import _build_locator_from_ai_point


class FakePage:
    def __init__(self, payload: dict | None) -> None:
        self.payload = payload
        self.locator_calls: list[str] = []

    def evaluate(self, _script: str, _args):
        return self.payload

    def locator(self, selector: str):
        self.locator_calls.append(selector)
        return {"selector": selector}


def test_normalize_bbox_supports_multiple_model_families() -> None:
    assert _normalize_bbox(
        bbox=[100, 200, 400, 600],
        image_width=1000,
        image_height=500,
        model_family="gpt-4o",
    ) == (100, 100, 400, 300)

    assert _normalize_bbox(
        bbox=[200, 100, 600, 400],
        image_width=1000,
        image_height=500,
        model_family="gemini",
    ) == (100, 100, 400, 300)

    assert _normalize_bbox(
        bbox=[10, 20, 30, 40],
        image_width=1000,
        image_height=500,
        model_family="qwen2.5-vl",
    ) == (10, 20, 30, 40)


def test_locate_element_by_vision_skips_when_model_not_configured(monkeypatch) -> None:
    monkeypatch.delenv("ENABLE_AI_VISUAL_LOCATE", raising=False)
    monkeypatch.delenv("VLM_API_KEY", raising=False)
    monkeypatch.delenv("VLM_MODEL", raising=False)
    get_settings.cache_clear()
    try:
        assert (
            locate_element_by_vision(
                screenshot_base64="ZmFrZQ==",
                target_description="登录按钮",
                image_width=1280,
                image_height=720,
            )
            is None
        )
    finally:
        get_settings.cache_clear()


def test_build_locator_from_ai_point_requires_dom_cross_verification() -> None:
    ai_candidate = AILocateResult(center=(200, 100), bbox=(150, 80, 250, 120), confidence=0.7, raw_response="{}")
    matching_page = FakePage(
        {
            "tag": "button",
            "text": "登录按钮",
            "role": "button",
            "aria_label": "登录按钮",
            "placeholder": None,
            "data_testid": None,
            "css_selector": "#login-btn",
            "xpath": "/html/body/button[1]",
            "rect": {"x": 150, "y": 80, "width": 100, "height": 40},
            "visible": True,
            "enabled": True,
        }
    )
    mismatching_page = FakePage(
        {
            "tag": "button",
            "text": "提交",
            "role": "button",
            "aria_label": "提交",
            "placeholder": None,
            "data_testid": None,
            "css_selector": "#submit-btn",
            "xpath": "/html/body/button[2]",
            "rect": {"x": 150, "y": 80, "width": 100, "height": 40},
            "visible": True,
            "enabled": True,
        }
    )

    resolved = _build_locator_from_ai_point(matching_page, target="登录按钮", ai_candidate=ai_candidate)
    assert resolved is not None
    assert resolved.strategy == "ai_visual"
    assert matching_page.locator_calls == ["#login-btn"]

    assert _build_locator_from_ai_point(mismatching_page, target="登录按钮", ai_candidate=ai_candidate) is None
