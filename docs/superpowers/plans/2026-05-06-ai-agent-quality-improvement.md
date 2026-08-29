# AI Agent 测试用例质量提升 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 通过三层修复（选择器评分、流程守卫、自动回归循环）将 AI 生成的测试用例步骤通过率提升到 80%+

**Architecture:** L1 修复 `page_explorer.py` 的稳定性评分和 `pre_scorer.py` 的脆弱性检测，L2 在 `test_planning_agent.py` 的 ReAct loop 中加强 generate_plan 守卫和变量校验，L3 新建自动化回归脚本循环测试

**Tech Stack:** Python 3.13, Playwright, pytest, FastAPI, httpx

---

## Task 1: L1 — 选择器稳定性评分修复

**Files:**
- Modify: `backend/app/ai/page_explorer.py:70-153` (`_compute_element_stability`)
- Modify: `backend/app/ai/page_explorer.py:156-229` (`_format_element_rich`)
- Modify: `backend/app/runners/pre_scorer.py:128-131` (nth-child fragility already exists, verify)

**Goal:** CSS nth-of-type/nth-child 选择器得分极低（≤0.15），无障碍树 aria-label+role 提升到 0.90，重复文本元素得分降低

### Step 1: 重写 `_compute_element_stability` 评分逻辑

Current scoring:
- data-testid unique: 0.95 ✓
- stable id: 0.90 ✓
- aria-label + role unique: 0.80 → **提升到 0.90**
- text-only match (unique text): 0.55 → **需要区分**
- text-only duplicate + CSS: 0.45 → **降为 0.15-0.25**
- CSS nth-of-type xpath: 0.20 → **降为 0.10**

```python
def _compute_element_stability(element: dict[str, Any], all_elements: list[dict[str, Any]]) -> float:
    """Compute a stability score for an element based on its distinguishing attributes.

    Scoring rules:
    - data-testid unique: 0.95
    - aria-label + role unique: 0.90  (accessibility tree first)
    - stable id (non-hash): 0.85
    - name/type combo: 0.75
    - href with business path: 0.70
    - unique text: 0.50
    - text with duplicates AND stable CSS: 0.30
    - text with duplicates AND fragile CSS: 0.15
    - CSS/XPath with nth-child/nth-of-type: 0.10
    - bare XPath with position index: 0.10
    """
    tag = element.get("tag", "")
    text = element.get("text") or ""
    data_testid = element.get("data_testid")
    elem_id = element.get("id") or ""
    aria_label = element.get("aria_label")
    role = element.get("role")
    href = element.get("href")
    css = element.get("css_selector") or ""
    xpath = element.get("xpath") or ""

    # Count duplicates by tag+text
    same_tag_text = sum(
        1 for e in all_elements
        if e.get("tag") == tag and (e.get("text") or "") == text
    )
    has_duplicates = same_tag_text > 1

    # Detect fragile CSS patterns (nth-child, nth-of-type, deep nesting)
    _FRAGILE_CSS = re.compile(r":nth-(child|of-type)\(|>\s*(body|html|div)\s*>\s*div\s*>\s*div")
    css_is_fragile = bool(_FRAGILE_CSS.search(css)) or bool(_FRAGILE_CSS.search(xpath))

    # 1. data-testid (highest priority)
    if data_testid:
        testid_count = sum(1 for e in all_elements if e.get("data_testid") == data_testid)
        if testid_count == 1:
            return 0.95
        return 0.85

    # 2. aria-label + role unique (accessibility tree — second highest)
    if aria_label and role:
        combo_count = sum(
            1 for e in all_elements
            if e.get("aria_label") == aria_label and e.get("role") == role
        )
        if combo_count == 1:
            return 0.90
    if aria_label:
        al_count = sum(1 for e in all_elements if e.get("aria_label") == aria_label)
        if al_count == 1:
            return 0.82

    # 3. stable element id (not hash/uuid pattern)
    _DYNAMIC_ID = re.compile(r"[0-9a-f]{8,}|auto\d+|tmp|rnd", re.IGNORECASE)
    if elem_id and not _DYNAMIC_ID.search(elem_id):
        id_count = sum(1 for e in all_elements if e.get("id") == elem_id)
        if id_count == 1:
            return 0.85

    # 4. href with business path
    if href and tag == "a" and not href.startswith(("#", "javascript:")):
        href_count = sum(1 for e in all_elements if e.get("href") == href and e.get("tag") == "a")
        if href_count == 1:
            return 0.70
        if href_count <= 3:
            return 0.55

    # 5. Fragile CSS/XPath — lowest score
    if css_is_fragile:
        return 0.10

    # 6. Unique text
    if text and not has_duplicates:
        return 0.50

    # 7. Text with duplicates
    if text and has_duplicates:
        if css and len(css) < 60 and not css_is_fragile:
            return 0.30
        return 0.15

    # 8. XPath with position index
    if re.search(r"\[\d+\]", xpath):
        return 0.10

    return 0.20
```

