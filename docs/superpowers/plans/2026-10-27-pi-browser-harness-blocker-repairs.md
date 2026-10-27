# Pi Browser Harness Blocker Repairs Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development to implement the independent tasks below. Follow a failing regression test, minimal fix, and focused verification for each task.

**Goal:** Fix cancellation authority, malformed nested execution evidence, and sequential browser MCP registration.

**Architecture:** PostgreSQL chooses and returns the terminal verdict under the run row lock. The existing Go protocol validator validates nested evidence objects. A single MCP wrapper factory decorates registration through an API proxy.

**Tech Stack:** Go, PostgreSQL/pgx, TypeScript, Pi SDK 1.4.0, Vitest, Python/Playwright.

**Spec:** `docs/superpowers/specs/2026-10-27-pi-browser-harness-blocker-repairs-design.md`

**Status:** Implemented and verified on 2026-10-27 in the existing `pi-browser-harness-migration` worktree.

## Global Constraints

- Changes remain local to the three verified blockers.
- No generic state machine, generated schemas, Pi internal patches, or unrelated refactors.
- All verification uses local services; Docker and Compose are out of scope.
- Retain the existing TypeScript execution-result contract and Go protocol-validation error.
- Only `mcp__browser_harness__*` tools receive sequential execution.

### Task 1: Authoritative terminal persistence and runtime verdict

**Files:** `backend/internal/postgres/harness.go`, `harness_integration_test.go`, `backend/internal/runtime/pi_store.go`, `pi_runner.go`, `pi_store_test.go`, `pi_runner_test.go`.

**Interfaces:** Change `FinishHarnessAttempt` and `CompleteHarnessRun` to return `(domain.RunStatus, error)`. All callers consume the committed status.

- [x] Add an integration regression that holds the run row lock, starts successful terminal completion, commits durable cancellation, then releases completion. Assert literal `cancelled` for the return value, run, root span, harness attempt, and terminal event; assert a cancellation error payload.
- [x] Run the new regression against an isolated local PostgreSQL database and observe the current successful outcome incorrectly overwrite cancellation.
- [x] Under the existing terminal transaction's run row lock, derive the effective status before terminal writes:

```go
if existingStatus == domain.RunCancelling || existingStatus == domain.RunCancelled {
    finish.Status = domain.RunCancelled
}
```

- [x] Derive the terminal event and cancellation payload using that status inside persistence. Preserve idempotency checks using the effective event; return the committed status through `PostgresPiTraceStore`.
- [x] Have `PiRunner` base its final success/error branch on the returned status. Add a regression for cancellation committed after the runner's final cancellation read.
- [x] Verify affected callers compile and focused PostgreSQL/runtime tests pass.
- [x] Preserve retryable errors when terminal persistence fails, even if its returned candidate status is cancelled. Verify three failing regressions before fixing the error/status ordering.

### Task 2: Nested Go protocol contract validation

**Files:** `backend/internal/piharness/protocol.go`, `protocol_test.go`. Reference: `pi-browser-harness/src/browser/contract.ts`.

**Interfaces:** Keep parser signatures and existing error types unchanged.

- [x] Add table cases for scalar/null elements, missing required fields, and wrong field types in execution step `conditions`, evidence `console`, and evidence `network`. These three TypeScript structures have no optional fields; nullable `detail` and `status` remain required.
- [x] Run `go test ./internal/piharness -run 'Test.*Nested|Test.*Execution' -count=1` and observe malformed objects accepted.
- [x] Add small object validation helpers matching the TypeScript fields and call them from the existing array validation. Reject the first malformed nested element; preserve valid nullable fields and existing extra-key acceptance.
- [x] Run `go test ./internal/piharness -count=1`.

### Task 3: Sequential MCP wrapper through the real Pi loader

**Files:** `pi-browser-harness/src/extension.ts`, `src/pi-session.ts`, `test/extension.test.ts`, and related session test only if its existing factory expectation changes.

**Interfaces:** The sequential MCP factory invokes Pi's built-in MCP factory itself; the lifecycle extension remains separate.

- [x] Add a real resource-loader/factory integration regression with distinct factory API objects. Exercise built-in MCP discovery and assert browser tool definitions are sequential while unrelated MCP definitions retain their configuration.
- [x] Run `npm test -- --run test/extension.test.ts` and observe missing sequential registration.
- [x] Use a stable API proxy forwarding all properties except `registerTool`:

```ts
const proxy = new Proxy(pi, {
  get(target, key, receiver) {
    if (key !== "registerTool") return Reflect.get(target, key, receiver);
    return registerBrowserTool;
  },
});
```

- [x] `registerBrowserTool` forwards the original definition unchanged except browser-prefixed definitions receive `executionMode: "sequential"`. Invoke `createMcpExtension()` with the proxy and remove the separate bare MCP factory from the session loader.
- [x] Run extension/session tests, all harness tests, typecheck, and build.

### Task 4: Review and full local acceptance

- [x] Review each task for spec compliance and code quality; resolve material findings.
- [x] Run all Go tests with isolated local PostgreSQL, harness tests/typecheck/build, worker tests, Web tests/build, and pure Python scenario/runner tests.
- [x] Run existing Pi live acceptance against local API/runtime/PostgreSQL/Kafka/Redis/worker/fixture services, including canonical pass, assertion failure evidence, cancellation, concurrent isolation, and failed-run rerun.
- [x] Check the final diff for whitespace errors and scope, and report actual verification results. Retain changes in the existing migration worktree without merging or publishing.

## Verification results

| Check | Result |
|---|---|
| All Go packages, `go test -p 1 ./... -count=1` | Passed with isolated native PostgreSQL and local Kafka; rerun after the persistence-error fix |
| Cancellation row-lock regression | Failed before fix; passed after fix, including 10 consecutive repetitions |
| Runtime completion-error regression | Three expected failures before fix; three passes after fix; runtime race suite passed |
| Nested execution evidence | 80 rejection cases and 11 valid leaf cases passed |
| Harness, `npm test -- --run` | 169 passed across 15 test files |
| Harness typecheck/build | Passed |
| Worker unittest discovery | 98 passed |
| Web unit tests/build | 53 passed; build passed |
| Python scenario unit tests / verification-runner tests | 47 / 3 passed |
| Magpie package tests | 30 passed |
| Final `integration/v3/pi_harness_scenario.py` | Passed, batch `d90323c5be4a` |
| Task reviews and final code review | Approved after fixing the persistence-error finding |
| Diff whitespace and API/dependency health | Clean; all required local services healthy |

Final live acceptance verified canonical success with all seven required Trace features, failed assertion evidence, cancelled run and harness attempt, two isolated concurrent runs, and a failed rerun with a fresh attempt.

Verification used native local services throughout. No Docker or Compose command ran. Redis's integration tests that require the Compose-only hostname and the Docker-dependent Web E2E suite were not run; native Redis was exercised by the live acceptance flow. Intermediate logs remain outside the repository under `/tmp`.
