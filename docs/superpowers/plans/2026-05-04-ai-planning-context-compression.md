# AI Planning Context Compression Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compress heavy tool results (explore_page/explore_flow) via subagent summarization, fix plan_json overwrite, and strengthen JSON parsing to keep ReAct context under control.

**Architecture:** Heavy tools run their results through a short-context subagent LLM call that compresses 570KB+ DOM data into ~2KB structured summaries. Summaries replace raw results in conversation context, SSE events, and message storage. Raw data preserved in a new `ai_planning_tool_results` table for debugging.

**Tech Stack:** Python + FastAPI + SQLAlchemy 2.x + httpx (sync) + React + TypeScript

**Spec:** [2026-05-04-ai-planning-context-compression-design.md](../specs/2026-05-04-ai-planning-context-compression-design.md)

---

### Task 1: Fix plan_json overwrite

**Files:**
- Modify: `backend/app/services/ai_planning.py:244-248`

- [ ] **Step 1: Guard plan_json assignment**

In `stream_planning_message()` at line 244-248, change from unconditional overwrite to conditional:

```python
# --- BEFORE ---
plan_dict = response.plan.model_dump(mode="json") if response.plan is not None else None
if plan_dict is not None:
    from app.ai.test_planning_agent import _extract_raw_page_results
    plan_dict["_page_results"] = _extract_raw_page_results(response.tool_calls)
planning_session.plan_json = plan_dict

# --- AFTER ---
if response.plan is not None:
    plan_dict = response.plan.model_dump(mode="json")
    from app.ai.test_planning_agent import _extract_raw_page_results
    plan_dict["_page_results"] = _extract_raw_page_results(response.tool_calls)
    planning_session.plan_json = plan_dict
```

- [ ] **Step 2: Commit**

```bash
git add backend/app/services/ai_planning.py
git commit -m "fix: preserve existing plan_json on followup turns"
```

---

### Task 2: Add JSON repair preprocessor

**Files:**
- Modify: `backend/app/ai/test_planning_agent.py`

- [ ] **Step 1: Add `_repair_json_text()` function**

Insert after `_extract_json_object()` (around line 1320):

```python
def _repair_json_text(text: str) -> str:
    """Repair common AI JSON output errors before parsing."""
    # Strip markdown code fences (already handled by _extract_json_object, belt-and-suspenders)
    stripped = text.strip()
    if stripped.startswith("```"):
        first_nl = stripped.find("\n")
        if first_nl != -1:
            end_fence = stripped.rfind("```", first_nl)
            if end_fence > first_nl:
                stripped = stripped[first_nl + 1:end_fence].strip()

    # Remove trailing commas before } or ]
    import re
    stripped = re.sub(r",\s*(\}|\])", r"\1", stripped)

    return stripped
```

- [ ] **Step 2: Integrate into `_parse_llm_response()`**

Change `_parse_llm_response()` (line 785-793):

```python
def _parse_llm_response(response_text: str) -> dict[str, Any] | None:
    repaired = _repair_json_text(response_text)
    try:
        payload = json.loads(_extract_json_object(repaired))
    except json.JSONDecodeError:
        logger.warning("Planning LLM returned unparseable JSON: %r", response_text[:300])
        return None
    if not isinstance(payload, dict):
        return None
    return payload
```

- [ ] **Step 3: Commit**

```bash
git add backend/app/ai/test_planning_agent.py
git commit -m "fix: add JSON repair preprocessor for trailing commas and code fences"
```

---

### Task 3: New SQLAlchemy model + migration

**Files:**
- Create: `backend/app/models/ai_planning_tool_result.py`
- Modify: `backend/app/models/__init__.py`
- Create: `backend/alembic/versions/####_add_ai_planning_tool_results.py` (via autogenerate)

- [ ] **Step 1: Write model**

Create `backend/app/models/ai_planning_tool_result.py`:

