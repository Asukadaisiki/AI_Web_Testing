# DSL 执行流程 — Session 309 Step-by-Step 代码追踪

以 Session 309 / Run 204 的真实 DSL（TestCase 138）为例，逐步骤追踪代码执行路径。

---

## 总览：一条 DSL Step 的完整生命周期

```
DSL JSON Step
  │
  ▼
┌──────────────────────────────────────────────────────────────┐
│  playwright_runner.py  _execute_steps()                       │
│                                                              │
│  for step in dsl.steps:                                      │
│      step.action   → goto / click / capture_text / ...       │
│      step.target   → 'link "Products"' / 'heading "Rs.500"'  │
│      step.value    → "https://..." / "${var}" / "2"          │
└──────────────────────┬───────────────────────────────────────┘
                       │
          ┌────────────┼────────────┐
          ▼            ▼            ▼
      goto        需要定位       不需要定位
     page.goto()   的action     (assert_url)
                      │
          ┌───────────▼───────────┐
          │ _resolve_with_        │
          │   confidence_gate()   │  ← runner.py:44
          │     │                 │
          │     ▼                 │
          │ resolve_with_fallback │  ← fallback.py:70
          │     │                 │
          │  Tier 0: correction   │  ← fallback.py:88
          │  Tier 1: semantic     │  ← fallback.py:120  ⭐ 主力路径
          │  Tier 2: VLM visual   │  ← fallback.py:144
          │  Tier 3: intervention │  ← fallback.py:159
          └───────────┬───────────┘
                      │
          ┌───────────▼───────────┐
          │  执行动作              │
          │  click  → click()     │
          │  input  → fill()      │
          │  capture_text → .inner_text()
          │  assert_text → expect().to_contain_text()
          │  wait_for → wait_for(state="visible")
          └───────────────────────┘
```

---

## Tier 1：语义定位器（主力路径）内部结构

```
target = 'heading "Rs. 500" inside "Blue Top"'
                    │
                    ▼
┌──────────────────────────────────────────────────────────┐
│  _parse_a11y_target(target)   ← semantic.py:118          │
│                                                          │
│  1. _A11Y_SCOPE_RE 匹配 inside "..."  → scope_name       │
│  2. _A11Y_ROLE_TARGET_RE 匹配 role="name" → role, name  │
│                                                          │
│  Output: (role="heading", name="Rs. 500", scope="BT")    │
└──────────────────────┬───────────────────────────────────┘
                       │
          ┌────────────▼────────────┐
          │ 有 role?                │
          └──────┬─────────────────┘
             YES │                    NO
                 ▼                    ▼
    _build_a11y_candidates()    纯文本候选链
    (scoped 或 unscoped)        text_exact → text_fuzzy
        │                       → label → placeholder
        │                       → role_link → role_button
   ┌────▼────┐                  (semantic.py:293-315)
   │ scope?  │
   └────┬────┘
   YES  │  NO
        │   │
        │   └──→ 直接页面级查找
        │        get_by_role(name=)
        │        get_by_text()
        │        (semantic.py:234-267)
        │
        └──→ scoped 查找
             find_scope() → scope.get_by_role(name=)
                            scope.get_by_text()
             (semantic.py:169-232)
```

---

## 典型步骤实战追踪

### Step 1: `goto "https://automationexercise.com/"`

```
runner.py:
  step.action == "goto"
  → page.goto("https://automationexercise.com/")
  ✅ 无定位器调用
```

---

### Step 6: `capture_text` target=`paragraph "Blue Top"`

