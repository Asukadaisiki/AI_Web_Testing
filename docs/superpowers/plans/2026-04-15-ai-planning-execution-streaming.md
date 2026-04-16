# AI Planning Execution Streaming Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the blocking AI planning save-and-execute UX with a cancellable WebSocket stream that shows save progress, case progress, step progress, and the persisted final summary.

**Architecture:** Keep `POST /drafts:save-and-execute` as fallback, and add `WS /api/v1/ai-planning/sessions/{session_id}/ws` as the Planning-panel primary path. Backend bridges the current synchronous SQLAlchemy + Playwright stack through a worker-thread async generator so the socket can still receive `cancel`. Frontend adds a socket client plus one transient `execution_progress` chat bubble that is replaced by the persisted `execution_summary` after reload.

**Tech Stack:** FastAPI WebSocket, SQLAlchemy sessionmaker, Playwright sync runner, React 18, TypeScript, Vitest

---

## File Structure

### Backend

| File | Change |
|------|--------|
| `backend/app/api/auth.py` | Reuse demo-user lookup for WebSocket auth |
| `backend/app/api/routes/ai_planning.py` | Add WS route, receive `execute`/`cancel`, clean up cancellation state |
| `backend/app/runners/playwright_runner.py` | Add step streaming generator and `RunnerCancelledError` |
| `backend/app/runners/__init__.py` | Export new runner symbols |
| `backend/app/services/executions.py` | Add `execute_case_streaming()` while preserving `execute_case()` |
| `backend/app/services/ai_planning_streaming.py` | New worker-thread bridge + event serializer + final summary persistence |
| `backend/tests/unit/test_case_executions_api.py` | Execution streaming tests |
| `backend/tests/unit/test_ai_planning_api.py` | WebSocket flow tests |

### Frontend

| File | Change |
|------|--------|
| `frontend/src/services/executionWebSocket.ts` | New socket lifecycle client |
| `frontend/src/services/executionWebSocket.test.ts` | New socket unit tests |
| `frontend/src/types/api.ts` | Add stream event and progress payload types |
| `frontend/src/components/AITestPlanningPanel.tsx` | Switch execute path to WS, render progress bubble, support cancel |
| `frontend/src/components/AITestPlanningPanel.test.tsx` | Panel streaming tests |

### Out of Scope

- No Alembic migration.
- No removal of the existing synchronous HTTP execute endpoint.
- No new persisted execution status like `cancelled`; cancellation stays transient in Planning UI and terminal WS events.

---

## Task 1: Runner And Execution Service Streaming

**Files:**
- Modify: `backend/app/runners/playwright_runner.py`
- Modify: `backend/app/runners/__init__.py`
- Modify: `backend/app/services/executions.py`
- Modify: `backend/tests/unit/test_case_executions_api.py`

- [ ] **Step 1: Add failing execution streaming tests**

```python
def test_execute_case_streaming_yields_step_events_and_returns_detail(...):
    stream = execution_service.execute_case_streaming(session, case_id, CaseExecutionRequest(actor_user_id=1))
    events = []
    try:
        while True:
            events.append(next(stream))
    except StopIteration as stop:
        detail = stop.value
    assert [event.type for event in events] == ["step_start", "step_complete"]
    assert detail.status == "passed"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_case_executions_api.py -q`
Expected: FAIL because `execute_case_streaming` / `StepStreamEvent` do not exist.

- [ ] **Step 3: Implement runner streaming primitive and service wrapper**

