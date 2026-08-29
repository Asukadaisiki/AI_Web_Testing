# Chat History + Full Test Flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add session history persistence and extend AI planning flow to support save+execute test cases end-to-end.

**Architecture:** Backend adds a session list API and extends the agent state machine with reviewing/saving/executing/completed phases. Frontend adds a session switcher and new message card types for draft review, save confirmation, and execution results.

**Tech Stack:** FastAPI + SQLAlchemy (backend), React + TypeScript + Ant Design (frontend)

---

## File Structure

### Backend — Modify

| File | Responsibility |
|------|---------------|
| `backend/app/api/routes/ai_planning.py` | Add `GET /sessions` list endpoint, add `POST /sessions/{id}/drafts/{id}/save-and-execute` endpoint |
| `backend/app/schemas/ai_planning.py:13` | Extend `AIPlanningSessionStatus` with new statuses |
| `backend/app/schemas/ai_planning.py:130-140` | Extend `AIPlanningTurnResponse` with save/execute result fields |
| `backend/app/ai/test_planning_agent.py` | Add handling for reviewing/saving/executing/completed phases |
| `backend/app/services/ai_planning.py` | Add `list_planning_sessions()`, `save_and_execute_drafts()` functions |

### Backend — Create

| File | Responsibility |
|------|---------------|
| (none) | |

### Frontend — Modify

| File | Responsibility |
|------|---------------|
| `frontend/src/components/AITestPlanningPanel.tsx` | Add session switcher, history restore, new message cards |
| `frontend/src/services/api.ts:232-262` | Add `listPlanningSessions()` API client function |

### Frontend — Create

| File | Responsibility |
|------|---------------|
| (none) | |

---

## Task 1: Backend — Add Session List API

**Files:**
- Modify: `backend/app/services/ai_planning.py`
- Modify: `backend/app/api/routes/ai_planning.py`
- Modify: `backend/app/schemas/ai_planning.py`

- [ ] **Step 1: Add session list schema**

In `backend/app/schemas/ai_planning.py`, add after the `AIPlanningSession` class (around line 75):

```python
class AIPlanningSessionSummary(DSLModel):
    id: int = Field(ge=1)
    title: str | None = Field(default=None, max_length=200)
    status: AIPlanningSessionStatus
    created_at: datetime
    updated_at: datetime
```

- [ ] **Step 2: Add list service function**

In `backend/app/services/ai_planning.py`, add a `list_planning_sessions` function:

```python
def list_planning_sessions(session: Session, actor_user_id: int, project_id: int | None = None) -> list[AIPlanningSessionSummary]:
    q = session.query(AIPlanningSessionModel).filter(AIPlanningSessionModel.actor_user_id == actor_user_id)
    if project_id is not None:
        q = q.filter(AIPlanningSessionModel.project_id == project_id)
    q = q.order_by(AIPlanningSessionModel.updated_at.desc())
    rows = q.all()
    return [
        AIPlanningSessionSummary(
            id=r.id,
            title=r.title or r.requirements_json.get("app_under_test"),
            status=r.status,
            created_at=r.created_at,
            updated_at=r.updated_at,
        )
        for r in rows
    ]
```

- [ ] **Step 3: Add list route**

In `backend/app/api/routes/ai_planning.py`, add before the `/{session_id}` GET route (around line 49):

```python
@router.get("/sessions", response_model=list[AIPlanningSessionSummary])
def list_planning_sessions_route(
    project_id: int | None = None,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
) -> list[AIPlanningSessionSummary]:
    return list_planning_sessions(session, actor_user_id=current_user.id, project_id=project_id)
```

- [ ] **Step 4: Verify backend starts**

Run: `cd backend && python -c "from app.main import app; print('OK')"`
Expected: `OK`

- [ ] **Step 5: Commit**

```bash
git add backend/app/schemas/ai_planning.py backend/app/services/ai_planning.py backend/app/api/routes/ai_planning.py
git commit -m "feat: add session list API for AI planning history"
```

---

## Task 2: Frontend — Session Switcher + History Restore

**Files:**
- Modify: `frontend/src/services/api.ts`
- Modify: `frontend/src/components/AITestPlanningPanel.tsx`

- [ ] **Step 1: Add API client function**

In `frontend/src/services/api.ts`, add after `getAISettings` (around line 266):

```typescript
export function listPlanningSessions(projectId?: number) {
  const params = projectId ? `?project_id=${projectId}` : "";
  return request<AIPlanningSessionSummary[]>(`/api/v1/ai-planning/sessions${params}`);
}
```