```
┌─ Step DSL ─────────────────────────────────────────────┐
│ {"action": "capture_text",                              │
│  "target": "paragraph \"Blue Top\"",                    │
│  "context_key": "product_a_name"}                       │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ _resolve_with_confidence_gate()  ── runner.py:521 ────┐
│  require_visible=False  (文本可能不可见)                  │
│  → resolve_with_fallback()                               │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ Tier 0: correction_store.find_active_correction() ─────┐
│  无匹配 → 跳过                                           │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ Tier 1: resolve_semantic_locator()  ── semantic.py ────┐
│                                                          │
│  _parse_a11y_target("paragraph \"Blue Top\"")            │
│    → role="paragraph", name="Blue Top", scope=None      │
│                                                          │
│  _build_a11y_candidates(role="paragraph", name="BT")    │
│    is_text_only = "paragraph" in _TEXT_ONLY_ROLES  ✅    │
│                                                          │
│    候选链 (semantic.py:236-244):                          │
│    ┌──────────────────────────────────────────┐         │
│    │ ① a11y_text_exact                        │         │
│    │    page.get_by_text("Blue Top",exact=True)│ ✅ 命中  │
│    │    → 找到 <p>Blue Top</p>                 │         │
│    │                                           │         │
│    │ 为什么不是 get_by_role("paragraph")?       │         │
│    │ <p> 元素的 accessible name 为空            │         │
│    │ get_by_role("paragraph",name="BT") → 0个  │         │
│    │ 所以 _TEXT_ONLY_ROLES 强制走 text 匹配     │         │
│    └──────────────────────────────────────────┘         │
│                                                          │
│  ✅ 返回 ResolvedLocator(strategy="a11y_text_exact")     │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 执行动作 ──────────────────────────────────────────────┐
│  step.action == "capture_text"                           │
│  captured = resolved.locator.inner_text()  → "Blue Top"  │
│  step_value = "Blue Top"                                 │
│  runtime_context["product_a_name"] = "Blue Top"          │
│                                                          │
│  ✅ Step 6 完成                                          │
└──────────────────────────────────────────────────────────┘
```

---

### Step 7: `capture_text` target=`heading "Rs. 500" inside "Blue Top"`

```
┌─ Step DSL ─────────────────────────────────────────────┐
│ {"action": "capture_text",                              │
│  "target": "heading \"Rs. 500\" inside \"Blue Top\"",   │
│  "context_key": "product_a_price"}                      │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ resolve_semantic_locator()  ── semantic.py ────────────┐
│                                                          │
│  _parse_a11y_target(target)                              │
│    _A11Y_SCOPE_RE:  inside "Blue Top" → scope_name="BT" │
│    _A11Y_ROLE_TARGET_RE: heading "Rs. 500"               │
│    → role="heading", name="Rs. 500", scope="Blue Top"  │
│                                                          │
│  _build_a11y_candidates(role="heading",name="Rs.500",   │
│                         scope_name="Blue Top")           │
│    is_text_only = False  (heading 不是 text-only)        │
│    pw_role = "heading"                                   │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
            ┌────────────────┐
            │ 有 scope_name  │
            └───────┬────────┘
                    │ YES
                    ▼
┌─ 第一步：找 scope ───────────────────────────────────────┐
│                                                          │
│  策略 1 (semantic.py:174-175):                            │
│    product_containers = page.get_by_role("product")      │
│    scope = product_containers.filter(                    │
│        has=page.get_by_text("Blue Top", exact=True)      │
│    )                                                     │
│    → count() == 0  (该网站没有 role="product")           │
│                                                          │
│  策略 2 (semantic.py:178-187):                            │
│    scope_text = page.get_by_text("Blue Top",exact=True)  │
│    → 找到 <p>Blue Top</p>                                │
│                                                          │
│    ★ 关键：parent-climbing ★                             │
│    for _ in range(3):                                    │
│        scope_text = scope_text.locator("xpath=..")       │
│                                                          │
│    第1次 xpath=..  <p> → <div class="product-image">     │
│    第2次 xpath=..  → <div class="single-products">       │
│    第3次 xpath=..  → <div class="productinfo">  ← scope  │
│                                                          │
│    为什么爬 3 层？                                        │
│    paragraph 是 SCOPE 文本的承载元素                       │
│    heading  "Rs. 500" 是 paragraph 的兄弟，不是子孙        │
│    爬上去之后，两者都在同一个 productinfo div 下           │
│                                                          │
│    a11y 树结构:                                           │
│    ┌─ div.productinfo (scope after climbing) ─┐          │
│    │  ├── paragraph "Blue Top"                 │          │
│    │  ├── heading "Rs. 500"    ← SIBLING!      │          │
│    │  └── link "Add to cart"                   │          │
│    └──────────────────────────────────────────┘          │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 第二步：在 scope 内找 target ───────────────────────────┐
│                                                          │
│  候选链 (semantic.py:207-215):                            │
│    role="heading", name="Rs. 500", pw_role="heading"    │
│    is_text_only=False → 走 role 匹配路径                  │
│                                                          │
│    ┌──────────────────────────────────────────┐         │
│    │ ① a11y_scoped_role_exact                 │         │
│    │    scope.get_by_role("heading",           │         │
│    │        name="Rs. 500", exact=True)        │ ✅ 命中  │
│    │    → 找到 <h2>Rs. 500</h2>                │         │
│    │                                           │         │
│    │ ② a11y_scoped_role_fuzzy (未执行)          │         │
│    │ ③ a11y_scoped_text_exact (未执行)          │         │
│    │ ④ a11y_scoped_text_fuzzy (未执行)          │         │
│    └──────────────────────────────────────────┘         │
│                                                          │
│  ✅ ResolvedLocator(strategy="a11y_scoped_role_exact")   │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 执行 ──────────────────────────────────────────────────┐
│  captured = locator.inner_text()  → "Rs. 500"            │
│  runtime_context["product_a_price"] = "Rs. 500"          │
│  ✅ Step 7 完成                                          │
└──────────────────────────────────────────────────────────┘
```

