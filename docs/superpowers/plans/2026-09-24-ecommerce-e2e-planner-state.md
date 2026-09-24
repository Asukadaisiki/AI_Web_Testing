# Ecommerce E2E Planner State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the one-input AI planner reliably complete login, product search, quantity selection, add-to-cart, and cart verification while keeping model context bounded and preserving prompt-cache reuse.

**Architecture:** Replace accumulated chat history with a canonical planner-state prompt containing the original goal, immutable committed steps, one current PageView, one compact result, and a bounded failure ledger. Make authoring actions transactional by waiting for their postconditions before committing; on failure, recreate the browser and replay the committed prefix. Keep complete observations server-side for grounding and send only ranked, bounded views to the model.

**Tech Stack:** Go 1.23 control plane and planner, Python 3.11/Pydantic/FastAPI/Playwright worker, SQLite usage store, React/TypeScript usage display, repository-local HTML fixtures.

**Spec:** `docs/superpowers/specs/2026-09-24-ecommerce-e2e-planner-state-design.md`

## Global Constraints

- The user supplies URL, account, password, product, quantity, and expected result once in the goal.
- Credentials are ordinary test data; do not add redaction, vault, or `secret_ref` behavior.
- Do not add ecommerce-specific production actions or hard-coded selectors.
- A model cannot remove a committed step.
- Failed authoring attempts never enter the case.
- Full observations remain server-side; one bounded current PageView is sent to the model.
- Keep existing run/session HTTP APIs backward compatible.
- Every behavior change follows red-green-refactor.
- Do not configure Git author identity; commit steps run only if repository identity is already available.

---

### Task 1: Separate Raw, Cached, and Fresh Token Accounting

**Files:**
- Modify: `backend/internal/usage/usage.go`
- Modify: `backend/internal/usage/usage_test.go`
- Modify: `backend/internal/store/usage.go`
- Modify: `backend/internal/store/usage_test.go`
- Modify: `backend/internal/agentruntime/loop.go`
- Modify: `backend/internal/agentruntime/loop_usage_test.go`
- Modify: `backend/cmd/loopd/main.go`
- Modify: `backend/cmd/loopd/main_test.go`
- Modify: `web/src/api.ts`
- Modify: `web/src/components/UsageLine.tsx`
- Modify: `test.config.json`

**Interfaces:**
- Produces: `usage.Usage.FreshPromptTokens`, `usage.Usage.FreshTotalTokens`
- Produces: `Config.MaxFreshTotalTokens`, `Config.MaxPromptTokensPerCall`
- Produces: env vars `LOOP_MAX_FRESH_TOTAL_TOKENS`, `LOOP_MAX_PROMPT_TOKENS_PER_CALL`
- Preserves: existing `PromptTokens`, `CompletionTokens`, `TotalTokens`, `CachedTokens`

- [ ] **Step 1: Write failing usage normalization tests**

```go
func TestCallSeparatesFreshPromptTokens(t *testing.T) {
    got := Call(1000, 100, 20, 800)
    if got.FreshPromptTokens != 200 || got.FreshTotalTokens != 300 {
        t.Fatalf("fresh usage = %+v", got)
    }
}

func TestFreshPromptNeverGoesNegative(t *testing.T) {
    got := Call(100, 10, 0, 120)
    if got.FreshPromptTokens != 0 || got.FreshTotalTokens != 10 {
        t.Fatalf("fresh usage = %+v", got)
    }
}
```

- [ ] **Step 2: Run the usage tests and verify RED**

Run: `cd backend && go test ./internal/usage -run 'TestCallSeparatesFreshPromptTokens|TestFreshPromptNeverGoesNegative'`

Expected: compile failure because the fresh-token fields do not exist.

- [ ] **Step 3: Add derived fresh-token fields**

