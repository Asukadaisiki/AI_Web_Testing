"""Tests for DSL validation endpoint."""

from __future__ import annotations

from types import SimpleNamespace

from app.models import DslGenerationRun, TestCase, User
from app.ai.dsl_generator import AI_DSL_PROMPT_VERSION, _normalize_string, build_generation_messages
from app.core.config import get_settings
from app.schemas.dsl import GenerateDslRequest
from app.services import dsl as dsl_service


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


def test_normalize_string_returns_none_for_non_strings_and_blank() -> None:
    assert _normalize_string(None) is None
    assert _normalize_string(123) is None
    assert _normalize_string("   ") is None
    assert _normalize_string(" value ") == "value"


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
        generation_mode="strict_steps_only",
        prompt_variant="repair_steps",
        supported_actions=["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    )

    assert len(messages) == 2
    assert "Allowed actions: goto, click, input, wait_for, assert_text, assert_url_contains." in messages[0]["content"]
    assert "Do not use any other action names." in messages[0]["content"]
    assert "strict_steps_only" in messages[1]["content"]
    assert "当前 DSL：" in messages[1]["content"]
    assert "当前步骤：" in messages[1]["content"]


def test_build_generation_messages_uses_contract_context_without_current_case() -> None:
    messages = build_generation_messages(
        payload=GenerateDslRequest.model_validate(
            {
                "prompt": "保留现有契约并补全步骤",
                "actor_user_id": 1,
                "import_mode": "contracts_only",
                "preserve_contracts": True,
                "current_input_contract": [
                    {
                        "name": "token",
                        "context_key": "token",
                        "value_type": "string",
                        "required": True,
                    }
                ],
                "current_output_contract": [
                    {
                        "name": "latestPage",
                        "context_key": "latest_page",
                        "value_type": "string",
                        "source": "latest_url",
                    }
                ],
            }
        ),
        generation_mode="draft",
        prompt_variant="contracts_focus",
        supported_actions=["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    )

    assert "当前契约：" in messages[1]["content"]
    assert "当前 DSL：" not in messages[1]["content"]


def test_build_generation_messages_includes_retry_strategy_context() -> None:
    messages = build_generation_messages(
        payload=GenerateDslRequest.model_validate(
            {
                "prompt": "重新生成登录冒烟",
                "actor_user_id": 1,
                "retry_from_generation_id": 12,
                "retry_reason_code": "bad_contracts",
                "retry_note": "上下文变量命名不稳定",
            }
        ),
        generation_mode="draft",
        prompt_variant="baseline_draft",
        supported_actions=["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    )

    assert "Retry strategy: bad_contracts." in messages[0]["content"]
    assert "Keep contracts minimal, stable" in messages[0]["content"]
    assert "上一版 generation_id：12" in messages[1]["content"]
    assert "上一版被放弃原因：bad_contracts" in messages[1]["content"]
    assert "用户补充说明：上下文变量命名不稳定" in messages[1]["content"]


def test_build_generation_messages_includes_governance_focus_reasons() -> None:
    messages = build_generation_messages(
        payload=GenerateDslRequest.model_validate(
            {
                "prompt": "保留当前业务上下文并稳定输出契约",
                "actor_user_id": 1,
            }
        ),
        generation_mode="draft",
        prompt_variant="baseline_draft",
        supported_actions=["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
        governance_focus_reasons=["wrong_actions", "invalid_structure"],
    )

    assert "Current governance focus reasons: wrong_actions, invalid_structure." in messages[0]["content"]
    assert "Only map well-known action aliases" in messages[0]["content"]
    assert "Never nest the DSL under wrapper keys like case, data, result, or draft." in messages[0]["content"]


def test_select_governance_focus_reasons_defaults_when_no_feedback_history(db_session) -> None:
    assert dsl_service._select_governance_focus_reasons(db_session) == ["wrong_actions", "invalid_structure"]


def test_select_governance_focus_reasons_prefers_current_rollout_targets(db_session) -> None:
    records = [
        DslGenerationRun(
            actor_user_id=1,
            prompt_preview=f"rejection-{index}",
            prompt_sha256=f"{index:064d}",
            prompt_version=AI_DSL_PROMPT_VERSION,
            prompt_variant="baseline_draft",
            request_base_url=None,
            generation_mode="draft",
            import_mode="replace",
            model_name="gpt-test",
            success=True,
            used_current_case_context=False,
            used_current_steps_context=False,
            context_profile="blank_request",
            base_url_source="none",
            base_url_backfilled=False,
            repaired_invalid_actions=0,
            removed_invalid_steps=0,
            removed_invalid_contracts=0,
            preserve_contracts_requested=False,
            preserve_contracts_applied=False,
            warnings_count=0,
            normalization_notes_count=0,
            warnings_json=[],
            normalization_notes_json=[],
            risk_flags_json=[],
            generated_case_json=None,
            feedback_status="rejected",
            feedback_import_mode=None,
            rejection_reason_code=rejection_reason_code,
            feedback_note="seed",
            feedback_recorded_at=None,
        )
        for index, rejection_reason_code in enumerate(
            [
                "bad_contracts",
                "bad_contracts",
                "context_mismatch",
                "context_mismatch",
                "context_mismatch",
                "invalid_structure",
                "invalid_structure",
                "wrong_actions",
            ],
            start=1,
        )
    ]
    db_session.add_all(records)
    db_session.commit()

    assert dsl_service._select_governance_focus_reasons(db_session) == ["wrong_actions", "invalid_structure"]


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
            "prompt_variant": "baseline_draft",
            "context_profile": "blank_request",
            "risk_flags": ["base_url_backfilled"],
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
        },
        {
            "name": "bad",
            "context_key": "bad",
            "value_type": "string",
            "source": None,
            "description": None,
        }
    ]
    assert response.json()["warnings"] == [
        "输入契约 #1 结构非法，已忽略。",
        "步骤 #2 无法修正为合法 DSL，已忽略。",
    ]
    assert response.json()["normalization_notes"] == [
        "AI 草案中的输入契约已从单个对象包装为数组。",
        "输出契约 #2 缺少 name，已回填为 bad。",
        "步骤 #1 的 action 已从 open 自动修正为 goto。",
        "步骤 #3 的 target 已自动转换为字符串。",
    ]
    assert response.json()["generation_meta"]["prompt_variant"] == "baseline_draft"
    assert response.json()["generation_meta"]["context_profile"] == "blank_request"
    assert response.json()["generation_meta"]["risk_flags"] == [
        "invalid_actions_repaired",
        "invalid_steps_removed",
        "invalid_contracts_removed",
    ]
    assert response.json()["generation_meta"]["repaired_invalid_actions"] == 1
    assert response.json()["generation_meta"]["removed_invalid_steps"] == 1
    assert response.json()["generation_meta"]["removed_invalid_contracts"] == 1


def test_generate_dsl_case_repairs_single_step_and_contract_shape_for_governance_v2(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    monkeypatch.setenv("AI_DSL_ALLOW_AUTO_REPAIR", "true")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "治理 v2 修复",
  "steps": {"action": "goto", "value": "/governance-v2"},
  "input_contract": {
    "name": "Session Token",
    "value_type": "text",
    "required": "yes"
  },
  "output_contract": {
    "context_key": "landing_url",
    "value_type": "text",
    "source": "page_url"
  }
}""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成稳定契约",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 200
    assert response.json()["case"]["steps"] == [{"action": "goto", "value": "/governance-v2"}]
    assert response.json()["case"]["input_contract"] == [
        {
            "name": "Session Token",
            "context_key": "session_token",
            "value_type": "string",
            "required": True,
            "description": None,
        }
    ]
    assert response.json()["case"]["output_contract"] == [
        {
            "name": "landing_url",
            "context_key": "landing_url",
            "value_type": "string",
            "source": "latest_url",
            "description": None,
        }
    ]
    assert response.json()["warnings"] == []
    assert response.json()["normalization_notes"] == [
        "AI 草案中的输入契约已从单个对象包装为数组。",
        "输入契约 #1 缺少 context_key，已从 name 派生为 session_token。",
        "输入契约 #1 的 value_type 已从 text 自动修正为 string。",
        "输入契约 #1 的 required 已自动修正为布尔值。",
        "AI 草案中的输出契约已从单个对象包装为数组。",
        "输出契约 #1 缺少 name，已回填为 landing_url。",
        "输出契约 #1 的 value_type 已从 text 自动修正为 string。",
        "输出契约 #1 的 source 已从 page_url 自动修正为 latest_url。",
        "AI 草案中的 steps 已从单个对象包装为数组。",
    ]
    assert response.json()["generation_meta"]["risk_flags"] == []


def test_generate_dsl_case_repairs_contract_aliases_for_governance_v3(client, db_session, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    monkeypatch.setenv("AI_DSL_ALLOW_AUTO_REPAIR", "true")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "治理 v3 契约修复",
  "steps": [{"action": "goto", "value": "/governance-v3"}],
  "input_contract": {
    "label": "Session Token",
    "type": "text",
    "isRequired": "yes"
  },
  "output_contract": {
    "key": "landing_url",
    "type": "text",
    "valueFrom": "page_url"
  }
}""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成更稳定的上下文契约",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 200
    assert response.json()["case"]["input_contract"] == [
        {
            "name": "Session Token",
            "context_key": "session_token",
            "value_type": "string",
            "required": True,
            "description": None,
        }
    ]
    assert response.json()["case"]["output_contract"] == [
        {
            "name": "landing_url",
            "context_key": "landing_url",
            "value_type": "string",
            "source": "latest_url",
            "description": None,
        }
    ]
    assert response.json()["normalization_notes"] == [
        "AI 草案中的输入契约已从单个对象包装为数组。",
        "输入契约 #1 的 label 已映射为 name。",
        "输入契约 #1 缺少 context_key，已从 name 派生为 session_token。",
        "输入契约 #1 的 type 已映射为 value_type。",
        "输入契约 #1 的 value_type 已从 text 自动修正为 string。",
        "输入契约 #1 的 isRequired 已映射为 required。",
        "输入契约 #1 的 required 已自动修正为布尔值。",
        "AI 草案中的输出契约已从单个对象包装为数组。",
        "输出契约 #1 的 key 已映射为 context_key。",
        "输出契约 #1 缺少 name，已回填为 landing_url。",
        "输出契约 #1 的 type 已映射为 value_type。",
        "输出契约 #1 的 value_type 已从 text 自动修正为 string。",
        "输出契约 #1 的 valueFrom 已映射为 source。",
        "输出契约 #1 的 source 已从 page_url 自动修正为 latest_url。",
    ]
    generation_run = db_session.get(DslGenerationRun, response.json()["generation_id"])
    assert generation_run is not None
    assert generation_run.prompt_version == AI_DSL_PROMPT_VERSION


def test_generate_dsl_case_repairs_wrapped_dsl_root_and_step_aliases(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    monkeypatch.setenv("AI_DSL_ALLOW_AUTO_REPAIR", "true")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "draft": {
    "title": "包装结构草案",
    "steps": {
      "items": [
        {"type": "open", "url": "/login"},
        {"command": "tap", "element": "登录按钮"},
        {"action": "input", "element": "用户名输入框", "text": "admin"},
        {"action": "assert_text", "element": "欢迎文案", "expected": "欢迎"},
        {"action": "assert_url_contains", "path": "/dashboard"}
      ]
    }
  }
}""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成包装结构的登录流程",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 200
    assert response.json()["case"]["name"] == "包装结构草案"
    assert response.json()["case"]["steps"] == [
        {"action": "goto", "value": "/login"},
        {"action": "click", "target": "登录按钮"},
        {"action": "input", "target": "用户名输入框", "value": "admin"},
        {"action": "assert_text", "target": "欢迎文案", "value": "欢迎"},
        {"action": "assert_url_contains", "value": "/dashboard"},
    ]
    assert response.json()["normalization_notes"] == [
        "AI 草案已从 draft 包装层中提取 DSL 根对象。",
        "AI 草案中的 title 已映射为 name。",
        "AI 草案中的 steps 已从包装对象中提取为数组。",
        "步骤 #1 的 type 已映射为 action。",
        "步骤 #1 的 url 已映射为 value。",
        "步骤 #1 的 action 已从 open 自动修正为 goto。",
        "步骤 #2 的 command 已映射为 action。",
        "步骤 #2 的 element 已映射为 target。",
        "步骤 #2 的 action 已从 tap 自动修正为 click。",
        "步骤 #3 的 element 已映射为 target。",
        "步骤 #3 的 text 已映射为 value。",
        "步骤 #4 的 element 已映射为 target。",
        "步骤 #4 的 expected 已映射为 value。",
        "步骤 #5 的 path 已映射为 value。",
    ]
    assert response.json()["generation_meta"]["risk_flags"] == ["invalid_actions_repaired"]


def test_generate_dsl_case_still_rejects_invalid_steps_under_governance_v3(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    monkeypatch.setenv("AI_DSL_ALLOW_AUTO_REPAIR", "true")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "仍然失败的草案",
  "steps": [],
  "input_contract": [{"name": "token", "context_key": "token", "value_type": "string"}]
}""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成一个仍然无效的草案",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 502
    assert response.json()["detail"] == "AI 生成草案中没有可导入的有效 steps。"


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
    assert response.json()["generation_meta"]["prompt_variant"] == "repair_steps"
    assert response.json()["generation_meta"]["context_profile"] == "repair_steps"
    assert response.json()["generation_meta"]["risk_flags"] == [
        "contracts_preserved_fallback",
        "base_url_backfilled",
    ]


def test_generate_dsl_case_preserves_contracts_without_current_case_context(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "仅保留契约",
  "steps": [
    {"action": "goto", "value": "/dashboard"}
  ]
}""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "保留当前契约并补全 dashboard 冒烟",
            "actor_user_id": 1,
            "preserve_contracts": True,
            "current_input_contract": [
                {
                    "name": "token",
                    "context_key": "token",
                    "value_type": "string",
                    "required": True,
                }
            ],
            "current_output_contract": [
                {
                    "name": "latestPage",
                    "context_key": "latest_page",
                    "value_type": "string",
                    "source": "latest_url",
                }
            ],
        },
    )

    assert response.status_code == 200
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
    assert response.json()["generation_meta"]["preserve_contracts_applied"] is True
    assert response.json()["generation_meta"]["used_current_case_context"] is False
    assert response.json()["generation_meta"]["used_current_steps_context"] is False
    assert response.json()["generation_meta"]["prompt_variant"] == "baseline_draft"
    assert response.json()["generation_meta"]["context_profile"] == "blank_request"
    assert response.json()["generation_meta"]["risk_flags"] == ["contracts_preserved_fallback"]


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

    case = TestCase(
        project_id=1,
        created_by=1,
        updated_by=1,
        name="已有关联用例",
        description="用于挂接 generation",
        dsl={"name": "已有关联用例", "steps": [{"action": "goto", "value": "/existing"}]},
    )
    db_session.add(case)
    db_session.commit()
    db_session.refresh(case)

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "打开 example.com 首页",
            "base_url": "https://example.com",
            "actor_user_id": 1,
            "project_id": 1,
            "case_id": case.id,
        },
    )

    assert response.status_code == 200
    generation_run = db_session.get(DslGenerationRun, response.json()["generation_id"])
    assert generation_run is not None
    assert generation_run.success is True
    assert generation_run.project_id == 1
    assert generation_run.case_id == case.id
    assert generation_run.prompt_preview == "打开 example.com 首页"
    assert generation_run.prompt_sha256
    assert generation_run.prompt_version == AI_DSL_PROMPT_VERSION
    assert generation_run.prompt_variant == "baseline_draft"
    assert generation_run.model_name == "gpt-test"
    assert generation_run.error_type is None
    assert generation_run.generated_case_json is not None
    assert generation_run.generated_case_json["name"] == "成功落库"
    assert generation_run.context_profile == "blank_request"
    assert generation_run.preserve_contracts_requested is False
    assert generation_run.preserve_contracts_applied is False
    assert generation_run.warnings_count == 1
    assert generation_run.normalization_notes_count == 0
    assert generation_run.warnings_json == ["AI 草案未提供 base_url，已回填请求中的 Base URL。"]
    assert generation_run.normalization_notes_json == []
    assert generation_run.governance_focus_reasons_json == ["wrong_actions", "invalid_structure"]
    assert generation_run.risk_flags_json == ["base_url_backfilled"]
    assert generation_run.feedback_status == "pending"
    assert generation_run.feedback_import_mode is None
    assert generation_run.rejection_reason_code is None
    assert generation_run.feedback_note is None
    assert generation_run.feedback_recorded_at is None
    assert generation_run.retry_from_generation_id is None
    assert generation_run.retry_reason_code is None
    assert generation_run.retry_note is None


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
    assert runs[0].prompt_version == AI_DSL_PROMPT_VERSION
    assert runs[0].prompt_variant == "baseline_draft"
    assert runs[0].warnings_json == []
    assert runs[0].normalization_notes_json == []
    assert runs[0].context_profile == "blank_request"
    assert runs[0].governance_focus_reasons_json == ["wrong_actions", "invalid_structure"]
    assert runs[0].risk_flags_json == []
    assert runs[0].feedback_status == "pending"
    assert runs[0].retry_from_generation_id is None
    assert runs[0].retry_reason_code is None
    assert runs[0].retry_note is None