Also add the `AIPlanningSessionSummary` type to the types file or inline:

```typescript
export interface AIPlanningSessionSummary {
  id: number;
  title: string | null;
  status: string;
  created_at: string;
  updated_at: string;
}
```

- [ ] **Step 2: Add session list state and fetch logic to AITestPlanningPanel**

In `AITestPlanningPanel.tsx`, add new state after line 122:

```typescript
const [sessionList, setSessionList] = useState<AIPlanningSessionSummary[]>([]);
const [isLoadingHistory, setIsLoadingHistory] = useState(false);
```

Add a function to load session list:

```typescript
async function loadSessionList() {
  if (!projectId) return;
  setIsLoadingHistory(true);
  try {
    const list = await listPlanningSessions(projectId);
    setSessionList(list);
  } catch {
    // silently fail — session list is non-critical
  } finally {
    setIsLoadingHistory(false);
  }
}
```

- [ ] **Step 3: Change mount behavior — restore instead of always creating new**

Replace the existing `useEffect` on mount (lines 127-161) with logic that:
1. Reads last sessionId from `localStorage` key `ai_planning_last_session`
2. If found, calls `GET /sessions/{id}` to restore that session
3. If not found or restore fails, create a new session as before
4. Always calls `loadSessionList()` to populate the switcher

```typescript
useEffect(() => {
  if (!projectId) return;
  let cancelled = false;

  async function init() {
    setIsBootstrapping(true);
    try {
      // Try restore last session
      const lastId = localStorage.getItem("ai_planning_last_session");
      if (lastId) {
        try {
          const detail = await getPlanningSessionDetail(Number(lastId));
          if (!cancelled) {
            setSessionId(detail.session.id);
            setRequirements(detail.session.requirements);
            setMissingSlots(detail.session.missing_slots);
            setPlan(detail.session.plan);
            setTranscript(detail.messages);
            setDrafts(detail.drafts);
          }
        } catch {
          // Session not found or expired — fall through to create new
        }
      }

      // If no session restored, create new
      if (!cancelled && sessionId === null) {
        const resp = await createPlanningSession({
          project_id: projectId,
          case_id: caseId ?? null,
        });
        if (!cancelled) {
          setSessionId(resp.session.id);
          setRequirements(resp.session.requirements);
          setMissingSlots(resp.session.missing_slots);
          setPlan(resp.session.plan);
          setTranscript(resp.messages);
          setDrafts(resp.drafts);
          localStorage.setItem("ai_planning_last_session", String(resp.session.id));
        }
      }

      // Always load session list
      if (!cancelled) await loadSessionList();
    } catch (err: any) {
      if (!cancelled) messageApi.error("初始化失败: " + (err.message ?? String(err)));
    } finally {
      if (!cancelled) setIsBootstrapping(false);
    }
  }

  init();
  return () => { cancelled = true; };
}, [projectId, caseId]);
```

Note: Also need to add `getPlanningSessionDetail` API function:

```typescript
export function getPlanningSessionDetail(sessionId: number) {
  return request<AIPlanningSessionDetail>(`/api/v1/ai-planning/sessions/${sessionId}`);
}
```

- [ ] **Step 4: Add session switcher UI to the component header**

Add a Select + Button row at the top of the center panel (the chat area), before the message list:

```tsx
<div style={{ display: "flex", gap: 8, marginBottom: 12, alignItems: "center" }}>
  <Select
    style={{ flex: 1 }}
    placeholder="选择会话"
    value={sessionId}
    loading={isLoadingHistory}
    onChange={async (id: number) => {
      setIsBootstrapping(true);
      try {
        const detail = await getPlanningSessionDetail(id);
        setSessionId(detail.session.id);
        setRequirements(detail.session.requirements);
        setMissingSlots(detail.session.missing_slots);
        setPlan(detail.session.plan);
        setTranscript(detail.messages);
        setDrafts(detail.drafts);
        localStorage.setItem("ai_planning_last_session", String(id));
      } catch (err: any) {
        messageApi.error("加载会话失败: " + (err.message ?? String(err)));
      } finally {
        setIsBootstrapping(false);
      }
    }}
    options={sessionList.map((s) => ({
      value: s.id,
      label: s.title || `会话 #${s.id} (${new Date(s.created_at).toLocaleString()})`,
    }))}
  />
  <Button
    type="primary"
    size="small"
    onClick={async () => {
      if (!projectId) return;
      setIsBootstrapping(true);
      try {
        const resp = await createPlanningSession({ project_id: projectId });
        setSessionId(resp.session.id);
        setRequirements(resp.session.requirements);
        setMissingSlots(resp.session.missing_slots);
        setPlan(resp.session.plan);
        setTranscript(resp.messages);
        setDrafts(resp.drafts);
        localStorage.setItem("ai_planning_last_session", String(resp.session.id));
        await loadSessionList();
      } catch (err: any) {
        messageApi.error("创建会话失败: " + (err.message ?? String(err)));
      } finally {
        setIsBootstrapping(false);
      }
    }}
  >
    新建会话
  </Button>
