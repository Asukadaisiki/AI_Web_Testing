# Langfuse First Closed Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Complete actual Runtime/Harness submission, durable state, cancellation,
failed rerun and native Trace navigation inside Langfuse.

**Architecture:** Extend the existing Langfuse browser-runs feature and BFF.
Keep Go/official SDK execution and Harness data independent. Protect the
service boundary and bind the first release to one project/conversation.

**Tech Stack:** Langfuse v3.225.14 fork, Node 24, pnpm 11.10.0, Next.js/tRPC/React,
existing Go/PostgreSQL/Kafka/Redis and official Pi/Claude Code/Codex SDKs.

**Spec:** `docs/superpowers/specs/2026-10-28-langfuse-first-loop-design.md`

## Global Constraints

- User approved implementation; continue without another plan approval.
- Native only, no Docker; preserve all existing changes, credentials and data.
- No commits/pushes/merges. Current branches are non-main feature branches.
- One configured Langfuse Project, Go Project and Conversation; installation
  Harness catalog is only exposed through that authorized Langfuse Project.
- No Langfuse Prisma/ClickHouse schema changes or Dataset batch implementation.
- Browser code never receives bridge/provider/Langfuse credentials.
- Canonical `/trace` is authoritative; do not recreate old `/evaluation`.
- Final reports in the AI_Web_Testing workspace; scratch/logs in Cache.
- Implementers do not spawn agents, rebuild running services or restart them.

### Task 1: Go service authentication and exporter project scope

**Files:**
- Create `backend/internal/api/v3/bridge_auth.go` and covering tests.
- Modify `backend/internal/api/v3/server.go`, `backend/cmd/agenttrace-api/main.go`,
  `backend/internal/config/config.go` and their tests.
- Modify `backend/internal/export/langfuse/exporter.go`,
  `backend/cmd/agenttrace-exporter/main.go` and covering exporter tests.
- Modify `.env.example` only for public empty/default configuration fields.

**Interfaces:**
Produces `Config.BridgeToken`, `ServerConfig.BridgeToken`,
`Config.BridgeProjectID`, `Config.BridgeConversationID`, matching API config fields,
`config.Exporter.ExportProjectID`, `langfuse.Exporter.ExportProjectID`.
Environment names are `AGENTTRACE_BRIDGE_TOKEN` and
`AGENTTRACE_EXPORT_PROJECT_ID`; both optional and empty by default. Bridge
binding IDs use `AGENTTRACE_PROJECT_ID` and `AGENTTRACE_CONVERSATION_ID`;
token and both binding IDs must be configured together.
Consumes existing API handler and `Repository.GetRun` / inbox export boundary.

- [x] Write regressions before implementation. Real HTTP assertions include:

```go
response := request(t, protectedHandler, http.MethodGet, "/api/v1/runtimes", "")
if response.Code != http.StatusUnauthorized { t.Fatal(response.Code) }
health := request(t, protectedHandler, http.MethodGet, "/api/v1/health", "")
if health.Code != http.StatusOK { t.Fatal(health.Code) }
```

  Add correct/wrong Bearer, no-token legacy, partial configuration, foreign
  Project/Conversation, route allowlist and artifact protection/symlink cases.
  Export a completion for another project with a recording client; assert
  zero ingestion, successful acknowledgement and unchanged Run.
- [x] Run focused tests and observe the expected authorization/filter failures.
- [x] Add a single optional bridge guard around the current routes, with the
  exact health exception and secret-safe constant-time token comparison.
  Allow runtime/Harness CRUD, only bound Conversation read/submit and owned
  Run read/trace/events/cancel/rerun. Deny other routes in bridge mode. Verify
  ownership before dispatch; pin both configured Project and Conversation.
  Only serve regular artifact files matching canonical screenshot evidence,
  the Run namespace and filesystem containment including symlinks.
  Wire and validate configuration in API startup. Apply project filter immediately after
  loading the Run inside the existing inbox-owned export operation.
- [x] Run config/API/exporter/command tests, then `go test -p 1 ./...`.
  Integration tests use only a separate `agenttrace_test_*` database.
- [x] Write a scoped source diff and test report to the plan's Cache directory.
  Do not commit. Await coordinator task review.

### Task 2: Langfuse product feature and BFF integration

**Files (relative to `/Users/bytedance/project/langfuse-agenttrace`):**
- Modify `web/src/features/browser-runs/contracts.ts`.
- Modify `server/agenttrace-client.ts`, `server/router.ts` in that feature.
- Modify `components/NewRunDialog.tsx`, `RunActions.tsx`, `RunList.tsx`,
  `BrowserEvidencePanel.tsx`, `AgentTracePanel.tsx`.
