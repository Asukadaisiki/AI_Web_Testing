"""Prompt constants and slot prompts for test planning."""

from __future__ import annotations


REQUIRED_REQUIREMENT_SLOTS = [
    "app_under_test",
    "business_goal",
    "entry_url_or_page",
    "core_user_flow",
    "main_assertions",
    "test_data_or_account",
    "scope_limits",
]

FOLLOWUP_QUESTION_BY_SLOT = {
    "app_under_test": "请补充被测系统或业务模块名称。",
    "business_goal": "请明确这次测试想验证的核心业务目标。",
    "entry_url_or_page": "请提供入口页面 URL 或页面名称。",
    "core_user_flow": "请描述核心用户操作流程。",
    "main_assertions": "请补充最关键的预期结果或断言。",
    "test_data_or_account": "请说明要使用的测试账号或测试数据。",
    "scope_limits": "请说明本轮不覆盖的范围或限制条件。",
}
