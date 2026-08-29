# Dual-Layer Locator Scoring Design

Date: 2026-04-28
Status: Approved
Scope: Full 5-phase implementation (research document dom_selector_scoring_vlm_fallback_research.md)

## Overview

Implement a dual-layer scoring system for element location: generation-time pre-scoring + runtime fine-scoring. Each DSL step carries multiple candidate locators ranked by score. The runner selects the best candidate, executes, verifies postconditions, and falls back on failure. All attempts are logged for data-driven model evolution.

## Architecture

```
Generation (AI Planning):
  explore_page → collect elements → PreScorer → candidates[] with pre_score
  → DSL step carries candidates + postconditions

Execution (Runner):
  candidates → RuntimeScorer → final_score → decide strategy → execute → verify
  → fallback on failure → VLM as last resort → LocatorAttemptLog

Data Loop:
  LocatorAttemptLog → aggregate → calibrate weights → train P_success model
```

## 1. DSL Schema Changes

### 1.1 New Types

```python
class LocatorCandidate(BaseModel):
    strategy: Literal["css", "xpath", "data-testid", "element_id",
                      "role", "label", "placeholder", "text",
                      "tag", "semantic", "vlm"]
    selector: str | None = None
    semantic_value: str | None = None
    pre_score: float  # 0.0-1.0
    pre_features: dict | None = None

class Postcondition(BaseModel):
    type: Literal["url_contains", "url_changes", "text_visible",
                  "text_gone", "element_visible", "element_gone",
                  "network_request", "dom_changed", "value_changed"]
    value: str | None = None
    timeout_ms: int = 3000
```

### 1.2 Step Changes

All steps with `target` fields (ClickStep, InputStep, WaitForStep, AssertTextStep, CaptureTextStep) gain:

```python
candidates: list[LocatorCandidate] = []     # new, optional
postconditions: list[Postcondition] = []    # new, optional
```

Backward compatibility: empty `candidates` triggers legacy fallback chain.

### 1.3 VLM Candidate

```python
LocatorCandidate(
    strategy="vlm",
    selector=None,
    semantic_value="购物车区域右下角的结算按钮",
    pre_score=0.0,  # no pre-score, final fallback
)
```

## 2. Scoring Model

### 2.1 Global Weights (from Excel weight table)

| Dimension | Weight |
|---|---:|
| selector_stability | 0.20 |
| semantic_match | 0.20 |
| uniqueness | 0.15 |
| actionability | 0.15 |
| context_match | 0.10 |
| visual_consistency | 0.10 |
| history_success | 0.05 |
| rank_margin | 0.05 |
| **Total** | **1.00** |

### 2.2 Pre-Scorer Dimensions (generation-time)

| Dimension | How to compute |
|---|---|
| selector_stability | Lookup from DOM feature prior table (Section 2.4) |
| semantic_match | role/name/label/text match with intent |
| uniqueness | match_count==1 → 1.0, match_count==0 → 0.0, else 1/log2(n+1) |
| context_match | parent form/card/table row match |

### 2.3 Runtime Scorer Dimensions (execution-time)

| Dimension | How to compute |
|---|---|
| actionability | JS eval: visible + enabled + receives_events |
| visual_consistency | bbox area > 0, in viewport, not covered |
| history_success | Query LocatorAttemptLog by domain/route/selector_type |
| rank_margin | top1_score - top2_score |

### 2.4 DOM Feature Prior Table (from Excel)

| Selector Type | Prior Score | Global Weight |
|---|---:|---:|
| data-testid / data-test / data-qa | 0.95 | 0.20 |
| role + accessible name | 0.90 | 0.20 |
| label / aria-labelledby / for | 0.85 | 0.18 |
| stable id | 0.80 | 0.15 |
| name / type / value combo | 0.75 | 0.12 |
| aria-label / title / alt | 0.75 | 0.12 |
| placeholder | 0.72 | 0.10 |
| stable visible text | 0.70 | 0.12 |
| href + link text | 0.68 | 0.08 |
| short CSS + stable attributes | 0.60 | 0.08 |
| stable class combo | 0.55 | 0.08 |
| relative XPath | 0.50 | 0.06 |
| bbox / center / overlap (auxiliary) | 0.50 | 0.10 |
| tag name (filter only) | 0.45 | 0.05 |
| nth-child / deep CSS | 0.25 | 0.03 |
| absolute XPath | 0.10 | 0.02 |
| canvas / SVG internal | 0.15 | 0.02 |

