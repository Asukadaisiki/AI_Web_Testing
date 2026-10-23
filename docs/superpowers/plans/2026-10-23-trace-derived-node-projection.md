# Trace Derived Node Projection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Display persisted browser execution steps as selectable derived children of canonical operation spans without changing durable Trace data.

**Architecture:** A pure Go `traceprojection` package converts persisted operation evidence into display-only nodes at Trace API read time. The React client strictly parses the separate `derived_nodes` array, normalizes canonical and derived records into one display tree, and branches details and tree interaction by source type.

**Tech Stack:** Go 1.24, `encoding/json`, React 18, TypeScript 5.6, Vitest, Testing Library, Vite, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-23-trace-derived-node-projection-design.md`

## Global Constraints

- Keep canonical PostgreSQL spans, attempts, usage, and operation evidence unchanged.
- Do not add database migrations, backfills, runtime changes, event changes, Redis changes, or worker changes.
- Return derived nodes only in the top-level `derived_nodes` Trace API field.
- Do not add redaction or desensitization to the projector.
- Use the exact fallback kind and name `unknown_tool`.
- Treat derived nodes as persisted-evidence read projections, not live step streams.
- Keep unrelated dirty-worktree changes intact.
- For a file that was already modified before this feature, stage only this feature's
  patch hunks after reviewing `git diff`; never stage the whole file.

---

### Task 1: Pure Backend Projection Package

**Files:**
- Create: `backend/internal/traceprojection/project.go`
- Create: `backend/internal/traceprojection/project_test.go`

**Interfaces:**
- Consumes: `domain.Span` and API-neutral operation evidence values.
- Produces:

```go
type Evidence struct {
    ID, SpanID, Status string
    Result, Artifacts, Error json.RawMessage
    CreatedAt, UpdatedAt time.Time
}

type Node struct {
    ID, ParentSpanID, SourceEvidenceID string
    Kind, Name, Status string
    StartedAt time.Time
    EndedAt *time.Time
    Data, Evidence json.RawMessage
}

func Project(spans []domain.Span, evidence []Evidence) []Node
```

- [x] **Step 1: Write failing browser projection tests**

Create table-driven tests with a canonical `operation` span named
`browser_execute_case`. Assert:

```go
nodes := Project([]domain.Span{parent}, []Evidence{{
    ID: "evidence-1", SpanID: parent.SpanID, Status: "succeeded",
    Result: json.RawMessage(`{
      "steps":[{
        "index":0,
        "action":"goto",
        "status":"passed",
        "started_at":"2026-10-23T10:00:00Z",
        "duration_ms":540,
        "url_before":"about:blank",
        "url_after":"https://example.test/",
        "evidence":{"screenshot_path":"session/exec_0.png"},
        "custom_secret":"preserved"
      }]
    }`),
}})

