# WebSocket → SSE Refactoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single WebSocket connection with per-operation SSE streams for the AI planning agent chat.

**Architecture:** Each operation (chat, generate drafts, execute, execute-with-judge) gets its own POST endpoint returning an SSE stream. The stream lives only for the operation duration, then closes. Cancellation uses a separate POST endpoint. Backend keeps the existing async generator bridge; only the transport layer changes. Frontend replaces WebSocket client with a fetch-based SSE reader.

**Tech Stack:** FastAPI StreamingResponse (SSE), fetch + ReadableStream (frontend), no new dependencies.

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `backend/app/services/ai_planning_streaming.py` | Modify | Add `sse_event()` helper, keep CancellationManager and async generators |
| `backend/app/api/routes/ai_planning.py` | Modify | Delete WebSocket endpoint, add 5 SSE POST endpoints |
| `frontend/src/services/executionWebSocket.ts` | Delete | No longer needed |
| `frontend/src/services/executionWebSocket.test.ts` | Delete | Tests for deleted file |
| `frontend/src/services/sseClient.ts` | Create | Generic SSE client using fetch + ReadableStream |
| `frontend/src/services/sseClient.test.ts` | Create | Tests for SSE client |
| `frontend/src/components/AITestPlanningPanel.tsx` | Modify | Replace WebSocket with SSE calls |
| `frontend/src/components/AITestPlanningPanel.test.tsx` | Modify | Update mocks from WebSocket to SSE |

---

### Task 1: Backend — Add SSE helper to streaming module

**Files:**
- Modify: `backend/app/services/ai_planning_streaming.py`

- [ ] **Step 1: Add `sse_event` helper function**

Append this function after the existing `_serialize_event` function (after line 43):

```python
def sse_event(event_type: str, data: dict) -> str:
    """Format a dict as an SSE event string."""
    payload = json.dumps(data, default=str, ensure_ascii=False)
    return f"event: {event_type}\ndata: {payload}\n\n"
```

- [ ] **Step 2: Verify the module imports and structure are intact**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run python -c "from app.services.ai_planning_streaming import sse_event; print(sse_event('status', {'phase': 'thinking'}))"`

Expected output:
```
event: status
data: {"phase": "thinking"}

```

- [ ] **Step 3: Commit**

```bash
git add backend/app/services/ai_planning_streaming.py
git commit -m "feat: add SSE event formatter to streaming module"
```

---

### Task 2: Backend — Replace WebSocket endpoint with SSE POST endpoints

**Files:**
- Modify: `backend/app/api/routes/ai_planning.py`

- [ ] **Step 1: Update imports**

In `backend/app/api/routes/ai_planning.py`, replace the imports at lines 1-48 with:

```python
"""Routes for AI planning sessions and drafts."""

from __future__ import annotations

import logging

from fastapi import APIRouter, Depends, HTTPException, Response, status
from fastapi.responses import StreamingResponse
from sqlalchemy.orm import Session

from app.api.auth import get_demo_user_or_raise, require_demo_user
from app.db import get_db_session
from app.db.session import get_session_factory
from app.models import User
from app.schemas.ai_planning import (
    AIPlanningDraft,
    AIPlanningMessageCreateRequest,
    AIPlanningSessionDetail,
    AIPlanningSessionSummary,
    AIPlanningTurnResponse,
    CreateAIPlanningSessionRequest,
    GenerateAIPlanningDraftsRequest,
    UpdateAIPlanningDraftStatusRequest,
)
from app.schemas.dsl import DSLModel
from pydantic import Field
from app.services.ai_planning import (
    AIPlanningAccessError,
    create_planning_session,
    delete_planning_draft,
    delete_planning_session,
    generate_planning_drafts,
    get_planning_session_detail,
    list_planning_sessions,
    retest_cases,
    save_and_execute_selected_drafts,
    send_planning_message,
    update_planning_draft_status,
)
from app.services.ai_planning_streaming import (
    CancellationManager,
    sse_event,
    stream_explorer_judge,
    stream_planning_chat,
    stream_planning_drafts,
    stream_save_and_execute,
)
from app.services.cases import EntityNotFoundError
```

Key changes: remove `asyncio`, `Query`, `WebSocket`, `WebSocketDisconnect`. Add `StreamingResponse`. Add `sse_event` import.

- [ ] **Step 2: Add request body schemas**

After the `RetestRequest` class (after line 179), add:

```python
class ChatSSERequest(DSLModel):
    content: str
    scenario_keys: list[str] = Field(default_factory=list)


