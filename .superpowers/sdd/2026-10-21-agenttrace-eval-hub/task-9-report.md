# Task 9 Report: Optional Asynchronous Langfuse Exporter

## Implementation

- Added an optional `agenttrace-exporter` process. Disabled configuration returns
  before creating PostgreSQL, Kafka, or Langfuse clients.
- The exporter consumes only `agenttrace.run.events.v1`, requires exact
  `agenttrace.event.v1` envelopes, validates completion identity/status, and ignores
  valid non-completion events.
- Completion handling loads the committed canonical project, run, conversation,
  ordered messages, and ordered spans from PostgreSQL. It rejects a completion whose
  terminal run transaction is not yet visible.
- AgentTrace conversation/run/span IDs produce stable Langfuse session, trace, and
  observation IDs. Transport event IDs are also deterministic, so a request replay
  reuses the same remote idempotency identities.
- Mapping preserves PostgreSQL span order and parent links and carries inputs,
  outputs, statuses, and timestamps. Configured field names are redacted
  case-insensitively through nested objects and arrays.
- A small `Client` interface isolates mapping from HTTP. The concrete client uses
  plausible Langfuse `POST /api/public/ingestion` batch events with Basic Auth and
  never includes credentials or response bodies in errors.
- Successful events commit through the shared PostgreSQL inbox. Provider failures
  roll back the inbox and return an error, preventing Kafka commit.
- Bounded failures use the shared PostgreSQL consumer retry and outbox primitives.
  The final attempt atomically hands one record to exact
  `agenttrace.run.dlq.v1`; later source replays acknowledge without another export.
- The process opens and migrates PostgreSQL, opens Kafka, runs a local outbox relay,
  retries interrupted Kafka consumption, handles signals, joins goroutines, and
  closes dependencies in reverse order.
- Compose adds the exporter only under profile `langfuse`. Default
  `docker compose up` does not include it. `.env.example` contains blank credential
  fields only.

## TDD Evidence

### RED

Initial exporter tests failed on the missing contracts:

```text
undefined: NewHTTPClient
undefined: HTTPConfig
undefined: Client
undefined: Exporter
undefined: Observation
undefined: Batch
undefined: DLQHandoff
undefined: RetryIncrement
```

Configuration and composition tests failed before their APIs existed:

```text
undefined: LoadExporter
undefined: runConfigured
undefined: config.Exporter
```

The canonical project read test failed before the repository extension:

```text
store.GetProject undefined (type *Store has no field or method GetProject)
```

Self-review added a completion payload regression before tightening validation:

```text
TestExporterRejectsMalformedCompletionPayloadBeforeCanonicalRead
accepted completion payload without a run ID
```

### GREEN

The focused suite covers:

- exact topic/schema rejection and malformed completion rejection;
- valid non-completion acknowledgement;
- recursive object/array redaction;
- committed canonical reads and ordered parented spans;
- stable HTTP ingestion identities under failed delivery and replay;
- transient provider error with no canonical run mutation;
- durable one-time DLQ handoff and replay suppression;
- disabled startup gating, enabled composition, and cancellation join.

Required focused command:

```text
cd backend && GOTOOLCHAIN=go1.24.11 go test \
  ./internal/export/langfuse ./cmd/agenttrace-exporter -v
PASS
```

Required complete verification:

```text
cd backend && GOTOOLCHAIN=go1.24.11 go test ./...
PASS

cd backend && GOTOOLCHAIN=go1.24.11 go vet ./...
PASS

docker compose config --quiet
PASS

git diff --check
PASS
```

Compose profile verification:

```text
docker compose config --services
kafka postgres runtime api web worker redis

docker compose --profile langfuse config --services
kafka postgres exporter redis runtime api web worker
```

## Live PostgreSQL Evidence

The host port was occupied by another local PostgreSQL instance, so the focused test
binaries were compiled for Linux arm64 and run inside healthy Compose PostgreSQL,
matching the Task 8 harness:

```text
TestPostgresExporterFailureHandsOffOnceWithoutCanonicalMutation PASS
TestGetProjectReturnsCanonicalMetadata PASS
```

The exporter integration test verifies:

- failed HTTP ingestion rolls back the PostgreSQL inbox;
- retry count persists durably;
- the run remains `succeeded`;
- the bounded attempt creates one shared-topic DLQ outbox record;
- retry state is removed at handoff;
- replay creates neither another export nor another outbox record.

## Extra Files And Rationale

- `backend/internal/config/config.go` and test: enabled-only provider validation and
  disabled fast path.
- `backend/internal/postgres/api.go` and integration test: minimal canonical
  `GetProject` read needed by the exporter.
