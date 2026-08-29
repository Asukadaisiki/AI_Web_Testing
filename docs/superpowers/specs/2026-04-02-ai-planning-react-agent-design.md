# AI Planning ReAct Agent Design

Date: 2026-04-02

## Problem

The current AI test planning module uses deterministic keyword extraction and fixed template questions. The supplementary information flow is rigid: 7 fixed slots, fixed follow-up questions, keyword-based extraction. This produces a mechanical, inflexible user experience that cannot adapt to different testing contexts.

## Goal

Replace the deterministic logic with an LLM-driven ReAct (Reasoning + Acting) agent that:
- Dynamically decides what to ask based on conversation context
- Can call project tools (list cases, view executions, etc.) to gather context autonomously
- Lets users trigger plan generation at any time
- Remains extensible for future tools/skills

## Architecture

### Core Flow

```
User message -> run_planning_turn()
  -> Build messages: [system_prompt + tool descriptions] + transcript history
  -> ReAct loop (max N rounds, configurable):
     1. Call LLM with full context
     2. Parse action JSON from LLM response
     3. If action == "call_tool" -> execute tool, append result to context, continue loop
     4. If action == "ask_user" -> return question to frontend (end loop)
     5. If action == "generate_plan" -> build plan, return to frontend (end loop)
     6. If user sent [FORCE_GENERATE] -> override to generate_plan with whatever info exists
  -> Return AIPlanningTurnResponse
```

### Tool/Skill System

Each tool is a Python function with a declarative description injected into the LLM system prompt. Tools call existing service-layer functions directly (no HTTP roundtrip).

**Built-in tools (v1):**

| Tool Name | Description | Service Used |
|-----------|-------------|--------------|
| `get_project_info` | Get project details (name, description, created_at) | `projects.get_project()` |
| `list_test_cases` | List test cases in the project with optional search | `cases.list_cases()` |
| `get_case_detail` | View a specific case with DSL steps | `cases.get_case()` |
| `list_recent_executions` | View recent execution results | `executions.list_executions()` |
| `get_case_stats` | Get case statistics for the project | `cases.get_case_stats()` |

**Extensibility:** New tools are added by registering a function in `planning_tools.py` with a `PlanningTool` descriptor. No changes to agent logic needed.

### LLM Output Format

The system prompt instructs the LLM to always return JSON:

```json
{
  "thought": "reasoning about what to do next",
  "action": "ask_user | call_tool | generate_plan",
  "action_input": {
    // for ask_user: { "message": "question text" }
    // for call_tool: { "tool": "tool_name", "params": { ... } }
    // for generate_plan: { "summary": "...", "scenarios": [...] }
  },
  "collected_info": {
    "app_under_test": null,
    "business_goal": "...",
    "entry_url_or_page": null,
    "core_user_flow": null,
    "main_assertions": [],
    "test_data_or_account": null,
    "scope_limits": null
  }
}
```

`collected_info` is merged into the session's `requirements` on every turn for structured storage and frontend progress display.

### Force Generate

Users can click a "直接生成方案" button at any time. This sends a special `[FORCE_GENERATE]` marker in the message. The agent wraps user content as: `用户要求直接生成方案。以下是用户原始输入：{content}`. The LLM generates a plan with whatever info is available, marking assumptions/risks for missing fields.

## Configuration

### New Settings Fields

| Field | Type | Default | ENV Key |
|-------|------|---------|---------|
| `enable_ai_planning` | bool | false | `ENABLE_AI_PLANNING` |
| `ai_planning_model` | string \| null | null | `AI_PLANNING_MODEL` |
| `ai_planning_base_url` | string | "https://api.openai.com/v1" | `AI_PLANNING_BASE_URL` |
| `ai_planning_api_key` | string \| null | null | `AI_PLANNING_API_KEY` |
| `ai_planning_timeout_ms` | int | 30000 | `AI_PLANNING_TIMEOUT_MS` |
| `ai_planning_max_react_rounds` | int | 5 | `AI_PLANNING_MAX_REACT_ROUNDS` |

These follow the same pattern as existing `ai_dsl_*` and `vlm_*` settings: stored in `.env`, managed via settings API, never expose API keys in responses.

### Backend Changes

| File | Change |
|------|--------|
| `backend/app/core/config.py` | Add 6 new settings fields |
| `backend/app/schemas/settings.py` | Add fields to `AISettingsResponse` and `AISettingsUpdateRequest` |
| `backend/app/services/settings.py` | Add new fields to get/update/persist |
| `backend/app/ai/test_planning_agent.py` | **Rewrite** to ReAct agent with LLM calls |
| `backend/app/ai/planning_tools.py` | **New** - tool registry and execution |
| `backend/app/ai/test_planning_prompts.py` | **Rewrite** to LLM system prompt template |
| `backend/app/schemas/ai_planning.py` | Add `AIPlanningToolCall` schema, adjust fields |
| `backend/app/services/ai_planning.py` | Pass `db_session` and `project_id` to agent |

### Frontend Changes

| File | Change |
|------|--------|
| `frontend/src/types/api.ts` | Add planning config fields to `AISettings` / `AISettingsUpdatePayload` |
| `frontend/src/pages/AISettingsPage.tsx` | Add "AI 规划" config section |
| `frontend/src/components/AITestPlanningPanel.tsx` | Replace fixed slots with dynamic progress display + "直接生成方案" button |

### Frontend Progress Display

Replace the current fixed slot list with a dynamic component:
- Show collected info as key-value pairs (only fields with values, dynamically from `requirements`)
- Show a progress indicator (filled count / total slots)
- "缺失槽位" label replaced with natural progress description

### Degradation Strategy

```
LLM call failure:
  -> Network timeout / API error: return friendly message "遇到了问题，请再描述一下你的测试需求"
  -> Unparseable LLM response: fall back to deterministic keyword extraction (reuse existing _fill_requirements_from_text)
  -> 3 consecutive failures: session_status -> "error", prompt user to check model config

Tool execution failure:
  -> Append error to LLM context ("工具 xxx 调用失败: reason"), let LLM decide next step
  -> Don't expose tool errors directly to user
```

### Testing Strategy

- **Unit tests for `test_planning_agent.py`**: Mock LLM responses, verify action parsing, tool dispatch, force generate, degradation
- **Unit tests for `planning_tools.py`**: Verify each tool's parameter validation and return values
- **Integration test**: Full ReAct loop with mock LLM + real DB session

### Audit & Governance

Tool calls are recorded in the planning session messages as structured payloads (`structured_payload_json`) with type `tool_call`. This preserves the existing audit trail pattern.

## Scope

This design covers:
- ReAct agent replacing deterministic planning logic
- Tool/skill registry with 5 built-in tools
- New AI planning model configuration
- Frontend progress display and force-generate button
- Degradation and error handling

Out of scope:
- Adding more tools beyond the initial 5 (extensible later)
- Changing the DSL draft generation flow (downstream, unchanged)
- Changing the session/draft database schema (reuse existing tables)
