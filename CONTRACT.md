# AgentTrace Eval Hub v3 Contract

PostgreSQL is the only source of truth for projects, conversations, messages,
runs, Trace nodes, operation results, usage, idempotency, outbox, and inbox.
Kafka preserves at-least-once delivery; Redis preserves only expiring live
experience. No API, runtime, worker, or Web client may introduce a local
persistence or legacy session-control fallback.

## v3 Contract Locations

| Contract | Authoritative implementation |
| --- | --- |
| Workspace HTTP, SSE, and health | `backend/internal/api/v3` |
| Durable run, event, and Trace types | `backend/internal/domain`, `backend/internal/event` |
| PostgreSQL transactions and Trace projection | `backend/internal/postgres` |
| Runtime checkpoints and shared SDK host execution | `backend/internal/runtime`, `backend/internal/piharness` |
| SDK-free Runtime/Harness snapshots | `backend/internal/execution`, `pi-browser-harness/src/execution-spec.ts` |
| Server-owned model defaults and safety policy | `backend/internal/config`, project Registry in `backend/internal/postgres` |
| Browser case, observation, and execution types | `backend/internal/contract` |
| Worker HTTP service and Playwright execution | `worker/loop_worker/main.py`, `runner.py` |
| Cross-language browser fixture cases | `fixtures/contract/case_contract.json` |
| Canonical scripted model response | `fixtures/scripts/agenttrace_browser_eval.json` |

The fixture is a compatibility input, not this document's replacement. A case
change updates this contract, Go and Python validation, and fixture coverage
together.

## Workspace, Run, and Delivery

`POST /api/v1/conversations/{id}/messages` accepts `content`, `runtime_id`,
`harness_id` (immutable project version ID) and `idempotency_key`, returning
`202` with stable `message_id` and `run_id`. Agent/model-only submissions are
rejected. Management bridge authentication and explicit Go/LF project, actor
and role scope are mandatory. PostgreSQL atomically resolves reviewed project
versions and an owned ready Environment, stores the immutable specification
and canonical facts, and appends the outbox command. Snapshotless/unfenced new
work fails closed; historical facts remain inspectable.

Kafka carries immutable envelopes on:

- `agenttrace.run.commands.v1` for runtime commands;
- `agenttrace.run.events.v1` for durable events;
- `agenttrace.run.dlq.v1` for terminal delivery failures.
- `agenttrace.environment.commands.v1` for managed lifecycle delivery.

Native startup validates at least eight partitions for all four topics and
defaults to four runtime command workers. It refuses undersized existing topics
without altering metadata, offsets or partitions.

Every consumer deduplicates by `event_id`. The runtime reloads its checkpoint
from PostgreSQL after delivery or restart; a duplicate delivery must not create
a second persisted message, span, operation result, or terminal transition.
`GET /api/v1/runs/{id}/events` replays durable PostgreSQL events and may use
Redis only to reduce live-update latency.

The production runtime owns one long-lived session host subprocess. Each
execution attempt selects an independent Pi, Claude Code or Codex session.
The host routes `pi-browser-harness.v1` events by Run and Attempt and
accepts separate run, cancel and shutdown commands over JSONL. A concurrency
slot remains occupied until `harness.released` confirms awaited resource cleanup.

Harness definitions supply prompts, skills, allowed tools, MCP descriptors and
an objective completion policy independently of the Pi SDK and Langfuse. The
reviewed Pi browser harness loads snapshot-selected browser-case-authoring and
requires the latest complete browser replay to pass. A selected Harness reports
its required `succeeded` or `failed` verdict in `harness.settled`; missing verdict
does not select a browser-evidence fallback. Durable cancellation overrides the
verdict. Assistant prose alone does not determine success.

## Runtime and Harness catalog

`GET /api/v1/runtimes` returns `runtimes` containing `id`, `name`, public
`model`, `available`, and optional `unavailable_reason`. Availability describes
local configuration and executable readiness; external provider reachability
is checked by actual execution.

Only project-scoped Registry routes publish/select immutable Harness and Skill
versions. Global mutable Harness CRUD and agent discovery are retired.
Snapshots contain selected identities, digests, system prompt, Skills and MCP.
Skill files have scoped relative paths. MCP descriptors
accept only reviewed deployed `capability`, validated `config` and scoped
`secret_refs`; arbitrary command/args/env and `env:VARIABLE_NAME` are rejected.

