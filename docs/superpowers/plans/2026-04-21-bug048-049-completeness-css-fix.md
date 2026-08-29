# BUG-048 & BUG-049 双缺陷修复计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 AI DSL 生成完整性校验缺失（BUG-048）和语义定位器不支持复合 CSS 选择器（BUG-049），双重保底。

**Architecture:** BUG-049 分两层修复：定位器侧增加复合 CSS 启发式检测，AI Prompt 侧增加 CSS 选择器指引。BUG-048 分两层修复：Prompt 侧增加测试五要素引导，后处理侧增加完整性 warning 检测。两个 BUG 相互独立，可并行。

**Tech Stack:** Python 3.12, Playwright locators, unittest/pytest, Pydantic

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `backend/app/locators/semantic.py` | Modify | 增加复合 CSS 选择器启发式检测 |
| `backend/app/ai/dsl_generator.py` | Modify | Prompt 增加完整性引导 + CSS 指引 + 后处理 completeness warning |
| `backend/tests/unit/test_locator_semantic.py` | Modify | 增加复合 CSS 定位器测试 |
| `backend/tests/unit/test_dsl_validation.py` | Modify | 增加 Prompt 规则 + completeness warning 测试 |

---

## Task 1: BUG-049 — 定位器侧支持复合 CSS 选择器

**Files:**
- Modify: `backend/app/locators/semantic.py:1-3` (新增 import re)
- Modify: `backend/app/locators/semantic.py:163-177` (扩展 `_resolve_explicit_locator`)
- Modify: `backend/app/locators/semantic.py:136` (element_id 跳过已有 explicit 的 target)
- Test: `backend/tests/unit/test_locator_semantic.py`

- [ ] **Step 1: 写失败测试**

在 `backend/tests/unit/test_locator_semantic.py` 末尾追加：

```python
class TestCompoundCssSelector:
    """BUG-049: 复合 CSS 选择器（如 button[type='submit']）应被识别为 CSS 策略。"""

    def test_tag_with_attribute_selector(self):
        """button[type='submit'] 应被解析为 CSS，而非文本匹配。"""
        page = FakePage({
            "locator:button[type='submit']": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "button[type='submit']")
        assert result.strategy == "css"

    def test_tag_child_selector(self):
        """'form button' 应被解析为 CSS。"""
        page = FakePage({
            "locator:form button": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "form button")
        assert result.strategy == "css"

    def test_tag_with_class_selector(self):
        """'div.container' 应被解析为 CSS。"""
        page = FakePage({
            "locator:div.container": [_candidate(preview_text="content", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "div.container")
        assert result.strategy == "css"

    def test_tag_direct_child_selector(self):
        """'form > button' 应被解析为 CSS。"""
        page = FakePage({
            "locator:form > button": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "form > button")
        assert result.strategy == "css"

    def test_plain_text_not_treated_as_css(self):
        """'Login' 不应被解析为 CSS。"""
        page = FakePage({
            "text:Login:True": [_candidate(preview_text="Login", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "Login")
        assert result.strategy != "css"

    def test_single_tag_not_treated_as_css(self):
        """'button'（裸标签名）不应被解析为 CSS，应走文本匹配。"""
        page = FakePage({
            "text:button:True": [_candidate(preview_text="button", visible=True, enabled=True)],
        })
        result = resolve_semantic_locator(page, "button")
        assert result.strategy != "css"
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_locator_semantic.py::TestCompoundCssSelector -v`
Expected: 多个 FAIL（`_resolve_explicit_locator` 不识别复合 CSS）

- [ ] **Step 3: 实现 compound CSS 检测**

在 `backend/app/locators/semantic.py` 顶部 import 区域添加：

```python
import re
```

在 `_resolve_explicit_locator` 函数前（约第 164 行）添加模块级正则：

```python
_COMPOUND_CSS_RE = re.compile(r"^[a-zA-Z][a-zA-Z0-9]*[\.\#\[\s\>:,~\+]")
```

修改 `_resolve_explicit_locator`（第 165-177 行），在 `return None` 之前插入复合 CSS 检测：

```python
def _resolve_explicit_locator(page, target: str) -> tuple[str, object] | None:
    if target.startswith("css="):
        return ("css", lambda: page.locator(target))
    if target.startswith("xpath="):
        return ("xpath", lambda: page.locator(target))
    if target.startswith("//"):
        return ("xpath", lambda: page.locator(f"xpath={target}"))
    if target.startswith(("#", ".", "[")):
        return ("css", lambda: page.locator(target))
    if target.startswith("data-testid="):
        value = target.split("=", 1)[1]
        return ("data-testid", lambda: page.get_by_test_id(value))
    if _COMPOUND_CSS_RE.match(target):
        return ("css", lambda: page.locator(target))
    return None
```

