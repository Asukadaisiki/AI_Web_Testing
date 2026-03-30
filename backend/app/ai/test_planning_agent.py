"""Deterministic agent loop for test planning."""

from __future__ import annotations

import re

from app.ai.test_planning_prompts import FOLLOWUP_QUESTION_BY_SLOT, REQUIRED_REQUIREMENT_SLOTS
from app.schemas.ai_planning import (
    AIPlanningPlan,
    AIPlanningRequirements,
    AIPlanningScenario,
    AIPlanningTestDataRequirement,
    AIPlanningTurnResponse,
)


URL_PATTERN = re.compile(r"https?://[^\s，。；;]+", re.IGNORECASE)


def run_planning_turn(
    *,
    transcript: list[dict[str, str]],
    existing_requirements: AIPlanningRequirements | None,
) -> AIPlanningTurnResponse:
    requirements = existing_requirements.model_copy(deep=True) if existing_requirements else AIPlanningRequirements()
    user_text = "\n".join(item["content"] for item in transcript if item.get("role") == "user")
    _fill_requirements_from_text(requirements, user_text)
    missing_slots = [slot for slot in REQUIRED_REQUIREMENT_SLOTS if _slot_is_missing(requirements, slot)]

    if missing_slots:
        return AIPlanningTurnResponse(
            assistant_message="为了完善测试方案，我还需要补充几项关键信息。",
            session_status="collecting",
            requirements=requirements,
            missing_slots=missing_slots,
            suggested_questions=[FOLLOWUP_QUESTION_BY_SLOT[slot] for slot in missing_slots[:2]],
            plan=None,
            drafts=[],
            next_action="ask_followup",
        )

    plan = _build_plan(requirements)
    return AIPlanningTurnResponse(
        assistant_message="信息已经足够，我先给出结构化测试方案，请选择要生成草案的场景。",
        session_status="plan_ready",
        requirements=requirements,
        missing_slots=[],
        suggested_questions=[],
        plan=plan,
        drafts=[],
        next_action="select_scenarios",
    )


def _fill_requirements_from_text(requirements: AIPlanningRequirements, text: str) -> None:
    if not requirements.app_under_test:
        requirements.app_under_test = _extract_after_keyword(text, ["被测系统是", "系统是", "应用是"])
    if not requirements.business_goal:
        requirements.business_goal = _extract_after_keyword(text, ["业务目标是", "目标是"])
    if not requirements.entry_url_or_page:
        requirements.entry_url_or_page = _extract_url(text) or _extract_after_keyword(text, ["入口页面是", "入口是", "页面是"])
    if not requirements.core_user_flow:
        requirements.core_user_flow = _extract_after_keyword(text, ["核心流程是", "流程是", "操作流程是"])
    if not requirements.main_assertions:
        assertions = _extract_after_keyword(text, ["主要断言是", "断言是", "预期是"])
        if assertions:
            requirements.main_assertions = _split_items(assertions)
    if not requirements.test_data_or_account:
        requirements.test_data_or_account = _extract_after_keyword(text, ["测试数据使用", "测试数据是", "测试账号是", "使用管理员账号"])
    if not requirements.scope_limits:
        requirements.scope_limits = _extract_after_keyword(text, ["范围限制是", "限制是", "不覆盖"])


def _extract_after_keyword(text: str, keywords: list[str]) -> str | None:
    for keyword in keywords:
        pattern = re.compile(rf"{re.escape(keyword)}(.+?)(?:[。；;\n]|$)")
        match = pattern.search(text)
        if match:
            value = match.group(1).strip(" ：:，,")
            if value:
                return value
    return None


def _extract_url(text: str) -> str | None:
    match = URL_PATTERN.search(text)
    return match.group(0) if match else None


def _split_items(value: str) -> list[str]:
    items = re.split(r"[，,、]|(?:\s+且\s+)|(?:\s+并\s+)", value)
    return [item.strip() for item in items if item.strip()]


