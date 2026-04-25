# AI Execution Analysis Phase 1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evolve the AI agent from a test plan generator into an intelligent QA Agent that can see execution results, analyze failures, and recommend next steps.

**Architecture:** Add 3 new analysis tools to the planning agent, upgrade the system prompt to QA Agent role, and inject auto-analysis after execution completes. The execution-to-analysis loop reuses the existing ReAct agent with a constructed analysis context message.

**Tech Stack:** Python, SQLAlchemy, Pydantic, FastAPI (existing stack)

**Design spec:** `docs/superpowers/specs/2026-04-25-ai-execution-analysis-phase1-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `backend/app/schemas/ai_planning.py` | Add `ExecutionAnalysis`, `CaseAnalysisResult`, `FailureDetail` schemas; add `test_context` to `AIPlanningRequirements`; add `execution_analysis` to `AIPlanningTurnResponse` |
| `backend/app/ai/planning_tools.py` | Add 3 new tools: `get_execution_detail`, `get_project_test_status`, `get_failure_analysis` + their handlers |
| `backend/app/ai/test_planning_prompts.py` | Upgrade role to QA Agent; add `analyze_results`/`plan_regression` actions; add Format B; add task classification rules |
| `backend/app/ai/test_planning_agent.py` | Handle `analyze_results` and `plan_regression` actions in the agent loop |
| `backend/app/services/ai_planning.py` | Add `_build_analysis_context()` and `_run_analysis_turn()` helpers; modify sync and streaming execution flows |
| `backend/tests/unit/test_planning_tools.py` | Tests for 3 new tools |
| `backend/tests/unit/test_execution_analysis.py` | Tests for analysis context builder and auto-analysis flow |

---

### Task 1: Add Analysis Schemas

**Files:**
- Modify: `backend/app/schemas/ai_planning.py:20-27` (AIPlanningRequirements)
- Modify: `backend/app/schemas/ai_planning.py:163-175` (AIPlanningTurnResponse)

- [ ] **Step 1: Add `test_context` field to `AIPlanningRequirements`**

In `backend/app/schemas/ai_planning.py`, add after `scope_limits` field (line 27):

```python
class AIPlanningRequirements(DSLModel):
    app_under_test: str | None = Field(default=None, max_length=500)
    business_goal: str | None = Field(default=None, max_length=1000)
    entry_url_or_page: str | None = Field(default=None, max_length=500)
    core_user_flow: str | None = Field(default=None, max_length=2000)
    main_assertions: list[str] = Field(default_factory=list)
    test_data_or_account: str | None = Field(default=None, max_length=1000)
    scope_limits: str | None = Field(default=None, max_length=1000)
    test_context: dict[str, Any] | None = Field(default=None, description="Persistent execution context: last_run_status, failures, root cause, regression scope.")
```

- [ ] **Step 2: Add analysis schema classes**

In `backend/app/schemas/ai_planning.py`, add before `AIPlanningTurnResponse` (before line 163):

```python
class FailureDetail(DSLModel):
    case_name: str = Field(min_length=1)
    step_index: int = Field(ge=0)
    action: str = Field(min_length=1)
    target: str | None = None
    error_message: str | None = None
    suspected_cause: str = Field(min_length=1)
    cause_probability: Literal["high", "medium", "low"] = "medium"


class CaseAnalysisResult(DSLModel):
    case_id: int = Field(ge=1)
    case_name: str = Field(min_length=1)
    status: str = Field(min_length=1)
    passed_steps: int = Field(ge=0)
    total_steps: int = Field(ge=0)
    failure_summary: str | None = None


class ExecutionAnalysis(DSLModel):
    conclusion: Literal["all_passed", "partial", "all_failed"] = "all_passed"
    case_results: list[CaseAnalysisResult] = Field(default_factory=list)
    failure_details: list[FailureDetail] = Field(default_factory=list)
    suspected_root_cause: str | None = None
    impact_scope: str | None = None
    recommended_action: Literal["targeted_retest", "regression", "manual", "done"] = "done"
    recommended_scope: str | None = None
```

- [ ] **Step 3: Add `execution_analysis` field to `AIPlanningTurnResponse`**

In `AIPlanningTurnResponse`, add after `execution_summaries` (line 174):

```python
    execution_analysis: ExecutionAnalysis | None = None
