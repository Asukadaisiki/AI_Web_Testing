# Trace Derived Node Projection Design

**Date:** 2026-10-23

**Status:** Ready for review

## 1. Purpose

The current Trace API exposes only canonical spans. A completed browser operation is
therefore shown as one `operation` node even though its persisted
`operation_evidence.result` contains every executed browser step. This makes the
product Trace substantially coarser than the execution data already stored in
PostgreSQL.

This change adds a read-time projection:

- canonical spans remain unchanged;
- the Trace API derives display-only child nodes from persisted operation results;
- the frontend merges canonical spans and derived nodes into one execution tree;
- browser steps become independently selectable and inspectable;
- tools without a registered projector produce one `unknown_tool` child node.

The projection is a presentation model. It does not create new durable spans or
change runtime execution.

## 2. Decisions

### 2.1 Selected approach

Project operation results in the Go API when `GET /api/v1/runs/{id}/trace` is read.
Return derived nodes in a new top-level `derived_nodes` array, separate from the
canonical `spans` array.

This preserves the meaning of a canonical span while giving the UI the finer
granularity it needs.

### 2.2 Confirmed constraints

- Do not add a database table, migration, backfill, or write-time materialization.
- Do not change the runtime, Kafka events, Redis state, worker callback, or worker
  result schema.
- Do not mix derived nodes into `spans`.
- Do not add a new redaction or desensitization pass to the projector.
- Keep the existing redaction behavior of the other public Trace fields unchanged.
- A tool without a usable registered projector must produce a child node whose kind
  and name are exactly `unknown_tool`.
- Projection is based only on persisted operation evidence. It is not a token-level
  or in-progress step stream.

### 2.3 Rejected alternatives

**Persist every worker step as a span**

This would make step granularity durable, but it requires runtime and schema changes,
history migration decisions, idempotency rules, and additional write amplification.
The current requirement is display granularity, not a new canonical data model.

**Parse tool-private JSON in React**

This couples the browser UI to every worker schema and duplicates validation and
fallback behavior across clients. The API is the correct compatibility boundary.

**Replace canonical spans with projected nodes**

This would blur stored facts and read-time views, break existing span references, and
make attempts and usage attribution ambiguous.

## 3. Existing Data

For an operation step, the runtime already persists:

- a canonical `operation` span whose input contains the selected operation call;
- one operation evidence record linked by `span_id`;
- `operation_evidence.result`, which is the worker callback's `output`;
- `operation_evidence.artifacts` and `operation_evidence.error`.

For `browser_execute_case`, the result is an `ExecutionResult`:

```json
{
  "execution_id": "exec_...",
  "status": "passed",
  "started_at": "2026-10-23T10:00:00Z",
  "finished_at": "2026-10-23T10:00:03Z",
  "final_url": "https://example.test/cart",
  "steps": [
    {
      "index": 0,
      "action": "goto",
      "status": "passed",
      "started_at": "2026-10-23T10:00:00Z",
      "duration_ms": 540,
      "url_before": "about:blank",
      "url_after": "https://example.test/",
      "conditions": [],
      "evidence": {
        "screenshot_path": "session/exec_0.png",
        "console": [],
        "network": []
      },
      "error": null,
      "blocker": null,
      "hit_test": null,
      "recovery": []
    }
  ],
  "error": null
}
```

No additional storage is required to display these steps.

## 4. API Contract

`GET /api/v1/runs/{id}/trace` keeps its existing fields and adds
`derived_nodes`:

```json
{
  "spans": [],
  "attempts": [],
  "usage": [],
  "operation_evidence": [],
  "derived_nodes": [
    {
      "id": "derived:operation-span-1:step:0",
      "parent_span_id": "operation-span-1",
      "source_evidence_id": "evidence-1",
      "kind": "step",
      "name": "Step 1: goto",
      "status": "passed",
      "started_at": "2026-10-23T10:00:00Z",
      "ended_at": "2026-10-23T10:00:00.540Z",
      "data": {
        "index": 0,
        "action": "goto",
        "duration_ms": 540,
        "url_before": "about:blank",
        "url_after": "https://example.test/",
        "conditions": [],
        "error": null,
        "blocker": null,
        "hit_test": null,
        "recovery": []
      },
      "evidence": {
        "screenshot_path": "session/exec_0.png",
        "console": [],
        "network": []
      }
    }
  ]
}
```