修改 `_build_candidate_builders`（第 136 行），将 element_id 策略改为仅在无 explicit locator 时尝试：

```python
    # Try matching the target as an HTML element id attribute.
    if explicit is None and target and not target.startswith(("css=", "xpath=", "//", "#", ".", "[", "data-testid=")):
        id_target = target
        builders.append(("element_id", lambda: page.locator(f"#{id_target}")))
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_locator_semantic.py -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/locators/semantic.py backend/tests/unit/test_locator_semantic.py
git commit -m "fix: support compound CSS selectors in semantic locator (BUG-049)"
```

---

## Task 2: BUG-049 — AI Prompt 侧增加 CSS 选择器指引

**Files:**
- Modify: `backend/app/ai/dsl_generator.py:139-145` (`_BASE_USER_RULE_LINES`)
- Test: `backend/tests/unit/test_dsl_validation.py`

- [ ] **Step 1: 写失败测试**

在 `backend/tests/unit/test_dsl_validation.py` 末尾追加：

```python
def test_base_user_rules_include_css_selector_guidance():
    """BUG-049: Prompt 规则中应包含复合 CSS 选择器使用指引。"""
    from backend.app.ai.dsl_generator import _BASE_USER_RULE_LINES
    joined = "\n".join(_BASE_USER_RULE_LINES)
    assert "CSS" in joined or "css" in joined
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_dsl_validation.py::test_base_user_rules_include_css_selector_guidance -v`
Expected: FAIL

- [ ] **Step 3: 在 `_BASE_USER_RULE_LINES` 中添加 CSS 指引**

修改 `backend/app/ai/dsl_generator.py` 第 139-145 行：

```python
_BASE_USER_RULE_LINES = [
    "要求：",
    "- steps 必须是数组，且每个 step 只能使用允许的 action。",
    "- input_contract 和 output_contract 如无需要，返回空数组。",
    "- 如果是相对路径跳转，优先保留为相对路径，并在 base_url 中提供站点地址。",
    "- 如果提供了当前 DSL 或当前 steps，请把它们视为改写上下文，而不是忽略。",
    "- 复合 CSS 选择器（如 button[type='submit']、form button）可直接使用，无需 css= 前缀；如果不确定是否为合法选择器，加上 css= 前缀。",
]
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_dsl_validation.py::test_base_user_rules_include_css_selector_guidance -v`
Expected: PASS

- [ ] **Step 5: 运行全部 DSL 验证测试确认无回归**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_dsl_validation.py -v`
Expected: ALL PASS

- [ ] **Step 6: Commit**

```bash
git add backend/app/ai/dsl_generator.py backend/tests/unit/test_dsl_validation.py
git commit -m "fix: add CSS selector guidance to AI DSL prompt (BUG-049)"
```

---

## Task 3: BUG-048 — AI Prompt 增加测试五要素完整性引导

**Files:**
- Modify: `backend/app/ai/dsl_generator.py:139-145` (`_BASE_USER_RULE_LINES`)
- Test: `backend/tests/unit/test_dsl_validation.py`

- [ ] **Step 1: 写失败测试**

在 `backend/tests/unit/test_dsl_validation.py` 末尾追加：

```python
def test_base_user_rules_include_completeness_guidance():
    """BUG-048: Prompt 规则中应包含测试五要素完整性引导。"""
    from backend.app.ai.dsl_generator import _BASE_USER_RULE_LINES
    joined = "\n".join(_BASE_USER_RULE_LINES)
    # 应包含入口/导航相关引导
    assert "base_url" in joined or "站点" in joined
    # 应包含完整性评估要求
    assert "完整" in joined or "入口" in joined or "前置" in joined
```

- [ ] **Step 2: 运行测试确认失败或通过（baseline 校验）**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_dsl_validation.py::test_base_user_rules_include_completeness_guidance -v`

注意：如果 Task 2 的修改已包含 "base_url" 或 "站点" 字样，第一个 assert 可能已通过。核心验证是第二个 assert 关于完整性引导。

- [ ] **Step 3: 在 `_BASE_USER_RULE_LINES` 中添加完整性引导**

修改 `backend/app/ai/dsl_generator.py` 的 `_BASE_USER_RULE_LINES`，在 CSS 指引行之后追加两行：

