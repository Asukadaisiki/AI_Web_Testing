# Management Infrastructure Faults, 2026-10-28

Status: **passed**. Duration: 30.964 seconds.

## Scope

Isolated Phase 3 component integration. Real Redis, Kafka and PostgreSQL;
real production HTTP SSE, stream, outbox relay, Kafka consumer and inbox deduplicator.
Synthetic SQL/event fixtures only: no SDK, LLM, browser, UI or full management-runtime workload.
Does not claim completion of phases 0-4 or the parent's separate acceptance work.

## Results

### TestRedisOutageSSEReplayAndBounds

Expected: Redis loss preserves SSE sequence/replay, bounded polling/capacity/slow delivery.
Observed: owned Redis killed; HTTP delivered IDs 1,2 then Last-Event-ID=1 replayed 2,3; bounds held.

```json
{
  "capacity_rejection_form": "EOF",
  "last_event_id": "1",
  "outage_delivery_ms": 514,
  "poll_window_durable_reads": 1,
  "poll_window_ms": 650,
  "replayed_ids": [
    2,
    3
  ],
  "slow_timeout_ms": 150,
  "stats": {
    "active_connections": 0,
    "rejected_connections": 1,
    "redis_fallback_reads": 3,
    "durable_reads": 9,
    "slow_consumers": 1
  },
  "subscription_cap": 2
}
```

### TestKafkaPauseRetainsOutboxAndRecovers

Expected: paused broker retains outbox; real lease expiry and broker recovery preserve one canonical fact.
Observed: owned KRaft Java process SIGSTOP verified; pending retained during pause; deadline-expired relay failed after SIGCONT; lease recovery and broker duplicate deduplicated.

```json
{
  "attempt_count_during_pause": 0,
  "broker_offset": 2,
  "broker_partition": 0,
  "caller_deadline_ms": 500,
  "canonical_facts": 1,
  "explicit_duplicate_observed": true,
  "explicit_duplicate_offset": 3,
  "lease_held_during_pause": true,
  "lease_wait_ms": 344,
  "pause_observation_ms": 750,
  "pause_publish_ms": 755,
  "pending_during_pause": true,
  "processed_inbox": 1,
  "recovery_and_delivery_ms": 865,
  "returned_before_resume": false
}
```

### TestPostgresSessionFailureRollsBackAndDeduplicates

Expected: loss of one owned PG backend fails closed with zero partial facts; replay and duplicate apply once.
Observed: owned tx wrote uncommitted event then guarded pg_terminate_backend; inbox+event rolled back; retry+duplicate persisted exactly once.

```json
{
  "canonical_facts_after_recovery": 1,
  "elapsed_ms": 30,
  "facts_after_fault": 0,
  "inbox_after_fault": 0,
  "processed_inbox_after_recovery": 1,
  "recovery_handler_calls": 1,
  "rejected_inbox": 0,
  "terminated_owned_pid": 87601,
  "termination_confirmed": true
}
```

## Negative Controls

Temporary Go overlays only; production workspace files were not modified.

- `redis_fallback_disabled`: caught; infrastructure_integration_test.go:287: expected durable SSE ID 2, got {ID: Type: Data:} open=false
- `outbox_ack_without_broker`: caught; infrastructure_integration_test.go:467: broker outage incorrectly acknowledged/lost durable outbox event
- `inbox_duplicate_guard_disabled`: caught; infrastructure_integration_test.go:621: handle inbox event "run_postgres_1793195208509855000_fault": 错误: 重复键违反唯一约束"run_events_pkey" (SQLSTATE 23505)
- `inbox_handler_failure_ignored`: caught; infrastructure_integration_test.go:635: canonical handler failure was incorrectly acknowledged

## Isolation And Limits

- No shared Redis6379, PG5432, Kafka9192, API8180, Worker8110 or Langfuse3300 process is signalled.
- Private Redis/Kafka listen on dynamically selected loopback ports with temporary state.
- PostgreSQL fault terminates only the exact transaction backend in the exclusively created test database.
- PG server-wide outage, Kafka cluster partition/rebalance, SDK/runtime/LLM/UI and scale-load behavior are not claimed.
- Redis timings use test client timeouts and 100ms polling, not deployment defaults.
- Capacity guard is verified by counters and stream closure; API may return EOF instead of an explicit SSE error frame due to closed-channel select ordering.
- Kafka is one KRaft broker/controller, replication factor one; SIGSTOP/SIGCONT only its own PID.
- Franz-go idempotent in-flight publish is not strictly bounded by its caller context; the probe resumes its own broker after a 750ms observation and bounds the post-resume wait.
- Deadline-expired relay retains claim until natural lease expiry; recovery does not manually unlock SQL rows.
- Existing test database causes refusal; only database created by this invocation may be dropped, never FORCE.
- Other agents may change sources/services concurrently; per-run source hashes and observed changes are recorded.

JSON companion records per-file SHA-256, measured ports/PIDs, versions,
timestamps, baseline test actions, source changes and cleanup checks.
