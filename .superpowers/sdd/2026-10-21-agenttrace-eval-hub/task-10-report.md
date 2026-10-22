# Task 10 Report: Three-Column Workspace And Typed Client

## Implementation Summary

- Replaced the v2 four-route console with one persistent AgentTrace workspace.
- Added a typed `/api/v1` client for projects, conversations, messages, runs,
  spans, agents, health, cancellation, responses, and SSE events.
- Added one selection state for `project`, `conversation`, `run`, and `span`.
  Those are the only query parameters written for restore/deep links.
- Added durable SSE sequence deduplication, named event listeners, explicit
  ephemeral event handling, and canonical run/conversation/trace refetches after
  terminal durable events.
- Added desktop left/center/right panes and narrow in-place pane tabs without
  route navigation.
- Added project/conversation creation and search, agent/model-authorized
  composition, duplicate-send protection, all v3 run-state blocks, cancellation,
  question/approval responses, Trace projection, keyboard Trace navigation, and
  selected-node details.
- Removed the obsolete pages and their v2-only components, hooks, event view, and
  router dependency.

## Files Changed

- Modified: `web/index.html`, `web/package.json`, `web/package-lock.json`,
  `web/src/App.tsx`, `web/src/api.ts`, `web/src/main.tsx`, `web/src/styles.css`.
- Added: `web/src/api.test.ts`.
- Added: `web/src/features/workspace/{Workspace,useWorkspace,workspace.test}.ts*`.
- Added: `web/src/features/projects/ProjectConversationPane.tsx`.
- Added: `web/src/features/conversation/{ConversationPane,Composer,MessageBlock}.tsx`.
- Added: `web/src/features/trace/{TracePane,TraceTree,TraceDetails,projectTrace,projectTrace.test}.ts*`.
- Deleted: `web/src/pages/*`, `web/src/components/*`,
  `web/src/hooks/*`, and `web/src/eventView.ts`; these were unreachable v2-only
  modules whose contracts would otherwise retain obsolete statuses and routes.

## TDD Evidence

### Required RED

Command:

```text
cd web && npm test -- --run
```

Result: expected failure, exit 1. Both suites failed because production modules
did not exist:

```text
Failed to load url ./useWorkspace
Failed to load url ./projectTrace
Test Files 2 failed
```

### Required GREEN

Focused command:

```text
cd web && npm test -- --run \
  src/features/workspace/workspace.test.ts \
  src/features/trace/projectTrace.test.ts
```

Result: exit 0, 2 files passed, 4 tests passed.

### Additional RED/GREEN

- Non-JSON HTTP error test failed with `The API returned invalid JSON` instead
  of retaining HTTP 404, then passed after error-order correction.
- Ephemeral token test failed because an inherited `lastEventId` became durable
  sequence 2, then passed after type-aware ephemeral handling.

## Final Verification

```text
cd web && npm test -- --run && npm run build
```

Result: exit 0. Vitest: 3 files passed, 6 tests passed. TypeScript and Vite:
1959 modules transformed; production build completed.

Additional checks:

```text
git diff --check
PASS

cd web && npm audit --omit=dev --cache /tmp/agenttrace-npm-cache
found 0 vulnerabilities
```

Headless Chrome shell review:

- `/tmp/agenttrace-desktop.png` at 1440x900.
- `/tmp/agenttrace-narrow-fixed.png` at 390x844 output size.
- Desktop columns were stable with no text overlap. Narrow mode retained the
  workspace and exposed pane controls in place.

## Self-Review

- Exact v3 statuses are used; no v2 status literals or workflow route paths remain.
- No callback URLs, credentials, or server-owned operation configuration are exposed.
- Composer reuses one idempotency key for retrying an unchanged draft and blocks
  concurrent duplicate sends.
- Named SSE events are registered in addition to `onmessage`; durable events are
  deduplicated and ephemeral token/error events remain unnumbered.
- Trace sibling updates retain a still-present selection. Trace nodes have stable
  labels, visible focus, and arrow/Home/End keyboard movement.
- Controls and panels use radii no greater than 8px, no gradients, no image assets,
  and no nested decorative cards.
- Query writing strips all keys except `project`, `conversation`, `run`, and `span`.
- Diff and searches found no router dependency or obsolete route/status artifacts.

## Concerns

