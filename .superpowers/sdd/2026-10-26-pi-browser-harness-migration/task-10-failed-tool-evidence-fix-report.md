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

## Fix Round 2

### Faithful Test-Only RED Reconstruction

The original RED is now reproduced, rather than inferred. A unique detached
temporary worktree was created outside the repository at:

```text
/tmp/ai-web-testing-task10-red-20261027165418-63376
```

Its detached `HEAD` was `a2f4aae`. Only these test files from
`a2f4aae..5bbb513` were applied there:

```text
backend/internal/piharness/protocol_test.go
backend/internal/runtime/pi_store_test.go
integration/v3/test_pi_harness_scenario.py
pi-browser-harness/test/case-builder.test.ts
pi-browser-harness/test/mcp-server.test.ts
pi-browser-harness/test/observation-view.test.ts
pi-browser-harness/test/trace-events.test.ts
```

No production diff was applied. The temporary worktree used a
`pi-browser-harness/node_modules` symlink to the current worktree only; no
production or test file in the current worktree was changed. The exact
commands below were run against that test-only reconstruction. Output is
recorded as captured stdout and stderr; terminal color control sequences are
omitted from the Markdown rendering only.

#### RED: Pi Focused Tests

```bash
cd pi-browser-harness
npm test -- --run \
  test/trace-events.test.ts \
  test/observation-view.test.ts \
  test/assertions.test.ts \
  test/case-builder.test.ts
```

Relevant stdout:

```text
> @agenttrace/pi-browser-harness@0.1.0 test
> vitest --run test/trace-events.test.ts test/observation-view.test.ts test/assertions.test.ts test/case-builder.test.ts

 RUN  v4.0.4 /private/tmp/ai-web-testing-task10-red-20261027165418-63376/pi-browser-harness

 ❯ test/trace-events.test.ts (24 tests | 1 failed) 7ms
 ❯ test/observation-view.test.ts (6 tests | 1 failed) 18ms
 ✓ test/assertions.test.ts (10 tests) 3ms
 ❯ test/case-builder.test.ts (10 tests | 2 failed) 272ms

 Test Files  3 failed | 1 passed (4)
      Tests  4 failed | 46 passed (50)
```

Relevant stderr:

```text
 FAIL  test/case-builder.test.ts > CaseBuilder transactional authoring > failed assertions retain the current compact page and committed steps
AssertionError: expected undefined to be 'run_1/obs_failed.png' // Object.is equality

- Expected:
"run_1/obs_failed.png"

+ Received:
undefined

 ❯ test/case-builder.test.ts:365:52
    363|       expect(outcome.result.ok).toBe(false);
    364|       expect(outcome.result.error).toBe("condition_unmet");
    365|       expect(outcome.result.page?.screenshot_path).toBe("run_1/obs_fai…
       |                                                    ^
    366|       expect(outcome.result.steps).toEqual([
    367|         expect.objectContaining({ index: 0, action: "goto" }),

 FAIL  test/case-builder.test.ts > CaseBuilder transactional authoring > finish_case retains the authored case when its dry run fails
AssertionError: Target cannot be null or undefined.

 ❯ test/case-builder.test.ts:549:34
    547|
    548|     expect(finished.result.ok).toBe(false);
    549|     expect(finished.case?.steps).toHaveLength(1);
       |                                  ^
    550|     expect(finished.dryRun?.status).toBe("failed");
    551|   });

 FAIL  test/observation-view.test.ts > buildPageView > preserves a nullable screenshot path from the observation
AssertionError: expected undefined to be 'run_1/obs_1.png' // Object.is equality

- Expected:
"run_1/obs_1.png"

+ Received:
undefined

 ❯ test/observation-view.test.ts:240:7
    238|         null,
    239|       ).screenshot_path,
    240|     ).toBe("run_1/obs_1.png")
       |       ^
    241|     expect(buildPageView(observation(), "", null).screenshot_path).toB…
    242|   });

 FAIL  test/trace-events.test.ts > mapPiEvent > carries a recoverable browser MCP failure as bounded completion evidence
AssertionError: expected { type: 'pi.tool.completed', …(1) } to deeply equal { type: 'pi.tool.completed', …(1) }

- Expected
+ Received

  {
    "payload": {
      "is_error": false,
-     "structured_result": {
-       "detail": "the text \"DoesNotExist\" is not present",
-       "error": "condition_unmet",
-       "ok": false,
-       "page": {
-         "screenshot_path": "run_1/obs_failed.png",
-         "title": "Shop",
-         "url": "https://shop.test/",
-       },
-       "steps": [
-         {
-           "action": "goto",
-           "index": 0,
-           "intent": "open shop",
-         },
-       ],
-     },
      "tool_call_id": "call_failed",
      "tool_name": "mcp__browser_harness__assert_text",
    },
    "type": "pi.tool.completed",
  }

 ❯ test/trace-events.test.ts:154:23
    152|     );
    153|
    154|     expect(events[0]).toEqual({
       |                       ^
    155|       type: "pi.tool.completed",
    156|       payload: {
```