class ExecuteSSERequest(DSLModel):
    draft_ids: list[int]
```

- [ ] **Step 3: Delete the entire WebSocket endpoint**

Delete lines 217-335 (the `@router.websocket` endpoint and its function `ai_planning_session_ws`).

- [ ] **Step 4: Add SSE endpoints**

In place of the deleted WebSocket endpoint, add these 5 endpoints:

```python
@router.post("/sessions/{session_id}/chat")
async def chat_sse(
    session_id: int,
    req: ChatSSERequest,
    current_user: User = Depends(require_demo_user),
) -> StreamingResponse:
    """SSE stream for AI planning chat."""
    session_factory = get_session_factory()

    async def event_generator():
        try:
            async for event in stream_planning_chat(
                session_factory=session_factory,
                planning_session_id=session_id,
                content=req.content,
                actor_user_id=current_user.id,
            ):
                yield sse_event(event.get("type", "message"), event)
        except Exception as exc:
            logger.exception("SSE chat streaming error for session %s", session_id)
            yield sse_event("error", {"message": str(exc)})
        yield sse_event("done", {})

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


@router.post("/sessions/{session_id}/drafts")
async def drafts_sse(
    session_id: int,
    req: GenerateAIPlanningDraftsRequest,
    current_user: User = Depends(require_demo_user),
) -> StreamingResponse:
    """SSE stream for draft generation."""
    session_factory = get_session_factory()

    async def event_generator():
        try:
            async for event in stream_planning_drafts(
                session_factory=session_factory,
                planning_session_id=session_id,
                payload=req,
                actor_user_id=current_user.id,
            ):
                yield sse_event(event.get("type", "message"), event)
        except Exception as exc:
            logger.exception("SSE draft streaming error for session %s", session_id)
            yield sse_event("error", {"message": str(exc)})
        yield sse_event("done", {})

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


@router.post("/sessions/{session_id}/execute")
async def execute_sse(
    session_id: int,
    req: ExecuteSSERequest,
    current_user: User = Depends(require_demo_user),
) -> StreamingResponse:
    """SSE stream for save-and-execute."""
    session_factory = get_session_factory()
    cancel_event = _cancellation_manager.register(session_id)

    async def event_generator():
        try:
            async for event in stream_save_and_execute(
                session_factory=session_factory,
                planning_session_id=session_id,
                draft_ids=req.draft_ids,
                actor_user_id=current_user.id,
                cancel_event=cancel_event,
            ):
                yield sse_event(event.get("type", "message"), event)
        except Exception as exc:
            logger.exception("SSE execute streaming error for session %s", session_id)
            yield sse_event("error", {"message": str(exc)})
        yield sse_event("done", {})
        _cancellation_manager.clear(session_id)

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


@router.post("/sessions/{session_id}/execute-with-judge")
async def execute_with_judge_sse(
    session_id: int,
    req: ExecuteSSERequest,
    current_user: User = Depends(require_demo_user),
) -> StreamingResponse:
    """SSE stream for Explorer-Judge execution."""
    session_factory = get_session_factory()
    cancel_event = _cancellation_manager.register(session_id)

    async def event_generator():
        try:
            async for event in stream_explorer_judge(
                session_factory=session_factory,
                planning_session_id=session_id,
                draft_ids=req.draft_ids,
                actor_user_id=current_user.id,
                cancel_event=cancel_event,
            ):
                yield sse_event(event.get("type", "message"), event)
        except Exception as exc:
            logger.exception("SSE Explorer-Judge error for session %s", session_id)
            yield sse_event("error", {"message": str(exc)})
        yield sse_event("done", {})
        _cancellation_manager.clear(session_id)

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"},
    )


@router.post("/sessions/{session_id}/cancel")
async def cancel_execution(
    session_id: int,
    current_user: User = Depends(require_demo_user),
) -> dict:
    """Cancel the in-progress execution for a planning session."""
    cancel_event = _cancellation_manager.get(session_id)
    if cancel_event is not None:
        cancel_event.set()
        _cancellation_manager.clear(session_id)
        return {"status": "cancelled"}
    return {"status": "no_active_execution"}
```

- [ ] **Step 5: Verify the backend starts**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run python -c "from app.api.routes.ai_planning import router; print('OK')"`