if got := nodes[0].ID; got != "derived:operation-1:step:0" { t.Fatal(got) }
if got := nodes[0].Name; got != "Step 1: goto" { t.Fatal(got) }
if got := nodes[0].EndedAt.Format(time.RFC3339Nano); got != "2026-10-23T10:00:00.54Z" {
    t.Fatal(got)
}
```

Decode `Data` and `Evidence` and prove `custom_secret` remains present while
`evidence`, `started_at`, and `status` are absent from `Data`.

- [x] **Step 2: Run the focused tests and verify failure**

Run:

```bash
cd backend && go test ./internal/traceprojection -run TestProject -count=1
```

Expected: FAIL because the package and `Project` do not exist.

- [x] **Step 3: Implement strict browser projection**

Implement:

- a private projector registry keyed by `browser_execute_case`;
- operation name resolution from `span.Input.name`, falling back to `span.Name`;
- one selected evidence record per operation span in repository order;
- strict one-document JSON decoding with `UseNumber`;
- browser step type validation, positional non-negative indexes, RFC 3339 time
  parsing, and non-negative integral `duration_ms`;
- stable IDs, names, and end-time calculation;
- JSON copying into `Data` and `Evidence` without key filtering;
- stable output ordering by canonical span order and step index.

Do not import the PostgreSQL package and do not call `redactTraceJSON`.

- [x] **Step 4: Add fallback and edge-case tests**

Add tests proving:

```go
if got := nodes[0].Kind; got != "unknown_tool" { t.Fatal(got) }
if got := nodes[0].ID; got != "derived:operation-1:unknown_tool" { t.Fatal(got) }
```

Cover an unknown operation, malformed known result, duplicate index, empty valid
steps, missing parent, duplicate evidence for one parent, and deterministic repeated
projection. Assert fallback `Data` contains `result` and fallback `Evidence` contains
`artifacts` and `error`.

- [x] **Step 5: Run and format the backend package**

Run:

```bash
cd backend && gofmt -w internal/traceprojection/project.go internal/traceprojection/project_test.go
cd backend && go test ./internal/traceprojection -count=1
```

Expected: PASS.

- [x] **Step 6: Commit the projection package**

```bash
git add backend/internal/traceprojection/project.go backend/internal/traceprojection/project_test.go
git commit -m "feat: project operation evidence into trace nodes"
```

---

### Task 2: Trace API Integration

**Files:**
- Modify: `backend/internal/api/v3/runs.go`
- Modify: `backend/internal/api/v3/server_test.go`

**Interfaces:**
- Consumes: `traceprojection.Project([]domain.Span, []traceprojection.Evidence)`.
- Produces: required JSON response field `"derived_nodes": []`.

- [x] **Step 1: Extend the handler tests first**

Update `TestGetTraceExposesRedactedCanonicalDetails` with a complete browser result
and assert:

```go
for _, required := range []string{
    `"derived_nodes"`,
    `"id":"derived:operation-1:step:0"`,
    `"parent_span_id":"operation-1"`,
    `"source_evidence_id":"evidence-1"`,
    `"kind":"step"`,
} {
    if !strings.Contains(body, required) { t.Fatalf("missing %s", required) }
}
```

Add a separate unknown-operation test that expects exactly one node with kind and
name `unknown_tool`. Decode the response into a small test struct so node counts are
asserted structurally rather than by substring.

- [x] **Step 2: Run the API tests and verify failure**

Run:

```bash
cd backend && go test ./internal/api/v3 -run 'TestGetTrace' -count=1
```

Expected: FAIL because `derived_nodes` is absent.

- [x] **Step 3: Wire projection into `handleGetTrace`**

Map each `postgres.TraceOperationDetail` to `traceprojection.Evidence`, call
`traceprojection.Project(trace.Spans, projectionEvidence)`, and add:

```go
writeJSON(w, http.StatusOK, map[string]any{
    "spans": spans,
    "attempts": attempts,
    "usage": usage,
    "operation_evidence": operations,
    "derived_nodes": derivedNodes,
})
```

Leave canonical mapping and redaction code unchanged.

- [x] **Step 4: Run focused and full backend tests**

Run:

```bash
cd backend && gofmt -w internal/api/v3/runs.go internal/api/v3/server_test.go
cd backend && go test ./internal/traceprojection ./internal/api/v3 -count=1
cd backend && go test -p 1 ./...
```

Expected: PASS.

- [x] **Step 5: Commit API integration**

```bash
git add backend/internal/api/v3/runs.go
git add -p backend/internal/api/v3/server_test.go
git diff --cached --check
git commit -m "feat: expose derived trace nodes"
```

Accept only the Trace-derived-node hunks from `server_test.go`; leave its pre-existing
changes unstaged.

---

### Task 3: Frontend Trace API Contract

**Files:**
- Modify: `web/src/api.ts`
- Modify: `web/src/api.test.ts`
- Modify: `web/src/features/workspace/useWorkspace.test.ts`

**Interfaces:**
- Produces:

```ts
export type DerivedTraceKind = 'step' | 'unknown_tool';

