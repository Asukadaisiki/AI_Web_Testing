# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Rules

Read and follow all instructions in `AGENTS.md` in this repository. Key rules inlined here:

- **Language**: Respond in Chinese unless user requests otherwise. Final responses include Summary, Changes, How to run, Tests, Notes sections.
- **Git**: Single-owner repo, direct push preferred over PRs. Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`). One focused commit per task.
- **Task logging**: After every meaningful task, add a record **at the top** of `docs/execution-log.md`. Log defects to `docs/bug-log.md` (also newest first). Ask user about GitHub sync after completing requirements.
- **Boundaries**: Frontend must not contain test execution logic. Backend runner is the only source of truth for results. AI generation cannot bypass DSL validation. No task-specific hardcoding in reusable runtime code.
- **Architecture truth**: If docs conflict with code, trust `docs/architecture-guide.md`, current schemas, and the code itself.

## Current Architecture (2026-09)

Go control plane + stateless Python browser worker + React frontend:

- **Go (`backend-go/`)** owns: AgentRun/TaskPlan/research state machines, DSL validation, cases, execution queue (PostgreSQL `FOR UPDATE SKIP LOCKED`), reports, SSE, cost fuses, prompt-cache identity, migrations.
- **Python (`browser-worker/`)** is a **stateless** browser capability worker: Playwright runner, A11y observation, locator preflight, evidence collection. It does **not** touch the business database; SQLAlchemy/Alembic were removed.
- **Frontend (`frontend/`)** talks only to Go `/api/v2` (plus `/artifacts` from the Python worker). No login/auth anywhere; the server uses a fixed actor and keeps ownership fields.
- **`contracts/`** holds versioned JSON schemas (browser observation, locator spec, target binding, grounding queries, agent pipeline trace). Contract changes must stay backward compatible.

```
backend-go/
  cmd/
    agentservice/        Hertz HTTP/SSE service (:8081)
    execution-worker/    Queue worker consuming execution_jobs
    migrate/             PostgreSQL schema migrations
    pipeline-audit/      AgentRun pipeline diagnostics
    research-export/     Research trajectory export
  internal/
    agent/               Pure agent loop + message contracts
    agentservice/        AgentRun, events, checkpoints, repositories
    harness/             Prompt, tools, run orchestration, phase boundaries
    taskplan/            TaskPlan state machine + DSL compiler
    research/            Agentic research: events, metrics, oracle, exporter
    tools/               Tool contracts and registry (ask_user, browser, dsl, execution, task_plan)
    execution/ cases/ corrections/ planning/ projects/ platform/ observation/ dsl/ dbschema/
    transport/http/      Thin HTTP handlers
    integration/ testpg/ Test helpers

browser-worker/
  src/browser_worker/
    server/              FastAPI entry, routes (:8000)
    capabilities/        Browser capability + stateless execution RPC
    exploration/         Page exploration, A11y collection, locator preflight
    runners/             Playwright DSL executor
    locators/            Locators + correction protocol
    reporting/           Execution reports, failure signals, acceptance
    contracts/           Pydantic request/response contracts
    runtime/             Config, logging, middleware
  scripts/
    research_e2e.py      Orchestrate, verify, export research experiments
    run_agentic_e2e.py   Live agentic E2E driver (progress + stalled detection)
    run_research_smoke.py

frontend/
  src/
    pages/               PlanningPage, CasesPage, CaseEditPage, ReportPage,
                         ExecutionDetailPage, RegressionPage, LocatorDebugPage, SessionListPage
    features/ components/ layouts/ services/ shared/ hooks/ types/
  e2e/                   Playwright smoke (desktop + mobile)