def test_generate_dsl_case_uses_runtime_strict_mode_default_when_request_omits_generation_mode(
    client,
    db_session,
    monkeypatch,
) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    monkeypatch.setenv("AI_DSL_STRICT_MODE", "true")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "严格模式默认值",
  "steps": [{"action": "goto", "value": "/strict"}]
}""",
    )

    response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "使用默认严格模式生成",
            "actor_user_id": 1,
        },
    )

    assert response.status_code == 200
    assert response.json()["generation_meta"]["generation_mode"] == "strict_steps_only"
    assert response.json()["generation_meta"]["prompt_variant"] == "baseline_draft"
    generation_run = db_session.get(DslGenerationRun, response.json()["generation_id"])
    assert generation_run is not None
    assert generation_run.generation_mode == "strict_steps_only"
    assert generation_run.context_profile == "blank_request"


def test_list_dsl_generation_runs_supports_filters_limit_and_offset(client, db_session, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    case = TestCase(
        project_id=1,
        created_by=1,
        updated_by=1,
        name="过滤关联用例",
        description=None,
        dsl={"name": "过滤关联用例", "steps": [{"action": "goto", "value": "/filter"}]},
    )
    db_session.add(case)
    db_session.commit()
    db_session.refresh(case)

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

    first_id = client.post(
        "/api/v1/dsl/generate",
        json={"prompt": "first", "actor_user_id": 1, "project_id": 1, "case_id": case.id, "base_url": "https://example.com"},
    ).json()["generation_id"]
    client.post("/api/v1/dsl/generate", json={"prompt": "second", "actor_user_id": 1})
    third_id = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "third",
            "actor_user_id": 1,
            "generation_mode": "strict_steps_only",
            "import_mode": "steps_only",
        },
    ).json()["generation_id"]
    client.patch(
        f"/api/v1/dsl/generations/{first_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "accepted",
            "feedback_import_mode": "replace",
        },
    )
    client.patch(
        f"/api/v1/dsl/generations/{third_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "rejected",
            "rejection_reason_code": "context_mismatch",
            "feedback_note": "当前上下文不适合该草案",
        },
    )

    response = client.get("/api/v1/dsl/generations", params={"limit": 2, "offset": 0})
    assert response.status_code == 200
    payload = response.json()
    assert len(payload) == 2
    assert payload[0]["prompt_preview"] == "third"
    assert payload[1]["prompt_preview"] == "second"
    assert payload[0]["prompt_version"] == AI_DSL_PROMPT_VERSION
    assert payload[0]["prompt_variant"] == "baseline_draft"
    assert payload[0]["governance_focus_reasons"] == ["wrong_actions", "invalid_structure"]
    assert payload[0]["risk_flags"] == []
    assert payload[0]["rejection_reason_code"] == "context_mismatch"

    failed_response = client.get("/api/v1/dsl/generations", params={"status": "failed"})
    assert failed_response.status_code == 200
    assert [item["prompt_preview"] for item in failed_response.json()] == ["second"]

    success_response = client.get("/api/v1/dsl/generations", params={"status": "success", "offset": 1, "limit": 1})
    assert success_response.status_code == 200
    assert [item["prompt_preview"] for item in success_response.json()] == ["first"]
    assert success_response.json()[0]["feedback_status"] == "accepted"
    assert success_response.json()[0]["feedback_import_mode"] == "replace"
    assert success_response.json()[0]["project_id"] == 1
    assert success_response.json()[0]["case_id"] == case.id
    assert success_response.json()[0]["risk_flags"] == ["base_url_backfilled"]

    filtered_response = client.get(
        "/api/v1/dsl/generations",
        params={
            "feedback_status": "rejected",
            "generation_mode": "strict_steps_only",
            "import_mode": "steps_only",
            "model_name": "gpt-test",
            "created_from": "2000-01-01T00:00:00",
            "created_to": "2100-12-31T23:59:59",
        },
    )
    assert filtered_response.status_code == 200
    assert [item["prompt_preview"] for item in filtered_response.json()] == ["third"]

    project_case_response = client.get(
        "/api/v1/dsl/generations",
        params={"project_id": 1, "case_id": case.id},
    )
    assert project_case_response.status_code == 200
    assert [item["prompt_preview"] for item in project_case_response.json()] == ["first"]

    prompt_variant_response = client.get(
        "/api/v1/dsl/generations",
        params={"prompt_variant": "baseline_draft", "rejection_reason_code": "context_mismatch", "has_risk_flags": False},
    )
    assert prompt_variant_response.status_code == 200
    assert [item["prompt_preview"] for item in prompt_variant_response.json()] == ["third"]

    risky_response = client.get("/api/v1/dsl/generations", params={"has_risk_flags": True})
    assert risky_response.status_code == 200
    assert [item["prompt_preview"] for item in risky_response.json()] == ["first"]


def test_get_dsl_generation_run_detail_returns_governance_payload(client, db_session, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    monkeypatch.setenv("AI_DSL_ALLOW_AUTO_REPAIR", "true")
    get_settings.cache_clear()
    case = TestCase(
        project_id=1,
        created_by=1,
        updated_by=1,
        name="详情关联用例",
        description=None,
        dsl={"name": "详情关联用例", "steps": [{"action": "goto", "value": "/detail"}]},
    )
    db_session.add(case)
    db_session.commit()
    db_session.refresh(case)
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "详情草案",
  "steps": [
    {"action": "open", "value": "/detail"}
  ]
}""",
    )

    generate_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成详情草案",
            "actor_user_id": 1,
            "project_id": 1,
            "case_id": case.id,
            "preserve_contracts": True,
            "current_input_contract": [
                {
                    "name": "token",
                    "context_key": "token",
                    "value_type": "string",
                    "required": True,
                }
            ],
        },
    )
    generation_id = generate_response.json()["generation_id"]
    feedback_response = client.patch(
        f"/api/v1/dsl/generations/{generation_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "rejected",
            "rejection_reason_code": "bad_contracts",
            "feedback_note": "输出契约不符合预期",
        },
    )

    assert feedback_response.status_code == 200

    detail_response = client.get(f"/api/v1/dsl/generations/{generation_id}")
    assert detail_response.status_code == 200
    payload = detail_response.json()
    assert payload["id"] == generation_id
    assert payload["project_id"] == 1
    assert payload["case_id"] == case.id
    assert payload["prompt_version"] == AI_DSL_PROMPT_VERSION
    assert payload["prompt_variant"] == "baseline_draft"
    assert payload["context_profile"] == "blank_request"
    assert payload["governance_focus_reasons"] == ["wrong_actions", "invalid_structure"]
    assert payload["generated_case_json"]["name"] == "详情草案"
    assert payload["warnings_json"] == []
    assert payload["normalization_notes_json"] == [
        "AI 草案未提供有效输入契约，已沿用当前 DSL 的输入契约。",
        "步骤 #1 的 action 已从 open 自动修正为 goto。",
    ]
    assert payload["risk_flags"] == ["contracts_preserved_fallback", "invalid_actions_repaired"]
    assert payload["rejection_reason_code"] == "bad_contracts"
    assert payload["feedback_note"] == "输出契约不符合预期"
    assert payload["preserve_contracts_requested"] is True
    assert payload["preserve_contracts_applied"] is True


