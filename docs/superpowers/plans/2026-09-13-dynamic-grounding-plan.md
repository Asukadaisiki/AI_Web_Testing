# Dynamic GroundingPlan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persisted dynamic GroundingPlan and a trusted `query_observation -> candidate_ref -> runtime revalidation` path that fixes BUG-188 without moving page facts into Semantic TaskPlan.

**Architecture:** Semantic TaskPlan remains the business-action authority. A new Go `groundingplan` domain stores immutable grounding revisions linked to an exact TaskPlan binding; `query_observation` reads complete exploration ToolResults from the current Run and emits opaque candidate references; Go resolves those references into trusted internal candidates before Python revalidates and executes them in a new probe.

**Tech Stack:** Go 1.23, PostgreSQL/pgx, JSON Schema Draft 2020-12, Python 3.12, Pydantic 2, Playwright, Pyright, Ruff.

**Spec:** `docs/superpowers/specs/2026-09-13-semantic-taskplan-dynamic-grounding-plan-design.md`

## Global Constraints

- Preserve `agent.task_plan.v2` as the authority for business action semantics.
- Persist one logical `grounding.plan.v1` per TaskPlan version as immutable revision rows.
- Resolve candidate references only from the current Agent Run's persisted exploration `tool.result`.
- Never accept model-supplied CSS, XPath, locator, element, observation hash, or artifact URI inside `candidate_ref`.
- Preserve `grounding.query.v1`; add `grounding.query.v2` with exactly one of `locator` or `candidate_ref` per action.
- Keep Python Browser Worker stateless with respect to historical Observations.
- Revalidate candidate runtime count, visibility, enabled state, and editability in the new probe.
- Preserve source candidate and runtime candidate lineage through ResolvedTarget and TargetBinding.
- Do not add task-specific products, selectors, URLs, values, or action order to reusable runtime code.
- Do not run paid model acceptance. Acceptance uses contracts, full Go/Python suites, and real Chromium focused tests.

---

### Task 1: Shared Candidate and GroundingQuery Contracts

**Files:**
- Create: `contracts/grounding-candidate-ref.v1.schema.json`
- Create: `contracts/grounding-observation-query.v1.schema.json`
- Create: `contracts/grounding-observation-query-result.v1.schema.json`
- Create: `contracts/grounding-query.v2.schema.json`
- Create: `testdata/grounding_candidate_ref_v1_contract.json`
- Modify: `contracts/browser-resolved-target.v1.schema.json`
- Modify: `backend-go/internal/browsercontract/types.go`
- Modify: `backend-go/internal/browsercontract/types_test.go`
- Modify: `browser-worker/src/browser_worker/contracts/browser_observation.py`
- Modify: `browser-worker/src/browser_worker/contracts/browser_capabilities.py`
- Test: `browser-worker/tests/test_browser_observation_contract.py`
- Test: `browser-worker/tests/test_browser_capabilities.py`

**Interfaces:**
- Produces Go `browsercontract.CandidateRef`, `TrustedResolvedCandidate`, and optional source lineage on `ResolvedTargetEvidence`.
- Produces Python `CandidateRef`, `TrustedResolvedCandidate`, and GroundingQuery v2 unions.
- Later tasks consume the exact schema versions and field names defined here.

- [ ] **Step 1: Write failing Go and Python contract tests**

Add literal fixtures asserting:

```json
{
  "schema_version": "grounding.candidate-ref.v1",
  "source_event_seq": 27,
  "probe_id": "probe-source",
  "observation_id": "obs-source",
  "candidate_id": "candidate-source"
}
```

Tests must reject a candidate reference containing `locator`, accept v1
LocatorSpec actions unchanged, accept v2 actions with exactly one of
`locator` or `candidate_ref`, and reject zero or two target modes.

- [ ] **Step 2: Run focused tests and verify RED**

Run:

