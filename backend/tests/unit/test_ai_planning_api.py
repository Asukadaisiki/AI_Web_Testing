"""Tests for AI planning agent loop and API."""

from __future__ import annotations

from types import SimpleNamespace

from app.schemas.ai_planning import (
    AIPlanningPlan,
    AIPlanningRequirements,
    AIPlanningScenario,
    AIPlanningTestDataRequirement,
    AIPlanningToolCall,
    AIPlanningTurnResponse,
)
from app.schemas.dsl import GenerateDslResponse


def _planning_settings(**overrides):
    values = {
        "enable_ai_planning": True,
        "ai_planning_model": "gpt-4.1-mini",
        "ai_planning_base_url": "https://api.openai.com/v1",
        "ai_planning_api_key": "planning-key",
        "ai_planning_timeout_ms": 30000,
        "ai_planning_max_react_rounds": 3,
    }
    values.update(overrides)
    return SimpleNamespace(**values)


def _plan_response() -> AIPlanningPlan:
    return AIPlanningPlan(
        summary="商城后台登录测试方案",
        assumptions=["入口页面为 /login"],
        risks=["未覆盖忘记密码流程"],
        scenarios=[
            AIPlanningScenario(
                scenario_key="login_success",
                title="登录成功",
                goal="验证管理员可以成功登录后台",
                preconditions=["准备管理员账号"],
                priority="high",
                test_data_requirements=[
                    AIPlanningTestDataRequirement(
                        key="username",
                        label="管理员账号",
                        value_type="string",
                        required=True,
                        source_hint="seed data",
                    )
                ],
                assertions=["跳转到 dashboard"],
                draft_prompt="请基于后台登录成功场景生成 DSL。",
            )
        ],
    )


def test_run_planning_turn_calls_tool_then_asks_user(monkeypatch) -> None:
    from app.ai import test_planning_agent as planning_agent

    llm_responses = iter(
        [
            """
            {
              "thought": "先看下项目里有没有现成登录用例",
              "action": "call_tool",
              "action_input": {
                "tool": "list_test_cases",
                "params": { "search": "登录", "limit": 1 }
              },
              "collected_info": {
                "app_under_test": "商城后台",
                "business_goal": "验证管理员登录"
              }
            }
            """,
            """
            {
              "thought": "已有业务目标，但还缺入口信息",
              "action": "ask_user",
              "action_input": {
                "message": "请补充登录入口页面或 URL。"
              },
              "collected_info": {
                "core_user_flow": "输入账号密码并点击登录"
              }
            }
            """,
        ]
    )

    monkeypatch.setattr(planning_agent, "get_settings", lambda: _planning_settings())
    monkeypatch.setattr(planning_agent, "_call_planning_llm", lambda **_: next(llm_responses))
    monkeypatch.setattr(
        planning_agent,
        "execute_tool",
        lambda **kwargs: (
            '{"cases": [{"id": 11, "name": "后台登录成功"}], "total": 1}'
            if kwargs["tool_name"] == "list_test_cases" and kwargs["project_id"] == 9
            else '{"error": "unexpected"}'
        ),
    )

    result = planning_agent.run_planning_turn(
        transcript=[{"role": "user", "content": "帮我规划商城后台登录测试"}],
        existing_requirements=None,
        db_session=object(),
        project_id=9,
    )

    assert result.next_action == "ask_followup"
    assert result.session_status == "collecting"
    assert result.assistant_message == "请补充登录入口页面或 URL。"
    assert result.requirements.app_under_test == "商城后台"
    assert result.requirements.business_goal == "验证管理员登录"
    assert result.requirements.core_user_flow == "输入账号密码并点击登录"
    assert "entry_url_or_page" in result.missing_slots
    assert result.tool_calls == [
        AIPlanningToolCall(
            tool="list_test_cases",
            params={"search": "登录", "limit": 1},
            result={"cases": [{"id": 11, "name": "后台登录成功"}], "total": 1},
        )
    ]