- Create feature-local `components/HarnessManager.tsx`; use a small
  `trace-evidence.ts` only if the evidence projection is shared meaningfully.
- Modify `hooks/useRunEvents.ts`, `pages/BrowserRunsPage.tsx`.
- Modify `web/src/pages/api/agenttrace/artifacts/[runId].ts` for canonical
  screenshot authorization; preserve the events proxy's authorized transport.
- Modify visible sidebar/page labels in `web/src/components/layouts/routes.tsx`
  and `web/src/pages/project/[projectId]/browser-runs/index.tsx` as needed.
- Add/update colocated client tests and
  `web/src/__tests__/server/unit/browser-runs/*.servertest.ts`.
- Update web-local AGENTS/env examples only when their maintenance contract applies.

**Interfaces:**
Consumes Task 1's protected Go API and the spec's existing REST contracts.
Adds tRPC `runtimes`, `harnesses`, `createHarness`, `updateHarness`,
`deleteHarness`, `trace`; existing create/get/list/cancel/rerun retain names.
`create` receives `{projectId,goal,url?,runtimeId,harnessId,idempotencyKey}`.
`trace` receives `{projectId,runId}` and returns the canonical trace structure.
Runtime catalog and Harness list return arrays after validating named wrappers.
`evaluation` call sites migrate together to `trace`; remove obsolete calls.

- [x] Read root/web AGENTS and applicable local React, PostHog, Sentry and
  browser-review skills. Preserve existing Langfuse layout/UI primitives.
- [x] Write actual contract/client and component regressions first. For the
  controlled HTTP boundary, assert the actual message body equals:

```typescript
{
  content: "Use the acceptance skill.",
  runtime_id: "codex",
  harness_id: "har_example",
  idempotency_key: "draft-key",
}
```

  Cover optional URL, unavailable runtime, wrong LF project, foreign Go Run,
  Harness CRUD/delete 200+204, exact screenshot allowlist and canonical missing
  evidence. Component tests cover selection-aware keys, historical revision,
  pending/error states, cancellation/rerun, and stream/poll state refresh.
- [x] Observe regression failures with existing Vitest client/server-unit projects.
- [x] Implement validated contracts, authenticated client and protected router.
  No model credentials or arbitrary upstream proxy path reaches the browser.
  Reuse `playground:execute` for execution mutations, `project:update` for
  Harness configuration writes, and configured project checks for every call.
- [x] Implement the compact Run composer and Harness manager, persisted
  identity, safe lifecycle actions and canonical evidence extension.
  Keep native model Trace rendering unchanged. Derive query state directly;
  effects only synchronize actual external streams/timers.
- [x] Run Node-24 targeted client/server-unit tests, typecheck, required scoped
  lint. Coordinator owns production build/browser signoff after code freezes.
- [x] Write source diff, complete test evidence and concerns to Cache; no commit.

### Task 3: Native binding, production build and actual browser acceptance

**Files:**
- Modify native integration documentation and launcher only when necessary.
- Final output `docs/langfuse-first-loop-acceptance-2026-10-28.{md,json}`.
- Intermediate acceptance scripts, browser traces/screenshots and private
  binding files live under Cache, not the source workspace.

**Interfaces:**
Consumes Task 1/2 source-ready results and current owned native services.
Five private Langfuse BFF environment values bind one actual Go Project and
Conversation. The same bridge token protects Go and authenticates BFF requests.
Exporter uses the bound Go Project ID and existing private Langfuse API keys.

- [x] Use real API/UI creation for the scoped Project/Conversation and Harness
  test data. Do not reset databases or invent trace records.
- [x] Build Langfuse with Node 24 into a fresh Next dist directory using private
  native environment. Run the required client-bundle scan on that build.
  Never overwrite a dist directory while it is serving a live process.
- [x] After active Runs/outbox drain, restart only owned Go services and native
  Langfuse processes with private bridge values and the same persistent Cache
  root. The normal browser Worker remains on 8110.
- [x] In a real browser at 3300, log in using private local credentials and test
  generic Harness submission for Pi/Claude/Codex, actual browser evidence for
  one selected Runtime, active cancellation, failed rerun, Harness CRUD and
  native Trace navigation. Verify screenshots are actual PNGs and scope checks
  reject a foreign Run.
- [x] Verify project-scoped export/read correlation and immutable Harness
  identity/snapshot after edits/deletion. Record real IDs/status/usage/evidence.
- [x] Complete independent task/final review, fix material findings, validate
  final health and leave the Langfuse product running. Update plan checkboxes,
  operational docs and save sanitized final reports with actual limitations.