```

- [ ] **Step 4: Run existing tests to verify no breakage**

Run: `cd backend && uv run pytest tests/unit/test_planning_tools.py tests/unit/test_planning_agent.py -v`
Expected: All existing tests pass. New fields have defaults so existing code is unaffected.

- [ ] **Step 5: Commit**

```bash
git add backend/app/schemas/ai_planning.py
git commit -m "feat: add ExecutionAnalysis schemas for AI result analysis"
```

---

### Task 2: Add `get_execution_detail` Tool

**Files:**
- Modify: `backend/app/ai/planning_tools.py` (add handler + registry entry)
- Test: `backend/tests/unit/test_planning_tools.py`

- [ ] **Step 1: Write the failing test**

Add to `backend/tests/unit/test_planning_tools.py`:

```python
class TestGetExecutionDetail:
    """Tests for _handle_get_execution_detail handler."""

    def test_returns_step_level_detail(self, db_session: Session) -> None:
        """Should return per-step status and error info for a specific run."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="Detail Case",
                description=None,
                steps=[{"action": "goto", "value": "/test"}],
            ),
            actor_user_id=1,
        )
        db_session.flush()

        execution = TestCaseRun(
            case_id=case.id,
            project_id=1,
            triggered_by=1,
            status="failed",
            report={
                "status": "failed",
                "steps": [
                    {"step_index": 0, "action": "goto", "target": None, "value": "/test", "status": "passed", "error_message": None, "resolved_by": None},
                    {"step_index": 1, "action": "click", "target": "#btn", "value": None, "status": "failed", "error_message": "Element not found", "resolved_by": None},
                ],
            },
        )
        db_session.add(execution)
        db_session.commit()

        from app.ai.planning_tools import _handle_get_execution_detail

        result = _handle_get_execution_detail(
            params={"run_id": str(execution.id)},
            db_session=db_session,
            project_id=1,
        )
        assert result["id"] == execution.id
        assert result["status"] == "failed"
        assert len(result["steps"]) == 2
        assert result["steps"][1]["status"] == "failed"
        assert result["steps"][1]["error_message"] == "Element not found"

    def test_missing_run_id_returns_error(self, db_session: Session) -> None:
        """Should return error when run_id is missing."""
        from app.ai.planning_tools import _handle_get_execution_detail

        result = _handle_get_execution_detail(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert "error" in result

    def test_nonexistent_run_returns_error(self, db_session: Session) -> None:
        """Should return error when run_id does not exist."""
        from app.ai.planning_tools import _handle_get_execution_detail

        result = _handle_get_execution_detail(
            params={"run_id": "99999"},
            db_session=db_session,
            project_id=1,
        )
        assert "error" in result
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_planning_tools.py::TestGetExecutionDetail -v`
Expected: FAIL — `ImportError: cannot import name '_handle_get_execution_detail'`

- [ ] **Step 3: Implement the handler**

Add to `backend/app/ai/planning_tools.py`, after `_handle_get_case_stats` (around line 185):

```python
def _handle_get_execution_detail(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    from app.services.executions import get_case_execution
    run_id = int(params.get("run_id", 0))
    if not run_id:
        return {"error": "必须提供 run_id 参数"}
    detail = get_case_execution(db_session, run_id)
    if detail is None:
        return {"error": f"执行记录 {run_id} 不存在"}
    steps_summary = []
    if detail.report and detail.report.steps:
        for step in detail.report.steps:
            s: dict[str, Any] = {
                "step_index": step.step_index,
                "action": step.action,
                "status": step.status,
            }
            if step.target is not None:
                s["target"] = step.target
            if step.value is not None:
                s["value"] = step.value
            if step.error_message is not None:
                s["error_message"] = step.error_message
            if step.resolved_by is not None:
                s["resolved_by"] = step.resolved_by
            if step.url is not None:
                s["url"] = step.url
            if step.duration_ms is not None:
                s["duration_ms"] = step.duration_ms
            if step.console_events:
                errors = [e for e in step.console_events if isinstance(e, dict) and e.get("level") == "error"]
                if errors:
                    s["console_errors"] = [e.get("text", "") for e in errors]
            if step.network_events:
                failures = [e for e in step.network_events if isinstance(e, dict) and (e.get("status") or 0) >= 400]
                if failures:
                    s["network_errors"] = [{"url": e.get("url", ""), "status": e.get("status")} for e in failures]
            steps_summary.append(s)
    return {
        "id": detail.id,
        "case_id": detail.case_id,
        "case_name": detail.case_name,
        "status": detail.status,
        "total_steps": detail.total_steps,
        "failed_step_index": detail.failed_step_index,
        "error_message": detail.error_message,
        "duration_ms": detail.duration_ms,
        "steps": steps_summary,
    }
```

- [ ] **Step 4: Register the tool**

Add to `_TOOL_REGISTRY` dict in `planning_tools.py`:

```python
    "get_execution_detail": PlanningTool(
        name="get_execution_detail",
        description="查看指定测试执行的完整详情，包括每一步的状态、错误信息、定位器解析结果、控制台和网络错误。用于分析失败原因。",
        parameters={
            "type": "object",
            "properties": {
                "run_id": {
                    "type": "integer",
                    "description": "要查看的测试执行记录 ID",
                },
            },
            "required": ["run_id"],
        },
    ),
```

Add to `_TOOL_HANDLERS` dict:

```python
    "get_execution_detail": _handle_get_execution_detail,
```

- [ ] **Step 5: Update the tool count in existing test**

In `test_planning_tools.py`, update `TestListAvailableTools.test_returns_all_registered_tools`:
Change `assert len(tools) == 8` to `assert len(tools) == 9` and add `"get_execution_detail"` to the `tool_names` set.

- [ ] **Step 6: Run all planning tool tests**

Run: `cd backend && uv run pytest tests/unit/test_planning_tools.py -v`
Expected: All tests pass, including new `TestGetExecutionDetail` tests.

- [ ] **Step 7: Commit**

```bash
git add backend/app/ai/planning_tools.py backend/tests/unit/test_planning_tools.py
git commit -m "feat: add get_execution_detail tool for AI result analysis"
```

---

### Task 3: Add `get_project_test_status` Tool

**Files:**
- Modify: `backend/app/ai/planning_tools.py`
- Test: `backend/tests/unit/test_planning_tools.py`

- [ ] **Step 1: Write the failing test**

Add to `backend/tests/unit/test_planning_tools.py`:

```python
class TestGetProjectTestStatus:
    """Tests for _handle_get_project_test_status handler."""

    def test_returns_no_runs_when_empty(self, db_session: Session) -> None:
        """Should return no_runs conclusion when no executions exist."""
        from app.ai.planning_tools import _handle_get_project_test_status

        result = _handle_get_project_test_status(
            params={},
            db_session=db_session,
            project_id=1,
        )
        assert result["conclusion"] == "no_runs"
        assert result["cases"] == []

    def test_returns_all_passed(self, db_session: Session) -> None:
        """Should return all_passed when all cases have passing latest runs."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(
                project_id=1,
                name="Passing Case",
                description=None,
                steps=[{"action": "goto", "value": "/test"}],
            ),
            actor_user_id=1,
        )
        db_session.flush()
        execution = TestCaseRun(
            case_id=case.id, project_id=1, triggered_by=1, status="passed",
        )
        db_session.add(execution)
        db_session.commit()

        from app.ai.planning_tools import _handle_get_project_test_status

        result = _handle_get_project_test_status(
            params={}, db_session=db_session, project_id=1,
        )
        assert result["conclusion"] == "all_passed"
        assert len(result["cases"]) == 1
        assert result["cases"][0]["latest_status"] == "passed"

    def test_returns_partial_when_mixed(self, db_session: Session) -> None:
        """Should return partial when some cases pass and some fail."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case_a = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Case A", description=None, steps=[{"action": "goto", "value": "/a"}]),
            actor_user_id=1,
        )
        case_b = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Case B", description=None, steps=[{"action": "goto", "value": "/b"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case_a.id, project_id=1, triggered_by=1, status="passed"))
        db_session.add(TestCaseRun(case_id=case_b.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_project_test_status

        result = _handle_get_project_test_status(
            params={}, db_session=db_session, project_id=1,
        )
        assert result["conclusion"] == "partial"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_planning_tools.py::TestGetProjectTestStatus -v`
Expected: FAIL — `ImportError`

- [ ] **Step 3: Implement the handler**

Add to `backend/app/ai/planning_tools.py`, after `_handle_get_execution_detail`:

```python
def _handle_get_project_test_status(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    from sqlalchemy import select
    from app.models import TestCase, TestCaseRun

    cases = db_session.scalars(
        select(TestCase).where(TestCase.project_id == project_id).order_by(TestCase.id)
    ).all()

    case_statuses: list[dict[str, Any]] = []
    for case in cases:
        latest_run = db_session.scalars(
            select(TestCaseRun)
            .where(TestCaseRun.case_id == case.id)
            .order_by(TestCaseRun.started_at.desc())
            .limit(1)
        ).first()

        if latest_run is None:
            case_statuses.append({
                "case_id": case.id,
                "case_name": case.name,
                "latest_status": "no_runs",
                "latest_run_id": None,
            })
        else:
            report = latest_run.report or {}
            steps = report.get("steps", [])
            passed = sum(1 for s in steps if isinstance(s, dict) and s.get("status") == "passed")
            total = len(steps)
            case_statuses.append({
                "case_id": case.id,
                "case_name": case.name,
                "latest_status": latest_run.status,
                "latest_run_id": latest_run.id,
                "passed_steps": passed,
                "total_steps": total,
                "error_message": latest_run.error_message,
            })

    if not case_statuses:
        conclusion = "no_runs"
    elif all(c["latest_status"] == "passed" for c in case_statuses):
        conclusion = "all_passed"
    elif all(c["latest_status"] in ("failed", "needs_intervention") for c in case_statuses):
        conclusion = "all_failed"
    else:
        conclusion = "partial"

    return {"conclusion": conclusion, "cases": case_statuses, "total_cases": len(case_statuses)}
```

- [ ] **Step 4: Register the tool**

Add to `_TOOL_REGISTRY`:

```python
    "get_project_test_status": PlanningTool(
        name="get_project_test_status",
        description="查看项目下所有测试用例的最新执行状态，包括通过率、失败摘要和整体结论（全部通过/部分通过/全部失败/无执行记录）。",
        parameters={
            "type": "object",
            "properties": {},
            "required": [],
        },
    ),
```

Add to `_TOOL_HANDLERS`:

```python
    "get_project_test_status": _handle_get_project_test_status,
```

- [ ] **Step 5: Update tool count and names in existing test**

Change tool count to `10` and add `"get_project_test_status"` to the set.

- [ ] **Step 6: Run all planning tool tests**

Run: `cd backend && uv run pytest tests/unit/test_planning_tools.py -v`
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add backend/app/ai/planning_tools.py backend/tests/unit/test_planning_tools.py
git commit -m "feat: add get_project_test_status tool for project-level test status"
```

---

### Task 4: Add `get_failure_analysis` Tool

**Files:**
- Modify: `backend/app/ai/planning_tools.py`
- Test: `backend/tests/unit/test_planning_tools.py`

- [ ] **Step 1: Write the failing test**

Add to `backend/tests/unit/test_planning_tools.py`:

```python
class TestGetFailureAnalysis:
    """Tests for _handle_get_failure_analysis handler."""

    def test_returns_empty_when_no_failures(self, db_session: Session) -> None:
        """Should return empty when all runs passed."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="OK Case", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_failure_analysis

        result = _handle_get_failure_analysis(
            params={}, db_session=db_session, project_id=1,
        )
        assert result["failure_patterns"] == []

    def test_detects_consecutive_failures(self, db_session: Session) -> None:
        """Should detect consecutive failure count for a case."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Flaky Case", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_failure_analysis

        result = _handle_get_failure_analysis(
            params={}, db_session=db_session, project_id=1,
        )
        assert len(result["failure_patterns"]) == 1
        assert result["failure_patterns"][0]["case_name"] == "Flaky Case"
        assert result["failure_patterns"][0]["consecutive_failures"] == 2

    def test_detects_flaky_pattern(self, db_session: Session) -> None:
        """Should flag alternating pass/fail as suspected flaky."""
        from app.models import TestCaseRun
        from app.services import cases as case_service

        case = case_service.create_case(
            db_session,
            CaseCreateRequest(project_id=1, name="Unstable", description=None, steps=[{"action": "goto", "value": "/"}]),
            actor_user_id=1,
        )
        db_session.flush()
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="passed"))
        db_session.add(TestCaseRun(case_id=case.id, project_id=1, triggered_by=1, status="failed"))
        db_session.commit()

        from app.ai.planning_tools import _handle_get_failure_analysis

        result = _handle_get_failure_analysis(
            params={}, db_session=db_session, project_id=1,
        )
        assert len(result["failure_patterns"]) == 1
        assert result["failure_patterns"][0]["suspected_flaky"] is True

    def test_filters_by_case_id(self, db_session: Session) -> None:
        """Should only analyze specific case when case_id is provided."""
        from app.ai.planning_tools import _handle_get_failure_analysis

        result = _handle_get_failure_analysis(
            params={"case_id": "99999"},
            db_session=db_session,
            project_id=1,
        )
        assert result["failure_patterns"] == []
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_planning_tools.py::TestGetFailureAnalysis -v`
Expected: FAIL — `ImportError`

- [ ] **Step 3: Implement the handler**

Add to `backend/app/ai/planning_tools.py`:

```python
def _handle_get_failure_analysis(
    *,
    params: dict[str, Any],
    db_session: Session,
    project_id: int,
) -> dict[str, Any]:
    from sqlalchemy import select
    from app.models import TestCase, TestCaseRun

    limit = min(int(params.get("limit", 5)), 10)
    case_id_filter = params.get("case_id")
    if case_id_filter:
        case_id_filter = int(case_id_filter)

    query = select(TestCaseRun).where(TestCaseRun.project_id == project_id)
    if case_id_filter:
        query = query.where(TestCaseRun.case_id == case_id_filter)
    query = query.order_by(TestCaseRun.started_at.desc()).limit(limit * 3)

    runs = db_session.scalars(query).all()

    case_map: dict[int, list[TestCaseRun]] = {}
    for run in runs:
        case_map.setdefault(run.case_id, []).append(run)

    patterns: list[dict[str, Any]] = []
    for cid, case_runs in case_map.items():
        case_record = db_session.get(TestCase, cid)
        case_name = case_record.name if case_record else f"Case {cid}"

        statuses = [r.status for r in case_runs]
        failed_runs = [r for r in case_runs if r.status in ("failed", "needs_intervention")]
        passed_runs = [r for r in case_runs if r.status == "passed"]

        consecutive_failures = 0
        for s in statuses:
            if s in ("failed", "needs_intervention"):
                consecutive_failures += 1
            else:
                break

        alternating = False
        if len(statuses) >= 3:
            switches = sum(1 for i in range(1, len(statuses)) if statuses[i] != statuses[i - 1])
            alternating = switches >= len(statuses) * 0.6

        last_error = None
        if failed_runs:
            last_error = failed_runs[0].error_message

        patterns.append({
            "case_id": cid,
            "case_name": case_name,
            "total_runs": len(case_runs),
            "passed_count": len(passed_runs),
            "failed_count": len(failed_runs),
            "consecutive_failures": consecutive_failures,
            "last_error_message": last_error,
            "suspected_flaky": alternating,
        })

    return {"failure_patterns": patterns}
```

- [ ] **Step 4: Register the tool**

Add to `_TOOL_REGISTRY`:

```python
    "get_failure_analysis": PlanningTool(
        name="get_failure_analysis",
        description="分析项目或指定用例的失败模式，包括连续失败次数、疑似 flaky 标记、最近错误信息。用于根因分析和回归策略决策。",
        parameters={
            "type": "object",
            "properties": {
                "case_id": {
                    "type": "integer",
                    "description": "指定分析某个用例（可选，不填则分析项目全部用例）",
                },
                "limit": {
                    "type": "integer",
                    "description": "分析的最近执行记录数量，默认5，最大10",
                },
            },
            "required": [],
        },
    ),
```

Add to `_TOOL_HANDLERS`:

```python
    "get_failure_analysis": _handle_get_failure_analysis,
```

- [ ] **Step 5: Update tool count and names in existing test**

Change tool count to `11` and add `"get_failure_analysis"` to the set.

- [ ] **Step 6: Run all planning tool tests**

Run: `cd backend && uv run pytest tests/unit/test_planning_tools.py -v`
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add backend/app/ai/planning_tools.py backend/tests/unit/test_planning_tools.py
git commit -m "feat: add get_failure_analysis tool for failure pattern detection"
```

---

### Task 5: Upgrade System Prompt

**Files:**
- Modify: `backend/app/ai/test_planning_prompts.py`

- [ ] **Step 1: Replace the full system prompt**

Replace the `SYSTEM_PROMPT_TEMPLATE` constant entirely with:

```python
SYSTEM_PROMPT_TEMPLATE = """\
你是一个面向 Web 自动化测试的智能 QA Agent，不是单纯的测试方案生成器。

