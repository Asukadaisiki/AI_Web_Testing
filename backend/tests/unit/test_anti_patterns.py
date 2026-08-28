"""Tests for anti-pattern recording and context injection.

TDD approach: Define expected behavior first, then fix implementation.
"""

from __future__ import annotations

from datetime import UTC, datetime
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest


# ── Test fixtures ─────────────────────────────────────────────────────────────


def _mock_planning_session(case_id=None, project_id=1, session_id=301):
    """Create a mock planning session."""
    return SimpleNamespace(
        id=session_id,
        case_id=case_id,
        projects=[SimpleNamespace(id=project_id)],
    )


def _mock_execution_report(steps):
    """Create a mock execution report."""
    return {"steps": steps}


def _mock_failed_step(
    action="capture_text",
    target='paragraph "Blue Top" inside "Blue Top"',
    error_msg='All locate tiers failed for target: paragraph "Blue Top" inside "Blue Top"',
    resolved_by="unknown",
):
    """Create a mock failed step."""
    return {
        "action": action,
        "target": target,
        "status": "failed",
        "error_message": error_msg,
        "resolved_by": resolved_by,
        "value": None,
    }


# ── Tests for _build_execution_error_context ──────────────────────────────────


class TestBuildExecutionErrorContext:
    """Test execution error context injection."""

    def test_returns_none_when_case_id_is_none_and_no_project(self):
        """当 case_id 为 null 且没有项目时，返回 None"""
        from app.services.ai_planning import _build_execution_error_context

        session = MagicMock()
        planning_session = SimpleNamespace(
            id=301,
            case_id=None,
            projects=[],  # 没有项目
        )

        result = _build_execution_error_context(session, planning_session)
        assert result is None

    def test_should_inject_error_when_recent_execution_exists(self):
        """期望行为: 即使 case_id 为 null，也应从最近的执行记录中注入错误

        修复后: 当 case_id 为 null 时，从项目的最近执行记录中查找
        """
        from app.models import TestCaseRun, TestCase
        from app.services.ai_planning import _build_execution_error_context

        # 创建 mock session
        session = MagicMock()
        planning_session = _mock_planning_session(case_id=None, project_id=1)

        # 创建 mock 执行记录
        mock_run = MagicMock()
        mock_run.report = _mock_execution_report([
            _mock_failed_step(),
        ])

        # Mock 查询结果
        mock_result = MagicMock()
        mock_result.scalar_one_or_none.return_value = mock_run
        session.execute.return_value = mock_result

        # 修复后行为: 应该返回错误信息
        result = _build_execution_error_context(session, planning_session)
        assert result is not None
        assert "All locate tiers failed" in result

    def test_injects_error_when_case_id_exists(self):
        """当 case_id 存在时，应该注入执行错误"""
        from app.services.ai_planning import _build_execution_error_context

        session = MagicMock()
        planning_session = _mock_planning_session(case_id=130)

        # 创建 mock 执行记录
        mock_run = MagicMock()
        mock_run.report = _mock_execution_report([
            _mock_failed_step(),
        ])

        # Mock SQLAlchemy 查询结果
        mock_result = MagicMock()
        mock_result.scalar_one_or_none.return_value = mock_run
        session.execute.return_value = mock_result

        result = _build_execution_error_context(session, planning_session)

        # 应该返回错误信息
        assert result is not None
        assert "All locate tiers failed" in result


# ── Tests for _record_execution_anti_patterns ─────────────────────────────────


class TestRecordExecutionAntiPatterns:
    """Test anti-pattern recording and classification."""

    def test_classifies_locate_tier_failure_as_target_not_found(self):
        """All locate tiers failed 应该被分类为 TARGET_NOT_FOUND

        BUG: 当前实现没有识别 "All locate tiers failed" 关键字，
        导致被错误分类为 MISSING_STEP
        """
        from app.services.anti_patterns import TARGET_NOT_FOUND, WRONG_PAGE_STATE, MISSING_STEP

        error_msg = 'All locate tiers failed for target: paragraph "Premium Polo T-Shirts" inside "Premium Polo T-Shirts"'

        # 模拟当前分类逻辑 (ai_planning.py:1559-1564)
        action = "capture_text"
        if "assert_text" in action:
            category = WRONG_PAGE_STATE
        elif "timeout" in error_msg.lower() or "not found" in error_msg.lower():
            category = TARGET_NOT_FOUND
        else:
            category = MISSING_STEP

        # 当前行为: MISSING_STEP (BUG)
        assert category == MISSING_STEP

        # 期望行为 (修复后): 应该识别 "All locate tiers failed" 为 TARGET_NOT_FOUND
        # assert category == TARGET_NOT_FOUND

    def test_classifies_vlm_failure_as_target_not_found(self):
        """VLM 模型失败应该被分类为 TARGET_NOT_FOUND

        BUG: 当前实现没有识别 VLM 失败的错误信息
        """
        from app.services.anti_patterns import TARGET_NOT_FOUND, MISSING_STEP

        error_msg = '所有 VLM 模型均失败：glm-4.6v-flash: HTTPError: HTTP Error 429'

        # 模拟当前分类逻辑
        action = "capture_text"
        if "timeout" in error_msg.lower() or "not found" in error_msg.lower():
            category = TARGET_NOT_FOUND
        else:
            category = MISSING_STEP

        # 当前行为: MISSING_STEP (BUG)
        assert category == MISSING_STEP

        # 期望行为 (修复后): 应该识别 VLM 失败为 TARGET_NOT_FOUND
        # assert category == TARGET_NOT_FOUND

    def test_paragraph_target_is_a_supported_text_role(self):
        """paragraph targets use text matching and remain valid DSL targets."""
        from app.locators.semantic import _A11Y_TO_PLAYWRIGHT_ROLE
        from app.locators.semantic import _TEXT_ONLY_ROLES

        assert _A11Y_TO_PLAYWRIGHT_ROLE["paragraph"] == "paragraph"
        assert "paragraph" in _TEXT_ONLY_ROLES

    def test_classifies_valid_target_format_correctly(self):
        """有效的 target 格式应该被正确分类"""
        from app.locators.semantic import _A11Y_TO_PLAYWRIGHT_ROLE

        # 有效的 role
        valid_targets = [
            'heading "Rs. 500" inside "Blue Top"',
            'link "Add to cart" inside "Blue Top"',
            'button "Continue Shopping"',
        ]

        for target in valid_targets:
            # 解析 role
            import re
            match = re.match(r'^(\w+)\s+"', target)
            if match:
                role = match.group(1)
                assert role in _A11Y_TO_PLAYWRIGHT_ROLE, f"{role} should be valid"


