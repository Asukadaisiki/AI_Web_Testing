# Semantic TaskPlan and Dynamic GroundingPlan Design

## Context

The current Agent pipeline persists an action-level `agent.task_plan.v2`
before browser exploration. This correctly protects user intent, action
order, occurrence count, idempotency, side-effect boundaries, and forbidden
actions. Browser exploration then produces `BrowserObservation`,
`ResolvedTargetEvidence`, and `TargetBinding`.

BUG-188 exposes a missing boundary between observations. A persisted
Observation can already contain a unique candidate such as
`#submit_search`, but the next GroundingQuery only accepts a newly authored
semantic `LocatorSpec`. The model therefore has to describe the element
again, which loses the observed candidate identity and can produce zero or
multiple runtime matches.

The long-term direction is to keep the action-level TaskPlan as the semantic
authority and introduce an independent, dynamic GroundingPlan as the
authority for observation queries, candidate selection, probe attempts, and
binding progress.

## Goals

1. Keep `Semantic TaskPlan` authoritative for business actions and their
   order, values, occurrence counts, conditions, idempotency, side effects,
   and forbidden actions.
2. Add a persisted and versioned `GroundingPlan` linked to one exact
   TaskPlan binding.
3. Let the model query candidates from a complete persisted Observation
   without receiving permission to invent CSS or XPath.
4. Let a GroundingQuery reuse a candidate returned by `query_observation`.
5. Resolve candidate references from the current Run's persisted
   `tool.result` event, then revalidate the locator in the Worker against the
   current probe state.
6. Preserve source and runtime candidate lineage through
   `ResolvedTargetEvidence`, `TargetBinding`, pipeline trace, executable DSL,
   and execution evidence.
7. Preserve `grounding.query.v1` and direct semantic LocatorSpec behavior for
   compatibility.

## Non-goals

- Do not make TaskPlan steps coarse-grained or expand one TaskPlan step into
  multiple DSL actions in this increment.
- Do not move browser execution logic into Go.
- Do not let the model submit arbitrary CSS, XPath, artifact paths, or raw
  Observation payloads.
- Do not use Worker process memory as the source of truth for historical
  observations.
- Do not implement ToolCallLedger, Context Materializer, typed
  `ConditionIntent`, or paid model acceptance in this increment.
- Do not add task-specific product names, selectors, URLs, or action order to
  reusable runtime code.

## Authority Boundaries

### Semantic TaskPlan

`agent.task_plan.v2` remains the authority for:

- user goal;
- ordered action-level PlanSteps;
- semantic target and value;
- occurrence count;
- preconditions and completion conditions;
- idempotency and side effect;
- forbidden actions;
- final `TargetBinding` projection used by the DSL compiler.

TaskPlan must not contain unobserved DOM, A11y, selector, element, or
candidate facts.

### BrowserObservation

`browser.observation.v2` remains the authority for page facts captured at a
specific probe and page-state revision:

- A11y and DOM facts;
- runtime state;
- element references;
- locator candidates;
- candidate observed counts;
- content-addressed artifact metadata.

### Dynamic GroundingPlan

`grounding.plan.v1` becomes the authority for the mutable grounding process:

- exact TaskPlan binding;
- current pending PlanStep;
- Observation queries issued for each PlanStep;
- candidate references returned by queries;
- selected candidate reference;
- probe attempts and failure reasons;
- resolved target reference;
- grounding status.

TaskPlan continues to store the final `TargetBinding` so the existing
research-v2 compiler and execution contracts remain compatible.

### Execution

The executable DSL and Execution Report remain the authorities for execution
instructions and actual results respectively.

## GroundingPlan Domain Model

One logical GroundingPlan exists per TaskPlan version and is persisted as a
sequence of immutable revision snapshots. Each query, selection, probe
result, or terminal transition creates the next revision. A TaskPlan
revision supersedes all revisions of the old logical GroundingPlan and
starts a new logical GroundingPlan linked to the new TaskPlan binding.

```text
GroundingPlan
  id
  schema_version = grounding.plan.v1
  run_id
  task_plan_id
  task_plan_version
  task_plan_sha256
  revision
  status
  current_plan_step_id
  steps[]
  content_sha256
  created_at
  updated_at
```

Each GroundingStep is keyed by `plan_step_id`:

```text
GroundingStep
  plan_step_id
  position
  action
  status
  observation_queries[]
  observation_refs[]
  candidate_refs[]
  selected_candidate_ref?
  probe_attempts[]
  resolved_target?
  last_error?
```

GroundingStep statuses are:

- `pending`
- `querying`
- `candidates_available`
- `candidate_selected`
- `probing`
- `grounded`
- `failed`
- `blocked`

GroundingPlan statuses are:

- `active`
- `ready`
- `failed`
- `blocked`
- `superseded`

The first implementation stores the GroundingPlan document as validated
JSONB in a dedicated `grounding_plans` table. This avoids duplicating the
TaskPlan step schema while preserving an independent persistence and
versioning boundary.

## Candidate Reference Contract

The public reference is opaque to the model except for stable identity:

```json
{
  "schema_version": "grounding.candidate-ref.v1",
  "source_event_seq": 27,
  "probe_id": "probe_f47d...",
  "observation_id": "obs_c2d8...",
  "candidate_id": "candidate_ac5d..."
}
```

The reference does not contain a selector. It is only valid inside the
current Agent Run.

The Go resolver must:

1. Load event `source_event_seq` from the current Run.
2. Require event type `tool.result`.
3. Require payload schema `agent.tool_result.v1`.
4. Require the source tool to be `explore_page` or `explore_flow`.
5. Find exactly one matching Observation by `probe_id` and
   `observation_id`.
6. Find exactly one candidate by `candidate_id`.
7. Require source `observed_count == 1`.
8. Require the source element to be visible and actionable for the requested
   action.
9. Reject frame context paths in v1 until frame-aware compilation is
   supported; retain normal Playwright shadow DOM behavior.
10. Return a trusted internal resolved-candidate object containing the
    locator, element, context, page-state SHA, and source reference.

The model cannot provide or override this internal object.

## Observation Query Tool

Add `query_observation` as a read-only Agent tool.

Request:

```json
{
  "schema_version": "grounding.observation-query.v1",
  "plan_step_id": "submit_search",
  "source_event_seq": 27,
  "observation_id": "obs_c2d8...",
  "action": "click",
  "query": "submit_search",
  "role": "button",
  "limit": 20
}
```

`plan_step_id`, `source_event_seq`, and `action` are required. Query,
role, observation ID, page-state ID, and candidate ID are optional filters.

The tool:

1. Verifies the referenced PlanStep belongs to the current Semantic
   TaskPlan and is the current pending step.
2. Reads the complete Observation from the current Run's persisted
   `tool.result`.
3. Searches A11y name, role, DOM tag/text/attributes, locator fields, and
   candidate ID.
4. Returns only source-actionable candidates with bounded result count.
5. Includes a reusable `candidate_ref` for every match.
6. Records query and candidate options in the current GroundingPlan.

Result:

```json
{
  "schema_version": "grounding.observation-query-result.v1",
  "plan_step_id": "submit_search",
  "source_event_seq": 27,
  "matches": [
    {
      "candidate_ref": {
        "schema_version": "grounding.candidate-ref.v1",
        "source_event_seq": 27,
        "probe_id": "probe_f47d...",
        "observation_id": "obs_c2d8...",
        "candidate_id": "candidate_ac5d..."
      },
      "element_ref": "S0:42",
      "role": "button",
      "name": "",
      "dom": {
        "tag": "button",
        "attrs": {"id": "submit_search", "type": "button"}
      },
      "locator": {"kind": "css", "value": "#submit_search"},
      "provenance": "a11y_backend_dom_node",
      "observed_count": 1
    }
  ],
  "omitted_count": 0
}
```

The result is already bounded and gets a dedicated model summary rather than
the current generic top-level-key summary.

## GroundingQuery v2

Add `grounding.query.v2`. Keep v1 accepted for compatibility.

Each action must provide exactly one of:

```text
locator
candidate_ref
```

`locator` remains restricted to semantic locator kinds.

Before calling the Worker, Go replaces each `candidate_ref` with an internal
`resolved_candidate` object. It removes the public reference from the Worker
request and rejects model-supplied internal fields.

The Worker accepts exactly one of:

```text
locator
resolved_candidate
```

For a resolved candidate, the Worker:

1. Compiles the trusted locator in the new probe.
2. Requires runtime count one.
3. Rechecks visible, enabled, and editable state.
4. Executes the action.
5. Builds a new runtime `ResolvedTargetEvidence`.
6. Includes the source candidate reference in that evidence.

The current probe candidate remains the selected candidate in the final
TargetBinding. The source reference is retained as lineage, not reused as a
runtime identity.

## State Transitions