def test_run_planning_turn_force_generate_overrides_followup(monkeypatch) -> None:
    from app.ai import test_planning_agent as planning_agent

    monkeypatch.setattr(planning_agent, "get_settings", lambda: _planning_settings())
    monkeypatch.setattr(
        planning_agent,
        "_call_planning_llm",
        lambda **_: """
        {
          "thought": "还缺一些信息，先继续追问",
          "action": "ask_user",
          "action_input": { "message": "请补充断言。" },
          "collected_info": {
            "app_under_test": "商城后台",
            "business_goal": "验证管理员登录",
            "entry_url_or_page": "https://shop.example.com/login",
            "core_user_flow": "输入账号密码并点击登录",
            "main_assertions": ["跳转到 dashboard"]
          }
        }
        """,
    )

    result = planning_agent.run_planning_turn(
        transcript=[{"role": "user", "content": "[FORCE_GENERATE] 请直接生成商城后台登录测试方案"}],
        existing_requirements=None,
        db_session=object(),
        project_id=1,
    )

    assert result.next_action == "select_scenarios"
    assert result.session_status == "plan_ready"
    assert result.plan is not None
    assert result.plan.summary
    assert result.plan.scenarios


def test_run_planning_turn_falls_back_when_llm_returns_invalid_json(monkeypatch) -> None:
    from app.ai import test_planning_agent as planning_agent

    monkeypatch.setattr(planning_agent, "get_settings", lambda: _planning_settings())
    monkeypatch.setattr(planning_agent, "_call_planning_llm", lambda **_: "not-json")

    result = planning_agent.run_planning_turn(
        transcript=[
            {
                "role": "user",
                "content": (
                    "被测系统是商城后台。业务目标是验证管理员登录。"
                    "入口页面是 https://shop.example.com/login。"
                    "核心流程是输入账号密码并点击登录。"
                    "主要断言是跳转到 dashboard 并显示欢迎文案。"
                    "测试数据使用管理员账号 admin@example.com。"
                    "范围限制是不覆盖忘记密码。"
                ),
            }
        ],
        existing_requirements=None,
        db_session=object(),
        project_id=1,
    )

    assert result.next_action == "select_scenarios"
    assert result.session_status == "plan_ready"
    assert result.plan is not None
    assert result.plan.scenarios


def test_run_planning_turn_returns_error_after_three_llm_failures(monkeypatch) -> None:
    from app.ai import test_planning_agent as planning_agent

    monkeypatch.setattr(planning_agent, "get_settings", lambda: _planning_settings())
    monkeypatch.setattr(
        planning_agent,
        "_call_planning_llm",
        lambda **_: (_ for _ in ()).throw(RuntimeError("timeout")),
    )

    result = planning_agent.run_planning_turn(
        transcript=[{"role": "user", "content": "帮我规划登录测试"}],
        existing_requirements=None,
        db_session=object(),
        project_id=1,
    )

    assert result.session_status == "error"
    assert result.next_action == "ask_followup"
    assert result.plan is None
    assert "模型配置" in result.assistant_message


def test_create_planning_session_and_restore_detail(client) -> None:
    create_response = client.post(
        "/api/v1/ai-planning/sessions",
        json={"project_id": 1, "case_id": None},
    )

    assert create_response.status_code == 201
    session_id = create_response.json()["session"]["id"]

    detail_response = client.get(f"/api/v1/ai-planning/sessions/{session_id}")

    assert detail_response.status_code == 200
    payload = detail_response.json()
    assert payload["session"]["project_id"] == 1
    assert payload["messages"] == []
    assert payload["drafts"] == []