你的目标是：
1. 理解用户当前的测试目标和业务上下文
2. 结合历史测试结果、失败信息和已有测试点，自动管理上下文
3. 判断当前应执行：首次测试设计、针对性复测、局部回归测试、全量回归测试，还是结果总结
4. 输出清晰、可执行、可追踪的测试方案或测试结论
5. 当拿到执行结果后，自动总结失败点、疑似根因、影响范围，并向用户反馈下一步建议

你可以做五类动作：
1. `ask_user`：当关键信息不足时，向用户继续追问。
2. `call_tool`：当项目上下文不清晰时，调用工具补充信息。
3. `generate_plan`：当信息足够，输出测试方案。
4. `analyze_results`：当收到执行结果时，输出结构化分析报告。
5. `plan_regression`：当分析发现失败需要复测时，输出针对性复测方案。

可用工具如下：
{tool_descriptions}

【任务分类】
收到输入后，先判断任务属于以下哪一类：
- 新测试点设计
- 已知失败后的针对性测试
- 修复后的回归测试
- 测试结果总结
- 缺陷归纳与用户反馈
如果用户没有明确说明，根据上下文自动判断。

【测试点成功定义】
一个项目下每个测试用例都是独立验证单元。一个测试点（项目）内所有测试用例的最新执行结果全部为 passed，才算该测试点成功。

