"""Tests for DSL validation endpoint."""

from __future__ import annotations

from app.models import DslGenerationRun
from app.ai.dsl_generator import build_generation_messages
from app.core.config import get_settings
from app.schemas.dsl import GenerateDslRequest


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
        payload=GenerateDslRequest.model_validate({
            "prompt": "打开 example.com 并验证 URL",
            "base_url": "https://example.com",
            "actor_user_id": 1,
            "generation_mode": "strict_steps_only",
            "import_mode": "steps_only",
            "preserve_contracts": True,
            "current_case": {
                "name": "旧用例",
                "description": "旧描述",
                "base_url": "https://old.example.com",
                "input_contract": [],
                "output_contract": [],
                "steps": [{"action": "goto", "value": "/old"}],
                },
                "current_steps": [{"action": "goto", "value": "/old"}],
            }),
        supported_actions=["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    )

    assert len(messages) == 2
    assert "Allowed actions: goto, click, input, wait_for, assert_text, assert_url_contains." in messages[0]["content"]
    assert "Do not use any other action names." in messages[0]["content"]
    assert "strict_steps_only" in messages[1]["content"]
    assert "当前 DSL：" in messages[1]["content"]
    assert "当前步骤：" in messages[1]["content"]


def test_generate_dsl_case_success(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    monkeypatch.setenv("AI_DSL_ALLOW_AUTO_REPAIR", "true")
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
        "generation_id": 1,
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
        "normalization_notes": [],
        "generation_meta": {
            "model": "gpt-test",
            "generation_mode": "draft",
            "import_mode": "replace",
            "base_url_source": "request",
            "base_url_backfilled": True,
            "repaired_invalid_actions": 0,
            "removed_invalid_steps": 0,
            "removed_invalid_contracts": 0,
            "preserve_contracts_applied": False,
            "used_current_case_context": False,
            "used_current_steps_context": False,
        },
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


def test_generate_dsl_case_auto_repairs_invalid_action_and_contracts(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    monkeypatch.setenv("AI_DSL_ALLOW_AUTO_REPAIR", "true")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "修正后的 DSL",
  "steps": [
    {"action": "open", "value": "/login"},
    {"action": "hover", "target": "按钮"},
    {"action": "click", "target": 123}
  ],
  "input_contract": {},
  "output_contract": [
    {"name": "pageUrl", "context_key": "page_url", "value_type": "string", "source": "latest_url"},
    {"name": "", "context_key": "bad", "value_type": "string"}
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

    assert response.status_code == 200
    assert response.json()["case"]["steps"] == [
        {"action": "goto", "value": "/login"},
        {"action": "click", "target": "123"},
    ]
    assert response.json()["case"]["output_contract"] == [
        {
            "name": "pageUrl",
            "context_key": "page_url",
            "value_type": "string",
            "source": "latest_url",
            "description": None,
        }
    ]
    assert response.json()["warnings"] == [
        "AI 草案中的输入契约不是数组，已忽略该字段。",
        "输出契约 #2 结构非法，已忽略。",
        "步骤 #2 无法修正为合法 DSL，已忽略。",
    ]
    assert response.json()["normalization_notes"] == [
        "步骤 #1 的 action 已从 open 自动修正为 goto。",
        "步骤 #3 的 target 已自动转换为字符串。",
    ]
    assert response.json()["generation_meta"]["repaired_invalid_actions"] == 1
    assert response.json()["generation_meta"]["removed_invalid_steps"] == 1
    assert response.json()["generation_meta"]["removed_invalid_contracts"] == 2


def test_generate_dsl_case_preserves_contracts_and_current_name_in_strict_steps_mode(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "模型生成的新名字",
  "steps": [
    {"action": "goto", "value": "/dashboard"}
  ]
}""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "保留当前契约，仅重写步骤",
            "actor_user_id": 1,
            "generation_mode": "strict_steps_only",
            "import_mode": "steps_only",
            "preserve_contracts": True,
            "current_case": {
                "name": "当前用例",
                "description": "当前描述",
                "base_url": "https://example.com",
                "input_contract": [
                    {
                        "name": "token",
                        "context_key": "token",
                        "value_type": "string",
                        "required": True,
                    }
                ],
                "output_contract": [
                    {
                        "name": "latestPage",
                        "context_key": "latest_page",
                        "value_type": "string",
                        "source": "latest_url",
                    }
                ],
                "steps": [{"action": "goto", "value": "/old"}],
            },
            "current_steps": [{"action": "goto", "value": "/old"}],
        },
    )

    assert response.status_code == 200
    assert response.json()["case"]["name"] == "当前用例"
    assert response.json()["case"]["description"] == "当前描述"
    assert response.json()["case"]["base_url"] == "https://example.com"
    assert response.json()["case"]["input_contract"] == [
        {
            "name": "token",
            "context_key": "token",
            "value_type": "string",
            "required": True,
            "description": None,
        }
    ]
    assert response.json()["case"]["output_contract"] == [
        {
            "name": "latestPage",
            "context_key": "latest_page",
            "value_type": "string",
            "source": "latest_url",
            "description": None,
        }
    ]
    assert response.json()["normalization_notes"] == [
        "strict_steps_only 模式下沿用了当前 DSL 的名称与描述。",
        "AI 草案未提供 base_url，已沿用当前 DSL 的 Base URL。",
        "AI 草案未提供有效契约，已沿用当前 DSL 的输入/输出契约。",
    ]
    assert response.json()["generation_meta"]["preserve_contracts_applied"] is True
    assert response.json()["generation_meta"]["used_current_case_context"] is True
    assert response.json()["generation_meta"]["used_current_steps_context"] is True


def test_generate_dsl_case_returns_502_for_invalid_dsl_shape_when_auto_repair_disabled(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    monkeypatch.setenv("AI_DSL_ALLOW_AUTO_REPAIR", "false")
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
    assert response.json()["detail"] == "步骤 #1 使用了不支持的 action: hover"


def test_generate_dsl_case_persists_success_record(client, db_session, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "成功落库",
  "description": "验证成功记录",
  "steps": [
    {"action": "goto", "value": "/"},
    {"action": "assert_url_contains", "value": "example.com"}
  ]
}""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "打开 example.com 首页",
            "base_url": "https://example.com",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 200
    generation_run = db_session.get(DslGenerationRun, response.json()["generation_id"])
    assert generation_run is not None
    assert generation_run.success is True
    assert generation_run.prompt_preview == "打开 example.com 首页"
    assert generation_run.prompt_sha256
    assert generation_run.model_name == "gpt-test"
    assert generation_run.error_type is None
    assert generation_run.generated_case_json is not None
    assert generation_run.generated_case_json["name"] == "成功落库"
    assert generation_run.warnings_count == 1
    assert generation_run.normalization_notes_count == 0


def test_generate_dsl_case_persists_failure_record(client, db_session, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr("app.ai.dsl_generator._call_llm", lambda **_: "not-json")

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "这次会失败",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 502
    runs = db_session.query(DslGenerationRun).all()
    assert len(runs) == 1
    assert runs[0].success is False
    assert runs[0].error_type == "DslGenerationError"
    assert runs[0].error_message == "AI 返回了无法解析的 DSL JSON。"
    assert runs[0].generated_case_json is None


def test_list_dsl_generation_runs_supports_status_limit_and_offset(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()

    responses = iter(
        [
            """
{
  "name": "第一次成功",
  "steps": [{"action": "goto", "value": "/first"}]
}""",
            "not-json",
            """
{
  "name": "第二次成功",
  "steps": [{"action": "goto", "value": "/second"}]
}""",
        ]
    )
    monkeypatch.setattr("app.ai.dsl_generator._call_llm", lambda **_: next(responses))

    client.post("/api/v1/dsl/generate", json={"prompt": "first", "actor_user_id": 1})
    client.post("/api/v1/dsl/generate", json={"prompt": "second", "actor_user_id": 1})
    client.post("/api/v1/dsl/generate", json={"prompt": "third", "actor_user_id": 1})

    response = client.get("/api/v1/dsl/generations", params={"limit": 2, "offset": 0})
    assert response.status_code == 200
    payload = response.json()
    assert len(payload) == 2
    assert payload[0]["prompt_preview"] == "third"
    assert payload[1]["prompt_preview"] == "second"

    failed_response = client.get("/api/v1/dsl/generations", params={"status": "failed"})
    assert failed_response.status_code == 200
    assert [item["prompt_preview"] for item in failed_response.json()] == ["second"]

    success_response = client.get("/api/v1/dsl/generations", params={"status": "success", "offset": 1, "limit": 1})
    assert success_response.status_code == 200
    assert [item["prompt_preview"] for item in success_response.json()] == ["first"]
