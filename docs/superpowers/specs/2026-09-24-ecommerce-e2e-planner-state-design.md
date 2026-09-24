# Ecommerce E2E Planner State Design

## Status

Approved direction, pending written-spec review.

## Goal

Given one user input containing the website URL, test credentials, product, target quantity, and expected outcome,
the AI autonomously authors and dry-runs one executable browser case that:

1. logs in;
2. searches for `Blue Top`;
3. opens the matching product;
4. sets quantity to `3` on the product detail page;
5. adds the product to the cart;
6. verifies the cart row contains `Blue Top` with quantity `3`.

The planner must keep prompt growth bounded without sacrificing provider prompt-cache reuse.

## User Contract

The user supplies all required material once in the goal text. Credentials are ordinary test data for this project:

- no `secret_ref` or credential-vault subsystem;
- no redaction requirement for model input, case payloads, events, or the local SQLite database;
- no follow-up question unless required information is genuinely missing or the site presents a user-only blocker.

The baseline never checks out, places an order, or deletes the account.

## Actual Site Semantics

The target is `https://www.automationexercise.com/`.

- Login page: `/login`; success marker: `Logged in as AI Web Test`.
- Product page: `/products`.
- Search input: `#search_product`.
- Search submit: icon-only `button#submit_search`, `type=button`; pressing Enter does not submit.
- Search result: `Blue Top`, product id `1`.
- Detail page: `/product_details/1`.
- Quantity control: `input#quantity`, default value `1`.
- Add result: modal containing `Added!`, `View Cart`, and `Continue Shopping`.
- Cart page: `/view_cart`.
- Cart quantity is a non-editable button with class `disabled`.

Therefore "change quantity to 3" means setting quantity on the detail page before adding the item. The cart page only
verifies the resulting quantity.

## Evidence From the Current Runtime

Run `run_12c6a72b94352d07` reached login, search, product detail, quantity `3`, and add-to-cart, then failed before
opening the cart:

- 23 model calls;
- 1,585,670 prompt tokens;
- 1,468,672 cached prompt tokens;
- 19,318 completion tokens;
- 1,604,988 total tokens;
- prompt size grew from 8,243 to 123,875 tokens per call;
- cache hit rate was 92.62%;
- uncached prompt tokens were 116,998.

The failure proves that provider caching reduces repeated-prefix cost but does not solve context growth:

- cached tokens still count in the current `LOOP_MAX_TOTAL_TOKENS` fuse;
- per-call context and latency continue to grow;
- the model must reason over stale observations and abandoned attempts;
- the runtime reaches the raw-token fuse despite a high cache-hit rate.

The run also exposed two state errors:

1. `drop_last_step` allowed the model to remove four successful steps, while the authoring browser stayed on the later
   page. The case and browser state diverged.
2. Authoring `act` returned immediately after clicking `Add to cart`. It did not wait for `expect_text: Added!`, so
   the next PageView omitted the modal's `View Cart`. The model selected the header `Cart`, which the modal correctly
   blocked as an overlay.

## Design Principles

1. One user input, autonomous AI execution.
2. The backend owns state; conversation history is not state.
3. A step is committed only after its authoring action and expectations pass.
4. Committed steps are immutable to the model.
5. Failed attempts never enter the case.
6. Repair preserves a verified prefix and rebuilds only the failed tail.
7. Full observations remain server-side; the model receives one bounded current view.
8. Stable prompt content stays before mutable content to preserve prefix caching.
9. Repeating an identical failed strategy on an unchanged page is rejected by the backend.
10. The final case still requires a passing dry run in a fresh browser context.

## Planner State Model

The planner owns an explicit state object:

```go
type PlanningState struct {
    Goal             string
    CommittedSteps   []contract.Step
    CurrentPage      *planner.PageView
    LastResult       *planner.CompactResult
    FailureLedger    []planner.FailureSignature
    StateVersion     int
    ModelCalls       int
    Usage            usage.Usage
}
```

