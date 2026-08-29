# WebSocket → SSE Refactoring Design

## Problem

Current AI planning agent uses a single WebSocket connection per session to handle all operations (chat, draft generation, test execution, cancellation). The backend uses a thread bridge pattern (sync generator → worker thread → asyncio.Queue → WebSocket) that frequently disconnects during streaming, causing failed operations and poor UX.

## Decision

Replace the single WebSocket with per-operation SSE streams. Each operation (chat, generate drafts, execute, execute-with-judge) gets its own REST endpoint that returns an SSE stream. The stream lives only for the duration of that operation, then closes automatically. Cancellation uses a separate POST endpoint.

## Why SSE per-operation instead of persistent SSE or WebSocket

- **Short-lived connections** — each SSE stream lasts only as long as the operation (seconds to minutes), drastically reducing the chance of mid-stream disconnects
- **Simple retry** — if a stream breaks, frontend retries just that one operation
- **No connection management** — no reconnect logic, no heartbeat, no stale connection detection
- **HTTP semantics** — standard POST + streaming response, easier to debug, proxy, and load-balance
- **Cleans up thread bridge** — the sync→async bridge complexity that caused disconnects is replaced with simpler async generators

## API Design

### New Endpoints

| Operation | Method | Path | Response |
|-----------|--------|------|----------|
| AI Chat | POST | `/api/v1/ai-planning/sessions/{id}/chat` | SSE stream |
| Generate Drafts | POST | `/api/v1/ai-planning/sessions/{id}/drafts` | SSE stream |
| Save & Execute | POST | `/api/v1/ai-planning/sessions/{id}/execute` | SSE stream |
| Explorer-Judge Execute | POST | `/api/v1/ai-planning/sessions/{id}/execute-with-judge` | SSE stream |
| Cancel | POST | `/api/v1/ai-planning/sessions/{id}/cancel` | JSON |

### Removed

- `@router.websocket("/sessions/{session_id}/ws")` — deleted entirely

### SSE Event Format

```
event: {event_type}
data: {json_payload}

```

Each SSE stream ends with either `event: done` or `event: error`.

### Request Bodies

**Chat**: `{"content": "string", "scenario_keys": ["string"]}` (scenario_keys optional, for context)

**Generate Drafts**: `{"scenario_keys": ["string"]}`

**Execute**: `{"draft_ids": [1, 2, 3]}`

**Execute-with-Judge**: `{"draft_ids": [1, 2, 3]}`

**Cancel**: no body

### Event Types per Endpoint

**Chat SSE**: `status`, `text_chunk`, `tool_call_start`, `tool_call_end`, `turn_complete`, `done`, `error`

**Drafts SSE**: `draft_generating`, `turn_complete`, `done`, `error`

**Execute SSE**: `save_progress`, `case_start`, `step_start`, `step_complete`, `done`, `error`, `cancelled`

**Execute-with-Judge SSE**: all execute events + `explorer_start`, `explorer_complete`, `judge_start`, `judge_complete`, `auto_fix_attempt`, `auto_fix_result`, `verdict_report`

All event payloads remain identical to current WebSocket payloads — no frontend type changes needed.

## Backend Architecture

### Remove Thread Bridge

Delete the current pattern in `ai_planning_streaming.py`:
- `_run_sync_generator()` / `_bridge_sync_generator()` — worker thread + asyncio.Queue bridge
- `_run_sync_save_and_execute()` / `_run_sync_explorer_judge()` — dedicated thread functions

Replace with direct async generators using `asyncio.to_thread()` to run sync code without the Queue bridge:

```python
async def stream_planning_chat(session_id: int, content: str):
    loop = asyncio.get_event_loop()
    gen = stream_planning_message(session_id, content)
    while True:
        event = await loop.run_in_executor(None, next, gen)
        yield event
```

### New SSE Helper

```python
def sse_event(event_type: str, data: dict) -> str:
    return f"event: {event_type}\ndata: {json.dumps(data, ensure_ascii=False)}\n\n"

def sse_done() -> str:
    return "event: done\ndata: {}\n\n"
```

### SSE Endpoint Pattern

```python
@router.post("/sessions/{session_id}/chat")
async def chat_sse(session_id: int, req: ChatRequest, db: Session = Depends(get_db)):
    async def event_generator():
        try:
            async for event in stream_planning_chat(session_id, req.content):
                yield sse_event(event["type"], event)
        except Exception as e:
            yield sse_event("error", {"message": str(e)})
        yield sse_done()
    return StreamingResponse(event_generator(), media_type="text/event-stream")
```

### Cancellation

`CancellationManager` unchanged — `POST /cancel` sets the threading.Event, running generators check it and yield a `cancelled` event before stopping.

## Frontend Architecture

### Delete

- `frontend/src/services/executionWebSocket.ts` — removed entirely

### New: `frontend/src/services/sseClient.ts`

```typescript
interface SSEClientOptions {
  url: string;
  body: object;
  onEvent: (type: string, data: any) => void;
  onError?: (error: Error) => void;
}

async function callSSE(opts: SSEClientOptions): Promise<void> {
  const response = await fetch(opts.url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(opts.body),
  });
  // Read stream via response.body.getReader()
  // Parse SSE events (event: / data: lines)
  // Call onEvent for each parsed event
}
```

### `AITestPlanningPanel.tsx` Changes

1. Remove `wsClientRef` and all WebSocket connection lifecycle code
2. Remove WebSocket fallback-to-REST logic
3. Each operation directly calls `callSSE`:
   - `handleSendMessage()` → `callSSE({url: '/sessions/{id}/chat', body: {content}, onEvent})`
   - `handleGenerateDrafts()` → `callSSE({url: '/sessions/{id}/drafts', body: {scenario_keys}, onEvent})`
   - Execute handlers → `callSSE({url: '/sessions/{id}/execute', body: {draft_ids}, onEvent})`
4. Cancel button → plain `fetch('/sessions/{id}/cancel', {method: 'POST'})`
5. Event processing in `onEvent` callback unchanged — same state updates

### `types/api.ts` — No Changes

All StreamEvent type definitions remain identical. SSE events carry the same payload structure.

## Files to Change

| File | Action |
|------|--------|
| `backend/app/api/routes/ai_planning.py` | Delete WebSocket endpoint, add 5 POST endpoints |
| `backend/app/services/ai_planning_streaming.py` | Rewrite: remove thread bridge, use async generators |
| `frontend/src/services/executionWebSocket.ts` | Delete |
| `frontend/src/services/sseClient.ts` | New: SSE client utility |
| `frontend/src/components/AITestPlanningPanel.tsx` | Replace WebSocket with SSE calls |

## Out of Scope

- No changes to event types or payloads
- No changes to `ai_planning.py` service business logic
- No changes to `playwright_runner.py` execution engine
- No changes to `CancellationManager`
- No database schema changes