```bash
cd backend-go && go test ./internal/browsercontract
cd browser-worker && uv run python -m unittest \
  tests.test_browser_observation_contract \
  tests.test_browser_capabilities
```

Expected: failures because new schemas/types and v2 action parsing do not exist.

- [ ] **Step 3: Implement minimal shared contracts**

Use these exact public structures:

```go
type CandidateRef struct {
    SchemaVersion  string `json:"schema_version"`
    SourceEventSeq int64  `json:"source_event_seq"`
    ProbeID        string `json:"probe_id"`
    ObservationID  string `json:"observation_id"`
    CandidateID    string `json:"candidate_id"`
}

type TrustedResolvedCandidate struct {
    Source          CandidateRef `json:"source"`
    PageStateID     string       `json:"page_state_id"`
    PageStateSHA256 string       `json:"page_state_sha256"`
    ElementRef      string       `json:"element_ref"`
    Locator         LocatorSpec  `json:"locator"`
    ContextPath     ContextPath  `json:"context_path"`
    Provenance      string       `json:"provenance"`
}
```

`ResolvedTargetEvidence` gains:

```go
SourceCandidate *CandidateRef `json:"source_candidate,omitempty"`
```

The equivalent Pydantic fields must use the same JSON names and strict validation.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run the Step 2 commands. Expected: all selected tests pass.

- [ ] **Step 5: Commit**

```bash
git add contracts testdata backend-go/internal/browsercontract \
  browser-worker/src/browser_worker/contracts \
  browser-worker/tests/test_browser_observation_contract.py \
  browser-worker/tests/test_browser_capabilities.py
git commit -m "feat: define grounding candidate contracts"
```

### Task 2: GroundingPlan Domain and Persistence

**Files:**
- Create: `backend-go/internal/groundingplan/types.go`
- Create: `backend-go/internal/groundingplan/service.go`
- Create: `backend-go/internal/groundingplan/memory_repository.go`
- Create: `backend-go/internal/groundingplan/postgres_repository.go`
- Create: `backend-go/internal/groundingplan/service_test.go`
- Create: `backend-go/internal/groundingplan/postgres_repository_test.go`
- Create: `backend-go/internal/dbschema/groundingplan.sql`
- Modify: `backend-go/internal/dbschema/schema.go`
- Modify: `backend-go/internal/dbschema/schema.sql`
- Modify: `backend-go/internal/dbschema/taskplan_test.go`

**Interfaces:**
- Consumes: `taskplan.Binding`, `taskplan.Plan`, `browsercontract.CandidateRef`, and `browsercontract.ResolvedTargetEvidence`.
- Produces:

```go
type Repository interface {
    CreateInitial(context.Context, Plan) (Plan, error)
    AppendRevision(context.Context, Plan) (Plan, error)
    GetCurrent(context.Context, string) (Plan, error)
    SupersedeForTaskPlan(context.Context, string, time.Time) error
}

func (s *Service) EnsureForTaskPlan(context.Context, taskplan.Plan) (Plan, error)
func (s *Service) RecordObservationQuery(context.Context, string, QueryRecord, []CandidateOption) (Plan, error)
func (s *Service) RecordCandidateSelection(context.Context, string, string, browsercontract.CandidateRef) (Plan, error)
func (s *Service) RecordProbeResult(context.Context, string, string, *browsercontract.ResolvedTargetEvidence, string) (Plan, error)
```

- [ ] **Step 1: Write failing domain and PostgreSQL tests**

Cover literal state transitions:

```text
pending -> candidates_available -> candidate_selected -> probing -> grounded
all grounded -> ready
TaskPlan revision -> old GroundingPlan superseded + new revision 1 active
```

Assert each mutation appends a new immutable row and leaves prior JSON/hash unchanged.

- [ ] **Step 2: Run focused tests and verify RED**

Run:

```bash
cd backend-go && go test ./internal/groundingplan ./internal/dbschema
```

Expected: package/table/embed symbols missing.