```python
@dataclass(frozen=True)
class StepStreamEvent:
    type: Literal["step_start", "step_complete"]
    step_index: int
    action: str
    target: str | None = None
    value: str | None = None
    status: Literal["passed", "failed"] | None = None
    duration_ms: int | None = None

class RunnerCancelledError(RuntimeError):
    def __init__(self, message: str, *, step_results: list[StepExecutionEvidence] | None = None) -> None:
        super().__init__(message)
        self.step_results = step_results or []

def execute_case_with_playwright_streaming(..., cancel_event: Event | None = None) -> Generator[StepStreamEvent, None, list[StepExecutionEvidence]]:
    for index, step in enumerate(case.steps):
        if cancel_event is not None and cancel_event.is_set():
            raise RunnerCancelledError("Execution cancelled by user.", step_results=step_results)
        yield StepStreamEvent(type="step_start", step_index=index, action=step.action, target=getattr(step, "target", None), value=getattr(step, "value", None))
        # keep existing step execution body
        yield StepStreamEvent(type="step_complete", step_index=index, action=step.action, status=step_results[-1].status, duration_ms=step_results[-1].duration_ms)
    return step_results

def execute_case_streaming(..., cancel_event: Event | None = None) -> Generator[StepStreamEvent, None, StoredCaseExecutionDetail]:
    step_results = yield from execute_case_with_playwright_streaming(..., cancel_event=cancel_event)
    ...
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_case_executions_api.py -q`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/runners/playwright_runner.py backend/app/runners/__init__.py backend/app/services/executions.py backend/tests/unit/test_case_executions_api.py
git commit -m "feat: add streaming execution primitives"
```

## Task 2: AI Planning Streaming Worker And WebSocket Route

**Files:**
- Create: `backend/app/services/ai_planning_streaming.py`
- Modify: `backend/app/api/auth.py`
- Modify: `backend/app/api/routes/ai_planning.py`
- Modify: `backend/tests/unit/test_ai_planning_api.py`

- [ ] **Step 1: Add failing WebSocket tests**

```python
def test_ai_planning_ws_streams_events_in_order(client, monkeypatch):
    async def fake_stream(**kwargs):
        yield {"type": "save_progress", "saved_count": 1, "total": 1, "case_name": "登录成功"}
        yield {"type": "done"}
    monkeypatch.setattr(ai_planning_streaming, "stream_save_and_execute", fake_stream)
    with client.websocket_connect(f"/api/v1/ai-planning/sessions/{session_id}/ws?user_id=1") as ws:
        ws.send_json({"type": "execute", "draft_ids": [11]})
        assert ws.receive_json()["type"] == "save_progress"
        assert ws.receive_json()["type"] == "done"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_ai_planning_api.py -q`
Expected: FAIL because WS route and `stream_save_and_execute()` do not exist.

- [ ] **Step 3: Implement demo-user lookup, worker-thread bridge, and WS route**

```python
def get_demo_user_or_raise(session: Session, *, user_id: int | None = None) -> User:
    resolved_user_id = user_id or DEFAULT_DEMO_USER_ID
    ...

class CancellationManager:
    def register(self, session_id: int) -> Event: ...
    def clear(self, session_id: int) -> None: ...

async def stream_save_and_execute(*, session_factory, planning_session_id, draft_ids, actor_user_id, cancel_event) -> AsyncGenerator[dict, None]:
    queue: asyncio.Queue[dict | _TerminalSignal] = asyncio.Queue()
    Thread(target=worker, daemon=True).start()
    while True:
        item = await queue.get()
        ...

@router.websocket("/sessions/{session_id}/ws")
async def ai_planning_session_ws(websocket: WebSocket, session_id: int) -> None:
    current_user = get_demo_user_or_raise(session, user_id=user_id)
    await websocket.accept()
    cancel_event = _cancellation_manager.register(session_id)
    ...
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_ai_planning_api.py tests/unit/test_case_executions_api.py -q`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/api/auth.py backend/app/api/routes/ai_planning.py backend/app/services/ai_planning_streaming.py backend/tests/unit/test_ai_planning_api.py
git commit -m "feat: add ai planning websocket execution stream"
```

## Task 3: Frontend Socket Client

**Files:**
- Create: `frontend/src/services/executionWebSocket.ts`
- Create: `frontend/src/services/executionWebSocket.test.ts`
- Modify: `frontend/src/types/api.ts`

- [ ] **Step 1: Add failing socket tests**

```typescript
it("builds the planning websocket URL and forwards parsed events", () => {
  const onEvent = vi.fn();
  connectExecutionStream(5, onEvent, vi.fn());
  expect(MockWebSocket.instances[0].url).toContain("/api/v1/ai-planning/sessions/5/ws?user_id=1");
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npm run test -- src/services/executionWebSocket.test.ts`
Expected: FAIL because the socket client module does not exist.

- [ ] **Step 3: Implement stream types and socket lifecycle**

