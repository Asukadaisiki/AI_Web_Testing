# Task 5 Report: Recoverable Redis Live Streams

## Implementation

- Added `live.LiveEvent` and `live.LiveStore`, plus an in-memory test store.
- Added a Redis live-store adapter using exactly:
  - `run:{id}:live` via `XADD MAXLEN ~ 2000`, refreshed with a 24-hour expiry;
  - `run:{id}:lease` via `SET owner EX <ttl>` (30 seconds when omitted);
  - `run:{id}:cancel` via `SET 1 EX <ttl>` (24 hours when omitted).
- Added `stream.Service` and a small `PostgreSQLReader`:
  - replays `run_events` in ascending `seq` order for `seq > Last-Event-ID`;
  - emits durable events with `ID=strconv.FormatInt(seq, 10)`;
  - tails Redis after replay, drops live durable notices at or below the replayed sequence, and emits token deltas without an ID;
  - polls PostgreSQL at the required one-second production default after a Redis read error and retries Redis after each poll;
  - polls PostgreSQL after idle live reads too, so a missing/flushed Redis stream cannot erase durable visibility;
  - honors cancellation for service polling and the Redis adapter's bounded reads.
- No HTTP composition, API routes, or runtime wiring were changed.

## RED / GREEN Evidence

### RED

The required first test command was run before the packages existed:

```text
GOTOOLCHAIN=go1.24.11 go test ./internal/live/... ./internal/stream -v
FAIL ./internal/live/...: lstat ./internal/live/: no such file or directory
FAIL internal/stream: no required module provides package .../internal/live
```

The Redis integration test was then added before the adapter and failed as intended:

```text
undefined: NewClient
undefined: liveKey
undefined: leaseKey
undefined: cancelKey
```

During adapter validation, the pinned go-redis API rejected the guessed field
name `MaxLenApprox`; local `go doc` established the correct
`MaxLen: 2000, Approx: true` representation for `XADD MAXLEN ~ 2000`.

The local go-redis source also established the cancellation root cause:
`XReadArgs{Block: 0}` serializes as `BLOCK 0` (infinite), and default clients
ignore request contexts for I/O. `Client.Read` therefore uses non-blocking
`XREAD` for zero-duration reads and at-most-100ms blocking slices for positive
durations, checking cancellation between slices. The pre-existing integration
test `TestClientTreatsFlushedStreamAsEmptyAndCancelsBlockingRead` is the
regression case.

### GREEN

Focused command:

```text
GOTOOLCHAIN=go1.24.11 go test ./internal/live/... ./internal/stream -v
PASS internal/live/redis (Redis environment-gated tests skipped without AGENTTRACE_TEST_REDIS_ADDR)
PASS internal/stream
```

Covered green service behavior:

- durable-first replay from a `Last-Event-ID`;
- Redis read outage followed by PostgreSQL fallback;
- Redis recovery after fallback;
- absent live stream without treating the run as absent;
- durable sequence dedupe and ID-less token deltas;
- service cancellation during a blocking read.

## Durable Replay, Redis Outage, and FLUSHDB

- `TestReplayStartsWithDurableEvents` verifies ordered durable IDs and payloads.
- `TestReplayFallsBackWhenRedisIsUnavailable` verifies a durable event added
  after a Redis error is delivered by PostgreSQL polling.
- `TestFallbackResumesLiveReadsWhenRedisRecovers` verifies the next successful
  Redis read resumes token delivery.
- `TestLiveEventsAfterDurableSequenceAreNotDuplicated` verifies the sequence
  carried by a Redis durable notice is deduplicated while a token delta remains
  ID-less.
- `TestPostgreSQLReplaySurvivesRedisFlushDB` provisions PostgreSQL run events,
  writes a Redis notice, executes `FLUSHDB`, and verifies the PostgreSQL replay
  remains available. It is environment-gated on both test DSNs.
- `TestClientTreatsFlushedStreamAsEmptyAndCancelsBlockingRead` verifies a
  flushed Redis stream is an empty live tail, not an error or missing run.