```text
TaskPlan created
  -> GroundingPlan active
  -> PlanStep pending
  -> explore_page/explore_flow persists Observation
  -> query_observation records candidates_available
  -> candidate_ref selected
  -> explore_flow records probing
  -> Worker runtime validation
     -> success: ResolvedTarget -> TargetBinding -> both plans grounded
     -> failure: GroundingStep failed with recoverable reason
  -> all TaskPlan steps grounded
  -> GroundingPlan ready
  -> TaskPlan ready_for_generation
```

TaskPlan remains the gate for DSL generation. GroundingPlan readiness is an
additional invariant: both states must agree before `generate_dsl` is
authorized.

## Persistence and Migration

Add a compatible `grounding_plans` table:

```text
id varchar primary key
schema_version varchar
run_id varchar not null references agent_runs(id) on delete cascade
task_plan_id varchar not null references task_plans(id) on delete cascade
task_plan_version integer not null
revision integer not null
status varchar not null
current_plan_step_id varchar null
content_json jsonb not null
content_sha256 varchar(64) not null
created_at timestamp not null
updated_at timestamp not null
unique(task_plan_id, revision)
```

Every row is one immutable GroundingPlan revision; the current revision is
the highest revision for the TaskPlan. The schema is additive. Existing runs
and TaskPlans remain readable. A GroundingPlan is lazily created for an old
active TaskPlan if one is absent.

## Events and Model Context

Add `grounding_plan.updated` to Agent events and frontend event typing.

The model-visible TaskPlan summary gains a compact GroundingPlan projection:

- grounding plan ID/revision/status;
- current PlanStep ID;
- per-step status;
- last query source event;
- candidate count;
- selected candidate reference;
- last failure category.

Complete GroundingPlan state remains in PostgreSQL and event payloads, not in
the model transcript.

## Error Handling

Candidate-reference failures are recoverable tool failures and must not
terminate the Run:

- source event missing or not in the current Run;
- source event is not an exploration result;
- Observation/probe mismatch;
- candidate missing or duplicated;
- source observed count is not one;
- source element is not actionable;
- unsupported frame context;
- current runtime count is zero or multiple;
- current element is hidden, disabled, or non-editable.

Errors must distinguish source validation from current runtime staleness so
the model can either query another candidate or request a fresh Observation.

## Security and Consistency

- Resolve references only from the current Run.
- Never accept a selector bundled with `candidate_ref`.
- Never trust model-supplied element, page-state, or observation hashes.
- Verify event schema, tool type, Observation schema, and candidate
  uniqueness.
- Keep full source lineage in ResolvedTarget and TargetBinding.
- Continue requiring TaskPlan authorization, contiguous pending steps,
  exploration budgets, DSL validation, and user approval.

## Compatibility

- Existing `grounding.query.v1` requests continue to work.
- Existing TargetBinding and executable DSL payloads remain readable.
- New source candidate lineage fields are optional on old data and required
  for candidate-reference-created bindings.
- Existing TaskPlans without GroundingPlans are lazily projected when first
  accessed.
- No frontend workflow change is required beyond recognizing the new event
  type.

## Verification

Acceptance does not use a paid model call.

Required evidence:

1. Cross-language JSON Schema and Pydantic tests for candidate reference,
   observation query result, GroundingQuery v2, and source lineage.
2. Go tests for current-Run event resolution, cross-Run rejection, candidate
   uniqueness, actionability, query filtering, GroundingPlan persistence,
   state transitions, Tool Schema, model summary, and TaskPlan gate.
3. Python tests for v1/v2 compatibility, trusted resolved-candidate
   validation, runtime zero/multiple/stale rejection, action compatibility,
   and source lineage.
4. Real Chromium test that:
   - observes an unnamed icon button;
   - queries and selects its CSS-backed candidate;
   - executes a later isolated probe using only `candidate_ref`;
   - returns a successful ResolvedTarget with source and runtime lineage.
5. Full Go tests, vet, and build.
6. Full Python tests, compileall, Pyright, and Ruff checks used by the
   repository.
7. Documentation and log updates, followed by a focused commit and direct
   push to the current branch.

## Rejected Alternatives

### Worker-owned observation cache

Rejected because Worker restarts and horizontal scaling would invalidate
references, and the cache would not enforce current-Run ownership.

### Model resubmits locator from query result

Rejected because it lets the model modify trusted CSS/XPath and recreates the
identity gap.

### Store grounding attempts only inside TaskPlan

Rejected because it keeps business semantics coupled to dynamic browser
hypotheses and prevents the requested two-plan architecture.

### Coarse TaskPlan with one-to-many action expansion

Deferred. GroundingPlan v1 retains fields that can support future action
expansion, but this increment keeps one action-level PlanStep mapped to one
DSL occurrence.