- `backend/internal/export/langfuse/postgres_integration_test.go`: live inbox,
  retry, outbox, replay, and canonical-isolation proof.
- `backend/cmd/agenttrace-exporter/main_test.go`: disabled startup and graceful
  consumer cancellation coverage.

No migration was needed because the exporter reuses the existing inbox,
`consumer_retry_counts`, and outbox schema.

## Self-Review

- No exporter code writes run, conversation, project, message, or span state.
- No product read depends on Langfuse.
- Remote IDs, ingestion event IDs, retry IDs, and DLQ IDs are deterministic.
- HTTP and configuration errors omit credential values and provider response bodies.
- Exporter code has no v2 or SQLite dependency.
- Exact event and DLQ topics are constants and cannot be redirected by exporter
  environment overrides.
- No credentials were added to source or environment examples.

## Concern

No live Kafka broker or live Langfuse instance was exercised. Kafka behavior is
covered through the existing `EventBus` contract and process tests; Langfuse HTTP is
covered with `httptest`, including authentication, status failures, deterministic
replay payloads, and ingestion path.

## Fix Round 1

### Findings Addressed

- Pinned the optional exporter Compose image to exact `golang:1.24.11`.
- Added a positive `AGENTTRACE_LANGFUSE_MAX_REQUEST_BYTES` setting with a documented
  1 MiB default. The HTTP client uses a field-by-field bounded JSON encoder, returns
  stable `ErrRequestTooLarge`, and performs no HTTP request when the limit is exceeded.
- Added typed `HTTPError` responses and delivery classification. Network failures,
  HTTP 408, 429, and 5xx retain bounded durable retries. Ordinary 4xx responses use
  the explicit permanent path: one retry row is atomically replaced by the
  deterministic DLQ outbox record on the first delivery, and replay is suppressed.
- Validated canonical span IDs, logical span IDs, run ownership, parent existence,
  self-parenting, and cycles before mapping or HTTP. Invalid topology follows the same
  deterministic permanent DLQ path.

### TDD Evidence

RED request-size contract:

```text
unknown field MaxRequestBytes in struct literal of type HTTPConfig
undefined: ErrRequestTooLarge
```

RED transport taxonomy:

```text
undefined: HTTPError
undefined: IsTransient
```

RED delivery classification after the bounded encoder accepted the canonical batch:

```text
TestExporterPermanentHTTPFailureHandsOffImmediatelyAndSuppressesReplay/Bad_Request
permanent failure handoff: ingest Langfuse batch: Langfuse ingestion returned HTTP 400
TestExporterPermanentHTTPFailureHandsOffImmediatelyAndSuppressesReplay/Unauthorized
permanent failure handoff: ingest Langfuse batch: Langfuse ingestion returned HTTP 401
```

RED canonical graph validation:

```text
TestExporterRejectsInvalidCanonicalSpanGraphBeforeHTTP
HTTP requests = 1, want 0
```

RED configuration:

```text
cfg.Langfuse.MaxRequestBytes undefined (type Langfuse has no field or method MaxRequestBytes)
```

GREEN focused verification:

```text
cd backend && GOTOOLCHAIN=go1.24.11 go test \
  ./internal/export/langfuse ./cmd/agenttrace-exporter ./internal/config -count=1 -v
PASS
```

The focused suite now covers exact bounded-encoder compatibility with Go JSON,
oversize zero-request behavior, typed 400/401/404 versus 408/429/503 responses,
network failure, immediate permanent DLQ handoff, transient durable retry, replay
suppression, and invalid graph zero-request handling.

### Complete Verification

```text
cd backend && GOTOOLCHAIN=go1.24.11 go test ./... -count=1
PASS

cd backend && GOTOOLCHAIN=go1.24.11 go vet ./...
PASS

docker compose config --quiet
PASS

docker compose --profile langfuse config --quiet
PASS

git diff --check
PASS
```

Compose services remain:

```text
default: kafka postgres runtime api web worker redis
langfuse: kafka postgres exporter redis runtime api web worker
```

Live PostgreSQL regression used a Go 1.24.11 Linux arm64 test binary inside the
healthy Compose PostgreSQL container:

```text
TestPostgresExporterFailureHandsOffOnceWithoutCanonicalMutation PASS
TestPostgresExporterPermanentHTTPFailureHandsOffImmediately PASS
```

The pinned exporter image pull was attempted but Docker Hub timed out before resolving
the manifest. Compose validation and all Go 1.24.11 host/container test evidence above
completed independently of that registry outage.

## Fix Round 2

### Findings Addressed