---

### Step 10: `click` target=`link "Add to cart" inside "Blue Top"`

```
┌─ Step DSL ─────────────────────────────────────────────┐
│ {"action": "click",                                     │
│  "target": "link \"Add to cart\" inside \"Blue Top\""}  │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 定位 (同上 scope 查找逻辑) ─────────────────────────────┐
│  role="link", name="Add to cart", scope="Blue Top"      │
│  is_text_only=False, pw_role="link"                     │
│                                                          │
│  scope 找到后，候选链:                                    │
│  ┌──────────────────────────────────────────┐         │
│  │ ① a11y_scoped_role_exact                 │         │
│  │    scope.get_by_role("link",              │         │
│  │        name="Add to cart", exact=True)    │         │
│  │    → 可能有多个同名 link → count() != 1   │         │
│  │                                          │         │
│  │ ② a11y_scoped_role_fuzzy                 │ ✅ 命中  │
│  │    scope.get_by_role("link",              │         │
│  │        name="Add to cart", exact=False)   │         │
│  │    → 在 scope 内找到唯一的 Add to cart     │         │
│  └──────────────────────────────────────────┘         │
│                                                          │
│  ✅ ResolvedLocator(strategy="a11y_scoped_role_fuzzy")   │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 执行 ──────────────────────────────────────────────────┐
│  click_with_precheck(page, locator)                      │
│  → locator.click()                                       │
│  → 弹层出现 (modal with "Continue Shopping"/"View Cart") │
│  ✅ Step 10 完成                                         │
└──────────────────────────────────────────────────────────┘
```

---

### Step 19: `assert_text` target=`cell "Rs. 500" inside "Blue Top"`