你每次都必须返回一个合法 JSON 对象，不要输出 Markdown 代码块，也不要输出 JSON 之外的解释。格式固定为：
{{
  "thought": "你对当前信息缺口和下一步动作的判断",
  "action": "ask_user | call_tool | generate_plan | analyze_results | plan_regression",
  "action_input": {{
    "message": "当 action=ask_user 时必填",
    "tool": "当 action=call_tool 时必填",
    "params": {{}},
    "summary": "当 action=generate_plan 时建议填写",
    "assumptions": [],
    "risks": [],
    "scenarios": [],
    "analysis": {{
      "conclusion": "all_passed | partial | all_failed",
      "case_results": [],
      "failure_details": [],
      "suspected_root_cause": null,
      "impact_scope": null,
      "recommended_action": "targeted_retest | regression | manual | done",
      "recommended_scope": "current | adjacent | module | core"
    }}
  }},
  "collected_info": {{
    "app_under_test": null,
    "business_goal": null,
    "entry_url_or_page": null,
    "core_user_flow": null,
    "main_assertions": [],
    "test_data_or_account": null,
    "scope_limits": null
  }},
  "test_context": {{
    "project_id": null,
    "test_point_status": null,
    "last_run_failures": [],
    "suspected_root_cause": null,
    "regression_scope": null,
    "next_action": null
  }},
  "todo_list": [
    {{"item": "任务描述", "status": "done|in_progress|pending"}}
  ]
}}

