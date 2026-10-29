# Runtime and Harness composition

User requirements: platform supports independently detachable Pi, Claude Code,
Codex and Harness; use appropriate design patterns, no unnecessary frameworks.
Harness is managed as skills + MCP + system prompt by this platform.
Langfuse remains an optional observer. Everything runs natively.

## Boundaries

Go coordinates durable Runs, delivery, concurrency, cancellation and evidence.
It never runs a model loop. A shared execution host accepts a selected runtime
and immutable Harness snapshot per Attempt. Adapter implementations translate
this into each official SDK; Strategy dispatch selects one of three adapters.
Harness data is separate from SDK types. PostgreSQL owns Harness records and
Run snapshots. There is no plugin marketplace, remote module installation,
second event authority, or generic orchestration framework.

## Shared contract

Extend `event.RunCommand` and JSONL `RunRequest` with optional `execution`:

```typescript
interface ExecutionSpec {
  runtime_id: "pi" | "claude-code" | "codex";
  harness: HarnessSnapshot;
}
interface HarnessSnapshot {
  id: string;
  name: string;
  revision: number;
  system_prompt: string;
  skills: { name: string; content: string; files?: { path: string; content: string }[] }[];
  mcp: Record<string, {
    command: string;
    args?: string[];
    env?: Record<string, string>;
    host_only_tools?: string[];
  }>;
}
```

Go owns these structs under `internal/execution`; SDK-free TypeScript mirrors
them in `src/execution-spec.ts`. Old requests without execution select the
existing Pi/browser Harness; existing Runs remain readable and rerunnable.
JSONL event wire names remain compatible (`pi.message.completed`, `pi.usage`,
tool events, browser evidence, settled/failed). These denote normalized
platform events for all adapters, not a Pi dependency. Do not mass-rename
packages as a substitute for removing dependency.

Runtime public model identities: Pi uses existing explicit model/provider;
Claude Code uses `anthropic-compatible` and configured Claude model; Codex uses
`openai-responses` and configured Codex model. Runtime credential values are
environment-only, never API, snapshots, reports, or command arguments.

## Platform management

CRUD `/api/v1/harnesses`, revision increment on update, seeded browser Harness
from current system prompt/skill resources and stdio MCP implementation.
GET `/api/v1/runtimes` lists public SDK/model configuration and readiness.
POST message accepts `runtime_id` and `harness_id`, selects server-owned
configuration, and atomically persists the copied Harness in specification and
queued command. Idempotency includes selected execution; reruns preserve
snapshots after Harness update or deletion. Keep legacy Agent API behavior.
Frontend adds Runtime/Harness selectors and a compact Harness editor for the
three requested configuration parts, with create/save/delete; no code editor
framework or duplicate Langfuse page. Display selected combination on a Run.

MCP env values support `{{worker_url}}`, `{{artifact_scope}}`, `{{goal}}` and
`env:VARIABLE_NAME` references. Resolve references only in the execution host.
Skill paths are relative and scoped; prohibit traversal/duplicate names. Every
Attempt gets private temporary resources, deleted after cleanup.

## SDK execution

Official SDKs pinned: `@anthropic-ai/claude-agent-sdk` 0.3.311 and
`@openai/codex-sdk` 0.165.0; MCP dependency 1.29.0 required by Claude SDK.
Pi keeps its existing SDK adapter. Lazy imports allow running one adapter
without constructing the others.

Claude uses SDK `query` with custom system prompt, isolated settings, selected
MCP only and AbortController. Codex uses SDK `startThread/runStreamed` with
isolated CODEX_HOME, developer instructions, selected MCP config and signal.
Never silently substitute Pi or scripted execution for SDK failure.
Record SDK-provided model/usage and timing at the granularity each SDK exposes;
do not invent individual model-call times or input transcripts.

An attempt-local MCP helper for Claude/Codex connects selected stdio servers,
exposes their model-visible tools via loopback Streamable HTTP, and retains
host-only cleanup access. It emits canonical tool/browser evidence from actual
MCP results. The helper is an adapter implementation detail, not a new service.
Cleanup calls resource-release tools before tearing down transport, joins SDK
work and emits release only after cleanup. Failure remains explicit.
All runtimes share browser completion policy when browser evidence is selected;
generic Harnesses settle from SDK completion, preserving runtime failure.

Native provider acceptance found that the available GLM Responses endpoint
ignores namespace tools, while identical flat function declarations produce
actual tool calls. Codex supports an explicit, optional
`AGENTTRACE_CODEX_TOOL_FORMAT=flat-functions` provider adapter inside each
Attempt. It translates namespace declarations and function-call names in
requests and streaming responses, preserving tool arguments, results, usage
and real model output. The official Codex SDK/native CLI and selected MCP
remain the execution path. Default `native` leaves the provider wire format
unchanged; no silent retry, alternate SDK or synthesized execution is allowed.
The loopback listener retains private credentials, has attempt-local mappings,
forwards provider failures and cancellation, and closes after SDK exit before
resource release. It adds no deployed service or event authority.

## Acceptance

Verify three official SDKs with real model calls using available local/provider
credentials, same browser Harness across runtimes, and a second generic Harness
across runtimes. Verify task selection/snapshot, edits, rerun, real tools,
evidence, cancellation/cleanup, concurrent isolation and optional Langfuse.
Use honest readiness/errors if an external credential/provider is unavailable;
continue independent work and do not label mocks as live acceptance.
All existing targeted tests/builds and native acceptance must continue passing.

User has already authorized implementation in this session. Routine choices
and local credential reuse do not require another approval gate.