- Replaced the hand-buffered request encoder with a length-only counter. It follows
  the ingestion JSON shape, stops traversal immediately once the bound is exceeded,
  and only calls `json.Marshal` after the exact bound proof succeeds. The request
  bytes sent to Langfuse are therefore standard-library JSON bytes rather than a
  parallel handwritten serialization.
- Added standard JSON counter coverage for typed slices, typed nil slices/maps,
  typed maps, sorted map keys, and HTML-sensitive strings. A hostile trailing
  `json.Marshaler` regression proves an oversized leading value returns
  `ErrRequestTooLarge` without inspecting or marshaling the tail.
- Added `PostgresDeliveryGate`, a session advisory lock keyed by the exporter
  consumer and source event in a distinct delivery namespace. The exporter acquires
  it before every completion inbox, HTTP, retry, and DLQ transition, then releases it
  on the same pooled connection with a background timeout. The process composition
  wires this gate for the production exporter.
- Removed delivery-attempt state from permanent DLQ payloads. The payload has a fixed
  original-event/error schema, so retries cannot change its bytes.
- Sorted canonical span validation and cycle traversal by logical span ID. Cycle
  errors are stable across map iteration, while the exported span order remains the
  canonical PostgreSQL order.

### TDD Evidence

RED request-counter tests:

```text
TestBoundedEncoderCountsStandardJSONValuesExactly
encode bounded request: unsupported Langfuse JSON value map[string]int

TestBoundedEncoderStopsBeforeHostileTailMarshal
encode error = unsupported Langfuse JSON value langfuse.marshalTail,
want ErrRequestTooLarge
```

RED deterministic delivery tests:

```text
TestSpanGraphCycleErrorUsesSortedLogicalID
cycle error = "invalid canonical trace: span parent cycle includes \"logical-2\"",
want "invalid canonical trace: span parent cycle includes \"logical-1\""

TestPermanentFailureDLQPayloadDoesNotDependOnAttempts
permanent DLQ payload changed with attempts: "attempt":1 vs "attempt":8

TestPostgresExporterDeliveryGateSerializesTwoInstances
unknown field DeliveryGate in struct literal of type Exporter
undefined: PostgresDeliveryGate
```

GREEN focused verification:

```text
cd backend && GOTOOLCHAIN=go1.24.11 go test \
  ./internal/export/langfuse ./cmd/agenttrace-exporter -count=1
PASS
```

### Complete Verification

```text
cd backend && GOTOOLCHAIN=go1.24.11 go test ./... -count=1
PASS

cd backend && GOTOOLCHAIN=go1.24.11 go vet ./...
PASS

docker compose config --quiet
docker compose --profile langfuse config --quiet
PASS

git diff --check
PASS
```

Default Compose services remain `kafka postgres api redis runtime web worker`; the
`langfuse` profile adds only `exporter`.

### Live PostgreSQL Verification

The focused Linux arm64 exporter test binary ran inside healthy Compose PostgreSQL:

```text
TestPostgresExporterFailureHandsOffOnceWithoutCanonicalMutation PASS
TestPostgresExporterPermanentHTTPFailureHandsOffImmediately PASS
TestPostgresExporterDeliveryGateSerializesTwoInstances PASS
```

The new two-instance regression pauses the first exporter after its inbox rollback
and before durable permanent-failure handling, starts a second exporter on the same
event, then releases the first. It proves one HTTP request, one DLQ row, zero retry
rows, and successful returns from both handlers.

### Self-Review

- The delivery advisory key is namespaced separately from the retry store advisory
  key, avoiding self-deadlock while preserving delivery-gate-before-inbox/retry order.
- The release path uses a non-cancelable timeout and closes a failed session before
  returning it to the pool, so a failed unlock cannot leak a session lock.
- No ledger files or dispatch/reviewer artifacts were changed.

## Fix Round 3

### Findings Addressed

- Replaced the counter-then-`json.Marshal` request path with one bounded serializer
  used first for exact byte counting and then for the `io.Pipe` request body. The
  outgoing request sets the proven `Content-Length` without retaining a batch-sized
  byte buffer.
- Restricted accepted input values to a closed JSON graph: primitive built-ins,
  `map[string]any`, `[]any`, `[]map[string]any`, validated `json.Number`, and
  validated `json.RawMessage`. Named/custom values, pointers, `[]byte`, and JSON,
  text, or binary marshal-capable values fail before HTTP.
- `json.RawMessage` is parsed with `UseNumber` and emitted through the same
  serializer, rejecting malformed or concatenated JSON while preserving arbitrary
  valid JSON number precision.
- Added direct coverage for canonical byte equivalence, malformed raw JSON,
  unsupported values, oversized hostile tails, and the received `Content-Length`
  and pipe body bytes.