规则：
- `collected_info` 只填写本轮明确获得的信息；未知字段保持 null 或空数组。
- 每次最多追问 1-2 个关键问题，问题尽量自然。
- 当已收集到 4 项及以上信息，或者用户连续两次未补充新信息时，通过 `ask_user` 主动询问用户："信息是否已经足够？是否需要我直接生成测试方案？"
- 如果用户回复确认（"是"/"够了"/"生成吧"/"可以"等），再使用 `generate_plan` 生成方案。
- 如果用户已经说"直接生成""够了""先给方案"，优先生成方案。
- `generate_plan` 时请输出完整方案字段：`summary`、`assumptions`、`risks`、`scenarios`。
- `scenarios` 中每个场景必须包含：`scenario_key`、`title`、`goal`、`preconditions`、`priority`、`test_data_requirements`、`assertions`、`draft_prompt`。
- 可以先调用工具了解项目已有用例、执行记录，再决定追问或生成。
- 不要向用户暴露工具报错细节；如果工具失败，可基于已有上下文继续判断。
- 在生成测试方案前，如果需求中包含入口 URL，系统会自动采集入口页面的可交互元素。你不需要手动调用 explore_page 采集入口页面。
- 对于涉及多个页面的测试流程，优先使用 `explore_flow` 工具一次性采集所有页面的元素和布局信息。
- 当已采集到页面元素时，`draft_prompt` 中的 target 必须严格使用元素清单中的实际可见文本、label、placeholder 或 id。
- `draft_prompt` 中涉及测试数据的 step value，必须使用 ${{context_key}} 格式引用 input_contract 变量。
- 当已收集到 3 项及以上信息时，你必须在 `todo_list` 中列出当前规划进度清单。
- 每轮回复都必须更新 `todo_list` 的状态。
- `todo_list` 仅用于向用户展示进度，不影响你的 action 决策逻辑。