### Step 2: 更新 `_format_element_rich` 标注低分元素

After the existing line 218 (`extras.append(f"stable={stability:.2f}")`), add UNSTABLE marker:

```python
    extras.append(f"stable={stability:.2f}")
    if stability < 0.30:
        extras.append("[UNSTABLE—avoid as primary locator]")
```

### Step 3: 更新 pre_scorer fragility 检测增强

In `backend/app/runners/pre_scorer.py`, verify `detect_fragility` already catches `nth_child`. The `:nth-child(` and `:nth-of-type(` already have a flag at line 128-129. Add a CSS depth fragility flag:

After line 129 (`if ":nth-child(" in selector or ":nth-of-type(" in selector:`), add:

```python
    # --- deep_css ---
    if selector.count(" > ") >= 3:
        flags.append("deep_css")
```

### Step 4: 运行已有测试验证

```bash
cd backend && uv run pytest tests/unit/test_page_explorer.py tests/unit/test_locator_semantic.py tests/unit/test_locator_fallback.py -v
```

Expected: all existing tests pass, stability scores may change but test assertions should still hold

### Step 5: Commit

```bash
git add backend/app/ai/page_explorer.py backend/app/runners/pre_scorer.py
git commit -m "fix: reprioritize element stability scoring — a11y tree above DOM, penalize nth-of-type CSS"
```

---

## Task 2: L2 — generate_plan 流程守卫增强

**Files:**
- Modify: `backend/app/ai/test_planning_agent.py:293-351` (generate_plan guard)
- Modify: `backend/app/ai/test_planning_agent.py:1975-2018` (`_build_draft_prompt`)
- Test: `backend/tests/unit/test_planning_agent.py`

**Goal:** 在 generate_plan 时强制检查页面探索数据质量（不只是是否存在），并在 draft prompt 中注入变量未定义警告

### Step 1: 增强 `generate_plan` 分支的探索质量检查

Replace the existing `if action == "generate_plan":` block (lines 293-389) with enhanced quality checks:

In `backend/app/ai/test_planning_agent.py`, replace lines 293-298:

```python
        if action == "generate_plan":
            has_explore = _has_explored_pages(tool_calls)
            has_flow = any(call.tool == "explore_flow" for call in tool_calls)
```

With quality-aware checks:

```python
        if action == "generate_plan":
            has_explore = _has_explored_pages(tool_calls)
            has_flow = any(call.tool == "explore_flow" for call in tool_calls)

            # Check exploration QUALITY, not just existence
            if has_explore:
                exploration_elements = _count_explored_elements(tool_calls)
                if exploration_elements < 10:
                    yield {"type": "status", "phase": "tool_call",
                           "message": f"页面探索仅采集到 {exploration_elements} 个元素，数据不足，需要更多探索"}
                    conversation.append(
                        {"role": "system", "content": (
                            f"⚠️ 页面探索仅采集到 {exploration_elements} 个元素，数据严重不足。"
                            "请使用 explore_page 采集更多页面（如登录页、商品列表页、购物车页）。"
                            "没有足够元素数据时不要生成 DSL。"
                        )},
                    )
                    continue
```

### Step 2: 新增 `_count_explored_elements` 辅助函数

Add before `_has_explored_pages` (line 975):

```python
def _count_explored_elements(tool_calls: list[AIPlanningToolCall]) -> int:
    """Return total element count across all explore_page/explore_flow calls."""
    total = 0
    for call in tool_calls:
        if call.tool == "explore_page" and isinstance(call.result, dict):
            total += int(call.result.get("element_count", 0))
        elif call.tool == "explore_flow" and isinstance(call.result, dict):
            for page in call.result.get("pages", []) or call.result.get("page_results", []):
                if isinstance(page, dict):
                    total += int(page.get("element_count", 0))
    return total
```

### Step 3: 新增 `_extract_undefined_variables` 检查函数

