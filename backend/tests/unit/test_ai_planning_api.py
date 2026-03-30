"""Tests for AI planning agent loop and API."""

from __future__ import annotations

from app.schemas.dsl import GenerateDslResponse


def test_run_planning_turn_returns_followup_when_required_slots_missing() -> None:
    from app.ai.test_planning_agent import run_planning_turn

    result = run_planning_turn(
        transcript=[{"role": "user", "content": "帮我设计登录测试方案"}],
        existing_requirements=None,
    )

    assert result.next_action == "ask_followup"
    assert result.session_status == "collecting"
    assert "entry_url_or_page" in result.missing_slots
    assert len(result.suggested_questions) <= 2


def test_run_planning_turn_returns_plan_when_required_slots_are_complete() -> None:
    from app.ai.test_planning_agent import run_planning_turn

    result = run_planning_turn(
        transcript=[
            {
                "role": "user",
                "content": (
                    "被测系统是电商后台。业务目标是验证管理员登录。"
                    "入口页面是 https://shop.example.com/login。"
                    "核心流程是输入账号密码并点击登录。"
                    "主要断言是跳转到 dashboard 且显示欢迎文案。"
                    "测试数据使用管理员账号 admin@example.com。"
                    "范围限制是不覆盖忘记密码和注册。"
                ),
            }
        ],
        existing_requirements=None,
    )

    assert result.next_action == "select_scenarios"
    assert result.session_status == "plan_ready"
    assert result.plan is not None
    assert result.plan.scenarios
    first_scenario = result.plan.scenarios[0]
    assert first_scenario.test_data_requirements
    assert first_scenario.assertions


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


def test_send_planning_message_records_user_and_assistant_messages(client) -> None:
    create_response = client.post(
        "/api/v1/ai-planning/sessions",
        json={"project_id": 1},
    )
    session_id = create_response.json()["session"]["id"]

    response = client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/messages",
        json={"content": "帮我设计登录测试方案"},
    )

    assert response.status_code == 200
    payload = response.json()
    assert payload["next_action"] == "ask_followup"
    assert payload["session_status"] == "collecting"

    detail_response = client.get(f"/api/v1/ai-planning/sessions/{session_id}")
    detail_payload = detail_response.json()
    assert [item["role"] for item in detail_payload["messages"]] == ["user", "assistant"]


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

    create_response = client.post("/api/v1/ai-planning/sessions", json={"project_id": 1})
    session_id = create_response.json()["session"]["id"]
    client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/messages",
        json={
            "content": (
                "被测系统是电商后台。业务目标是验证管理员登录。"
                "入口页面是 https://shop.example.com/login。"
                "核心流程是输入账号密码并点击登录。"
                "主要断言是跳转到 dashboard 且显示欢迎文案。"
                "测试数据使用管理员账号 admin@example.com。"
                "范围限制是不覆盖忘记密码和注册。"
            )
        },
    )

    response = client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/drafts:generate",
        json={"scenario_keys": ["login_success", "login_error"]},
    )

    assert response.status_code == 200
    payload = response.json()
    assert payload["next_action"] == "drafts_generated"
    assert payload["session_status"] == "drafts_ready"
    assert len(payload["drafts"]) == 2
    assert {draft["scenario_key"] for draft in payload["drafts"]} == {"login_success", "login_error"}


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

    create_response = client.post("/api/v1/ai-planning/sessions", json={"project_id": 1})
    session_id = create_response.json()["session"]["id"]
    client.post(
        f"/api/v1/ai-planning/sessions/{session_id}/messages",
        json={
            "content": (
                "被测系统是电商后台。业务目标是验证管理员登录。"
                "入口页面是 https://shop.example.com/login。"
                "核心流程是输入账号密码并点击登录。"
                "主要断言是跳转到 dashboard 且显示欢迎文案。"
                "测试数据使用管理员账号 admin@example.com。"
                "范围限制是不覆盖忘记密码和注册。"
            )
        },
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
