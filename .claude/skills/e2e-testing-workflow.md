---
name: e2e-testing-workflow
description: Use when performing E2E manual testing of this AI Web Testing platform — covers the full chain of starting the three services, AI planning conversation, grounding & DSL generation with user approval, execution via the queue, analyzing reports, and the feedback loop (fix_and_retry / corrections). Triggers when user says "test the platform", "run E2E test", "manual test", "验证平台", "端到端测试", "手动测试".
---

# E2E Testing Workflow for AI Web Testing Platform

## Overview

This skill guides you through the complete E2E testing chain of this platform:

**AI Conversation → TaskPlan Grounding → DSL Generation → User Approval → Queue Execution → Report → Feedback Loop (fix_and_retry / corrections)**

Architecture context (must not be violated during testing):

- Go control plane (`backend-go`, port 8081) owns planning, DSL validation, execution queue, and reports.
- Python browser worker (`browser-worker`, port 8000) is a stateless Playwright/A11y capability worker.
- Frontend (`frontend`, port 5173) calls Go `/api/v2` only.
- No login/auth; server fixes the actor. DSL targets use A11y semantic format `role="name"`.

## Prerequisites

```bash
# 1) PostgreSQL must be reachable (local dev uses .pgdata/ or your local install)

# 2) Apply Go schema migrations
cd backend-go && go run ./cmd/migrate

# 3) Browser worker env
cd browser-worker && cp .env.example .env 2>/dev/null; uv sync
```

Required env for the Go service (LLM provider):
- `VLM_BASE_URL` / provider base URL, model, and API key used by the AgentCore
- `AGENTSERVICE_MAX_TOTAL_TOKENS` — run-level cost fuse (default 3M; raise for long agentic runs)

## Phase 1 — Start the System (three processes)

### 1.1 Browser worker

```bash
cd browser-worker && uv run browser-worker-dev
```

Verify: `curl http://127.0.0.1:8000/api/v1/internal/browser-capabilities` exists (internal API, 404 on unknown routes is normal).

### 1.2 Go execution worker

```bash
cd backend-go && go run ./cmd/execution-worker --concurrency 2
```

### 1.3 Go AgentService + Frontend

```bash
cd backend-go && go run ./cmd/agentservice   # :8081
cd frontend && npm run dev                   # :5173, proxies /api/v2 → :8081
```

Verify: open `http://127.0.0.1:5173` — no login, it goes straight to the workbench.

## Phase 2 — AI Planning Conversation

### 2.1 Enter Planning page

1. Open `http://127.0.0.1:5173`, navigate to **AI 规划 / Planning** (left sidebar)
2. Select a project; a new planning session starts automatically
3. SSE streams messages, ToolCalls, and artifacts live; refreshing replays events by sequence number

### 2.2 Act as real user — conversation pattern

When testing the AI conversation flow, act as a **real QA engineer**:

**First message — describe the testing goal:**
> "我要测试 [目标系统名] 的 [功能模块]。系统地址是 [base_url]。主要业务流程是 [简述核心流程]。"

**Then respond naturally to the agent's questions.** Do NOT dump all information at once — simulate how a real user would interact.

The agent works through: clarification (`ask_user_question`) → page exploration (`explore_page` / `explore_flow`) → element validation → **TaskPlan grounding** (every planned step must bind to observed candidates) → DSL generation → **user approval** → execution.

### 2.3 Verify quality checkpoints

| Check Point | What to Verify |
|---|---|
| Exploration | Did the agent explore via A11y observation (not raw DOM dump)? |
| Grounding | Do all planned steps reach `grounded` status with candidate refs? Plan revisions must not lose grounded bindings. |
| DSL approval | Was the DSL draft shown for user approval before execution? The model must never execute unapproved DSL. |
| Target format | All DSL targets use `role="name"` semantic format; no XPath/CSS. |
| Variables | `${var}` only in `value` fields; the generation notes explain extraction/normalization. |
| Governance | Warnings, normalization notes, and generation metadata are visible to the user. |

If grounding or DSL generation fails, the agent should surface the specific gate rejection reason (field names + both sides' values), not a generic error.

## Phase 3 — Execute, Observe, Feedback

### 3.1 Approve & execute

1. Approve the generated DSL generation in the planning page
2. The agent calls `execute_dsl`; the run goes through the Go execution queue (`ExecutionBatch -> ExecutionJob -> TestCaseRun`)
3. Watch progress live via SSE; the execution worker claims the job and runs Playwright via the stateless Python RPC

### 3.2 Analyze the report

1. Open the run report: step-level results with evidence (screenshot, console, network)
2. Check aggregate views: batch/project level statistics, persisted FailureSignals

### 3.3 Feedback loop

- **If the run failed on a locator**: use the report/debug page to inspect target vs candidates vs final match and failure reason. Submit a **correction**; it becomes a Tier 0 priority match on future runs. Rerun from the UI.
- **If the run failed on logic**: use `fix_and_retry` in the planning conversation — the agent re-explores or regenerates DSL from persisted failure facts, then **requires user approval again** before rerun.
- **If the platform itself misbehaved**: record the defect in `docs/bug-log.md` (newest first) and link the execution record in `docs/execution-log.md`.

## Non-Goals During Testing

- Do not hand-edit DSL targets to XPath/CSS to "make it pass" — that masks locator defects.
- Do not bypass DSL validation or approval to force a run.
- Do not hardcode task-specific flows or expected values into runtime code; use `research/` fixtures and acceptance specs.