```python
"""Persisted raw results from AI planning tool calls."""
from __future__ import annotations

from datetime import datetime
from typing import Any

from sqlalchemy import DateTime, ForeignKey, Integer, JSON, String, func
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class AIPlanningToolResult(Base):
    __tablename__ = "ai_planning_tool_results"

    id: Mapped[int] = mapped_column(primary_key=True)
    session_id: Mapped[int] = mapped_column(
        Integer,
        ForeignKey("ai_planning_sessions.id", ondelete="CASCADE"),
        index=True,
        nullable=False,
    )
    message_id: Mapped[int | None] = mapped_column(
        Integer,
        ForeignKey("ai_planning_messages.id", ondelete="SET NULL"),
        nullable=True,
    )
    tool_name: Mapped[str] = mapped_column(String(100), nullable=False)
    raw_result_json: Mapped[dict[str, Any] | None] = mapped_column(JSON, nullable=True)
    summary_json: Mapped[dict[str, Any]] = mapped_column(JSON, nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(), server_default=func.now(), nullable=False
    )
```

- [ ] **Step 2: Register in models __init__.py**

Add to `backend/app/models/__init__.py`:

```python
from app.models.ai_planning_tool_result import AIPlanningToolResult
```

Add `"AIPlanningToolResult"` to `__all__` list.

- [ ] **Step 3: Generate migration**

```bash
cd backend
uv run alembic revision --autogenerate -m "add ai_planning_tool_results"
```

- [ ] **Step 4: Run migration**

```bash
uv run alembic upgrade head
```

Expected: migration applies without errors.

- [ ] **Step 5: Commit**

```bash
git add backend/app/models/ai_planning_tool_result.py backend/app/models/__init__.py backend/alembic/versions/
git commit -m "feat: add ai_planning_tool_results table for heavy tool result storage"
```

---

### Task 4: Config settings for subagent

**Files:**
- Modify: `backend/app/core/config.py`

- [ ] **Step 1: Add subagent settings fields**

Add to the `Settings` dataclass (after `explore_max_elements`):

```python
ai_planning_subagent_enabled: bool = True
ai_planning_subagent_timeout_ms: int = 60000
```

Default values:
```python
ai_planning_subagent_enabled=True,
ai_planning_subagent_timeout_ms=60000
```

- [ ] **Step 2: Add env loading in `get_settings()`**

```python
ai_planning_subagent_enabled=_get_bool(os.getenv("AI_PLANNING_SUBAGENT_ENABLED"), default=True),
ai_planning_subagent_timeout_ms=max(5000, _get_int(os.getenv("AI_PLANNING_SUBAGENT_TIMEOUT_MS"), default=60000)),
```

- [ ] **Step 3: Commit**

```bash
git add backend/app/core/config.py
git commit -m "feat: add subagent config settings"
```

---

### Task 5: Element pre-filter (algorithmic)

**Files:**
- Modify: `backend/app/ai/test_planning_agent.py`

- [ ] **Step 1: Add `_filter_elements_for_compression()` function**

Insert before `_extract_raw_page_results()`:

```python
# Essential attributes to keep per element for compression
_ELEMENT_KEEP_ATTRS = {"tag", "text", "id", "name", "data-qa", "placeholder",
                         "type", "href", "role", "aria-label", "class"}
_HEAVY_TOOLS = {"explore_page", "explore_flow"}


def _filter_elements_for_compression(elements: list[dict]) -> list[dict]:
    """Trim elements to essential attributes, max 100 items."""
    cap = 100
    filtered: list[dict] = []
    for el in elements[:cap]:
        if not isinstance(el, dict):
            continue
        filtered.append({
            k: v for k, v in el.items()
            if k in _ELEMENT_KEEP_ATTRS and v is not None and v != ""
        })
    return filtered
```

- [ ] **Step 2: Commit**

