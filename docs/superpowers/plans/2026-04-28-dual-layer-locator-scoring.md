# Dual-Layer Locator Scoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement dual-layer (generation-time + runtime) scoring for element location, with multi-candidate DSL steps, postcondition verification, and data-loop logging.

**Architecture:** Generation-time PreScorer scores candidates during page exploration; runtime RuntimeScorer supplements with actionability/visual/history features; PostconditionVerifier confirms actions succeeded; LocatorAttemptLog records every attempt for future model training.

**Tech Stack:** Python, Pydantic, SQLAlchemy 2.x, Playwright, FastAPI

---

## File Structure

### New Files
| File | Responsibility |
|---|---|
| `backend/app/runners/pre_scorer.py` | DOM feature prior table, fragility detection, pre-score computation |
| `backend/app/runners/runtime_scorer.py` | Runtime actionability/visual/history scoring |
| `backend/app/runners/postcondition_verifier.py` | Pre-state capture, post-action verification |
| `backend/app/models/locator_attempt_log.py` | SQLAlchemy model for attempt logging |
| `backend/tests/unit/test_pre_scorer.py` | PreScorer unit tests |
| `backend/tests/unit/test_runtime_scorer.py` | RuntimeScorer unit tests |
| `backend/tests/unit/test_postcondition_verifier.py` | PostconditionVerifier unit tests |

### Modified Files
| File | Change |
|---|---|
| `backend/app/schemas/dsl.py` | Add LocatorCandidate, Postcondition types; add fields to step classes |
| `backend/app/runners/playwright_runner.py` | Add `_execute_step_v2()` with dual-layer scoring |
| `backend/app/ai/page_explorer.py` | Call PreScorer during exploration, output candidates |
| `backend/app/models/__init__.py` | Export LocatorAttemptLog |

---

### Task 1: DSL Schema — LocatorCandidate and Postcondition Types

**Files:**
- Modify: `backend/app/schemas/dsl.py:15-80`
- Test: `backend/tests/unit/test_dsl_validation.py`

- [ ] **Step 1: Add LocatorCandidate and Postcondition models in `schemas/dsl.py`**

Insert after line 16 (`LocatorConfidence = ...`), before line 19 (`class GotoStep`):

```python
class LocatorCandidate(BaseModel):
    """Pre-scored candidate locator strategy for a DSL step."""
    model_config = ConfigDict(extra="forbid", str_strip_whitespace=True)

    strategy: Literal[
        "css", "xpath", "data-testid", "element_id",
        "role", "label", "placeholder", "text",
        "tag", "semantic", "vlm",
    ]
    selector: str | None = Field(default=None, description="Explicit selector value (for css/xpath/data-testid/etc).")
    semantic_value: str | None = Field(default=None, description="Semantic value (role name, label text, etc).")
    pre_score: float = Field(ge=0.0, le=1.0, description="Generation-time pre-score 0.0-1.0.")
    pre_features: dict | None = Field(default=None, description="Pre-score feature breakdown for debugging.")


class Postcondition(BaseModel):
    """Post-action verification condition."""
    model_config = ConfigDict(extra="forbid", str_strip_whitespace=True)

    type: Literal[
        "url_contains", "url_changes", "text_visible",
        "text_gone", "element_visible", "element_gone",
        "network_request", "dom_changed", "value_changed",
    ]
    value: str | None = Field(default=None, description="Expected value (URL fragment, text, selector).")
    timeout_ms: int = Field(default=3000, ge=100, le=30000)
```

- [ ] **Step 2: Add `candidates` and `postconditions` fields to ClickStep**

In `schemas/dsl.py`, modify `ClickStep` (lines 24-30). Add two fields after `locator_confidence`:

```python
class ClickStep(DSLModel):
    action: Literal["click"]
    target: str = Field(min_length=1, description="Semantic or explicit locator.")
    target_strategy: TargetStrategy | None = Field(default=None, description="Locator strategy hint.")
    locator_confidence: LocatorConfidence | None = Field(
        default=None, description="AI-assessed locator confidence. low triggers VLM pre-verification.",
    )
    candidates: list[LocatorCandidate] = Field(default_factory=list, description="Pre-scored candidate locators.")
    postconditions: list[Postcondition] = Field(default_factory=list, description="Post-action verification conditions.")
```

Apply the same two-field addition to: `InputStep` (lines 33-40), `WaitForStep` (lines 43-50), `AssertTextStep` (lines 53-60), `CaptureTextStep` (lines 68-80). Each gets:

```python
    candidates: list[LocatorCandidate] = Field(default_factory=list, description="Pre-scored candidate locators.")
    postconditions: list[Postcondition] = Field(default_factory=list, description="Post-action verification conditions.")
```

- [ ] **Step 3: Run existing tests to verify backward compatibility**

Run: `cd backend && uv run pytest tests/unit/test_dsl_validation.py -v`
Expected: ALL PASS (new fields have defaults, existing DSL parses unchanged)

- [ ] **Step 4: Write test for new types**

Add to `tests/unit/test_dsl_validation.py`:

```python
def test_locator_candidate_valid():
    from app.schemas.dsl import LocatorCandidate
    cand = LocatorCandidate(strategy="role", selector="getByRole('button')", pre_score=0.87)
    assert cand.strategy == "role"
    assert cand.pre_score == 0.87
    assert cand.selector is not None


def test_locator_candidate_vlm_no_selector():
    from app.schemas.dsl import LocatorCandidate
    cand = LocatorCandidate(strategy="vlm", semantic_value="checkout button", pre_score=0.0)
    assert cand.selector is None
    assert cand.semantic_value == "checkout button"


def test_postcondition_url_contains():
    from app.schemas.dsl import Postcondition
    pc = Postcondition(type="url_contains", value="/success")
    assert pc.type == "url_contains"
    assert pc.timeout_ms == 3000


def test_click_step_with_candidates_and_postconditions():
    from app.schemas.dsl import ClickStep
    step = ClickStep(
        action="click",
        target="Submit",
        candidates=[
            {"strategy": "role", "selector": "getByRole('button', {name: 'Submit'})", "pre_score": 0.9},
            {"strategy": "vlm", "semantic_value": "Submit button", "pre_score": 0.0},
        ],
        postconditions=[
            {"type": "url_contains", "value": "/success"},
        ],
    )
    assert len(step.candidates) == 2
    assert len(step.postconditions) == 1


def test_click_step_backward_compatible():
    from app.schemas.dsl import ClickStep
    step = ClickStep(action="click", target="Submit")
    assert step.candidates == []
    assert step.postconditions == []
```