```
┌─ Step DSL ─────────────────────────────────────────────┐
│ {"action": "assert_text",                               │
│  "target": "cell \"Rs. 500\" inside \"Blue Top\"",      │
│  "value": "${product_a_price}"}                          │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 变量替换  ── runner.py:_substitute_variables() ────────┐
│  "${product_a_price}" → "Rs. 500"                        │
│  来源: runtime_context["product_a_price"] = Step 7 捕获  │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 定位 ──────────────────────────────────────────────────┐
│                                                          │
│  注意: 这是购物车页 (https://.../view_cart)                │
│  不是商品列表页了！a11y 树结构不同                          │
│                                                          │
│  role="cell", name="Rs. 500", scope="Blue Top"           │
│  is_text_only=False, pw_role="cell"                      │
│                                                          │
│  购物车页 a11y 树 (table 结构):                            │
│  ┌─ row ──────────────────────────────────────┐         │
│  │  ├── cell → link "Blue Top"                 │         │
│  │  ├── cell "Rs. 500"        ← target!        │         │
│  │  ├── cell → button "1"      (数量)           │         │
│  │  └── cell "Rs. 500"         (总价)           │         │
│  └─────────────────────────────────────────────┘        │
│                                                          │
│  scope = 包含 "Blue Top" 的 row (通过 parent-climbing)    │
│                                                          │
│  ┌──────────────────────────────────────────┐         │
│  │ ① a11y_scoped_role_exact                 │ ✅ 命中  │
│  │    scope.get_by_role("cell",              │         │
│  │        name="Rs. 500", exact=True)        │         │
│  │    → 找到 <td>Rs. 500</td>               │         │
│  └──────────────────────────────────────────┘         │
│                                                          │
│  ✅ ResolvedLocator(strategy="a11y_scoped_role_exact")   │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 执行 ──────────────────────────────────────────────────┐
│  pw_expect(locator).to_contain_text("Rs. 500")           │
│  → 元素文本包含 "Rs. 500"                                 │
│  ✅ Step 19 完成                                         │
└──────────────────────────────────────────────────────────┘
```

---

### Step 27 (失败的): `input` target=`button "1" inside "Fancy Green Top"`

```
┌─ Step DSL ─────────────────────────────────────────────┐
│ {"action": "input",                                     │
│  "target": "button \"1\" inside \"Fancy Green Top\"",   │
│  "value": "2",                                          │
│  "trigger": "Enter"}                                    │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 定位 ──────────────────────────────────────────────────┐
│  role="button", name="1", scope="Fancy Green Top"       │
│  _resolve_with_confidence_gate(prefer_input=True)       │
│                                                          │
│  ┌──────────────────────────────────────────┐         │
│  │ ① a11y_scoped_role_exact                 │ ✅ 命中  │
│  │    scope.get_by_role("button",            │         │
│  │        name="1", exact=True)              │         │
│  │    → 找到 <button class="disabled">1</button>       │
│  └──────────────────────────────────────────┘         │
│                                                          │
│  ✅ 定位成功！问题不在定位，在动作！                         │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─ 执行 fill()  ── runner.py (BUG-044 修复后) ────────────┐
│                                                          │
│  // JS evaluate 检查元素是否可填充                         │
│  fillable_info = locator.evaluate(                       │
│      "el => ({tag: el.tagName.toLowerCase(),             │
│        isContentEditable: el.isContentEditable})"        │
│  )                                                       │
│                                                          │
│  tag_name = "button"                                     │
│  is_editable = false                                     │
│                                                          │
│  "button" not in ("input","textarea") and not editable   │
│  → raise RunnerExecutionError(                           │
│      "Input action target resolved to <button>,          │
│       which is not fillable. Only <input>,               │
│       <textarea>, <select>, and [contenteditable]        │
│       elements support the input action.")               │
│                                                          │
│  ❌ Step 27 失败                                         │
│                                                          │
│  根因：购物车页数量是 <button class="disabled">1</button>  │
│  网站根本不支持在购物车页修改数量                            │
└──────────────────────────────────────────────────────────┘
```

---

## 候选策略链总览

```
target 字符串
    │
    ▼
_parse_a11y_target()
    │
    ├── 有 role ──→ _build_a11y_candidates()
    │                   │
    │                   ├── 有 scope ──→ scope 查找 → 候选链:
    │                   │     find_scope():
    │                   │       ① product role 容器
    │                   │       ② 文本 + parent-climbing (xpath=.. ×3)
    │                   │       ③ 模糊文本 + parent-climbing (×2)
    │                   │
    │                   │     scope 内候选:
    │                   │       text-only role → text_exact → text_fuzzy
    │                   │       普通 role     → role_exact → role_fuzzy
    │                   │                       → text_exact → text_fuzzy
    │                   │
    │                   └── 无 scope ──→ 页面级候选:
    │                         text-only → text_exact → text_fuzzy
    │                         普通 role → role_exact → role_fuzzy
    │                                     → text_exact → text_fuzzy
    │
    └── 无 role ──→ 纯文本候选链:
                      placeholder → label → text_exact → text_fuzzy
                      → text_stripped → text_regex
                      → role_link_fuzzy → role_button_fuzzy
```