【错误分析要求（action=analyze_results 时必须遵守）】
当 action 为 analyze_results 时，`action_input.analysis` 必须填写完整的分析结果：
- `conclusion`: 本轮总体结论（all_passed / partial / all_failed）
- `case_results`: 每个用例的执行结果（case_id, case_name, status, passed_steps, total_steps, failure_summary）
- `failure_details`: 每个失败点的详细分析（case_name, step_index, action, target, error_message, suspected_cause, cause_probability）
- `suspected_root_cause`: 最可能的根因
- `impact_scope`: 影响范围评估
- `recommended_action`: 建议的下一步动作
- `recommended_scope`: 如果建议回归测试，回归范围

错误原因优先级（按概率排序）：
1. 元素定位失效（页面结构变更）
2. 断言不匹配（产品逻辑变更）
3. 等待条件不足
4. 测试数据问题
5. 环境问题
6. 权限/登录态问题
7. 网络/接口异常
8. 疑似偶发 flaky

【回归测试策略（action=plan_regression 时）】
根据失败点和影响范围决定回归级别：
- 仅当前用例回归：失败点局限在单一功能
- 相邻流程回归：失败点可能影响上下游流程
- 模块级回归：涉及公共模块变更
- 核心链路回归：涉及登录、导航、核心业务链路
必须说明选择理由。