Expected: `OK`

- [ ] **Step 6: Commit**

```bash
git add backend/app/api/routes/ai_planning.py
git commit -m "feat: replace WebSocket with SSE POST endpoints for AI planning"
```

---

### Task 3: Frontend — Create SSE client and delete WebSocket client

**Files:**
- Delete: `frontend/src/services/executionWebSocket.ts`
- Delete: `frontend/src/services/executionWebSocket.test.ts`
- Create: `frontend/src/services/sseClient.ts`
- Create: `frontend/src/services/sseClient.test.ts`

- [ ] **Step 1: Create `frontend/src/services/sseClient.ts`**

```typescript
/**
 * Generic SSE client using fetch + ReadableStream.
 * Supports POST requests with JSON bodies (unlike EventSource which is GET-only).
 */

export interface SSEClientOptions {
  url: string;
  body: Record<string, unknown>;
  onEvent: (eventType: string, data: unknown) => void;
}

export async function callSSE(opts: SSEClientOptions): Promise<void> {
  const response = await fetch(opts.url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(opts.body),
  });

  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
  }

  const reader = response.body!.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const parts = buffer.split("\n\n");
    buffer = parts.pop() ?? "";

    for (const part of parts) {
      let eventType = "message";
      let eventData = "{}";
      for (const line of part.split("\n")) {
        if (line.startsWith("event: ")) {
          eventType = line.slice(7);
        } else if (line.startsWith("data: ")) {
          eventData = line.slice(6);
        }
      }
      try {
        opts.onEvent(eventType, JSON.parse(eventData));
      } catch {
        // Ignore malformed JSON
      }
    }
  }
}

export async function cancelExecution(sessionId: number): Promise<{ status: string }> {
  const response = await fetch(`/api/v1/ai-planning/sessions/${sessionId}/cancel`, {
    method: "POST",
  });
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
  }
  return response.json();
}
```

- [ ] **Step 2: Create `frontend/src/services/sseClient.test.ts`**

```typescript
import { describe, expect, test, vi, beforeEach, afterEach } from "vitest";
import { callSSE, cancelExecution } from "./sseClient";

function createMockStream(chunks: string[]) {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    },
  });
}

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve({
        ok: true,
        status: 200,
        body: createMockStream([]),
      } as Response),
    ),
  );
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("callSSE", () => {
  test("sends POST request and parses SSE events", async () => {
    const events: Array<{ type: string; data: unknown }> = [];
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      status: 200,
      body: createMockStream([
        'event: status\ndata: {"phase":"thinking"}\n\n',
        'event: text_chunk\ndata: {"text":"hello"}\n\n',
        "event: done\ndata: {}\n\n",
      ]),
    } as unknown as Response);

    await callSSE({
      url: "/api/v1/ai-planning/sessions/1/chat",
      body: { content: "test" },
      onEvent: (type, data) => events.push({ type, data }),
    });

    expect(fetch).toHaveBeenCalledWith("/api/v1/ai-planning/sessions/1/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content: "test" }),
    });
    expect(events).toEqual([
      { type: "status", data: { phase: "thinking" } },
      { type: "text_chunk", data: { text: "hello" } },
      { type: "done", data: {} },
    ]);
  });

  test("handles split chunks across reads", async () => {
    const events: Array<{ type: string; data: unknown }> = [];
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      status: 200,
      body: createMockStream([
        'event: text_chunk\ndata: {"text":"hel',
        'lo"}\n\nevent: done\ndata: {}\n\n',
      ]),
    } as unknown as Response);

    await callSSE({
      url: "/api/test",
      body: {},
      onEvent: (type, data) => events.push({ type, data }),
    });

    expect(events).toEqual([
      { type: "text_chunk", data: { text: "hello" } },
      { type: "done", data: {} },
    ]);
  });

  test("throws on non-2xx response", async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: false,
      status: 500,
      statusText: "Internal Server Error",
    } as Response);

    await expect(
      callSSE({ url: "/api/test", body: {}, onEvent: vi.fn() }),
    ).rejects.toThrow("HTTP 500");
  });

  test("ignores malformed JSON in data", async () => {
    const events: Array<{ type: string; data: unknown }> = [];
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      status: 200,
      body: createMockStream(["event: test\ndata: not-json\n\n"]),
    } as unknown as Response);

    await callSSE({
      url: "/api/test",
      body: {},
      onEvent: (type, data) => events.push({ type, data }),
    });

    expect(events).toEqual([]);
  });
});

describe("cancelExecution", () => {
  test("POSTs to cancel endpoint and returns result", async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ status: "cancelled" }),
    } as Response);

    const result = await cancelExecution(5);
    expect(fetch).toHaveBeenCalledWith("/api/v1/ai-planning/sessions/5/cancel", {
      method: "POST",
    });
    expect(result).toEqual({ status: "cancelled" });
  });

  test("throws on non-2xx response", async () => {
    vi.mocked(fetch).mockResolvedValueOnce({
      ok: false,
      status: 404,
      statusText: "Not Found",
    } as Response);

    await expect(cancelExecution(999)).rejects.toThrow("HTTP 404");
  });
});
```