def _slot_is_missing(requirements: AIPlanningRequirements, slot: str) -> bool:
    value = getattr(requirements, slot)
    if isinstance(value, list):
        return not value
    return not bool(value and str(value).strip())


def _build_plan(requirements: AIPlanningRequirements) -> AIPlanningPlan:
    assertions = requirements.main_assertions or ["页面状态符合预期"]
    is_login = _looks_like_login(requirements)
    flow_label = "登录" if is_login else "核心流程"
    scenarios = [
        AIPlanningScenario(
            scenario_key="login_success" if is_login else "primary_flow_success",
            title=f"{flow_label}成功",
            goal=requirements.business_goal or "验证主流程可正常通过",
            preconditions=[
                requirements.entry_url_or_page or "提供有效入口页",
                requirements.test_data_or_account or "准备可用测试数据",
            ],
            priority="high",
            test_data_requirements=_build_test_data_requirements(requirements, is_login=is_login),
            assertions=assertions,
            draft_prompt=_build_draft_prompt(requirements, scenario_title=f"{flow_label}成功", negative_case=False),
        ),
        AIPlanningScenario(
            scenario_key="login_error" if is_login else "primary_flow_validation",
            title=f"{flow_label}异常处理",
            goal=f"验证{flow_label}流程在异常输入下的兜底行为",
            preconditions=[requirements.entry_url_or_page or "提供有效入口页"],
            priority="medium",
            test_data_requirements=_build_test_data_requirements(requirements, is_login=is_login),
            assertions=["错误提示符合预期", *assertions[:1]],
            draft_prompt=_build_draft_prompt(requirements, scenario_title=f"{flow_label}异常处理", negative_case=True),
        ),
    ]
    return AIPlanningPlan(
        summary=f"{requirements.app_under_test} - {requirements.business_goal}",
        assumptions=[
            f"入口页使用 {requirements.entry_url_or_page}",
            f"测试数据以 {requirements.test_data_or_account} 为准",
        ],
        risks=[requirements.scope_limits or "未补充范围限制"],
        scenarios=scenarios,
    )


def _looks_like_login(requirements: AIPlanningRequirements) -> bool:
    haystack = " ".join(filter(None, [requirements.business_goal, requirements.core_user_flow, requirements.entry_url_or_page]))
    lowered = haystack.casefold()
    return "登录" in haystack or "login" in lowered or "signin" in lowered


def _build_test_data_requirements(
    requirements: AIPlanningRequirements,
    *,
    is_login: bool,
) -> list[AIPlanningTestDataRequirement]:
    source = requirements.test_data_or_account or "测试数据"
    if is_login:
        return [
            AIPlanningTestDataRequirement(
                key="username",
                label="登录账号",
                value_type="string",
                required=True,
                source_hint=source,
            ),
            AIPlanningTestDataRequirement(
                key="password",
                label="登录密码",
                value_type="string",
                required=True,
                source_hint="secret",
            ),
        ]
    return [
        AIPlanningTestDataRequirement(
            key="input_data",
            label="主流程输入数据",
            value_type="string",
            required=True,
            source_hint=source,
        )
    ]


def _build_draft_prompt(
    requirements: AIPlanningRequirements,
    *,
    scenario_title: str,
    negative_case: bool,
) -> str:
    assertions = "；".join(requirements.main_assertions)
    data_labels = "；".join(item.label for item in _build_test_data_requirements(requirements, is_login=_looks_like_login(requirements)))
    negative_hint = "需要覆盖异常输入和错误提示。" if negative_case else "走正常主流程。"
    return (
        f"基于测试方案生成 DSL 草案。场景：{scenario_title}。"
        f"被测系统：{requirements.app_under_test}。"
        f"目标：{requirements.business_goal}。"
        f"入口：{requirements.entry_url_or_page}。"
        f"流程：{requirements.core_user_flow}。"
        f"断言：{assertions}。"
        f"测试数据需求：{data_labels}。"
        f"范围限制：{requirements.scope_limits}。"
        f"{negative_hint}"
    )