Note: The "Global Weight" column in Section 2.4 is informational from the Excel research table, showing per-selector-type importance. It is NOT a separate scoring dimension. The selector type's prior score feeds into the `selector_stability` dimension (weight 0.20) in the 8-dimension model. The selector type also informs `semantic_match` via role/name/text matching.

| Element Type | DOM Prior | VLM Prior | Recommended Features |
|---|---:|---:|---|
| button / role=button | 0.90 | 0.25 | role + name + text + test_id |
| input / textarea | 0.88 | 0.20 | label, placeholder, name, type |
| select / combobox | 0.85 | 0.25 | label, role, name, selected value |
| a[href] / role=link | 0.83 | 0.25 | role=link + name + href |
| checkbox / radio / switch | 0.86 | 0.25 | role + label + checked state |
| div/span (custom) | 0.60 | 0.40 | role, aria-label, text, event listener |
| svg/icon (inside button) | 0.45 | 0.60 | parent role/button, bbox overlap |
| canvas | 0.10 | 0.90 | screenshot grounding, coordinate |
| iframe / shadow DOM | 0.55 | 0.45 | frame locator, shadow piercing, bbox |
| hidden / offscreen | 0.05 | 0.05 | render state (skip) |

### 2.6 DOM/VLM Routing Decision (from Excel)

| Scenario | DOM Weight | VLM Weight | Strategy |
|---|---:|---:|---|
| unique test_id + actionability pass | 0.95 | 0.05 | DOM action + postcondition |
| role/name/label strong + unique | 0.85 | 0.15 | DOM action + screenshot verify |
| repeated buttons in table/card | 0.70 | 0.30 | DOM top-k + context + VLM rerank |
| icon button with aria-label | 0.60 | 0.40 | DOM candidate + VLM rerank |
| only dynamic class/CSS | 0.40 | 0.60 | regenerate candidates + VLM rerank |
| deep nth-child / absolute XPath | 0.25 | 0.75 | skip direct exec + VLM rerank |
| actionability failed | 0.35 | 0.65 | overlay handling → VLM |
| canvas / SVG graphics | 0.20 | 0.80 | VLM grounding → elementFromPoint |
| high-res complex GUI | 0.30 | 0.70 | ROI crop + VLM |
| no DOM candidate + screenshot clear | 0.10 | 0.90 | VLM grounding + learning record |

### 2.7 Hard Rules

```python
HARD_RULES = {
    "match_count_0":       "skip → VLM grounding",
    "not_visible":         "cap_score <= 0.40",
    "disabled":            "cap_score <= 0.30",
    "bbox_zero":           "cap_score <= 0.20",
    "not_receives_events": "cap_score <= 0.45",
    "rank_margin_lt_008":  "trigger VLM rerank",
}
```

### 2.8 Fragility Detection

```python
def detect_fragility(selector: str, element_attrs: dict) -> list[str]:
    flags = []
    if re.match(r'.*[0-9a-f]{8}-|.*\d{10,}|.*random', element_attrs.get("id", ""), re.I):
        flags.append("dynamic_id")
    if re.search(r'_[a-zA-Z0-9]{5,}', element_attrs.get("class", "")):
        flags.append("css_module_hash")
    if selector.startswith("/") and selector.count("/") > 5:
        flags.append("deep_xpath")
    if "nth-child" in selector or "nth-of-type" in selector:
        flags.append("nth_child")
    return flags
```

Each fragility flag reduces context_match by 0.15, floored at 0.0:
`max(0.0, context_match - len(flags) * 0.15)`.

