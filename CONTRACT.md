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
| Runtime checkpoints and model/operation loop | `backend/internal/runtime` |
| Server-owned operation schemas and callback validation | `backend/internal/operation`, `backend/internal/agentprofile` |
| Browser case, observation, and execution types | `backend/internal/contract` |
| Worker HTTP service and Playwright execution | `worker/loop_worker/main.py`, `runner.py` |
| Cross-language browser fixture cases | `fixtures/contract/case_contract.json` |
| Canonical scripted model response | `fixtures/scripts/agenttrace_browser_eval.json` |

The fixture is a compatibility input, not this document's replacement. A case
change updates this contract, Go and Python validation, and fixture coverage
together.

## Workspace, Run, and Delivery

`POST /api/v1/conversations/{id}/messages` accepts a message with an
idempotency key and returns `202` with stable `message_id` and `run_id`. Before
returning, PostgreSQL atomically records canonical facts and an outbox envelope.

Kafka carries immutable envelopes on:

- `agenttrace.run.commands.v1` for runtime commands;
- `agenttrace.run.events.v1` for durable events;
- `agenttrace.run.dlq.v1` for terminal delivery failures.

Every consumer deduplicates by `event_id`. The runtime reloads its checkpoint
from PostgreSQL after delivery or restart; a duplicate delivery must not create
a second persisted message, span, operation result, or terminal transition.
`GET /api/v1/runs/{id}/events` replays durable PostgreSQL events and may use
Redis only to reduce live-update latency.

The browser operation name is `browser_execute_case`. Its callback URL is
selected from the server-owned browser agent profile, never from browser input.
The operation request includes a stable run ID, span ID, and operation ID; the
worker treats the operation ID as its idempotency key.

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

`steps` is non-empty and its first action is `goto`. The selected model supplies
the `browser_execute_case` tool arguments, including the complete case JSON.
The scripted model fixture supplies the complete browser case JSON
deterministically in the default local stack. The operation schema is the
server-side input boundary, `backend/internal/contract.Validate` validates the
Go representation, and `worker/loop_worker/contracts.py` is its Python mirror.

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
{"session_id":"artifact_scope","case":{"case_version":"loop.case.v1"}}
```

It returns an execution result with `passed`, `failed`, or `error`, per-step
status, start time, duration, URLs before and after the action, evaluated
conditions, and structured errors. Error kinds include `target_not_found`,
`condition_unmet`, `step_timeout`, `worker_error`, `case_invalid`, and the
`blocked_by_*` kinds.

Each step's evidence can include a relative screenshot path, browser console
entries, network entries, hit-test facts, and safe recovery attempts. Artifact
paths are relative to the shared artifacts root and are exposed by the v3 API
under `/artifacts/`; no repository or host absolute path is contractual.

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

The runtime presents the durable conversation and server-owned operation
schemas to the selected model. The default scripted model returns the
deterministic canonical operation request from
`fixtures/scripts/agenttrace_browser_eval.json`; an OpenAI-compatible model may
instead produce schema-valid operation arguments at runtime.

## Optional Langfuse Export

`backend/cmd/agenttrace-exporter` is an optional Kafka consumer that mirrors
completed v3 runs to Langfuse. Its retry and DLQ contract is implemented in
`backend/internal/export/langfuse`. It never blocks API acceptance, operation
execution, local completion, or product reads.