def test_send_planning_message_records_tool_call_and_assistant_messages(client, monkeypatch) -> None:
    from app.services import ai_planning as ai_planning_service

    monkeypatch.setattr(
        ai_planning_service,
        "run_planning_turn",
        lambda **_: AIPlanningTurnResponse(
            assistant_message="请补充登录入口页面。",
            session_status="collecting",
            requirements=AIPlanningRequirements(
                app_under_test="商城后台",
                business_goal="验证管理员登录",
            ),
            missing_slots=["entry_url_or_page"],
            suggested_questions=["请补充登录入口页面。"],
            plan=None,
            drafts=[],
            next_action="ask_followup",
            tool_calls=[
                AIPlanningToolCall(
                    tool="list_test_cases",
                    params={"search": "登录", "limit": 1},
                    result={"cases": [{"id": 11, "name": "后台登录成功"}], "total": 1},
                )
            ],
        ),
    )

    create_response = client.post(
        "/api/v1/ai-planning/sessions",
        json={"project_id": 1},
    )
    session_id = create_response.json()["session"]["id"]

    response = client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/messages",
        json={"content": "帮我规划登录测试"},
    )

    assert response.status_code == 200
    payload = response.json()
    assert payload["next_action"] == "ask_followup"
    assert payload["tool_calls"][0]["tool"] == "list_test_cases"

    detail_response = client.get(f"/api/v1/ai-planning/sessions/{session_id}")
    detail_payload = detail_response.json()
    assert [item["role"] for item in detail_payload["messages"]] == ["user", "assistant", "assistant"]
    assert [item["turn_type"] for item in detail_payload["messages"]] == ["user", "tool_call", "followup"]
    assert detail_payload["messages"][1]["structured_payload"]["type"] == "tool_call"


def test_generate_planning_drafts_creates_one_draft_per_selected_scenario(client, monkeypatch) -> None:
    from app.services import ai_planning as ai_planning_service

    def fake_generate_dsl_case(session, payload):
        return GenerateDslResponse.model_validate(
            {
                "generation_id": 101 + len(payload.prompt),
                "case": {
                    "name": payload.prompt[:20],
                    "description": "generated from scenario",
                    "base_url": "https://shop.example.com",
                    "input_contract": [],
                    "output_contract": [],
                    "steps": [{"action": "goto", "value": "/login"}],
                },
                "supported_actions": ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
                "warnings": [],
                "normalization_notes": [],
                "generation_meta": {
                    "model": "fake-model",
                    "generation_mode": "draft",
                    "import_mode": "replace",
                    "prompt_variant": "baseline_draft",
                    "context_profile": "blank_request",
                    "active_governance_focus_reasons": [],
                    "risk_flags": [],
                    "base_url_source": "request",
                    "base_url_backfilled": False,
                    "repaired_invalid_actions": 0,
                    "removed_invalid_steps": 0,
                    "removed_invalid_contracts": 0,
                    "preserve_contracts_applied": False,
                    "used_current_case_context": False,
                    "used_current_steps_context": False,
                },
            }
        )

    monkeypatch.setattr(ai_planning_service, "generate_dsl_case", fake_generate_dsl_case)
    monkeypatch.setattr(
        ai_planning_service,
        "run_planning_turn",
        lambda **_: AIPlanningTurnResponse(
            assistant_message="已生成测试规划。",
            session_status="plan_ready",
            requirements=AIPlanningRequirements(
                app_under_test="商城后台",
                business_goal="验证管理员登录",
                entry_url_or_page="https://shop.example.com/login",
                core_user_flow="输入账号密码并点击登录",
                main_assertions=["跳转到 dashboard"],
                test_data_or_account="admin@example.com",
                scope_limits="不覆盖忘记密码",
            ),
            missing_slots=[],
            suggested_questions=[],
            plan=AIPlanningPlan.model_validate(_plan_response().model_dump(mode="json")),
            drafts=[],
            next_action="select_scenarios",
            tool_calls=[],
        ),
    )

    create_response = client.post("/api/v1/ai-planning/sessions", json={"project_id": 1})
    session_id = create_response.json()["session"]["id"]
    client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/messages",
        json={"content": "请生成后台登录测试规划"},
    )

    response = client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/drafts:generate",
        json={"scenario_keys": ["login_success"]},
    )

    assert response.status_code == 200
    payload = response.json()
    assert payload["next_action"] == "drafts_generated"
    assert payload["session_status"] == "drafts_ready"
    assert len(payload["drafts"]) == 1
    assert payload["drafts"][0]["scenario_key"] == "login_success"