Exit status: `1`.

```bash
npm run typecheck
```

Relevant stdout:

```text
> @agenttrace/pi-browser-harness@0.1.0 typecheck
> tsc --noEmit

test/case-builder.test.ts(365,35): error TS2339: Property 'screenshot_path' does not exist on type 'PageView'.
test/mcp-server.test.ts(163,11): error TS2353: Object literal may only specify known properties, and 'screenshot_path' does not exist in type 'PageView'.
test/observation-view.test.ts(239,9): error TS2339: Property 'screenshot_path' does not exist on type 'PageView'.
test/observation-view.test.ts(241,51): error TS2339: Property 'screenshot_path' does not exist on type 'PageView'.
```

stderr: empty. Exit status: `2`.

#### RED: Go Focused Tests

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test -count=1 -p 1 ./internal/runtime -run 'TestPostgresPiTraceStore.*(Tool|Finish)' -v
```

Relevant stdout:

```text
=== RUN   TestPostgresPiTraceStorePersistsFinishCaseEvidence
--- PASS: TestPostgresPiTraceStorePersistsFinishCaseEvidence (0.70s)
=== RUN   TestPostgresPiTraceStorePersistsFailedBrowserToolEvidence
    pi_store_test.go:207: operation results = 0, want 1
--- FAIL: TestPostgresPiTraceStorePersistsFailedBrowserToolEvidence (0.36s)
=== RUN   TestPostgresPiTraceStoreDefersFinishCaseResultToCaseCompletion
--- PASS: TestPostgresPiTraceStoreDefersFinishCaseResultToCaseCompletion (0.21s)
FAIL
FAIL	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/runtime	1.897s
FAIL
```

stderr: empty. Exit status: `1`.

```bash
go test -count=1 ./internal/piharness -v
```

Relevant stdout:

```text
FAIL	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness [build failed]
FAIL
```

Relevant stderr:

```text
# github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness [github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness.test]
internal/piharness/protocol_test.go:265:20: payload.StructuredResult undefined (type ToolCompletedPayload has no field or method StructuredResult)
internal/piharness/protocol_test.go:266:46: payload.StructuredResult undefined (type ToolCompletedPayload has no field or method StructuredResult)
```

Exit status: `1`.

#### RED: Python Focused Tests

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest \
  integration.v3.test_pi_harness_scenario.FailedEvidenceTest \
  integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest -v
```

stdout: empty.

stderr:

```text
test_pi_harness_scenario (unittest.loader._FailedTest) ... ERROR
test_pi_harness_scenario (unittest.loader._FailedTest) ... ERROR
test_pi_harness_scenario (unittest.loader._FailedTest) ... ERROR

======================================================================
ERROR: test_pi_harness_scenario (unittest.loader._FailedTest)
----------------------------------------------------------------------
ImportError: Failed to import test module: test_pi_harness_scenario
Traceback (most recent call last):
  File "/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/3.9/lib/python3.9/unittest/loader.py", line 154, in loadTestsFromName
    module = __import__(module_name)
  File "/private/tmp/ai-web-testing-task10-red-20261027165418-63376/integration/v3/test_pi_harness_scenario.py", line 17, in <module>
    from pi_harness_scenario import (
ImportError: cannot import name 'run_has_failed_assertion_evidence' from 'pi_harness_scenario' (/private/tmp/ai-web-testing-task10-red-20261027165418-63376/integration/v3/pi_harness_scenario.py)

======================================================================
ERROR: test_pi_harness_scenario (unittest.loader._FailedTest)
----------------------------------------------------------------------
ImportError: Failed to import test module: test_pi_harness_scenario
Traceback (most recent call last):
  File "/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/3.9/lib/python3.9/unittest/loader.py", line 154, in loadTestsFromName
    module = __import__(module_name)
  File "/private/tmp/ai-web-testing-task10-red-20261027165418-63376/integration/v3/test_pi_harness_scenario.py", line 17, in <module>
    from pi_harness_scenario import (
ImportError: cannot import name 'run_has_failed_assertion_evidence' from 'pi_harness_scenario' (/private/tmp/ai-web-testing-task10-red-20261027165418-63376/integration/v3/pi_harness_scenario.py)

======================================================================
ERROR: test_pi_harness_scenario (unittest.loader._FailedTest)
----------------------------------------------------------------------
ImportError: Failed to import test module: test_pi_harness_scenario
Traceback (most recent call last):
  File "/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/3.9/lib/python3.9/unittest/loader.py", line 154, in loadTestsFromName
    module = __import__(module_name)
  File "/private/tmp/ai-web-testing-task10-red-20261027165418-63376/integration/v3/test_pi_harness_scenario.py", line 17, in <module>
    from pi_harness_scenario import (
ImportError: cannot import name 'run_has_failed_assertion_evidence' from 'pi_harness_scenario' (/private/tmp/ai-web-testing-task10-red-20261027165418-63376/integration/v3/pi_harness_scenario.py)

----------------------------------------------------------------------
Ran 3 tests in 0.000s

FAILED (errors=3)
```

