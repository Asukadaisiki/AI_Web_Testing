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

The repository is organized as a Go control plane, a Python browser worker, and a TypeScript frontend.

- `backend-go/` contains the Hertz HTTP/SSE API, AgentCore, tool registry, application services, and control-plane persistence.
- `backend/` is the legacy FastAPI backend during migration and remains the Python Playwright/A11y/locator execution worker after migration.
- `frontend/` contains the React + TypeScript platform UI.
- `docs/` contains project planning, DSL specification, UI planning, and architecture notes.

Keep the following boundaries:

- The frontend must not contain execution logic for official test runs.
- The backend runner is the only source of truth for test execution results.
- AI generation and analysis must not bypass structured DSL validation.
- Reports must be based on structured JSON data first, with UI rendering built on top of that data.

## Backend Rules

- Use Go for new AgentCore and control-plane development.
- Use Hertz for browser-facing HTTP and SSE APIs.
- Use ordinary Go interfaces for in-process boundaries; use Kitex only when a capability is deployed as a separate service.
- Organize Go code by domain with thin transport handlers, application services, repository interfaces, and infrastructure adapters.
- Keep the existing Python Playwright/A11y/locator implementation as an isolated browser worker until replacement has equivalent contract and browser coverage.
- Use `uv` for the Python worker dependency and environment management.
- Use SQLAlchemy 2.x style models and sessions in retained Python modules.
- Use PostgreSQL for production design assumptions.
- Use SQLite only for lightweight tests that do not validate production migrations or queue locking.
- Add compatible migrations for schema changes.
- Keep execution logic, locator logic, and reporting logic in separate modules.
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
- Every executed step must produce evidence.
- Locator output should record target, candidates, final match, and failure reason when available.


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