export interface DerivedTraceNode {
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

- `Trace` gains `derived_nodes: DerivedTraceNode[]`.

- [x] **Step 1: Add failing parser tests**

Add one valid derived node to the successful `getTrace` fixture and assert:

```ts
expect(trace.derived_nodes[0]).toMatchObject({
  id: 'derived:operation-1:step:0',
  parent_span_id: 'operation-1',
  kind: 'step',
  status: 'passed',
});
```

Add a test whose derived kind is `other` and expect `getTrace` to reject with an
`ApiError` pointing at `derived_nodes[0].kind`.

- [x] **Step 2: Run the API test and verify failure**

Run:

```bash
cd web && npm test -- --run src/api.test.ts
```

Expected: FAIL because `Trace` and `getTrace` do not expose `derived_nodes`.

- [x] **Step 3: Implement the strict derived-node parser**

Add `derivedTraceKinds`, `parseDerivedTraceNode`, and required mapping:

```ts
derived_nodes: arrayValue(
  response['derived_nodes'],
  status,
  'derived_nodes',
).map((node, index) =>
  parseDerivedTraceNode(node, status, `derived_nodes[${String(index)}]`),
),
```

Use `jsonValue` for both `data` and `evidence`.

- [x] **Step 4: Update all Trace fixtures**

Add `derived_nodes: []` to every typed or mocked `Trace` response in frontend unit
tests. Do not make the API field optional.

- [x] **Step 5: Run API tests and typecheck**

Run:

```bash
cd web && npm test -- --run src/api.test.ts
cd web && npm run typecheck
```

Expected: PASS.

- [x] **Step 6: Commit the frontend contract**

```bash
git add web/src/api.ts web/src/api.test.ts web/src/features/workspace/useWorkspace.test.ts
git commit -m "feat: parse derived trace nodes"
```

---

### Task 4: Normalized Display Tree And Workspace State

**Files:**
- Modify: `web/src/features/trace/projectTrace.ts`
- Modify: `web/src/features/trace/projectTrace.test.ts`
- Modify: `web/src/features/workspace/useWorkspace.ts`
- Modify: `web/src/features/workspace/useWorkspace.test.ts`
- Modify: `web/src/features/workspace/workspace.test.ts`
- Modify: `web/src/features/trace/TracePane.tsx`
- Modify: `web/src/features/trace/TraceTree.tsx`
- Modify: `web/src/features/trace/TraceDetails.tsx`
- Modify: `web/src/features/trace/TraceDetails.test.tsx`

**Interfaces:**
- Produces a normalized `TraceNode`:

```ts
export interface TraceNode {
  id: string;
  parentId?: string;
  kind: SpanKind | DerivedTraceKind;
  name: string;
  status: string;
  startedAt: string;
  endedAt?: string | null;
  source:
    | { type: 'span'; value: Span }
    | { type: 'derived'; value: DerivedTraceNode };
  children: TraceNode[];
}

export function projectTrace(
  spans: readonly Span[],
  derivedNodes: readonly DerivedTraceNode[],
): TraceNode[];
```

- Workspace exposes `derivedNodes`, `selectedNode`, and keeps the existing URL
  selection property name `spanId`.

- [x] **Step 1: Write failing tree projection tests**

Add tests asserting:

```ts
const projected = projectTrace([operation], [step]);
expect(projected[0]?.children[0]).toMatchObject({
  id: 'derived:operation-1:step:0',
  parentId: 'operation-1',
  kind: 'step',
  source: { type: 'derived' },
});
```

Also cover input-order independence, a missing derived parent being omitted, and a
canonical orphan remaining a root.

- [x] **Step 2: Run the projection test and verify failure**

Run:

```bash
cd web && npm test -- --run src/features/trace/projectTrace.test.ts
```

Expected: FAIL because `projectTrace` accepts only spans and nodes inherit `Span`.

- [x] **Step 3: Implement normalized projection**

Create canonical nodes first, add only derived nodes whose canonical parent exists,
attach all parents after node creation, sort by `startedAt` then `id`, and update
`flattenTrace` for the normalized shape.

- [x] **Step 4: Add failing workspace state tests**

Extend hook tests with a derived node response. Assert it is stored, projected,
selectable by stable derived ID, retained after refetch, and cleared immediately when
switching runs. Extend stale response tests so an older response cannot replace the
current run's derived nodes.

- [x] **Step 5: Update workspace state**

Add `derivedNodes` state, set it from `nextTrace.derived_nodes`, clear it on run
changes, and project with:

```ts
const trace = useMemo(
  () => projectTrace(spans, derivedNodes),
  [derivedNodes, spans],
);
```

Update selection helpers to compare `node.id`. Rename the exposed selected value to
`selectedNode` and update its direct consumers in the same commit. Adapt
`TraceTree.tsx` and the canonical branch of `TraceDetails.tsx` mechanically to the
normalized field names and `source.value`; do not add collapse behavior or a derived
details branch until Task 5.

- [x] **Step 6: Run workspace and projection tests**

Run:

```bash
cd web && npm test -- --run \
  src/features/trace/projectTrace.test.ts \
  src/features/workspace/workspace.test.ts \
  src/features/workspace/useWorkspace.test.ts
cd web && npm run typecheck
```

Expected: PASS.

- [x] **Step 7: Commit normalized tree state**

```bash
git add web/src/features/trace/projectTrace.ts \
  web/src/features/trace/projectTrace.test.ts \
  web/src/features/workspace/useWorkspace.ts \
  web/src/features/workspace/useWorkspace.test.ts \
  web/src/features/workspace/workspace.test.ts \
  web/src/features/trace/TracePane.tsx \
  web/src/features/trace/TraceTree.tsx \
  web/src/features/trace/TraceDetails.tsx
git commit -m "feat: merge derived nodes into trace tree"
```

---

### Task 5: Tree Interaction And Derived Details

**Files:**
- Modify: `web/src/features/trace/TraceTree.tsx`
- Create: `web/src/features/trace/TraceTree.test.tsx`
- Modify: `web/src/features/trace/TraceDetails.tsx`
- Modify: `web/src/features/trace/TraceDetails.test.tsx`
- Modify: `web/src/features/trace/TracePane.tsx`
- Create: `web/src/features/trace/TracePane.test.tsx`
- Modify: `web/src/styles.css`

**Interfaces:**
- `TraceTree` selects by normalized `node.id`.
- `TraceDetails` branches on `node.source.type`.
- `TracePane` attributes attempts, usage, and operation evidence only when the
  selected source is a canonical span.

- [x] **Step 1: Write failing tree interaction tests**

Render an operation with two derived children and assert:

```ts
expect(screen.getByRole('treeitem', { name: /browser_execute_case/i }))
  .toHaveAttribute('aria-expanded', 'true');
fireEvent.click(screen.getByRole('button', { name: /collapse browser_execute_case/i }));
expect(screen.queryByText('Step 1: goto')).not.toBeInTheDocument();
fireEvent.click(screen.getByRole('button', { name: /expand browser_execute_case/i }));
expect(screen.getByText('Step 1: goto')).toBeInTheDocument();
```

Add keyboard assertions for Arrow Right, Arrow Left, Arrow Up, Arrow Down, Home, and
End using visible rows.

- [x] **Step 2: Run the tree test and verify failure**

Run:

```bash
cd web && npm test -- --run src/features/trace/TraceTree.test.tsx
```

Expected: FAIL because operation collapse controls do not exist.

- [x] **Step 3: Implement accessible collapse behavior**

Use local `Set<string>` collapsed state. Render a row wrapper with `role="treeitem"`,
a sibling chevron icon button, and a selection button so no button contains another
button. Add `ListChecks` for `step`, `Wrench` for `unknown_tool`, `aria-expanded` on
operation rows with children, and navigation over visible rows only.

- [x] **Step 4: Write failing derived detail tests**

Render one derived step and one `unknown_tool` node. Assert the selected derived node
shows:

- its stable ID, source evidence ID, status, and duration;
- `Step data`;
- `Evidence`;
- screenshot path and preserved custom fields;
- no `Attempt`, `Usage`, `Canonical output`, or canonical evidence panels.

- [x] **Step 5: Implement the details branch and pane attribution**

In `TraceDetails`, render a dedicated derived-node article before the canonical
branch. In `TracePane`, derive `selectedSpanId` only when:

```ts
workspace.selectedNode?.source.type === 'span'
```

Count nodes with `flattenTrace(workspace.trace).length`, not
`workspace.spans.length`.

- [x] **Step 6: Update fixed-layout CSS**

Add stable columns for chevron, kind icon, label, and status. Ensure row wrappers,
toggle buttons, selection buttons, step/unknown icons, focus state, and narrow-panel
text truncation use existing color and radius tokens.

- [x] **Step 7: Run trace component tests and typecheck**

Run:

```bash
cd web && npm test -- --run \
  src/features/trace/TraceTree.test.tsx \
  src/features/trace/TraceDetails.test.tsx \
  src/features/trace/TraceOverview.test.tsx
cd web && npm run typecheck
```

Expected: PASS.

- [x] **Step 8: Commit tree UI**

```bash
git add web/src/features/trace/TraceTree.tsx \
  web/src/features/trace/TraceTree.test.tsx \
  web/src/features/trace/TraceDetails.tsx \
  web/src/features/trace/TraceDetails.test.tsx \
  web/src/features/trace/TracePane.tsx \
  web/src/styles.css
git commit -m "feat: inspect derived trace steps"
```

---

### Task 6: Full Verification And Real Trace Acceptance

**Files:**
- Modify only files required by failures attributable to this feature.
- Update: `docs/superpowers/plans/2026-10-23-trace-derived-node-projection.md`

**Interfaces:**
- Verifies the complete API-to-UI path without changing durable schema or worker
  contracts.

- [x] **Step 1: Run all backend and frontend checks**

Run:

```bash
cd backend && go test -p 1 ./...
cd web && npm test -- --run
cd web && npm run build
```

Expected: PASS.

- [x] **Step 2: Inspect the Trace API against completed formal runs**

For the current 23-step and 35-step runs, request:

```bash
RUN_IDS="$(docker compose exec -T postgres psql -U agenttrace -d agenttrace -Atc \
  "SELECT run_id FROM operation_results
   WHERE jsonb_array_length(result->'steps') IN (23, 35)
   ORDER BY created_at DESC LIMIT 2")"
for run_id in ${RUN_IDS}; do
  curl -fsS "http://127.0.0.1:8080/api/v1/runs/${run_id}/trace"
done
```

Verify structurally that:

- canonical span count remains four for each current formal run;
- `derived_nodes` count equals persisted browser step count;
- every derived node parent is the canonical operation span;
- IDs and step indexes are unique and ordered;
- no `unknown_tool` appears for valid browser results.

If the current Compose services predate the code change, rebuild only the API and web
services before repeating the check.

- [x] **Step 3: Run browser verification at desktop and narrow widths**

Use the existing Playwright/browser verification path at `1440x900` and `390x844`.
Open each formal run, expand and collapse the operation, select early and late steps,
refresh a selected derived-node URL, and assert:

- details show the correct source evidence, timing, data, and screenshot path;
- the 23-step and 35-step trees scroll without overlap;
- pointer and keyboard navigation follow visible rows;
- browser console has no errors.

- [x] **Step 4: Review scope and diff**

Run:

```bash
git diff --check
git status --short
git diff --stat 2c67c42..HEAD
```

Confirm no database migration, runtime, worker, event, Redis, or unrelated dirty file
was added to feature commits.

- [x] **Step 5: Mark this plan complete and commit verification fixes**

Change completed checkboxes to `[x]`. Any code fix discovered here must repeat the
focused test and commit cycle from the task that owns that file. Then commit only the
plan status update:

```bash
git add docs/superpowers/plans/2026-10-23-trace-derived-node-projection.md
git commit -m "docs: record trace projection verification"
```
