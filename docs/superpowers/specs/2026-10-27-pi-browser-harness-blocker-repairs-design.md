# Pi Browser Harness Blocker Repairs Design

## Scope

Repair the three verified blockers remaining after the Pi browser harness
migration:

1. A durable cancellation can lose to a concurrent successful harness finish.
2. The Go protocol parser accepts malformed nested browser execution evidence.
3. Browser harness MCP tools are not reliably registered as sequential when Pi
   creates a separate extension API for each factory.

The changes must remain local to these defects. They will not add a generic
state machine, generate schemas, patch Pi internals, or refactor unrelated
runtime and persistence code. All verification uses local services; Docker and
Compose are out of scope.

## Cancellation Authority

`FinishHarnessAttempt` is the transaction boundary that owns the terminal
decision. After locking the run row, it derives one effective terminal status:

- If the locked run is `cancelling` or `cancelled`, the effective status is
  `cancelled`.
- Otherwise, the effective status is the harness outcome supplied by the
  runtime.

Every terminal write in that transaction uses the effective status: the run,
root span, harness attempt, and terminal event. The persistence API returns the
effective status through `PostgresPiTraceStore` to `PiRunner`, so the caller
cannot report success when the transaction committed cancellation.

The terminal event is derived from the effective result inside the persistence
boundary. A cancelled result carries a cancellation error payload even when the
incoming harness outcome was successful. This keeps the event payload and all
persisted statuses consistent.

A final cancellation read in `PiRunner` remains useful for stopping the process
early, but it is not authoritative for the terminal commit. Adding another
pre-commit read would leave the same race window and is therefore not the fix.

## Protocol Validation

The Go parser will mirror the existing TypeScript execution-result contract for
the nested values that currently accept arbitrary array elements:

- Dry-run `conditions`
- Evidence `console`
- Evidence `network`

Each array element must be an object with the required fields and field types
defined by the TypeScript parser. Optional fields, when present, must also have
the matching type. The Go parser returns the existing protocol-validation error
for the first malformed element.

The implementation stays in the current protocol validator. It will add only
the small typed validation helpers needed to express the three existing
structures; it will not introduce schema generation or a second contract
abstraction.

## Sequential MCP Registration

Pi invokes each extension factory with a distinct API object, so one factory
cannot mutate the API later received by a separately registered built-in MCP
factory.

The harness will register one wrapper factory for MCP loading. That wrapper
invokes the built-in MCP extension factory with a stable API proxy. The proxy
forwards every API member unchanged except `registerTool`: tools whose names
start with `mcp__browser_harness__` receive `executionMode: "sequential"`;
other tools retain their original configuration.

The browser server lifecycle extension remains a separate factory because it
does not depend on MCP tool registration. Pi's loader and built-in extension
code are not patched.

## Testing

Implementation follows one failing test per blocker before production changes:

1. A PostgreSQL integration test pauses terminal completion, records a durable
   cancellation, then verifies the committed run, root span, harness attempt,
   terminal event, and returned status are all cancelled.
2. Go protocol table tests supply malformed nested elements for `conditions`,
   `console`, and `network`, and verify each payload is rejected.
3. A Pi extension integration test runs the real resource-loader/factory path
   with distinct factory APIs and verifies only browser harness MCP tools are
   registered as sequential.

After the focused tests pass, run the affected Go and TypeScript suites, the
repository's full pure-local verification, and the existing live acceptance
flow. No Docker service is started.

## Completion Criteria

- Cancellation always wins when it is durable before the terminal transaction
  selects its effective status.
- All terminal persistence and the runtime return value agree on that status.
- Malformed nested execution evidence cannot produce a successful result.
- Every `mcp__browser_harness__*` tool is sequential under the real Pi extension
  loading behavior, without changing unrelated MCP tools.
- Focused, full local, and live acceptance checks pass.
