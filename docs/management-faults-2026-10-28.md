# Management Fault Acceptance

Date: 2026-10-28
Status: provisional

## Actual Checks

- schema-fault-injection: skipped
- kafka-pause: skipped
- redis-outage: skipped
- finite-fault-preflight: passed
- immutable-registry-bootstrap: passed
- concurrent-real-sdk-load: passed
- active-drain-cancel-rebalance: passed
- owned-crash-after-external-effect: passed
- phase3-actual-measurements: passed
- healthy-conversation-delete: passed

Complete sanitized evidence, individual Runs, durable Kafka/lease records, all samples and original preparation checkpoint history: [JSON report](management-faults-2026-10-28.json).

## Topology

```json
{
  "AGENTTRACE_PROJECT_ID": "prj_2f2b5e037cfb0ce640841aa7702d50f9",
  "AGENTTRACE_API_URL": "http://127.0.0.1:8180",
  "AGENTTRACE_WORKER_URL": "http://127.0.0.1:8110",
  "AGENTTRACE_KAFKA_GROUP_ID": "agenttrace-management-runtime-acceptance",
  "AGENTTRACE_KAFKA_BROKERS": "127.0.0.1:9192",
  "AGENTTRACE_BROWSER_WORKER_CAPACITY": "4",
  "AGENTTRACE_MANAGEMENT_IDENTITY_URL": "http://127.0.0.1:8125/management_identity.html",
  "runtime_workers": [
    "acceptance-runtime-a-20261028",
    "acceptance-runtime-b-20261028"
  ],
  "runtime_concurrency_per_process": 2,
  "browser_worker_id": "unset; canonical endpoint-derived shared provider",
  "initial_runs": 4,
  "database_scope": "agenttrace_test_management_acceptance_20261028",
  "existing_stack_owner_pid": 49449,
  "original_runtime_worker": "runtime-KDQDT6WYL9-51297-HSIIBS7I3NCRA2CXSPVSYQENRR"
}
```

## Measurements

- Actual scenario Runs: 9
- Elapsed execution: 276.339 seconds.
- Sample count: 120
- Peak sampled simultaneous Assignments: 3
- Actual model tokens: 300455
- Queue p50/p95: 4.122294/37.965469 seconds.
- Durable cancellation latency: 0.240357 seconds.
- HTTP-observed cancellation latency: 0.529366 seconds.
- Peak DB connections: 19.0 (includes read-only measurement connection).
- Runtime subtree RSS distributions (KiB): `{"70286":{"count":89,"min":19264.0,"p50":207696.0,"p95":294832.0,"max":318896.0},"70287":{"count":120,"min":19408.0,"p50":151168.0,"p95":240112.0,"max":245840.0}}`.
- Protected API subtree RSS peak: 33344.0 KiB; inspection only.

## Crash And Cleanup

- Signal: SIGKILL only to owned PID 70286 after successful unique login assertion and PNG.
- Crash Run: `run_e99ff65f38a1d98955f92c6f8361a212`.
- Retained Conversation: `conv_e19d00c041c2a5ae3c04af778c3ee195`.
- PNG bytes/SHA256: 17886 / `803ca242e6a79be93db054f253eec929ac4cab7b0dd3b8609d76d20c99b2de49`.
- Recovery after actual B survivor success: `{"status":"failed","error_code":"execution_lost","environment_state":"needs_review","fencing_token":3,"attempt_status":"lost","attempt_count":1,"assignment_state":"expired","lease_state":"expired"}`.
- Original drain restore: `{"previous": false, "acknowledged_in_use": 0, "restored": true}`.
- Protected identity checks: `{"native_owner_unchanged": true, "api_unchanged": true, "original_runtime_unchanged": true}`.
- Remaining verified owned descendants: `[]`.
- Cleanup errors: `null`.
- Recorded Node descendants all exited; no orphan cleanup signal was needed.

## Build And Verification

```json
{
  "source_sha": "645942d983c2c3cb8432209e484ffec86da392e9",
  "dirty_diff_sha256": "2c7fe587fcdd3ab0a571ae39ac09e4b80bebc0d4816dbd4ac6e7bd63441040fe",
  "source_tree_sha256": "356fbdcd818e31e9060cfc261206fff52c92942e9a5c03df7bdfec7dfb937373",
  "source_file_count": 247,
  "runtime_binary_sha256": "1972f934c818f11c1ef5beee01245a045af9b35f97d5790f47ae50e4bb056430",
  "node_dist_main_sha256": "981a97f0307af9f7ccdac5e3edd3fb34f33699899cf6810b02a5bc23c678f477",
  "node_dist_mcp_sha256": "d594935340cc5dbb842e8ac692db5f2582aad03356bc942e7508738dde6b1a06",
  "harness_sha256": "4369238e56ea125b836abe6b03c76d0ccfe0efaa0a0cdfc33ba71082e178180d",
  "private_supervisor_sha256": "ab54a06318777c7003f3f33b7b4e4d3ddea3c1105e967795e221b9d25a66dcec"
}
```
- Release identity changed during concurrent source edits; before/after hashes are in JSON.
- Fresh post-fix verification: 36 scoped harness tests and 7 private ownership safety tests passed.
- Original API predates latest SSE redaction/BaseContext edits; it was not restarted.
- Post-cleanup API/browser health: ok/ok.
- Original runtime restored alive/undrained/idle; active leases: 0.
- Post-cleanup verification submitted zero Runs and sent zero signals.

## Limits

- Finite local workload, not a production soak or horizontal browser-store validation.
- Parent owns full tests, Langfuse UI/RBAC, identity and final privacy acceptance.
- Schema migration faults, Kafka pause and Redis outage were not injected or verified.
- No operator performance budgets were supplied; timings are measurements, not capacity approval.
- Existing API RSS is inspection-only under its verified native owner, not a private owned signal target.
- Killed runtime is not restarted. Orphan SDK/browser children may remain; parent must inspect owned descendants. Crash Conversation retained for review; no automatic review/reset/delete.
- Source/build provenance changed during this run; before/after hashes are recorded.
- Supervisor privacy-ready flag referred to the prior authorized Node build, identified by release_identity Node hashes. Newly discovered public model-input/premarker-text password fixes and control-close-across-TTL fixes were not deployed or validated by this scenario. No latest-privacy acceptance is claimed.
- Actual four-Run submission observed peak three simultaneous Assignments across both owners; this is not proof of sustained four-slot saturation.
- Post-cleanup verified all recorded owned Node descendants already exited; no orphan-cleanup signal was required and no protected service was signalled.
