"""Planning draft generation and lifecycle use cases."""

from __future__ import annotations

import logging
from typing import Any

from sqlalchemy import select

from app.application.planning.context_service import build_execution_error_context
from app.application.planning.project_context import (
    get_active_project_id as _get_active_project_id,
    get_owned_session as _get_session,
    get_session_project_ids as _get_session_project_ids,
)
from app.application.planning.presenters import (
    to_draft_schema as _to_draft_schema,
    to_session_schema as _to_session_schema,
)
from app.core.config import get_settings
from app.core.structured_logging import get_structured_logger
from sqlalchemy.orm import Session

from app.models import AIPlanningDraft, AIPlanningMessage, DslGenerationRun
from app.models.ai_planning_tool_result import AIPlanningToolResult
from app.schemas.ai_planning import (
    AIPlanningDraft as AIPlanningDraftSchema,
    AIPlanningRequirements,
    AIPlanningTurnResponse,
    GenerateAIPlanningDraftsRequest,
    UpdateAIPlanningDraftStatusRequest,
)
from app.schemas.dsl import GenerateDslRequest
from app.services.cases import EntityNotFoundError
from app.services.dsl import generate_dsl_case


logger = logging.getLogger(__name__)
slog = get_structured_logger(__name__)


def generate_auto_drafts_for_scenarios(
    *,
    db_session: Session,
    planning_session_id: int,
    scenario_keys: list[str],
    actor_user_id: int,
) -> list[AIPlanningDraftSchema]:
    response = generate_planning_drafts(
        db_session,
        planning_session_id,
        GenerateAIPlanningDraftsRequest(scenario_keys=scenario_keys),
        actor_user_id=actor_user_id,
    )
    return response.drafts


