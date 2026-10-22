# AgentTrace Eval Hub v3 contract

This document describes the current cross-process v3 contract. PostgreSQL is
the source of truth. API, runtime, worker, and Web clients must not create
alternate local persistence, delivery, or legacy session-control fallbacks.

## HTTP workspace contract

The API implementation and request/response tests live in
`backend/internal/api/v3`. The Web client is `web/src/api.ts` and the
three-column workspace is `web/src/features/workspace`.

| Endpoint | Contract |
| --- | --- |
| `GET/POST /api/v1/projects` | List or create durable projects |
| `GET/POST /api/v1/projects/{id}/conversations` | List or create conversations |
| `GET /api/v1/conversations/{id}` | Conversation, messages, and runs |
| `POST /api/v1/conversations/{id}/messages` | Accept a message and queue a run; requires an idempotency key |
| `GET /api/v1/runs/{id}` | Durable run state |
| `GET /api/v1/runs/{id}/trace` | Trace tree, operations, and browser evidence |
| `GET /api/v1/runs/{id}/events` | SSE replay of durable run events |
| `POST /api/v1/runs/{id}/cancel` | Persist a cancellation request |
| `POST /api/v1/runs/{id}/responses` | Persist an approval/input response and enqueue continuation |
| `GET /api/v1/agents` | Available agent profiles |
| `GET /api/v1/health` | Dependency health and pending outbox count |

Errors have shape:

```json
{"error":{"code":"machine_readable_code","message":"human-readable detail"}}
```

## Run and event contract

The Go types are in `backend/internal/domain` and
`backend/internal/event`. PostgreSQL persistence, migrations, and transactional
outbox writes are in `backend/internal/postgres`; publishing is in
`backend/internal/outbox`.

A message is accepted as `202` with stable `message_id`, `run_id`, and
`queued` status. The transaction stores canonical facts and an outbox envelope
before it returns. Kafka uses:

- `agenttrace.run.commands.v1` for runtime commands;
- `agenttrace.run.events.v1` for durable run events;
- `agenttrace.run.dlq.v1` for failed delivery handling.

The runtime implementation is `backend/internal/runtime` and its process entry
point is `backend/cmd/agenttrace-runtime`. It consumes idempotently, checkpoints
durable state, and emits terminal run events. `GET /api/v1/runs/{id}/events`
replays PostgreSQL events and may use Redis for low-latency notification; Redis
is never canonical.

## Operation contract

Operation schemas, dispatch, and HTTP callback behavior are in
`backend/internal/operation`. The browser-evaluation agent profile is
`backend/internal/agentprofile`. The runtime invokes the worker client in
`backend/internal/worker`; the Python service is
`worker/loop_worker/main.py`.

Browser artifacts are stored on the shared artifacts volume and served by the
API under `/artifacts/`. A trace references durable operation results and
artifact paths rather than embedding arbitrary worker state in the UI.

## Browser case contract

`fixtures/contract/case_contract.json` remains the compatibility contract for
browser case fixtures. Its Go mirror is `backend/internal/contract`; its
Python mirror is `worker/loop_worker/contracts.py`. Changes to that fixture
require matching validation tests on both sides. It describes worker input,
not the removed v2 session/run/report UI.

## Optional Langfuse export

`backend/cmd/agenttrace-exporter` consumes completed events asynchronously when
the `langfuse` Compose profile is enabled. Its transport and retry contract is
in `backend/internal/export/langfuse`. It must not block API acceptance or
local runtime completion. Provider credentials stay in local environment
variables and are never part of this contract or committed configuration.
