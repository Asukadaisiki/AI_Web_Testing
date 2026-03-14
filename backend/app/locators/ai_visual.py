"""Optional AI visual locator."""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Literal
from urllib import error, request

from app.core.config import get_settings


ModelFamily = Literal["qwen-vl", "gemini", "gpt-4o", "qwen2.5-vl"]

SYSTEM_PROMPT = """You are an AI assistant that locates a UI element in a screenshot.
Return JSON only in the shape {"bbox":[xmin,ymin,xmax,ymax],"errors":["..."]?}."""


@dataclass(frozen=True)
class AILocateResult:
    center: tuple[int, int]
    bbox: tuple[int, int, int, int]
    confidence: float
    raw_response: str


def locate_element_by_vision(
    *,
    screenshot_base64: str,
    target_description: str,
    image_width: int,
    image_height: int,
    model_family: ModelFamily | None = None,
    deep_locate: bool = False,
) -> AILocateResult | None:
    settings = get_settings()
    if not settings.enable_ai_visual_locate:
        return None
    if not settings.vlm_api_key or not settings.vlm_model:
        return None

    family = model_family or settings.vlm_model_family
    response_text = _call_vlm(
        screenshot_base64=screenshot_base64,
        target_description=target_description,
        api_key=settings.vlm_api_key,
        model=settings.vlm_model,
        base_url=settings.vlm_base_url,
    )
    return _parse_bbox_response(
        response_text=response_text,
        image_width=image_width,
        image_height=image_height,
        model_family=family,
    )


def _call_vlm(
    *,
    screenshot_base64: str,
    target_description: str,
    api_key: str,
    model: str,
    base_url: str,
) -> str:
    payload = {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {
                "role": "user",
                "content": [
                    {
                        "type": "image_url",
                        "image_url": {"url": f"data:image/png;base64,{screenshot_base64}", "detail": "high"},
                    },
                    {"type": "text", "text": f"Find: {target_description}"},
                ],
            },
        ],
        "response_format": {"type": "json_object"},
    }
    endpoint = f"{base_url.rstrip('/')}/chat/completions"
    http_request = request.Request(
        endpoint,
        data=json.dumps(payload).encode("utf-8"),
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with request.urlopen(http_request, timeout=30) as response:
            raw_payload = json.loads(response.read().decode("utf-8"))
    except error.URLError:
        return ""

    return (
        raw_payload.get("choices", [{}])[0]
        .get("message", {})
        .get("content", "")
    )


def _parse_bbox_response(
    *,
    response_text: str,
    image_width: int,
    image_height: int,
    model_family: ModelFamily,
) -> AILocateResult | None:
    if not response_text:
        return None
    try:
        payload = json.loads(_extract_json_object(response_text))
    except json.JSONDecodeError:
        return None

    bbox = payload.get("bbox")
    if not isinstance(bbox, list) or len(bbox) != 4:
        return None
    normalized_bbox = _normalize_bbox(
        bbox=bbox,
        image_width=image_width,
        image_height=image_height,
        model_family=model_family,
    )
    xmin, ymin, xmax, ymax = normalized_bbox
    center = ((xmin + xmax) // 2, (ymin + ymax) // 2)
    errors = payload.get("errors") or []
    confidence = 0.35 if errors else 0.7
    return AILocateResult(
        center=center,
        bbox=normalized_bbox,
        confidence=confidence,
        raw_response=response_text,
    )


def _extract_json_object(response_text: str) -> str:
    stripped = response_text.strip()
    if stripped.startswith("{") and stripped.endswith("}"):
        return stripped
    start = stripped.find("{")
    end = stripped.rfind("}")
    if start == -1 or end == -1 or end <= start:
        return stripped
    return stripped[start : end + 1]


def _normalize_bbox(
    *,
    bbox: list[Any],
    image_width: int,
    image_height: int,
    model_family: ModelFamily,
) -> tuple[int, int, int, int]:
    numbers = [float(value) for value in bbox]
    if model_family == "gemini":
        ymin, xmin, ymax, xmax = numbers
        numbers = [xmin, ymin, xmax, ymax]
    if model_family != "qwen2.5-vl":
        xmin, ymin, xmax, ymax = numbers
        numbers = [
            xmin / 1000 * image_width,
            ymin / 1000 * image_height,
            xmax / 1000 * image_width,
            ymax / 1000 * image_height,
        ]

    xmin, ymin, xmax, ymax = numbers
    return (
        max(0, min(image_width, int(round(xmin)))),
        max(0, min(image_height, int(round(ymin)))),
        max(0, min(image_width, int(round(xmax)))),
        max(0, min(image_height, int(round(ymax)))),
    )


__all__ = [
    "AILocateResult",
    "ModelFamily",
    "locate_element_by_vision",
    "_normalize_bbox",
]