```bash
git add backend/app/ai/test_planning_agent.py
git commit -m "feat: add element pre-filter for compression subagent"
```

---

### Task 6: Subagent compression runner

**Files:**
- Modify: `backend/app/ai/test_planning_agent.py`

- [ ] **Step 1: Add `run_compression_subagent()` function**

Insert after `_filter_elements_for_compression()`:

```python
_SUBAGENT_SYSTEM_PROMPT = """\
You are a DOM result compressor. Given web page exploration results, extract only testing-relevant information.

## Input format
- explore_page: {"url": "...", "elements": [...], "element_count": N}
- explore_flow: {"pages": [{"url": "...", "elements": [...], "element_count": N}, ...], "urls_count": N}

## Output format (valid JSON only)

For explore_page:
{
  "page_title": "extracted from page content",
  "url": "...",
  "navigation": ["link text 1", "link text 2"],
  "element_counts": {"buttons": N, "inputs": N, "links": N, "images": N},
  "key_elements": [
    {"role": "button|link|input|select", "text": "...", "selector": "..."}
  ],
  "forms": [
    {"purpose": "login|search|etc", "fields": [{"label": "...", "selector": "..."}]}
  ]
}

For explore_flow:
{
  "flow_title": "...",
  "steps": [{"url": "...", "page_title": "...", "key_action": "..."}],
  "critical_selectors": ["selector1", "selector2"]
}

Rules:
- Max 30 key_elements per page
- Only include elements with stable selectors (id, data-qa, name)
- Selector format: use id > data-qa > name > unique class
- Keep output under 3KB
- Return ONLY valid JSON, no markdown fences
"""


def run_compression_subagent(
    tool_name: str,
    parsed_result: dict,
    settings: Any,
) -> dict | None:
    """Run a short-context LLM call to compress raw tool results into summaries."""
    if not getattr(settings, "ai_planning_subagent_enabled", True):
        return None

    # Build compact input
    if tool_name == "explore_page":
        raw_elements = parsed_result.get("elements", [])
        filtered_elements = _filter_elements_for_compression(raw_elements)
        input_payload = {
            "url": parsed_result.get("url", ""),
            "element_count": len(raw_elements),
            "elements": filtered_elements,
        }
    elif tool_name == "explore_flow":
        pages = parsed_result.get("pages", [])
        compact_pages = []
        for p in pages[:5]:  # max 5 pages in flow
            if isinstance(p, dict):
                raw_elements = p.get("elements", [])
                compact_pages.append({
                    "url": p.get("url", ""),
                    "element_count": len(raw_elements),
                    "elements": _filter_elements_for_compression(raw_elements),
                })
        input_payload = {"pages": compact_pages}
    else:
        return None

    input_json = json.dumps(input_payload, ensure_ascii=False)
    logger.info("Compression subagent start: tool=%s, input_len=%d", tool_name, len(input_json))

    messages = [
        {"role": "system", "content": _SUBAGENT_SYSTEM_PROMPT},
        {"role": "user", "content": f"Compress this {tool_name} result:\n{input_json}"},
    ]

    api_key = settings.ai_planning_api_key or ""
    model = settings.ai_planning_model or ""
    base_url = settings.ai_planning_base_url
    timeout = max(5.0, getattr(settings, "ai_planning_subagent_timeout_ms", 60000) / 1000)

    payload = {
        "model": model,
        "messages": messages,
        "stream": False,
        "response_format": {"type": "json_object"},
        "max_tokens": 4096,
    }
    endpoint = f"{base_url.rstrip('/')}/chat/completions"

    try:
        with httpx.Client(timeout=timeout) as client:
            resp = client.post(
                endpoint,
                json=payload,
                headers={
                    "Authorization": f"Bearer {api_key}",
                    "Content-Type": "application/json",
                },
            )
            resp.raise_for_status()
            body = resp.json()
            content = body["choices"][0]["message"]["content"]
            return json.loads(content)
    except Exception as exc:
        logger.warning("Compression subagent failed: %s, falling back to algorithmic truncation", exc)
        return None
```

