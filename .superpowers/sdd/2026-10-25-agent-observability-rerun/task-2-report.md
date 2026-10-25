# Task 2 Report: Create Failed Reruns Atomically

## Status

DONE

## Commit

`ce65fe7 feat: create immutable failed-run reruns`

## Implementation Details

- Added `CreateRerunParams` and `Store.CreateRerun`.
- Validates nonblank source Run and idempotency key, hashes only
  `{"run_id": sourceID}`, and scopes idempotency records as
  `run-rerun:<sourceRunID>`.
- Uses one transaction that locks the source Run before attempting the
  idempotency insert. This serializes concurrent requests for the same failed
  source.
- Requires an existing failed source with a nonblank `source_message_id`.
- On matching idempotency replay, locks the idempotency record, verifies the
  request hash, reads the referenced Run including lineage fields, commits, and
  returns that same rerun identity.
- For a new request, decodes the stored immutable specification, creates fresh
  Run, event, and idempotency IDs, replaces only command `run_id` and
  `checkpoint_version`, and stores a queued child Run.
- The child inherits project, conversation, and source message; it records the
  locked failed Run as `retry_of_run_id`.
- Persists the new `run.requested` command outbox envelope in the same
  transaction. Its causation ID is the immediate failed parent Run. No message
  is inserted.

## Files Changed

- `backend/internal/postgres/runs.go`
- `backend/internal/postgres/store_integration_test.go`
- `backend/internal/postgres/api_integration_test.go`
- `backend/internal/postgres/deletion_integration_test.go`

## TDD Evidence

### RED

Tests were written before the production API. The required focused command was
run before implementation:

```bash
docker compose exec -T api go test ./internal/postgres -run 'TestCreateRerun' -count=1
```

Relevant output:

```text
store.CreateRerun undefined (type *Store has no field or method CreateRerun)
undefined: CreateRerunParams
FAIL github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres [build failed]
```

The failure was the expected missing repository interface, not a test setup
failure.

### GREEN: Focused Rerun Coverage

The Compose API container has no default `AGENTTRACE_TEST_POSTGRES_DSN`, so a
dedicated disposable PostgreSQL database was created and injected for the
integration tests:

```bash
docker compose exec -T -e AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@postgres:5432/agenttrace_test_task2_20261025_rerun?sslmode=disable' api go test ./internal/postgres -run 'TestCreateRerun' -count=1 -v
```

Relevant output:

```text
--- PASS: TestCreateRerunIsReturnedByConversationRead
--- PASS: TestCreateRerunAllowsConversationDeletionAfterDelivery
--- PASS: TestCreateRerunQueuesImmutableFailedSpecification
--- PASS: TestCreateRerunRejectsNonFailedSourceStatuses
--- PASS: TestCreateRerunReplaysMatchingIdempotencyRequest
--- PASS: TestCreateRerunSerializesConcurrentMatchingIdempotencyRequests
--- PASS: TestCreateRerunUsesImmediateFailedParent
--- PASS: TestCreateRerunValidatesRequiredParams
PASS
ok github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres 0.682s
```

Coverage proves fresh command identity and checkpoint zero, copied immutable
model/messages/operations, no additional message row, failed-only eligibility
over every other status, same-key replay, concurrent same-key serialization,
immediate-parent lineage, conversation reads, and deletion of delivered terminal
lineage.

### GREEN: Full PostgreSQL Package

```bash
docker compose exec -T -e AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@postgres:5432/agenttrace_test_task2_20261025_rerun?sslmode=disable' api go test ./internal/postgres -count=1
```

Output:

```text
ok github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres 3.563s
```

## Self-Review

- Confirmed source Run locking occurs before the idempotency insert, avoiding
  duplicate rerun creation under same-source concurrent requests.
- Confirmed rerun replay locks and checks the durable idempotency record and
  reads all Run lineage columns in the established projection order.
- Confirmed the source specification is decoded and copied without mutation;
  only the command Run ID and checkpoint are changed.
- Confirmed the outbox event is keyed and correlated by the fresh rerun while
  using the immediate parent as causation.
- Confirmed self-referential `retry_of_run_id` deletion succeeds after both
  commands are published and processed.