Exit status: `1`.

### Fresh GREEN Evidence

The same focused commands were rerun on current `ddb09da` in the current
worktree. The Go runtime command used only the supplied `agenttrace_test` DSN.

#### GREEN: Pi Focused Tests

```bash
cd pi-browser-harness
npm test -- --run \
  test/trace-events.test.ts \
  test/observation-view.test.ts \
  test/assertions.test.ts \
  test/case-builder.test.ts
```

Relevant stdout:

```text
> @agenttrace/pi-browser-harness@0.1.0 test
> vitest --run test/trace-events.test.ts test/observation-view.test.ts test/assertions.test.ts test/case-builder.test.ts

 RUN  v4.0.4 /Users/bytedance/project/AI_Web_Testing/.worktrees/pi-browser-harness-migration/pi-browser-harness

 ✓ test/trace-events.test.ts (24 tests) 5ms
 ✓ test/observation-view.test.ts (6 tests) 16ms
 ✓ test/assertions.test.ts (10 tests) 3ms
 ✓ test/case-builder.test.ts (10 tests) 277ms

 Test Files  4 passed (4)
      Tests  50 passed (50)
```

stderr: empty. Exit status: `0`.

```bash
npm run typecheck
```

Relevant stdout:

```text
> @agenttrace/pi-browser-harness@0.1.0 typecheck
> tsc --noEmit
```

stderr: empty. Exit status: `0`.

#### GREEN: Go Focused Tests

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test -count=1 -p 1 ./internal/runtime -run 'TestPostgresPiTraceStore.*(Tool|Finish)' -v
```

Relevant stdout:

```text
=== RUN   TestPostgresPiTraceStorePersistsFinishCaseEvidence
--- PASS: TestPostgresPiTraceStorePersistsFinishCaseEvidence (0.26s)
=== RUN   TestPostgresPiTraceStorePersistsFailedBrowserToolEvidence
--- PASS: TestPostgresPiTraceStorePersistsFailedBrowserToolEvidence (0.26s)
=== RUN   TestPostgresPiTraceStoreDefersFinishCaseResultToCaseCompletion
--- PASS: TestPostgresPiTraceStoreDefersFinishCaseResultToCaseCompletion (0.23s)
PASS
ok  	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/runtime	1.422s
```

stderr: empty. Exit status: `0`.

```bash
go test -count=1 ./internal/piharness -v
```

Relevant stdout:

```text
=== RUN   TestToolCompletedPayloadPreservesStructuredResult
--- PASS: TestToolCompletedPayloadPreservesStructuredResult (0.00s)
PASS
ok  	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness	1.412s
```

stderr: empty. Exit status: `0`.

#### GREEN: Python Focused Tests

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest \
  integration.v3.test_pi_harness_scenario.FailedEvidenceTest \
  integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest -v
```

stdout: empty.

stderr:

```text
test_rejects_raw_steps_projected_as_unknown_tool (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_rejects_unlinked_operation_evidence (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_reports_missing_shape_when_counts_are_zero (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_requires_model_tools_usage_linked_operation_and_execution_steps (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_accepts_a_later_qualifying_failed_assertion (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_rejects_missing_screenshot_path (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_requires_failed_assert_text_with_stable_error_attempt_and_screenshot (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_accepts_distinct_attempts_and_run_scoped_operation_artifacts (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_absolute_and_parent_traversal_artifact_paths (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_operation_artifact_outside_its_run_scope (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_shared_harness_attempt_ids (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok

----------------------------------------------------------------------
Ran 11 tests in 0.005s

OK
```

Exit status: `0`.

### Round 2 Scope

No Docker or Compose command was run. No production or test file was changed
in this round. The temporary detached worktree was removed before this
report-only update was committed.

## Fix Round 3

### Capture Method

