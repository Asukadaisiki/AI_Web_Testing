# Shared Pi Harness Implementation Plan

> **For agentic workers:** Use subagent-driven-development for the independent TypeScript server and Go transport tasks. The user approved implementation in this session; proceed without another approval gate.

**Goal:** Run concurrent, isolated Pi sessions inside one long-lived Harness per Go runtime, with attempt-scoped routing, cancellation and awaited resource cleanup.

**Architecture:** Keep the existing `pi-browser-harness.v1` RunRequest and event payloads. Add a bidirectional daemon transport selected by `--server`. The Go client supervises one lazily started child and routes each attempt through its own strict decoder; the TypeScript server owns a session registry and emits a release acknowledgement after awaited cleanup.

**User refinement:** Langfuse, Pi and Harness are separate replaceable boundaries. The session host depends only on an execution-session factory. The Pi adapter converts an SDK-free Harness definition into Pi tools/resources; the browser Harness is the default implementation. Startup can select another Harness module without editing Pi, the transport or Langfuse. Langfuse consumes persisted normalized events asynchronously.

**Tech Stack:** Go, Node.js, TypeScript, Pi SDK 1.4.0, stdio JSONL, Vitest.

**Spec:** User-approved shared Harness / per-Attempt Pi session design in the current conversation.

## Global Constraints

- Keep Pi SDK pinned to `1.4.0`.
- Preserve existing browser tools, event payload validation and PostgreSQL trace semantics.
- Keep the existing one-shot CLI for direct integration fixtures; production uses `--server`.
- Session storage, settings and extension runtime remain per attempt.
- Do not commit, overwrite pre-existing untracked plans, or print credentials.
- An attempt's concurrency slot is held through `harness.released`, not merely `harness.settled`.
- The session host and Harness definitions must not import Pi SDK or Langfuse.
- New `harness.settled` payloads include optional `verdict: "succeeded" | "failed"` computed by the selected Harness. Go trusts that policy result; old payloads retain their browser-evidence fallback.
- Active attempts retain the Harness selected at creation; startup module selection affects new host processes.

## Transport Contract

Host commands:

```typescript
type HarnessCommand =
  | { type: "run"; request: RunRequest }
  | { type: "cancel"; run_id: string; attempt_id: string }
  | { type: "shutdown" };
```

Server controls (separate from sequenced trace events):

```typescript
type HarnessControl =
  | { type: "harness.ready" }
  | { type: "harness.released"; run_id: string; attempt_id: string }
  | { type: "harness.protocol_error"; error: string };
```

- Emit `harness.ready` once the command reader can accept requests.
- Register an attempt synchronously before awaiting session creation, so cancellation during initialization is retained.
- Run attempts concurrently; never await an entire run in the command reader.
- Emit existing trace events with independent sequences, then exactly one release control after cleanup.
- Treat malformed commands as fatal transport errors; duplicate active attempt submissions are rejected by the host before sending.
- Unknown or already released cancel commands are harmless.
- EOF, shutdown and process signals cancel and await all active attempts.
- Each Run's context cancellation sends only its own cancel command. A grace timeout or transport failure fails all active attempts and terminates the shared child; later requests may start a new child.
- No automatic replay within the client; durable runtime retry creates a fresh Attempt.

## Task 1: TypeScript Session Server and Cleanup

**Files:** `pi-browser-harness/src/server.ts` (including SDK-free lifecycle interfaces), Harness definition/default modules, `main.ts`, `pi-session.ts`, related tests.

**Produces:** `serverMain(options?)`, the `--server` entrypoint and an awaited, idempotent `BrowserSession.close()`.

- [x] Add failing tests for overlapping attempts, independent sequences, targeted cancellation during initialization, duplicate cancellation, failure isolation, release after cleanup, and shutdown draining.
- [x] Implement the registry and control protocol, with session factories injectable for tests.
- [x] Extract an SDK-free Harness definition and browser default; keep the generic Pi adapter separate from prompt/tool/completion policy.
- [x] Add a startup Harness module selection boundary and prove alternate Harness prompt/tools/completion through the same host and Pi adapter.
- [x] Keep mutable model registrations from contaminating other attempts; use an independent ModelRuntime per attempt initially.
- [x] Give Pi sessions explicit attempt IDs with `SessionManager.inMemory(cwd, { id: request.attempt_id })`.
- [x] Explicitly emit session shutdown before disposing and await MCP/browser cleanup.
- [x] Run focused tests, typecheck and build.

## Task 2: Go Shared Process Client

**Files:** `backend/internal/piharness/shared.go`, `shared_test.go`.

**Produces:** `SharedProcessRunner` with the same command/config fields as `ProcessRunner`, `Run(context.Context, Request, func(Event)) error`, and `Close() error`.

- [x] Add failing real-child tests for process reuse, interleaved events, independent attempt decoders, targeted cancellation, cleanup acknowledgements, crash/restart and shutdown.
- [x] Supervise one lazily started child with bounded stdout/stderr, ready handshake, serialized command writes and per-attempt queues.
- [x] Invoke each attempt's callback sequentially from its own Run goroutine; do not let another attempt's slow persistence block the reader.
- [x] Wait for release on successful or cancelled attempts; enforce cancellation grace and attempt timeout.
- [x] Reject duplicate active attempts and cancelled contexts before starting work.
- [x] Run Go package tests with the race detector.

## Task 3: Runtime Wiring and Integration

**Files:** `backend/cmd/agenttrace-runtime/main.go`, `main_test.go`, `backend/internal/runtime/pi_runner.go`, `pi_runner_test.go`, architecture and operations docs.

- [x] Add failing wiring and queued-cancellation tests.
- [x] Wire `SharedProcessRunner` with `--server`; close it when the runtime exits.
- [x] Short-circuit durable queued cancellation before submitting a Run to the Harness.
- [x] Consume validated Harness-provided verdicts without requiring browser-case evidence from another Harness.
- [x] Exercise the built server with real Pi SDK and local deterministic model/MCP/HTTP fixtures, including concurrent Run cancellation and subsequent process reuse.
- [x] Review interfaces, failure propagation, attempt fencing and cleanup ordering.
- [x] Run affected Go/TypeScript suites and update current architecture/operation documentation.

## Verification results

- `npm test -- --run --maxWorkers=2`: 20 files, 197 tests passed. The earlier unrestricted parallel run timed out during concurrent compilation; the bounded rerun passed without increasing test timeouts.
- `npm run typecheck` and `npm run build`: passed.
- `go test ./...`: all backend packages passed; external-service integration tests retain their existing opt-in gates.
- `go test -race ./internal/piharness ./internal/runtime ./cmd/agenttrace-runtime -count=1`: passed.
- `AGENTTRACE_TEST_HARNESS_ENTRY=<absolute compiled main.js> go test -race ./internal/piharness -run TestSharedHostWithRealPiAndReplacementHarness -count=1 -v`: passed with actual Node, Pi and MCP transports.
- The actual browser MCP child regression confirms worker DELETE completes before child exit on stdin EOF.
- Final independent code review found no P1/P2 issues; `git diff --check` passed.
- Full Compose acceptance was not run because the local Docker daemon is unavailable.