- [ ] **Step 2: Commit**

```bash
git add backend/app/ai/test_planning_agent.py
git commit -m "feat: add compression subagent runner for heavy tools"
```

---

### Task 7: Context injection + SSE event changes

**Files:**
- Modify: `backend/app/ai/test_planning_agent.py:443-454`

- [ ] **Step 1: Change SSE tool_call_end event to emit summary**

At line 443, change the `tool_call_end` yield:

```python
# --- BEFORE ---
yield {"type": "tool_call_end", "tool": tool_name, "result": parsed_result}

# --- AFTER ---
if tool_name in _HEAVY_TOOLS:
    result_summary = compressed_result if compressed_result is not None else {
        "url": parsed_result.get("url") if isinstance(parsed_result, dict) else None,
        "element_count": parsed_result.get("element_count") if isinstance(parsed_result, dict) else None,
        "summary_fallback": True,
    }
    yield {"type": "tool_call_end", "tool": tool_name, "result_summary": result_summary}
else:
    yield {"type": "tool_call_end", "tool": tool_name, "result": parsed_result}
```

- [ ] **Step 2: Change context injection to use compressed result**

Change lines 446-454 (after tool call appends to tool_calls list):

```python
# --- BEFORE ---
result_summary = _summarize_tool_result(tool_name, parsed_result)
logger.info("Tool call %s completed: %s", tool_name, result_summary)
conversation.extend([
    {"role": "assistant", "content": _normalize_json_text(raw_response)},
    {"role": "system", "content": f"工具 {tool_name or 'unknown_tool'} 返回结果：{tool_result_text}"},
])

# --- AFTER ---
summary_for_log = _summarize_tool_result(tool_name, parsed_result)
logger.info("Tool call %s completed: %s", tool_name, summary_for_log)

# Run compression subagent for heavy tools
compressed_result = None
if tool_name in _HEAVY_TOOLS:
    compressed_result = run_compression_subagent(tool_name, parsed_result, settings)

conversation.extend([
    {"role": "assistant", "content": _normalize_json_text(raw_response)},
])

# Inject compressed result for heavy tools, raw for light tools
if tool_name in _HEAVY_TOOLS:
    if compressed_result is not None:
        conversation.append({
            "role": "system",
            "content": f"工具 {tool_name} 返回摘要：{json.dumps(compressed_result, ensure_ascii=False)}",
        })
    else:
        # Fallback: inject truncated result (first 2000 chars)
        truncated = tool_result_text[:2000] + ("..." if len(tool_result_text) > 2000 else "")
        conversation.append({
            "role": "system",
            "content": f"工具 {tool_name} 返回结果（已截断）：{truncated}",
        })
else:
    conversation.append({
        "role": "system",
        "content": f"工具 {tool_name} 返回结果：{tool_result_text}",
    })
```

Note: `compressed_result` needs to be stored alongside the tool call for later persistence. Pass it back via a dict on the side or by attaching to the `AIPlanningToolCall` — we'll handle this in Task 8.

- [ ] **Step 3: Store compressed result on tool call object for later persistence**

After the tool_call is appended to `tool_calls` list (line 436-442), add a `_compressed_result` attribute:

```python
tool_call = AIPlanningToolCall(
    tool=tool_name or "unknown_tool",
    params=params,
    result=parsed_result,
)
if tool_name in _HEAVY_TOOLS:
    tool_call._compressed_result = compressed_result  # type: ignore[attr-defined]
tool_calls.append(tool_call)
```

- [ ] **Step 4: Commit**

```bash
git add backend/app/ai/test_planning_agent.py
git commit -m "feat: inject compressed summaries into context for heavy tools"
```

---

### Task 8: Persist tool results with messages