def test_generate_dsl_case_persists_retry_context_and_retry_prompt_version(client, db_session, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "重试草案",
  "steps": [{"action": "goto", "value": "/retry"}]
}""",
    )

    previous_generation_id = client.post(
        "/api/v1/dsl/generate",
        json={"prompt": "上一版草案", "actor_user_id": 1},
    ).json()["generation_id"]
    rejected_response = client.patch(
        f"/api/v1/dsl/generations/{previous_generation_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "rejected",
            "rejection_reason_code": "bad_contracts",
            "feedback_note": "契约命名不稳定",
        },
    )
    assert rejected_response.status_code == 200

    retry_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "按上一版反馈重新生成",
            "actor_user_id": 1,
            "retry_from_generation_id": previous_generation_id,
            "retry_reason_code": "bad_contracts",
            "retry_note": "契约命名不稳定",
        },
    )

    assert retry_response.status_code == 200
    generation_run = db_session.get(DslGenerationRun, retry_response.json()["generation_id"])
    assert generation_run is not None
    assert generation_run.retry_from_generation_id == previous_generation_id
    assert generation_run.retry_reason_code == "bad_contracts"
    assert generation_run.retry_note == "契约命名不稳定"
    assert generation_run.prompt_version == f"{AI_DSL_PROMPT_VERSION}+retry.bad_contracts"
    assert generation_run.governance_focus_reasons_json == ["wrong_actions", "invalid_structure"]


def test_record_generation_feedback_accepts_first_decision_and_is_idempotent(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "反馈记录",
  "steps": [{"action": "goto", "value": "/feedback"}]
}""",
    )

    generate_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成可反馈草案",
            "actor_user_id": 1,
        },
    )
    generation_id = generate_response.json()["generation_id"]

    response = client.patch(
        f"/api/v1/dsl/generations/{generation_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "accepted",
            "feedback_import_mode": "steps_only",
        },
    )

    assert response.status_code == 200
    assert response.json()["feedback_status"] == "accepted"
    assert response.json()["feedback_import_mode"] == "steps_only"
    assert response.json()["feedback_recorded_at"] is not None

    repeated_response = client.patch(
        f"/api/v1/dsl/generations/{generation_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "accepted",
            "feedback_import_mode": "steps_only",
        },
    )

    assert repeated_response.status_code == 200
    assert repeated_response.json()["feedback_status"] == "accepted"
    assert repeated_response.json()["feedback_import_mode"] == "steps_only"


