# Langfuse Agent Runtime Management Implementation Plan

> **For agentic workers:** Use subagent-driven-development for independent tasks and review each phase before proceeding.

**Goal:** Implement the approved runtime management design through Phase 0–4, retaining Langfuse as the sole product entry.

**Architecture:** PostgreSQL owns execution facts and immutable registry versions. Kafka delivers commands keyed by Environment; Redis supplies best-effort wakeups. Project-scoped Control API and Langfuse BFF manage registry, conversations, browser environments and worker capacity.

**Tech Stack:** Go, PostgreSQL, Kafka, Redis, TypeScript SDK host, Python Playwright worker, Langfuse Next.js/tRPC.

**Spec:** `docs/superpowers/specs/2026-10-28-langfuse-agent-runtime-management-design.md`

**Status:** Phase 0–4 implemented and locally verified on 2026-10-28.
See [acceptance evidence](../../management-runtime-acceptance-2026-10-28.md)
and [final review](../../management-runtime-review-2026-10-28.md).
Existing dirty work is retained; no commit or push. Native local services were
updated after quiescence and verified with all three real SDKs.

## Global Constraints

- One Run selects one Runtime and one Harness; no agent delegation.
- One Conversation exclusively owns one Browser Environment.
- Published Skill/MCP/Harness versions and Run snapshots are immutable.
- Secrets remain references; no arbitrary user MCP command, host env or host filesystem.
- No database transaction remains open during external execution.
- Lost execution with possible external effects becomes failed/needs_review and is never automatically replayed.
- PostgreSQL integration tests only use independent `agenttrace_test_*` databases.
- Preserve existing uncommitted work; work in current feature branch `v3`.
- Do not commit, push or deploy without a separate instruction.

## Phase 0: safety and consistency

**Files:** runtime consumer/inbox, execution spec/builtin, TypeScript execution-spec/resources, Python actions and managed uploads, associated tests.

- [x] Add failing tests proving inbox claims commit before runner invocation and duplicate concurrent delivery cannot start another runner.
- [x] Replace runtime inbox long transaction with durable claim/finalize; interrupted claims reconcile the existing attempt.
- [x] Add failing Go/Node tests rejecting user command/args/env and resolving only reviewed capability descriptors and scoped secret references.
- [x] Register the deployed browser capability server-side and remove unrestricted command/env materialization.
- [x] Add Python tests for traversal, absolute paths, symlinks and out-of-scope uploads, then constrain file upload to managed relative files.
- [x] Verify Go, host and worker suites before Phase 1.

## Phase 1: project-scoped registry

**Files:** new registry package/models/validation, PostgreSQL registry migration/repository, API registry handlers, host snapshot materialization.

**Interface:** project-scoped upload previews and published Skill versions; system-owned MCP capability versions; project bindings; Harness version resolution returns `execution.HarnessSnapshot`.

- [x] Test archive size/type/path/digest validation and secret-reference/config schema rejection first.
- [x] Persist Skills, immutable versions, MCP definitions/versions/bindings and project Harness versions with audit.
- [x] Resolve selected fixed versions in the same transaction that creates Run and Outbox; reruns use source snapshot.
- [x] Expose project-scoped endpoints with admin/system role checks.
- [x] Verify revoked/current version changes cannot mutate existing snapshots.

## Phase 2: conversations and environments

**Files:** project binding/conversation/environment migrations and repositories, API ownership guard/handlers, Langfuse agent-runs BFF/UI, exporter Session mapping.

**Interface:** explicit `X-Agenttrace-Project-Id` from authenticated BFF; canonical Conversation ID in every conversation/run request; environment lifecycle commands.

- [x] Test atomic Conversation + Environment + provisioning Outbox creation and cross-project refusal.
- [x] Add project bindings, conversation archive/tombstone and environment lifecycle state transitions.
- [x] Gate Run creation on active conversation/ready environment and guard deletion against Run, delivery and Lease activity.
- [x] Replace fixed conversation BFF routing with project-scoped list/create/select/archive/delete.
- [x] Process provision/reset/delete through durable lifecycle commands and deterministic `agenttrace-session-{conversation_id}` grouping.

## Phase 3: scheduling and worker recovery

**Files:** worker/assignment/lease migration/repository, runtime registration/heartbeat/runner/store fencing, Kafka environment keys, stream fallback controls.

**Interface:** durable worker registration, atomic capacity + lease claim, monotonically increasing fencing token, conditional event/finalize/release.

- [x] Test concurrent claims and expired/stale-token writes before implementation.
- [x] Add runtime/browser worker role, capacities, heartbeat and drain.
- [x] Claim Assignment/Lease using short transactions; renew while running and release after host acknowledgement.
- [x] Reject stale writes, recover expired attempts without replaying external work and mark environments needs_review.
- [x] Run outage/concurrency integration tests and record topology and measured results.

## Phase 4: browser identity, takeover and services

**Files:** browser provider/control-session package, worker encrypted profile storage and input gateway, API control-session/environment/services handlers, Langfuse management pages.

**Interface:** short-lived one-time project-scoped control URL; managed browser profile identified by Environment and generation.

- [x] Test expiry/replay/ownership and generation fencing.
- [x] Persist encrypted browser storage state; reset advances generation and invalidates old writes.
- [x] Provide takeover page with controlled browser screenshot/input; stop and review/reset actions.
- [x] Add Langfuse Registry, Environment and admin Services controls using existing project RBAC.
- [x] Run final regression, browser acceptance and independent review; publish honest evidence and remaining deployment limitations.

## Rollback and validation

Use additive schema and a management feature flag, retaining existing read contracts while migration runs. Disable management writes and drain workers before rollback; retain immutable version and execution records. Do not downgrade to unrestricted MCP execution. Build Go binaries and TypeScript host, run worker/host/Go suites, isolated PostgreSQL integration and Langfuse BFF tests; inspect product behavior in the browser before claiming the complete product workflow.
