# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Rules

Read and follow all instructions in AGENTS.md in this repository.

## Commands

### Backend (from `backend/`)

```bash
uv sync                                    # Install dependencies
uv run alembic upgrade head                # Run database migrations
uv run backend-dev                         # Start dev server (http://127.0.0.1:8000)
uv run pytest                              # Run unit tests (tests/unit/)
uv run pytest tests/integration -m browser_integration   # Browser regression tests
uv run pytest tests/integration/test_platform_api_chain.py -v  # API chain integration tests
uv run pytest tests/unit/test_dsl_validation.py -k "test_name"  # Run single test
```

### Frontend (from `frontend/`)

```bash
npm install          # Install dependencies
npm run dev          # Start dev server (http://127.0.0.1:5173)
npm run build        # Production build (tsc --noEmit && vite build)
npm test -- --run    # Run Vitest tests
```

## Architecture

Monorepo with Python backend, TypeScript frontend, and docs:

```
backend/app/
  main.py              # FastAPI app factory, Uvicorn entry
  api/router.py        # Route assembly (auth, cases, executions, corrections, dsl, ai-planning, etc.)
  api/routes/          # Thin route handlers
  services/            # Business logic: executions, dsl, cases, ai_planning, corrections
  models/              # SQLAlchemy 2.x ORM: TestCase, TestCaseRun, LocatorCorrection, AIPlanning*, User
  runners/
    playwright_runner.py   # Execution engine: sync + streaming modes, artifact collection
  locators/            # 4-tier hybrid locator system
    corrections.py     # Tier 0: historical manual corrections (priority match)
    semantic.py        # Tier 1: DOM semantic (element_id, CSS, XPath, case-insensitive text match)
    ai_visual.py       # Tier 2: VLM-based visual locate (disabled by default)
    fallback.py        # Tier 3: raise InterventionNeededError, collect DOM snapshot
  ai/
    dsl_generator.py       # NL→DSL with governance, auto-repair, rejection tracking
    test_planning_agent.py # ReAct-style conversational test planning agent
  schemas/dsl.py       # Pydantic DSL models (GotoStep, ClickStep, InputStep, etc.)
  core/config.py       # Settings from env vars

frontend/src/
  app/AppRouter.tsx          # React Router v6, lazy-loaded pages
  pages/                     # PlanningPage, CasesPage, ReportPage, ExecutionDetailPage
  components/                # AITestPlanningPanel, NotebookNav, StepList, InterventionPanel
  services/executionWebSocket.ts  # WebSocket client for streaming execution events
```

## Key Data Flows

**Execution flow**: Case DSL → `playwright_runner` → per-step locator fallback chain → evidence (screenshot, console, network) → `TestCaseRun` with step-level results.

**Streaming flow**: WebSocket at `/api/v1/ai-planning/sessions/{id}/ws` → thread worker → `StepStreamEvent` objects → frontend socket client → React Query cache invalidation.

**AI Planning flow**: User conversation → `test_planning_agent` (ReAct + tool calls) → DSL draft → user review → save as TestCase → trigger execution → stream progress.

**Correction flow**: Failed step → `needs_intervention` → user submits correction → stored as `LocatorCorrection` → Tier 0 priority match on future runs.

## Conventions

- **Language**: Respond in Chinese unless user requests otherwise. Include Summary, Changes, How to run, Tests, Notes sections.
- **Backend**: FastAPI + SQLAlchemy 2.x + Alembic. Route handlers thin, logic in services. SQLite for local dev, PostgreSQL for production design.
- **Frontend**: React + TypeScript + Vite + Ant Design + TanStack Query. No execution logic in frontend.
- **Testing**: `tests/unit/` for unit tests, `tests/integration/` for integration tests. `browser_integration` pytest marker for browser-level tests. All meaningful features need tests.
- **Git**: Single-owner repo. Direct push preferred over PRs. Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`). One focused commit per task.
- **Task logging**: Append to `docs/execution-log.md` after completing tasks, `docs/bug-log.md` for defects. Ask user about GitHub sync after completing requirements.
- **DSL**: All test cases must be structured DSL. No free-form NL into executor. Validate before execution. Every step produces evidence.
- **AI**: AI generation cannot bypass DSL validation. AI visual is opt-in (disabled by default). DSL generator outputs governance metadata (warnings, normalization_notes, generation_meta).
