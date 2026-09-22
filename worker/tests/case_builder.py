"""构造 case 工件的小工具（测试专用，不含任何任务专有名词）。

`grounding` 的取值只用于满足契约形状；worker 执行期只用 `locator`（CONTRACT §3.1：
执行期只使用 case 里已经记录的那一条定位器）。
"""

from __future__ import annotations

from typing import Any

CASE_VERSION = "loop.case.v1"


def grounding(
    page_url: str,
    *,
    observation_id: str = "obs_test",
    page_state_id: str = "ps_test",
    candidate_id: str = "e0:0",
) -> dict[str, str]:
    return {
        "observation_id": observation_id,
        "page_state_id": page_state_id,
        "candidate_id": candidate_id,
        "page_url": page_url,
    }


def role_locator(role: str, name: str) -> dict[str, Any]:
    return {"kind": "role", "role": role, "name": name, "exact": True}


def text_locator(text: str) -> dict[str, Any]:
    return {"kind": "text", "text": text, "exact": True}


def css_locator(css: str) -> dict[str, Any]:
    return {"kind": "css", "css": css}


def target(locator: dict[str, Any], page_url: str, hint: str = "target") -> dict[str, Any]:
    return {"hint": hint, "locator": locator, "grounding": grounding(page_url)}


def condition(type_: str, value: str, timeout_ms: int = 3000) -> dict[str, Any]:
    return {"type": type_, "value": value, "timeout_ms": timeout_ms}


def goto_step(index: int, url: str, contains: str, timeout_ms: int = 5000) -> dict[str, Any]:
    return {
        "index": index,
        "action": "goto",
        "intent": f"open {url}",
        "value": url,
        "preconditions": [],
        "postconditions": [condition("url_contains", contains)],
        "timeout_ms": timeout_ms,
    }


def action_step(
    index: int,
    action: str,
    *,
    locator: dict[str, Any] | None = None,
    page_url: str,
    pre: list[dict[str, Any]],
    post: list[dict[str, Any]],
    value: str | None = None,
    timeout_ms: int = 5000,
) -> dict[str, Any]:
    step: dict[str, Any] = {
        "index": index,
        "action": action,
        "intent": f"{action} step",
        "preconditions": pre,
        "postconditions": post,
        "timeout_ms": timeout_ms,
    }
    if locator is not None:
        step["target"] = target(locator, page_url)
    if value is not None:
        step["value"] = value
    return step


def build_case(
    steps: list[dict[str, Any]],
    *,
    name: str = "local fixture case",
    base_url: str = "http://127.0.0.1",
) -> dict[str, Any]:
    return {
        "case_version": CASE_VERSION,
        "name": name,
        "goal": "exercise the local static fixture",
        "base_url": base_url,
        "steps": steps,
    }


__all__ = [
    "CASE_VERSION",
    "action_step",
    "build_case",
    "condition",
    "css_locator",
    "goto_step",
    "grounding",
    "role_locator",
    "target",
    "text_locator",
]