【默认输出语言】
默认使用中文输出，表述专业、清晰、简洁。
"""
```

- [ ] **Step 2: Run existing agent tests to verify prompt still works**

Run: `cd backend && uv run pytest tests/unit/test_planning_agent.py -v`
Expected: All pass. The prompt change is backward-compatible — existing actions still valid, new actions are opt-in.

- [ ] **Step 3: Commit**

```bash
git add backend/app/ai/test_planning_prompts.py
git commit -m "feat: upgrade system prompt to QA Agent with analysis and regression actions"
```

---

### Task 6: Handle New Actions in Agent Loop

**Files:**
- Modify: `backend/app/ai/test_planning_agent.py`

- [ ] **Step 1: Handle `analyze_results` action**

In `stream_planning_turn()`, after the `generate_plan` action block (around line 253), add before the `ask_user` fallback:

```python
        if action == "analyze_results":
            analysis_payload = action_input.get("analysis") if isinstance(action_input, dict) else None
            if not isinstance(analysis_payload, dict):
                analysis_payload = {}
            try:
                from app.schemas.ai_planning import ExecutionAnalysis
                analysis = ExecutionAnalysis.model_validate(analysis_payload)
            except Exception:
                analysis = ExecutionAnalysis(conclusion="partial")
            analysis_message = action_input.get("summary") or _build_analysis_message(analysis)
            response = AIPlanningTurnResponse(
                assistant_message=analysis_message,
                session_status="completed",
                requirements=requirements,
                missing_slots=[],
                suggested_questions=[],
                plan=None,
                drafts=[],
                next_action="ask_followup",
                tool_calls=tool_calls,
                todo_list=todo_items,
                execution_analysis=analysis,
            )
            yield _turn_complete_payload(response)
            return response

        if action == "plan_regression":
            regression_summary = str(action_input.get("summary") or "").strip() if isinstance(action_input, dict) else ""
            if not regression_summary:
                regression_summary = "根据失败分析，建议进行回归测试。"
            response = _plan_response(
                requirements=requirements,
                plan_payload=action_input,
                assistant_message=regression_summary,
                tool_calls=tool_calls,
                todo_list=todo_items,
            )
            yield _turn_complete_payload(response)
            return response
```

- [ ] **Step 2: Add the `_build_analysis_message` helper**

Add after `_default_followup_question`:

```python
def _build_analysis_message(analysis: Any) -> str:
    lines = ["执行结果分析：\n"]
    conclusion_labels = {
        "all_passed": "全部通过",
        "partial": "部分通过",
        "all_failed": "全部失败",
    }
    lines.append(f"本轮结论：{conclusion_labels.get(getattr(analysis, 'conclusion', ''), '未知')}")
    for cr in getattr(analysis, "case_results", []):
        status_icon = "✅" if cr.status == "passed" else "❌"
        lines.append(f"  {status_icon} {cr.case_name} — {cr.status} ({cr.passed_steps}/{cr.total_steps}步)")
    for fd in getattr(analysis, "failure_details", []):
        lines.append(f"  ⚠ 失败点：{fd.case_name} 步骤{fd.step_index}({fd.action}) — {fd.suspected_cause}")
    if getattr(analysis, "suspected_root_cause", None):
        lines.append(f"疑似根因：{analysis.suspected_root_cause}")
    if getattr(analysis, "recommended_action", None):
        action_labels = {
            "targeted_retest": "针对性复测",
            "regression": "回归测试",
            "manual": "人工介入",
            "done": "测试完成",
        }
        lines.append(f"建议下一步：{action_labels.get(analysis.recommended_action, analysis.recommended_action)}")
        if getattr(analysis, "recommended_scope", None):
            scope_labels = {"current": "仅当前用例", "adjacent": "相邻流程", "module": "模块级", "core": "核心链路"}
            lines.append(f"回归范围：{scope_labels.get(analysis.recommended_scope, analysis.recommended_scope)}")
    return "\n".join(lines)
```

- [ ] **Step 3: Run agent tests**

Run: `cd backend && uv run pytest tests/unit/test_planning_agent.py -v`
Expected: All pass.

- [ ] **Step 4: Commit**

```bash
git add backend/app/ai/test_planning_agent.py
git commit -m "feat: handle analyze_results and plan_regression actions in agent loop"
```

---

### Task 7: Add Post-Execution Auto-Analysis (Sync Path)

**Files:**
- Modify: `backend/app/services/ai_planning.py:506-558`

- [ ] **Step 1: Write the test**

Create `backend/tests/unit/test_execution_analysis.py`:

```python
"""Unit tests for post-execution auto-analysis flow."""
from __future__ import annotations

from unittest.mock import patch, MagicMock

import pytest
from sqlalchemy.orm import Session

from app.models import AIPlanningMessage, AIPlanningSession, TestCaseRun
from app.schemas.ai_planning import (
    ExecutionAnalysis,
    ExecutionSummaryResult,
    SavedCaseResult,
)
from app.services import cases as case_service
from app.schemas.cases import CaseCreateRequest


class TestBuildAnalysisContext:
    """Tests for _build_analysis_context helper."""

    def test_builds_context_from_execution_summaries(self, db_session: Session) -> None:
        """Should produce a context message containing all case statuses."""
        from app.services.ai_planning import _build_analysis_context

        summaries = [
            ExecutionSummaryResult(
                execution_id=1, case_id=1, case_name="Login Test",
                status="failed", total_steps=5, passed_steps=3, failed_steps=2,
                duration_ms=1000, screenshot_url=None, report_url="/run/1",
            ),
            ExecutionSummaryResult(
                execution_id=2, case_id=2, case_name="Search Test",
                status="passed", total_steps=3, passed_steps=3, failed_steps=0,
                duration_ms=500, screenshot_url=None, report_url="/run/2",
            ),
        ]
        context = _build_analysis_context(summaries, db_session)
        assert "Login Test" in context
        assert "failed" in context
        assert "Search Test" in context
        assert "passed" in context


class TestAutoAnalysisOnFailure:
    """Tests that auto-analysis triggers when execution has failures."""

    def test_no_analysis_when_all_passed(self, db_session: Session) -> None:
        """Should not trigger AI analysis when all cases pass."""
        from app.services.ai_planning import _should_run_analysis

        summaries = [
            ExecutionSummaryResult(
                execution_id=1, case_id=1, case_name="A",
                status="passed", total_steps=3, passed_steps=3, failed_steps=0,
                duration_ms=100, screenshot_url=None, report_url="/run/1",
            ),
        ]
        assert _should_run_analysis(summaries) is False

    def test_analysis_triggered_on_failure(self, db_session: Session) -> None:
        """Should trigger AI analysis when any case fails."""
        from app.services.ai_planning import _should_run_analysis

        summaries = [
            ExecutionSummaryResult(
                execution_id=1, case_id=1, case_name="A",
                status="passed", total_steps=3, passed_steps=3, failed_steps=0,
                duration_ms=100, screenshot_url=None, report_url="/run/1",
            ),
            ExecutionSummaryResult(
                execution_id=2, case_id=2, case_name="B",
                status="failed", total_steps=5, passed_steps=3, failed_steps=2,
                duration_ms=500, screenshot_url=None, report_url="/run/2",
            ),
        ]
        assert _should_run_analysis(summaries) is True
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_execution_analysis.py -v`
Expected: FAIL — `ImportError: cannot import name '_build_analysis_context'`

- [ ] **Step 3: Implement `_build_analysis_context`, `_should_run_analysis`, and `_run_analysis_turn`**

Add to `backend/app/services/ai_planning.py` (before `save_and_execute_selected_drafts`, around line 440):

```python
def _should_run_analysis(
    execution_summaries: list[ExecutionSummaryResult],
) -> bool:
    """Return True if any execution result is not passed."""
    return any(s.status != "passed" for s in execution_summaries)


def _build_analysis_context(
    execution_summaries: list[ExecutionSummaryResult],
    db_session: Session,
) -> str:
    """Build a context message for the analysis turn from execution summaries."""
    lines = ["本轮执行已完成，请分析以下结果：\n"]
    for ex in execution_summaries:
        icon = "✅" if ex.status == "passed" else "❌"
        failure_info = ""
        if ex.status != "passed":
            failure_info = f" [失败步骤: {ex.failed_steps}步]"
        lines.append(
            f"{icon} {ex.case_name} — {ex.status} "
            f"({ex.passed_steps}/{ex.total_steps}步){failure_info}"
        )
    lines.append("\n请使用 analyze_results 模式输出分析报告。")
    return "\n".join(lines)


def _run_analysis_turn(
    *,
    execution_summaries: list[ExecutionSummaryResult],
    db_session: Session,
    project_id: int,
) -> AIPlanningTurnResponse | None:
    """Run an analysis turn using the AI agent with execution results as context.

    Returns None if AI is not configured or analysis fails.
    """
    try:
        context_message = _build_analysis_context(execution_summaries, db_session)
        transcript = [{"role": "user", "content": context_message}]
        return run_planning_turn(
            transcript=transcript,
            existing_requirements=None,
            db_session=db_session,
            project_id=project_id,
        )
    except Exception:
        logger.warning("Auto-analysis turn failed", exc_info=True)
        return None
```

- [ ] **Step 4: Integrate into `save_and_execute_selected_drafts` sync path**

In `save_and_execute_selected_drafts()`, after building `assistant_message` and creating the summary `AIPlanningMessage` (after line 544 `session.commit()`), replace the return block:

Find this code (lines 548-558):
```python
    return AIPlanningTurnResponse(
        assistant_message=assistant_message,
        session_status="completed",
        requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        missing_slots=[],
        plan=None,
        drafts=[],
        next_action="ask_followup",
        saved_cases=saved_cases,
        execution_summaries=execution_summaries,
    )
```

Replace with:
```python
    execution_analysis = None
    if _should_run_analysis(execution_summaries):
        analysis_response = _run_analysis_turn(
            execution_summaries=execution_summaries,
            db_session=db_session,
            project_id=planning_session.project_id or 1,
        )
        if analysis_response and analysis_response.execution_analysis:
            execution_analysis = analysis_response.execution_analysis
            analysis_msg = analysis_response.assistant_message
            session.add(
                AIPlanningMessage(
                    session_id=planning_session.id,
                    role="assistant",
                    turn_type="followup",
                    content=analysis_msg,
                    structured_payload_json={
                        "type": "execution_analysis",
                        "analysis": execution_analysis.model_dump(mode="json"),
                    },
                )
            )
            session.commit()
            assistant_message = f"{assistant_message}\n\n---\n\n{analysis_msg}"

    return AIPlanningTurnResponse(
        assistant_message=assistant_message,
        session_status="completed",
        requirements=AIPlanningRequirements.model_validate(planning_session.requirements_json or {}),
        missing_slots=[],
        suggested_questions=[],
        plan=None,
        drafts=[],
        next_action="ask_followup",
        saved_cases=saved_cases,
        execution_summaries=execution_summaries,
        execution_analysis=execution_analysis,
    )
```

- [ ] **Step 5: Run the tests**

Run: `cd backend && uv run pytest tests/unit/test_execution_analysis.py -v`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add backend/app/services/ai_planning.py backend/tests/unit/test_execution_analysis.py
git commit -m "feat: add post-execution auto-analysis in sync execution path"
```

---

### Task 8: Add Post-Execution Auto-Analysis (Streaming Path)

**Files:**
- Modify: `backend/app/services/ai_planning.py:677-700`

- [ ] **Step 1: Integrate auto-analysis into streaming path**

In `save_and_execute_selected_drafts_streaming()`, find the block after "Persist execution summary message" (lines 677-700):

```python
    # Persist execution summary message
    lines = ["测试执行完成：\n"]
    ...
    yield {"type": "done"}
```

Replace the `yield {"type": "done"}` at line 700 with:

```python
    if _should_run_analysis(execution_summaries):
        yield {"type": "status", "phase": "analyzing", "message": "正在分析执行结果..."}
        analysis_response = _run_analysis_turn(
            execution_summaries=execution_summaries,
            db_session=db_session,
            project_id=planning_session.project_id or 1,
        )
        if analysis_response and analysis_response.execution_analysis:
            analysis_msg = analysis_response.assistant_message
            session.add(
                AIPlanningMessage(
                    session_id=planning_session.id,
                    role="assistant",
                    turn_type="followup",
                    content=analysis_msg,
                    structured_payload_json={
                        "type": "execution_analysis",
                        "analysis": analysis_response.execution_analysis.model_dump(mode="json"),
                    },
                )
            )
            session.commit()
            yield {
                "type": "analysis_complete",
                "analysis": analysis_response.execution_analysis.model_dump(mode="json"),
                "message": analysis_msg,
            }

    yield {"type": "done"}
```

- [ ] **Step 2: Run all tests**

Run: `cd backend && uv run pytest tests/unit/ -v`
Expected: All pass.

- [ ] **Step 3: Commit**

```bash
git add backend/app/services/ai_planning.py
git commit -m "feat: add post-execution auto-analysis in streaming execution path"
```

---

### Task 9: Final Verification

- [ ] **Step 1: Run full unit test suite**

Run: `cd backend && uv run pytest tests/unit/ -v`
Expected: All tests pass.

- [ ] **Step 2: Verify backend starts**

Run: `cd backend && uv run python -c "from app.ai.planning_tools import list_available_tools; tools = list_available_tools(); print(f'{len(tools)} tools registered'); print([t.name for t in tools])"`
Expected: `11 tools registered`, includes `get_execution_detail`, `get_project_test_status`, `get_failure_analysis`.

- [ ] **Step 3: Verify schema changes are valid**

Run: `cd backend && uv run python -c "from app.schemas.ai_planning import ExecutionAnalysis, CaseAnalysisResult, FailureDetail, AIPlanningTurnResponse; print('Schemas OK')"`
Expected: `Schemas OK`

- [ ] **Step 4: Update tool count assertion**

Verify `test_planning_tools.py::TestListAvailableTools::test_returns_all_registered_tools` asserts `len(tools) == 11`.

- [ ] **Step 5: Final commit if any fixes needed**

```bash
git add -A
git commit -m "test: update tool count and fix any test drift"
```