```typescript
export type ExecutionStreamEvent =
  | { type: "save_progress"; saved_count: number; total: number; case_name: string }
  | { type: "case_start"; case_id: number; case_name: string; total_steps: number }
  | { type: "step_start"; case_id: number; step_index: number; action: string; target?: string | null }
  | { type: "step_complete"; case_id: number; step_index: number; action: string; status: "passed" | "failed"; duration_ms: number }
  | { type: "execution_summary"; message: string; structured_payload: { type: "execution_summary"; saved_cases: SavedCaseResult[]; execution_summaries: ExecutionSummaryResult[] } }
  | { type: "cancelled" }
  | { type: "done" }
  | { type: "error"; message: string };

export function connectExecutionStream(sessionId: number, onEvent: (event: ExecutionStreamEvent) => void, onError: (error: Error) => void) {
  ...
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npm run test -- src/services/executionWebSocket.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/api.ts frontend/src/services/executionWebSocket.ts frontend/src/services/executionWebSocket.test.ts
git commit -m "feat: add planning execution websocket client"
```

## Task 4: Planning Panel Streaming Integration

**Files:**
- Modify: `frontend/src/components/AITestPlanningPanel.tsx`
- Modify: `frontend/src/components/AITestPlanningPanel.test.tsx`

- [ ] **Step 1: Add failing panel tests**

```typescript
test("保存并执行改为显示流式进度并在 done 后回读会话详情", async () => {
  await userEvent.click(screen.getByRole("button", { name: "保存并执行" }));
  expect(send).toHaveBeenCalledWith({ type: "execute", draft_ids: [11] });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npm run test -- src/components/AITestPlanningPanel.test.tsx`
Expected: FAIL because the panel still calls the blocking HTTP execute path.

- [ ] **Step 3: Implement progress bubble, socket integration, and cancel button**

```typescript
function createExecutionProgressMessage(sessionId: number): AIPlanningMessage {
  return createOptimisticMessage(sessionId, "assistant", "followup", "正在保存并执行已选草案…", {
    type: "execution_progress",
    saved_count: 0,
    total: 0,
    cases: [],
  });
}

async function handleExecuteSelectedDrafts() {
  const progressMessage = createExecutionProgressMessage(sessionId!);
  setTranscript((current) => [...current, progressMessage]);
  executionStreamRef.current = connectExecutionStream(sessionId!, async (event) => {
    if (event.type === "done") {
      await loadSessionDetail(sessionId!);
      await loadSessionList();
      return;
    }
    setTranscript((current) => current.map((message) => message.id === progressMessage.id ? { ...message, structured_payload: applyExecutionStreamEvent(message.structured_payload as ExecutionProgressStructuredPayload, event) } : message));
  }, async (error) => {
    void messageApi.error(error.message);
    await loadSessionDetail(sessionId!);
  });
  executionStreamRef.current.send({ type: "execute", draft_ids });
}
```

- [ ] **Step 4: Run test and type-check to verify they pass**

Run: `cd frontend && npm run test -- src/services/executionWebSocket.test.ts src/components/AITestPlanningPanel.test.tsx`
Expected: PASS

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/AITestPlanningPanel.tsx frontend/src/components/AITestPlanningPanel.test.tsx
git commit -m "feat: stream ai planning execution progress in panel"
```

---

## Validation

1. Backend: `cd backend && uv run pytest tests/unit/test_ai_planning_api.py tests/unit/test_case_executions_api.py -q`
2. Frontend tests: `cd frontend && npm run test -- src/services/executionWebSocket.test.ts src/components/AITestPlanningPanel.test.tsx`
3. Frontend type-check: `cd frontend && npx tsc --noEmit`
4. Manual smoke:
   Start backend with `cd backend && uv run uvicorn app.main:create_app --factory --reload`
   Start frontend with `cd frontend && npm run dev`
   In Planning page click `保存并执行`, observe save/case/step updates, click `取消执行`, then refresh and confirm only persisted `execution_summary` remains.

## Implementation Notes

- Keep all runnable steps DSL-driven; transport changes only the UX timing.
- Final persisted summary should only include completed case runs.
- Use `user_id=1` query param for the first implementation because the repo still uses demo-user auth; do not invent a wider auth system in this task.