Round 3 recreates the test-only RED worktree exactly as Round 2 in detached
worktree `/tmp/ai-web-testing-task10-red-round3-20261027170042-67178` at
`a2f4aae`. It applies only these test-file diffs from `a2f4aae..5bbb513`:

```text
backend/internal/piharness/protocol_test.go
backend/internal/runtime/pi_store_test.go
integration/v3/test_pi_harness_scenario.py
pi-browser-harness/test/case-builder.test.ts
pi-browser-harness/test/mcp-server.test.ts
pi-browser-harness/test/observation-view.test.ts
pi-browser-harness/test/trace-events.test.ts
```

The worktree uses a `pi-browser-harness/node_modules` symlink to the current
worktree. The following are the complete captured stdout and stderr streams
and exact exit status for every required focused command. Empty streams are
shown as empty code blocks.

### RED

#### Pi Focused Tests

```bash
cd pi-browser-harness
npm test -- --run \
  test/trace-events.test.ts \
  test/observation-view.test.ts \
  test/assertions.test.ts \
  test/case-builder.test.ts
```

stdout:

```text

> @agenttrace/pi-browser-harness@0.1.0 test
> vitest --run test/trace-events.test.ts test/observation-view.test.ts test/assertions.test.ts test/case-builder.test.ts


 RUN  v4.0.4 /private/tmp/ai-web-testing-task10-red-round3-20261027170042-67178/pi-browser-harness

 ❯ test/trace-events.test.ts (24 tests | 1 failed) 8ms
     ✓ maps an authoritative assistant message to pi.message.completed and pi.usage 1ms
     ✓ emits usage at most once per assistant message 0ms
     ✓ ignores non-assistant message_end events 0ms
     ✓ preserves tool call ids on tool start and completion 0ms
     × carries a recoverable browser MCP failure as bounded completion evidence 4ms
     ✓ derives a compact browser observation from MCP structured content 0ms
     ✓ derives a browser observation from the Pi MCP adapter result envelope 0ms
     ✓ unwraps only the exact browser-harness Pi MCP adapter envelope 0ms
     ✓ preserves an ambiguous direct result with domain structuredContent 0ms
     ✓ derives objective finish evidence from finish_case structured content 0ms
     ✓ derives objective finish evidence from the Pi MCP adapter result envelope 0ms
     ✓ ignores passing finish evidence from any other tool 0ms
     ✓ requires valid object case and dry-run artifacts: 'missing case' 0ms
     ✓ requires valid object case and dry-run artifacts: 'null case' 0ms
     ✓ requires valid object case and dry-run artifacts: 'array case' 0ms
     ✓ requires valid object case and dry-run artifacts: 'missing dry run' 0ms
     ✓ requires valid object case and dry-run artifacts: 'null dry run' 0ms
     ✓ requires valid object case and dry-run artifacts: 'array dry run' 0ms
     ✓ requires valid object case and dry-run artifacts: 'invalid dry-run status' 0ms
     ✓ records a failed dry run as a failed browser case verdict 0ms
     ✓ records a nested failed dry run as a failed browser case verdict 0ms
     ✓ records a nested error dry run as a failed browser case verdict 0ms
     ✓ emits harness.settled only on agent_settled with the last assistant text 0ms
     ✓ harness.settled carries an empty assistant text when no message completed 0ms
 ❯ test/observation-view.test.ts (6 tests | 1 failed) 22ms
     ✓ enforces all compact bounds and reports stable omitted counts 11ms
     ✓ retains a goal-relevant candidate, target, and nested scope ahead of ads 3ms
     ✓ retains entries related to the latest result ahead of unrelated controls 1ms
     ✓ preserves source truncation and omits raw candidate locators 0ms
     × preserves a nullable screenshot path from the observation 3ms
     ✓ matches Chinese, Japanese, and Korean goal text 3ms
 ✓ test/assertions.test.ts (10 tests) 3ms
 ❯ test/case-builder.test.ts (10 tests | 2 failed) 280ms
     ✓ open_page must be the first action and use an absolute URL 2ms
     ✓ every target action requires at least one objective expectation 25ms
     ✓ a failed authoring action never commits a step 10ms
     ✓ a successful action commits only after the worker reports passed 6ms
     ✓ assert_text and assert_url evaluate the current observation without mutating the page 3ms
     × failed assertions retain the current compact page and committed steps 6ms
     ✓ navigation retries transient worker failures three times and reports navigation_failed 211ms
     ✓ overload pages report site_overloaded 4ms
     ✓ finish_case with a passing dry run returns case and execution 7ms
     × finish_case retains the authored case when its dry run fails 5ms

 Test Files  3 failed | 1 passed (4)
      Tests  4 failed | 46 passed (50)
   Start at  17:00:54
   Duration  511ms (transform 307ms, setup 0ms, collect 384ms, tests 312ms, environment 0ms, prepare 19ms)
```

