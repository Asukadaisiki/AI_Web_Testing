# Management-only Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development task by task; each implementation has an independent review.

**Goal:** Remove obsolete production compatibility paths while retaining the verified Langfuse management workflow.

**Architecture:** Langfuse UI/BFF and the authenticated Go API remain the product/control boundary. PostgreSQL resolves project Registry snapshots and owns leases; shared Node SDK Host and managed Python browser Worker execute them.

**Tech Stack:** Go/PostgreSQL/Kafka/Redis, TypeScript SDK Host, Python Playwright, adapted Langfuse.

**Spec:** `docs/superpowers/specs/2026-10-28-management-only-cleanup-design.md`

**Status:** Complete, 2026-10-29. Independent task reviews and final scoped
fix review passed; evidence and limits are in `docs/management-only-cleanup-2026-10-28.md`.
Changes and verification documentation are included in the cleanup commit on v3.

## Global Constraints

- Existing persisted history remains readable; no database, profile, artifact or historical evidence is deleted.
- Work in the existing feature checkout; preserve unrelated files and the Langfuse sibling.
- Retain Pi, Claude Code and Codex official SDKs, active native/flat provider adaptation, scripted deterministic tests, browser fixtures and current `pi.*` / JSONL protocol names.
- Never delete private state, stop an unrelated process, reset a database or inject faults into shared services.
- Opt-in database tests use isolated `agenttrace_test_*` databases.
- Preserve idempotency, immutable rerun lineage, environment ownership, provider affinity, controlled takeover, expiry quarantine, password redaction and live grounded browser progress.
- Implement one task at a time; parent owns integration, source capture and final native restart.

### Task 1: Mandatory Go control and leased execution

**Files:** `backend/cmd/agenttrace-api`, `backend/cmd/agenttrace-runtime`, `backend/cmd/agenttrace-management-seed`, `backend/internal/api/v3`, `backend/internal/config`, `backend/internal/postgres`, `backend/internal/runtime`, `backend/internal/event`, associated Go tests.

**Interfaces:** Keep current project Registry/Environment/Services/control endpoints and Run responses consumed by Langfuse. Commands retain explicit execution snapshot and project/environment/generation identity; the runtime assigns current fencing/worker identity after claim.

- [x] Add behavioral regressions proving removed `/agents` and global Harness routes cannot operate, management cannot be disabled into an unauthenticated mode, and agent/model-only submissions cannot create Runs.
- [x] Remove the optional management mode, fixed-conversation bridge path and global mutable Harness API; require project binding/authentication and explicit Runtime/Harness selection.
- [x] Make the store's current Run creation, lifecycle and fenced claim/event/finalization path authoritative. Remove direct snapshot/unfenced production fallback; migrate useful tests to managed fixtures.
- [x] Remove old completion aliases after auditing the active application's pending envelopes; retain existing applied migrations and read-only historical projection.
- [x] Verify full Go race and isolated PostgreSQL registry/run/lease/control paths. Report exact commands/results and any dependency needing later tasks.

### Task 2: Snapshot-only shared SDK Host

**Files:** `pi-browser-harness/src/main.ts`, `run-protocol.ts`, `session-factory.ts`, `harness-resources.ts`, SDK/browser adapter boundaries and corresponding tests.

**Interfaces:** Shared host commands `run`, `cancel`, `shutdown`; each run includes explicit runtime_id and immutable execution Harness snapshot. Browser requests carry complete managed lease identity assigned by Go.

- [x] Add rejection tests for snapshotless requests, unsupported/no runtime selection, legacy startup-module fallback and obsolete one-shot CLI entry.
- [x] Remove one-shot process mode, default-Pi selection, `AGENTTRACE_HARNESS_MODULE` loader and disk-Skill/default-Harness production fallback.
- [x] Keep lazy official SDK selection, reviewed capability resolution, generic selected Harness execution, native/flat Codex support, cleanup acknowledgements and bounded public-password protection.
- [x] Update meaningful test fixtures to the explicit current contract; remove tests solely exercising deleted compatibility.
- [x] Run full Host tests, typecheck/build and the real Chromium password regression separately from dist-rebuilding tests.

### Task 3: Managed browser Worker only

**Files:** `worker/loop_worker/main.py`, `sessions.py`, `contracts.py`, browser environment/runner integration and worker tests.

**Interfaces:** Current internal provider `/environments/...`, `/sessions` and `/execute`; identity includes project_id, environment_id, profile_generation, fencing_token and owned browser session handle when replaying.

- [x] Add HTTP/model tests proving incomplete/unmanaged session/replay identity is rejected and requests cannot bypass internal authentication.
- [x] Remove unmanaged session/context/replay production branches; retain fresh managed dry-run context from Run-start identity.
- [x] Keep encrypted provider-affine profiles, network/upload policy, same-fence repair and uncertain-cleanup quarantine.
- [x] Migrate still-valuable browser/action/session tests to current managed fixtures; run the full worker suite.

### Task 4: Remove old product/deployment and migrate acceptance helpers

**Files:** Delete `web/`, `compose.yaml`, obsolete `integration/v3`; update `integration/local/run.py`, `run_test.py`, current management/fault acceptance imports, `.env.example`, current Dockerfiles and active documentation.

**Interfaces:** Native launcher starts fixture/browser/API/runtime and optional Exporter, always uses managed bootstrap and existing private Langfuse deployment. Current acceptance imports shared helpers without exposing old `/agents` or mutable Harness workflow.

- [x] Add launcher regression proving one managed plan regardless of deprecated flag and mandatory private provider/bridge settings.
- [x] Extract SQL/environment/SSE/wait/cleanup helpers still consumed by current management and infrastructure acceptance into a focused support module; update imports and helper tests.
- [x] Delete legacy native acceptance/composition/fault entrypoints whose replacement managed scenarios are already present. Remove old React frontend and legacy Compose/scenario tree after checking source/build references.
- [x] Update launcher/config examples, Docker wiring and active docs to the single current architecture; retain dated historical evidence.
- [x] Verify native offline tests, source-reference audit and independent final review. Run final full Go/Host/worker checks and scoped real managed SDK/UI smoke after controlled quiescent restart.

## Completion

- [x] Record removed code, fresh verification, final review and deployed identity in `docs/management-only-cleanup-2026-10-28.md`.
- [x] Preserve original profile/history/quarantine, finish any owned test cleanup, and report resulting source/commit status.