## Redis Integration Limitation

The required `docker compose up -d redis postgres` was attempted. PostgreSQL
was already healthy and was left running. Redis could not be pulled because
Docker Hub timed out:

```text
failed to resolve reference "docker.io/library/redis:7-alpine":
Head "https://registry-1.docker.io/v2/library/redis/manifests/7-alpine":
dial tcp 54.157.162.28:443: i/o timeout
```

Consequently, Redis integration and the cross-store FLUSHDB test are present
but skipped in this environment. They run when
`AGENTTRACE_TEST_REDIS_ADDR` is supplied; because Redis is Compose-network
only, the intended execution is a Go test binary run in `v3-postgres-1` with
`AGENTTRACE_TEST_REDIS_ADDR=redis:6379` and the existing `agenttrace` DSN.
The unrelated host PostgreSQL was not stopped or used.

## Go 1.24 Verification

```text
GOTOOLCHAIN=go1.24.11 go version
go version go1.24.11 darwin/arm64

GOTOOLCHAIN=go1.24.11 go test ./...
PASS
```

`GOTOOLCHAIN=go1.24.11 go vet ./...` and `git diff --check` also exited zero.
Task commit: `d8899c5` (`feat: add recoverable Redis live streams`).

## Files

- `backend/internal/live/live.go`
- `backend/internal/live/memory.go`
- `backend/internal/live/redis/client.go`
- `backend/internal/live/redis/client_integration_test.go`
- `backend/internal/stream/service.go`
- `backend/internal/stream/service_test.go`
- `backend/internal/stream/service_integration_test.go`

## Self-Review

- PostgreSQL remains authoritative; neither a Redis append failure nor a Redis
  flush changes durable `run_events`.
- Only `Sequence > 0` live notifications receive an SSE ID. Token deltas are
  never replay cursors.
- The PostgreSQL reader orders SQL results by `seq ASC`.
- Redis reads use the last Redis stream ID independently from the SSE durable
  sequence cursor.
- Redis failures never terminate a normally configured subscription; only a
  durable-reader failure is terminal.
- The change is confined to Task 5 packages and does not wire Task 8 HTTP.

## Concerns

- Live Redis/FLUSHDB verification is blocked only by the Docker Hub timeout
  above. Re-run the environment-gated integration package after `redis:7-alpine`
  is available locally.
- The Redis adapter checks cancellation every 100ms because the pinned
  go-redis default intentionally ignores contexts for socket I/O; this avoids
  indefinitely retained `XREAD` calls without requiring all callers to
  construct Redis clients with non-default options.

## Fix round 1

### Implementation

- Redis durable notifications no longer emit an SSE event or advance the
  PostgreSQL/SSE sequence cursor. Each notification instead triggers
  authoritative PostgreSQL catch-up, preserving ordered durable replay even
  when Redis first reports a later sequence or reports notices out of order.
- Redis append now sends one Lua `EVAL` server-side unit containing `XADD
  MAXLEN ~ 2000` and `EXPIRE 24h`, eliminating the gap where a successful
  append could outlive its expiry.
- Added deterministic regression coverage for Redis notices in the order
  sequence 3 then 2 after PostgreSQL has replayed sequence 1. PostgreSQL
  emits sequences 2 then 3 exactly once. Added a focused Redis client test
  that intercepts the server command and requires one `EVAL` containing the
  capped `XADD` and expiry.

### RED

Before production changes:

```text
$ GOTOOLCHAIN=go1.24.11 go test ./internal/live/redis ./internal/stream -run 'TestAppendUsesOneServerSideCommandForAddAndExpiry|TestDurableRedisNoticesCatchUpPostgresWithoutSkippingOrDuplicating' -v
=== RUN   TestAppendUsesOneServerSideCommandForAddAndExpiry
    client_test.go:37: server command = [xadd run:run-atomic:live maxlen ~ 2000 * event {"sequence":9,"type":"span.completed","data":{"span":"plan"}}], want EVAL for atomic append and expiry
--- FAIL: TestAppendUsesOneServerSideCommandForAddAndExpiry (0.00s)
FAIL
=== RUN   TestDurableRedisNoticesCatchUpPostgresWithoutSkippingOrDuplicating
    service_test.go:160: stream event = stream.StreamEvent{ID:"3", Type:"run.finished", ...}, want stream.StreamEvent{ID:"2", Type:"span.completed", ...}
--- FAIL: TestDurableRedisNoticesCatchUpPostgresWithoutSkippingOrDuplicating (0.00s)
FAIL
```