stderr:

```text

⎯⎯⎯⎯⎯⎯⎯ Failed Tests 4 ⎯⎯⎯⎯⎯⎯⎯

 FAIL  test/case-builder.test.ts > CaseBuilder transactional authoring > failed assertions retain the current compact page and committed steps
AssertionError: expected undefined to be 'run_1/obs_failed.png' // Object.is equality

- Expected:
"run_1/obs_failed.png"

+ Received:
undefined

 ❯ test/case-builder.test.ts:365:52
    363|       expect(outcome.result.ok).toBe(false);
    364|       expect(outcome.result.error).toBe("condition_unmet");
    365|       expect(outcome.result.page?.screenshot_path).toBe("run_1/obs_fai…
       |                                                    ^
    366|       expect(outcome.result.steps).toEqual([
    367|         expect.objectContaining({ index: 0, action: "goto" }),

⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯[1/4]⎯

 FAIL  test/case-builder.test.ts > CaseBuilder transactional authoring > finish_case retains the authored case when its dry run fails
AssertionError: Target cannot be null or undefined.
 ❯ test/case-builder.test.ts:549:34
    547|
    548|     expect(finished.result.ok).toBe(false);
    549|     expect(finished.case?.steps).toHaveLength(1);
       |                                  ^
    550|     expect(finished.dryRun?.status).toBe("failed");
    551|   });

⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯[2/4]⎯

 FAIL  test/observation-view.test.ts > buildPageView > preserves a nullable screenshot path from the observation
AssertionError: expected undefined to be 'run_1/obs_1.png' // Object.is equality

- Expected:
"run_1/obs_1.png"

+ Received:
undefined

 ❯ test/observation-view.test.ts:240:7
    238|         null,
    239|       ).screenshot_path,
    240|     ).toBe("run_1/obs_1.png")
       |       ^
    241|     expect(buildPageView(observation(), "", null).screenshot_path).toB…
    242|   });

⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯[3/4]⎯

 FAIL  test/trace-events.test.ts > mapPiEvent > carries a recoverable browser MCP failure as bounded completion evidence
AssertionError: expected { type: 'pi.tool.completed', …(1) } to deeply equal { type: 'pi.tool.completed', …(1) }

- Expected
+ Received

  {
    "payload": {
      "is_error": false,
-     "structured_result": {
-       "detail": "the text \"DoesNotExist\" is not present",
-       "error": "condition_unmet",
-       "ok": false,
-       "page": {
-         "screenshot_path": "run_1/obs_failed.png",
-         "title": "Shop",
-         "url": "https://shop.test/",
-       },
-       "steps": [
-         {
-           "action": "goto",
-           "index": 0,
-           "intent": "open shop",
-         },
-       ],
-     },
      "tool_call_id": "call_failed",
      "tool_name": "mcp__browser_harness__assert_text",
    },
    "type": "pi.tool.completed",
  }

 ❯ test/trace-events.test.ts:154:23
    152|     );
    153|
    154|     expect(events[0]).toEqual({
       |                       ^
    155|       type: "pi.tool.completed",
    156|       payload: {

⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯⎯[4/4]⎯
```

Exit status: `1`.

#### Pi Typecheck

```bash
cd pi-browser-harness
npm run typecheck
```

stdout:

```text

> @agenttrace/pi-browser-harness@0.1.0 typecheck
> tsc --noEmit

test/case-builder.test.ts(365,35): error TS2339: Property 'screenshot_path' does not exist on type 'PageView'.
test/mcp-server.test.ts(163,11): error TS2353: Object literal may only specify known properties, and 'screenshot_path' does not exist in type 'PageView'.
test/observation-view.test.ts(239,9): error TS2339: Property 'screenshot_path' does not exist on type 'PageView'.
test/observation-view.test.ts(241,51): error TS2339: Property 'screenshot_path' does not exist on type 'PageView'.
```

stderr:

```text
```

Exit status: `2`.

#### Go Runtime Focused Tests

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test -count=1 -p 1 ./internal/runtime -run 'TestPostgresPiTraceStore.*(Tool|Finish)' -v
```

stdout:

```text
=== RUN   TestPostgresPiTraceStorePersistsFinishCaseEvidence
--- PASS: TestPostgresPiTraceStorePersistsFinishCaseEvidence (0.49s)
=== RUN   TestPostgresPiTraceStorePersistsFailedBrowserToolEvidence
    pi_store_test.go:207: operation results = 0, want 1