- [ ] **Step 5: Run new tests**

Run: `cd backend && uv run pytest tests/unit/test_dsl_validation.py -v`
Expected: ALL PASS (old + new)

- [ ] **Step 6: Commit**

```bash
cd backend
git add app/schemas/dsl.py tests/unit/test_dsl_validation.py
git commit -m "feat: add LocatorCandidate and Postcondition types to DSL schema"
```

---

### Task 2: LocatorAttemptLog Model + Migration

**Files:**
- Create: `backend/app/models/locator_attempt_log.py`
- Modify: `backend/app/models/__init__.py`
- Test: `backend/tests/unit/test_locator_attempt_log.py`

- [ ] **Step 1: Write test for LocatorAttemptLog model**

Create `tests/unit/test_locator_attempt_log.py`:

```python
"""Unit tests for LocatorAttemptLog model."""
from datetime import datetime

from app.models.locator_attempt_log import LocatorAttemptLog


def test_locator_attempt_log_creation():
    log = LocatorAttemptLog(
        run_id=1,
        project_id=1,
        step_index=0,
        step_action="click",
        target_description="Submit button",
        page_url="https://example.com/checkout",
        page_url_pattern="https://example.com/checkout",
        candidates_json={"candidates": []},
        selected_candidate={"strategy": "role", "score": 0.87},
        strategy_used="role",
        fallback_tier_reached=1,
        pre_features={"selector_stability": 0.9},
        runtime_features={"actionability": 0.95},
        final_score=0.88,
        action_success=True,
        postcondition_result={"passed": True},
        postcondition_passed=True,
        overall_success=True,
        element_type="button",
        selector_type="role",
        domain="example.com",
        route="/checkout",
    )
    assert log.run_id == 1
    assert log.overall_success is True
    assert log.final_score == 0.88
    assert log.click_recovery_used is None
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && uv run pytest tests/unit/test_locator_attempt_log.py -v`
Expected: FAIL with ImportError (module not found)

- [ ] **Step 3: Create LocatorAttemptLog model**

Create `backend/app/models/locator_attempt_log.py`:

```python
"""Structured logging for every locator attempt during test execution."""

from __future__ import annotations

from datetime import datetime

from sqlalchemy import DateTime, Float, ForeignKey, Index, Integer, String, Text, func
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base


class LocatorAttemptLog(Base):
    """Records every locator attempt with full scoring details for data-loop training."""

    __tablename__ = "locator_attempt_logs"
    __table_args__ = (
        Index("ix_lal_run_step", "run_id", "step_index"),
        Index("ix_lal_domain_strategy", "domain", "selector_type"),
        Index("ix_lal_project_success", "project_id", "overall_success"),
    )

    id: Mapped[int] = mapped_column(primary_key=True)
    run_id: Mapped[int] = mapped_column(Integer, ForeignKey("test_case_runs.id", ondelete="CASCADE"), nullable=False)
    project_id: Mapped[int] = mapped_column(Integer, ForeignKey("projects.id", ondelete="RESTRICT"), nullable=False)
    step_index: Mapped[int] = mapped_column(Integer, nullable=False)
    step_action: Mapped[str] = mapped_column(String(20), nullable=False)
    target_description: Mapped[str] = mapped_column(String(200), nullable=False)
    page_url: Mapped[str] = mapped_column(String(2000), nullable=False)
    page_url_pattern: Mapped[str] = mapped_column(String(500), nullable=False)

    candidates_json: Mapped[dict] = mapped_column(Text, nullable=False)
    selected_candidate: Mapped[dict] = mapped_column(Text, nullable=False)
    strategy_used: Mapped[str] = mapped_column(String(50), nullable=False)
    fallback_tier_reached: Mapped[int] = mapped_column(Integer, nullable=False)

    pre_features: Mapped[dict] = mapped_column(Text, nullable=True)
    runtime_features: Mapped[dict] = mapped_column(Text, nullable=True)
    final_score: Mapped[float] = mapped_column(Float, nullable=False)

    action_success: Mapped[bool] = mapped_column(nullable=False)
    postcondition_result: Mapped[dict] = mapped_column(Text, nullable=True)
    postcondition_passed: Mapped[bool] = mapped_column(nullable=False, default=False)
    click_recovery_used: Mapped[str | None] = mapped_column(String(50), nullable=True)

    overall_success: Mapped[bool] = mapped_column(nullable=False, default=False)

    element_type: Mapped[str | None] = mapped_column(String(50), nullable=True)
    selector_type: Mapped[str | None] = mapped_column(String(50), nullable=True)
    domain: Mapped[str | None] = mapped_column(String(200), nullable=True)
    route: Mapped[str | None] = mapped_column(String(500), nullable=True)

    created_at: Mapped[datetime] = mapped_column(DateTime, server_default=func.now(), nullable=False)
```

- [ ] **Step 4: Export model in `__init__.py`**

Add to `backend/app/models/__init__.py`:

```python
from app.models.locator_attempt_log import LocatorAttemptLog
```

Add `"LocatorAttemptLog"` to the `__all__` list.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && uv run pytest tests/unit/test_locator_attempt_log.py -v`
Expected: PASS

- [ ] **Step 6: Create Alembic migration**

Run: `cd backend && uv run alembic revision --autogenerate -m "add locator_attempt_logs table"`

Verify the generated migration includes the `locator_attempt_logs` table creation.

Run: `cd backend && uv run alembic upgrade head`

- [ ] **Step 7: Commit**

```bash
cd backend
git add app/models/locator_attempt_log.py app/models/__init__.py tests/unit/test_locator_attempt_log.py alembic/
git commit -m "feat: add LocatorAttemptLog model for data-loop logging"
```

---

### Task 3: PreScorer — Generation-Time Scoring

**Files:**
- Create: `backend/app/runners/pre_scorer.py`
- Test: `backend/tests/unit/test_pre_scorer.py`

- [ ] **Step 1: Write tests for PreScorer**

Create `tests/unit/test_pre_scorer.py`:

```python
"""Unit tests for PreScorer."""
from app.runners.pre_scorer import (
    DOM_FEATURE_PRIORS,
    ELEMENT_TYPE_SCORES,
    compute_pre_score,
    detect_fragility,
    score_candidates_for_element,
    PreScoreFeatures,
)


