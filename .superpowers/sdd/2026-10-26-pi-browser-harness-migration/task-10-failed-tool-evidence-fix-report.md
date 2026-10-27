# Task 10 Supplemental: Failed Pi Tool Evidence Report

## Status

Implemented the recoverable Pi browser tool failure evidence path on base
`a2f4aae`. No Docker or Compose command was invoked. Destructive Go runtime
tests used only:

```text
postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable
```

## RED

Production code was unchanged when the following focused tests were added.

```bash
cd pi-browser-harness
npm test -- --run \
  test/trace-events.test.ts \
  test/observation-view.test.ts \
  test/assertions.test.ts \
  test/case-builder.test.ts
npm run typecheck
```

Output: 46 passed, 4 failed. The failures were the intended missing behaviors:

```text
trace-events: expected pi.tool.completed.payload.structured_result, received none
observation-view: expected screenshot_path "run_1/obs_1.png", received undefined
case-builder: expected failed assertion screenshot_path, received undefined
case-builder: expected failed finish_case outcome to retain case, received undefined
typecheck: Property 'screenshot_path' does not exist on type 'PageView'
```

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test -count=1 -p 1 ./internal/runtime -run 'TestPostgresPiTraceStore.*(Tool|Finish)' -v
go test -count=1 ./internal/piharness -v
```

Output: runtime failed as intended with
`operation results = 0, want 1`; protocol failed to compile because
`ToolCompletedPayload.StructuredResult` did not exist.

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest \
  integration.v3.test_pi_harness_scenario.FailedEvidenceTest \
  integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest -v
```

Output: intended import failure:

```text
ImportError: cannot import name 'run_has_failed_assertion_evidence'
```

## GREEN

Focused verification after the minimal implementation:

```bash
cd pi-browser-harness
npm test -- --run \
  test/trace-events.test.ts \
  test/observation-view.test.ts \
  test/assertions.test.ts \
  test/case-builder.test.ts
npm run typecheck
```

Output: 4 files, 50 tests passed; `tsc --noEmit` passed.

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test -count=1 -p 1 ./internal/runtime -run 'TestPostgresPiTraceStore.*(Tool|Finish)' -v
go test -count=1 ./internal/piharness -v
```

Output: runtime focused tests passed, including failed browser tool persistence
and finish-case deferral; all `internal/piharness` tests passed.

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest \
  integration.v3.test_pi_harness_scenario.FailedEvidenceTest \
  integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest -v
```

Output: 9 tests passed.

Broad verification:

```bash
cd pi-browser-harness && npm test && npm run typecheck
```

Output: 14 files, 167 tests passed; typecheck passed.

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test -count=1 -p 1 ./...
```

Output: all backend packages passed.

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.HarnessEventTypesTest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest \
  integration.v3.test_pi_harness_scenario.HarnessAttemptTest \
  integration.v3.test_pi_harness_scenario.FailedEvidenceTest \
  integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest \
  integration.v3.test_pi_harness_scenario.AgentServerProbeTest -v
```

Output: 14 non-Docker acceptance tests passed. `ComposeConfigTest` was
intentionally excluded because this task prohibits Docker and Compose.

## Changed Files

- `pi-browser-harness/src/run-protocol.ts`
- `pi-browser-harness/src/trace-events.ts`
- `pi-browser-harness/src/browser/observation-view.ts`
- `pi-browser-harness/src/browser/case-builder.ts`
- `pi-browser-harness/test/trace-events.test.ts`
- `pi-browser-harness/test/observation-view.test.ts`
- `pi-browser-harness/test/case-builder.test.ts`
- `pi-browser-harness/test/mcp-server.test.ts` (required PageView fixture update)
- `backend/internal/piharness/protocol.go`
- `backend/internal/piharness/protocol_test.go`
- `backend/internal/runtime/pi_store.go`
- `backend/internal/runtime/pi_store_test.go`
- `integration/v3/pi_harness_scenario.py`
- `integration/v3/test_pi_harness_scenario.py`

## Self-Review

- `pi.tool.completed.structured_result` is optional and emitted only for the
  already validated exact browser-harness MCP envelope.
- The Go mirror keeps this field as `json.RawMessage`; non-`finish_case`
  browser tools reuse their completion attempt operation ID and persist a
  result on every completion.
- A result is `passed` only for `is_error == false` and structured `ok == true`;
  every other outcome is `failed` with stable error/detail JSON evidence.
- Artifacts always use `{"screenshot_paths":[...]}` and derive only the
  non-empty compact page screenshot path. No raw DOM or screenshot bytes are
  added.
- Failed `assert_text` and `assert_url` include the current compact page and
  committed steps. Failed dry runs retain both authored case and dry-run
  artifacts. `finish_case` completion itself still creates no operation result.
- Failed-run acceptance now requires a failed linked `assert_text` operation
  with `condition_unmet`, detail, attempt, and screenshot. Concurrent
  acceptance reads `operation_results.artifacts` and verifies all paths are
  scoped beneath their run ID.
- `git diff --check` passed; no changed file falls outside the brief's allowed
  source/test/report scope.

## Concerns

None. The live standalone scenario was not run because its HTTP API target
would be destructive outside the explicitly supplied safe test DSN; the
required focused and broad local verification commands all passed.