### TDD Evidence

The pre-repair focused package test failed at compilation because the old tests
still called the removed buffered encoder and the pipe was passed directly where
the serializer adapter was required:

```text
cannot use bodyWriter (*io.PipeWriter) as ingestionJSONWriter
undefined: encodeIngestionRequest
```

After replacing those expectations with serializer/count/pipe tests:

```text
cd backend && GOTOOLCHAIN=go1.24.11 go test ./internal/export/langfuse -count=1 -v
PASS
```

### Complete Verification

```text
cd backend && GOTOOLCHAIN=go1.24.11 go test ./... -count=1
PASS

cd backend && GOTOOLCHAIN=go1.24.11 go vet ./...
PASS

docker compose config --quiet
docker compose --profile langfuse config --quiet
PASS

git diff --check
PASS
```

### Concern

The focused package suite skips PostgreSQL integration tests unless
`AGENTTRACE_TEST_POSTGRES_DSN` is configured. This repair does not require a
database schema or Compose runtime change; HTTP streaming is covered with
`httptest`, not a live Langfuse service.

## Fix Round 4

### Findings Addressed

- The bounded counter now proves object-key allocation is within the configured
  request limit before collecting or sorting keys. Its first map pass computes
  exact escaped-key, colon, delimiter, and brace minimum bytes without building
  a key slice; it stops as soon as that minimum exceeds the remaining budget.
  Only then does the serializer allocate and sort keys. The key slice, sort
  workspace, decoder input, container count, and maximum nesting are therefore
  bounded by `AGENTTRACE_LANGFUSE_MAX_REQUEST_BYTES` (with a 128-level nesting
  ceiling), rather than hostile input cardinality.
- `json.RawMessage` now rejects source input larger than the current remaining
  request budget before constructing a JSON decoder. This prevents a huge
  escaped token from allocating its decoded Go string before the request-size
  error. Plain escaped strings still use the streaming encoder with no
  batch-sized output allocation.
- `Ingest` closes the `io.PipeReader` and joins the producer as soon as
  `http.Client.Do` resolves, including early 2xx/4xx responses and cancellation.
  A reader-close `io.ErrClosedPipe` is expected after an early response and
  does not hide that response; a canceled context returns `context.Canceled`.
  The buffered producer result is always joined, so no producer goroutine is
  left blocked on an unread request body.

### TDD Evidence

RED focused command:

```text
cd backend && go test ./internal/export/langfuse -run \
  'TestIngestionSerializerRejectsHugeMapBeforeSortingKeys|TestIngestionSerializerRejectsHugeEscapedRawMessageBeforeDecoding|TestIngestionSerializerRejectsHugeEscapedString|TestHTTPClientReturnsAfterEarlyResponseWithoutConsumingBody|TestHTTPClientCancelsWhenEarlyResponseLeavesWriterBlocked' \
  -count=1 -v

undefined: sortJSONMapKeys
undefined: newRawMessageDecoder
```

The failures identify the missing, instrumentable structural boundaries: map
sort was reachable before a budget proof, and raw decoding was reachable before
the source-length guard.

GREEN focused verification:

```text
cd backend && go test ./internal/export/langfuse -count=1 -v
PASS
```

New coverage proves:

- a 4,096-key map with a 256-byte request budget is rejected before key sorting;
- a huge escaped `RawMessage` with that budget is rejected before decoder
  construction;
- a huge HTML-escaped string rejects with `ErrRequestTooLarge`;
- transports that return 202 or 400 without reading the request body return
  promptly with the expected success or typed HTTP error;
- cancellation after a transport has left the writer blocked returns
  `context.Canceled` promptly.

### Complete Verification

```text
cd backend && go test ./... -count=1
PASS

cd backend && go vet ./...
PASS

docker compose config --quiet
docker compose --profile langfuse config --quiet
PASS

git diff --check
PASS
```

Default Compose services remain `postgres kafka api redis runtime web worker`;
the `langfuse` profile adds only `exporter`.

### Self-Review

- Canonical standard-library JSON bytes and exact `Content-Length` tests remain
  green; the new preflight is only a conservative safety boundary before the
  existing serializer.
- A forced pipe close is ignored only after `Do` has produced a response, so
  actual encoding errors remain delivery failures and request-transport errors
  retain their prior classification.
- Closed JSON type restrictions, hostile-tail behavior, deterministic span
  graph validation, permanent delivery gating, and one-time DLQ behavior remain
  covered by the focused and full suites.
- No ledger, dispatch, reviewer, schema, configuration, or Compose content was
  changed for this round.