def _load_a11y_nodes_for_scenario(
    session: Session,
    planning_session_id: int,
    *,
    scenario: dict | None = None,
) -> list[dict] | None:
    """Load a11y_nodes from ALL explore results for this session.

    Aggregates pages across multiple explore_flow / explore_page calls,
    deduplicating by URL so that re-exploring the same page with different
    actions doesn't produce duplicate nodes.  Earlier calls that explored
    more pages are no longer silently discarded.
    """
    # Step 1: Query ALL explore results for this session
    result_records = list(session.scalars(
        select(AIPlanningToolResult)
        .where(AIPlanningToolResult.session_id == planning_session_id)
        .where(AIPlanningToolResult.tool_name.in_(["explore_flow", "explore_page"]))
        .order_by(AIPlanningToolResult.id.asc())
    ).all())

    # Step 2: Check if any records exist
    if not result_records:
        logger.warning(
            "[_load_a11y_nodes] NO RECORD FOUND in AIPlanningToolResult for session %d. "
            "This means tool results were NOT persisted. Check stream_planning_turn logic.",
            planning_session_id,
        )
        all_results = session.scalars(
            select(AIPlanningToolResult)
            .where(AIPlanningToolResult.session_id == planning_session_id)
        ).all()
        logger.warning(
            "[_load_a11y_nodes] Total tool results for session %d: %d",
            planning_session_id,
            len(all_results),
        )
        if all_results:
            for r in all_results[:5]:
                logger.warning(
                    "[_load_a11y_nodes]   - id=%d, tool=%s, raw_type=%s",
                    r.id, r.tool_name, type(r.raw_result_json).__name__,
                )
        return None

    logger.info(
        "[_load_a11y_nodes] Found %d explore records for session %d",
        len(result_records), planning_session_id,
    )

    # Step 3: Aggregate pages from ALL records, deduplicating by URL
    # Key: normalized URL → best (most nodes) page data
    pages_by_url: dict[str, dict] = {}
    state_counter = 0

    for record in result_records:
        raw = record.raw_result_json
        if not isinstance(raw, dict):
            logger.warning(
                "[_load_a11y_nodes]   - id=%d tool=%s: raw_result_json is NOT a dict (type=%s), skipping",
                record.id, record.tool_name, type(raw).__name__,
            )
            continue

        # Extract pages from explore_flow result
        if "pages" in raw:
            for page in raw.get("pages", []):
                url = (page.get("url") or "").strip().rstrip("/").lower()
                if not url:
                    continue

                # Check if page has actions (new format)
                actions = page.get("actions", [])
                if actions:
                    # New format: page -> actions -> a11y_nodes
                    # Keep all actions with their nodes
                    existing = pages_by_url.get(url)
                    if existing is None:
                        pages_by_url[url] = {
                            "url": page.get("url"),
                            "page_state": f"S{state_counter}",
                            "description": page.get("description", ""),
                            "actions": actions,
                        }
                        state_counter += 1
                        logger.info(
                            "[_load_a11y_nodes]   - id=%d: page url=%s, actions=%d (new)",
                            record.id, url, len(actions),
                        )
                    else:
                        # Merge actions from different records
                        existing_actions = existing.get("actions", [])
                        existing_actions.extend(actions)
                        logger.info(
                            "[_load_a11y_nodes]   - id=%d: page url=%s, actions=%d (merged, total=%d)",
                            record.id, url, len(actions), len(existing_actions),
                        )
                else:
                    # Old format: page -> a11y_nodes
                    nodes = page.get("a11y_nodes", [])
                    if not nodes:
                        continue
                    existing = pages_by_url.get(url)
                    if existing is None or len(nodes) > len(existing.get("a11y_nodes", [])):
                        # Keep the version with more nodes
                        pages_by_url[url] = page
                        logger.info(
                            "[_load_a11y_nodes]   - id=%d: page url=%s, nodes=%d (new/better)",
                            record.id, url, len(nodes),
                        )
                    else:
                        logger.info(
                            "[_load_a11y_nodes]   - id=%d: page url=%s, nodes=%d (kept existing %d)",
                            record.id, url, len(nodes), len(existing.get("a11y_nodes", [])),
                        )

        # Extract a11y_nodes directly from explore_page result
        elif "a11y_nodes" in raw:
            nodes = raw.get("a11y_nodes") or []
            url = (raw.get("url") or "").strip().rstrip("/").lower()
            if url and nodes:
                existing = pages_by_url.get(url)
                if existing is None or len(nodes) > len(existing.get("a11y_nodes", [])):
                    pages_by_url[url] = {"url": raw.get("url"), "page_state": f"S{state_counter}", "a11y_nodes": nodes}
                    state_counter += 1

    if not pages_by_url:
        logger.warning("[_load_a11y_nodes] All explore records had empty pages/nodes!")
        return None

    # Step 4: Assign sequential page_states and flatten nodes
    all_nodes: list[dict] = []
    state_counter = 0
    for url in pages_by_url:
        page = pages_by_url[url]
        state = f"S{state_counter}"
        page["page_state"] = state
        state_counter += 1

        # Handle new format (page -> actions -> a11y_nodes)
        actions = page.get("actions", [])
        if actions:
            for action in actions:
                action_nodes = action.get("a11y_nodes", [])
                action_desc = action.get("action_description", "")
                logger.info(
                    "[_load_a11y_nodes] aggregated page: state=%s, url=%s, action=%s, nodes=%d",
                    state, page.get("url", "?"), action_desc, len(action_nodes),
                )
                for n in action_nodes:
                    n = dict(n)
                    n["page_state"] = state
                    n["action_description"] = action_desc
                    all_nodes.append(n)
        else:
            # Handle old format (page -> a11y_nodes)
            a11y_nodes = page.get("a11y_nodes", [])
            logger.info(
                "[_load_a11y_nodes] aggregated page: state=%s, url=%s, nodes=%d",
                state, page.get("url", "?"), len(a11y_nodes),
            )
            for n in a11y_nodes:
                n = dict(n)
                n["page_state"] = state
                all_nodes.append(n)

    logger.info(
        "[_load_a11y_nodes] total: %d pages, %d nodes from %d explore records",
        len(pages_by_url), len(all_nodes), len(result_records),
    )
    return all_nodes if all_nodes else None