- [ ] **Step 3: Run the new tests**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/frontend && npm test -- --run src/services/sseClient.test.ts`

Expected: All tests pass.

- [ ] **Step 4: Delete WebSocket client files**

Delete `frontend/src/services/executionWebSocket.ts` and `frontend/src/services/executionWebSocket.test.ts`.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/services/sseClient.ts frontend/src/services/sseClient.test.ts
git rm frontend/src/services/executionWebSocket.ts frontend/src/services/executionWebSocket.test.ts
git commit -m "feat: replace WebSocket client with fetch-based SSE client"
```

---

### Task 4: Frontend — Update AITestPlanningPanel to use SSE

**Files:**
- Modify: `frontend/src/components/AITestPlanningPanel.tsx`

- [ ] **Step 1: Update imports**

Replace lines 1-34 with:

```typescript
import { useEffect, useMemo, useRef, useState } from "react";
import { Alert, Button, Checkbox, Input, Progress, Select, Tag, Typography, message } from "antd";
import { DeleteOutlined, SendOutlined, CheckCircleFilled, LoadingOutlined, ClockCircleOutlined } from "@ant-design/icons";
import { useQueryClient } from "@tanstack/react-query";

import {
  createPlanningSession,
  deletePlanningDraft,
  deletePlanningSession,
  generatePlanningDrafts,
  getPlanningSession,
  listPlanningSessions,
  saveAndExecuteDrafts,
  sendPlanningMessage,
  updatePlanningDraftStatus,
} from "../services/api";
import { callSSE, cancelExecution } from "../services/sseClient";
import type {
  AIPlanningDraft,
  AIPlanningMessage,
  AIPlanningPlan,
  AIPlanningRequirements,
  AIPlanningSessionSummary,
  AIPlanningToolCall,
  AISettings,
  DSLCaseInputContract,
  DSLCaseOutputContract,
  DSLCasePayload,
  DSLStep,
  ExecutionStreamEvent,
  ExecutionSummaryResult,
} from "../types/api";
import { NotebookLMLayout } from "../layouts/NotebookLMLayout";
import VerdictPanel from "./VerdictPanel";
```

Key change: remove `connectExecutionStream` and `ExecutionStreamClient` imports. Add `callSSE` and `cancelExecution` imports.

- [ ] **Step 2: Remove WebSocket refs**

In the component state declarations, delete these two lines (around line 198-199):

```typescript
// DELETE these:
const executionStreamRef = useRef<ExecutionStreamClient | null>(null);
const wsClientRef = useRef<ExecutionStreamClient | null>(null);
```

- [ ] **Step 3: Replace WebSocket useEffect with extracted stream event handler**

Delete the entire `useEffect` at lines 309-467 (the one that creates a persistent WebSocket connection).

Replace it with a standalone function (place it after the `useEffect` for session initialization, around line 308):