Management uses project-scoped immutable Skill and Harness versions,
system-owned MCP versions and project bindings. All requests authenticate the
server bridge and explicit project, Langfuse project, actor and role. Conversation
and Environment ownership are checked at the API and provider boundaries.
See [management wire contracts](integration/langfuse-management/CONTRACTS.md)
and [deployment setup](docs/management-runtime-setup.md).

Message submission accepts `runtime_id` and `harness_id` together. The server
selects the model and persists the copied `execution` snapshot in the Run and
queued command atomically. Management idempotency hashes the requested selection
before resolution so a replay returns the original Run despite later registry
changes. Replay is resolved before readiness checks and preserves the existing
managed hash serialization and response identities.
Run responses include a sanitized immutable snapshot with selected Runtime,
version identities and digests. Rerun copies the source snapshot and uses the
current ready Environment generation after explicit review or reset.

The official SDK adapters are Pi, Claude Agent SDK `query`, and Codex SDK
`startThread/runStreamed`. The `pi.*` event names are normalized compatibility
names for all three adapters. Model completion precedes its usage records.
Claude query-level usage and Codex turn-level usage are labeled as such; the
platform does not infer individual call inputs/times from cumulative results.
Claude cumulative query/model usage has distinct record identities and belongs
to the Run root. Langfuse exports it as Trace/root metadata rather than assigning
the cumulative tokens to an individual GENERATION.

Codex's optional `AGENTTRACE_CODEX_TOOL_FORMAT=flat-functions` converts only the
selected MCP namespace declarations and callable names at the Responses
provider boundary. It preserves arguments, tool results, usage and actual SDK
execution. The default is `native`; no adapter silently substitutes another
runtime on provider failure.

## Browser Case

A case is the only executable browser artifact:

```json
{
  "case_version": "loop.case.v1",
  "name": "Add a product and verify the cart",
  "goal": "Original user goal",
  "base_url": "https://example.test",
  "steps": []
}
```

`steps` is non-empty and its first action is `goto`. The case is authored
through the browser-case-authoring skill and browser MCP tools exposed by the
selected SDK adapter. The deterministic case
builder grounds targets, validates expectations, and runs a fresh-context dry
run before `finish_case` can settle a passing evaluation. The operation schema
is the server-side input boundary, `backend/internal/contract.Validate`
validates the Go representation, and `worker/loop_worker/contracts.py` is its
Python mirror.

Each step has `index`, `action`, human-readable `intent`, optional `value`,
optional grounded `target`, `preconditions`, `postconditions`, and
`timeout_ms`. The default condition timeout is 10 seconds and the default step
timeout is 20 seconds.

| Action | Value | Target | Conditions |
| --- | --- | --- | --- |
| `goto` | Required absolute HTTP(S) URL | Forbidden | No preconditions; at least one postcondition |
| `click` | Forbidden | Required | At least one precondition and postcondition |
| `input` | Required; may be empty | Required | At least one precondition and postcondition |
| `select` | Required label or value | Required | At least one precondition and postcondition |
| `check`, `uncheck`, `scroll_into_view`, `hover`, `dismiss_dialog` | Forbidden | Required | At least one precondition and postcondition |
| `upload_file` | Required local path | Required | At least one precondition and postcondition |
| `assert_text`, `assert_url` | Required | Forbidden | At least one precondition and postcondition |
| `assert_element`, `assert_attribute`, `assert_count` | Forbidden | Required | At least one precondition and postcondition |

`submit` is valid only for `input`; `true` means press Enter after input. It is
not a substitute for grounding a JavaScript-only submit button.

## Conditions and Validation

Conditions have `type`, `value`, and positive `timeout_ms`.

| Type | Precondition | Postcondition | Meaning |
| --- | --- | --- | --- |
| `url_contains` | Yes | Yes | Current URL contains value |
| `text_visible` | Yes | Yes | Visible page text contains value |
| `text_gone` | Yes | Yes | No visible page text contains value |
| `url_changes` | No | Yes | URL changed during this step |
| `value_equals` | No | Yes | Target value equals value |
| `element_state` | Yes | Yes | Target state is visible, hidden, enabled, disabled, checked, or unchecked |
| `attribute_equals` | No | Yes | Target attribute equals value |
| `count_equals` | No | Yes | Target locator match count equals value |

The validators reject unknown actions, targets, condition types, invalid phase
use, unexpected values, ungrounded targets, non-positive timeouts, and a
non-`goto` first step with stable machine-readable error codes.

## Grounding and Observation

A `click`, `input`, selection, assertion target, or recovery action carries
recorded grounding metadata:

