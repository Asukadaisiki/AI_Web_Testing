"""Tests for centralized prompt registry."""

from __future__ import annotations

import pytest

from app.ai.prompts import PromptStage, render_prompt


def test_render_planning_init_prompt_injects_tool_descriptions() -> None:
    rendered = render_prompt(
        PromptStage.PLANNING_INIT,
        {"tool_descriptions": "### explore_page\n采集页面元素"},
    )

    assert rendered.stage == PromptStage.PLANNING_INIT
    assert rendered.version == "planning.init.v1"
    assert "### explore_page" in rendered.content
    assert "只返回合法 JSON" in rendered.content
    assert "product_a_price" not in rendered.content
    assert rendered.extension_slots == ("runtime_context", "policy_context")


def test_render_planning_init_prompt_requires_tool_descriptions() -> None:
    with pytest.raises(ValueError, match="tool_descriptions"):
        render_prompt(PromptStage.PLANNING_INIT)


def test_planning_prompt_compatibility_wrapper_uses_registry(monkeypatch) -> None:
    from app.ai import test_planning_prompts

    monkeypatch.setattr(
        test_planning_prompts,
        "get_tool_descriptions_for_prompt",
        lambda: "### get_project_info\n读取项目信息",
    )

    prompt = test_planning_prompts.build_system_prompt()

    assert "### get_project_info" in prompt
    assert "Web 自动化测试规划 Agent" in prompt


def test_static_prompt_stages_render_without_variables() -> None:
    for stage in (
        PromptStage.DSL_GENERATE_SYSTEM,
        PromptStage.VLM_LOCATE_SYSTEM,
        PromptStage.VLM_SECTION_SYSTEM,
        PromptStage.VLM_RANK_CANDIDATE_SYSTEM,
        PromptStage.VLM_PAGE_ANNOTATION_SYSTEM,
    ):
        rendered = render_prompt(stage)
        assert rendered.content.strip()
        assert rendered.stage == stage