def generate_planning_drafts(
    session: Session,
    planning_session_id: int,
    payload: GenerateAIPlanningDraftsRequest,
    *,
    actor_user_id: int,
) -> AIPlanningTurnResponse:
    planning_session = _get_session(session, planning_session_id, actor_user_id=actor_user_id)
    project_ids = _get_session_project_ids(planning_session)
    if not project_ids:
        raise ValueError("请先关联至少一个项目再生成 DSL 草稿。")
    plan = planning_session.plan_json or {}
    scenarios = {
        item["scenario_key"]: item
        for item in plan.get("scenarios", [])
        if isinstance(item, dict) and isinstance(item.get("scenario_key"), str)
    }
    drafts: list[AIPlanningDraftSchema] = []
    base_url = _normalize_base_url(planning_session.requirements_json or {})
    logger.info(
        "[generate_drafts] session=%d, requirements_json_keys=%s, entry_url_or_page=%s, base_url=%s",
        planning_session_id,
        list((planning_session.requirements_json or {}).keys()),
        (planning_session.requirements_json or {}).get("entry_url_or_page"),
        base_url,
    )
    invalid_scenarios: list[str] = []

    # Build user_context: original requirements summary for DSL generator
    _req = planning_session.requirements_json or {}
    _user_ctx_parts: list[str] = []
    if _req.get("app_under_test"):
        _user_ctx_parts.append(f"被测系统：{_req['app_under_test']}")
    if _req.get("business_goal"):
        _user_ctx_parts.append(f"业务目标：{_req['business_goal']}")
    if _req.get("core_user_flow"):
        _user_ctx_parts.append(f"核心流程：{_req['core_user_flow']}")
    if _req.get("main_assertions"):
        _user_ctx_parts.append(f"关键断言：{'; '.join(_req['main_assertions'])}")
    if _req.get("test_data_or_account"):
        _user_ctx_parts.append(f"测试数据：{_req['test_data_or_account']}")
    if _req.get("scope_limits"):
        _user_ctx_parts.append(f"范围限制：{_req['scope_limits']}")

    # 注入执行错误上下文
    error_context = build_execution_error_context(session, planning_session)
    if error_context:
        _user_ctx_parts.append(error_context)

    user_context = "\n".join(_user_ctx_parts) if _user_ctx_parts else None
    if user_context:
        logger.info("[generate_drafts] user_context built, len=%d", len(user_context))

    for scenario_key in payload.scenario_keys:
        scenario = scenarios.get(scenario_key)
        if scenario is None:
            invalid_scenarios.append(scenario_key)
            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=f"场景 {scenario_key} 不存在",
                status="failed",
                dsl_generation_id=None,
                dsl_case_json=None,
                warnings_json=[f"场景 '{scenario_key}' 未在 AI 生成的测试计划中找到"],
                normalization_notes_json=[],
                error_message=f"场景 '{scenario_key}' 不存在于当前测试计划中。",
            )
            session.add(record)
            session.flush()
            drafts.append(_to_draft_schema(record))
            continue

        existing = session.scalar(
            select(AIPlanningDraft).where(
                AIPlanningDraft.session_id == planning_session.id,
                AIPlanningDraft.scenario_key == scenario_key,
            )
        )
        # Self-healing: reuse successful drafts; regenerate failed ones with anti-pattern learning
        retry_reason_code: str | None = None
        if existing is not None:
            if existing.status == "generated":
                drafts.append(_to_draft_schema(existing))
                continue
            if existing.status in ("imported", "rejected"):
                drafts.append(_to_draft_schema(existing))
                continue
            # status == "failed": delete and regenerate with anti-patterns as few-shot
            prev_error = existing.error_message or ""
            if "缺少页面导航" in prev_error or "缺少导航" in prev_error:
                retry_reason_code = "missing_navigation"
            elif "缺少 input" in prev_error or "输入步骤" in prev_error:
                retry_reason_code = "missing_step"
            elif "capture_text" in prev_error:
                retry_reason_code = "missing_capture_text"
            else:
                retry_reason_code = "invalid_structure"
            logger.info(
                "Self-healing: deleting failed draft #%d for scenario '%s', retry=%s",
                existing.id, scenario_key, retry_reason_code,
            )
            session.delete(existing)
            session.flush()

        # Load a11y_nodes from the most recent explore result
        a11y_nodes_raw = _load_a11y_nodes_for_scenario(session, planning_session_id, scenario=scenario)
        logger.info(
            "[generate_drafts] scenario='%s', a11y_nodes_raw=%s, type=%s, len=%s",
            scenario_key,
            "None" if a11y_nodes_raw is None else "list",
            type(a11y_nodes_raw).__name__,
            len(a11y_nodes_raw) if a11y_nodes_raw else 0,
        )
        if not a11y_nodes_raw:
            logger.warning(
                "Skipping DSL generation for scenario '%s': no A11y elements collected",
                scenario_key,
            )
            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=scenario["title"],
                status="failed",
                dsl_generation_id=None,
                dsl_case_json=None,
                warnings_json=[],
                normalization_notes_json=[],
                error_message="页面元素采集失败（探索超时或 URL 不可达），无法生成 DSL 草案。请检查入口 URL 或稍后重试。",
            )
            session.add(record)
            session.flush()
            drafts.append(_to_draft_schema(record))
            continue

        try:
            flow_steps = scenario.get("flow_steps", [])
            scenario_variables = scenario.get("variables", []) or []
            settings_local = get_settings()

            logger.info(
                "[session:%d] DSL generation for scenario '%s': flow_steps=%d, a11y_nodes=%d, has_page_elements=%s, flow_steps_enabled=%s, scenario_variables=%d",
                planning_session_id, scenario_key, len(flow_steps), len(a11y_nodes_raw),
                bool(scenario.get("page_elements")), settings_local.ai_planning_flow_steps_enabled,
                len(scenario_variables),
            )

            if flow_steps and settings_local.ai_planning_flow_steps_enabled:
                from app.ai.dsl_generator import generate_segmented_case_draft

                a11y_nodes_by_state: dict[str, list[dict]] = {}
                for n in a11y_nodes_raw:
                    ps = n.get("page_state", "S0") or "S0"
                    a11y_nodes_by_state.setdefault(ps, []).append(n)

                logger.info(
                    "[session:%d] Using segmented DSL generation: a11y_nodes_by_state=%s",
                    planning_session_id,
                    {k: len(v) for k, v in a11y_nodes_by_state.items()},
                )

                case_obj, gen_warnings, gen_notes, gen_meta = generate_segmented_case_draft(
                    payload=GenerateDslRequest(
                        prompt=scenario["draft_prompt"],
                        base_url=base_url,
                        actor_user_id=actor_user_id,
                        project_id=_get_active_project_id(planning_session),
                        case_id=planning_session.case_id,
                        current_steps=payload.current_steps,
                        current_input_contract=payload.current_input_contract,
                        current_output_contract=payload.current_output_contract,
                        preserve_contracts=payload.preserve_contracts,
                        flow_steps=flow_steps,
                        scenario_variables=scenario_variables or None,
                        user_context=user_context,
                        retry_reason_code=retry_reason_code,
                    ),
                    flow_steps=flow_steps,
                    a11y_nodes_by_state=a11y_nodes_by_state,
                    scenario_variables=scenario_variables or None,
                    db_session=session,
                )
                # Wrap to match the existing interface
                generated = type("GeneratedHolder", (), {
                    "case": case_obj,
                    "warnings": gen_warnings,
                    "normalization_notes": gen_notes,
                    "generation_id": None,
                })()
            else:
                # No structured flow_steps from scenario. Pass a11y_nodes (grouped
                # by page_state) via payload so generate_dsl_case → segmented
                # generator still has element context.
                a11y_nodes_by_state: dict[str, list[dict]] = {}
                for n in a11y_nodes_raw:
                    ps = n.get("page_state", "S0") or "S0"
                    a11y_nodes_by_state.setdefault(ps, []).append(n)

                logger.info(
                    "[session:%d] Using single-segment DSL generation: a11y_nodes=%d, a11y_nodes_by_state=%s",
                    planning_session_id, len(a11y_nodes_raw), {k: len(v) for k, v in a11y_nodes_by_state.items()},
                )
                generated = generate_dsl_case(
                    session,
                    GenerateDslRequest(
                        prompt=scenario["draft_prompt"],
                        base_url=base_url,
                        actor_user_id=actor_user_id,
                        project_id=_get_active_project_id(planning_session),
                        case_id=planning_session.case_id,
                        current_steps=payload.current_steps,
                        current_input_contract=payload.current_input_contract,
                        current_output_contract=payload.current_output_contract,
                        preserve_contracts=payload.preserve_contracts,
                        a11y_nodes_by_state=a11y_nodes_by_state or None,
                        scenario_variables=scenario_variables or None,
                        user_context=user_context,
                        retry_reason_code=retry_reason_code,
                    ),
                )
            # --- Locator preflight ---
            dsl_dict = generated.case.model_dump(mode="json")
            preflight_warnings: list[str] = []
            preflight_rejected = False

            if not a11y_nodes_raw:
                preflight_rejected = True
                raise ValueError(
                    "No page exploration data available for locator verification. "
                    "AI must call explore_page/explore_flow to collect page elements "
                    "before generating DSL. Currently no explored elements exist."
                )

            try:
                from app.ai.locator_preflight import apply_preflight_to_dsl
                dsl_dict = apply_preflight_to_dsl(dsl_dict, a11y_nodes_raw)
                pf = dsl_dict.pop("_preflight", {})
                preflight_warnings = pf.get("warnings", [])
                preflight_confidence = pf.get("locator_confidence", "unknown")
                step_results = pf.get("step_results", [])
                # --- Preflight gate: reject low-quality locators ---
                total_targets = len(step_results)
                unmatched = sum(1 for sr in step_results if sr.get("match_count", 0) == 0)
                low_conf = sum(1 for sr in step_results if sr.get("confidence") == "low")
                unmatched_ratio = unmatched / total_targets if total_targets > 0 else 0
                low_ratio = low_conf / total_targets if total_targets > 0 else 0

                if unmatched_ratio > 0.5:
                    preflight_rejected = True
                    unresolved_states: set[str] = set()
                    for sr in step_results:
                        if sr.get("match_count", 0) == 0 and sr.get("target"):
                            unresolved_states.add(sr["target"][:80])
                    rejection_msg = (
                        f"Preflight gate: {unmatched}/{total_targets} steps have targets "
                        f"not found in {len(a11y_nodes_raw)} explored elements.\n"
                        f"Missing: {', '.join(sorted(unresolved_states)[:5])}"
                    )
                    raise ValueError(rejection_msg)

                if low_ratio > 0.5 and unmatched_ratio < 0.5:
                    preflight_rejected = True
                    _low_suggestions: list[str] = []
                    for sr in step_results:
                        if sr.get("confidence") != "low":
                            continue
                        _t = sr.get("target", "")[:60]
                        _alts: list[str] = []
                        for me in sr.get("matched_elements", [])[:2]:
                            _text = (me.get("text") or "").strip()
                            if _text and _text not in _alts and f"'{_text}'" != _t[:len(_text)+2]:
                                _alts.append(f"'{_text}'")
                        _hint = f"  {_t} → 建议用 {', '.join(_alts)}" if _alts else f"  {_t}"
                        _low_suggestions.append(_hint)
                    rejection_msg = (
                        f"Preflight gate: {low_conf}/{total_targets} steps ({low_ratio*100:.0f}%) "
                        f"have low-confidence locators.\n"
                        f"请使用页面元素清单中的实际可见文本作为 target：\n"
                        + "\n".join(_low_suggestions[:8])
                    )
                    raise ValueError(rejection_msg)
                logger.info(
                    "Preflight for scenario '%s': confidence=%s, warnings=%d, elements=%d, unmatched=%d/%d",
                    scenario_key, preflight_confidence, len(preflight_warnings),
                    len(a11y_nodes_raw), unmatched, total_targets,
                )
            except Exception as exc:
                if preflight_rejected:
                    logger.warning("Preflight gate rejected scenario '%s': %s", scenario_key, exc)
                    raise
                logger.warning("Preflight failed for scenario '%s': %s", scenario_key, exc)

            all_warnings = list(generated.warnings) + preflight_warnings

            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=scenario["title"],
                status="generated",
                dsl_generation_id=(
                    generated.generation_id if session.get(DslGenerationRun, generated.generation_id) is not None else None
                ),
                dsl_case_json=dsl_dict,
                warnings_json=all_warnings,
                normalization_notes_json=generated.normalization_notes,
                error_message=None,
            )
        except Exception as exc:
            logger.error(
                "Failed to generate DSL case for scenario '%s' in session %s",
                scenario_key,
                planning_session.id,
                exc_info=True,
            )
            record = AIPlanningDraft(
                session_id=planning_session.id,
                scenario_key=scenario_key,
                title=scenario["title"],
                status="failed",
                dsl_generation_id=None,
                dsl_case_json=None,
                warnings_json=[],
                normalization_notes_json=[],
                error_message=str(exc),
            )
            # --- Self-healing: record anti-pattern for failed draft ---
            try:
                from app.services.anti_patterns import (
                    record_anti_pattern,
                    MISSING_NAVIGATION, MISSING_STEP,
                    MISSING_CAPTURE_TEXT, MISSING_INPUT_BEFORE_ASSERT,
                )
                err_msg = str(exc)
                # Classify error message to anti-pattern category
                if "缺少页面导航" in err_msg or "缺少导航" in err_msg:
                    category = MISSING_NAVIGATION
                elif "缺少 input" in err_msg or "输入步骤" in err_msg:
                    category = MISSING_INPUT_BEFORE_ASSERT
                elif "capture_text" in err_msg and "assert" in err_msg:
                    category = MISSING_CAPTURE_TEXT
                else:
                    category = MISSING_STEP
                # Capture the wrong step snippet from the error context if available
                snippet: dict[str, Any] = {"error": err_msg[:500]}
                context_note = err_msg[:500] if len(err_msg) <= 500 else err_msg[:497] + "..."
                record_anti_pattern(
                    session,
                    error_category=category,
                    wrong_snippet=snippet,
                    context_note=context_note,
                    source="auto",
                    project_id=_get_active_project_id(planning_session),
                )
            except Exception as ap_exc:
                logger.warning("Failed to record anti-pattern: %s", ap_exc)
        session.add(record)
        session.flush()
        drafts.append(_to_draft_schema(record))

    message = "已根据所选场景生成 DSL 草案。"
    failed_count = sum(1 for d in drafts if d.status == "failed")
    generated_count = sum(1 for d in drafts if d.status == "generated")
    first_error = next((d.error_message for d in drafts if d.error_message), None)
    if generated_count == 0 and failed_count > 0:
        message = f"所有 {failed_count} 个草案均生成失败。"
        if first_error:
            message += f"\n失败原因：{first_error}"
        message += "\n请检查入口 URL 是否可访问后重试。"
    elif failed_count > 0:
        message = f"已生成 {generated_count} 个 DSL 草案，{failed_count} 个失败。"
    if invalid_scenarios:
        message += f" 注意：以下场景不存在于当前测试计划中：{', '.join(invalid_scenarios)}"

    planning_session.status = "drafts_ready"
    session.add(
        AIPlanningMessage(
            session_id=planning_session.id,
            role="assistant",
            turn_type="plan",
            content=message,
            structured_payload_json={
                "type": "draft_generation_result",
                "drafts": [item.model_dump(mode="json") for item in drafts],
            },
        )
    )
    session.commit()
    session.refresh(planning_session)

    return AIPlanningTurnResponse(
        assistant_message=message,
        session_status="drafts_ready",
        requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        missing_slots=planning_session.missing_slots_json or [],
        suggested_questions=[],
        plan=_to_session_schema(planning_session).plan,
        drafts=drafts,
        next_action="drafts_generated",
        tool_calls=[],
    )