### GREEN

```text
$ GOTOOLCHAIN=go1.24.11 go test ./internal/live/... ./internal/stream -v
?    github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live [no test files]
=== RUN   TestClientStoresCappedExpiringLiveEventsAndHints
    client_integration_test.go:15: AGENTTRACE_TEST_REDIS_ADDR is not configured
--- SKIP: TestClientStoresCappedExpiringLiveEventsAndHints (0.00s)
=== RUN   TestClientTreatsFlushedStreamAsEmptyAndCancelsBlockingRead
    client_integration_test.go:76: AGENTTRACE_TEST_REDIS_ADDR is not configured
--- SKIP: TestClientTreatsFlushedStreamAsEmptyAndCancelsBlockingRead (0.00s)
=== RUN   TestAppendUsesOneServerSideCommandForAddAndExpiry
--- PASS: TestAppendUsesOneServerSideCommandForAddAndExpiry (0.00s)
PASS
ok   github.com/Asukadaisiki/AI_Web_Testing/backend/internal/live/redis 0.697s
=== RUN   TestPostgreSQLReplaySurvivesRedisFlushDB
    service_integration_test.go:21: AGENTTRACE_TEST_POSTGRES_DSN and AGENTTRACE_TEST_REDIS_ADDR are required
--- SKIP: TestPostgreSQLReplaySurvivesRedisFlushDB (0.00s)
=== RUN   TestReplayStartsWithDurableEvents
--- PASS: TestReplayStartsWithDurableEvents (0.03s)
=== RUN   TestReplayFallsBackWhenRedisIsUnavailable
--- PASS: TestReplayFallsBackWhenRedisIsUnavailable (0.04s)
=== RUN   TestFallbackResumesLiveReadsWhenRedisRecovers
--- PASS: TestFallbackResumesLiveReadsWhenRedisRecovers (0.04s)
=== RUN   TestMissingRedisStreamDoesNotMeanMissingRun
--- PASS: TestMissingRedisStreamDoesNotMeanMissingRun (0.03s)
=== RUN   TestLiveEventsAfterDurableSequenceAreNotDuplicated
--- PASS: TestLiveEventsAfterDurableSequenceAreNotDuplicated (0.03s)
=== RUN   TestDurableRedisNoticesCatchUpPostgresWithoutSkippingOrDuplicating
--- PASS: TestDurableRedisNoticesCatchUpPostgresWithoutSkippingOrDuplicating (0.03s)
=== RUN   TestSubscribeStopsBlockingReadWhenContextIsCanceled
--- PASS: TestSubscribeStopsBlockingReadWhenContextIsCanceled (0.00s)
PASS
ok   github.com/Asukadaisiki/AI_Web_Testing/backend/internal/stream 1.370s
```

```text
$ GOTOOLCHAIN=go1.24.11 go test ./...
PASS

$ GOTOOLCHAIN=go1.24.11 go vet ./...
PASS (exit 0)

$ git diff --check
PASS (exit 0)
```

### Compose Redis / FLUSHDB retry

```text
$ docker compose up -d redis postgres
Image redis:7-alpine Error failed to resolve reference "docker.io/library/redis:7-alpine":
failed to do request: Head "https://registry-1.docker.io/v2/library/redis/manifests/7-alpine":
dial tcp 54.235.133.183:443: i/o timeout
```

The existing Redis and cross-store `FLUSHDB` tests remain environment-gated
and were skipped only because Redis could not be pulled. PostgreSQL remained
healthy. Code/test commit: `559a454` (`fix: preserve durable replay ordering`).