- Confirmed no HTTP route, API interface change, frontend work, migration, or
  unrelated refactor was added.
- `git diff --check` completed without whitespace errors.

## Concerns

None. The only test-environment detail is that the real isolated database had
to be explicitly created and passed through `AGENTTRACE_TEST_POSTGRES_DSN`;
the unmodified Compose API service does not provide that variable.

## Fix Round 1

### Commit

`99529c0 fix: order rerun locks before source`

### Changes

- `backend/internal/postgres/runs.go`: `CreateRerun` now reads the source
  conversation, locks that conversation through the established
  project/conversation helper, then re-reads and locks the source Run within
  that locked conversation before inserting the idempotency key.
- `backend/internal/postgres/store_integration_test.go`: the duplicate-rerun
  test now holds the source row and verifies that a rerun request reaches a
  real PostgreSQL lock wait before release.
- `backend/internal/postgres/deletion_integration_test.go`: added a
  deterministic rerun-versus-conversation-deletion overlap using a test-only
  advisory-lock trigger. It proves the original order yields `40P01` and the
  corrected order returns a successful rerun plus deletion `ErrConflict`.

### RED

```bash
docker compose exec -T -e AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@postgres:5432/agenttrace_test_task2_fixround1_20261025?sslmode=disable' api go test ./internal/postgres -run 'TestCreateRerun(SerializesConcurrentMatchingIdempotencyRequests|DoesNotDeadlockConcurrentConversationDeletion)' -count=1 -v
```

Relevant output before the production change:

```text
=== RUN   TestCreateRerunDoesNotDeadlockConcurrentConversationDeletion
    deletion_integration_test.go:207: DeleteConversation during rerun overlap error = iterate deletion runs: ERROR: deadlock detected (SQLSTATE 40P01), want ErrConflict
--- FAIL: TestCreateRerunDoesNotDeadlockConcurrentConversationDeletion (1.30s)
=== RUN   TestCreateRerunSerializesConcurrentMatchingIdempotencyRequests
--- PASS: TestCreateRerunSerializesConcurrentMatchingIdempotencyRequests (0.07s)
FAIL
FAIL github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres 1.379s
FAIL
```

### GREEN: Focused Rerun Coverage

```bash
docker compose exec -T -e AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@postgres:5432/agenttrace_test_task2_fixround1_20261025?sslmode=disable' api go test ./internal/postgres -run 'TestCreateRerun' -count=1 -v
```

Relevant output:

```text
--- PASS: TestCreateRerunIsReturnedByConversationRead
--- PASS: TestCreateRerunAllowsConversationDeletionAfterDelivery
--- PASS: TestCreateRerunDoesNotDeadlockConcurrentConversationDeletion
--- PASS: TestCreateRerunQueuesImmutableFailedSpecification
--- PASS: TestCreateRerunRejectsNonFailedSourceStatuses
--- PASS: TestCreateRerunReplaysMatchingIdempotencyRequest
--- PASS: TestCreateRerunSerializesConcurrentMatchingIdempotencyRequests
--- PASS: TestCreateRerunUsesImmediateFailedParent
--- PASS: TestCreateRerunValidatesRequiredParams
PASS
ok github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres 0.800s
```

### GREEN: Full PostgreSQL Package

```bash
docker compose exec -T -e AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@postgres:5432/agenttrace_test_task2_fixround1_20261025?sslmode=disable' api go test ./internal/postgres -count=1
```

Output:

```text
ok github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres 3.600s
```

### Self-Review

- Confirmed the lock order is project, conversation, then source Run, matching
  conversation deletion and preserving the required source-before-idempotency
  ordering.
- Confirmed the source is revalidated after the conversation lock using the
  same project and conversation identifiers, so deletion cannot leave a stale
  source eligible for rerun.
- Confirmed the deterministic overlap waits for both the rerun synchronization
  point and the deletion row-lock wait; it fails against the original
  implementation with `40P01`.
- Confirmed `git diff --check` and the focused and full real-PostgreSQL tests
  completed successfully.

### Concerns

None. The focused and full package tests require the explicitly supplied
isolated `AGENTTRACE_TEST_POSTGRES_DSN`.