### 4.1 Derived node fields

| Field | Rule |
| --- | --- |
| `id` | Stable display identity; never a database row ID. |
| `parent_span_id` | Canonical operation `span_id`; derived nodes are never roots. |
| `source_evidence_id` | Canonical operation evidence row that produced the node. |
| `kind` | `step` or `unknown_tool`. |
| `name` | Human-readable tree label. |
| `status` | Source status without normalization, such as `passed` or `failed`. |
| `started_at` | RFC 3339 timestamp selected by the projector. |
| `ended_at` | RFC 3339 timestamp or `null`. |
| `data` | Tool-independent JSON shown in the details panel. |
| `evidence` | Evidence JSON shown separately in the details panel. |

All fields except `ended_at` are required. `data` and `evidence` may be JSON `null`.
The frontend parser must reject malformed successful responses using the existing
`invalid_response` behavior.

### 4.2 Stable IDs

Browser step IDs use:

```text
derived:<operation-span-id>:step:<step-index>
```

The browser projector validates that step indexes are non-negative, unique, and match
their array positions. This makes IDs stable across refreshes and prevents duplicate
nodes.

The fallback ID uses:

```text
derived:<operation-span-id>:unknown_tool
```

Canonical span IDs cannot collide with derived IDs because the latter always have the
`derived:` prefix.

### 4.3 Unknown and unprojectable tools

The API emits exactly one fallback child when:

- the operation name has no registered projector;
- the operation name cannot be resolved;
- the registered projector rejects malformed or incompatible result JSON.

The fallback node is:

```json
{
  "id": "derived:operation-span-1:unknown_tool",
  "parent_span_id": "operation-span-1",
  "source_evidence_id": "evidence-1",
  "kind": "unknown_tool",
  "name": "unknown_tool",
  "status": "failed",
  "started_at": "2026-10-23T10:00:00Z",
  "ended_at": "2026-10-23T10:00:01Z",
  "data": {
    "result": {}
  },
  "evidence": {
    "artifacts": {},
    "error": {}
  }
}
```

Its timestamps come from the parent operation span when available, then from operation
evidence `created_at` and `updated_at`. The fallback preserves the stored result,
artifacts, and error for inspection. A projection problem must never fail the whole
Trace request.

Operation evidence that has no matching canonical parent span cannot be attached to
the tree. It remains in `operation_evidence` and produces no derived node.

## 5. Backend Design

### 5.1 Projection package

Add a focused package under `backend/internal/traceprojection`. It owns:

- the public-neutral derived node model;
- operation name resolution;
- the projector registry;
- the browser result parser and projector;
- the `unknown_tool` fallback.

The package must not query PostgreSQL or write state. The API handler supplies the
canonical operation span and its evidence record.

Conceptually:

```go
type Projector interface {
    Project(parent domain.Span, evidence Evidence) ([]Node, error)
}

type Registry map[string]Projector
```

The initial registry contains only `browser_execute_case`.

### 5.2 Operation name resolution

For each operation evidence record:

1. Find the canonical span whose `span_id` equals the evidence `span_id`.
2. Require that the parent span kind is `operation`.
3. Parse `span.input.name` as the operation name.
4. If `input.name` is absent, use `span.name`.
5. Look up that name in the registry.

Name resolution and projection are fail-soft. Any unresolved or rejected input uses
the `unknown_tool` fallback.

### 5.3 Browser projector

The browser projector decodes the complete result with `json.Decoder.UseNumber` and
rejects trailing JSON. It validates:

- the root is an object;
- `steps` is an array;
- every step is an object;
- `index`, `action`, `status`, `started_at`, `duration_ms`, and `evidence` exist with
  the expected JSON types;
- indexes match array positions and are unique;
- timestamps parse as RFC 3339;
- durations are non-negative.

For each step:

- `id` uses the parent span ID and step index;
- `name` is `Step <index + 1>: <action>`;
- `status` is copied unchanged;
- `started_at` is the step timestamp;
- `ended_at` is `started_at + duration_ms`;
- `data` contains the step object without `evidence` or the duplicate
  `started_at/status` display fields;
- `evidence` contains the complete step evidence object.

Unknown fields are preserved in `data`. This allows the worker contract to add
diagnostic fields without requiring a frontend change.

### 5.4 Redaction boundary

The projector does not call `redactTraceJSON`, filter keys, or mutate source JSON.
Derived `data` and `evidence` are built directly from the persisted operation result.

The existing redaction behavior for `spans`, `attempts`, `usage`, and
`operation_evidence` stays as it is. Removing or redesigning that existing behavior is
outside this change.

### 5.5 Handler integration

`handleGetTrace`:

1. Loads the existing repeatable-read `TraceDetails`.
2. Builds the current public canonical arrays without changing their shape.
3. Projects operation evidence against the loaded spans.
4. Adds `derived_nodes` to the response.

`GetTraceDetails`, the repository interface, and all SQL remain unchanged. Projection
order follows canonical operation span order, then step index, so the response is
deterministic.

## 6. Frontend Design

### 6.1 API types and parser

Add:

```ts
type DerivedTraceKind = 'step' | 'unknown_tool';

interface DerivedTraceNode {
  id: string;
  parent_span_id: string;
  source_evidence_id: string;
  kind: DerivedTraceKind;
  name: string;
  status: string;
  started_at: string;
  ended_at?: string | null;
  data: JSONValue;
  evidence: JSONValue;
}
```

`Trace` gains the required `derived_nodes: DerivedTraceNode[]` field. The API parser
validates every field rather than accepting untyped JSON.

This is an intentional additive API change within the current repository. Backend and
frontend deploy together; no compatibility shim for responses missing
`derived_nodes` is required.

### 6.2 Display tree model

Replace the current `TraceNode extends Span` assumption with a normalized union:

- canonical node: ID is `span.span_id`, source is the complete `Span`;
- derived node: ID is `derived.id`, source is the complete `DerivedTraceNode`;
- both expose normalized `id`, `parentId`, `kind`, `name`, `status`,
  `startedAt`, `endedAt`, and `children`.

`projectTrace(spans, derivedNodes)` creates all nodes before attaching parents, so API
array order does not matter. A missing parent leaves the node out of the displayed
tree rather than promoting a derived node to a root.

Canonical orphan behavior remains unchanged: a canonical span with no known parent is
a root.

The URL query key remains `span` for backward compatibility, but its value becomes a
display node ID. Existing canonical deep links therefore continue to work, while a
derived node can be selected with its stable `derived:` ID.

### 6.3 Workspace state

`useWorkspace` stores `derivedNodes` beside spans, attempts, usage, and operation
evidence.

On every accepted run load:

- replace all five Trace collections atomically from the same response;
- project `trace` from `spans` and `derivedNodes`;
- preserve the selected display node if its ID still exists;
- otherwise select the first visible canonical root.

Changing runs clears derived nodes with the other Trace collections. Existing stale
request suppression applies unchanged.

### 6.4 Tree behavior

The execution tree displays both node kinds.

- `step` uses a step/list icon.
- `unknown_tool` uses the existing tool icon with an explicit
  `unknown_tool` label.
- operation nodes with children have a chevron control.
- operations are expanded by default.
- collapse state is local to the tree and keyed by stable node ID.
- collapsing changes only visible rows; it does not discard nodes or selection.
- Arrow Right expands a collapsed operation, then moves to its first child.
- Arrow Left collapses an expanded operation, then moves to its parent when already
  collapsed.
- Arrow Up, Arrow Down, Home, and End operate on visible rows.

The row and chevron controls must not be nested buttons. Grid dimensions remain fixed
so expanding, status changes, and longer action names do not shift the panel.

The heading count changes from canonical span count to total projected node count.

### 6.5 Details behavior

Canonical nodes retain the current details:

- duration and timestamps;
- attempts;
- usage;
- execution evidence;
- planner/case input;
- failure feedback;
- canonical output.

Derived nodes use a separate detail branch:

- kind, name, stable derived ID, status, duration, and timestamps;
- `Step data` showing `data`;
- `Evidence` showing `evidence`;
- a source reference showing `source_evidence_id`.

Attempts, usage, and canonical operation evidence are never attributed to a derived
node. The frontend does not parse `data` or `evidence` to rediscover tool semantics.

## 7. Failure Handling

- A malformed known result emits `unknown_tool`; the request still returns `200`.
- An unregistered tool emits `unknown_tool`.
- Missing parent span emits no derived node; canonical evidence remains available.
- Invalid derived JSON produced by the backend is rejected by the frontend parser as
  `invalid_response`.
- A derived node whose parent is absent from a client-side response is not shown as a
  root.
- Empty browser `steps` produces zero step nodes, not `unknown_tool`, because the
  schema is valid and there is nothing to expand.
- Projection never changes run status, span status, operation evidence, or persisted
  data.

## 8. Testing

### 8.1 Backend unit tests

Add table-driven tests for:

- one passed browser step;
- multiple steps with stable ordering and IDs;
- failed step data and evidence preservation;
- exact end-time calculation from `duration_ms`;
- no additional key filtering in projected JSON;
- empty valid step list;
- unregistered operation name producing one `unknown_tool`;
- malformed known result producing one `unknown_tool`;
- missing parent evidence producing no node;
- duplicate or non-positional step indexes using the fallback;
- deterministic output across repeated projection.

### 8.2 API handler tests

Extend Trace endpoint tests to prove:

- `derived_nodes` is separate from `spans`;
- canonical span count and payload remain unchanged;
- a browser result creates step children with the operation `span_id` as parent;
- an unknown operation creates exactly one `unknown_tool`;
- existing attempts, usage, and operation evidence remain present.

### 8.3 Frontend unit tests

Cover:

- strict parsing of valid and malformed derived nodes;
- merging canonical and derived nodes regardless of input order;
- derived nodes never becoming roots when their parent is missing;
- selection retention for stable derived IDs;
- clearing and replacing derived nodes across run changes and stale requests;
- operation expansion/collapse and visible-row keyboard navigation;
- canonical details remaining unchanged;
- derived step and `unknown_tool` details rendering data and evidence;
- total node count including derived nodes.

### 8.4 Browser verification

Run the workspace against a completed browser case and verify at desktop and narrow
viewports:

- the operation expands to all persisted steps;
- selecting a step shows its status, timing, data, and screenshot path;
- collapse/expand does not resize or overlap adjacent controls;
- keyboard navigation follows visible rows;
- refreshing preserves a selected derived-node deep link;
- no console errors occur.

## 9. Acceptance Criteria

The change is complete when:

1. A completed `browser_execute_case` with 23 persisted steps shows 23 derived child
   nodes under its canonical operation span.
2. Canonical span, attempt, usage, and operation evidence records are unchanged.
3. Each derived browser step has a stable ID, correct parent, action label, source
   status, and computed timestamps.
4. Selecting a derived step displays backend-projected data and evidence without
   frontend tool-specific parsing.
5. An unknown or unprojectable operation shows exactly one `unknown_tool` child.
6. No new redaction or desensitization is performed by the projector.
7. Projection errors do not fail the Trace endpoint.
8. Operation nodes can be expanded and collapsed with pointer and keyboard controls.
9. Existing canonical Trace behavior and deep links continue to work.
10. Backend and frontend tests pass, and browser verification shows no overlap or
    runtime errors.

## 10. Implementation Scope

Expected implementation files:

- new `backend/internal/traceprojection` package and tests;
- `backend/internal/api/v3/runs.go`;
- `backend/internal/api/v3/server_test.go`;
- `web/src/api.ts` and `web/src/api.test.ts`;
- `web/src/features/trace/projectTrace.ts` and tests;
- `web/src/features/trace/TraceTree.tsx` and tests;
- `web/src/features/trace/TraceDetails.tsx` and tests;
- `web/src/features/trace/TracePane.tsx`;
- `web/src/features/workspace/useWorkspace.ts` and tests;
- the existing Trace CSS.

No database, runtime, worker, event, infrastructure, or migration files are in scope.