- [ ] **Step 3: Implement types, validation, canonical hash, and memory service**

Use exact schema version `grounding.plan.v1`. Validate that steps exactly match
the linked TaskPlan IDs, positions, and actions. Compute `content_sha256` from
canonical JSON excluding database timestamps and the hash itself.

- [ ] **Step 4: Implement additive PostgreSQL migration and repository**

Embed `groundingplan.sql` from `schema.go`. Add the table/indexes to the canonical
`schema.sql`. Serialize the complete validated plan into `content_json`; select
current with `ORDER BY revision DESC LIMIT 1`.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run the Step 2 command with the repository's configured PostgreSQL test environment.
Expected: all selected tests pass without skips when `TEST_DATABASE_URL` is available.

- [ ] **Step 6: Commit**

```bash
git add backend-go/internal/groundingplan backend-go/internal/dbschema
git commit -m "feat: persist dynamic grounding plans"
```

### Task 3: Current-Run Observation Reader and Query Tool

**Files:**
- Create: `backend-go/internal/groundingplan/observation_reader.go`
- Create: `backend-go/internal/groundingplan/observation_reader_test.go`
- Create: `backend-go/internal/tools/observation_query.go`
- Create: `backend-go/internal/tools/observation_query_test.go`
- Modify: `backend-go/internal/agentservice/repository.go`
- Modify: `backend-go/internal/agentservice/memory_repository.go`
- Modify: `backend-go/internal/agentservice/postgres_repository.go`
- Modify: `backend-go/internal/agentservice/service.go`
- Modify: `backend-go/internal/agent/tool_result.go`
- Modify: `backend-go/internal/agent/tool_result_test.go`

**Interfaces:**
- Consumes: Agent events and TaskPlan current pending step.
- Produces:

```go
func (s *Service) GetEvent(context.Context, string, int64) (agentservice.Event, error)
func (r *ObservationReader) Query(context.Context, string, ObservationQuery) (ObservationQueryResult, error)
type ObservationQueryTool struct { ... }
```

The result matches `grounding.observation-query-result.v1`.

- [ ] **Step 1: Write failing reader tests**

Use complete literal `agent.tool_result.v1` fixtures. Cover:

- current Run candidate returned;
- another Run's sequence rejected;
- non-tool-result/non-exploration event rejected;
- observation/probe mismatch rejected;
- duplicate candidate ID rejected;
- `observed_count != 1` omitted;
- click/input actionability filtering;
- query matches DOM id even when accessible name is empty;
- result limit and omitted count.

- [ ] **Step 2: Run reader tests and verify RED**

```bash
cd backend-go && go test ./internal/groundingplan ./internal/agentservice
```

Expected: missing `GetEvent`, `ObservationReader`, and query types.

- [ ] **Step 3: Implement event lookup and trusted Observation parsing**

`GetEvent(runID, seq)` must query both keys, not fetch by sequence globally.
The reader must decode the persisted full `content`, never the model summary.

- [ ] **Step 4: Write failing tool and model-summary tests**

Assert `query_observation`:

- authorizes only the next pending PlanStep and matching action;
- returns candidate references;
- records `candidates_available` in GroundingPlan;
- produces a dedicated bounded model summary with candidate identity and DOM facts.

- [ ] **Step 5: Implement the tool and dedicated summary**

Register a structured input schema with required
`schema_version`, `plan_step_id`, `source_event_seq`, and `action`.
Do not expose arbitrary selector input.

- [ ] **Step 6: Run focused tests and verify GREEN**

```bash
cd backend-go && go test \
  ./internal/groundingplan \
  ./internal/agentservice \
  ./internal/tools \
  ./internal/agent
```

- [ ] **Step 7: Commit**

```bash
git add backend-go/internal/groundingplan \
  backend-go/internal/agentservice \
  backend-go/internal/tools/observation_query* \
  backend-go/internal/agent/tool_result*
git commit -m "feat: query persisted observation candidates"
```