```typescript
  function handleStreamEvent(event: ExecutionStreamEvent) {
    if (
      event.type === "status" ||
      event.type === "text_chunk" ||
      event.type === "tool_call_start" ||
      event.type === "tool_call_end"
    ) {
      const targetId = activeAssistantMessageIdRef.current;
      if (targetId == null) return;

      setTranscript((current) =>
        current.map((msg) => {
          if (msg.id !== targetId) return msg;
          const payload = (msg.structured_payload ?? {}) as Record<string, unknown>;
          if (event.type === "status") {
            return {
              ...msg,
              structured_payload: { ...payload, _phase: event.phase, _phaseMessage: event.message, _streaming: true },
            };
          }
          if (event.type === "text_chunk") {
            return { ...msg, content: msg.content + event.text };
          }
          if (event.type === "tool_call_start") {
            return {
              ...msg,
              structured_payload: { ...payload, _phase: "tool_calling", _phaseMessage: `正在调用工具: ${event.tool}` },
            };
          }
          if (event.type === "tool_call_end") {
            return {
              ...msg,
              structured_payload: { ...payload, _phase: "thinking", _phaseMessage: "正在分析需求..." },
            };
          }
          return msg;
        }),
      );
      return;
    }

    if (event.type === "draft_generating") {
      const targetId = activeAssistantMessageIdRef.current;
      if (targetId == null) return;
      setTranscript((current) =>
        current.map((msg) =>
          msg.id === targetId
            ? {
                ...msg,
                structured_payload: {
                  ...(msg.structured_payload ?? {}),
                  _phase: "draft_generating",
                  _phaseMessage: event.message,
                  _streaming: true,
                },
              }
            : msg,
        ),
      );
      return;
    }

    if (
      event.type === "save_progress" ||
      event.type === "case_start" ||
      event.type === "step_start" ||
      event.type === "step_complete" ||
      event.type === "explorer_start" ||
      event.type === "explorer_complete" ||
      event.type === "judge_start" ||
      event.type === "judge_complete" ||
      event.type === "auto_fix_attempt" ||
      event.type === "auto_fix_result"
    ) {
      const targetId = activeAssistantMessageIdRef.current;
      if (targetId == null) return;
      setTranscript((current) =>
        current.map((msg) =>
          msg.id === targetId
            ? {
                ...msg,
                content: applyStreamEventToContent(msg.content, event),
                structured_payload: applyStreamEventToPayload(
                  msg.structured_payload as Record<string, unknown> | null,
                  event,
                ),
              }
            : msg,
        ),
      );
      return;
    }

    if (event.type === "verdict_report") {
      const targetId = activeAssistantMessageIdRef.current;
      if (targetId == null) return;
      setTranscript((current) =>
        current.map((msg) =>
          msg.id === targetId
            ? {
                ...msg,
                content: applyStreamEventToContent(msg.content, event),
                structured_payload: {
                  ...(msg.structured_payload as Record<string, unknown> | null ?? {}),
                  type: "verdict_report",
                  verdict: event.verdict,
                  requires_user_action: event.requires_user_action,
                },
              }
            : msg,
        ),
      );
      return;
    }

    if (event.type === "turn_complete") {
      if (sessionId) {
        void loadSessionDetail(sessionId);
        void loadSessionList();
      }
      setIsSending(false);
      setIsGenerating(false);
      return;
    }

    if (event.type === "done" || event.type === "cancelled" || event.type === "error") {
      if (sessionId) {
        void loadSessionDetail(sessionId);
        void loadSessionList();
        void queryClient.invalidateQueries({ queryKey: ["cases"] });
        void queryClient.invalidateQueries({ queryKey: ["executions"] });
      }
      setIsExecuting(false);
      if (event.type === "cancelled") {
        void messageApi.info("执行已取消");
      }
      if (event.type === "error") {
        void messageApi.error("执行错误: " + event.message);
      }
    }
  }
```

- [ ] **Step 4: Replace `handleSendMessage` with SSE**

Replace the `handleSendMessage` function (lines 481-534) with:

```typescript
  async function handleSendMessage() {
    if (!sessionId) {
      return;
    }
    const trimmed = inputValue.trim();
    if (!trimmed) {
      return;
    }

    setIsSending(true);
    const optimisticUser = createOptimisticMessage(sessionId, "user", "user", trimmed);
    const optimisticAssistant = createOptimisticMessage(sessionId, "assistant", "followup", "", {
      _phase: "thinking",
      _phaseMessage: "正在分析需求...",
      _streaming: true,
    });
    activeAssistantMessageIdRef.current = optimisticAssistant.id;
    setTranscript((current) => [...current, optimisticUser, optimisticAssistant]);
    setInputValue("");

    try {
      await callSSE({
        url: `/api/v1/ai-planning/sessions/${sessionId}/chat`,
        body: { content: trimmed },
        onEvent: (_type, data) => handleStreamEvent(data as ExecutionStreamEvent),
      });
    } catch (error) {
      void messageApi.error((error as Error).message);
    } finally {
      setIsSending(false);
    }
  }
```