Before `_build_draft_prompt` (line 1975):

```python
_VARIABLE_REF_RE = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")

def _extract_undefined_variables(
    steps: list[dict[str, Any]],
    input_contract: list[dict[str, Any]] | None = None,
) -> list[str]:
    """Return variables referenced in steps but not defined in input_contract or capture_text."""
    defined = set()
    if input_contract:
        for c in input_contract:
            if isinstance(c, dict) and c.get("context_key"):
                defined.add(c["context_key"])

    # capture_text steps define runtime variables
    for step in steps:
        if isinstance(step, dict) and step.get("action") == "capture_text":
            ck = step.get("context_key")
            if ck:
                defined.add(ck)

    referenced = set()
    for step in steps:
        if not isinstance(step, dict):
            continue
        for field in ("value", "target"):
            val = step.get(field, "")
            if isinstance(val, str):
                for match in _VARIABLE_REF_RE.finditer(val):
                    referenced.add(match.group(1))

    return sorted(referenced - defined)
```

### Step 4: 在 `_build_draft_prompt` 中注入变量警告

In `_build_draft_prompt`, add at the end of the returned prompt string (line 2018), before returning:

```python
    # Check for undefined variable references
    steps = []
    # (steps come from the scenario context, handled by the caller)
    
    return (
        # ... existing prompt ...
        f"{dom_section}"
        # Variable safety reminder:
        "\n\n重要：所有使用 ${variable_name} 格式引用的变量，必须先在 input_contract 中定义（如通过 capture_text 捕获的运行时变量必须在 output_contract 中出现）。"
        "如果某个变量（如 product_a_price）是从页面提取的，必须先用 capture_text 步骤捕获它，再在后续 assert_text 中引用。"
        "不要引用未在 input_contract 或 capture_text 中定义的变量。"
    )
```

### Step 5: 单元测试

```python
# Add to backend/tests/unit/test_planning_agent.py

def test_count_explored_elements_empty():
    from app.ai.test_planning_agent import _count_explored_elements
    assert _count_explored_elements([]) == 0

def test_count_explored_elements_with_explore_page():
    from app.ai.test_planning_agent import _count_explored_elements
    from app.schemas.ai_planning import AIPlanningToolCall
    calls = [
        AIPlanningToolCall(tool="explore_page", params={}, result={"element_count": 250}),
        AIPlanningToolCall(tool="explore_flow", params={}, result={"pages": [
            {"element_count": 100}, {"element_count": 50}
        ]}),
    ]
    assert _count_explored_elements(calls) == 400

def test_extract_undefined_variables_detects_missing():
    from app.ai.test_planning_agent import _extract_undefined_variables
    steps = [
        {"action": "assert_text", "target": "td > h4", "value": "${product_a_name}"},
        {"action": "assert_text", "target": "td > p", "value": "${product_a_price}"},
    ]
    input_contract = [
        {"name": "登录邮箱", "context_key": "login_email", "value_type": "string", "required": True},
    ]
    undefined = _extract_undefined_variables(steps, input_contract)
    assert "product_a_name" in undefined
    assert "product_a_price" in undefined
    assert "login_email" not in undefined

def test_extract_undefined_variables_handles_capture_text():
    from app.ai.test_planning_agent import _extract_undefined_variables
    steps = [
        {"action": "capture_text", "target": "Product Name", "context_key": "product_a_name"},
        {"action": "assert_text", "target": "cart td", "value": "${product_a_name}"},
    ]
    undefined = _extract_undefined_variables(steps, [])
    assert len(undefined) == 0

def test_extract_undefined_variables_all_vars_defined():
    from app.ai.test_planning_agent import _extract_undefined_variables
    steps = [
        {"action": "input", "target": "Email", "value": "${login_email}"},
        {"action": "input", "target": "Password", "value": "${login_password}"},
    ]
    input_contract = [
        {"context_key": "login_email", "value_type": "string", "name": "邮箱", "required": True},
        {"context_key": "login_password", "value_type": "string", "name": "密码", "required": True},
    ]
    assert _extract_undefined_variables(steps, input_contract) == []
```

### Step 6: 运行测试

```bash
cd backend && uv run pytest tests/unit/test_planning_agent.py -v -k "count_explored or extract_undefined"
```

Expected: 4 new tests PASS

### Step 7: Commit

```bash
git add backend/app/ai/test_planning_agent.py backend/tests/unit/test_planning_agent.py
git commit -m "feat: add exploration quality guard and undefined variable detection to ReAct loop"
```