### Task 4: GroundingQuery v2 Candidate Hydration in Go

**Files:**
- Modify: `backend-go/internal/tools/browser.go`
- Modify: `backend-go/internal/tools/capability_tools_test.go`
- Modify: `backend-go/internal/taskplan/service.go`
- Modify: `backend-go/internal/taskplan/service_test.go`
- Modify: `backend-go/internal/browsercontract/types.go`
- Modify: `backend-go/cmd/agentservice/main.go`
- Test: `backend-go/internal/harness/harness_test.go`

**Interfaces:**
- Consumes: `ObservationReader.ResolveCandidate(runID, action, CandidateRef)`.
- Produces: a Worker-only action field:

```json
{
  "resolved_candidate": {
    "source": {"schema_version":"grounding.candidate-ref.v1", "...":"..."},
    "page_state_id":"S0",
    "page_state_sha256":"...",
    "element_ref":"S0:42",
    "locator":{"kind":"css","value":"#submit_search"},
    "context_path":{"frames":[],"shadow_hosts":[]},
    "provenance":"a11y_backend_dom_node"
  }
}
```

- [ ] **Step 1: Write failing browser tool hydration tests**

Assert:

- v2 `candidate_ref` is resolved and replaced before the Worker client sees it;
- public `candidate_ref` and `plan_step_ids` are absent from Worker arguments;
- model-supplied `resolved_candidate` is rejected by JSON Schema;
- v1 LocatorSpec request remains byte-semantically compatible;
- cross-Run and stale source errors are returned before Worker invocation.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
cd backend-go && go test ./internal/tools ./internal/taskplan ./internal/harness
```

- [ ] **Step 3: Implement BrowserTool dependency and v2 hydration**

Change construction to:

```go
func NewBrowserTools(
    client BrowserCapabilityClient,
    candidates CandidateResolver,
    groundingPlans *groundingplan.Service,
) []Handler
```

Hydration occurs in Go after TaskPlan authorization and before the Worker call.
Record candidate selection/probing in GroundingPlan.

- [ ] **Step 4: Update TaskPlan action ownership parsing**

`probeFlowRequest` accepts `Locator` or `CandidateRef`; enforce exactly one.
Forbidden-action evaluation includes only action/value/description and trusted
candidate metadata resolved by Go, never an untrusted selector.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run the Step 2 command. Expected: all selected tests pass.

- [ ] **Step 6: Commit**

```bash
git add backend-go/internal/tools/browser.go \
  backend-go/internal/tools/capability_tools_test.go \
  backend-go/internal/taskplan \
  backend-go/internal/browsercontract \
  backend-go/internal/harness/harness_test.go \
  backend-go/cmd/agentservice/main.go
git commit -m "feat: hydrate grounding candidate references"
```

### Task 5: Worker Runtime Revalidation and Source Lineage

**Files:**
- Modify: `browser-worker/src/browser_worker/contracts/browser_capabilities.py`
- Modify: `browser-worker/src/browser_worker/contracts/browser_observation.py`
- Modify: `browser-worker/src/browser_worker/exploration/observation.py`
- Modify: `browser-worker/src/browser_worker/exploration/page_explorer.py`
- Modify: `browser-worker/src/browser_worker/capabilities/browser_capabilities.py`
- Test: `browser-worker/tests/test_browser_capabilities.py`
- Test: `browser-worker/tests/test_browser_observation_contract.py`
- Test: `browser-worker/tests/test_page_explorer.py`

**Interfaces:**
- Consumes: Worker-only `resolved_candidate`.
- Produces runtime `browser.resolved-target.v1` with optional
`source_candidate` and a new current-probe candidate ID.

- [ ] **Step 1: Write failing Python tests**

Cover:

- resolved candidate CSS compiles and executes;
- source candidate ID is not reused as runtime candidate ID;
- zero/multiple runtime match rejected;
- hidden/disabled/non-editable rejected by action;
- frame context rejected;
- source lineage round-trips;
- v1 semantic locator behavior unchanged.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
cd browser-worker && uv run python -m unittest \
  tests.test_browser_capabilities \
  tests.test_browser_observation_contract \
  tests.test_page_explorer
```