- [ ] **Step 5: Replace `handleGenerateDrafts` with SSE**

Replace the `handleGenerateDrafts` function (lines 536-585) with:

```typescript
  async function handleGenerateDrafts() {
    if (!sessionId || !selectedScenarioKeys.length) {
      return;
    }
    setIsGenerating(true);

    const optimisticAssistant = createOptimisticMessage(sessionId, "assistant", "plan", "", {
      _phase: "generating",
      _phaseMessage: "正在生成 DSL...",
      _streaming: true,
    });
    activeAssistantMessageIdRef.current = optimisticAssistant.id;
    setTranscript((current) => [...current, optimisticAssistant]);

    try {
      await callSSE({
        url: `/api/v1/ai-planning/sessions/${sessionId}/drafts`,
        body: {
          scenario_keys: selectedScenarioKeys,
          current_case: currentCase ?? null,
          current_steps: currentSteps ?? null,
          current_input_contract: currentInputContract ?? null,
          current_output_contract: currentOutputContract ?? null,
          preserve_contracts: true,
        },
        onEvent: (_type, data) => handleStreamEvent(data as ExecutionStreamEvent),
      });
    } catch (error) {
      void messageApi.error((error as Error).message);
    } finally {
      setIsGenerating(false);
    }
  }
```

- [ ] **Step 6: Replace execute button onClick with SSE**

In the `renderRightCards` function, find the "保存并执行" Button's `onClick` handler (starting around line 1017). Replace the entire onClick body with:

```typescript
                  onClick={async () => {
                    if (!sessionId || selectedScenarioKeys.length === 0) return;
                    const draftIds = drafts
                      .filter((d) => selectedScenarioKeys.includes(d.scenario_key))
                      .map((d) => d.id);
                    setIsExecuting(true);

                    const progressMessage = createOptimisticMessage(
                      sessionId,
                      "assistant",
                      "followup",
                      "正在保存并执行已选草案…",
                      { type: "execution_progress", saved_count: 0, total: 0, cases: [] },
                    );
                    activeAssistantMessageIdRef.current = progressMessage.id;
                    setTranscript((current) => [...current, progressMessage]);

                    try {
                      await callSSE({
                        url: `/api/v1/ai-planning/sessions/${sessionId}/execute`,
                        body: { draft_ids: draftIds },
                        onEvent: (_type, data) => handleStreamEvent(data as ExecutionStreamEvent),
                      });
                    } catch (error) {
                      void messageApi.error("执行失败: " + (error instanceof Error ? error.message : String(error)));
                      setIsExecuting(false);
                    }
                  }}
```

- [ ] **Step 7: Replace cancel button with POST**

Find the cancel button inside the execution progress rendering (around line 756). Replace:

```typescript
                          onClick={() => {
                            executionStreamRef.current?.send({ type: "cancel" });
                          }}
```

with:

```typescript
                          onClick={() => {
                            if (sessionId) void cancelExecution(sessionId);
                          }}
```

- [ ] **Step 8: Verify frontend builds**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/frontend && npm run build`

Expected: Build succeeds with no errors.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/components/AITestPlanningPanel.tsx
git commit -m "feat: replace WebSocket with SSE in AITestPlanningPanel"
```

---

### Task 5: Frontend — Update component tests

**Files:**
- Modify: `frontend/src/components/AITestPlanningPanel.test.tsx`

- [ ] **Step 1: Update test imports and mocks**

Replace the WebSocket mock import and mock setup at the top of the test file. Change:

```typescript
import * as wsModule from "../services/executionWebSocket";
```

to:

```typescript
import * as sseModule from "../services/sseClient";
```

Change the mock:

```typescript
vi.mock("../services/executionWebSocket", () => ({
  connectExecutionStream: vi.fn(),
}));
```

to:

```typescript
vi.mock("../services/sseClient", () => ({
  callSSE: vi.fn(),
  cancelExecution: vi.fn(),
}));
```

- [ ] **Step 2: Update test "保存并执行后会重新加载会话详情并展示持久化的执行摘要"**

This test (starting around line 417) mocks `connectExecutionStream`. Replace the mock setup:

```typescript
  let capturedOnEvent: ((event: ExecutionStreamEvent) => void) | null = null;
  vi.mocked(wsModule.connectExecutionStream).mockImplementation((_sid, onEvent, _onErr) => {
    capturedOnEvent = onEvent as (event: ExecutionStreamEvent) => void;
    return {
      send: (_data: Record<string, unknown>) => {
        setTimeout(() => {
          onEvent({ type: "done" });
        }, 0);
      },
      close: vi.fn(),
      isOpen: () => true,
    };
  });
```

with:

```typescript
  vi.mocked(sseModule.callSSE).mockImplementation(async (opts) => {
    opts.onEvent("done", {});
  });
```

Replace the assertion:

```typescript
    expect(wsModule.connectExecutionStream).toHaveBeenCalledWith(5, expect.any(Function), expect.any(Function));
```

with:

```typescript
    expect(sseModule.callSSE).toHaveBeenCalledWith(
      expect.objectContaining({
        url: "/api/v1/ai-planning/sessions/5/execute",
        body: { draft_ids: [11] },
      }),
    );
```

- [ ] **Step 3: Update test "保存并执行改为流式 WebSocket 并在 done 后回读会话详情"**

This test (starting around line 586) uses `capturedOnEvent` to simulate streaming events. Replace the mock setup:

```typescript
  const send = vi.fn();
  const close = vi.fn();
  let capturedOnEvent: ((event: ExecutionStreamEvent) => void) | null = null;

  vi.mocked(wsModule.connectExecutionStream).mockImplementation((_sessionId, onEvent, _onError) => {
    capturedOnEvent = onEvent as (event: ExecutionStreamEvent) => void;
    return { send, close, isOpen: () => true };
  });
```

with:

```typescript
  let capturedOnEvent: ((eventType: string, data: unknown) => void) | null = null;
  vi.mocked(sseModule.callSSE).mockImplementation(async (opts) => {
    capturedOnEvent = opts.onEvent;
  });
```

Replace the assertion:

```typescript
    expect(wsModule.connectExecutionStream).toHaveBeenCalledWith(5, expect.any(Function), expect.any(Function));
```

with:

```typescript
    expect(sseModule.callSSE).toHaveBeenCalled();
```

Replace:

```typescript
  expect(send).toHaveBeenCalledWith({ type: "execute", draft_ids: [11] });
```

with:

```typescript
  expect(sseModule.callSSE).toHaveBeenCalledWith(
    expect.objectContaining({
      url: "/api/v1/ai-planning/sessions/5/execute",
      body: { draft_ids: [11] },
    }),
  );
```

Replace the simulated event dispatch:

```typescript
  act(() => {
    capturedOnEvent?.({ type: "save_progress", saved_count: 1, total: 1, case_name: "登录成功" });
    capturedOnEvent?.({ type: "done" });
  });
```

with:

```typescript
  act(() => {
    capturedOnEvent?.("save_progress", { type: "save_progress", saved_count: 1, total: 1, case_name: "登录成功" });
    capturedOnEvent?.("done", { type: "done" });
  });
```

- [ ] **Step 4: Run frontend tests**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/frontend && npm test -- --run`

Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/AITestPlanningPanel.test.tsx
git commit -m "test: update AITestPlanningPanel tests from WebSocket to SSE mocks"
```

---

### Task 6: Manual E2E verification

**No files to modify — manual testing only.**

- [ ] **Step 1: Start backend**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run backend-dev`

- [ ] **Step 2: Start frontend**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/frontend && npm run dev`

- [ ] **Step 3: Test chat flow**

1. Open http://127.0.0.1:5173
2. Navigate to AI Planning
3. Send a message like "测试登录功能"
4. Verify: status indicator shows, text streams in, tool calls appear
5. Verify: `turn_complete` reloads session data correctly

- [ ] **Step 4: Test draft generation**

1. After requirements are collected, select scenarios
2. Click "生成选中草案"
3. Verify: draft generation progress shows, drafts appear in sidebar

- [ ] **Step 5: Test execution**

1. Select drafts, click "保存并执行"
2. Verify: execution progress streams in real-time
3. Verify: step events show correctly
4. Test cancel button works (if execution is long enough)

- [ ] **Step 6: Verify no WebSocket connections in browser DevTools**

Open DevTools → Network → WS tab. Confirm no WebSocket connections are made during any of the above operations. Check the Fetch/XHR tab to see SSE requests instead.

- [ ] **Step 7: Commit final state (if any fixes were needed)**

```bash
git add -A
git commit -m "fix: address SSE integration issues found during manual testing"
```