```json
{
  "hint": "Add to cart",
  "locator": {
    "kind": "role",
    "role": "button",
    "name": "Add to cart",
    "exact": true,
    "match_count": 1
  },
  "grounding": {
    "observation_id": "obs_...",
    "page_state_id": "ps_...",
    "candidate_id": "cand_...",
    "page_url": "https://example.test/products"
  }
}
```

Grounding IDs and locator fields are model-supplied grounding metadata. Current
v3 validation checks their structure, required values, and supported locator
kinds; it does not independently prove that an ID came from a prior observation
or that `match_count` was measured on the live page. The worker resolves the
recorded locator during execution and does not invent a replacement.

The shared case format can also represent observations with a browser-session
ID, URL, title, visible elements, structures, blockers, action candidates, and
a relative screenshot path. `browser_session_id` denotes an ephemeral worker
browser context, not a v3 conversation or run. Observation-backed candidate
provenance is not enforced by the current v3 operation boundary.

Blockers are page facts such as dialog, overlay, interstitial, cookie banner,
auth wall, captcha, and loading state. The worker may safely wait, scroll, or
use an already verified close/accept/reject control. It must not bypass auth or
captcha.

## Execution and Evidence

The worker receives:

```json
{"session_id":"run_...","browser_session_id":"bsess_...","project_id":"prj_...","environment_id":"env_...","profile_generation":1,"fencing_token":2,"case":{"case_version":"loop.case.v1"}}
```

Browser open/replay requires complete strict body identity and internal Bearer
authentication. Replay requires the held browser handle and the current project,
Environment, generation and fencing token. No unbound ephemeral fallback is
available. Runtime evidence writes require a current Lease/fencing identity.

It returns an execution result with `passed`, `failed`, or `error`, per-step
status, start time, duration, URLs before and after the action, evaluated
conditions, and structured errors. Error kinds include `target_not_found`,
`condition_unmet`, `step_timeout`, `worker_error`, `case_invalid`, and the
`blocked_by_*` kinds.

Each step's evidence can include a relative screenshot path, browser console
entries, network entries, hit-test facts, and safe recovery attempts. Artifact
paths are relative to the shared artifacts root and are exposed by the v3 API
under authenticated, project-scoped `/artifacts/`; no repository or host
absolute path is contractual. Management and the BFF retain that root URL.

The v3 runtime persists operation output, artifact references, structured
errors, attempts, and spans in PostgreSQL through
`backend/internal/postgres`. `GET /api/v1/runs/{id}/trace` projects the
durable Trace.

## Usage and Model Context

Usage records retain provider-reported prompt, completion, total, reasoning,
and cached tokens. Fresh accounting is:

```text
fresh_prompt_tokens = max(prompt_tokens - cached_tokens, 0)
fresh_total_tokens = fresh_prompt_tokens + completion_tokens
```

Reasoning tokens are already part of completion tokens, and cached tokens are
already part of prompt tokens; neither is added twice. The normalized type is
`backend/internal/usage.Usage`; runtime writes it with the corresponding
durable Trace attempt.

The runtime passes the current Run's latest user goal, public model reference
and reasoning effort to the Harness. The selected official SDK owns the
evolving model context, turns and tool dispatch. `AGENTTRACE_MODEL_PROVIDER` selects Pi's default
real OpenAI-compatible `glm-5.3-flash` or the deterministic scripted fixture.
Credentials remain in Runtime's environment. Each Run explicitly selects its
supported SDK and validated Harness snapshot. The Node process implements only
the shared session-host protocol: no default-Pi, startup-module or disk-Skill
fallback is executable.

Each `pi.message.completed` may carry `input` with the actual transformed
messages and paired `started_at`/`ended_at` UTC call times. Usage preserves
reported total and cache token fields; unreported billed cost is absent.
Missing model input/time remains unknown rather than an empty request or
zero-duration observation.

## Optional Langfuse Export

`backend/cmd/agenttrace-exporter` is an optional Kafka consumer that mirrors
completed v3 runs to Langfuse. Its retry and DLQ contract is implemented in
`backend/internal/export/langfuse`. It never blocks API acceptance, operation
execution, local completion, or product reads.

New producers publish `run.completed` with `{run_id, status, error?}` where
status is `succeeded`, `failed` or `cancelled`. Producers and consumers use only
this canonical completion type; retired completion aliases are not executable.
Historical rows and applied additive migrations remain untouched.
Durable completion and its outbox record are committed atomically.
Kafka Live Notifier writes disposable `run.events.available` Redis wakeups;
SSE always emits persisted events identified by their PostgreSQL sequence.