- [ ] **Step 3: Implement trusted candidate parsing and execution**

Only the internal Pydantic action model may carry `resolved_candidate`.
Select the action target through one helper returning `(locator, source_ref)`;
reuse existing `compile_locator`, runtime count checks, and action dispatch.

- [ ] **Step 4: Generate current-probe candidate and ResolvedTarget**

When a trusted candidate is used, attach its locator to the before-action
Observation with provenance `candidate_ref_runtime`, derive a new candidate ID
using the current `probe_id`, and set `source_candidate` on the evidence.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run the Step 2 command. Expected: all selected tests pass.

- [ ] **Step 6: Commit**

```bash
git add browser-worker/src/browser_worker \
  browser-worker/tests/test_browser_capabilities.py \
  browser-worker/tests/test_browser_observation_contract.py \
  browser-worker/tests/test_page_explorer.py
git commit -m "feat: revalidate referenced candidates in worker"
```

### Task 6: Harness, State Synchronization, Events, and Frontend Types

**Files:**
- Modify: `backend-go/internal/harness/harness.go`
- Modify: `backend-go/internal/harness/harness_test.go`
- Modify: `backend-go/internal/taskplan/service.go`
- Modify: `backend-go/internal/taskplan/service_test.go`
- Modify: `backend-go/internal/agentservice/types.go`
- Modify: `backend-go/internal/agent/tool_result.go`
- Modify: `backend-go/internal/agent/tool_result_test.go`
- Modify: `backend-go/internal/harness/pipeline_trace.go`
- Modify: `backend-go/internal/harness/pipeline_trace_test.go`
- Modify: `backend-go/internal/taskplan/compiler.go`
- Modify: `backend-go/internal/taskplan/service_test.go`
- Modify: `frontend/src/features/agent/types.ts`
- Modify: `frontend/src/features/agent/events.test.ts`

**Interfaces:**
- Consumes: GroundingPlan service and source lineage.
- Produces `grounding_plan.updated` events, compact model projection, and
TaskPlan/GroundingPlan ready-state invariant.

- [ ] **Step 1: Write failing state and event tests**

Assert:

- TaskPlan creation ensures GroundingPlan revision 1;
- TaskPlan revision supersedes old GroundingPlan;
- `query_observation` and candidate probes emit `grounding_plan.updated`;
- successful candidate-ref action grounds both plans;
- `generate_dsl` is rejected unless TaskPlan is `ready_for_generation` and
GroundingPlan is `ready`;
- source and runtime candidate lineage appears in pipeline trace and compiled DSL.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
cd backend-go && go test \
  ./internal/harness \
  ./internal/taskplan \
  ./internal/agent \
  ./internal/agentservice
cd frontend && npm test -- --run
```

- [ ] **Step 3: Implement orchestration and projections**

Add `EventGroundingPlanUpdated`. Record full event payload after each
GroundingPlan revision. Extend model summaries with plan ID/revision/status,
current step, candidate count/reference, and last error only.

- [ ] **Step 4: Extend compiler and trace lineage**

Add optional source candidate fields without changing old executable DSL
validation. The runtime candidate remains `selected_candidate_id`.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run the Step 2 commands. Expected: Go and frontend focused suites pass.

- [ ] **Step 6: Commit**

```bash
git add backend-go/internal/harness backend-go/internal/taskplan \
  backend-go/internal/agent backend-go/internal/agentservice \
  frontend/src/features/agent