### 2.9 Score Fusion

Not a simple weighted average of pre + runtime. Each dimension independently contributes:

```python
def compute_final_score(pre_features: dict, runtime_features: dict) -> float:
    raw = 0.0
    for dim in ALL_DIMENSIONS:
        weight = SCORING_WEIGHTS[dim]
        if dim in PRE_SCORE_DIMENSIONS:
            value = pre_features.get(dim, 0.5)
        else:
            value = runtime_features.get(dim, 0.5)
        raw += weight * value
    return apply_hard_rules(raw, runtime_features)
```

## 3. Postcondition Verification

### 3.1 Types

| Type | What it checks |
|---|---|
| url_contains | URL contains specified fragment |
| url_changes | URL differs from pre-action state |
| text_visible | Specified text appears on page |
| text_gone | Specified text disappears from page |
| element_visible | Specified element becomes visible |
| element_gone | Specified element disappears |
| network_request | Specific network request fired |
| dom_changed | DOM structure changed |
| value_changed | Input value changed |

### 3.2 Flow

1. Capture pre-state: URL, DOM hash, visible texts, input values
2. Execute action
3. Capture post-state
4. Verify each postcondition
5. Return PostconditionResult(passed, details)

### 3.3 Auto-inference + Manual Override

- AI DSL generator infers postconditions from action intent
- User can override or add in DSL
- Default fallback: check DOM change OR network request OR no console errors

### 3.4 Failure Handling

- Postcondition failed → record to LocatorAttemptLog → try next candidate
- All candidates exhausted → VLM fallback
- VLM also fails → InterventionNeededError with postcondition details

## 4. Data Model

### 4.1 LocatorAttemptLog

```python
class LocatorAttemptLog(Base):
    id: int PK
    run_id: int FK → test_case_run
    project_id: int FK → projects  # multi-tenant isolation
    step_index: int
    step_action: str
    target_description: str
    page_url: str
    page_url_pattern: str

    candidates_json: dict        # full candidate list + pre_scores
    selected_candidate: dict     # chosen candidate + final_score
    strategy_used: str
    fallback_tier_reached: int   # 0-3

    pre_features: dict           # generation-time feature details
    runtime_features: dict       # runtime feature details
    final_score: float

    action_success: bool
    postcondition_result: dict
    postcondition_passed: bool
    click_recovery_used: str | None

    overall_success: bool        # action_success AND postcondition_passed

    element_type: str
    selector_type: str
    domain: str
    route: str
    created_at: datetime
```

### 4.2 LocatorModelWeights (Phase 4+)

```python
class LocatorModelWeights(Base):
    id: int PK
    version: int
    weights_json: dict           # calibrated SCORING_WEIGHTS
    model_type: str              # "fixed", "calibrated", "logistic", "ltr"
    model_blob: bytes | None     # serialized model (Phase 4-5)
    accuracy: float | None
    trained_at: datetime
    active: bool
```

## 5. Execution Flow

### 5.1 Step Execution (new path)

```
step.candidates non-empty?
  No  → legacy resolve_with_fallback()
  Yes → dual-layer scoring path:

1. Sort candidates by pre_score (descending)
2. PostconditionVerifier.capture_pre_state()
3. For each candidate:
   a. RuntimeScorer.evaluate(page, candidate) → runtime_features
   b. compute_final_score(pre_features, runtime_features) → final_score
   c. apply_hard_rules(final_score)
   d. Decide strategy (DOM/VLM rerank/VLM grounding)
   e. Execute action
   f. ClickPreprocessor handles overlay (existing)
   g. PostconditionVerifier.verify()
   h. If passed → success
   i. If failed → LocatorAttemptLog.write(), try next candidate
4. All candidates failed → existing AI visual fallback (Tier 2)
5. All failed → InterventionNeededError
```

### 5.2 Decision Thresholds

```
final_score >= 0.85 → DOM action
0.65 <= score < 0.85 → DOM action + strong postcondition verification
0.45 <= score < 0.65 → DOM top-k + VLM rerank
score < 0.45 → VLM screenshot grounding
```

