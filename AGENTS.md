# AGENTS.md

This repository follows the OpenAI Codex `AGENTS.md` mechanism for repo-specific instructions.

## Scope

These instructions apply to the entire repository.


## Codex and CLAUDE Working Rules

Before making changes:

- Search the repository for related files first.
- Understand local patterns before editing.
- Prefer minimal changes over broad refactors.


When implementing:

- Define or update types and schemas before adding logic.
- Add or update tests for every meaningful feature.
- Avoid inventing unsupported frameworks or tools.
- Keep comments short and only where they add clarity.

When responding:

- Use Chinese unless the user explicitly requests another language.
- Include `Summary`, `Changes`, `How to run`, `Tests`, and `Notes` in final responses.
- State assumptions and limitations clearly.
- Answer in chinese


## Product Goal

Build an AI-enhanced Web UI automation testing platform.

Primary goals for the first milestone:

- Manage test cases and suites through a web platform
- Execute structured test cases through a stable backend runner
- Produce step-level evidence and reports
- Support DOM-based enhanced locator logic before adding vision-based ranking

## Architecture Rules

The repository is organized as a Go control plane, a stateless Python browser worker, and a TypeScript frontend.

- `backend-go/` contains the Hertz HTTP/SSE API, AgentCore, tool registry, TaskPlan/research state machines, execution queue, report aggregation, application services, and control-plane PostgreSQL persistence.
- `browser-worker/` contains the stateless Python Playwright/A11y/locator worker: browser capability API and a stateless browser-execution RPC. It must not touch the business database.
- `frontend/` contains the React + TypeScript platform UI; it calls the Go `/api/v2` API only.
- `contracts/` contains versioned JSON schemas (browser observation, locator spec, target binding, grounding queries, pipeline trace) shared across services.
- `docs/` contains project planning, DSL specification, UI planning, and architecture notes.

Keep the following boundaries:

- The frontend must not contain execution logic for official test runs.
- The backend runner is the only source of truth for test execution results.
- AI generation and analysis must not bypass structured DSL validation.
- Reports must be based on structured JSON data first, with UI rendering built on top of that data.
- Schema/contract changes must be versioned and backward compatible on the wire.

## Backend Rules

- Use Go for new AgentCore and control-plane development.
- Use Hertz for browser-facing HTTP and SSE APIs.
- Use ordinary Go interfaces for in-process boundaries; use Kitex only when a capability is deployed as a separate service.
- Organize Go code by domain with thin transport handlers, application services, repository interfaces, and infrastructure adapters.
- Keep the Python Playwright/A11y/locator implementation as an isolated stateless browser worker; it exposes only internal browser capability and execution RPCs.
- Use `uv` for the Python worker dependency and environment management.
- The Python worker holds no ORM and no business-database access; SQLAlchemy/Alembic were removed. Do not reintroduce them without an explicit architecture decision.
- PostgreSQL is the only production datastore; all business persistence and migrations live in Go (`backend-go/cmd/migrate`, `internal/dbschema`).
- Add compatible migrations for schema changes.
- Keep execution logic, locator logic, and reporting logic in separate modules.
- LLM runtime rules: reuse the run-scoped prompt-cache identity per run, keep the cost fuse (`AGENTSERVICE_MAX_TOTAL_TOKENS`) enforced, and respect provider hard constraints on reasoning-content replay and context budget resets (see `docs/plan/2026-09-18-context-budget-design.md`).
- The current local milestone does not implement login, token, or role authorization. Preserve project and actor ownership fields for a later identity adapter.


## Frontend Rules

- Use React + TypeScript + Vite.
- Build the UI as a platform plus workbench, not as a browser extension product.
- The core pages are dashboard, case management, suite management, execution center, report center, and case workbench.
- The workbench can preview pages and debugging data, but official execution remains backend-driven.
- Prefer clear information architecture over decorative UI.

## DSL and Execution Rules

- All runnable test cases must be represented as structured DSL.
- Validate DSL before execution.
- Do not allow free-form natural language directly into the executor.
- Keep first-phase actions limited to a small stable set.
- DSL targets must use A11y semantic format `role="name"`; XPath/CSS targets and `${var}` placeholders in `target` are not allowed. `${var}` placeholders are allowed in `value` only.
- Every executed step must produce evidence.
- Locator output should record target, candidates, final match, and failure reason when available.
- `execute_dsl` only accepts the user-approved DSL generation of the current AgentRun; the model must never be able to bypass approval.

## No Task-Specific Hardcoding

- Reusable Agent, Harness, Tool, E2E driver, Runner, and Oracle code must not hardcode task-specific product names, prices, quantities, URL paths, selectors, action order, or expected outcomes.
- Task-specific goals, flows, and expected facts belong in versioned datasets, fixtures, or declarative acceptance specifications loaded as data.
- The Agent Task Plan owns business actions, order, parameters, and occurrence counts. Exploration only grounds and verifies PlanSteps; it must not inject or rewrite task semantics.
- Generic validators and Oracles may interpret declarative contracts, but must not add one-off branches for a named E2E task.
- Do not modify reusable runtime code merely to make one acceptance task pass.


## Collaboration Preference

- This repository is maintained by a single owner by default.
- Do not default to Pull Request workflows for completed work.
- When code is ready and the user asks to sync changes, prefer direct branch push or direct GitHub sync.
- Only suggest, create, or rely on a Pull Request when the user explicitly asks for PR flow.

## GitHub Sync Reference

When the user asks Claude Code to sync the current changes to GitHub, prefer a clear non-interactive flow:

```bash
# 1) Review current changes
git status --short
git branch --show-current

# 2) Stage only the intended files
git add AGENTS.md docs/execution-log.md

# 3) Create a focused commit
git commit -m "docs: add github sync reference for claude code"

# 4) Push the current branch
git push origin HEAD
```

Additional reference commands for common cases:

```bash
# Push main directly when already on main
git push origin main

# First push for a new branch and set upstream
git push -u origin <branch-name>

# Quick verification after push
git status --short
git log -1 --stat --oneline
```

Notes for Claude Code:

- Prefer explicit file paths in `git add` over blindly staging everything.
- Prefer one focused commit per completed task.
- Use concise Conventional Commit style messages such as `feat: ...`, `fix: ...`, `docs: ...`, `test: ...`, `refactor: ...`.
- Avoid interactive git flows by default.
- After pushing, report the branch name, latest commit hash, and whether the worktree is clean.

## Task Logging Rules

- After completing any meaningful task, append one record to `docs/execution-log.md`.
- If a clear defect, failure, inconsistency, or follow-up fix item is found during the task, append one record to `docs/bug-log.md`.
- Update the log files before sending the final response.
- If the task only involves analysis, debugging, validation, or documentation updates, it should still be recorded in `docs/execution-log.md`.
- The user may explicitly opt out for a specific task; otherwise logging is the default behavior.
- After completing a requirement or bug-related task, explicitly ask the user whether to sync the current changes to GitHub.
- Always write the latest record in the front of the log file instead of the end.

## Non-Goals for the First Milestone

- Do not turn the product into a generic browser agent.
- Do not rely on vision as the primary locator path.
- Do not let AI directly control browser execution without DSL validation.
- Do not optimize for complex distributed deployment before the local platform flow works.
