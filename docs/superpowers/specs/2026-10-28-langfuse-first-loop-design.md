# Langfuse Runtime / Harness first closed loop

Status: user approved implementation on 2026-10-28.

## Product outcome

Langfuse is the primary product UI. A member of the configured Langfuse project
can select Pi, Claude Code or Codex and an independent Harness, submit a real
execution, observe its durable status, cancel active work, rerun failed work,
and open the corresponding native Langfuse Trace. Harness CRUD is available
within the Langfuse feature. Browser evidence remains an optional extension.

This phase does not include Dataset batch execution, Experiment comparison,
multi-project credential routing, SDK upgrades or a second model/trace viewer.

## Existing implementation and scope

Langfuse checkout: `/Users/bytedance/project/langfuse-agenttrace`, branch
`feat/browser-agent-workspace`, baseline `05fe6fe3c310299f4eb3a19989fc14c9a931878f`.
It already contains `web/src/features/browser-runs`, a project page, tRPC
router, authorized SSE/artifact proxies and native Trace evidence panel.
Continue this feature; retain its URL and router names for compatibility.
Change the visible feature name to Agent runs.

Go/SDK execution remains in `/Users/bytedance/project/AI_Web_Testing`.
Its current Run, Harness and trace contracts are the implementation authority.
The former `/evaluation` response is not the current `/trace` response.
Do not recreate an evaluation API or fabricate its absent verdict/repair fields.

## Ownership and deployment

- Langfuse owns login, project membership, native trace storage and display.
- Go owns durable Run state, commands, cancellation, immutable Harness
  snapshots and independent Harness storage.
- Pi, Claude Code and Codex continue through their official SDK adapters.
- The exporter projects completed canonical traces through Langfuse ingestion.
- First release binds one configured Langfuse Project, Go Project and
  Conversation. Run reads/actions verify the bound Go Project.
- Harness is an installation-level catalog in this release, reachable from
  that one authorized Langfuse Project. This is not multi-project Harness
  tenancy; dynamic project routing is a subsequent phase.
- No Langfuse Prisma or ClickHouse schema changes.

## Backend boundary

Add optional `AGENTTRACE_BRIDGE_TOKEN`, wired through `config.Config.BridgeToken`
and `api/v3.ServerConfig.BridgeToken`, together with `BridgeProjectID` and
`BridgeConversationID` from `AGENTTRACE_PROJECT_ID` and
`AGENTTRACE_CONVERSATION_ID`. Reject partial bridge configuration.
When configured, every API and artifact request requires the service Bearer
token, except `/api/v1/health`. Only runtime/Harness catalog and CRUD, the bound
Conversation read/message submission, and bound Run read/trace/events/cancel/
rerun routes are reachable. Project CRUD, conversation deletion, agent listing
and input-response routes are outside this first-loop bridge.
Check the Run's Project and Conversation before reading evidence or opening
SSE; check the Conversation's actual Project on submissions and list reads.
The unrestricted legacy mux must not remain reachable in bridge mode.
Artifacts require exact persisted screenshot reference, matching Run namespace,
bound Run ownership, filesystem containment including symlinks, and a regular
file; directory browsing and arbitrary paths are rejected.
When absent, current native development and existing API tests keep working.
Do not log or echo tokens. Browser code never receives this credential.

Add optional `AGENTTRACE_EXPORT_PROJECT_ID`, wired through
`config.Exporter.ExportProjectID` and `langfuse.Exporter.ExportProjectID`.
An exporter configured with this value only exports Runs belonging to the
bound Go project. Other projects are acknowledged without ingestion or retry.
Existing delivery locks, inbox, retry/DLQ and model-usage semantics remain.

## Langfuse contract

Use `protectedProjectProcedure`. Every procedure verifies configured
`AGENTTRACE_LANGFUSE_PROJECT_ID`; reads require membership and mutations require
`playground:execute`. Harness configuration writes require `project:update`
(owner/admin), because they configure executable MCP resources. Keep five
existing bridge settings server-only:
`AGENTTRACE_API_URL`, `AGENTTRACE_BRIDGE_TOKEN`,
`AGENTTRACE_LANGFUSE_PROJECT_ID`, `AGENTTRACE_PROJECT_ID`,
`AGENTTRACE_CONVERSATION_ID`.

The client consumes:

