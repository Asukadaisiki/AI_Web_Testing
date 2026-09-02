"""Shared Planning context construction use cases."""

from __future__ import annotations

import logging

from sqlalchemy import select

from app.application.planning.project_context import (
    get_active_project_id as _get_active_project_id,
    get_session_project_ids as _get_session_project_ids,
)
from app.core.structured_logging import get_structured_logger
from sqlalchemy.orm import Session

from app.models import AIPlanningSession
from app.schemas.ai_planning import (
    AIPlanningRequirements,
)


logger = logging.getLogger(__name__)
slog = get_structured_logger(__name__)


def categorize_error(error_message: str) -> str:
    """Use the same failure taxonomy as execution reports."""
    from app.services.failure_signals import categorize_failure

    return categorize_failure(error_message=error_message)


def build_session_context_preamble(
    planning_session: AIPlanningSession,
    db_session: Session,
    existing_msg_count: int,
) -> str | None:
    """Build an auto-context preamble with current project test status and cross-session insights.

    Returns None if injection is not needed (first turn or no project).
    """
    project_ids = _get_session_project_ids(planning_session)
    if not project_ids or existing_msg_count <= 1:
        return None

    from app.ai.planning_tools import get_project_test_status
    try:
        status = get_project_test_status(
            params={}, db_session=db_session, project_id=_get_active_project_id(planning_session),
        )
    except Exception:
        logger.warning("Auto-context injection: failed to query project status", exc_info=True)
        return None

    conclusion_labels = {
        "all_passed": "全部通过", "partial": "部分通过",
        "all_failed": "全部失败", "no_runs": "无执行记录",
    }
    conclusion = status.get("conclusion", "unknown")
    if conclusion == "no_runs":
        return None

    lines = ["[系统自动注入 - 当前项目测试状态]"]
    lines.append(f"整体结论：{conclusion_labels.get(conclusion, conclusion)}")
    for case in status.get("cases", []):
        cs = case.get("latest_status", "unknown")
        if cs == "no_runs":
            continue
        icon = "✅" if cs == "passed" else "❌"
        p = case.get("passed_steps", 0)
        t = case.get("total_steps", 0)
        err = case.get("error_message", "")
        line = f"{icon} {case.get('case_name', '?')} — {cs} ({p}/{t}步)"
        if err:
            line += f" | 错误: {err}"
        lines.append(line)

    # Session-level test_context
    requirements = AIPlanningRequirements.model_validate(planning_session.requirements_json or {})
    tc = requirements.test_context
    if tc:
        if tc.get("suspected_root_cause"):
            lines.append(f"上次分析根因：{tc['suspected_root_cause']}")
        if tc.get("next_action"):
            lines.append(f"上次建议动作：{tc['next_action']}")
        if tc.get("regression_scope"):
            lines.append(f"上次回归范围：{tc['regression_scope']}")

    # Cross-session insights from TestPointInsight
    try:
        from app.ai.planning_tools import get_project_insights
        insights = get_project_insights(
            params={}, db_session=db_session, project_id=_get_active_project_id(planning_session),
        )
        if insights.get("has_insights"):
            lines.append("")
            lines.append("[历史洞察 - 跨会话积累]")
            if insights.get("regression_risk"):
                lines.append(f"回归风险等级：{insights['regression_risk']}")
            if insights.get("flaky_case_ids"):
                lines.append(f"已知 Flaky 用例 ID：{', '.join(str(i) for i in insights['flaky_case_ids'])}")
            if insights.get("last_analysis_summary"):
                lines.append(f"上次分析摘要：{insights['last_analysis_summary']}")
            fp = insights.get("failure_patterns", {})
            if fp:
                for pattern_name, pattern_info in fp.items():
                    if isinstance(pattern_info, dict):
                        lines.append(f"失败模式 {pattern_name}：出现 {pattern_info.get('count', '?')} 次")
    except Exception:
        logger.warning("Auto-context injection: failed to load cross-session insights", exc_info=True)

    return "\n".join(lines)


def build_tool_call_summary(
    db_session: Session,
    session_id: int,
    limit: int = 20,
) -> str | None:
    """从 DB 重建之前 turn 的工具调用摘要"""
    from app.models import AIPlanningMessage
    import json as _json

    tool_messages = db_session.scalars(
        select(AIPlanningMessage)
        .where(AIPlanningMessage.session_id == session_id)
        .where(AIPlanningMessage.turn_type == "tool_call")
        .order_by(AIPlanningMessage.id.desc())
        .limit(limit)
    ).all()

    if not tool_messages:
        return None

    summaries = []
    tool_names = []
    for msg in tool_messages:
        payload = msg.structured_payload_json or {}
        tool_name = payload.get("tool", "unknown")
        tool_names.append(tool_name)
        params = payload.get("params", {})
        result_summary = payload.get("result_summary")

        # 压缩显示
        params_str = _json.dumps(params, ensure_ascii=False)[:150]
        if result_summary:
            result_str = _json.dumps(result_summary, ensure_ascii=False)[:300]
        else:
            result_str = "无"
        summaries.append(f"- {tool_name}({params_str}) → {result_str}")

    # 记录结构化日志
    slog.tool_call(
        "tool_history_injection",
        message=f"Injecting {len(tool_messages)} tool call summaries",
        data={
            "session_id": session_id,
            "tool_count": len(tool_messages),
            "tool_names": list(set(tool_names)),
        },
        session_id=session_id,
    )

    return "[系统自动注入 - 之前的工具调用历史]\n\n" + "\n".join(summaries)