```go
type Usage struct {
    ModelCalls       int `json:"model_calls"`
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
    ReasoningTokens  int `json:"reasoning_tokens"`
    CachedTokens     int `json:"cached_tokens"`
    FreshPromptTokens int `json:"fresh_prompt_tokens"`
    FreshTotalTokens  int `json:"fresh_total_tokens"`
}

func (u Usage) Normalize() Usage {
    if u.TotalTokens == 0 {
        u.TotalTokens = u.PromptTokens + u.CompletionTokens
    }
    u.FreshPromptTokens = max(u.PromptTokens-u.CachedTokens, 0)
    u.FreshTotalTokens = u.FreshPromptTokens + u.CompletionTokens
    return u
}
```

Derive fresh fields from persisted raw columns when reading aggregate usage; do not add SQLite columns.

- [ ] **Step 4: Add failing runtime fuse tests**

Add tests proving:

```go
// Cached prompt tokens do not consume the fresh-token fuse.
per := usage.Call(1000, 100, 0, 900) // fresh total = 200
// Two calls pass a 500 fresh-token limit even though raw total is 2200.

// A single call reporting 31000 prompt tokens fails a 30000 per-call limit.
```

Expected errors must mention `LOOP_MAX_FRESH_TOTAL_TOKENS` or `LOOP_MAX_PROMPT_TOKENS_PER_CALL`.

- [ ] **Step 5: Run runtime usage tests and verify RED**

Run: `cd backend && go test ./internal/agentruntime -run 'TestFreshTokenBudget|TestPromptTokenPerCallLimit'`

Expected: compile failure because the new config fields and guards do not exist.

- [ ] **Step 6: Implement independent usage guards**

Extend `agentruntime.Config`:

```go
MaxFreshTotalTokens    int
MaxPromptTokensPerCall int
MaxRequestBytes        int
```

Keep `MaxTotalTokens` as an emergency raw-token fuse. Check per-call prompt and cumulative fresh totals after recording
the provider response. Report every raw and fresh counter in `model_usage` events.

- [ ] **Step 7: Wire defaults and frontend types**

Use these defaults:

```text
LOOP_MAX_MODEL_CALLS=25
LOOP_MAX_TOTAL_TOKENS=1500000
LOOP_MAX_FRESH_TOTAL_TOKENS=300000
LOOP_MAX_PROMPT_TOKENS_PER_CALL=30000
LOOP_MAX_REQUEST_BYTES=98304
```

Display cached and fresh totals in `UsageLine` without monetary conversion.

- [ ] **Step 8: Run focused and frontend tests**

Run:

```bash
cd backend && go test ./internal/usage ./internal/store ./internal/agentruntime ./cmd/loopd
cd web && npm run build
```

Expected: all pass.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/usage backend/internal/store/usage.go \
  backend/internal/agentruntime/loop.go backend/internal/agentruntime/loop_usage_test.go \
  backend/cmd/loopd web/src/api.ts web/src/components/UsageLine.tsx test.config.json
git commit -m "feat: track fresh model tokens and bounded usage"
```

---

### Task 2: Build Each Model Call From Canonical Planner State

**Files:**
- Create: `backend/internal/agentruntime/context.go`
- Create: `backend/internal/agentruntime/context_test.go`
- Modify: `backend/internal/agentruntime/loop.go`
- Modify: `backend/internal/agentruntime/llm_scripted.go`
- Modify: `backend/internal/planner/planner.go`
- Modify: `backend/internal/planner/resolve.go`

**Interfaces:**
- Produces: `planner.StateSnapshot`
- Produces: `agentruntime.PlanningContext`
- Produces: `BuildPlanningMessages(context PlanningContext) []Message`
- Produces: `RequestSizer.RequestSize(messages []Message) (int, error)`
- Consumes: Task 1 usage counters and `Config.MaxRequestBytes`

- [ ] **Step 1: Write failing context-shape tests**

Create a recording LLM and assert:

```go
func TestPlanningCallsContainOneCurrentStateInsteadOfHistory(t *testing.T) {
    // Run open_page -> input -> click -> finish_case.
    // Every captured call must contain:
    // system, original goal, committed-prefix message, current-state message.
    // No call may contain an older observation or earlier assistant prose.
}