**角色分类决策表**：

| 角色 | `_A11Y_TO_PLAYWRIGHT_ROLE` | `_TEXT_ONLY_ROLES` | 主策略 | 兜底策略 |
|------|---------------------------|---------------------|--------|---------|
| `button` | ✅ `"button"` | ❌ | `get_by_role("button", name=)` | `get_by_text()` |
| `link` | ✅ `"link"` | ❌ | `get_by_role("link", name=)` | `get_by_text()` |
| `heading` | ✅ `"heading"` | ❌ | `get_by_role("heading", name=)` | `get_by_text()` |
| `cell` | ✅ `"cell"` | ❌ | `get_by_role("cell", name=)` | `get_by_text()` |
| `paragraph` | ✅ `"paragraph"` | ✅ | **`get_by_text()`** (优先!) | `get_by_role("paragraph", name=)` |
| `statictext` | ❌ | ✅ | **`get_by_text()`** (优先!) | — |
| 纯文本 (无 role) | — | — | `get_by_text()` → `placeholder` → `label` | `get_by_role("link"/"button")` |

---

## 完整 Tier 回退链

```
┌─────────────────────────────────────────────────────────┐
│  resolve_with_fallback()  — fallback.py:70              │
│                                                          │
│  Tier 0:  Manual Correction Store                       │
│           correction_store.find_active_correction()      │
│           用户历史手动修正 → CSS/XPath 精确选择器          │
│           ↓ 无匹配                                       │
│                                                          │
│  Tier 0.5: AI Visual Session Cache                      │
│            VLM 结果缓存 (同 URL + 同 target)               │
│            ↓ 无缓存                                       │
│                                                          │
│  Tier 1:  A11y Semantic Locator  ⭐                      │
│           resolve_semantic_locator()                     │
│           候选链逐条尝试 → 第一条 count()==1 的返回        │
│           ↓ 全部候选失败                                  │
│                                                          │
│  Tier 2:  VLM Visual Locate                             │
│           截图 → AI 视觉识别 → 返回 bbox 坐标              │
│           坐标 → page.mouse.click(x,y)                   │
│           ↓ VLM 失败/超时/429                            │
│                                                          │
│  Tier 3:  InterventionNeededError                       │
│           所有策略失败 → 抛出异常                          │
│           等待用户手动提供 CSS/XPath 修正                  │
└─────────────────────────────────────────────────────────┘
```

Session 309 的 26 个成功步骤，全部在 **Tier 1** 就命中了。零 VLM fallback，零人工干预。

---

关联代码文件：
- [`playwright_runner.py:44-78`](backend/app/runners/playwright_runner.py#L44-L78) — `_resolve_with_confidence_gate()`
- [`playwright_runner.py:320-356`](backend/app/runners/playwright_runner.py#L320-L356) — 候选路径执行 + `capture_text`
- [`fallback.py:70-166`](backend/app/locators/fallback.py#L70-L166) — `resolve_with_fallback()` 四层回退链
- [`semantic.py:48-66`](backend/app/locators/semantic.py#L48-L66) — `_A11Y_ROLE_TARGET_RE` + `_A11Y_SCOPE_RE` 正则
- [`semantic.py:69-115`](backend/app/locators/semantic.py#L69-L115) — 角色映射 + `_TEXT_ONLY_ROLES`
- [`semantic.py:118-147`](backend/app/locators/semantic.py#L118-L147) — `_parse_a11y_target()` 解析器
- [`semantic.py:153-267`](backend/app/locators/semantic.py#L153-L267) — `_build_a11y_candidates()` 候选链