## 6. Generation Flow Changes

### 6.1 Page Explorer Enhancement

`collect_interactable_elements()` output gains:

```python
elements[i]["candidates"] = [
    LocatorCandidate(strategy="role", selector=..., pre_score=0.87, pre_features={...}),
    LocatorCandidate(strategy="css", selector=..., pre_score=0.62, pre_features={...}),
    # ... more candidates
]
elements[i]["element_type_score"] = {"dom": 0.90, "vlm": 0.25}
```

PreScorer runs on each element during exploration:
1. Generate all possible locator strategies for the element
2. Score each candidate using pre-score dimensions
3. Sort by pre_score descending
4. Attach to element data

### 6.2 DSL Generator Enhancement

- Prompt includes candidates data from page exploration
- Prompt instructs: use candidates field in steps
- Prompt instructs: infer postconditions for each action
- VLM candidate auto-appended as last fallback for every interactive step

## 7. Code Impact

### New Files

| File | Purpose |
|---|---|
| `backend/app/runners/pre_scorer.py` | Generation-time scoring |
| `backend/app/runners/runtime_scorer.py` | Runtime scoring |
| `backend/app/runners/postcondition_verifier.py` | Post-action verification |
| `backend/app/models/locator_attempt_log.py` | Data loop model |
| `backend/app/models/locator_model_weights.py` | Model weights (Phase 4+) |
| `backend/app/alembic/versions/xxx_add_locator_tables.py` | Migration |

### Modified Files

| File | Change |
|---|---|
| `backend/app/schemas/dsl.py` | +LocatorCandidate, +Postcondition, step fields |
| `backend/app/runners/playwright_runner.py` | New `_execute_step_v2()`, keep legacy path |
| `backend/app/ai/page_explorer.py` | Call PreScorer, output candidates |
| `backend/app/ai/dsl_generator.py` | Prompt changes for candidates + postconditions |
| `backend/app/locators/semantic.py` | Output pre_features with candidates |
| `backend/app/locators/fallback.py` | Accept candidates parameter |
| `backend/app/models/__init__.py` | Export new models |
| `backend/app/api/router.py` | New endpoints for attempt log queries (Phase 2+) |

### Unchanged Files

| File | Reason |
|---|---|
| `backend/app/locators/ai_visual.py` | VLM capability unchanged |
| `backend/app/locators/corrections.py` | Tier 0 still works |
| `backend/app/runners/click_preprocessor.py` | Overlay handling unchanged |
| `frontend/` | No UI changes needed for backend scoring |

## 8. 5-Phase Evolution

### Phase 1 (this implementation)

- PreScorer + RuntimeScorer with fixed weights (from Excel tables)
- PostconditionVerifier
- LocatorAttemptLog complete recording
- Hard rules cap
- DSL schema changes
- Runner dual-path execution

### Phase 2 (after data accumulation)

- Aggregate success rate by domain/route/selector_type/element_type
- Dynamically calibrate history_success dimension
- Identify high-fragility patterns
- Surface insights to AI planning

### Phase 3 (more data)

- VLM rerank feedback loop: did VLM pick correctly?
- ROI crop strategy optimization
- Multi-model fallback order optimization

### Phase 4 (sufficient data)

- Upgrade from fixed weights to logistic regression: P(success | features)
- Train on LocatorAttemptLog
- Output calibrated P_success per candidate
- Store in LocatorModelWeights table

### Phase 5 (long-term)

- Learning-to-Rank model for candidate ordering
- Joint DOM ranker + VLM reranker + execution verifier
- Fully automated, self-optimizing location system

## 9. Testing Strategy

- Unit tests for PreScorer: verify scores match expected priors for each selector type
- Unit tests for RuntimeScorer: mock page state, verify score computation
- Unit tests for PostconditionVerifier: mock page transitions, verify detection
- Unit tests for fragility detection: cover all flag patterns
- Integration tests: full step execution with candidates → postcondition → log
- Existing tests remain passing (legacy path unchanged)