def test_dom_feature_priors_data_testid():
    assert DOM_FEATURE_PRIORS["data-testid"] == 0.95


def test_dom_feature_priors_role():
    assert DOM_FEATURE_PRIORS["role"] == 0.90


def test_dom_feature_priors_absolute_xpath():
    assert DOM_FEATURE_PRIORS["absolute_xpath"] == 0.10


def test_element_type_scores_button():
    assert ELEMENT_TYPE_SCORES["button"]["dom"] == 0.90
    assert ELEMENT_TYPE_SCORES["button"]["vlm"] == 0.25


def test_element_type_scores_canvas():
    assert ELEMENT_TYPE_SCORES["canvas"]["dom"] == 0.10
    assert ELEMENT_TYPE_SCORES["canvas"]["vlm"] == 0.90


def test_detect_fragility_dynamic_id():
    flags = detect_fragility("#user-abc12345-def", {"id": "user-abc12345-def"})
    assert "dynamic_id" in flags


def test_detect_fragility_css_module_hash():
    flags = detect_fragility(".btn_abc12", {"class": "btn_abc12"})
    assert "css_module_hash" in flags


def test_detect_fragility_deep_xpath():
    flags = detect_fragility("/html/body/div/div/div/div/div/button", {"id": None})
    assert "deep_xpath" in flags


def test_detect_fragility_nth_child():
    flags = detect_fragility("div:nth-child(2)", {"id": None})
    assert "nth_child" in flags


def test_detect_fragility_clean():
    flags = detect_fragility(".checkout-submit", {"id": "checkout-submit", "class": "checkout-submit"})
    assert flags == []


def test_compute_pre_score_high():
    features = PreScoreFeatures(
        selector_stability=0.90,
        semantic_match=0.95,
        uniqueness=1.0,
        context_match=0.80,
        fragility_flags=[],
    )
    score = compute_pre_score(features)
    assert 0.80 <= score <= 1.0


def test_compute_pre_score_low():
    features = PreScoreFeatures(
        selector_stability=0.10,
        semantic_match=0.20,
        uniqueness=0.3,
        context_match=0.2,
        fragility_flags=["dynamic_id", "deep_xpath", "nth_child"],
    )
    score = compute_pre_score(features)
    assert score < 0.4


def test_score_candidates_for_element():
    element = {
        "tag": "button",
        "text": "Submit",
        "role": "button",
        "aria_label": None,
        "data_testid": None,
        "css_selector": "button.submit-btn",
        "xpath": "/html/body/main/button",
    }
    candidates = score_candidates_for_element(element, intent="Submit")
    assert len(candidates) >= 2
    # role candidate should score higher than xpath
    role_cand = next((c for c in candidates if c["strategy"] == "role"), None)
    xpath_cand = next((c for c in candidates if c["strategy"] == "xpath"), None)
    if role_cand and xpath_cand:
        assert role_cand["pre_score"] > xpath_cand["pre_score"]
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && uv run pytest tests/unit/test_pre_scorer.py -v`
Expected: FAIL with ImportError

- [ ] **Step 3: Implement PreScorer**

Create `backend/app/runners/pre_scorer.py`:

```python
"""Generation-time element scoring based on DOM feature priors."""

from __future__ import annotations

import re
from dataclasses import dataclass, field

# --- Weight constants (from Excel "评分模型" sheet) ---

PRE_SCORE_WEIGHTS = {
    "selector_stability": 0.40,
    "semantic_match": 0.30,
    "uniqueness": 0.20,
    "context_match": 0.10,
}

# DOM feature prior scores (from Excel "DOM特征权重" sheet)
DOM_FEATURE_PRIORS: dict[str, float] = {
    "data-testid": 0.95,
    "role": 0.90,
    "label": 0.85,
    "stable_id": 0.80,
    "name_type": 0.75,
    "aria_label": 0.75,
    "placeholder": 0.72,
    "text": 0.70,
    "href": 0.68,
    "short_css": 0.60,
    "stable_class": 0.55,
    "relative_xpath": 0.50,
    "bbox": 0.50,
    "tag": 0.45,
    "nth_child": 0.25,
    "absolute_xpath": 0.10,
    "canvas_svg": 0.15,
}

# Element type DOM/VLM routing (from Excel "元素类型权重" sheet)
ELEMENT_TYPE_SCORES: dict[str, dict[str, float]] = {
    "button": {"dom": 0.90, "vlm": 0.25},
    "input": {"dom": 0.88, "vlm": 0.20},
    "textarea": {"dom": 0.88, "vlm": 0.20},
    "select": {"dom": 0.85, "vlm": 0.25},
    "a": {"dom": 0.83, "vlm": 0.25},
    "checkbox": {"dom": 0.86, "vlm": 0.25},
    "radio": {"dom": 0.86, "vlm": 0.25},
    "label": {"dom": 0.65, "vlm": 0.20},
    "div": {"dom": 0.60, "vlm": 0.40},
    "span": {"dom": 0.60, "vlm": 0.40},
    "svg": {"dom": 0.45, "vlm": 0.60},
    "canvas": {"dom": 0.10, "vlm": 0.90},
    "iframe": {"dom": 0.55, "vlm": 0.45},
}

_DYNAMIC_ID = re.compile(r"[0-9a-f]{8,}|auto\d+|tmp|rnd", re.IGNORECASE)
_CSS_MODULE_HASH = re.compile(r"_[a-zA-Z0-9]{5,}")


@dataclass
class PreScoreFeatures:
    selector_stability: float
    semantic_match: float
    uniqueness: float
    context_match: float
    fragility_flags: list[str] = field(default_factory=list)