`CommittedSteps` is the only case source. Model conversation messages are disposable transport.

### Step Lifecycle

```text
proposed
  -> derived and validated
  -> executed in authoring browser
  -> expectations awaited
  -> committed
```

If derivation, target resolution, action execution, or expectation evaluation fails:

- do not append the step;
- record a failure signature;
- restore the browser to the committed prefix;
- return the restored current PageView and compact failure to the model.

There is no model-accessible operation that removes a committed step.

### Dry-Run Repair

When a fresh-context dry run fails at step `k`:

1. steps `0..k-1` are the verified prefix;
2. steps `k..end` are removed by the backend;
3. the authoring browser session is recreated;
4. the backend replays steps `0..k-1`;
5. replay must pass before planning resumes;
6. the model receives the page after step `k-1`, the failed-step summary, and repair hints.

If replay fails, the run fails with a structured `committed_prefix_replay_failed` error. The model is not allowed to
paper over an invalid prefix.

`drop_last_step` is removed from the exposed tool list. Existing scripted fixtures are migrated to the backend-owned
repair behavior.

## Authoring Action Handshake

The current authoring request executes only the mechanical action. It must instead carry the derived expectations:

```go
type ActRequest struct {
    Action         Action
    Locator        Locator
    Value          string
    Submit         bool
    Postconditions []Condition
}
```

The worker performs:

1. target resolution;
2. action execution;
3. postcondition polling using the same condition evaluator as case execution;
4. observation only after all postconditions pass.

On unmet expectations, the worker returns:

- structured condition results;
- latest URL;
- blocker and hit-test data when present;
- a fresh observation of the resulting page.

This guarantees that a successful `Add to cart` authoring action exposes the `Added!` modal and its `View Cart`
candidate in the next PageView.

## Rolling Model Context

Every model call is built from current planner state rather than appending the full conversation:

```text
1. System prompt                         stable
2. Original user goal                    stable
3. Canonical committed-step summary      append-only stable prefix
4. Current PageView                      mutable
5. Last compact result                   mutable
6. Failure ledger                        bounded mutable tail
7. Remaining budgets                     mutable
```

Assistant prose, previous PageViews, previous tool responses, and superseded failures are not resent.

The state message uses deterministic field ordering and canonical step serialization. Stable content appears before
mutable fields, preserving provider prefix-cache reuse.

The full `contract.Observation` remains in `Planner` for target resolution. Context compaction affects only what the
model sees.

## PageView Budget

The model-facing PageView is ranked, not sliced in DOM order:

1. blockers and their dismiss candidates;
2. candidates related to the last action or expected transition;
3. high-confidence form submit and dialog candidates;
4. candidates matching goal terms;
5. elements inside goal-relevant scopes;
6. remaining visible actionable elements.

Default model-facing limits:

- 20 elements;
- 12 action candidates;
- 8 scopes;
- 5 blockers;
- 120 characters per element text;
- 160 characters per scope or blocker summary.

The view includes truncation reasons and counts for omitted items. The backend still resolves against the complete
observation.

## Failure Ledger

```go
type FailureSignature struct {
    PageFingerprint string
    Action          contract.Action
    TargetKey       string
    ErrorCode       string
}
```

`TargetKey` is the candidate id when present; otherwise it is the canonical normalized `TargetSpec` or hint.
`PageFingerprint` is a deterministic SHA-256 hash of the normalized URL, title, sorted candidate ids, visible control
values, and blocker kinds. It deliberately does not use the random `Observation.PageStateID`, so replaying an
equivalent page still detects the same failed strategy.

Rules:

- reject an identical signature on the same page fingerprint;
- allow the same target after the page state changes;
- retain at most eight recent unique signatures;
- return `strategy_repeated` without calling the worker;
- tell the model which dimension must change: target, scope, action, or page state.

## Usage and Fuses

