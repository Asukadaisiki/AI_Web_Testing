# Task 10 Supplemental Implementation Report

## Status

Implemented Pi finish evidence persistence and migrated v3 acceptance to the
Pi trace contract.

## RED Evidence

### TypeScript finish evidence

Command:

```bash
cd pi-browser-harness
npm test -- --run test/trace-events.test.ts
```

Observed: 2 of 23 tests failed. Both direct and Pi-wrapped `finish_case`
results produced only `{"verdict":"passed"}`; Vitest reported the literal
`case` and `dry_run` objects as missing.

The existing malformed artifact, wrong-tool, and strict Pi-envelope tests
continued to pass.

### PostgreSQL runtime persistence

The required DSN command:

```bash
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace?sslmode=disable' \
  go test ./internal/runtime -run TestPostgresPiTraceStorePersistsFinishCaseEvidence -v
```

stopped at the repository safety guard:

```text
refusing destructive PostgreSQL reset for database "agenttrace":
require agenttrace_test or agenttrace_test_*
```

The same local PostgreSQL server already had the allowed `agenttrace_test`
database. Re-running against it produced the intended behavior failure:

```text
operation links = span "" attempt "", want finish span
"span_0c29eeb54ab24bb2277d52d983b9c4f6" and a linked attempt
```

A later self-review regression also proved detached evidence was accepted:

```text
append case completion without a completed finish attempt = nil, want error
```

### PostgreSQL completed-tool lookup

Command:

```bash
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test ./internal/postgres \
  -run TestPiCompletedToolAttemptLookupSelectsExactNameAndSourceSequence -v
```

Observed build failures:

```text
store.LatestCompletedToolAttempt undefined
undefined: CompletedToolAttempt
```

### Trace projection

Command:

```bash
go test ./internal/traceprojection \
  -run TestProjectPiFinishCaseBrowserSteps -v
```

Observed:

```text
node kind = "unknown_tool", want step
```

### Python acceptance

Command, excluding the pre-existing Docker-backed class:

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest \
  integration.v3.test_canonical_scenario
```

Observed 7 tests with 3 failures and 2 errors:

- trace shape still returned `report` and had no `linked_operation`;
- `harness.completed` SSE records did not terminate the consumer;
- canonical API acceptance rejected actual Pi model/MCP spans in favor of
  `model:plan -> operation:operation -> report:report -> model:final`.

## Implementation

- `browser.case.completed` now emits literal `verdict`, `case`, and `dry_run`
  for valid direct and Pi-wrapped finish results. Existing exact tool,
  envelope, case-object, and dry-run checks remain in force.
- Go protocol decoding preserves `case` and `dry_run` as raw JSON.
- PostgreSQL now resolves the latest completed operation attempt by exact
  operation name and source sequence.
- Case completion requires a resolved
  `mcp__browser_harness__finish_case` span attempt. Detached evidence is
  rejected.
- The operation result links to that attempt, uses the objective verdict as
  status, and stores only `dry_run` as `result`.
- Artifacts are exactly `{"screenshot_paths":[...]}` with non-empty paths
  deduplicated in step order.
- No synthetic `browser.case` span is created. The existing finish span name
  and parentage remain unchanged.
- Trace projection routes both `browser_execute_case` and
  `mcp__browser_harness__finish_case` through the browser-step projector.
- Pi trace-shape acceptance now requires linked operation evidence and
  non-empty execution steps, with no report-span requirement.
- Canonical API/PostgreSQL acceptance now checks one root, completed model
  spans, positive model usage, browser MCP operation spans, exactly one finish
  span, linked passed finish evidence, screenshot paths, dry-run steps,
  finish-parented derived step nodes, and the exact final assistant message.
- Canonical SSE and failure-drill terminal queries now use
  `harness.completed`.

## GREEN Verification

Focused TypeScript:

```text
npm test -- --run test/trace-events.test.ts: 23 passed
npm run typecheck: passed
```

Focused Go, serialized because the integration packages reset one test schema:

```text
GOFLAGS='-p=1' AGENTTRACE_TEST_POSTGRES_DSN=.../agenttrace_test \
  go test ./internal/runtime ./internal/postgres ./internal/traceprojection \
  -run 'Test.*(Pi|Harness|Project)' -v