def detect_fragility(selector: str, element_attrs: dict) -> list[str]:
    flags = []
    elem_id = element_attrs.get("id") or ""
    elem_class = element_attrs.get("class") or ""

    if elem_id and _DYNAMIC_ID.search(elem_id):
        flags.append("dynamic_id")
    if elem_class and _CSS_MODULE_HASH.search(elem_class):
        flags.append("css_module_hash")
    if selector.startswith("/") and selector.count("/") > 5:
        flags.append("deep_xpath")
    if "nth-child" in selector or "nth-of-type" in selector:
        flags.append("nth_child")
    return flags


def compute_pre_score(features: PreScoreFeatures) -> float:
    context = max(0.0, features.context_match - len(features.fragility_flags) * 0.15)
    raw = (
        PRE_SCORE_WEIGHTS["selector_stability"] * features.selector_stability
        + PRE_SCORE_WEIGHTS["semantic_match"] * features.semantic_match
        + PRE_SCORE_WEIGHTS["uniqueness"] * features.uniqueness
        + PRE_SCORE_WEIGHTS["context_match"] * context
    )
    return min(1.0, max(0.0, raw))


def _compute_semantic_match(element: dict, intent: str) -> float:
    if not intent:
        return 0.5
    intent_lower = intent.lower().strip()
    score = 0.0

    text = (element.get("text") or "").lower().strip()
    aria = (element.get("aria_label") or "").lower().strip()
    placeholder = (element.get("placeholder") or "").lower().strip()
    role = (element.get("role") or "").lower().strip()

    if text and text == intent_lower:
        score = max(score, 0.95)
    elif text and intent_lower in text:
        score = max(score, 0.80)
    elif text and text in intent_lower:
        score = max(score, 0.70)

    if aria and aria == intent_lower:
        score = max(score, 0.90)
    elif aria and intent_lower in aria:
        score = max(score, 0.75)

    if placeholder and intent_lower in placeholder:
        score = max(score, 0.70)

    if role and intent_lower in role:
        score = max(score, 0.60)

    return score if score > 0 else 0.30


def score_candidates_for_element(
    element: dict,
    intent: str = "",
) -> list[dict]:
    """Generate and score all possible locator candidates for an element.

    Returns list of dicts compatible with LocatorCandidate schema.
    """
    candidates = []
    tag = element.get("tag", "")
    text = element.get("text") or ""
    role = element.get("role")
    aria_label = element.get("aria_label")
    placeholder = element.get("placeholder")
    data_testid = element.get("data_testid")
    css = element.get("css_selector") or ""
    xpath = element.get("xpath") or ""
    elem_id = element.get("id")

    sem = _compute_semantic_match(element, intent)

    if data_testid:
        candidates.append(_make_candidate("data-testid", f"[data-testid='{data_testid}']", DOM_FEATURE_PRIORS["data-testid"], sem, element))

    if role and text:
        candidates.append(_make_candidate("role", f"getByRole('{role}', {{name: '{text}'}})", DOM_FEATURE_PRIORS["role"], sem, element))

    if aria_label:
        candidates.append(_make_candidate("label", f"[aria-label='{aria_label}']", DOM_FEATURE_PRIORS["aria_label"], sem, element))

    if placeholder:
        candidates.append(_make_candidate("placeholder", f"[placeholder='{placeholder}']", DOM_FEATURE_PRIORS["placeholder"], sem, element))

    if text:
        candidates.append(_make_candidate("text", text, DOM_FEATURE_PRIORS["text"], sem, element))

    if elem_id:
        id_type = "stable_id" if not _DYNAMIC_ID.search(elem_id) else "nth_child"
        candidates.append(_make_candidate("element_id", f"#{elem_id}", DOM_FEATURE_PRIORS.get(id_type, 0.25), sem, element))

    if css:
        css_type = "short_css" if len(css) < 60 else "nth_child"
        candidates.append(_make_candidate("css", css, DOM_FEATURE_PRIORS.get(css_type, 0.25), sem, element))

    if xpath:
        xp_type = "relative_xpath" if xpath.count("/") <= 5 else "absolute_xpath"
        candidates.append(_make_candidate("xpath", xpath, DOM_FEATURE_PRIORS.get(xp_type, 0.10), sem, element))

    candidates.append(_make_candidate("tag", tag, DOM_FEATURE_PRIORS["tag"], sem, element))

    candidates.sort(key=lambda c: c["pre_score"], reverse=True)
    return candidates


def _make_candidate(strategy: str, selector: str, stability: float, semantic: float, element: dict) -> dict:
    attrs = {"id": element.get("id"), "class": element.get("class", "")}
    fragility = detect_fragility(selector, attrs)
    features = PreScoreFeatures(
        selector_stability=stability,
        semantic_match=semantic,
        uniqueness=1.0,
        context_match=0.70,
        fragility_flags=fragility,
    )
    return {
        "strategy": strategy,
        "selector": selector,
        "semantic_value": element.get("text") or element.get("aria_label"),
        "pre_score": round(compute_pre_score(features), 4),
        "pre_features": {
            "selector_stability": features.selector_stability,
            "semantic_match": features.semantic_match,
            "uniqueness": features.uniqueness,
            "context_match": features.context_match,
            "fragility_flags": fragility,
        },
    }
```

- [ ] **Step 4: Run tests**

Run: `cd backend && uv run pytest tests/unit/test_pre_scorer.py -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
cd backend
git add app/runners/pre_scorer.py tests/unit/test_pre_scorer.py
git commit -m "feat: add PreScorer for generation-time element scoring"
```

---

### Task 4: RuntimeScorer — Runtime Scoring

**Files:**
- Create: `backend/app/runners/runtime_scorer.py`
- Test: `backend/tests/unit/test_runtime_scorer.py`

- [ ] **Step 1: Write tests for RuntimeScorer**

Create `tests/unit/test_runtime_scorer.py`:

```python
"""Unit tests for RuntimeScorer."""
from app.runners.runtime_scorer import (
    RuntimeScoreFeatures,
    compute_runtime_score,
    apply_hard_rules,
    compute_final_score,
    HARD_CAP_SCORES,
)