## Fix Round 1

### Review Corrections

- Failed assertion evidence now uses a single `SELECT EXISTS` query containing
  every acceptance predicate. It cannot select an unrelated failed assertion
  by operation ID when another failed `assert_text` row fully qualifies.
- Concurrent artifact validation parses a normalized `PurePosixPath` and
  rejects absolute paths, parent traversal components, empty artifact names,
  and paths outside the run ID scope.
- The deferred backend attempt/idempotency Minor was not changed.

### Prior Evidence Recovery

The original report content committed in `5bbb513` and its Git history contain
only the paraphrased RED/GREEN summaries already shown above. No exact original
test-runner transcript is recoverable from the report or Git context, so it
cannot be reconstructed faithfully.

### RED

Production code was unchanged when the two focused regression tests were
added:

```bash
python3 -m unittest integration.v3.test_pi_harness_scenario.FailedEvidenceTest integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest -v
```

Exact output:

```text
test_accepts_a_later_qualifying_failed_assertion (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... FAIL
test_rejects_missing_screenshot_path (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_requires_failed_assert_text_with_stable_error_attempt_and_screenshot (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_accepts_distinct_attempts_and_run_scoped_operation_artifacts (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_absolute_and_parent_traversal_artifact_paths (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... test_rejects_operation_artifact_outside_its_run_scope (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_shared_harness_attempt_ids (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok

======================================================================
FAIL: test_accepts_a_later_qualifying_failed_assertion (integration.v3.test_pi_harness_scenario.FailedEvidenceTest)
----------------------------------------------------------------------
Traceback (most recent call last):
  File "/Users/bytedance/project/AI_Web_Testing/.worktrees/pi-browser-harness-migration/integration/v3/test_pi_harness_scenario.py", line 276, in test_accepts_a_later_qualifying_failed_assertion
    self.assertTrue(run_has_failed_assertion_evidence("run-1"))
AssertionError: False is not true

======================================================================
FAIL: test_rejects_absolute_and_parent_traversal_artifact_paths (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) (path='run-1/../run-2/obs_1.png')
----------------------------------------------------------------------
Traceback (most recent call last):
  File "/Users/bytedance/project/AI_Web_Testing/.worktrees/pi-browser-harness-migration/integration/v3/test_pi_harness_scenario.py", line 372, in test_rejects_absolute_and_parent_traversal_artifact_paths
    assert_concurrent_isolation(["run-1", "run-2"])
AssertionError: AssertionError not raised

----------------------------------------------------------------------
Ran 7 tests in 0.005s

FAILED (failures=2)
```

### GREEN

Focused regression verification:

```bash
python3 -m unittest integration.v3.test_pi_harness_scenario.FailedEvidenceTest integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest -v
```

Exact output:

```text
test_accepts_a_later_qualifying_failed_assertion (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_rejects_missing_screenshot_path (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_requires_failed_assert_text_with_stable_error_attempt_and_screenshot (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_accepts_distinct_attempts_and_run_scoped_operation_artifacts (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_absolute_and_parent_traversal_artifact_paths (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_operation_artifact_outside_its_run_scope (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_shared_harness_attempt_ids (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok

----------------------------------------------------------------------
Ran 7 tests in 0.002s

OK
```

Non-Docker Python coverage of the changed acceptance code:

```bash
python3 -m unittest integration.v3.test_pi_harness_scenario.HarnessEventTypesTest integration.v3.test_pi_harness_scenario.TraceShapeTest integration.v3.test_pi_harness_scenario.HarnessAttemptTest integration.v3.test_pi_harness_scenario.FailedEvidenceTest integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest integration.v3.test_pi_harness_scenario.AgentServerProbeTest -v
```

Exact output:

```text
test_enumerates_the_complete_protocol_discriminants (integration.v3.test_pi_harness_scenario.HarnessEventTypesTest) ... ok
test_rejects_raw_steps_projected_as_unknown_tool (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_rejects_unlinked_operation_evidence (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_reports_missing_shape_when_counts_are_zero (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_requires_model_tools_usage_linked_operation_and_execution_steps (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_raises_when_no_harness_attempt_exists (integration.v3.test_pi_harness_scenario.HarnessAttemptTest) ... ok
test_returns_the_first_attempt_for_a_run (integration.v3.test_pi_harness_scenario.HarnessAttemptTest) ... ok
test_accepts_a_later_qualifying_failed_assertion (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_rejects_missing_screenshot_path (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_requires_failed_assert_text_with_stable_error_attempt_and_screenshot (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_accepts_distinct_attempts_and_run_scoped_operation_artifacts (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_absolute_and_parent_traversal_artifact_paths (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_operation_artifact_outside_its_run_scope (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_shared_harness_attempt_ids (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_fails_when_a_probe_port_accepts_connections (integration.v3.test_pi_harness_scenario.AgentServerProbeTest) ... ok
test_passes_when_no_agentserver_port_is_listening (integration.v3.test_pi_harness_scenario.AgentServerProbeTest) ... ok

----------------------------------------------------------------------
Ran 16 tests in 0.005s

OK
```

```bash
git diff --check
```

Exact output: no output (exit status 0).
