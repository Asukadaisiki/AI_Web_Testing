# Runtime and Harness Composition Implementation Plan

> **For agentic workers:** Use subagent-driven-development for independent
> implementation and focused review, preserving existing workspace changes.

**Goal:** Select Pi, Claude Code or Codex with independently managed Harness
data, retain immutable execution snapshots and verify official SDK execution.

**Architecture:** Go coordinates Runs behind the existing versioned JSONL
boundary. SDK Adapters implement ExecutionSession, Strategy dispatch selects by
runtime_id, and PostgreSQL stores Harness data and Run snapshots.

**Tech Stack:** Go/PostgreSQL/Kafka/Redis, TypeScript/Pi/Claude Agent SDK/Codex
SDK/MCP, React, native Playwright Worker, optional Langfuse.

**Spec:** `docs/superpowers/specs/2026-10-28-runtime-harness-design.md`

## Global Constraints

- Native only; no Docker. Preserve uncommitted existing work.
- No credentials in arguments, events, reports or public configuration.
- Existing contracts remain backward compatible; runtime selection is explicit.
- Harness contains skills, MCP and system prompt, plus identity/revision.
- Do not add runtime substitutes, plugin frameworks or duplicate observers.
- Keep test output/artifacts outside repository except sanitized final reports.

## Tasks

- [x] 1. Go execution contract/catalog + Harness PostgreSQL/API integration.
  Files: `internal/execution`, event envelope, config, API message/server/routes,
  PostgreSQL Harness CRUD/migration and message snapshot creation, PiRunner request.
  Regression: unknown selections rejected, revisions increment, snapshots remain
  after edit/delete, idempotency distinguishes execution, legacy requests work.
- [x] 2. SDK-free Harness materialization and attempt-local MCP helper.
  Files: `execution-spec.ts`, `harness-resources.ts`, `runtime-mcp.ts`, tests.
  Regression: traversal/environment references, scopes, real MCP calls,
  normalized evidence, host-only cleanup, delayed/failed release.
- [x] 3. Claude Code Adapter using official query SDK.
  Files: `claude-session.ts` and SDK tests, lazy factory branch.
  Regression: prompt/skills/MCP wiring, completion/error/usage, abort/cleanup.
  Live verification uses compatible provider; never falls back to Pi.
- [x] 4. Codex Adapter using official TypeScript SDK.
  Files: `codex-session.ts` and SDK tests, lazy factory branch.
  Regression: instructions/MCP/config isolation, streaming/error/usage,
  cancellation joins process before releasing helper.
  Live verification imports only local provider credentials into private env.
- [x] 5. Factory strategy and managed Harness integration for existing Pi.
  Files: run protocol, session factory, Pi harness resolver, protocol tests.
  Three selected runtime paths cross Go host boundary and preserve old Pi runs.
- [x] 6. React Runtime/Harness selection, compact configuration editor and Run
  identity display. Regression: selected combination sent, no invalid model mix,
  create/update/delete refresh, narrow layout.
- [x] 7. Native combined acceptance, independent code review and README/contracts.
  Verify actual SDK matrix, same browser tools, second Harness, cancellation,
  immutable snapshots and optional Langfuse. Final report includes limitations.

Native acceptance passed all 15 actual SDK scenarios, followed by a fresh Pi
browser Run on the final build. Final SDK suite: 422 passed, 1 opt-in live smoke
test skipped; actual live provider acceptance passed separately.
Report: `docs/runtime-harness-acceptance-2026-10-28.md` and companion JSON.

### Native provider compatibility correction

- [x] 8. Optional Codex namespace-to-flat-function provider adapter, selected
  explicitly through execution-host environment. Preserve official SDK/native
  CLI, actual MCP execution, unmodified results/usage, attempt isolation and
  awaited listener cleanup. Regression tests exercise actual SDK streaming
  through a provider that accepts flat functions, including function-call
  deltas, subsequent tool outputs, error propagation and cancellation.
  Live comparison confirmed namespace request yields assistant text only,
  while the same GLM endpoint with flat declarations yields a function call.
  This is an authorized correction needed to complete the SDK matrix.