def test_runtime_score_all_good():
    features = RuntimeScoreFeatures(
        actionability=0.95,
        visual_consistency=0.90,
        history_success=0.80,
        rank_margin=0.30,
        overlay_risk=0.0,
    )
    score = compute_runtime_score(features)
    assert 0.75 <= score <= 1.0


def test_runtime_score_all_bad():
    features = RuntimeScoreFeatures(
        actionability=0.10,
        visual_consistency=0.10,
        history_success=0.10,
        rank_margin=0.02,
        overlay_risk=0.80,
    )
    score = compute_runtime_score(features)
    assert score < 0.4


def test_hard_rules_not_visible():
    score = apply_hard_rules(0.90, {"visible": False, "enabled": True, "bbox_area": 100, "receives_events": True})
    assert score <= HARD_CAP_SCORES["not_visible"]


def test_hard_rules_disabled():
    score = apply_hard_rules(0.90, {"visible": True, "enabled": False, "bbox_area": 100, "receives_events": True})
    assert score <= HARD_CAP_SCORES["disabled"]


def test_hard_rules_bbox_zero():
    score = apply_hard_rules(0.90, {"visible": True, "enabled": True, "bbox_area": 0, "receives_events": True})
    assert score <= HARD_CAP_SCORES["bbox_zero"]


def test_hard_rules_no_event():
    score = apply_hard_rules(0.90, {"visible": True, "enabled": True, "bbox_area": 100, "receives_events": False})
    assert score <= HARD_CAP_SCORES["not_receives_events"]


def test_compute_final_score_fusion():
    pre_features = {"selector_stability": 0.90, "semantic_match": 0.80, "uniqueness": 1.0, "context_match": 0.70}
    runtime_features = {"actionability": 0.95, "visual_consistency": 0.85, "history_success": 0.70, "rank_margin": 0.25}
    score = compute_final_score(pre_features, runtime_features)
    assert 0.7 <= score <= 1.0


def test_compute_final_score_hard_rule_caps():
    pre_features = {"selector_stability": 0.95, "semantic_match": 0.90, "uniqueness": 1.0, "context_match": 0.80}
    runtime_features = {"actionability": 0.95, "visual_consistency": 0.85, "history_success": 0.80, "rank_margin": 0.30}
    # Override with not visible
    runtime_features["_hard_overrides"] = {"visible": False}
    score = compute_final_score(pre_features, runtime_features)
    assert score <= 0.40
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && uv run pytest tests/unit/test_runtime_scorer.py -v`
Expected: FAIL with ImportError

- [ ] **Step 3: Implement RuntimeScorer**

Create `backend/app/runners/runtime_scorer.py`:

```python
"""Runtime scoring for element location candidates."""

from __future__ import annotations

from dataclasses import dataclass

# 8-dimension global weights (from Excel "评分模型" sheet)
SCORING_WEIGHTS = {
    "selector_stability": 0.20,
    "semantic_match": 0.20,
    "uniqueness": 0.15,
    "actionability": 0.15,
    "context_match": 0.10,
    "visual_consistency": 0.10,
    "history_success": 0.05,
    "rank_margin": 0.05,
}

PRE_SCORE_DIMENSIONS = {"selector_stability", "semantic_match", "uniqueness", "context_match"}
RUNTIME_SCORE_DIMENSIONS = {"actionability", "visual_consistency", "history_success", "rank_margin"}

# Hard rule cap scores
HARD_CAP_SCORES = {
    "not_visible": 0.40,
    "disabled": 0.30,
    "bbox_zero": 0.20,
    "not_receives_events": 0.45,
}

RUNTIME_WEIGHTS = {
    "actionability": 0.30,
    "visual_consistency": 0.25,
    "history_success": 0.20,
    "rank_margin": 0.15,
    "overlay_risk": 0.10,
}

# Decision thresholds
THRESHOLD_HIGH = 0.85
THRESHOLD_MEDIUM = 0.65
THRESHOLD_LOW = 0.45
RANK_MARGIN_VLM_TRIGGER = 0.08


@dataclass
class RuntimeScoreFeatures:
    actionability: float
    visual_consistency: float
    history_success: float
    rank_margin: float
    overlay_risk: float = 0.0


def compute_runtime_score(features: RuntimeScoreFeatures) -> float:
    raw = (
        RUNTIME_WEIGHTS["actionability"] * features.actionability
        + RUNTIME_WEIGHTS["visual_consistency"] * features.visual_consistency
        + RUNTIME_WEIGHTS["history_success"] * features.history_success
        + RUNTIME_WEIGHTS["rank_margin"] * min(1.0, features.rank_margin / 0.20)
        + RUNTIME_WEIGHTS["overlay_risk"] * (1.0 - features.overlay_risk)
    )
    return min(1.0, max(0.0, raw))


def apply_hard_rules(score: float, state: dict) -> float:
    if not state.get("visible", True):
        score = min(score, HARD_CAP_SCORES["not_visible"])
    if not state.get("enabled", True):
        score = min(score, HARD_CAP_SCORES["disabled"])
    if state.get("bbox_area", 1) == 0:
        score = min(score, HARD_CAP_SCORES["bbox_zero"])
    if not state.get("receives_events", True):
        score = min(score, HARD_CAP_SCORES["not_receives_events"])
    return score


def decide_strategy(final_score: float, runtime_state: dict) -> str:
    if not runtime_state.get("visible", True) or not runtime_state.get("enabled", True):
        return "vlm_or_repair"
    if runtime_state.get("match_count", 1) == 0:
        return "vlm_grounding"
    margin = runtime_state.get("rank_margin", 1.0)
    if margin < RANK_MARGIN_VLM_TRIGGER:
        return "vlm_rerank"
    if final_score >= THRESHOLD_HIGH:
        return "dom_action"
    if final_score >= THRESHOLD_MEDIUM:
        return "dom_action_strong_verify"
    if final_score >= THRESHOLD_LOW:
        return "vlm_rerank"
    return "vlm_grounding"