- `GET /api/v1/runs/{id}/trace` currently exposes canonical spans only; it does not
  return `span_attempts`, `usage_records`, or operation evidence rows. The details
  pane renders usage/evidence if present in canonical input/output and explicitly
  reports unavailable attempt data rather than inventing it.
- Midscene visual automation could not run because its model environment variables
  are not configured. Isolated headless Chrome screenshots covered the empty/error
  shell only; Task 13 remains responsible for live populated desktop/narrow
  Playwright acceptance.

## Fix Round 1

### Findings Addressed

1. **Stale selection and live-refetch writes/errors**
   - Files: `web/src/features/workspace/useWorkspace.ts`,
     `web/src/features/workspace/useWorkspace.test.ts`.
   - Conversation and run loaders now associate a request sequence with the active
     selection. A stale completion, including a stale rejection, becomes a no-op;
     the newest request still propagates its real error.

2. **Canonical attempt, usage, and evidence details**
   - Files: `backend/internal/postgres/api.go`,
     `backend/internal/api/v3/{server.go,runs.go,server_test.go}`,
     `backend/internal/postgres/store_integration_test.go`,
     `web/src/{api.ts,api.test.ts}`, `web/src/features/trace/{TracePane.tsx,
     TraceDetails.tsx,TraceDetails.test.tsx,TraceOverview.tsx,
     TraceOverview.test.tsx}`, `web/src/styles.css`.
   - `GET /api/v1/runs/{id}/trace` now returns redacted, persisted span attempts,
     usage records, and operation evidence alongside spans. The selected-span pane
     filters by canonical `span_id`; the inline report uses only canonical usage and
     operation records. Attempt request/response bodies remain excluded.

3. **Inline report, feedback, execution, planner, and usage projections**
   - Files: `web/src/features/trace/{TraceOverview.tsx,TraceDetails.tsx,
     TracePane.tsx,TraceOverview.test.tsx,TraceDetails.test.tsx}`.
   - The right pane provides a run report, execution summary, usage totals,
     execution evidence, planner/case input, and persisted failure feedback without
     restoring or calling any obsolete v2 route.

4. **Successful-response runtime validation**
   - Files: `web/src/api.ts`, `web/src/api.test.ts`.
   - Every HTTP client endpoint now parses its successful response through
     field-level object/array/string/number/enum/JSON validators before state
     receives it. Malformed 2xx responses produce `ApiError` at the boundary.

### RED/GREEN Evidence

Inherited partial-state focused command:

```text
cd web && npm test -- --run src/api.test.ts src/features/workspace/useWorkspace.test.ts src/features/trace/TraceDetails.test.tsx src/features/trace/TraceOverview.test.tsx
```

Result: exit 1. `TraceOverview.test.tsx` could not find the separate
`1 passed` text node; Go focused tests already passed.

New regression RED command:

```text
cd web && npm test -- --run src/api.test.ts src/features/workspace/useWorkspace.test.ts src/features/trace/TraceOverview.test.tsx
```

Result: exit 1. The stale overlapping refresh surfaced `stale run failure`, and
a 200 agent payload missing `safety` resolved instead of failing validation.

GREEN command:

```text
cd web && npm test -- --run src/api.test.ts src/features/workspace/useWorkspace.test.ts src/features/trace/TraceDetails.test.tsx src/features/trace/TraceOverview.test.tsx
```

Result: exit 0, 4 files and 11 tests passed.

### Final Verification

```text
cd backend && go test ./...
```

Result: exit 0; all tested packages passed.

```text
cd web && npm_config_cache=/tmp/agenttrace-npm-cache npm test -- --run && npm_config_cache=/tmp/agenttrace-npm-cache npm run build
```

Result: exit 0; Vitest 6 files/15 tests passed; TypeScript and Vite production
build completed (1960 modules transformed).

```text
git diff --check
```

Result: exit 0.

### Self-Review

- Confirmed only `/api/v1` workspace endpoints are consumed; no v2 route was
  restored.
- Confirmed trace details derive from persisted canonical records and are scoped to
  the selected run/span; unavailable records render explicit absence messages.
- Confirmed stale completions and stale errors cannot replace current
  conversation/run state, while an unsuperseded request error remains visible.
- Confirmed all 2xx HTTP responses used by the typed client have runtime parsers.
- Residual limitation: end-to-end live populated-browser acceptance remains out of
  scope for this fix round; unit, integration, full backend, full web, build, and
  diff checks passed.