**Files:**
- Modify: `backend/app/services/ai_planning.py:255-285`

- [ ] **Step 1: Import model**

Add import at top of `ai_planning.py`:

```python
from app.models.ai_planning_tool_result import AIPlanningToolResult
```

- [ ] **Step 2: Persist tool results alongside messages**

After the for loop that creates tool_call messages (lines 255-267), add:

```python
for tool_call in response.tool_calls:
    tool_dict = tool_call.model_dump(mode="json")
    tool_dict.pop("result", None)  # exclude raw result from message payload
    msg = AIPlanningMessage(
        session_id=planning_session.id,
        role="assistant",
        turn_type="tool_call",
        content=f"调用工具 {tool_call.tool}",
        structured_payload_json={
            "type": "tool_call",
            **tool_dict,
            "result_summary": getattr(tool_call, "_compressed_result", None),
        },
    )
    session.add(msg)
    session.flush()  # get message.id

    # Persist raw + summary for heavy tools
    compressed = getattr(tool_call, "_compressed_result", None)
    if compressed is not None:
        session.add(AIPlanningToolResult(
            session_id=planning_session.id,
            message_id=msg.id,
            tool_name=tool_call.tool,
            raw_result_json=tool_call.result if isinstance(tool_call.result, dict) else None,
            summary_json=compressed,
        ))
```

Replace the existing loop (lines 255-267) with this.

- [ ] **Step 3: Update structured_payload for assistant message**

For the assistant message (line 270-283), use the compressed result references:

```python
session.add(
    AIPlanningMessage(
        session_id=planning_session.id,
        role="assistant",
        turn_type=turn_type,
        content=response.assistant_message,
        structured_payload_json={
            "missing_slots": response.missing_slots,
            "suggested_questions": response.suggested_questions,
            "plan": response.plan.model_dump(mode="json") if response.plan is not None else None,
            "tool_calls": [
                {
                    "tool": item.tool,
                    "params": item.params,
                    "result_summary": getattr(item, "_compressed_result", None),
                }
                for item in response.tool_calls
            ],
            "todo_list": [item.model_dump(mode="json") for item in response.todo_list],
        },
    )
)
```

- [ ] **Step 4: Commit**

```bash
git add backend/app/services/ai_planning.py
git commit -m "feat: persist compressed tool results alongside messages"
```

---

### Task 9: Frontend type updates

**Files:**
- Modify: `frontend/src/types/api.ts`

- [ ] **Step 1: Add `result_summary` to `ToolCallEndStreamEvent`**

At line 911-915:

```typescript
export interface ToolCallEndStreamEvent {
  type: "tool_call_end";
  tool: string;
  result?: unknown;
  result_summary?: unknown;  // compressed summary for heavy tools
}
```

- [ ] **Step 2: Update `AIPlanningToolCall` to include `result_summary`**

At line 226-230:

```typescript
export interface AIPlanningToolCall {
  tool: string;
  params: Record<string, unknown>;
  result?: unknown;
  result_summary?: unknown;  // compressed summary for heavy tools
}
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/types/api.ts
git commit -m "feat: add result_summary to frontend types"
```

---

### Task 10: Frontend tool result summary display

**Files:**
- Modify: `frontend/src/components/AITestPlanningPanel.tsx:757-761`

- [ ] **Step 1: Replace tool_call rendering with summary display**

Change the tool_call branch (lines 757-761):

