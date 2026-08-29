"""Services for AI planning sessions and drafts."""

from __future__ import annotations

import logging


from app.application.planning.analysis_retest_service import (
    auto_update_insights as _auto_update_insights,
    build_analysis_context as _build_analysis_context,
    retest_cases,
    run_analysis_turn as _run_analysis_turn,
    should_run_analysis as _should_run_analysis,
)
from app.application.planning.context_service import (
    build_anti_pattern_context as _build_anti_pattern_context,
    build_auto_context_preamble as _build_auto_context_preamble,
    build_execution_error_context as _build_execution_error_context,
    build_session_context_preamble as _build_session_context_preamble,
    build_tool_call_summary as _build_tool_call_summary,
    categorize_error as _categorize_error,
    inject_auto_context,
)
from app.application.planning.draft_service import (
    _load_a11y_nodes_for_scenario,
    _normalize_base_url,
    delete_planning_draft,
    generate_auto_drafts_for_scenarios,
    generate_planning_drafts,
    stream_generate_planning_drafts,
    update_planning_draft_status,
)
from app.application.planning.execution_inputs import (
    build_input_values_from_session as _build_input_values_from_session,
)
from app.application.planning.save_execute_service import (
    _record_execution_anti_patterns,
    save_and_execute_selected_drafts,
    save_and_execute_selected_drafts_streaming,
)
from app.core.structured_logging import get_structured_logger



logger = logging.getLogger(__name__)
slog = get_structured_logger(__name__)

__all__ = [
    "_auto_update_insights",
    "_build_analysis_context",
    "_build_anti_pattern_context",
    "_build_auto_context_preamble",
    "_build_execution_error_context",
    "_build_session_context_preamble",
    "_build_tool_call_summary",
    "_build_input_values_from_session",
    "_categorize_error",
    "_load_a11y_nodes_for_scenario",
    "_normalize_base_url",
    "_record_execution_anti_patterns",
    "_run_analysis_turn",
    "_should_run_analysis",
    "delete_planning_draft",
    "generate_auto_drafts_for_scenarios",
    "generate_planning_drafts",
    "inject_auto_context",
    "retest_cases",
    "save_and_execute_selected_drafts",
    "save_and_execute_selected_drafts_streaming",
    "stream_generate_planning_drafts",
    "update_planning_draft_status",
]