```typescript
type RuntimeId = "pi" | "claude-code" | "codex";
type Runtime = {
  id: RuntimeId; name: string;
  model: { provider: string; name: string; reasoning_effort?: string };
  available: boolean; unavailable_reason?: string;
};
type HarnessDefinition = {
  name: string; system_prompt: string;
  skills: { name: string; content: string; files?: { path: string; content: string }[] }[];
  mcp: Record<string, {
    command: string; args?: string[]; env?: Record<string, string>;
    host_only_tools?: string[];
  }>;
};
type HarnessSnapshot = HarnessDefinition & { id: string; revision: number };
type CreateRunInput = {
  projectId: string; goal: string; url?: string;
  runtimeId: RuntimeId; harnessId: string; idempotencyKey: string;
};
```

`GET /runtimes` and `GET /harnesses` return their named arrays. Harness
create/update/delete use existing POST/PUT/DELETE APIs; accept DELETE 204 and
the actual Go 200 response. Submit `{content,runtime_id,harness_id,idempotency_key}`
to the configured Conversation. Go selects the public model; no hardcoded
`browser-eval` model fallback. URL is optional and validated as HTTP(S) when
present. A generic Harness can run a plain goal without browser fields.

Read `/runs/{id}/trace` as `{spans,attempts,usage,operation_evidence,derived_nodes}`.
Use canonical spans/results for the extension panel. Screenshots are authorized
only by exact `operation_evidence[].artifacts.screenshot_paths` membership and
the selected Run scope. Missing Case or dry-run evidence remains unavailable.
Do not infer success, repair counts or model transcripts from assistant text.
Model trees and usage continue in the native Langfuse Trace UI.

## UI behavior

- Use existing Langfuse Pages Router, tRPC, Shadcn and Tailwind conventions.
- Runtime/Harness selectors display readiness and disable invalid submission.
- Creation idempotency includes normalized goal, URL, Runtime and selected
  Harness revision; retry unchanged work with the same key.
- Preserve and visibly display the Run's persisted `runtime_id`, `harness_id`
  and `harness_revision` after catalog edit/delete.
- A compact Harness manager edits name, system prompt, Skills JSON and MCP JSON.
  Validate data and show action errors; do not install another editor framework.
- Refresh durable intermediate and terminal states through SSE; use bounded
  polling for stale/unavailable streams. Never refresh only `harness.completed`.
- Cancel/rerun show pending/error states, prevent duplicate clicks and preserve
  the failed-rerun idempotency key across an unchanged failed retry.
- Native Trace link stays
  `/project/{langfuseProjectId}/traces/agenttrace-trace-{runId}`. Show export
  syncing separately from execution status. An unavailable exporter does not
  change the Run's terminal status.
- Browser evidence panel and native Trace extension use the current canonical
  trace and authorized artifact proxy. Do not replace `TraceDetailBody`.

## Verification

Test token absence/default, wrong/missing token, health exemption and artifact
protection, route restrictions, foreign Project/Conversation rejection, partial
configuration and symlink containment. Test exporter project filtering without
mutating canonical facts.
Test BFF wrong project/foreign Run rejection, selected submission, catalogs,
CRUD, canonical evidence and artifact allowlist. Test selectors, stable
idempotency, persisted identity, intermediate state updates, pending/error
behavior and narrow layout.

Use Node 24 and the checkout's pnpm 11.10.0 for Langfuse. Run targeted
server-unit/client tests, typecheck, required lint, production build and client
bundle scan. Do not broadly bootstrap destructive Langfuse server tests.
Go integration tests use `agenttrace_test_*`, never `agenttrace_local`.

Native browser acceptance uses the existing local Langfuse at 3300 with real
SDK/provider execution and actual UI submission. Verify all three Runtime
choices with a generic Harness, real browser evidence on one selected Runtime,
active cancellation, failed-Run rerun and native Trace navigation. Verify
Harness edit/delete does not alter historical identity or rerun snapshots.
Data comes from real UI/API execution; no fabricated acceptance traces.

## Working constraints

User authorized implementation and routine native restarts; no approval pause.
Preserve preexisting uncommitted work and existing native databases/artifacts.
Work on current non-main feature branches; no commits, pushes or merges.
Use native processes only, no Docker. Stop only processes owned by our launchers.
Never print provider, bridge or Langfuse credentials or full secret-bearing argv.
Keep intermediate reports/logs under
`/Users/bytedance/Library/Caches/agenttrace-native/langfuse-first-loop`.
Save final user-facing reports under the AI_Web_Testing workspace.