def compute_final_score(pre_features: dict, runtime_features: dict) -> float:
    hard_overrides = runtime_features.pop("_hard_overrides", None) or {}
    raw = 0.0
    for dim, weight in SCORING_WEIGHTS.items():
        if dim in PRE_SCORE_DIMENSIONS:
            value = pre_features.get(dim, 0.5)
        else:
            value = runtime_features.get(dim, 0.5)
        raw += weight * value
    raw = apply_hard_rules(raw, hard_overrides)
    return round(min(1.0, max(0.0, raw)), 4)
```

- [ ] **Step 4: Run tests**

Run: `cd backend && uv run pytest tests/unit/test_runtime_scorer.py -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
cd backend
git add app/runners/runtime_scorer.py tests/unit/test_runtime_scorer.py
git commit -m "feat: add RuntimeScorer with hard rules and strategy decision"
```

---

### Task 5: PostconditionVerifier

**Files:**
- Create: `backend/app/runners/postcondition_verifier.py`
- Test: `backend/tests/unit/test_postcondition_verifier.py`

- [ ] **Step 1: Write tests for PostconditionVerifier**

Create `tests/unit/test_postcondition_verifier.py`:

```python
"""Unit tests for PostconditionVerifier."""
from unittest.mock import MagicMock, patch

from app.runners.postcondition_verifier import (
    PostconditionResult,
    PostconditionVerifier,
    verify_default_postcondition,
)
from app.schemas.dsl import Postcondition


def test_postcondition_result_passed():
    r = PostconditionResult(passed=True, details={})
    assert r.passed is True


def test_postcondition_result_failed():
    r = PostconditionResult(passed=False, details={"url_contains": "expected '/success' but url was '/checkout'"})
    assert r.passed is False


def test_verify_default_postcondition_url_changed():
    pre = {"url": "https://example.com/checkout"}
    post = {"url": "https://example.com/success"}
    assert verify_default_postcondition(pre, post) is True


def test_verify_default_postcondition_dom_changed():
    pre = {"url": "https://example.com/page", "dom_hash": "abc123"}
    post = {"url": "https://example.com/page", "dom_hash": "def456"}
    assert verify_default_postcondition(pre, post) is True


def test_verify_default_postcondition_no_change():
    pre = {"url": "https://example.com/page", "dom_hash": "abc123"}
    post = {"url": "https://example.com/page", "dom_hash": "abc123"}
    assert verify_default_postcondition(pre, post) is False
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && uv run pytest tests/unit/test_postcondition_verifier.py -v`
Expected: FAIL with ImportError

- [ ] **Step 3: Implement PostconditionVerifier**

Create `backend/app/runners/postcondition_verifier.py`:

```python
"""Post-action verification of state changes."""

from __future__ import annotations

import hashlib
from dataclasses import dataclass, field

from playwright.sync_api import Page

from app.schemas.dsl import Postcondition


@dataclass
class PostconditionResult:
    passed: bool
    details: dict = field(default_factory=dict)


class PostconditionVerifier:
    def __init__(self, page: Page):
        self.page = page
        self._pre_state: dict | None = None

    def capture_pre_state(self) -> dict:
        self._pre_state = {
            "url": self.page.url,
            "dom_hash": self._compute_dom_hash(),
            "visible_texts": self._get_visible_texts(),
            "input_values": self._get_input_values(),
        }
        return self._pre_state

    def verify(self, postconditions: list[Postcondition]) -> PostconditionResult:
        if not self._pre_state:
            self.capture_pre_state()

        post_url = self.page.url
        details = {}

        for pc in postconditions:
            try:
                ok = self._verify_single(pc, post_url)
                if not ok:
                    details[pc.type] = f"failed: {pc.value}"
            except Exception as exc:
                details[pc.type] = f"error: {exc}"

        if not postconditions:
            pre = self._pre_state or {}
            post_state = {"url": post_url, "dom_hash": self._compute_dom_hash()}
            if not verify_default_postcondition(pre, post_state):
                details["default"] = "no detectable state change"

        return PostconditionResult(passed=len(details) == 0, details=details)

    def _verify_single(self, pc: Postcondition, post_url: str) -> bool:
        match pc.type:
            case "url_contains":
                return pc.value is not None and pc.value in post_url
            case "url_changes":
                return post_url != (self._pre_state or {}).get("url", "")
            case "text_visible":
                if pc.value:
                    return self.page.locator(f"text={pc.value}").is_visible(timeout=pc.timeout_ms)
                return False
            case "text_gone":
                if pc.value:
                    return not self.page.locator(f"text={pc.value}").is_visible(timeout=pc.timeout_ms)
                return False
            case "element_visible":
                if pc.value:
                    return self.page.locator(pc.value).is_visible(timeout=pc.timeout_ms)
                return False
            case "element_gone":
                if pc.value:
                    return not self.page.locator(pc.value).is_visible(timeout=pc.timeout_ms)
                return False
            case "dom_changed":
                return self._compute_dom_hash() != (self._pre_state or {}).get("dom_hash", "")
            case "value_changed":
                return self._get_input_values() != (self._pre_state or {}).get("input_values", {})
            case _:
                return True

    def _compute_dom_hash(self) -> str:
        try:
            body_html = self.page.evaluate("() => document.body?.innerHTML?.length || 0")
            return hashlib.md5(str(body_html).encode()).hexdigest()[:12]
        except Exception:
            return ""

    def _get_visible_texts(self) -> list[str]:
        try:
            return self.page.evaluate("""
                () => Array.from(document.querySelectorAll('h1,h2,h3,p,span,div,button,a,label'))
                    .filter(el => el.offsetParent !== null)
                    .map(el => (el.innerText || '').trim())
                    .filter(t => t.length > 0)
                    .slice(0, 50)
            """)
        except Exception:
            return []

    def _get_input_values(self) -> dict:
        try:
            return self.page.evaluate("""
                () => Object.fromEntries(
                    Array.from(document.querySelectorAll('input,textarea,select'))
                        .map(el => [el.name || el.id || el.getAttribute('data-testid') || '', el.value || ''])
                        .filter(([k]) => k)
                )
            """)
        except Exception:
            return {}


def verify_default_postcondition(pre: dict, post: dict) -> bool:
    if pre.get("url") != post.get("url"):
        return True
    if pre.get("dom_hash") != post.get("dom_hash"):
        return True
    return False