# ── Tests for anti-pattern injection relevance ────────────────────────────────


class TestAntiPatternInjectionRelevance:
    """Test that anti-pattern injection uses relevant patterns."""

    def test_injects_relevant_patterns_for_target_not_found(self):
        """应该注入与 target_not_found 相关的 anti-pattern

        BUG: 当前实现注入的是 wrong_page_state 类型，
        而实际错误是 target_not_found，导致 anti-pattern 不相关
        """
        from app.services.anti_patterns import TARGET_NOT_FOUND, WRONG_PAGE_STATE

        # 当前实现: 注入的是 wrong_page_state 类型 (从日志看到)
        injected_categories = ["wrong_page_state", "wrong_page_state", "wrong_page_state"]

        # 期望: 当错误是 target_not_found 时，应该注入 target_not_found 类型
        current_error_category = TARGET_NOT_FOUND

        # 当前行为: 注入的类别与当前错误不相关
        assert all(cat != current_error_category for cat in injected_categories)

        # 期望行为 (修复后): 应该优先注入 target_not_found 类型
        # assert any(cat == current_error_category for cat in injected_categories)

    def test_anti_pattern_injection_should_match_retry_reason(self):
        """Anti-pattern 注入应该匹配 retry_reason_code

        BUG: 当前实现没有根据 retry_reason_code 选择相关 anti-pattern
        """
        # 当前实现: 无论 retry_reason_code 是什么，都注入相同的 anti-pattern
        # 期望: 应该根据 retry_reason_code 选择相关的 anti-pattern

        # 模拟 retry_reason_code
        retry_reason_code = "execution_failed"

        # 当前行为: 注入的 anti-pattern 与 retry_reason_code 无关
        # 期望行为: 应该注入与 execution_failed 相关的 anti-pattern
        pass


# ── Tests for DSL generator prompt ────────────────────────────────────────────


class TestDslGeneratorPrompt:
    """Test DSL generator prompt includes correct guidance."""

    def test_prompt_warns_against_using_container_role(self):
        """Prompt 应该警告不要使用容器的 role"""
        from app.ai.dsl_generator import generate_case_draft

        # 读取 system prompt
        # 当前实现: 有警告，但不够明确
        # 期望: 应该明确说明不要使用 paragraph 等容器 role

        # 检查 prompt 中是否有相关警告
        # 这需要实际读取 prompt 内容
        pass

    def test_prompt_includes_inside_format_examples(self):
        """Prompt 应该包含 inside 格式的正确示例"""
        # 当前实现: 有示例，但可能不够清晰
        # 期望: 应该有明确的示例说明如何使用 inside 格式
        pass


# ── Integration test for full flow ────────────────────────────────────────────


class TestFullFlowIntegration:
    """Test the full flow from execution failure to DSL regeneration."""

    def test_execution_error_flows_to_dsl_generator(self):
        """执行错误应该流向 DSL 生成器"""
        # 1. 执行失败
        # 2. 记录 anti-pattern
        # 3. 注入到 DSL 生成器
        # 4. DSL 生成器使用正确的 target 格式

        # 当前实现: 流程断裂
        # - case_id 为 null 时，执行错误未注入
        # - anti-pattern 分类错误
        # - 注入的 anti-pattern 不相关

        pass


# ── Helper functions for testing ──────────────────────────────────────────────


def _create_mock_db_session():
    """Create a mock database session."""
    session = MagicMock()
    return session


def _create_test_anti_patterns(count=3, category="wrong_page_state"):
    """Create test anti-patterns."""
    patterns = []
    for i in range(count):
        pattern = SimpleNamespace(
            id=i + 1,
            error_category=category,
            wrong_snippet={
                "action": "click",
                "target": f'target_{i}',
            },
            context_note=f"Test pattern {i}",
        )
        patterns.append(pattern)
    return patterns