</div>
```

- [ ] **Step 5: Update sessionId in localStorage after message send**

In `handleSendMessage`, after getting response, update localStorage:

```typescript
if (response.session_status) {
  localStorage.setItem("ai_planning_last_session", String(sessionId));
}
```

- [ ] **Step 6: Verify frontend compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 7: Commit**

```bash
git add frontend/src/services/api.ts frontend/src/components/AITestPlanningPanel.tsx
git commit -m "feat: add session switcher with history restore"
```

---

## Task 3: Backend — Extend Agent State Machine for Save + Execute

**Files:**
- Modify: `backend/app/schemas/ai_planning.py:13` — extend status literal
- Modify: `backend/app/schemas/ai_planning.py:130-140` — add result fields
- Modify: `backend/app/api/routes/ai_planning.py` — add save+execute endpoint
- Modify: `backend/app/services/ai_planning.py` — add save+execute logic

- [ ] **Step 1: Extend session status literal**

In `backend/app/schemas/ai_planning.py`, change line 13:

```python
AIPlanningSessionStatus = Literal[
    "collecting", "plan_ready", "drafts_ready",
    "reviewing", "saving", "executing", "completed",
    "closed", "error",
]
```

- [ ] **Step 2: Add save+execute result schema**

In `backend/app/schemas/ai_planning.py`, add after `AIPlanningDraft` class:

```python
class SavedCaseResult(DSLModel):
    case_id: int = Field(ge=1)
    case_name: str
    status: Literal["saved"] = "saved"

class ExecutionSummaryResult(DSLModel):
    execution_id: int = Field(ge=1)
    case_id: int = Field(ge=1)
    case_name: str
    status: Literal["passed", "failed", "needs_intervention", "error"]
    total_steps: int
    passed_steps: int
    failed_steps: int
    duration_ms: int | None = None
    screenshot_url: str | None = None
    report_url: str
```

- [ ] **Step 3: Extend AIPlanningTurnResponse**

Add optional fields to `AIPlanningTurnResponse`:

```python
class AIPlanningTurnResponse(DSLModel):
    # ... existing fields ...
    saved_cases: list[SavedCaseResult] = Field(default_factory=list)
    execution_summaries: list[ExecutionSummaryResult] = Field(default_factory=list)
```

- [ ] **Step 4: Add save+execute service function**

In `backend/app/services/ai_planning.py`, add:

```python
from app.services.cases import create_case
from app.services.executions import execute_case
from app.schemas.cases import CaseCreateRequest
from app.schemas.executions import CaseExecutionRequest