```python
_BASE_USER_RULE_LINES = [
    "要求：",
    "- steps 必须是数组，且每个 step 只能使用允许的 action。",
    "- input_contract 和 output_contract 如无需要，返回空数组。",
    "- 如果是相对路径跳转，优先保留为相对路径，并在 base_url 中提供站点地址。",
    "- 如果提供了当前 DSL 或当前 steps，请把它们视为改写上下文，而不是忽略。",
    "- 复合 CSS 选择器（如 button[type='submit']、form button）可直接使用，无需 css= 前缀；如果不确定是否为合法选择器，加上 css= 前缀。",
    "- base_url 应为站点根地址（如 https://example.com），页面路径放在 goto 步骤中（如 /login）。不要将完整页面 URL 填入 base_url。",
    "- 生成前评估测试信息完整性：前置条件（系统初始状态）、入口（目标页面 URL 或导航路径）、操作步骤、预期结果。如果描述中缺少入口信息，通过 base_url + goto 步骤明确入口。",
]
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_dsl_validation.py -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add backend/app/ai/dsl_generator.py backend/tests/unit/test_dsl_validation.py
git commit -m "fix: add test completeness guidance to AI DSL prompt (BUG-048)"
```

---

## Task 4: BUG-048 — 后处理增加完整性 warning 检测

**Files:**
- Modify: `backend/app/ai/dsl_generator.py` (新增 `_check_dsl_completeness` 函数)
- Modify: `backend/app/ai/dsl_generator.py:553-558` (在 `_normalize_generated_case` 中调用)
- Test: `backend/tests/unit/test_dsl_validation.py`

- [ ] **Step 1: 写失败测试**

在 `backend/tests/unit/test_dsl_validation.py` 末尾追加：

```python
class TestDslCompletenessCheck:
    """BUG-048: 完整性检测应在 base_url 含路径或缺少 goto 时发出 warning。"""

    def test_warns_when_base_url_contains_path(self):
        """base_url 含页面路径时应发出 warning。"""
        from backend.app.ai.dsl_generator import _check_dsl_completeness
        warnings = []
        _check_dsl_completeness(
            {"base_url": "https://example.com/login", "steps": [{"action": "input", "target": "#user", "value": "test"}]},
            warnings,
        )
        assert any("base_url" in w and "路径" in w for w in warnings)

    def test_warns_when_no_goto_with_base_url(self):
        """有 base_url 和 steps 但无 goto 时应发出 normalization_note。"""
        from backend.app.ai.dsl_generator import _check_dsl_completeness
        warnings = []
        notes = []
        _check_dsl_completeness(
            {"base_url": "https://example.com", "steps": [{"action": "input", "target": "#user", "value": "test"}]},
            warnings,
            notes,
        )
        assert any("goto" in n for n in notes)

    def test_no_warning_when_goto_present(self):
        """有 goto 步骤时不应发出 goto 相关 warning。"""
        from backend.app.ai.dsl_generator import _check_dsl_completeness
        warnings = []
        notes = []
        _check_dsl_completeness(
            {"base_url": "https://example.com", "steps": [{"action": "goto", "value": "/login"}]},
            warnings,
            notes,
        )
        assert not any("goto" in n for n in notes)

    def test_no_warning_when_no_base_url(self):
        """无 base_url 时不应发出 base_url 路径 warning。"""
        from backend.app.ai.dsl_generator import _check_dsl_completeness
        warnings = []
        _check_dsl_completeness(
            {"steps": [{"action": "click", "target": "Login"}]},
            warnings,
        )
        assert not any("base_url" in w for w in warnings)

    def test_no_warning_for_root_base_url(self):
        """base_url 为根路径时不应发出 warning。"""
        from backend.app.ai.dsl_generator import _check_dsl_completeness
        warnings = []
        _check_dsl_completeness(
            {"base_url": "https://example.com", "steps": [{"action": "goto", "value": "/login"}]},
            warnings,
        )
        assert not any("base_url" in w and "路径" in w for w in warnings)
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_dsl_validation.py::TestDslCompletenessCheck -v`
Expected: FAIL（`_check_dsl_completeness` 不存在）

- [ ] **Step 3: 实现 `_check_dsl_completeness` 函数**

在 `backend/app/ai/dsl_generator.py` 的 `_normalize_steps` 函数之前（约第 654 行）添加：