def test_record_generation_feedback_rejects_conflicting_decision(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "冲突反馈",
  "steps": [{"action": "goto", "value": "/feedback"}]
}""",
    )

    generate_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成冲突反馈草案",
            "actor_user_id": 1,
        },
    )
    generation_id = generate_response.json()["generation_id"]

    first_feedback = client.patch(
        f"/api/v1/dsl/generations/{generation_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "accepted",
            "feedback_import_mode": "replace",
        },
    )
    assert first_feedback.status_code == 200

    conflict_response = client.patch(
        f"/api/v1/dsl/generations/{generation_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "rejected",
            "rejection_reason_code": "wrong_actions",
        },
    )

    assert conflict_response.status_code == 409
    assert conflict_response.json()["detail"] == "该生成记录的反馈已写入不同决策，不能覆盖。"


def test_record_generation_feedback_requires_import_mode_for_accepted(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "缺少导入方式",
  "steps": [{"action": "goto", "value": "/feedback"}]
}""",
    )

    generate_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成缺少导入方式草案",
            "actor_user_id": 1,
        },
    )
    generation_id = generate_response.json()["generation_id"]

    response = client.patch(
        f"/api/v1/dsl/generations/{generation_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "accepted",
        },
    )

    assert response.status_code == 422
    assert "accepted 反馈必须提供 feedback_import_mode" in response.text