async def save_and_execute_selected_drafts(
    db: Session,
    session_id: int,
    draft_ids: list[int],
    actor_user_id: int,
    execute: bool = True,
) -> AIPlanningTurnResponse:
    """Save selected drafts as test cases, optionally execute them."""
    planning_session = db.query(AIPlanningSessionModel).get(session_id)
    if not planning_session:
        raise EntityNotFoundError("session", session_id)

    # Fetch selected drafts
    drafts = (
        db.query(AIPlanningDraftModel)
        .filter(
            AIPlanningDraftModel.session_id == session_id,
            AIPlanningDraftModel.id.in_(draft_ids),
        )
        .all()
    )

    saved_cases: list[SavedCaseResult] = []
    for draft in drafts:
        if not draft.dsl_case_json:
            continue
        case_payload = CaseCreateRequest(
            project_id=planning_session.project_id,
            actor_user_id=actor_user_id,
            **draft.dsl_case_json,
        )
        case = create_case(db, case_payload, actor_user_id=actor_user_id)
        saved_cases.append(SavedCaseResult(case_id=case.id, case_name=case.name))
        # Update draft status
        draft.status = "imported"

    db.commit()

    if not execute or not saved_cases:
        return AIPlanningTurnResponse(
            assistant_message=f"已保存 {len(saved_cases)} 个测试用例。" + ("\n是否立即执行？" if saved_cases else ""),
            session_status="saving" if not execute else "executing",
            requirements=AIPlanningSessionDetail.model_validate(planning_session).session.requirements if False else AIPlanningRequirements(),
            missing_slots=[],
            plan=None,
            drafts=[],
            next_action="ask_followup",
            saved_cases=saved_cases,
        )

    # Execute
    execution_summaries: list[ExecutionSummaryResult] = []
    for saved in saved_cases:
        payload = CaseExecutionRequest(actor_user_id=actor_user_id)
        result = execute_case(db, saved.case_id, payload)
        passed = sum(1 for s in (result.report.steps or []) if s.status == "passed")
        failed = sum(1 for s in (result.report.steps or []) if s.status == "failed")
        execution_summaries.append(ExecutionSummaryResult(
            execution_id=result.id,
            case_id=saved.case_id,
            case_name=saved.case_name,
            status=result.status,
            total_steps=result.total_steps,
            passed_steps=passed,
            failed_steps=failed,
            duration_ms=result.duration_ms,
            screenshot_url=result.latest_screenshot_url,
            report_url=f"/run/{result.id}",
        ))

    # Build summary message
    lines = ["测试执行完成：\n"]
    for ex in execution_summaries:
        icon = "✅" if ex.status == "passed" else "❌"
        lines.append(f"{icon} {ex.case_name} — {ex.status} ({ex.passed_steps}/{ex.total_steps}步)")

    return AIPlanningTurnResponse(
        assistant_message="\n".join(lines),
        session_status="completed",
        requirements=AIPlanningRequirements(),
        missing_slots=[],
        plan=None,
        drafts=[],
        next_action="ask_followup",
        saved_cases=saved_cases,
        execution_summaries=execution_summaries,
    )
```

- [ ] **Step 5: Add route endpoint**

In `backend/app/api/routes/ai_planning.py`, add:

```python
class SaveAndExecuteRequest(DSLModel):
    draft_ids: list[int] = Field(min_length=1)
    execute: bool = True

@router.post("/sessions/{session_id}/drafts:save-and-execute", response_model=AIPlanningTurnResponse)
def save_and_execute_route(
    session_id: int,
    payload: SaveAndExecuteRequest,
    session: Session = Depends(get_db_session),
    current_user: User = Depends(require_demo_user),
):
    return save_and_execute_selected_drafts(session, session_id, payload.draft_ids, current_user.id, payload.execute)
```

- [ ] **Step 6: Verify backend starts**

Run: `cd backend && python -c "from app.main import app; print('OK')"`
Expected: `OK`

- [ ] **Step 7: Commit**

```bash
git add backend/app/schemas/ai_planning.py backend/app/services/ai_planning.py backend/app/api/routes/ai_planning.py
git commit -m "feat: add save-and-execute endpoint for AI planning drafts"
```

---

## Task 4: Frontend — Draft Review Cards + Save/Execute Integration

**Files:**
- Modify: `frontend/src/services/api.ts` — add API client
- Modify: `frontend/src/components/AITestPlanningPanel.tsx` — add review cards and execute logic

- [ ] **Step 1: Add API client function**

In `frontend/src/services/api.ts`, add:

```typescript
export function saveAndExecuteDrafts(sessionId: number, draftIds: number[], execute: boolean = true) {
  return request<AIPlanningTurnResponse>(`/api/v1/ai-planning/sessions/${sessionId}/drafts:save-and-execute`, {
    method: "POST",
    body: JSON.stringify({ draft_ids: draftIds, execute }),
  });
}
```

- [ ] **Step 2: Add types for new response fields**

Add to the types:

```typescript
export interface SavedCaseResult {
  case_id: number;
  case_name: string;
  status: "saved";
}