runtime: passed
postgres: passed
traceprojection: passed
```

Focused Python without Docker/Compose:

```text
14 tests passed
```

Broad checks:

```text
pi-browser-harness npm test -- --run: 163 passed
pi-browser-harness npm run build: passed
backend go test -p 1 ./...: passed
integration/v3 filtered unittest discovery: 49 passed
python3 -m py_compile on all six modified Python files: passed
git diff --check: passed
```

The Python discovery used `worker/.venv` after installing the declared
`integration/v3/requirements.txt` dependency (`psycopg`). It excluded 12
pre-existing tests that either invoke Docker/Compose or actively stop, start,
or disrupt the live stack.

## Self-Review

- All source and test changes are restricted to the 16 files listed in the
  brief; this report is the only additional file.
- No Compose, runtime lifecycle, worker API, service, or parent-workspace file
  was changed.
- No Docker/Compose command was run and no service was stopped or started.
- `runtime.completed` is absent from all executable v3 acceptance files listed
  in the brief.
- The operation evidence test verifies result shape, exact artifact shape,
  ordered deduplication, finish span and attempt linkage, verdict status, and
  absence of an extra operation span.
- The completed-tool lookup test verifies exact-name filtering, strict
  source-sequence ordering, and selection of the latest of two finish
  attempts.

## Concerns

- The brief-provided `agenttrace` DSN is incompatible with the repository's
  destructive integration-test guard. PostgreSQL integration tests were run
  against the existing `agenttrace_test` database on the same local server.
- The exact broad `unittest discover` command was not run because it would
  execute Docker/Compose and live outage drills, contrary to the environment
  policy. All discovered unit tests were run; 12 live/Docker tests were
  explicitly filtered.

## Fix Round 1 (2026-10-27)

### Status

Addressed both Important review findings from reviewed head `bce20b9`. The
Minor finding about optional event type fields was intentionally not changed.

### Behavior

- Harness-emitted spans now persist their owning `harness_attempt_id` through
  migration `007_harness_span_attempt.sql`.
- `LatestCompletedToolAttempt` accepts the current harness attempt ID and
  filters completed finish spans by that durable identity as well as Run,
  operation name, and source sequence.
- Span ownership cannot be overwritten by a different harness attempt;
  conflicting writes return `ErrConflict`.
- `PostgresPiTraceStore` passes `ref.ID` when resolving finish evidence.
- `trace_has_required_shape` no longer treats arbitrary linked operation rows
  or raw non-empty `result.steps` as execution evidence. It reads the trace API,
  requires operation evidence linked to a
  `mcp__browser_harness__finish_case` span attempt, and requires a `step`
  derived node parented by that linked finish span.

### RED

PostgreSQL attempt-scoping regression:

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test ./internal/postgres \
  -run TestPiCompletedToolAttemptLookupSelectsExactNameAndSourceSequence -v
```

Observed build failure before implementation at all four desired-signature
calls:

```text
too many arguments in call to store.LatestCompletedToolAttempt
have (context.Context, string, string, string, number)
want (context.Context, string, string, int64)
FAIL github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres [build failed]
```

The regression uses two harness attempts with source sequence `1`: the first
has a completed finish span and the retry has only a completed click span. The
retry lookup must return no finish attempt.

API projection acceptance regression:

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest -v
```

Observed 4 tests run with 2 intended failures:

```text
FAIL: test_rejects_raw_steps_projected_as_unknown_tool
AssertionError: True is not false
FAIL: test_rejects_unlinked_operation_evidence
AssertionError: True is not false
Ran 4 tests
FAILED (failures=2)
```

The negative API fixture includes non-empty raw steps but only an
`unknown_tool` derived node, proving raw operation JSON cannot satisfy the new
acceptance assertion.

### GREEN

Focused PostgreSQL regression:

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test ./internal/postgres \
  -run TestPiCompletedToolAttemptLookupSelectsExactNameAndSourceSequence -v
```

```text
--- PASS: TestPiCompletedToolAttemptLookupSelectsExactNameAndSourceSequence
PASS
ok github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres
```

Focused Python regression:

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest -v
```

```text
Ran 4 tests in 0.002s
OK
```

Focused Go suites:

```bash
cd backend
GOFLAGS='-p=1' \
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test ./internal/runtime ./internal/postgres ./internal/traceprojection \
  -run 'Test.*(Pi|Harness|Project)' -v
```

```text
runtime: PASS
postgres: PASS
traceprojection: PASS
```

Focused Python scenario suites:

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario \
  integration.v3.test_canonical_scenario -v
```

```text
Ran 16 tests in 0.926s
OK
```

Adding the seventh migration first exposed the two stale hard-coded migration
counts:

```text
TestMigrateAppliesSchemaOnceAndEnforcesContracts:
applied migrations = 7, want 6
TestMigrateSerializesConcurrentInitialMigration:
applied migrations after attempt 1 = 7, want 6
```

After updating migration bookkeeping and asserting the new span column:

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test ./internal/postgres \
  -run 'TestMigrate(AppliesSchemaOnceAndEnforcesContracts|SerializesConcurrentInitialMigration)' -v
```

```text
--- PASS: TestMigrateAppliesSchemaOnceAndEnforcesContracts
--- PASS: TestMigrateSerializesConcurrentInitialMigration
PASS
```

Fresh full backend verification:

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test -count=1 -p 1 ./...
```

```text
All backend packages passed; exit 0.
```

Static checks:

```bash
python3 -m py_compile \
  integration/v3/pi_harness_scenario.py \
  integration/v3/test_pi_harness_scenario.py
git diff --check
```

Both commands exited `0` with no output.

### Files

- `backend/internal/postgres/migrations/007_harness_span_attempt.sql`
- `backend/internal/postgres/harness.go`
- `backend/internal/postgres/harness_integration_test.go`
- `backend/internal/postgres/testdb_test.go`
- `backend/internal/runtime/pi_store.go`
- `integration/v3/pi_harness_scenario.py`
- `integration/v3/test_pi_harness_scenario.py`
- `.superpowers/sdd/2026-10-26-pi-browser-harness-migration/task-10-evidence-acceptance-fix-report.md`

### Self-Review

- The regression fails if the lookup drops the harness attempt filter: the
  retry reuses the prior finish span.
- The API acceptance regression fails if execution evidence is derived from
  raw `result.steps`: its projected node is deliberately `unknown_tool`.
- The new span column is nullable, so non-harness and historical spans remain
  valid. Harness writes stamp it in the same transaction as span/event
  persistence.
- No optional event type handling, Compose, runtime lifecycle, worker API, or
  parent-workspace files were changed.
- No Docker/Compose command ran and no service was stopped or started.

### Concerns

None.
