"""Tests for AI visual locator helpers."""

from __future__ import annotations

from urllib import error

from app.core.config import get_settings
from app.locators.ai_visual import (
    AILocateResult,
    _normalize_bbox,
    locate_element_by_vision,
    reset_ai_visual_runtime_state,
)
from app.locators.fallback import _build_locator_from_ai_point, _dom_snapshot_matches_target
from app.schemas.executions import DOMElementSnapshot


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

    assert _normalize_bbox(
        bbox=[100, 200, 400, 600],
        image_width=1000,
        image_height=500,
        model_family="qwen-vl",
    ) == (100, 100, 400, 300)


def test_locate_element_by_vision_skips_when_model_not_configured(monkeypatch) -> None:
    monkeypatch.delenv("ENABLE_AI_VISUAL_LOCATE", raising=False)
    monkeypatch.delenv("VLM_API_KEY", raising=False)
    monkeypatch.delenv("VLM_MODEL", raising=False)
    get_settings.cache_clear()
    reset_ai_visual_runtime_state()
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
        reset_ai_visual_runtime_state()
        get_settings.cache_clear()


def test_locate_element_by_vision_rate_limits_after_configured_budget(monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_VISUAL_LOCATE", "true")
    monkeypatch.setenv("VLM_API_KEY", "test-key")
    monkeypatch.setenv("VLM_MODEL", "gpt-4o-mini")
    monkeypatch.setenv("AI_VISUAL_RATE_LIMIT_PER_MINUTE", "1")
    get_settings.cache_clear()
    reset_ai_visual_runtime_state()
    call_count = {"count": 0}

    monkeypatch.setattr(
        "app.locators.ai_visual._call_vlm",
        lambda **_kwargs: call_count.__setitem__("count", call_count["count"] + 1) or '{"bbox":[100,200,400,600]}',
    )

    try:
        first = locate_element_by_vision(
            screenshot_base64="ZmFrZQ==",
            target_description="登录按钮",
            image_width=1000,
            image_height=500,
        )
        second = locate_element_by_vision(
            screenshot_base64="ZmFrZQ==",
            target_description="登录按钮",
            image_width=1000,
            image_height=500,
        )
    finally:
        reset_ai_visual_runtime_state()
        get_settings.cache_clear()

    assert first is not None
    assert second is None
    assert call_count["count"] == 1


def test_locate_element_by_vision_opens_breaker_after_consecutive_failures(monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_VISUAL_LOCATE", "true")
    monkeypatch.setenv("VLM_API_KEY", "test-key")
    monkeypatch.setenv("VLM_MODEL", "gpt-4o-mini")
    monkeypatch.setenv("AI_VISUAL_FAILURE_THRESHOLD", "2")
    monkeypatch.setenv("AI_VISUAL_COOLDOWN_SECONDS", "60")
    get_settings.cache_clear()
    reset_ai_visual_runtime_state()
    call_count = {"count": 0}

    def fake_call_vlm(**_kwargs):
        call_count["count"] += 1
        raise error.URLError("provider boom")

    monotonic_values = iter([1.0, 1.0, 2.0, 2.0, 3.0, 4.0])
    monkeypatch.setattr("app.locators.ai_visual._call_vlm", fake_call_vlm)
    monkeypatch.setattr("app.locators.ai_visual.monotonic", lambda: next(monotonic_values))

    try:
        first = locate_element_by_vision(
            screenshot_base64="ZmFrZQ==",
            target_description="登录按钮",
            image_width=1000,
            image_height=500,
        )
        second = locate_element_by_vision(
            screenshot_base64="ZmFrZQ==",
            target_description="登录按钮",
            image_width=1000,
            image_height=500,
        )
        third = locate_element_by_vision(
            screenshot_base64="ZmFrZQ==",
            target_description="登录按钮",
            image_width=1000,
            image_height=500,
        )
    finally:
        reset_ai_visual_runtime_state()
        get_settings.cache_clear()

    assert first is None
    assert second is None
    assert third is None
    assert call_count["count"] == 2


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


def test_dom_snapshot_target_matching_uses_whole_tokens() -> None:
    snapshot = DOMElementSnapshot(
        tag="button",
        text="Booking",
        role="button",
        aria_label=None,
        placeholder=None,
        data_testid="booking-submit",
        css_selector="#booking-submit",
        xpath="/html/body/button[1]",
        rect={"x": 0, "y": 0, "width": 10, "height": 10},
        visible=True,
        enabled=True,
    )

    assert _dom_snapshot_matches_target(snapshot, "booking")
    assert not _dom_snapshot_matches_target(snapshot, "ok")