def test_record_generation_feedback_requires_rejection_reason_for_rejected(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "缺少拒绝原因",
  "steps": [{"action": "goto", "value": "/feedback"}]
}""",
    )

    generate_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "生成缺少拒绝原因草案",
            "actor_user_id": 1,
        },
    )
    generation_id = generate_response.json()["generation_id"]

    response = client.patch(
        f"/api/v1/dsl/generations/{generation_id}/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "rejected",
        },
    )

    assert response.status_code == 422
    assert "rejected 反馈必须提供 rejection_reason_code" in response.text

def test_record_generation_feedback_returns_403_for_non_owner_actor(client, db_session, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "闈炵敓鎴愯€呭弽棣?",
  "steps": [{"action": "goto", "value": "/feedback"}]
}""",
    )
    db_session.add(User(id=2, email="other-user@example.com", display_name="Other User"))
    db_session.commit()

    generate_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "鐢?actor 1 鐢熸垚鑽夋",
            "actor_user_id": 1,
        },
    )
    generation_id = generate_response.json()["generation_id"]

    response = client.patch(
        f"/api/v1/dsl/generations/{generation_id}/feedback",
        json={
            "actor_user_id": 2,
            "feedback_status": "rejected",
            "rejection_reason_code": "other",
        },
    )

    assert response.status_code == 403
    assert response.json()["detail"] == "Only the actor who generated this draft can record feedback."