```

- [ ] **Step 4: Run tests**

Run: `cd backend && uv run pytest tests/unit/test_postcondition_verifier.py -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
cd backend
git add app/runners/postcondition_verifier.py tests/unit/test_postcondition_verifier.py
git commit -m "feat: add PostconditionVerifier for post-action state verification"
```

---

### Task 6: Runner Integration — `_execute_step_v2`

**Files:**
- Modify: `backend/app/runners/playwright_runner.py`

- [ ] **Step 1: Add imports at the top of `playwright_runner.py`**

Add after existing imports:

```python
from app.runners.pre_scorer import score_candidates_for_element
from app.runners.runtime_scorer import (
    RuntimeScoreFeatures,
    compute_final_score,
    decide_strategy,
)
from app.runners.postcondition_verifier import PostconditionVerifier
from app.models.locator_attempt_log import LocatorAttemptLog
```

- [ ] **Step 2: Add `_execute_step_v2` function**

Insert before `execute_case_with_playwright()` (before line 150). This is the new dual-layer scoring execution path:

```python
def _has_candidates(step) -> bool:
    return hasattr(step, "candidates") and bool(step.candidates)


def _evaluate_runtime_features(page, candidate_entry: dict) -> tuple[dict, dict]:
    try:
        selector = candidate_entry.get("selector", "")
        if not selector or candidate_entry.get("strategy") == "vlm":
            return {"actionability": 0.5, "visual_consistency": 0.5, "history_success": 0.5, "rank_margin": 0.5}, {}

        locator = _build_locator_from_candidate(page, candidate_entry)
        if locator is None:
            return {"actionability": 0.3, "visual_consistency": 0.3, "history_success": 0.5, "rank_margin": 0.5}, {}

        count = locator.count()
        if count == 0:
            return {"actionability": 0.0, "visual_consistency": 0.0, "history_success": 0.5, "rank_margin": 0.0}, {"match_count": 0}

        state = _evaluate_element_state(locator.first)
        rt_features = {
            "actionability": _compute_actionability(state),
            "visual_consistency": _compute_visual_consistency(state),
            "history_success": 0.5,
            "rank_margin": 0.5,
        }
        return rt_features, state
    except Exception:
        return {"actionability": 0.5, "visual_consistency": 0.5, "history_success": 0.5, "rank_margin": 0.5}, {}


def _build_locator_from_candidate(page, candidate_entry: dict):
    strategy = candidate_entry.get("strategy", "")
    selector = candidate_entry.get("selector", "")
    try:
        if strategy == "css":
            return page.locator(selector)
        elif strategy == "xpath":
            return page.locator(f"xpath={selector}")
        elif strategy == "data-testid":
            return page.get_by_test_id(selector.replace("[data-testid='", "").replace("']", ""))
        elif strategy == "role":
            return page.get_by_role("button", name=candidate_entry.get("semantic_value", ""))
        elif strategy == "text":
            return page.get_by_text(selector, exact=False)
        elif strategy == "label":
            return page.locator(selector)
        elif strategy == "element_id":
            return page.locator(selector)
        else:
            return page.locator(selector) if selector else None
    except Exception:
        return None


def _evaluate_element_state(element) -> dict:
    try:
        return element.evaluate("""(el) => {
            const rect = el.getBoundingClientRect();
            const style = window.getComputedStyle(el);
            return {
                visible: rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden' && style.display !== 'none',
                enabled: !el.disabled && el.getAttribute('aria-disabled') !== 'true',
                bbox_area: Math.round(rect.width * rect.height),
                receives_events: rect.width > 0 && rect.height > 0,
                in_viewport: rect.top >= 0 && rect.left >= 0 && rect.bottom <= window.innerHeight && rect.right <= window.innerWidth,
            };
        }""")
    except Exception:
        return {"visible": False, "enabled": False, "bbox_area": 0, "receives_events": False, "in_viewport": False}


def _compute_actionability(state: dict) -> float:
    score = 0.0
    if state.get("visible"):
        score += 0.4
    if state.get("enabled"):
        score += 0.3
    if state.get("receives_events"):
        score += 0.3
    return score


def _compute_visual_consistency(state: dict) -> float:
    area = state.get("bbox_area", 0)
    if area == 0:
        return 0.0
    score = min(1.0, area / 10000) * 0.7
    if state.get("in_viewport"):
        score += 0.3
    return min(1.0, score)
```

- [ ] **Step 3: Add candidate-check branch in the step dispatch loop**

In `execute_case_with_playwright()`, inside the `for step in case.steps:` loop (around line 199), add a check BEFORE the existing `match step.action` block:

```python
        # --- Dual-layer scoring path (new) ---
        if _has_candidates(step):
            result = _execute_step_v2(
                page, step, execution_id=execution_id,
                correction_store=correction_store,
                resolved_by=resolved_by if 'resolved_by' in dir() else None,
            )
            # result is a StepExecutionEvidence or raises exception
            if result.status == "failed":
                if result.intervention_request:
                    raise RunnerInterventionError(result.intervention_request)
                raise RunnerExecutionError(result.error_message or "step failed")
            evidence.append(result)
            continue  # skip legacy dispatch below

        # --- Legacy path (unchanged below) ---
        match step.action:
```

Do the same for `execute_case_with_playwright_streaming()` in its step loop (around line 468). Use the same check but yield streaming events instead of appending to evidence.

NOTE: `_execute_step_v2` will call `_execute_with_candidates` which iterates candidates, scores them with RuntimeScorer, tries execution, verifies postconditions, and returns StepExecutionEvidence. It falls back to existing `resolve_with_fallback` if all candidates fail.

- [ ] **Step 4: Run existing tests to verify no regression**

Run: `cd backend && uv run pytest tests/unit/ -v`
Expected: ALL PASS (new code paths not triggered — candidates default to empty list)

- [ ] **Step 5: Commit**

```bash
cd backend
git add app/runners/playwright_runner.py
git commit -m "feat: add dual-layer scoring execution path to runner"
```

---

### Task 7: Page Explorer Enhancement — Integrate PreScorer

**Files:**
- Modify: `backend/app/ai/page_explorer.py`

- [ ] **Step 1: Add PreScorer import**

At top of `page_explorer.py`:

```python
from app.runners.pre_scorer import score_candidates_for_element, ELEMENT_TYPE_SCORES
```

- [ ] **Step 2: Enhance element collection output**

In `collect_interactable_elements()` (around line 340 where elements are built), add after each element dict is constructed:

```python
element["candidates"] = score_candidates_for_element(element)
tag = element.get("tag", "")
element["element_type_score"] = ELEMENT_TYPE_SCORES.get(tag, {"dom": 0.60, "vlm": 0.40})
```

- [ ] **Step 3: Enhance `format_elements_for_prompt()`**

In `_format_element_rich()` or `format_elements_for_prompt()`, append candidate info to the formatted string. After the existing `stable=0.XX` output, add:

```python
if elem.get("candidates"):
    top_cand = elem["candidates"][0]
    lines.append(f"  top_candidate={top_cand['strategy']}({top_cand['pre_score']:.2f})")
```

- [ ] **Step 4: Run existing tests**

Run: `cd backend && uv run pytest tests/unit/ -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
cd backend
git add app/ai/page_explorer.py
git commit -m "feat: integrate PreScorer into page explorer for candidate scoring"
```

---

### Task 8: DSL Generator Prompt Enhancement

**Files:**
- Modify: `backend/app/ai/dsl_generator.py`

- [ ] **Step 1: Locate and modify the DSL generation prompt**

In `backend/app/ai/dsl_generator.py`, find `_build_user_prompt_lines()` (line 361). The `page_elements` injection is at lines 375-381. After that block, add candidate/postcondition instructions:

```python
if payload.page_elements:
    user_lines.extend(
        [
            "页面可交互元素清单（请严格使用其中的 label、placeholder 或 id 作为 target）：",
            payload.page_elements,
            "",
            "评分与候选策略规则：",
            "- 每个交互步骤应包含 candidates 字段，列出 3 个候选定位策略（按 pre_score 降序）",
            "- 最后一个候选必须是 VLM 策略（strategy='vlm', pre_score=0.0）作为兜底",
            "- 每个交互步骤应推断至少 1 个 postcondition：",
            "  - 导航点击 → {type: 'url_changes'} 或 {type: 'url_contains', value: '...'}",
            "  - 表单提交 → {type: 'url_contains', value: '...'} 或 {type: 'text_visible', value: '...'}",
            "  - 输入操作 → {type: 'value_changed'}",
            "  - 删除操作 → {type: 'text_gone', value: '...'}",
        ]
    )
```

- [ ] **Step 2: Add instructions for candidates and postconditions**

In the prompt section that instructs DSL generation, add guidance:

```
When page_elements contain candidates with pre_score, you MUST use the candidates field in each step:
- Set candidates to the top 3 scored candidates for the target element
- Always include a VLM candidate (strategy="vlm") as the last fallback
- Infer at least one postcondition for each interactive action (click/input/select):
  - For navigation clicks: postcondition type="url_changes" or "url_contains"
  - For form submissions: postcondition type="url_contains" or "text_visible"
  - For input actions: postcondition type="value_changed"
  - For delete/remove actions: postcondition type="text_gone" or "element_gone"
```

- [ ] **Step 3: Run existing tests**

Run: `cd backend && uv run pytest tests/unit/ -v`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
cd backend
git add app/ai/dsl_generator.py
git commit -m "feat: enhance DSL generator prompt for candidates and postconditions"
```

---

### Task 9: Integration Test

**Files:**
- Create: `backend/tests/integration/test_dual_layer_scoring.py`

- [ ] **Step 1: Write integration test**

Create `tests/integration/test_dual_layer_scoring.py`:

```python
"""Integration test for dual-layer locator scoring end-to-end."""
import pytest
from app.schemas.dsl import DSLCase, ClickStep, LocatorCandidate, Postcondition


@pytest.fixture
def dsl_with_candidates():
    return DSLCase(
        name="Test with candidates",
        steps=[
            {
                "action": "goto",
                "value": "https://example.com",
            },
            {
                "action": "click",
                "target": "More information",
                "candidates": [
                    {"strategy": "text", "selector": "More information", "pre_score": 0.65,
                     "pre_features": {"selector_stability": 0.70, "semantic_match": 0.80, "uniqueness": 0.50, "context_match": 0.60}},
                    {"strategy": "vlm", "semantic_value": "More information link", "pre_score": 0.0},
                ],
                "postconditions": [
                    {"type": "url_changes"},
                ],
            },
        ],
    )


def test_dsl_parse_with_candidates(dsl_with_candidates):
    assert len(dsl_with_candidates.steps) == 2
    click_step = dsl_with_candidates.steps[1]
    assert hasattr(click_step, "candidates")
    assert len(click_step.candidates) == 2
    assert click_step.candidates[0].strategy == "text"
    assert click_step.candidates[1].strategy == "vlm"
    assert len(click_step.postconditions) == 1
    assert click_step.postconditions[0].type == "url_changes"


def test_dsl_without_candidates_backward_compatible():
    case = DSLCase(
        name="Legacy test",
        steps=[
            {"action": "goto", "value": "https://example.com"},
            {"action": "click", "target": "Submit"},
        ],
    )
    click_step = case.steps[1]
    assert click_step.candidates == []
    assert click_step.postconditions == []
```

- [ ] **Step 2: Run integration test**

Run: `cd backend && uv run pytest tests/integration/test_dual_layer_scoring.py -v`
Expected: ALL PASS

- [ ] **Step 3: Run full test suite**

Run: `cd backend && uv run pytest tests/unit/ -v`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
cd backend
git add tests/integration/test_dual_layer_scoring.py
git commit -m "test: add integration tests for dual-layer locator scoring"
```

---

## Execution Notes

- Tasks 1-5 are independent and can be parallelized
- Task 6 depends on Tasks 3, 4, 5 (imports)
- Task 7 depends on Task 3 (imports PreScorer)
- Task 8 depends on Task 1 (uses new schema types)
- Task 9 depends on Tasks 1-8 (full integration)

Run all tests after all tasks: `cd backend && uv run pytest -v`