def test_update_planning_draft_status_marks_imported(client, monkeypatch) -> None:
    from app.services import ai_planning as ai_planning_service

    def fake_generate_dsl_case(session, payload):
        return GenerateDslResponse.model_validate(
            {
                "generation_id": 301,
                "case": {
                    "name": "登录成功",
                    "description": "generated from scenario",
                    "base_url": "https://shop.example.com",
                    "input_contract": [],
                    "output_contract": [],
                    "steps": [{"action": "goto", "value": "/login"}],
                },
                "supported_actions": ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
                "warnings": [],
                "normalization_notes": [],
                "generation_meta": {
                    "model": "fake-model",
                    "generation_mode": "draft",
                    "import_mode": "replace",
                    "prompt_variant": "baseline_draft",
                    "context_profile": "blank_request",
                    "active_governance_focus_reasons": [],
                    "risk_flags": [],
                    "base_url_source": "request",
                    "base_url_backfilled": False,
                    "repaired_invalid_actions": 0,
                    "removed_invalid_steps": 0,
                    "removed_invalid_contracts": 0,
                    "preserve_contracts_applied": False,
                    "used_current_case_context": False,
                    "used_current_steps_context": False,
                },
            }
        )

    monkeypatch.setattr(ai_planning_service, "generate_dsl_case", fake_generate_dsl_case)
    monkeypatch.setattr(
        ai_planning_service,
        "run_planning_turn",
        lambda **_: AIPlanningTurnResponse(
            assistant_message="已生成测试规划。",
            session_status="plan_ready",
            requirements=AIPlanningRequirements(
                app_under_test="商城后台",
                business_goal="验证管理员登录",
                entry_url_or_page="https://shop.example.com/login",
                core_user_flow="输入账号密码并点击登录",
                main_assertions=["跳转到 dashboard"],
                test_data_or_account="admin@example.com",
                scope_limits="不覆盖忘记密码",
            ),
            missing_slots=[],
            suggested_questions=[],
            plan=AIPlanningPlan.model_validate(_plan_response().model_dump(mode="json")),
            drafts=[],
            next_action="select_scenarios",
            tool_calls=[],
        ),
    )

    create_response = client.post("/api/v1/ai-planning/sessions", json={"project_id": 1})
    session_id = create_response.json()["session"]["id"]
    client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/messages",
        json={"content": "请生成后台登录测试规划"},
    )
    drafts_response = client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/drafts:generate",
        json={"scenario_keys": ["login_success"]},
    )
    draft_id = drafts_response.json()["drafts"][0]["id"]

    update_response = client.patch(
        f"/api/v1/ai-planning/drafts/{draft_id}",
        json={"status": "imported"},
    )

    assert update_response.status_code == 200
    assert update_response.json()["status"] == "imported"


def test_delete_planning_session_removes_session_and_returns_204(client) -> None:
    create_response = client.post("/api/v1/ai-planning/sessions", json={"project_id": 1})
    session_id = create_response.json()["session"]["id"]

    delete_response = client.delete(f"/api/v1/ai-planning/sessions/{session_id}")

    assert delete_response.status_code == 204

    detail_response = client.get(f"/api/v1/ai-planning/sessions/{session_id}")
    assert detail_response.status_code == 404


def test_delete_planning_session_returns_404_when_missing(client) -> None:
    response = client.delete("/api/v1/ai-planning/sessions/999")

    assert response.status_code == 404