def test_record_generation_feedback_returns_404_for_missing_generation_run(client) -> None:
    response = client.patch(
        "/api/v1/dsl/generations/999/feedback",
        json={
            "actor_user_id": 1,
            "feedback_status": "rejected",
            "rejection_reason_code": "other",
        },
    )

    assert response.status_code == 404
    assert response.json()["detail"] == "DSL generation run 999 not found."


def test_record_generation_feedback_returns_404_for_missing_actor(client, monkeypatch) -> None:
    monkeypatch.setenv("ENABLE_AI_DSL_GENERATE", "true")
    monkeypatch.setenv("AI_DSL_API_KEY", "test-key")
    monkeypatch.setenv("AI_DSL_MODEL", "gpt-test")
    get_settings.cache_clear()
    monkeypatch.setattr(
        "app.ai.dsl_generator._call_llm",
        lambda **_: """
{
  "name": "缂哄皯 actor",
  "steps": [{"action": "goto", "value": "/feedback"}]
}""",
    )

    generate_response = client.post(
        "/api/v1/dsl/generate",
        json={
            "prompt": "鍏堢敓鎴愪竴鏉¤褰?",
            "actor_user_id": 1,
        },
    )
    generation_id = generate_response.json()["generation_id"]

    response = client.patch(
        f"/api/v1/dsl/generations/{generation_id}/feedback",
        json={
            "actor_user_id": 999,
            "feedback_status": "rejected",
            "rejection_reason_code": "other",
        },
    )

    assert response.status_code == 404
    assert response.json()["detail"] == "User 999 not found."


def test_get_generation_run_for_feedback_uses_for_update_on_postgresql(db_session, monkeypatch) -> None:
    captured_statement = None

    def fake_scalars(statement):
        nonlocal captured_statement
        captured_statement = statement
        return SimpleNamespace(first=lambda: None)

    monkeypatch.setattr(dsl_service, "_supports_for_update", lambda session: True)
    monkeypatch.setattr(db_session, "scalars", fake_scalars)

    dsl_service._get_generation_run_for_feedback(db_session, 123)

    assert captured_statement is not None
    assert captured_statement._for_update_arg is not None


def test_get_generation_run_for_feedback_skips_for_update_on_sqlite(db_session, monkeypatch) -> None:
    get_calls: list[int] = []

    def fake_get(model, generation_id):
        get_calls.append(generation_id)
        return None

    monkeypatch.setattr(dsl_service, "_supports_for_update", lambda session: False)
    monkeypatch.setattr(db_session, "get", fake_get)

    dsl_service._get_generation_run_for_feedback(db_session, 456)

    assert get_calls == [456]