Keep existing raw usage fields and add:

```go
FreshPromptTokens = max(PromptTokens - CachedTokens, 0)
FreshTotalTokens  = FreshPromptTokens + CompletionTokens
```

Use four independent guards:

1. maximum model calls;
2. maximum serialized request bytes before a call;
3. maximum prompt tokens for one completed call;
4. maximum cumulative fresh tokens.

Raw total tokens remain visible but are not the sole cost fuse. Initial targets for the ecommerce baseline:

- at most 25 model calls;
- at most 96 KiB serialized messages per call;
- at most 30,000 prompt tokens per call;
- at most 600,000 raw total tokens for the full successful run;
- no repeated failure signature.

The 600,000 raw-token target is an acceptance target, not a reason to hide actual usage.

## Ecommerce Baseline

The canonical input contains:

- entry URL;
- account email and password;
- expected login name;
- product `Blue Top`;
- target quantity `3`;
- no checkout and no account deletion.

Expected semantic path:

```text
open home
click Signup / Login
input email
input password
click Login
verify Logged in as AI Web Test
click Products
input Blue Top
click search candidate
verify Searched Products
click View Product within Blue Top card
input quantity 3
click Add to cart
click modal View Cart
verify Blue Top cart row and quantity 3
finish_case
```

The LLM remains autonomous. No ecommerce-specific action or hard-coded site selector is added to planner logic.

## Testing Strategy

### Runtime Context Tests

- each model call contains one PageView only;
- old assistant and tool messages are absent;
- original goal and committed prefix remain;
- canonical prefix serialization is stable;
- request byte limit rejects oversized state before an LLM call;
- cached and fresh token counters are accumulated independently.

### State Machine Tests

- failed authoring actions do not append steps;
- successful authoring actions become committed;
- committed steps cannot be removed by a model tool;
- duplicate failure signatures return `strategy_repeated`;
- dry-run failure preserves the passed prefix;
- browser replay restores the exact URL and observable state before tail repair.

### Worker Tests

- authoring `Add to cart` waits until `Added!` is visible;
- the returned observation includes modal `View Cart`;
- an unmet authoring expectation returns conditions and a fresh observation;
- existing blocker recovery behavior remains unchanged.

### Offline Ecommerce Fixture

Add a deterministic fixture covering:

- login form and logged-in marker;
- icon-only `type=button` search submit;
- `Blue Top` product card and scoped `View Product`;
- detail quantity input;
- asynchronous added-to-cart modal;
- modal `View Cart`;
- cart table row with quantity `3`.

Run the complete goal through scripted LLM + real Playwright worker. The test must reach a passing report without
special ecommerce actions.

### Live Canary

Run the canonical input against `https://www.automationexercise.com/`.

Pass criteria:

- planning reaches `awaiting_approval`;
- dry run passes;
- approved execution passes;
- cart row shows `Blue Top` and quantity `3`;
- zero report signals;
- model-call and token targets are met;
- screenshots are accessible through the control plane.

## Non-Goals

- credential vaults, `secret_ref`, or redaction;
- checkout or order placement;
- account creation or deletion during each run;
- ecommerce-specific planner actions;
- replacing the model with a fixed ecommerce script;
- cross-industry P6 benchmarks in this change.

## Migration

- preserve stored historical cases and executions;
- update scripted LLM fixtures that call `drop_last_step`;
- add new usage fields without removing existing raw counters;
- keep the public run/session API backward compatible;
- document changed fuse semantics in `README.md`, `CONTRACT.md`, and `test.config.json`.

## Acceptance

The refactor is complete only when:

1. the full offline ecommerce baseline passes;
2. all existing five test layers pass;
3. the live ecommerce canary passes under the agreed context and token limits;
4. a failed attempt cannot delete a committed prefix;
5. every authoring observation reflects satisfied action expectations;
6. the event log explains any rejected duplicate strategy or replay failure.