git commit -m "feat: integrate grounding plan lifecycle"
```

### Task 7: Real Chromium Regression and Full Verification

**Files:**
- Modify: `browser-worker/tests/test_explore_flow_chromium.py`
- Modify: `docs/bug-log.md`
- Modify: `docs/execution-log.md`
- Modify: `docs/plan/agent-pipeline-consistency-audit-2026-09-12.md`

**Interfaces:**
- Consumes the complete implementation.
- Produces acceptance evidence and closes BUG-188 only if every required gate passes.

- [ ] **Step 1: Write the failing real Chromium regression**

Use local deterministic HTML containing:

```html
<input placeholder="Search Product">
<button id="submit_search" type="button"><span aria-hidden="true">icon</span></button>
<button id="subscribe" type="button"><span aria-hidden="true">icon</span></button>
```

The test must:

1. create an initial Observation;
2. query `submit_search` and select its CSS-backed candidate;
3. call a later isolated flow through `candidate_ref`;
4. assert the click occurred exactly once;
5. assert source candidate lineage and a different current-probe candidate ID.

- [ ] **Step 2: Run Chromium test and verify RED**

```bash
cd browser-worker && RUN_BROWSER_INTEGRATION=1 \
  uv run python -m unittest \
  tests.test_explore_flow_chromium.ExploreFlowChromiumTest.test_reuses_queried_candidate_in_later_probe
```

Expected: failure before final integration is complete.

- [ ] **Step 3: Make only integration fixes required by the regression**

Do not add site-specific code. Adjust generic contracts/orchestration only.

- [ ] **Step 4: Run focused Chromium and cross-language contract tests**

```bash
cd backend-go && go test ./internal/browsercontract ./internal/groundingplan ./internal/tools ./internal/harness ./internal/taskplan
cd browser-worker && RUN_BROWSER_INTEGRATION=1 uv run python -m unittest \
  tests.test_explore_flow_chromium
```

- [ ] **Step 5: Run full repository gates**

```bash
cd backend-go && go test ./... && go vet ./... && go build ./...
cd browser-worker && uv run python -m unittest discover -s tests -p 'test_*.py'
cd browser-worker && uv run python -m compileall -q src tests
cd browser-worker && uv run pyright
cd browser-worker && uv run ruff check src tests --select F,I
cd frontend && npm test -- --run && npm run build
```

Expected: all commands exit zero. Real Chromium environment-only skips must
remain explicit and must not replace the required focused Chromium pass.

- [ ] **Step 6: Update documentation and BUG status**

Mark BUG-188 `fixed` only after Step 5. Record exact commands, pass counts,
Chromium result, migration compatibility, and the fact that no paid model E2E
was run.

- [ ] **Step 7: Commit**

```bash
git add browser-worker/tests/test_explore_flow_chromium.py \
  docs/bug-log.md docs/execution-log.md \
  docs/plan/agent-pipeline-consistency-audit-2026-09-12.md
git commit -m "test: verify dynamic grounding candidate reuse"
```

### Task 8: Final Review and GitHub Sync

**Files:**
- No production files expected; fixes, if required, stay within reviewed scope.

**Interfaces:**
- Consumes all prior task commits and verification reports.
- Produces a reviewed branch pushed directly to GitHub.

- [ ] **Step 1: Run final whole-branch review**

Review from design commit `e78cd4a` to `HEAD`, prioritizing contract drift,
cross-Run reference access, mutable historical revisions, model-supplied
trusted fields, stale runtime candidates, TaskPlan/GroundingPlan disagreement,
and missing tests.

- [ ] **Step 2: Resolve review findings**

Use one scoped fix wave, rerun tests covering every changed file, then one
scoped re-review.

- [ ] **Step 3: Verify branch and push**

```bash
git status --short
git log --oneline e78cd4a..HEAD
git push origin HEAD
```

- [ ] **Step 4: Report sync**

Report branch name, latest commit hash, push result, worktree cleanliness,
verification commands, and the explicit limitation that paid model E2E was
not run.