func TestCommittedPrefixSerializationIsDeterministic(t *testing.T) {
    // Building the same state twice must produce byte-identical messages.
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `cd backend && go test ./internal/agentruntime -run 'TestPlanningCallsContainOneCurrentStateInsteadOfHistory|TestCommittedPrefixSerializationIsDeterministic'`

Expected: failure because `BuildPlanningMessages` and batched call capture do not exist.

- [ ] **Step 3: Add planner snapshots**

```go
type CompactResult struct {
    OK      bool           `json:"ok"`
    Summary string         `json:"summary,omitempty"`
    Warning string         `json:"warning,omitempty"`
    Error   string         `json:"error,omitempty"`
    Detail  string         `json:"detail,omitempty"`
    Failure *DryRunFailure `json:"dry_run_failure,omitempty"`
}

type StateSnapshot struct {
    Version    int                `json:"version"`
    Steps      []StepView         `json:"committed_steps"`
    Page       *PageView          `json:"current_page,omitempty"`
    LastResult *CompactResult     `json:"last_result,omitempty"`
    Failures   []FailureSignature `json:"failure_ledger,omitempty"`
}
```

`Snapshot` must never embed a second PageView inside `LastResult`.

- [ ] **Step 4: Implement canonical message construction**

```go
type PlanningContext struct {
    Goal      string
    State     planner.StateSnapshot
    Usage     usage.Usage
    Remaining BudgetView
}

func BuildPlanningMessages(state PlanningContext) []Message
```

Return fresh messages on every call:

1. system prompt;
2. original goal;
3. canonical committed-step prefix;
4. mutable current state.

Use structs and `encoding/json`; do not assemble state JSON with string concatenation.

- [ ] **Step 5: Replace append-only history in `Runtime.Plan`**

Remove the long-lived `messages = append(messages, ...)` flow. Preserve assistant/tool details in SSE events, but pass
only `BuildPlanningMessages(...)` to `LLM.Next`.

Store an `ask_user` answer in `LastResult`; do not retain its historical tool-call pair.

- [ ] **Step 6: Add and enforce request-size measurement**

`OpenAILLM.RequestSize` must serialize the same request envelope used by `Next`. Abort before the network call when
the request exceeds `MaxRequestBytes`.

- [ ] **Step 7: Run focused tests**

Run:

```bash
cd backend && go test ./internal/agentruntime ./internal/planner
```

Expected: all pass, and captured message batches remain bounded as calls increase.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/agentruntime/context.go backend/internal/agentruntime/context_test.go \
  backend/internal/agentruntime/loop.go backend/internal/agentruntime/llm_scripted.go \
  backend/internal/planner/planner.go backend/internal/planner/resolve.go
git commit -m "refactor: build model prompts from planner state"
```

---

### Task 3: Make Authoring Actions Wait for Their Expectations

**Files:**
- Modify: `backend/internal/contract/observation.go`
- Modify: `backend/internal/worker/client.go`
- Modify: `backend/internal/agentruntime/fakeworker_test.go`
- Modify: `worker/loop_worker/contracts.py`
- Modify: `worker/loop_worker/conditions.py`
- Modify: `worker/loop_worker/sessions.py`
- Modify: `worker/loop_worker/main.py`
- Modify: `worker/tests/test_api.py`
- Modify: `worker/fixtures/site/detail.html`
- Modify: `fixtures/contract/case_contract.json`

**Interfaces:**
- Produces: `contract.ActRequest.Postconditions []Condition`
- Produces: `contract.ActResponse`
- Produces: Python `ActRequest.postconditions` and `ActResponse`
- Consumes: existing condition polling semantics

- [ ] **Step 1: Write a failing worker API test for delayed modal observation**

Update the detail fixture so `Add to cart` reveals `Added!` and `View Cart` after 250 ms. Add:

```python
def test_authoring_act_waits_for_postconditions_before_observing(self) -> None:
    # Navigate to detail.
    # POST /act with Add-to-cart locator and text_visible=Added!.
    # Assert response.status == "passed".
    # Assert response.observation contains View Cart.
```

- [ ] **Step 2: Run the worker test and verify RED**

Run: `cd worker && uv run python -m unittest tests.test_api.SessionLifecycleTest.test_authoring_act_waits_for_postconditions_before_observing`

Expected: response has the old Observation shape and does not wait for the modal.

- [ ] **Step 3: Define the cross-language response**

```go
type ActResponse struct {
    Status      string            `json:"status"`
    Observation Observation       `json:"observation"`
    Conditions  []ConditionResult `json:"conditions"`
    Error       *StepError        `json:"error,omitempty"`
}
```

Mirror it in Pydantic. Extend `ActRequest` with `Postconditions []Condition`.

- [ ] **Step 4: Reuse condition polling in the session manager**

Add a helper in `conditions.py`:

```python
async def evaluate_postcondition_list(
    page: Page,
    conditions: list[Condition],
    *,
    url_before: str,
    target_locator: Locator | None,
) -> list[ConditionResult]:
```

`SessionManager.act` executes the action, waits for every postcondition, then observes. It returns `status="failed"`
with condition details and the fresh observation when an expectation remains unmet.

- [ ] **Step 5: Update the Go worker client and planner**

Change:

```go
func (c *Client) Act(
    ctx context.Context,
    sessionID string,
    request contract.ActRequest,
) (contract.ActResponse, error)
```

Derive the step before authoring execution and send `step.Postconditions`. Append the step only when
`response.Status == "passed"`.

- [ ] **Step 6: Update contract fixtures and run cross-language tests**

Run:

```bash
cd backend && go test ./internal/contract ./internal/worker ./internal/agentruntime
cd worker && uv run python -m unittest tests.test_contract_conformance tests.test_api
```

Expected: all pass with byte-compatible field names.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/contract/observation.go backend/internal/worker/client.go \
  backend/internal/agentruntime/fakeworker_test.go worker/loop_worker/contracts.py \
  worker/loop_worker/conditions.py worker/loop_worker/sessions.py worker/loop_worker/main.py \
  worker/tests/test_api.py worker/fixtures/site/detail.html fixtures/contract/case_contract.json
git commit -m "feat: wait for authoring action expectations"
```

---

### Task 4: Replace Mutable Step Stack With Committed Prefix Replay

**Files:**
- Create: `backend/internal/planner/replay.go`
- Create: `backend/internal/planner/replay_test.go`
- Modify: `backend/internal/planner/planner.go`
- Modify: `backend/internal/planner/tools.go`
- Modify: `backend/internal/agentruntime/prompt.go`
- Modify: `backend/internal/agentruntime/loop_test.go`
- Modify: `backend/internal/agentruntime/fakeworker_test.go`
- Modify: `fixtures/scripts/catalog_alpha.json`

**Interfaces:**
- Produces: `Planner.ReplayCommitted(ctx context.Context, prefixLength int) error`
- Produces: `Planner.PrepareDryRunRepair(ctx context.Context, result contract.ExecutionResult) (Result, error)`
- Removes: model tool `drop_last_step`
- Consumes: Task 3 transactional `ActResponse`

- [ ] **Step 1: Write failing committed-step tests**

Add tests proving:

```go
func TestToolsDoNotExposeDropLastStep(t *testing.T)
func TestFailedAuthoringActionDoesNotCommitStep(t *testing.T)
func TestDryRunFailureKeepsPassedPrefixAndReplaysBrowser(t *testing.T)
func TestReplayFailureStopsPlanning(t *testing.T)
```

The replay test must assert the worker receives a new browser session, replays the exact prefix, and returns the
observation after the last replayed step.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `cd backend && go test ./internal/planner ./internal/agentruntime -run 'Committed|Replay|DropLastStep'`

Expected: `drop_last_step` is still exposed and replay APIs are missing.

- [ ] **Step 3: Implement one replay primitive**

`ReplayCommitted` must:

1. close the current browser session;
2. open a new session for the same domain session id;
3. execute the goto step with `Navigate`;
4. replay each action with its saved locator and postconditions;
5. evaluate assertion steps without mutating the page;
6. set `p.observation` to the final replay observation;
7. leave `p.steps` equal to the requested prefix.

- [ ] **Step 4: Make repair backend-owned**

On dry-run failure at index `k`, truncate to `steps[:k]`, replay that prefix, and return the failure plus restored
PageView. Do not require repeated model calls to pop steps.

On any authoring action that may have changed the page but did not satisfy expectations, replay the existing committed
prefix before returning control to the model.

- [ ] **Step 5: Remove the model mutation tool**

Delete `ToolDropLastStep`, its schema, prompt instructions, dispatch branch, and scripted uses. Keep historical stored
events readable because event type values do not change.

- [ ] **Step 6: Run planner and runtime tests**

Run:

```bash
cd backend && go test ./internal/planner ./internal/agentruntime ./internal/api
```

Expected: all pass and no current script references `drop_last_step`.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/planner/replay.go backend/internal/planner/replay_test.go \
  backend/internal/planner/planner.go backend/internal/planner/tools.go \
  backend/internal/agentruntime/prompt.go backend/internal/agentruntime/loop_test.go \
  backend/internal/agentruntime/fakeworker_test.go fixtures/scripts/catalog_alpha.json
git commit -m "refactor: protect and replay committed planner steps"
```

---

### Task 5: Reject Repeated Failed Strategies

**Files:**
- Create: `backend/internal/planner/failures.go`
- Create: `backend/internal/planner/failures_test.go`
- Modify: `backend/internal/planner/planner.go`
- Modify: `backend/internal/planner/resolve.go`
- Modify: `backend/internal/agentruntime/prompt.go`
- Modify: `backend/internal/agentruntime/loop_test.go`

**Interfaces:**
- Produces: `planner.PageFingerprint`
- Produces: `planner.FailureSignature`
- Produces: error code `strategy_repeated`
- Consumes: Task 2 `StateSnapshot.Failures`

- [ ] **Step 1: Write failing fingerprint and duplicate tests**

```go
func TestEquivalentReplayPageHasSameFingerprint(t *testing.T)
func TestChangedInputValueChangesFingerprint(t *testing.T)
func TestRepeatedFailureIsRejectedBeforeWorkerCall(t *testing.T)
func TestSameCandidateCanRunAfterPageFingerprintChanges(t *testing.T)
```

Use URL, title, sorted candidate ids, visible control values, and blocker kinds. Do not use random
`Observation.PageStateID` as the semantic fingerprint.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd backend && go test ./internal/planner -run 'Fingerprint|RepeatedFailure'`

Expected: missing types and duplicate strategy behavior.

- [ ] **Step 3: Implement deterministic signatures**

```go
type FailureSignature struct {
    PageFingerprint string          `json:"page_fingerprint"`
    Action          contract.Action `json:"action"`
    TargetKey       string          `json:"target_key"`
    ErrorCode       string          `json:"error_code"`
}
```

Hash a canonical JSON payload with SHA-256. Keep the newest eight unique signatures.

- [ ] **Step 4: Guard actions before worker execution**

If a signature already exists for the current fingerprint, return:

```json
{
  "ok": false,
  "error": "strategy_repeated",
  "detail": "this action and target already failed on the unchanged page; change target, scope, action, or page state"
}
```

Do not call the worker and do not append a step.

- [ ] **Step 5: Run tests**

Run:

```bash
cd backend && go test ./internal/planner ./internal/agentruntime
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/planner/failures.go backend/internal/planner/failures_test.go \
  backend/internal/planner/planner.go backend/internal/planner/resolve.go \
  backend/internal/agentruntime/prompt.go backend/internal/agentruntime/loop_test.go
git commit -m "feat: reject repeated planner failure strategies"
```

---

### Task 6: Rank and Bound the Current PageView

**Files:**
- Modify: `backend/internal/planner/planner.go`
- Modify: `backend/internal/planner/resolve.go`
- Modify: `backend/internal/planner/resolve_test.go`

**Interfaces:**
- Produces: `BuildPageView(observation contract.Observation, goal string, last *CompactResult) *PageView`
- Preserves: full `contract.Observation` for resolver use
- Consumes: Task 5 page fingerprint and failure context

- [ ] **Step 1: Write failing ranking tests**

Add one observation with more than 30 irrelevant ad candidates and assert:

```go
func TestPageViewKeepsGoalRelevantFormSubmitAheadOfAds(t *testing.T)
func TestPageViewKeepsVisibleDialogCandidatesAfterAction(t *testing.T)
func TestPageViewReportsOmittedCounts(t *testing.T)
```

The expected search submit candidate must remain visible even when its DOM position is after the old limit.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd backend && go test ./internal/planner -run 'PageViewKeeps|PageViewReportsOmitted'`

Expected: current DOM-order slicing drops the relevant candidate.

- [ ] **Step 3: Implement deterministic relevance ranking**

Rank blockers, candidates, scopes, and elements using:

1. relation to the latest action or expectation;
2. form-submit/dialog candidate kind;
3. exact goal-token matches;
4. confidence;
5. viewport visibility;
6. stable ref tie-break.

Apply limits from the spec and expose omitted counts in `TruncationReason`.

- [ ] **Step 4: Run planner tests**

Run: `cd backend && go test ./internal/planner`

Expected: all pass; resolver behavior remains based on full observations.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/planner/planner.go backend/internal/planner/resolve.go \
  backend/internal/planner/resolve_test.go
git commit -m "feat: rank model page views by current goal"
```

---

### Task 7: Add the Complete Offline Ecommerce Baseline

**Files:**
- Create: `worker/fixtures/site/ecommerce_login.html`
- Create: `worker/fixtures/site/ecommerce_products.html`
- Create: `worker/fixtures/site/ecommerce_detail.html`
- Create: `worker/fixtures/site/ecommerce_cart.html`
- Create: `worker/fixtures/site/ecommerce.js`
- Create: `worker/tests/test_ecommerce_baseline.py`
- Create: `fixtures/scripts/ecommerce_login_cart.json`
- Create: `integration/ecommerce_closed_loop.py`
- Modify: `test.config.json`
- Modify: `run_tests.py`
- Modify: `backend/internal/agentruntime/fakeworker_test.go`
- Modify: `backend/internal/agentruntime/loop_test.go`

**Interfaces:**
- Produces: deterministic login/search/detail/modal/cart fixture
- Produces: scripted tool sequence with quantity `3`
- Produces: zero-cost full control-plane + real Playwright integration layer
- Consumes: Tasks 2-6 planner state behavior

- [ ] **Step 1: Write the failing Playwright baseline test**

The test must construct a case that:

```text
goto login
input email
input password
click Login
click Products
input Blue Top
click icon-only search candidate
click View Product within Blue Top card
input quantity 3
click Add to cart and wait for Added!
click modal View Cart
assert Blue Top row contains quantity 3
```

Assert every step passes and every step has screenshot, URL, console, and network evidence.

- [ ] **Step 2: Run the Playwright test and verify RED**

Run: `cd worker && uv run python -m unittest tests.test_ecommerce_baseline`

Expected: missing fixture files.

- [ ] **Step 3: Implement the deterministic fixture**

Use `sessionStorage` for login and cart state. Make the add modal appear after 250 ms so Task 3's authoring wait is
exercised. Make cart quantity non-editable to match the real site.

- [ ] **Step 4: Run the Playwright test and verify GREEN**

Run: `cd worker && uv run python -m unittest tests.test_ecommerce_baseline`

Expected: one complete ecommerce case passes with evidence.

- [ ] **Step 5: Write the failing full-loop integration**

`integration/ecommerce_closed_loop.py` must:

1. allocate free local ports;
2. start fixture server, worker, and loopd with a temporary data directory;
3. use `fixtures/scripts/ecommerce_login_cart.json`;
4. create one session with the full natural-language goal;
5. wait for `awaiting_approval`;
6. approve;
7. assert report `steps_failed == 0`;
8. assert cart evidence exists;
9. terminate every subprocess in `finally`.

- [ ] **Step 6: Run integration and verify RED**

Run: `cd worker && uv run python ../integration/ecommerce_closed_loop.py`

Expected: failure until the script fixture and new planner behavior are complete.

- [ ] **Step 7: Complete fake-worker and scripted fixture support**

Extend the fake site with login, modal, quantity, cart row, and expectation-aware authoring responses. The script must
use only public planner tools and must not use `drop_last_step`.

- [ ] **Step 8: Add the integration layer to `test.config.json`**

Add:

```json
{
  "name": "完整电商闭环（脚本模型 + 真浏览器）",
  "cwd": "worker",
  "cmd": ["uv", "run", "python", "../integration/ecommerce_closed_loop.py"]
}
```

- [ ] **Step 9: Run the complete offline ecommerce layer**

Run: `python3 run_tests.py --layer 完整电商闭环`

Expected: planning, approval, execution, report, and evidence all pass.

- [ ] **Step 10: Commit**

```bash
git add worker/fixtures/site/ecommerce_*.html worker/fixtures/site/ecommerce.js \
  worker/tests/test_ecommerce_baseline.py fixtures/scripts/ecommerce_login_cart.json \
  integration/ecommerce_closed_loop.py test.config.json run_tests.py \
  backend/internal/agentruntime/fakeworker_test.go backend/internal/agentruntime/loop_test.go
git commit -m "test: add complete ecommerce closed-loop baseline"
```

---

### Task 8: Documentation, Full Regression, and Live Canary

**Files:**
- Modify: `README.md`
- Modify: `CONTRACT.md`
- Modify: `plan/10-baseline.md`
- Modify: `docs/execution-log.md`

**Interfaces:**
- Documents: committed-step lifecycle, rolling context, fresh-token accounting, and ecommerce baseline
- Verifies: all repository layers plus the live target

- [ ] **Step 1: Update documentation**

Document:

- one-input autonomous planning;
- credentials supplied directly in the goal;
- committed steps and backend-owned tail repair;
- authoring expectation wait;
- raw/cached/fresh token meanings;
- all new environment variables;
- canonical ecommerce goal and pass criteria.

- [ ] **Step 2: Run formatting checks**

Run:

```bash
cd backend && test -z "$(gofmt -l .)"
cd worker && uv run python -m compileall -q loop_worker tests
```

Expected: exit code 0 and no Go filenames.

- [ ] **Step 3: Run all offline gates**

Run: `python3 run_tests.py`

Expected: every configured layer passes, including the complete ecommerce closed loop.

- [ ] **Step 4: Run focused invariants**

Run:

```bash
cd backend && go test ./internal/agentruntime -run 'Context|Committed|Replay|Fresh|Repeated'
cd backend && go test ./internal/planner -run 'PageView|Fingerprint|Replay'
cd worker && uv run python -m unittest tests.test_api tests.test_ecommerce_baseline
```

Expected: all pass.

- [ ] **Step 5: Run the live ecommerce canary**

Use the existing test account and the canonical quantity-`3` goal against
`https://www.automationexercise.com/`.

Required result:

```text
planning -> awaiting_approval -> executing -> completed
report: steps_failed=0
cart: Blue Top, quantity=3
model_calls <= 25
prompt_tokens per call <= 30000
total_tokens <= 600000
repeated failure signatures = 0
```

- [ ] **Step 6: Record measured evidence**

Append run id, session id, execution id, per-call prompt maximum, raw/cached/fresh totals, report summary, final URL,
and screenshot checks to `plan/10-baseline.md`.

- [ ] **Step 7: Run final diff and repository checks**

Run:

```bash
git diff --check
git status --short
python3 run_tests.py
```

Expected: no whitespace errors, only intentional files changed, all layers pass.

- [ ] **Step 8: Commit**

```bash
git add README.md CONTRACT.md plan/10-baseline.md docs/execution-log.md
git commit -m "docs: record bounded ecommerce e2e baseline"
```