def stream_generate_planning_drafts(
    session: Session,
    planning_session_id: int,
    payload: GenerateAIPlanningDraftsRequest,
    *,
    actor_user_id: int,
    session_factory=None,
):
    """Generator: yield draft_generating events, then delegate to generate_planning_drafts."""
    from app.services.sse_event_log import EventLogWriter
    event_log = EventLogWriter(
        session_factory=session_factory,
        session_id=planning_session_id,
        flush_interval=3,
    )

    logger.info(
        "[session:%d] Draft generation start, scenarios=%s",
        planning_session_id, payload.scenario_keys,
    )
    for scenario_key in payload.scenario_keys:
        logger.info("[session:%d] Generating draft for scenario '%s'", planning_session_id, scenario_key)
        event = {
            "type": "draft_generating",
            "scenario_key": scenario_key,
            "message": f"正在生成 {scenario_key} 的 DSL...",
        }
        event_log.write("draft_generating", event)
        yield event

    result = generate_planning_drafts(
        session,
        planning_session_id,
        payload,
        actor_user_id=actor_user_id,
    )
    complete_event = {
        "type": "turn_complete",
        "session_status": result.session_status,
        "payload": {
            "assistant_message": result.assistant_message,
            "drafts": [item.model_dump(mode="json") for item in result.drafts],
            "plan": result.plan.model_dump(mode="json") if result.plan else None,
        },
    }
    event_log.write("turn_complete", complete_event)
    event_log.flush()
    yield complete_event
    return result


def update_planning_draft_status(
    session: Session,
    draft_id: int,
    payload: UpdateAIPlanningDraftStatusRequest,
    *,
    actor_user_id: int,
) -> AIPlanningDraftSchema:
    draft = session.get(AIPlanningDraft, draft_id)
    if draft is None:
        raise EntityNotFoundError(f"AI planning draft {draft_id} not found.")
    _get_session(session, draft.session_id, actor_user_id=actor_user_id)
    draft.status = payload.status
    session.add(draft)
    session.commit()
    session.refresh(draft)
    return _to_draft_schema(draft)


def delete_planning_draft(
    session: Session,
    draft_id: int,
    *,
    actor_user_id: int,
) -> None:
    """Delete a single planning draft (owner only)."""
    draft = session.get(AIPlanningDraft, draft_id)
    if draft is None:
        raise EntityNotFoundError(f"AI planning draft {draft_id} not found.")
    # Verify the user owns the parent session
    _get_session(session, draft.session_id, actor_user_id=actor_user_id)
    session.delete(draft)
    session.commit()



def _normalize_base_url(requirements_json: dict) -> str | None:
    value = requirements_json.get("entry_url_or_page")
    if isinstance(value, str) and value.startswith(("http://", "https://")):
        return value
    return None