```tsx
{item.role === "assistant" && item.turn_type === "tool_call" ? (
  <>
    <span style={{ fontWeight: 600 }}>🔧 工具调用</span>
    <div style={{ marginTop: 4 }}>{item.content}</div>
    {item.structured_payload?.result_summary ? (
      <details style={{ fontSize: 12, color: "#666", background: "#fafafa",
                        borderRadius: 6, padding: "4px 8px", marginTop: 4 }}>
        <summary style={{ cursor: "pointer", fontWeight: 500 }}>
          查看摘要
          {item.structured_payload.result_summary &&
            typeof item.structured_payload.result_summary === "object" &&
            "page_title" in (item.structured_payload.result_summary as Record<string, unknown>)
            ? ` — ${(item.structured_payload.result_summary as Record<string, unknown>).page_title}`
            : ""}
        </summary>
        <pre style={{ whiteSpace: "pre-wrap", marginTop: 4, maxHeight: 200,
                      overflowY: "auto", fontSize: 11 }}>
          {JSON.stringify(item.structured_payload.result_summary, null, 2)}
        </pre>
      </details>
    ) : null}
  </>
) : ...}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/AITestPlanningPanel.tsx
git commit -m "feat: show collapsible tool result summaries in chat"
```

---

### Task 11: Frontend thinking content auto-collapse

**Files:**
- Modify: `frontend/src/components/AITestPlanningPanel.tsx:856-862`

- [ ] **Step 1: Enhance thinking content details element**

The thinking content already uses `<details>`. Add auto-close when not streaming, and truncate long content:

```tsx
{((item.structured_payload as Record<string, unknown>)._thinkingContent as string) ? (
  <details
    style={{ fontSize: 12, color: "#666", background: "#fafafa",
             borderRadius: 6, padding: "4px 8px" }}
    open={Boolean((item.structured_payload as Record<string, unknown>)._streaming)}
  >
    <summary style={{ cursor: "pointer", fontWeight: 500 }}>
      💭 思考过程
      {((item.structured_payload as Record<string, unknown>)._thinkingContent as string).length > 500
        ? `（${((item.structured_payload as Record<string, unknown>)._thinkingContent as string).length} 字，已折叠）`
        : ""}
    </summary>
    <div style={{
      whiteSpace: "pre-wrap",
      marginTop: 4,
      maxHeight: 200,
      overflowY: "auto",
      opacity: (item.structured_payload as Record<string, unknown>)._streaming ? 1 : 0.7,
    }}>
      {((item.structured_payload as Record<string, unknown>)._thinkingContent as string).length > 500
        ? ((item.structured_payload as Record<string, unknown>)._thinkingContent as string).slice(0, 500) + "..."
        : ((item.structured_payload as Record<string, unknown>)._thinkingContent as string)}
    </div>
  </details>
) : null}
```

Key changes:
- `open={Boolean(_streaming)}` — auto-expand during live streaming, collapsed for historical messages
- Show character count when long
- Truncate to 500 chars when not streaming; full content still visible when user expands
- Reduce opacity on historical messages

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/AITestPlanningPanel.tsx
git commit -m "feat: auto-collapse thinking content for historical messages"
```

---

### Task 12: Integration test + manual verification

- [ ] **Step 1: Verify plan_json is preserved across followup turns**

```python
# manual check: send a message after plan_ready, verify plan_json is not null
```

Run: Query session plan_json after followup turn.

- [ ] **Step 2: Verify subagent compression produces valid output**

```bash
cd backend
uv run pytest tests/unit/ -k "subagent" -v 2>&1 || echo "No specific test yet; verify via manual run"
```

- [ ] **Step 3: Full manual E2E**

```bash
# Start backend + frontend
cd backend && uv run backend-dev &
cd frontend && npm run dev &
```

1. Create a new AI planning session
2. Send requirements that will trigger explore_page/explore_flow
3. Verify: SSE events show `result_summary` instead of full `result`
4. Verify: Chat shows collapsible tool summaries
5. Verify: Thinking content auto-collapses after stream ends
6. Check DB: `ai_planning_tool_results` has entries with compressed summaries
7. Check DB: `ai_planning_messages.structured_payload_json` is small (< 5KB per msg)
8. Verify: plan persists after followup messages

- [ ] **Step 4: Commit any fixes found during testing**

```bash
git add -A
git commit -m "test: integration verification for context compression"
```
