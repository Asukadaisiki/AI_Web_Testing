# Agentic Research Pilot Implementation

Date: 2026-09-06

Status: Pilot validated

Target: <https://automationexercise.com>

Source roadmap: `AI_Web_Testing_Agentic_Research_SOP.md`

## 1. Goal

Establish a reproducible research baseline on the existing platform without
introducing a second Agent or bypassing structured DSL validation.

The first concrete goal is:

> Given an anonymous browser session on Automation Exercise, search for
> `Blue Top`, open the product detail page, add the item to the cart, and verify
> its name, unit price, quantity, and total using structured DSL and step-level
> evidence.

The machine-readable definition is:

`research/goals/automationexercise-blue-top-cart.json`

### Success criteria

1. The same goal can be executed repeatedly from a clean browser context.
2. Every DSL step emits structured evidence.
3. The final cart contains `Blue Top`, `Rs. 500`, and quantity `1`.
4. The baseline result contains success, timing, recovery, and vision metrics.
5. VLM is disabled for the first baseline.
6. No login, account creation, checkout, payment, or destructive operation is
   performed.

## 2. Scope

### Included in the pilot

- One versioned research Goal.
- One deterministic Candidate + DSL + Verification baseline.
- A standalone Browser Worker smoke runner for rapid feasibility checks.
- Structured JSON output that can later be projected into a trajectory.
- Unit tests for goal loading and baseline metric calculation.
- A real Chromium run against Automation Exercise.
- A detailed Week 1-12 implementation backlog.

### Excluded from the pilot

- A production research API or frontend.
- Full PostgreSQL trajectory persistence.
- Direct free-form LLM control of Playwright.
- Contextual Bandit, PPO, or another learned policy.
- VLM as a default locator.
- Account registration, login, checkout, or payment.

The standalone runner is research tooling only. Official platform runs remain
Go-controlled and must pass DSL validation and approval.

## 3. Current capability map

| SOP capability | Current state | Pilot action |
|---|---|---|
| AgentRun and replay | Available | Reuse |
| ToolCall and Checkpoint | Available | Reuse |
| Candidate extraction | Available | Reuse |
| Structured DSL | Available | Reuse |
| Postcondition verification | Partial | Use stable checks; fix measurement defects before ablation |
| Step evidence | Available | Reuse and export |
| Failure taxonomy | Six broad categories | Add stage and reason code |
| Recovery | Static category mapping | Record as baseline |
| Trajectory | Missing | Implement in Week 1-2 |
| Research metrics | Partial | Add normalized metric projector |
| Experiment variants | Missing | Implement in Week 5-6 |
| Observation routing | Missing | Implement in Week 9 |
| Bandit/RL | Missing | Implement after stable datasets |

## 4. Architecture

### Control plane

Add `backend-go/internal/research/` after the pilot:

```text
research/
  types.go
  store.go
  service.go
  projector.go
  metrics.go
  export.go
```

Go owns:

- experiment definitions;
- dataset and prompt versions;
- variant assignment;
- run orchestration;
- trajectory projection;
- reward and metric calculation;
- export;
- policy publication.

### Browser Worker

Python continues to own only:

- A11y and DOM observations;
- candidate extraction and deterministic scoring;
- Playwright actions;
- precondition and postcondition checks;
- screenshots, console, network, locator, and failure evidence.

Python must not gain an Agent loop, experiment scheduler, reward policy, or
training logic.

### Data model

Add three PostgreSQL tables:

```text
research_experiments
  id
  project_id
  name
  dataset_version
  model_name
  prompt_version
  seed
  config_json
  status
  created_at

research_runs
  id
  experiment_id
  task_key
  variant
  repetition
  agent_run_id
  execution_batch_id
  status
  success
  metrics_json

research_transitions
  research_run_id
  ordinal
  agent_step_id
  phase
  state_before_json
  observation_json
  candidates_json
  action_json
  execution_json
  verification_json
  failure_stage
  failure_code
  recovery_strategy
  cost_json
  reward
  done
  created_at
```

Required uniqueness:

- `(experiment_id, task_key, variant, repetition)`
- `(research_run_id, ordinal)`

Large screenshots, raw reports, and transcripts remain in existing storage.
Transitions contain summaries, hashes, and references.

## 5. Delivery phases

### Phase R0: Feasibility pilot

Deliverables:

- versioned Automation Exercise goal;
- standalone baseline runner;
- structured result artifact;
- real browser evidence;
- known measurement defects recorded.

Exit criteria:

- unit tests pass;
- Chromium completes all goal steps;
- result JSON reports `task_success=true`;
- no VLM call occurs.

Validation result:

- Two consecutive live-site runs passed all 13/13 DSL steps.
- `task_success`, `execution_success`, and `verification_success` are true.
- `recovery_count=0` and `vision_calls=0`.
- The final screenshot contains `Blue Top`, unit price `Rs. 500`, quantity `1`,
  and total `Rs. 500`.
- The two confirming runs completed in approximately 11.3 and 11.6 seconds.

Full Agent validation:

- Natural-language AgentRun: `run_70b96dff4dfc833516f4d0a7`.
- Approved DSL generation: `33`.
- Official execution batch: `33`.
- Persisted execution: `26`.
- Result: 18/18 steps passed with deterministic analysis `all_passed`.
- Post-fix rerun: Execution `28`, 18/18 steps passed, duration `11990ms`.
- The flow exercised Go Agent/LLM, Browser Worker exploration, element
  validation, DSL generation, user checkpoint approval, PostgreSQL queue,
  Execution Worker, Playwright, report aggregation, and final Agent response.

### Phase R1: Trajectory foundation

Tasks:

1. Define Go `Experiment`, `ResearchRun`, `Transition`, and `RunMetrics`.
2. Add compatible Alembic migration for the three research tables.
3. Implement repository interfaces and PostgreSQL adapters.
4. Link `research_run` to `agent_run` and `execution_batch`.
5. Project existing Agent events and execution reports into ordered transitions.
6. Make projection idempotent.
7. Export `trajectory.jsonl`.

Exit criteria:

- one run can be replayed from sequence zero;
- every transition has stable ordering;
- rerunning projection creates no duplicate transitions;
- JSONL validates against a versioned schema.

### Phase R2: Measurement integrity

Tasks:

1. Extend model responses with model name, input/output tokens, and latency.
2. Add failure `stage` and stable `code` without replacing current categories.
3. Persist precondition and postcondition results in step evidence.
4. Correct pre-state capture ordering.
5. Implement real `network_request` observation.
6. Prevent unsafe repetition of non-idempotent actions after failed verification.

Exit criteria:

- metric values can be derived entirely from stored data;
- no verifier path reports placeholder success;
- failed verification does not silently duplicate a click or submission.

### Phase R3: DSL as action IR

Add to interactive steps:

```json
{
  "intent": "Add the selected product to the cart",
  "target": "button \"Add to cart\"",
  "preconditions": [
    {"type": "visible"},
    {"type": "enabled"}
  ],
  "action": "click",
  "postconditions": [
    {"type": "text_visible", "value": "Added!"}
  ]
}
```

Compatibility policy:

- existing cases may omit `intent` and `preconditions`;
- research dataset v1 requires both;
- Go validates first, then Python validates and executes;
- unsupported fields or actions fail before queue submission.

### Phase R4: Baseline and ablation

Implement four initial variants:

| Variant | Candidate set | Action IR | Verification |
|---|---:|---:|---:|
| direct | no | primitive wrapped in valid DSL | no |
| candidate | yes | primitive wrapped in valid DSL | no |
| dsl | yes | yes | no |
| dsl_verification | yes | yes | yes |

`direct` remains a valid DSL envelope. It must not expose free-form browser
control in the official executor.

Each variant runs:

- the same task version;
- the same model and prompt version;
- a clean browser context;
- at least 20 repetitions before drawing conclusions;
- randomized execution order;
- separately reported warm-up runs.

### Phase R5: Diagnosis and recovery

Introduce:

- deterministic failure stage and reason code;
- recovery strategy contract;
- bounded retry budget;
- recovery outcome and recovery cost;
- rollback only where state restoration is defined.

Initial strategies:

```text
RE_EXPLORE
RE_GROUND
REGENERATE_DSL
RETRY_TRANSIENT
RESET_CONTEXT
MANUAL
```

### Phase R6: Adaptive observation

Only after R0-R5 produce stable data:

1. Introduce an observation router in Go.
2. Compare A11y, A11y+DOM, and optional Vision.
3. Record the decision, confidence, reason code, cost, and outcome.
4. Start with an explicit deterministic policy.
5. Train a contextual bandit only when offline evaluation data is sufficient.
6. Use sequential RL only if transition dependency is empirically significant.

## 6. Metrics

Required per run:

```text
task_success
grounding_accuracy
invalid_action_rate
execution_success
verification_success
recovery_rate
average_steps
average_retries
llm_calls
input_tokens
output_tokens
latency_ms
vision_calls
```

Required experiment controls:

- goal version;
- dataset version;
- browser version;
- viewport;
- model and prompt version;
- seed;
- variant;
- repetition;
- VLM enabled flag;
- code commit SHA.

## 7. Test strategy

### Unit

- Goal schema validation.
- Transition projection and idempotency.
- Metric formulas.
- Failure category, stage, and code mapping.
- Execution profile isolation.
- Reward calculation.

### Contract

- Go and Python accept the same DSL fixture.
- Research event payloads validate against a versioned schema.
- Reports preserve candidate, selected match, verification, and failure data.

### PostgreSQL integration

- Migration from the current head.
- Foreign keys and uniqueness.
- Concurrent sequence allocation.
- Run completion and retry lineage.
- JSONL export ordering.

### Real browser

- Automation Exercise Blue Top cart baseline.
- Chromium first; Firefox and WebKit after the baseline stabilizes.
- VLM disabled for deterministic runs.
- Browser context reset for every repetition.

## 8. Commands

Run the isolated pilot:

```bash
cd browser-worker
uv sync --frozen
uv run playwright install chromium
uv run python scripts/run_research_smoke.py
```

Run its unit tests:

```bash
cd browser-worker
uv run python -m unittest tests.test_research_smoke_runner -v
```

Run the repository gates:

```bash
cd backend-go
go test ./...
go vet ./...
go build ./...

cd ../browser-worker
uv run python -m compileall -q app scripts tests
uv run python -m unittest discover -s tests -p "test_*.py"

cd ../frontend
npm test
npm run build
```

## 9. Risks and controls

| Risk | Control |
|---|---|
| Third-party ads or overlays | Click preprocessor; classify separately from product failure |
| Site content changes | Version goal and capture final URL/evidence |
| External network instability | Separate network failures from locator failures |
| Repeated non-idempotent action | Stop after verification failure until safe retry semantics exist |
| VLM changes baseline | Disable VLM in R0 |
| Benchmark leaks into production | Keep pilot runner outside API and preserve Go approval gate |
| False research conclusions | Require repeated, randomized runs and confidence intervals |

## 10. Definition of done

The first research milestone is done when:

1. The Automation Exercise goal passes repeatedly from a clean context.
2. An AgentRun and its browser execution can be projected into one ordered
   trajectory.
3. The trajectory can be exported as versioned JSONL.
4. All required baseline metrics are computed from stored evidence.
5. Direct, Candidate, DSL, and DSL+Verification variants are reproducible.
6. No variant bypasses structured validation in an official platform run.