def build_anti_pattern_context(
    db_session: Session,
    planning_session: AIPlanningSession,
) -> str | None:
    """从 DB 获取相关的 anti-patterns"""
    from app.services.anti_patterns import retrieve_relevant_anti_patterns, format_anti_patterns_for_prompt

    project_ids = _get_session_project_ids(planning_session)
    if not project_ids:
        return None

    try:
        patterns = retrieve_relevant_anti_patterns(
            db_session,
            project_id=_get_active_project_id(planning_session),
            limit=3,
        )
    except Exception:
        logger.warning("Failed to retrieve anti-patterns for context injection", exc_info=True)
        return None

    if not patterns:
        return None

    # 记录结构化日志
    pattern_categories = [p.error_category for p in patterns]
    slog.ai_thinking(
        "anti_pattern_injection",
        message=f"Injecting {len(patterns)} anti-patterns",
        data={
            "session_id": planning_session.id,
            "project_id": _get_active_project_id(planning_session),
            "pattern_count": len(patterns),
            "pattern_categories": pattern_categories,
        },
        session_id=planning_session.id,
    )

    return format_anti_patterns_for_prompt(patterns)


def build_execution_error_context(
    db_session: Session,
    planning_session: AIPlanningSession,
) -> str | None:
    """从 DB 获取最近的执行错误

    当 case_id 存在时，从该用例的最近执行记录中查找。
    当 case_id 为 null 时，从项目的最近执行记录中查找。
    """
    from app.models import TestCaseRun, TestCase

    case_id = planning_session.case_id

    try:
        if case_id:
            # 从该用例的最近执行记录中查找
            latest_run = db_session.execute(
                select(TestCaseRun)
                .where(TestCaseRun.case_id == case_id)
                .order_by(TestCaseRun.id.desc())
                .limit(1)
            ).scalar_one_or_none()
        else:
            # 从项目的最近执行记录中查找
            project_ids = [p.id for p in planning_session.projects]
            if not project_ids:
                return None

            # 查找项目下最近的执行记录
            latest_run = db_session.execute(
                select(TestCaseRun)
                .join(TestCase, TestCaseRun.case_id == TestCase.id)
                .where(TestCase.project_id.in_(project_ids))
                .order_by(TestCaseRun.id.desc())
                .limit(1)
            ).scalar_one_or_none()
    except Exception:
        logger.warning("Failed to query execution errors for context injection", exc_info=True)
        return None

    if not latest_run or not latest_run.report:
        return None

    report = latest_run.report if isinstance(latest_run.report, dict) else {}
    steps = report.get("steps") or []

    errors = []
    error_actions = []
    for step in steps:
        if step.get("status") == "failed":
            action = step.get("action", "unknown")
            target = step.get("target", "unknown")
            error_msg = step.get("error_message", "未知")
            error_actions.append(action)
            errors.append(f"- {action} → {target}: {error_msg}")

    if not errors:
        return None

    # 记录结构化日志
    slog.dsl_execution(
        "execution_error_injection",
        message=f"Injecting {len(errors)} execution errors",
        data={
            "session_id": planning_session.id,
            "case_id": case_id,
            "error_count": len(errors),
            "error_actions": list(set(error_actions)),
        },
        execution_id=latest_run.id,
    )

    return "[系统自动注入 - 最近一次执行的错误]\n\n" + "\n".join(errors)


def build_auto_context_preamble(
    planning_session: AIPlanningSession,
    db_session: Session,
    existing_msg_count: int,
) -> str | None:
    """Build auto-context preamble from various sources.

    Returns a single string containing all context sections, or None if no context.
    This function can be used by both the ReAct loop and the DSL generator.
    """
    preamble_parts = []
    injected_sections = []

    # 1. 项目测试状态和跨会话洞察（现有逻辑）
    session_context = build_session_context_preamble(planning_session, db_session, existing_msg_count)
    if session_context:
        preamble_parts.append(session_context)
        injected_sections.append("session_context")

    # 2. 之前 turn 的工具调用摘要
    tool_summary = build_tool_call_summary(db_session, planning_session.id)
    if tool_summary:
        preamble_parts.append(tool_summary)
        injected_sections.append("tool_call_history")

    # 3. Anti-patterns 上下文
    anti_pattern_context = build_anti_pattern_context(db_session, planning_session)
    if anti_pattern_context:
        preamble_parts.append(anti_pattern_context)
        injected_sections.append("anti_patterns")

    # 4. 执行错误上下文
    error_context = build_execution_error_context(db_session, planning_session)
    if error_context:
        preamble_parts.append(error_context)
        injected_sections.append("execution_errors")

    if not preamble_parts:
        return None

    # 记录结构化日志
    preamble = "\n\n---\n\n".join(preamble_parts)
    slog.ai_thinking(
        "context_injection",
        message=f"Built {len(injected_sections)} context sections: {', '.join(injected_sections)}",
        data={
            "session_id": planning_session.id,
            "injected_sections": injected_sections,
            "preamble_length": len(preamble),
        },
        session_id=planning_session.id,
    )

    return preamble


def inject_auto_context(
    transcript: list[dict[str, str]],
    planning_session: AIPlanningSession,
    db_session: Session,
    existing_msg_count: int,
) -> list[dict[str, str]]:
    """Prepend auto-context preamble to transcript if applicable."""
    preamble = build_auto_context_preamble(planning_session, db_session, existing_msg_count)
    if not preamble:
        return transcript

    return [{"role": "system", "content": preamble}, *transcript]
