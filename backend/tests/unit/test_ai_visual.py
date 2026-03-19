"""Tests for AI visual locator helpers."""

from __future__ import annotations

import threading
from urllib import error

from app.core.config import get_settings
from app.locators.ai_visual import (
    AILocateResult,
    RUNTIME_STATE,
    _decode_base64_image,
    _deep_locate,
    _extract_json_object,
    _normalize_bbox,
    _parse_bbox_response,
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


def test_decode_base64_image_raises_clear_error_for_invalid_payload() -> None:
    try:
        _decode_base64_image("not-base64!!")
    except ValueError as exc:
        assert str(exc) == "Invalid base64 image payload."
    else:  # pragma: no cover - defensive assertion
        raise AssertionError("Expected invalid base64 payload to raise ValueError.")


def test_deep_locate_respects_total_timeout_budget(monkeypatch) -> None:
    monotonic_values = iter([100.0, 100.0, 100.8, 101.2])
    section_calls: list[float] = []
    single_calls: list[float] = []

    monkeypatch.setattr("app.locators.ai_visual.monotonic", lambda: next(monotonic_values))
    monkeypatch.setattr(
        "app.locators.ai_visual._locate_section",
        lambda **kwargs: section_calls.append(kwargs["timeout_seconds"])
        or AILocateResult(center=(100, 60), bbox=(50, 20, 150, 100), confidence=0.4, raw_response="section"),
    )
    monkeypatch.setattr(
        "app.locators.ai_visual._crop_and_scale",
        lambda **_kwargs: ("ZmFrZQ==", (10, 20)),
    )
    monkeypatch.setattr(
        "app.locators.ai_visual._single_locate",
        lambda **kwargs: single_calls.append(kwargs["timeout_seconds"])
        or AILocateResult(center=(40, 20), bbox=(20, 10, 60, 30), confidence=0.7, raw_response="local"),
    )

    result = _deep_locate(
        screenshot_base64="ZmFrZQ==",
        target_description="登录按钮",
        image_width=800,
        image_height=600,
        model_family="gpt-4o",
        api_key="test-key",
        model="gpt-4o-mini",
        base_url="https://api.openai.com/v1",
        timeout_seconds=1.0,
    )

    assert result is not None
    assert len(section_calls) == 1
    assert len(single_calls) == 1
    assert section_calls[0] <= 1.0
    assert single_calls[0] <= 0.2 + 1e-9
    assert result.center == (30, 30)


# --- _extract_json_object tests ---


def test_extract_json_object_clean_json() -> None:
    assert _extract_json_object('{"bbox":[1,2,3,4]}') == '{"bbox":[1,2,3,4]}'


def test_extract_json_object_markdown_wrapped() -> None:
    text = '```json\n{"bbox":[1,2,3,4]}\n```'
    assert _extract_json_object(text) == '{"bbox":[1,2,3,4]}'


def test_extract_json_object_markdown_without_language_tag() -> None:
    text = '```\n{"bbox":[1,2,3,4]}\n```'
    assert _extract_json_object(text) == '{"bbox":[1,2,3,4]}'


def test_extract_json_object_leading_trailing_commentary() -> None:
    text = 'Here is the result: {"bbox":[1,2,3,4]}. Done.'
    assert _extract_json_object(text) == '{"bbox":[1,2,3,4]}'


def test_extract_json_object_nested_braces_in_string_values() -> None:
    text = '{"bbox":[1,2,3,4],"note":"use {css} selector"}'
    import json
    parsed = json.loads(_extract_json_object(text))
    assert parsed["bbox"] == [1, 2, 3, 4]
    assert parsed["note"] == "use {css} selector"


def test_extract_json_object_multiple_objects_returns_first() -> None:
    text = '{"bbox":[1,2,3,4]} and also {"other":"data"}'
    import json
    parsed = json.loads(_extract_json_object(text))
    assert parsed["bbox"] == [1, 2, 3, 4]


def test_extract_json_object_no_valid_json_returns_original() -> None:
    text = "No JSON here"
    assert _extract_json_object(text) == text


def test_extract_json_object_empty_string() -> None:
    assert _extract_json_object("") == ""
    assert _extract_json_object("   ") == ""


def test_extract_json_object_escaped_quotes_in_strings() -> None:
    text = r'{"key":"value with \"escaped\" quotes"}'
    import json
    parsed = json.loads(_extract_json_object(text))
    assert parsed["key"] == 'value with "escaped" quotes'


# --- _parse_bbox_response edge cases ---


def test_parse_bbox_response_returns_none_for_empty_text() -> None:
    assert _parse_bbox_response(response_text="", image_width=1000, image_height=500, model_family="gpt-4o") is None


def test_parse_bbox_response_returns_none_for_invalid_json() -> None:
    assert _parse_bbox_response(response_text="not json", image_width=1000, image_height=500, model_family="gpt-4o") is None


def test_parse_bbox_response_returns_none_for_missing_bbox() -> None:
    assert _parse_bbox_response(response_text='{"errors":["no match"]}', image_width=1000, image_height=500, model_family="gpt-4o") is None


def test_parse_bbox_response_returns_none_for_short_bbox() -> None:
    assert _parse_bbox_response(response_text='{"bbox":[1,2]}', image_width=1000, image_height=500, model_family="gpt-4o") is None


def test_parse_bbox_response_returns_none_for_non_list_bbox() -> None:
    assert _parse_bbox_response(response_text='{"bbox":"not a list"}', image_width=1000, image_height=500, model_family="gpt-4o") is None


def test_parse_bbox_response_reduces_confidence_when_errors_present() -> None:
    result = _parse_bbox_response(
        response_text='{"bbox":[100,200,400,600],"errors":["uncertain"]}',
        image_width=1000,
        image_height=500,
        model_family="gpt-4o",
    )
    assert result is not None
    assert result.confidence == 0.35


def test_parse_bbox_response_full_confidence_without_errors() -> None:
    result = _parse_bbox_response(
        response_text='{"bbox":[100,200,400,600]}',
        image_width=1000,
        image_height=500,
        model_family="gpt-4o",
    )
    assert result is not None
    assert result.confidence == 0.7


# --- reset_ai_visual_runtime_state ---


def test_reset_clears_all_runtime_state_fields() -> None:
    RUNTIME_STATE.window_started_at = 100.0
    RUNTIME_STATE.window_request_count = 42
    RUNTIME_STATE.consecutive_failures = 5
    RUNTIME_STATE.opened_until = 999.0

    reset_ai_visual_runtime_state()

    assert RUNTIME_STATE.window_started_at == 0.0
    assert RUNTIME_STATE.window_request_count == 0
    assert RUNTIME_STATE.consecutive_failures == 0
    assert RUNTIME_STATE.opened_until == 0.0


# --- Thread safety ---


def test_concurrent_rate_limit_does_not_exceed_budget(monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_VISUAL_LOCATE", "true")
    monkeypatch.setenv("VLM_API_KEY", "test-key")
    monkeypatch.setenv("VLM_MODEL", "gpt-4o-mini")
    monkeypatch.setenv("AI_VISUAL_RATE_LIMIT_PER_MINUTE", "3")
    get_settings.cache_clear()
    reset_ai_visual_runtime_state()

    call_count = {"count": 0}
    call_lock = threading.Lock()

    def fake_call_vlm(**_kwargs):
        with call_lock:
            call_count["count"] += 1
        return '{"bbox":[100,200,400,600]}'

    monkeypatch.setattr("app.locators.ai_visual._call_vlm", fake_call_vlm)

    results: list[object] = [None] * 10
    threads = []

    def worker(idx: int):
        results[idx] = locate_element_by_vision(
            screenshot_base64="ZmFrZQ==",
            target_description="button",
            image_width=1000,
            image_height=500,
        )

    try:
        for i in range(10):
            t = threading.Thread(target=worker, args=(i,))
            threads.append(t)
            t.start()
        for t in threads:
            t.join()
    finally:
        reset_ai_visual_runtime_state()
        get_settings.cache_clear()

    successful = sum(1 for r in results if r is not None)
    assert successful <= 3
    assert call_count["count"] <= 3