---

## Task 3: L3 — 自动化 E2E 回归脚本

**Files:**
- Create: `backend/scripts/e2e_regression.py`
- Input: `test_brand_filter_cart` (project root)

**Goal:** 通过 API 驱动完整 AI 规划 → DSL 生成 → 执行流程，统计步骤通过率，循环直到 ≥80%

### Step 1: 创建回归脚本

```python
#!/usr/bin/env python3
"""E2E regression loop — feeds test_brand_filter_cart to the AI agent,
executes the generated test case, measures step success rate, and iterates
until the target rate is met or the loop limit is reached.

Usage: uv run python scripts/e2e_regression.py
"""

from __future__ import annotations

import json
import os
import sys
import time
import urllib.request
import urllib.error
from pathlib import Path
from typing import Any

BASE_URL = os.environ.get("E2E_BASE_URL", "http://127.0.0.1:8000")
MAX_ROUNDS = 10
TARGET_PASS_RATE = 0.80

# ── helpers ──────────────────────────────────────────────────────────

def _req(method: str, path: str, body: dict | None = None) -> tuple[int, Any]:
    url = f"{BASE_URL}{path}"
    data = json.dumps(body).encode() if body else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return resp.status, json.loads(resp.read()) if resp.status != 204 else None
    except urllib.error.HTTPError as exc:
        try:
            err_body = exc.read().decode()
        except Exception:
            err_body = str(exc)
        return exc.code, err_body


def _get(path: str) -> Any:
    _, data = _req("GET", path)
    return data


def _post(path: str, body: dict) -> Any:
    _, data = _req("POST", path, body)
    return data


def _read_test_file() -> str:
    test_file = Path(__file__).parent.parent.parent / "test_brand_filter_cart"
    return test_file.read_text(encoding="utf-8")


def _wait_for_drafts(session_id: int, max_wait: int = 300) -> list[dict]:
    """Poll session messages until drafts are ready or timeout."""
    deadline = time.time() + max_wait
    while time.time() < deadline:
        time.sleep(5)
        session = _get(f"/api/v1/ai-planning/sessions/{session_id}")
        msgs = session.get("messages", [])
        for msg in msgs:
            sp = msg.get("structured_payload") or {}
            if isinstance(sp, dict) and "drafts" in sp:
                drafts = sp["drafts"]
                # Wait for at least one generated draft
                if any(d.get("status") in ("generated", "imported") for d in drafts):
                    return drafts
        status = session.get("session", session).get("status", "")
        print(f"  Waiting for drafts... status={status}")
    return []


def _execute_draft(session_id: int, draft_id: int, input_values: dict) -> dict | None:
    """Save and execute a draft, return execution summary."""
    body = {
        "draft_ids": [draft_id],
        "execute": True,
        "input_values": input_values,
    }
    result = _post(f"/api/v1/ai-planning/sessions/{session_id}/drafts:save-and-execute", body)
    return result


def _get_execution_result(execution_id: int) -> dict:
    return _get(f"/api/v1/executions/{execution_id}")


def _compute_pass_rate(execution: dict) -> tuple[int, int, list[dict]]:
    steps = execution.get("step_results", [])
    if isinstance(steps, str):
        steps = json.loads(steps)
    if not steps:
        return 0, 0, []
    passed = sum(1 for s in steps if s.get("status") == "passed")
    return passed, len(steps), steps


# ── main loop ───────────────────────────────────────────────────────

def main() -> int:
    test_content = _read_test_file()
    print(f"=== E2E Regression: test_brand_filter_cart ===\n")
    print(f"Target: {TARGET_PASS_RATE*100:.0f}% pass rate | Max rounds: {MAX_ROUNDS}\n")

    for round_num in range(1, MAX_ROUNDS + 1):
        print(f"--- Round {round_num}/{MAX_ROUNDS} ---")

        # 1. Create session
        print("  Creating session...")
        session = _post("/api/v1/ai-planning/sessions", {"title": f"E2E Regression Round {round_num}"})
        session_id = session.get("session", session).get("id")
        if not session_id:
            print(f"  FAIL: Could not create session: {session}")
            continue
        print(f"  Session {session_id} created")

        # 2. Send test requirements
        print("  Sending requirements...")
        msg_resp = _post(
            f"/api/v1/ai-planning/sessions/{session_id}/chat",
            {"message": test_content},
        )
        print(f"  Requirements sent, status={msg_resp.get('session_status', '?')}")

        # 3. Wait for drafts to be generated
        print("  Waiting for AI to generate drafts...")
        drafts = _wait_for_drafts(session_id)
        if not drafts:
            # Try triggering draft generation
            print("  No drafts found, triggering generation...")
            _post(
                f"/api/v1/ai-planning/sessions/{session_id}/drafts:generate",
                {"scenario_keys": [], "force": True},
            )
            drafts = _wait_for_drafts(session_id)

        if not drafts:
            print("  FAIL: No drafts generated")
            continue

        print(f"  Got {len(drafts)} drafts:")
        for d in drafts:
            print(f"    Draft {d.get('id')}: {d.get('scenario_title', '?')} [{d.get('status', '?')}]")

        # 4. Pick the first generated/imported draft
        usable = [d for d in drafts if d.get("status") in ("generated", "imported")]
        if not usable:
            print("  No usable drafts")
            continue
        draft = usable[0]
        draft_id = draft["id"]
        print(f"  Selected draft {draft_id}: {draft.get('scenario_title', '?')}")

        # 5. Execute
        input_values = {
            "login_email": "Xjy13302412005@outlook.com",
            "login_password": "123456",
        }
        print("  Executing draft...")
        exec_result = _execute_draft(session_id, draft_id, input_values)
        if not exec_result:
            print("  FAIL: Execution returned no result")
            continue

        summaries = exec_result.get("execution_summaries", [])
        if not summaries:
            print("  FAIL: No execution summaries")
            continue

        # 6. Get full execution details
        for summary in summaries:
            exec_id = summary.get("execution_id")
            run_status = summary.get("status", "unknown")
            if not exec_id:
                continue

            execution = _get_execution_result(exec_id)
            passed, total, step_results = _compute_pass_rate(execution)
            rate = passed / total if total > 0 else 0
            print(f"\n  Result: Run {exec_id}: {passed}/{total} passed ({rate*100:.1f}%)")

            # Print failed steps
            for i, s in enumerate(step_results):
                status = s.get("status", "?")
                if status != "passed":
                    action = s.get("action", "?")
                    target = s.get("target", "")
                    err = s.get("error", "")[:200]
                    strat = s.get("locator_strategy", "?")
                    print(f"    FAIL Step {i}: [{action}] target={target!r} strategy={strat!r}")
                    print(f"      Error: {err}")

            print(f"  Session: http://127.0.0.1:5173/planning/{session_id}")
            print(f"  Report: http://127.0.0.1:5173/run/{exec_id}")

            if rate >= TARGET_PASS_RATE:
                print(f"\n✅ Target reached in round {round_num}: {rate*100:.1f}%")
                return 0

        print()

    print(f"⚠️ Max rounds ({MAX_ROUNDS}) reached without hitting {TARGET_PASS_RATE*100:.0f}% target")
    return 1


if __name__ == "__main__":
    sys.exit(main())
```