contracts/               Versioned JSON schemas shared across services
research/                Acceptance specs, fixtures, goals, experiment results
testdata/                Contract test fixtures
docs/                    Planning, specs, execution-log, bug-log
deploy/ + compose.prod.yml  Production stack (PostgreSQL, migrations, Python API/Worker, Go AgentCore, Nginx)
.claude/skills/          Claude Code skills
```

## Commands

### Go control plane (from `backend-go/`)

```bash
go run ./cmd/migrate                          # Apply PostgreSQL schema migrations
go run ./cmd/agentservice                     # Start Hertz API (:8081)
go run ./cmd/execution-worker --concurrency 2 # Start execution queue worker
go test ./...                                 # Unit tests
go vet ./... && go build ./...                # Vet + build gate
```

Requires a reachable PostgreSQL (local dev uses `.pgdata/` via compose or local install).

### Browser worker (from `browser-worker/`)

```bash
cp .env.example .env                          # First-time env config
uv sync                                       # Install dependencies
uv run browser-worker-dev                     # Start FastAPI (:8000)
uv run python -m compileall -q src tests      # Compile gate (matches CI)
uv run python -m unittest discover -s tests -p "test_*.py"   # Full test suite
```

Playwright browser regression lives under `tests/` (e.g. `test_explore_flow_chromium.py`); it needs Playwright + chromium installed.

### Frontend (from `frontend/`)

```bash
npm install
npm run dev                 # Vite dev server (:5173), proxies /api/v2 → :8081, /artifacts → :8000
npm test                    # Vitest run
npm run build               # tsc --noEmit && vite build
npm run test:smoke:install  # Install Playwright chromium (local browsers path)
npm run test:smoke          # Playwright desktop + mobile smoke
```

### Research / agentic E2E

```bash
./scripts/research-e2e --help               # Wrapper for browser-worker/scripts/research_e2e.py
cd browser-worker && uv run python scripts/run_agentic_e2e.py --help   # Live E2E driver
```

Live E2E needs PostgreSQL + the three services running and an LLM API key configured for the Go service. The driver prints progress every 30s and aborts on stalled event sequences instead of waiting out the wall clock.

## Environment Setup

- Python 3.12+, dependencies via `uv` (`browser-worker/pyproject.toml`, no dev extras beyond stdlib unittest).
- Go: PostgreSQL via `pgx/v5`; no SQLite in production paths.
- No login/token/role auth; server fixes the actor (`DEFAULT_ACTOR_USER_ID`, default `1`).
- LLM config lives in the Go service env (provider base URL, model, key, `AGENTSERVICE_MAX_TOTAL_TOKENS` cost fuse).
- AI visual (VLM) locating is **off by default**; enabling it is a governed opt-in, never a default.

## Key Data Flows

**Agentic planning flow**: User conversation → Go AgentCore (native tool calling; explore/validate/generate/execute/report tools) → TaskPlan grounding → DSL generation → **user approval** → `execute_dsl` → queue → report. `execute_dsl` only accepts the user-approved generation; the model cannot bypass approval. SSE streams messages, ToolCalls, and artifacts; refresh replays events by sequence number from PostgreSQL.

**Execution queue flow**: Case DSL → `ExecutionBatch -> ExecutionJob -> TestCaseRun` in PostgreSQL → `cmd/execution-worker` claims jobs with `FOR UPDATE SKIP LOCKED` → stateless Python `/api/v1/internal/browser-executions` RPC runs Playwright → Go writes back step-level results with evidence (screenshot, console, network) and DSL snapshot/hash/attempt.

**Correction flow**: Failed locator → run lands `needs_intervention` → user submits correction (LocatorDebugPage) → Tier 0 priority match on future runs; rerun from the UI.

**Report aggregation**: Report Core reads run/batch/project levels from persisted structured JSON; FailureSignals and analysis summaries are stored, not recomputed ad hoc.

## Conventions

- **DSL**: Structured DSL only; no free-form NL into the executor. Targets use A11y semantic format `role="name"` — no XPath/CSS in DSL targets. `${var}` placeholders allowed in `value` only. Every executed step produces evidence.
- **Contracts first**: Change/version JSON schemas in `contracts/` before wiring new logic; both Go and Python sides validate against them.
- **Go style**: Domain-organized packages, thin HTTP handlers, application services, repository interfaces, infrastructure adapters. Ordinary Go interfaces in-process; Kitex only for separately deployed capabilities.
- **Python style**: Stateless HTTP worker; Pydantic contracts; stdlib `unittest` (no pytest dependency); SQLAlchemy only if a business-DB dependency is ever reintroduced (currently none).
- **Frontend style**: React 18 + TypeScript + Vite + Ant Design + TanStack Query + React Router. Streaming uses fetch-based SSE (POST + JSON body), not WebSocket.
- **LLM calls**: Reuse the run-scoped prompt-cache identity (BUG-204); never per-call identities. Respect context budget resets at phase boundaries; DeepSeek thinking+tools requires full reasoning-content replay (BUG-203).
- **No task-specific hardcoding**: Goals, flows, and expected facts belong in `research/` fixtures and declarative acceptance specs, never in runtime code.
- **Structured logging**: Go logs pipeline traces into `agent_events` (DB-backed, replayable); Python request logging is middleware-based. Do not reintroduce file-based `backend_structured.log` conventions.

## Project Skills

- **e2e-testing-workflow** (`.claude/skills/e2e-testing-workflow.md`): E2E manual testing of the platform — start the three services → AI planning conversation → approve & execute DSL → analyze reports → feedback loop (`fix_and_retry` / corrections). Triggered by "测试平台", "E2E 测试", "手动测试", etc.
