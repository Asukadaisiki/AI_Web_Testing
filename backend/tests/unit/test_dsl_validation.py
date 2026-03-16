"""Tests for DSL validation endpoint."""

from __future__ import annotations

from app.ai.dsl_generator import build_generation_messages
from app.core.config import get_settings


def test_validate_dsl_case_success(client) -> None:
    response = client.post(
        "/api/v1/dsl/validate",
        json={
            "name": "登录冒烟",
            "base_url": "https://example.com",
            "input_contract": [
                {
                    "name": "username",
                    "context_key": "login_username",
                    "value_type": "string",
                    "required": True,
                }
            ],
            "output_contract": [
                {
                    "name": "sessionToken",
                    "context_key": "session_token",
                    "value_type": "string",
                    "source": "latest_url",
                }
            ],
            "steps": [
                {"action": "goto", "value": "/login"},
                {"action": "input", "target": "用户名输入框", "value": "admin"},
                {"action": "click", "target": "登录按钮"},
                {"action": "assert_url_contains", "value": "/dashboard"},
            ],
        },
    )

    assert response.status_code == 200
    assert response.json() == {
        "valid": True,
        "case": {
            "name": "登录冒烟",
            "description": None,
            "base_url": "https://example.com",
            "input_contract": [
                {
                    "name": "username",
                    "context_key": "login_username",
                    "value_type": "string",
                    "required": True,
                    "description": None,
                }
            ],
            "output_contract": [
                {
                    "name": "sessionToken",
                    "context_key": "session_token",
                    "value_type": "string",
                    "source": "latest_url",
                    "description": None,
                }
            ],
            "steps": [
                {"action": "goto", "value": "/login"},
                {"action": "input", "target": "用户名输入框", "value": "admin"},
                {"action": "click", "target": "登录按钮"},
                {"action": "assert_url_contains", "value": "/dashboard"},
            ],
        },
        "supported_actions": [
            "goto",
            "click",
            "input",
            "wait_for",
            "assert_text",
            "assert_url_contains",
        ],
    }


def test_validate_dsl_case_rejects_invalid_payload(client) -> None:
    response = client.post(
        "/api/v1/dsl/validate",
        json={
            "name": "非法 DSL",
            "steps": [
                {"action": "click", "value": "缺少 target"},
            ],
        },
    )

    assert response.status_code == 422


def test_build_generation_messages_only_list_supported_actions() -> None:
    messages = build_generation_messages(
        prompt="打开 example.com 并验证 URL",
        base_url="https://example.com",
        supported_actions=["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    )

    assert len(messages) == 2
    assert "Allowed actions: goto, click, input, wait_for, assert_text, assert_url_contains." in messages[0]["content"]
    assert "Do not use any other action names." in messages[0]["content"]


def test_generate_dsl_case_success(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
```json
{
  "name": "AI 生成冒烟",
  "description": "验证首页可访问",
  "steps": [
    {"action": "goto", "value": "/"},
    {"action": "assert_url_contains", "value": "example.com"}
  ]
}
```""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "打开 example.com 并验证 URL 包含 example.com",
            "base_url": "https://example.com",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 200
    assert response.json() == {
        "case": {
            "name": "AI 生成冒烟",
            "description": "验证首页可访问",
            "base_url": "https://example.com",
            "input_contract": [],
            "output_contract": [],
            "steps": [
                {"action": "goto", "value": "/"},
                {"action": "assert_url_contains", "value": "example.com"},
            ],
        },
        "supported_actions": [
            "goto",
            "click",
            "input",
            "wait_for",
            "assert_text",
            "assert_url_contains",
        ],
        "warnings": ["AI 草案未提供 base_url，已回填请求中的 Base URL。"],
    }


def test_generate_dsl_case_returns_503_when_not_configured(client) -> None:
    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "打开 example.com",
            "base_url": "https://example.com",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 503
    assert "AI DSL 生成功能未开启" in response.json()["detail"]


def test_generate_dsl_case_returns_502_for_invalid_json(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr("app.ai.dsl_generator._call_llm", lambda **_: "not-json")

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "打开 example.com",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 502
    assert response.json()["detail"] == "AI 返回了无法解析的 DSL JSON。"


def test_generate_dsl_case_returns_502_for_invalid_dsl_shape(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "非法 DSL",
  "steps": [
    {"action": "hover", "target": "按钮"}
  ]
}""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "悬停按钮",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 502
    assert "AI 返回的 DSL 不符合当前 schema" in response.json()["detail"]
