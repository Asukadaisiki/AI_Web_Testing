# Phase 1: AI Execution Analysis — Design Spec

## Goal

Evolve the AI planning agent from a "test plan generator" into an "intelligent QA Agent" that can see execution results, analyze failures, and recommend next steps — forming a closed loop: plan → execute → analyze → retest.

## Current State

The AI agent (`test_planning_prompts.py`) has 7 tools but cannot see detailed execution results. After execution completes (`ai_planning.py: save_and_execute_selected_drafts`), a plain-text summary is returned but no AI analysis happens. The loop is open-ended: execution → summary → done.

## Changes

### 1. Three New Analysis Tools (`planning_tools.py`)

#### `get_execution_detail`

- **Input**: `run_id` (required)
- **Output**: Full `ExecutionReport` for a single `TestCaseRun` — per-step status, error messages, locator traces, console/network errors, screenshots
- **Implementation**: Call `get_case_execution(session, run_id)` from `executions.py`, serialize the `StoredCaseExecutionDetail.report` into a compact text summary the LLM can consume. Include: step index, action, target, status, error_message, resolved_by, console errors, network errors. Omit raw locator trace candidates (too verbose) — keep match_strategy and failure_reason only.

#### `get_project_test_status`

- **Input**: no params (uses `project_id` from context)
- **Output**: For each test case in the project: latest run status, passed/failed step counts, last error message. Plus an overall conclusion: `all_passed | partial | all_failed | no_runs`.
- **Implementation**: Query `TestCase` by `project_id`, for each case find the latest `TestCaseRun` via `list_executions(session, project_id=project_id, case_id=case_id, limit=1)`. Aggregate into a per-case summary and project-level verdict.

#### `get_failure_analysis`

- **Input**: `case_id` (optional), `limit` (default 5)
- **Output**: Failure patterns across recent runs — error messages, failure categories, consecutive failure count, flaky score (passed vs failed in last N runs).
- **Implementation**: Query last N `TestCaseRun` records for the case (or all cases in project). Analyze: count consecutive failures, compute pass/fail ratio, categorize errors (locator failure, assertion failure, timeout, etc.), flag as "suspected flaky" if alternating pass/fail pattern detected.

### 2. Upgraded System Prompt (`test_planning_prompts.py`)

#### Role change

```
Before: "你是一个专业的 Web 测试规划助手"
After:  "你是一个面向 Web 自动化测试的智能 QA Agent"
```

#### New actions

Add two actions to the existing `ask_user | call_tool | generate_plan` set:

- **`analyze_results`**: Triggered when execution results are available. AI must output structured analysis following Format B (below).
- **`plan_regression`**: Triggered after analysis identifies failures. AI must output a targeted retest plan (minimal set of cases/scenarios to re-run).

#### New `collected_info` sub-field

Add `test_context` to the JSON response format:

```json
{
  "test_context": {
    "project_id": null,
    "test_point_status": null,
    "last_run_failures": [],
    "suspected_root_cause": null,
    "regression_scope": null,
    "next_action": null
  }
}
```

This field persists across the conversation so the AI maintains awareness of execution state.

#### New output format B (result summary)

When action is `analyze_results`, the `action_input.summary` must follow:

```
1. 任务判断: 结果分析
2. 本轮结论: 全部通过 / 部分通过 / 全部失败
3. 各测试点状态: (per case: name, status, passed/failed steps)
4. 失败点分析: (per failure: step, error, suspected cause ranked by probability)
5. 影响范围评估
6. 建议处理方式
7. 下一步建议: 针对性复测 / 回归测试 / 人工介入 / 测试完成
```

Error cause analysis dimensions (ordered by priority):

1. Locator failure (element changed or removed)
2. Assertion mismatch (product logic change)
3. Timeout / wait condition insufficient
4. Test data issue
5. Environment issue
6. Permission / session expired
7. Network / API error
8. Suspected flaky

#### Task classification rule

Add to prompt: on receiving input, first classify the task as one of:

- New test design
- Targeted retest (after known failure)
- Regression test (after fix)
- Result summary
- Defect analysis

If the user doesn't specify, infer from context (e.g., if `test_context` shows failures → retest/analysis; if no prior runs → new test).

#### Test point success definition

Add explicit rule: "A test point (project) is successful only when ALL test cases' latest runs are `passed`. If any case is `failed`, the test point is `partial` or `all_failed`."

### 3. Post-Execution Auto-Analysis (`ai_planning.py`)

#### Non-streaming flow change

In `save_and_execute_selected_drafts()`, after building `execution_summaries`:

```python
# After all cases executed and summaries built:
has_failure = any(s.status != "passed" for s in execution_summaries)

if has_failure:
    # 1. Build analysis context message
    analysis_context = _build_analysis_context(execution_summaries, db_session)

    # 2. Inject into conversation and call AI
    analysis_response = _run_analysis_turn(
        session_id=session.id,
        analysis_context=analysis_context,
        db_session=db_session,
        project_id=project_id,
    )

    # 3. Persist analysis as a new AIPlanningMessage
    # 4. Include analysis in the returned AIPlanningTurnResponse
    response.assistant_message = analysis_response.assistant_message
    response.execution_analysis = analysis_response.analysis_payload
```

#### Streaming flow change

In `save_and_execute_selected_drafts_streaming()`, after yielding the final `done` event:

```python
# After yield {"type": "done", ...}
if has_failure:
    yield {"type": "status", "phase": "analyzing", "message": "正在分析执行结果..."}
    # Call AI analysis (non-streaming for simplicity, or streaming if needed)
    analysis = _run_analysis_turn(...)
    yield {"type": "analysis_complete", "analysis": analysis_payload}
```

#### `_build_analysis_context()` helper

Constructs a structured context message from execution summaries:

```
本轮执行已完成，请分析以下结果：

项目: {project_name}
测试点总数: {N}

测试结果:
- {case_name}: {status} ({passed_steps}/{total_steps}步) [失败步骤: step {i} - {error}]
...

请使用 analyze_results 模式输出分析报告。
```

#### `_run_analysis_turn()` helper

Creates a minimal transcript with the analysis context as a user message, then calls `run_planning_turn()` (synchronous wrapper). The AI sees the execution data, may call tools to investigate (e.g., `get_failure_analysis` to check history), and outputs an analysis via `generate_plan` using Format B.

```python
def _run_analysis_turn(session, db_session, project_id, execution_summaries):
    # 1. Build context message
    context_msg = _build_analysis_context(execution_summaries, db_session)
    # 2. Create minimal transcript
    transcript = [{"role": "user", "content": context_msg}]
    # 3. Call existing agent (sync)
    return run_planning_turn(
        transcript=transcript,
        existing_requirements=None,
        db_session=db_session,
        project_id=project_id,
    )
```

The analysis response's `assistant_message` is persisted as an `AIPlanningMessage` in the session and included in the frontend response.

#### Schema additions (`schemas/ai_planning.py`)

Add `ExecutionAnalysis` model:

```python
class ExecutionAnalysis(DSLModel):
    conclusion: str                           # all_passed | partial | all_failed
    case_results: list[CaseAnalysisResult]    # Per-case breakdown
    failure_details: list[FailureDetail]      # Detailed failure analysis
    suspected_root_cause: str | None
    impact_scope: str | None
    recommended_action: str                   # targeted_retest | regression | manual | done
    recommended_scope: str | None             # current | adjacent | module | core

class CaseAnalysisResult(DSLModel):
    case_id: int
    case_name: str
    status: str
    passed_steps: int
    total_steps: int
    failure_summary: str | None

class FailureDetail(DSLModel):
    case_name: str
    step_index: int
    action: str
    target: str | None
    error_message: str | None
    suspected_cause: str
    cause_probability: str                    # high | medium | low
```

Add `execution_analysis: ExecutionAnalysis | None = None` to `AIPlanningTurnResponse`.

Add `test_context: dict | None = None` to `AIPlanningRequirements` (stored as JSON in session).

## Files Changed

| File | Change |
|------|--------|
| `backend/app/ai/planning_tools.py` | Add 3 tools + handlers |
| `backend/app/ai/test_planning_prompts.py` | Upgrade role, add actions, add Format B |
| `backend/app/ai/test_planning_agent.py` | Handle `analyze_results` and `plan_regression` actions |
| `backend/app/services/ai_planning.py` | Add post-execution auto-analysis in both sync and streaming paths |
| `backend/app/schemas/ai_planning.py` | Add `ExecutionAnalysis` schema and `test_context` field to requirements |

## Not Changed

- No new database tables (Phase 3)
- No frontend changes (analysis flows through existing message/streaming infrastructure)
- No DSL generation changes
- No execution engine changes

## Success Criteria

1. AI can answer "最近测试结果怎么样" by calling `get_project_test_status` and summarizing
2. After executing test cases with failures, AI automatically generates an analysis report with root cause hypotheses
3. AI can recommend next steps: targeted retest scope, regression scope, or mark as done
4. Analysis results persist in the session and are visible in the conversation history
5. Test point success is correctly determined (all cases passed = success)