```python
def _check_dsl_completeness(
    case_data: dict[str, Any],
    warnings: list[str],
    normalization_notes: list[str] | None = None,
) -> None:
    """检查生成 DSL 的完整性，对可疑模式发出 warning。

    不阻断生成，仅发出提示，保持用例灵活性。
    """
    if normalization_notes is None:
        normalization_notes = []

    base_url = case_data.get("base_url") or ""
    steps = case_data.get("steps") or []

    # 检测 base_url 是否包含页面路径（如 https://example.com/login）
    if base_url:
        from urllib.parse import urlparse
        parsed = urlparse(base_url)
        if parsed.path and parsed.path.strip("/"):
            warnings.append(
                f"base_url 疑似包含页面路径（{parsed.path}），建议将站点根地址放在 base_url，"
                f"页面路径放在 goto 步骤中（如 goto {parsed.path}）。"
            )

    # 检测有 base_url 和 steps 但无 goto 步骤的情况
    has_goto = any(
        isinstance(s, dict) and s.get("action") == "goto"
        for s in steps
    )
    if base_url and steps and not has_goto:
        normalization_notes.append(
            "DSL 中没有 goto 步骤。如果测试需要先导航到目标页面，建议添加 goto 步骤。"
        )
```

- [ ] **Step 4: 在 `_normalize_generated_case` 中调用完整性检查**

在 `backend/app/ai/dsl_generator.py` 的 `_normalize_generated_case` 函数中，在 steps normalize 之后（约第 558 行 `steps, removed_invalid_steps, ... = _normalize_steps(...)` 之后）插入调用：

```python
    _check_dsl_completeness(
        {"base_url": base_url_value, "steps": [{"action": s.action, **({"value": s.value} if hasattr(s, "value") else {}), **({"target": s.target} if hasattr(s, "target") else {})} for s in steps]},
        warnings,
        normalization_notes,
    )
```

等价简化写法——直接用 raw_case 中已有的 base_url 和 normalized steps：

```python
    _check_dsl_completeness(
        {"base_url": base_url_value, "steps": steps},
        warnings,
        normalization_notes,
    )
```

注意：DSLStep 是 Pydantic model，`isinstance(s, dict)` 检查会返回 False。需要调整 `_check_dsl_completeness` 中 has_goto 的检测方式，兼容 dict 和 Pydantic model：

```python
    has_goto = any(
        (isinstance(s, dict) and s.get("action") == "goto")
        or (hasattr(s, "action") and getattr(s, "action", None) == "goto")
        for s in steps
    )
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_dsl_validation.py::TestDslCompletenessCheck -v`
Expected: ALL PASS

- [ ] **Step 6: 运行全部 DSL 测试确认无回归**

Run: `cd d:/AutoTestingLearingProject/AI_Web_Testing/backend && uv run pytest tests/unit/test_dsl_validation.py -v`
Expected: ALL PASS

- [ ] **Step 7: Commit**

```bash
git add backend/app/ai/dsl_generator.py backend/tests/unit/test_dsl_validation.py
git commit -m "fix: add completeness warning to DSL post-processing (BUG-048)"
```

---

## Task 5: 更新 bug-log 并最终提交

**Files:**
- Modify: `docs/bug-log.md`

- [ ] **Step 1: 更新 BUG-048 状态**

将 `docs/bug-log.md` 中 BUG-048 的 `- 状态：open` 改为 `- 状态：fixed`，`- 处理：` 改为：

```
- 处理：已修复。(1) Prompt 增加测试五要素完整性引导和 base_url 规范说明（dsl_generator.py _BASE_USER_RULE_LINES）；(2) 后处理新增 _check_dsl_completeness 函数，检测 base_url 含页面路径和无 goto 步骤时发出 warning/normalization_note，不阻断生成，保持灵活性
```

`- 验证：` 改为：

```
- 验证：cd backend && uv run pytest tests/unit/test_dsl_validation.py tests/unit/test_locator_semantic.py -v
```

- [ ] **Step 2: 更新 BUG-049 状态**

将 `docs/bug-log.md` 中 BUG-049 的 `- 状态：open` 改为 `- 状态：fixed`，`- 处理：` 改为：

```
- 处理：已修复，双重保底。(1) 定位器侧：_resolve_explicit_locator 新增 _COMPOUND_CSS_RE 启发式正则，识别 tag[attr]、tag.class、tag > child、tag child 等复合 CSS 模式；_build_candidate_builders 在已有 explicit locator 时跳过 element_id 策略避免误匹配；(2) AI Prompt 侧：_BASE_USER_RULE_LINES 增加复合 CSS 选择器使用指引
```

`- 验证：` 改为：

```
- 验证：cd backend && uv run pytest tests/unit/test_locator_semantic.py tests/unit/test_dsl_validation.py -v
```

- [ ] **Step 3: Commit**

```bash
git add docs/bug-log.md
git commit -m "docs: update BUG-048 and BUG-049 status to fixed"
```