--- FAIL: TestPostgresPiTraceStorePersistsFailedBrowserToolEvidence (0.24s)
=== RUN   TestPostgresPiTraceStoreDefersFinishCaseResultToCaseCompletion
--- PASS: TestPostgresPiTraceStoreDefersFinishCaseResultToCaseCompletion (0.32s)
FAIL
FAIL	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/runtime	1.988s
FAIL
```

stderr:

```text
```

Exit status: `1`.

#### Go Protocol Focused Tests

```bash
cd backend
go test -count=1 ./internal/piharness -v
```

stdout:

```text
FAIL	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness [build failed]
FAIL
```

stderr:

```text
# github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness [github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness.test]
internal/piharness/protocol_test.go:265:20: payload.StructuredResult undefined (type ToolCompletedPayload has no field or method StructuredResult)
internal/piharness/protocol_test.go:266:46: payload.StructuredResult undefined (type ToolCompletedPayload has no field or method StructuredResult)
```

Exit status: `1`.

#### Python Focused Tests

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest \
  integration.v3.test_pi_harness_scenario.FailedEvidenceTest \
  integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest -v
```

stdout:

```text
```

stderr:

```text
test_pi_harness_scenario (unittest.loader._FailedTest) ... ERROR
test_pi_harness_scenario (unittest.loader._FailedTest) ... ERROR
test_pi_harness_scenario (unittest.loader._FailedTest) ... ERROR

======================================================================
ERROR: test_pi_harness_scenario (unittest.loader._FailedTest)
----------------------------------------------------------------------
ImportError: Failed to import test module: test_pi_harness_scenario
Traceback (most recent call last):
  File "/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/3.9/lib/python3.9/unittest/loader.py", line 154, in loadTestsFromName
    module = __import__(module_name)
  File "/private/tmp/ai-web-testing-task10-red-round3-20261027170042-67178/integration/v3/test_pi_harness_scenario.py", line 17, in <module>
    from pi_harness_scenario import (
ImportError: cannot import name 'run_has_failed_assertion_evidence' from 'pi_harness_scenario' (/private/tmp/ai-web-testing-task10-red-round3-20261027170042-67178/integration/v3/pi_harness_scenario.py)


======================================================================
ERROR: test_pi_harness_scenario (unittest.loader._FailedTest)
----------------------------------------------------------------------
ImportError: Failed to import test module: test_pi_harness_scenario
Traceback (most recent call last):
  File "/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/3.9/lib/python3.9/unittest/loader.py", line 154, in loadTestsFromName
    module = __import__(module_name)
  File "/private/tmp/ai-web-testing-task10-red-round3-20261027170042-67178/integration/v3/test_pi_harness_scenario.py", line 17, in <module>
    from pi_harness_scenario import (
ImportError: cannot import name 'run_has_failed_assertion_evidence' from 'pi_harness_scenario' (/private/tmp/ai-web-testing-task10-red-round3-20261027170042-67178/integration/v3/pi_harness_scenario.py)


======================================================================
ERROR: test_pi_harness_scenario (unittest.loader._FailedTest)
----------------------------------------------------------------------
ImportError: Failed to import test module: test_pi_harness_scenario
Traceback (most recent call last):
  File "/Library/Developer/CommandLineTools/Library/Frameworks/Python3.framework/Versions/3.9/lib/python3.9/unittest/loader.py", line 154, in loadTestsFromName
    module = __import__(module_name)
  File "/private/tmp/ai-web-testing-task10-red-round3-20261027170042-67178/integration/v3/test_pi_harness_scenario.py", line 17, in <module>
    from pi_harness_scenario import (
ImportError: cannot import name 'run_has_failed_assertion_evidence' from 'pi_harness_scenario' (/private/tmp/ai-web-testing-task10-red-round3-20261027170042-67178/integration/v3/pi_harness_scenario.py)


----------------------------------------------------------------------
Ran 3 tests in 0.000s

FAILED (errors=3)
```

Exit status: `1`.

### GREEN

The same five commands were rerun on current `HEAD` `7731ece`.

#### Pi Focused Tests

```bash
cd pi-browser-harness
npm test -- --run \
  test/trace-events.test.ts \
  test/observation-view.test.ts \
  test/assertions.test.ts \
  test/case-builder.test.ts
```

stdout:

```text

> @agenttrace/pi-browser-harness@0.1.0 test
> vitest --run test/trace-events.test.ts test/observation-view.test.ts test/assertions.test.ts test/case-builder.test.ts


 RUN  v4.0.4 /Users/bytedance/project/AI_Web_Testing/.worktrees/pi-browser-harness-migration/pi-browser-harness

 ✓ test/trace-events.test.ts (24 tests) 5ms
 ✓ test/observation-view.test.ts (6 tests) 20ms
 ✓ test/assertions.test.ts (10 tests) 3ms
 ✓ test/case-builder.test.ts (10 tests) 267ms

 Test Files  4 passed (4)
      Tests  50 passed (50)
   Start at  17:01:15
   Duration  497ms (transform 286ms, setup 0ms, collect 367ms, tests 295ms, environment 0ms, prepare 14ms)
```