export interface ExecutionSummaryResult {
  execution_id: number;
  case_id: number;
  case_name: string;
  status: "passed" | "failed" | "needs_intervention" | "error";
  total_steps: number;
  passed_steps: number;
  failed_steps: number;
  duration_ms: number | null;
  screenshot_url: string | null;
  report_url: string;
}
```

- [ ] **Step 3: Add draft review card with save+execute button**

In `AITestPlanningPanel.tsx`, in the drafts rendering section (around line 464), replace the individual "导入" button with a review card that includes a checkbox and a combined "保存并执行" button:

```tsx
{drafts.length > 0 && (
  <Card
    title="测试用例草案"
    size="small"
    extra={
      <Space>
        <Button
          size="small"
          onClick={async () => {
            if (!sessionId || selectedScenarioKeys.length === 0) return;
            setIsSending(true);
            try {
              const resp = await saveAndExecuteDrafts(sessionId, selectedScenarioKeys, false);
              messageApi.success(`已保存 ${resp.saved_cases?.length ?? 0} 个用例`);
              await loadSessionList();
            } catch (err: any) {
              messageApi.error("保存失败: " + (err.message ?? String(err)));
            } finally {
              setIsSending(false);
            }
          }}
        >
          仅保存
        </Button>
        <Button
          type="primary"
          size="small"
          loading={isSending}
          onClick={async () => {
            if (!sessionId || selectedScenarioKeys.length === 0) return;
            setIsSending(true);
            try {
              const resp = await saveAndExecuteDrafts(sessionId, selectedScenarioKeys, true);
              // Add execution summary as a chat message
              if (resp.execution_summaries && resp.execution_summaries.length > 0) {
                const summaryMsg: AIPlanningMessage = {
                  id: Date.now(),
                  session_id: sessionId!,
                  role: "assistant",
                  turn_type: "execution_summary",
                  content: resp.assistant_message,
                  structured_payload_json: {
                    execution_summaries: resp.execution_summaries,
                    saved_cases: resp.saved_cases,
                  },
                  created_at: new Date().toISOString(),
                };
                setTranscript((prev) => [...prev, summaryMsg]);
              }
              await loadSessionList();
            } catch (err: any) {
              messageApi.error("执行失败: " + (err.message ?? String(err)));
            } finally {
              setIsSending(false);
            }
          }}
        >
          保存并执行
        </Button>
      </Space>
    }
  >
    {drafts.map((draft) => (
      <div key={draft.id} style={{ marginBottom: 8 }}>
        <Checkbox
          checked={selectedScenarioKeys.includes(draft.scenario_key)}
          onChange={(e) => {
            setSelectedScenarioKeys((prev) =>
              e.target.checked
                ? [...prev, draft.scenario_key]
                : prev.filter((k) => k !== draft.scenario_key)
            );
          }}
        >
          <Text strong>{draft.title}</Text>
        </Checkbox>
        {draft.dsl_case && (
          <div style={{ marginLeft: 24, color: "#888", fontSize: 12 }}>
            {draft.dsl_case.steps.map((s) => s.action).join(" → ")}
          </div>
        )}
      </div>
    ))}
  </Card>
)}
```

- [ ] **Step 4: Add execution summary message renderer**

In the transcript rendering section, add a case for `turn_type === "execution_summary"`:

```tsx
{msg.turn_type === "execution_summary" && msg.structured_payload_json && (
  <Card size="small" style={{ marginTop: 8 }}>
    <div style={{ whiteSpace: "pre-wrap" }}>{msg.content}</div>
    {(msg.structured_payload_json.execution_summaries as ExecutionSummaryResult[])?.map((ex) => (
      <div key={ex.execution_id} style={{ marginTop: 8, display: "flex", alignItems: "center", gap: 8 }}>
        {ex.status === "passed" ? "✅" : "❌"}
        <Text>{ex.case_name}</Text>
        <Text type="secondary">{ex.passed_steps}/{ex.total_steps}步</Text>
        {ex.duration_ms && <Text type="secondary">{(ex.duration_ms / 1000).toFixed(1)}s</Text>}
        <Button
          type="link"
          size="small"
          onClick={() => navigate(ex.report_url)}
        >
          查看报告
        </Button>
      </div>
    ))}
  </Card>
)}
```

Note: This requires `useNavigate()` from react-router. Add at top of component:
```typescript
const navigate = useNavigate();
```

- [ ] **Step 5: Verify frontend compiles**

Run: `cd frontend && npx tsc --noEmit`
Expected: No errors

- [ ] **Step 6: Commit**

```bash
git add frontend/src/services/api.ts frontend/src/components/AITestPlanningPanel.tsx
git commit -m "feat: add draft review cards and save+execute integration"
```

---

## Validation

After all tasks are complete, validate with the test data from the design spec:

1. Create a new session, paste the Login Page test data
2. Verify AI collects requirements and generates a test plan
3. Generate DSL drafts, verify they contain correct steps
4. Select drafts and click "保存并执行"
5. Verify execution completes and summary appears in chat
6. Navigate away, come back — verify session is restored via switcher
7. Verify "查看报告" link navigates to the correct execution report page
