# Architecture

AgentTrace Eval Hub v3 is one durable browser-evaluation workflow. The React
workspace presents projects, conversations, and run traces in three columns.
The API stores requests in PostgreSQL and appends outbox rows atomically. Kafka
delivers commands to the runtime, which uses the scripted model fixture by
default and calls the Playwright worker. The worker evaluates the local fixture
site and returns evidence that becomes durable run events and trace details.

PostgreSQL preserves facts; Kafka preserves delivery; Redis preserves live experience.

## Canonical browser scenario

`integration/v3/canonical_scenario.py` creates a project and conversation,
sends the deterministic browser-evaluation request, follows the run SSE stream
until it succeeds, and verifies PostgreSQL facts and browser evidence. The
scripted model drives the local storefront fixture: filter `alpha`, open the
detail page, add two items, and verify the cart summary. It runs entirely
against Compose services and does not need an external model key.

The retained browser-case interoperability contract remains
`fixtures/contract/case_contract.json`. It is consumed by the worker contract
tests; it is not a second control-plane architecture.

## Runtime topology

```mermaid
flowchart LR
    UI[React three-column workspace] --> API[AgentTrace API]
    API --> PG[(PostgreSQL facts)]
    API --> OUTBOX[PostgreSQL outbox]
    OUTBOX --> KAFKA[(Kafka delivery)]
    KAFKA --> RUNTIME[AgentTrace runtime]
    RUNTIME --> WORKER[Playwright worker]
    WORKER --> FIXTURE[Local storefront fixture]
    WORKER --> RUNTIME
    RUNTIME --> KAFKA
    KAFKA --> API
    API <--> REDIS[(Redis live updates)]
    API --> UI
    KAFKA -. completed events .-> EXPORTER[Optional async Langfuse exporter]
    EXPORTER -. ingestion .-> LANGFUSE[Langfuse]
```

PostgreSQL owns projects, conversations, messages, runs, events, traces,
operations, idempotency, and pending outbox rows. Kafka topics carry immutable
command and event envelopes and have a dedicated DLQ. Redis is not a source of
truth: when unavailable, the API's SSE stream uses `postgres_poll`. Langfuse
is an optional profile and asynchronously observes completed work; it is not
on the request or runtime critical path.

## Failed-Run rerun lineage

A Run records its originating user message in `source_message_id`. A user rerun
is a new Run with the same `source_message_id` and an immediate parent recorded
in `retry_of_run_id`; it copies the failed Run's immutable specification,
receives a new Run ID, and starts at checkpoint zero. The source Run and all of
its durable evidence remain unchanged. The new execution persists its own
spans, usage, operation results, events, and artifacts with identities that do
not overlap the source.

This is a user-requested execution, not Kafka/DLQ delivery recovery. Kafka
redelivery and DLQ handling retry durable transport or consumer work without
creating a Run. A user rerun is eligible only when its direct source Run is
`failed`, uses an idempotency key scoped to that source, and may repeat the
original external effects.