stderr:

```text
```

Exit status: `0`.

#### Pi Typecheck

```bash
cd pi-browser-harness
npm run typecheck
```

stdout:

```text

> @agenttrace/pi-browser-harness@0.1.0 typecheck
> tsc --noEmit
```

stderr:

```text
```

Exit status: `0`.

#### Go Runtime Focused Tests

```bash
cd backend
AGENTTRACE_TEST_POSTGRES_DSN='postgres://agenttrace:agenttrace@127.0.0.1:5432/agenttrace_test?sslmode=disable' \
  go test -count=1 -p 1 ./internal/runtime -run 'TestPostgresPiTraceStore.*(Tool|Finish)' -v
```

stdout:

```text
=== RUN   TestPostgresPiTraceStorePersistsFinishCaseEvidence
--- PASS: TestPostgresPiTraceStorePersistsFinishCaseEvidence (0.22s)
=== RUN   TestPostgresPiTraceStorePersistsFailedBrowserToolEvidence
--- PASS: TestPostgresPiTraceStorePersistsFailedBrowserToolEvidence (0.20s)
=== RUN   TestPostgresPiTraceStoreDefersFinishCaseResultToCaseCompletion
--- PASS: TestPostgresPiTraceStoreDefersFinishCaseResultToCaseCompletion (0.36s)
PASS
ok  	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/runtime	1.523s
```

stderr:

```text
```

Exit status: `0`.

#### Go Protocol Focused Tests

```bash
cd backend
go test -count=1 ./internal/piharness -v
```

stdout:

```text
=== RUN   TestRunWritesRequestToStdinAndClosesIt
--- PASS: TestRunWritesRequestToStdinAndClosesIt (0.01s)
=== RUN   TestRunCallbackCalledSynchronouslyInSequenceOrder
--- PASS: TestRunCallbackCalledSynchronouslyInSequenceOrder (0.01s)
=== RUN   TestRunEnforcesStdoutLineSizeBound
--- PASS: TestRunEnforcesStdoutLineSizeBound (0.01s)
=== RUN   TestRunRetainsStderrAsBoundedDiagnostics
--- PASS: TestRunRetainsStderrAsBoundedDiagnostics (0.01s)
=== RUN   TestRunRejectsStderrOverflow
--- PASS: TestRunRejectsStderrOverflow (0.01s)
=== RUN   TestRunContextCancellationSendsInterruptThenKills
--- PASS: TestRunContextCancellationSendsInterruptThenKills (0.14s)
=== RUN   TestRunNonZeroExitAfterFailedReturnsTypedHarnessFailure
--- PASS: TestRunNonZeroExitAfterFailedReturnsTypedHarnessFailure (0.02s)
=== RUN   TestRunZeroExitBeforeTerminalReturnsErrMissingTerminal
--- PASS: TestRunZeroExitBeforeTerminalReturnsErrMissingTerminal (0.02s)
=== RUN   TestRunNonZeroExitBeforeTerminalIsPrematureExit
--- PASS: TestRunNonZeroExitBeforeTerminalIsPrematureExit (0.01s)
=== RUN   TestRunMalformedJSONStopsProcessing
--- PASS: TestRunMalformedJSONStopsProcessing (0.01s)
=== RUN   TestRunDuplicateSequenceStopsProcessing
--- PASS: TestRunDuplicateSequenceStopsProcessing (0.01s)
=== RUN   TestRunDoesNotInvokeShell
--- PASS: TestRunDoesNotInvokeShell (0.00s)
=== RUN   TestRunCleanExitDoesNotSignalInterrupt
--- PASS: TestRunCleanExitDoesNotSignalInterrupt (0.52s)
=== RUN   TestRunTimeoutReturnsErrTimeout
--- PASS: TestRunTimeoutReturnsErrTimeout (0.05s)
=== RUN   TestRequestRoundTrip
--- PASS: TestRequestRoundTrip (0.00s)
=== RUN   TestRequestRejectsUnsupportedProtocolVersion
--- PASS: TestRequestRejectsUnsupportedProtocolVersion (0.00s)
=== RUN   TestDecoderAcceptsValidTerminalStream
--- PASS: TestDecoderAcceptsValidTerminalStream (0.00s)
=== RUN   TestDecoderRejectsRunIDMismatch
--- PASS: TestDecoderRejectsRunIDMismatch (0.00s)
=== RUN   TestDecoderRejectsAttemptIDMismatch
--- PASS: TestDecoderRejectsAttemptIDMismatch (0.00s)
=== RUN   TestDecoderRejectsSequenceNotStartingAtOne
--- PASS: TestDecoderRejectsSequenceNotStartingAtOne (0.00s)
=== RUN   TestDecoderRejectsDuplicateSequence
--- PASS: TestDecoderRejectsDuplicateSequence (0.00s)
=== RUN   TestDecoderRejectsOutOfOrderSequence
--- PASS: TestDecoderRejectsOutOfOrderSequence (0.00s)
=== RUN   TestDecoderRejectsInvalidTimestamp
=== RUN   TestDecoderRejectsInvalidTimestamp/not_rfc3339
=== RUN   TestDecoderRejectsInvalidTimestamp/non-utc_offset
=== RUN   TestDecoderRejectsInvalidTimestamp/empty
--- PASS: TestDecoderRejectsInvalidTimestamp (0.00s)
    --- PASS: TestDecoderRejectsInvalidTimestamp/not_rfc3339 (0.00s)
    --- PASS: TestDecoderRejectsInvalidTimestamp/non-utc_offset (0.00s)
    --- PASS: TestDecoderRejectsInvalidTimestamp/empty (0.00s)
=== RUN   TestDecoderAcceptsUTCTimestampVariants
=== RUN   TestDecoderAcceptsUTCTimestampVariants/2026-10-26T12:34:56.789Z
=== RUN   TestDecoderAcceptsUTCTimestampVariants/2026-10-26T12:34:56Z
=== RUN   TestDecoderAcceptsUTCTimestampVariants/2026-10-26T12:34:56.789012345Z
=== RUN   TestDecoderAcceptsUTCTimestampVariants/2026-10-26T12:34:56+00:00
--- PASS: TestDecoderAcceptsUTCTimestampVariants (0.00s)
    --- PASS: TestDecoderAcceptsUTCTimestampVariants/2026-10-26T12:34:56.789Z (0.00s)
    --- PASS: TestDecoderAcceptsUTCTimestampVariants/2026-10-26T12:34:56Z (0.00s)
    --- PASS: TestDecoderAcceptsUTCTimestampVariants/2026-10-26T12:34:56.789012345Z (0.00s)
    --- PASS: TestDecoderAcceptsUTCTimestampVariants/2026-10-26T12:34:56+00:00 (0.00s)
=== RUN   TestDecoderRejectsUnknownEventType
--- PASS: TestDecoderRejectsUnknownEventType (0.00s)
=== RUN   TestDecoderRejectsSecondTerminalRecord
--- PASS: TestDecoderRejectsSecondTerminalRecord (0.00s)
=== RUN   TestDecoderRejectsMalformedJSON
--- PASS: TestDecoderRejectsMalformedJSON (0.00s)
=== RUN   TestDecoderTypedPayloads
--- PASS: TestDecoderTypedPayloads (0.00s)
=== RUN   TestToolCompletedPayloadPreservesStructuredResult
--- PASS: TestToolCompletedPayloadPreservesStructuredResult (0.00s)
PASS
ok  	github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness	1.371s
```

stderr:

```text
```

Exit status: `0`.

#### Python Focused Tests

```bash
python3 -m unittest \
  integration.v3.test_pi_harness_scenario.TraceShapeTest \
  integration.v3.test_pi_harness_scenario.FailedEvidenceTest \
  integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest -v
```

stdout:

```text
```

stderr:

```text
test_rejects_raw_steps_projected_as_unknown_tool (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_rejects_unlinked_operation_evidence (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_reports_missing_shape_when_counts_are_zero (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_requires_model_tools_usage_linked_operation_and_execution_steps (integration.v3.test_pi_harness_scenario.TraceShapeTest) ... ok
test_accepts_a_later_qualifying_failed_assertion (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_rejects_missing_screenshot_path (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_requires_failed_assert_text_with_stable_error_attempt_and_screenshot (integration.v3.test_pi_harness_scenario.FailedEvidenceTest) ... ok
test_accepts_distinct_attempts_and_run_scoped_operation_artifacts (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_absolute_and_parent_traversal_artifact_paths (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_operation_artifact_outside_its_run_scope (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok
test_rejects_shared_harness_attempt_ids (integration.v3.test_pi_harness_scenario.ConcurrentIsolationTest) ... ok

----------------------------------------------------------------------
Ran 11 tests in 0.004s

OK
```

Exit status: `0`.

### Round 3 Scope

No Docker or Compose command was run. No production or test file was changed.