### Step 2: 验证脚本语法

```bash
cd backend && uv run python -c "import ast; ast.parse(open('scripts/e2e_regression.py').read()); print('Syntax OK')"
```

### Step 3: Commit

```bash
git add backend/scripts/e2e_regression.py
git commit -m "feat: add E2E regression script for automated AI agent quality testing"
```

---

## Task 4: 第一轮 E2E 测试 — 执行并分析

**Goal:** 运行回归脚本，获取第一轮 AI agent 产出的基线成功率

### Step 1: 确保后端运行

```bash
curl -s http://127.0.0.1:8000/api/v1/ai-planning/sessions?limit=1 | python -c "import json,sys; d=json.load(sys.stdin); print('Backend OK, sessions:', len(d))"
```

### Step 2: 运行回归脚本第 1 轮

```bash
cd backend && uv run python scripts/e2e_regression.py
```

### Step 3: 分析结果

After the run, check:
1. How many steps passed vs failed
2. Which step types fail most (assert_text, click, input, etc.)
3. What locator strategies were used
4. Whether variables were properly substituted
5. Whether AI explored pages before generating

Record findings to `docs/execution-log.md`

### Step 4: 根据结果决定下一步

- If pass rate ≥ 80%: done
- If < 80%: analyze failures and iterate on Tasks 1-3 fixes
- Common fixes needed:
  - Adjust stability scoring weights further
  - Strengthen ReAct guard conditions
  - Fix specific locator strategy failures
  - Add more page exploration in the flow
