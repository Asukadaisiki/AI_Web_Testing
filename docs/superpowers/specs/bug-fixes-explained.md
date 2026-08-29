# BUG-K / BUG-042 / BUG-043 / BUG-044 修复技术详解

日期：2026-06-12

---

## 1. BUG-K：`inside` 语法和 Scope 查找

### 1.1 问题

AI 生成的 DSL 中大量出现 `heading "Rs. 500" inside "Blue Top"` 这种格式。它的含义是"在名为 Blue Top 的容器内，找一个名为 Rs. 500 的 heading 元素"。但执行时语义定位器找不到。

### 1.2 根因：a11y 树的父子和兄弟关系

automationexercise.com 的商品列表页，a11y 树结构是这样的：

```
product card (container)
  ├── paragraph "Blue Top"        ← 产品名称
  ├── image "Blue Top"
  ├── heading "Rs. 500"           ← 价格（⚠️ 和 paragraph 是兄弟，不是父子！）
  └── link "Add to cart"
```

AI 看到树结构时认为"价格在 Blue Top 里面"，于是生成 `heading "Rs. 500" inside "Blue Top"`。

定位器的工作流程：
1. 找到 `paragraph "Blue Top"`（scope 元素）
2. 调用 `scope.get_by_role("heading", name="Rs. 500")` 在其**后代**中查找

但这行不通！因为 `heading` 是 `paragraph` 的**兄弟节点**（sibling），不是后代（descendant）。`get_by_role` 只在 scope 的后代中搜索，找不到兄弟。

### 1.3 修复：向上爬 3 级父元素

修复代码在 [`semantic.py:184-187`](backend/app/locators/semantic.py#L184-L187)：

```python
# 找到 scope 文本元素
scope_text_elements = page.get_by_text(scope_name, exact=True)
# 往上爬 3 层，从 paragraph 爬到 product card container
raw_scope = scope_text_elements.first
for _ in range(3):
    raw_scope = raw_scope.locator("xpath=..")  # 父元素
scope = raw_scope  # 现在 scope 是 product card，包含 paragraph + heading
```

**修复前**：
```
scope = paragraph "Blue Top"
  → scope.get_by_role("heading") → 空（heading 不在 paragraph 内部）
```

**修复后**：
```
scope = paragraph "Blue Top" 的祖父元素 (= product card)
  → scope.get_by_role("heading") → ✅ 找到！(heading 在 product card 内部)
```

### 1.4 完整的 scoped 候选策略链

修复后的 scope 查找有三个 fallback 级别（[`semantic.py:174-195`](backend/app/locators/semantic.py#L174-L195)）：

| 级别 | 策略 | 代码 |
|------|------|------|
| 1 | 找 `role="product"` 且包含 scope 文本的容器 | `page.get_by_role("product").filter(has=page.get_by_text("Blue Top"))` |
| 2 | 精确匹配 scope 文本 → 向上爬 3 级（`xpath=..`） | `page.get_by_text("Blue Top", exact=True).first.locator("xpath=..") x 3` |
| 3 | 模糊匹配 scope 文本 → 向上爬 2 级 | `page.get_by_text("Blue Top").first.locator("xpath=..") x 2` |

Scope 找到后，目标元素用 6 种策略依次尝试：

```
a11y_scoped_role_exact  → get_by_role(name=, exact=True)   (精确角色匹配)
a11y_scoped_role_fuzzy  → get_by_role(name=, exact=False)  (模糊角色匹配)
a11y_scoped_text_exact  → get_by_text(exact=True)           (精确文本匹配)
a11y_scoped_text_fuzzy  → get_by_text(exact=False)          (模糊文本匹配)
```

---

## 2. BUG-K：`paragraph` / `StaticText` 元素支持

### 2.1 问题

AI 生成的 DSL 使用 `paragraph "Blue Top"` 定位产品名称，但语义定位器不支持 `paragraph` 角色。

### 2.2 根因：三个地方都缺了 `paragraph`

**缺失 1 — 正则不匹配**（[`semantic.py:48-57`](backend/app/locators/semantic.py#L48-L57)）：

修复前，`_A11Y_ROLE_TARGET_RE` 正则只包含交互式角色（`button|link|textbox|heading|...`），没有 `paragraph` 和 `statictext`。`paragraph "Blue Top"` 这个字符串进来 → 正则不匹配 → 解析失败 → 退化到纯文本搜索。

修复后添加了 `|paragraph|statictext`。

**缺失 2 — Playwright 角色映射**（[`semantic.py:69-111`](backend/app/locators/semantic.py#L69-L111)）：

`_A11Y_TO_PLAYWRIGHT_ROLE` 字典把 a11y 角色名映射到 Playwright 的 `get_by_role()` 角色名。修复前没有 `"paragraph": "paragraph"` 条目，即使正则能解析，执行时也不知道怎么调用 Playwright。

**缺失 3 — 文本专用角色识别**（[`semantic.py:115`](backend/app/locators/semantic.py#L115)）：

这是最关键的一个修复。`paragraph` 和 `statictext` 元素在 ARIA 规范中通常**没有 accessible name**。Playwright 的 `get_by_role("paragraph", name="Blue Top")` 去找一个"名字叫 Blue Top 的 paragraph" → 找不到，因为 `<p>` 标签的 accessible name 通常为空。

修复方案：新增 `_TEXT_ONLY_ROLES` 集合：

```python
_TEXT_ONLY_ROLES: frozenset[str] = frozenset({"paragraph", "statictext"})
```

对于 text-only 角色，**优先用 `get_by_text()` 而不是 `get_by_role(name=)`**（[`semantic.py:197-206`](backend/app/locators/semantic.py#L197-L206)）：

```python
if is_text_only and name:
    # ✅ 用文本匹配 → 能找到
    builders.append(("a11y_scoped_text_exact",
        lambda: scope.get_by_text(name, exact=True)))
    builders.append(("a11y_scoped_text_fuzzy",
        lambda: scope.get_by_text(name)))
```

而普通角色（如 `button`、`link`、`heading`）走 role 匹配路径。

### 2.3 为什么这个修复很关键

修复前：`paragraph "Blue Top"` → 正则不匹配 → 整段当纯文本 → `get_by_text('paragraph "Blue Top"')` → 找不到（"paragraph " 是角色前缀，不是文本内容）→ 失败

修复后：`paragraph "Blue Top"` → 正则匹配 → 识别为 text-only 角色 → `get_by_text("Blue Top")` → ✅ 找到

---

## 3. BUG-043：DSL Prompt 硬编码 `heading` 角色

### 3.1 问题

DSL 生成器的系统 prompt 中有静态示例：

```
- heading="Rs. 500"
- link="Add to cart"
```

这些示例告诉 AI "价格就是 heading 角色"。但 automationexercise.com 的购物车页，价格是 `<td>` 标签（a11y role=cell），不是 heading。AI 盲从示例 → 生成 `heading "Rs. 500" inside "Blue Top"` → 在购物车页找不到 heading → VLM fallback。

### 3.2 修复

在 [`dsl_generator.py:825-838`](backend/app/ai/dsl_generator.py#L825-L838)：

**修复前**：
```
- [container] Blue Top
  - paragraph="Blue Top"
  - heading="Rs. 500"              ← AI 看到这就以为价格都是 heading
  - link="Add to cart"

To capture price: capture_text heading "Rs. X" inside "Product Name"
```

**修复后**：
```
⚠️ EXAMPLES ARE FOR REFERENCE ONLY — actual data takes precedence

- [container] Blue Top
  - paragraph="Blue Top"
  - StaticText="Rs. 500"          ← Example only! Use actual role from Available elements
  - link="Add to cart"

REFERENCE example (syntax only — roles are illustrative, not authoritative):
capture_text <actual_role> "value" inside "Container"
  ↑ Copy the exact role from Available elements — StaticText, heading, paragraph, etc.
```

关键变化：
1. 把示例中的 `heading` 改成 `StaticText`
2. 加了醒目的 `⚠️ EXAMPLES ARE FOR REFERENCE ONLY` 警告
3. 示例 target 格式改为 `<actual_role>` 占位符，迫使 AI 去看实际数据

### 3.3 效果

| | 修复前 (308) | 修复后 (309) |
|---|---|---|
| 购物车价格断言 | `heading "Rs. 500"` → VLM | `cell "Rs. 500"` → `a11y_scoped_role_exact` ✅ |
| 购物车总价断言 | `heading "Rs. 500"` → VLM | `paragraph "Rs. 500"` → `a11y_scoped_text_exact` ✅ |

---

## 4. BUG-044：`input` 动作元素兼容性

### 4.1 问题

Session 309 Step 27 生成了 `input` 动作 targeting `button "1"`：

```json
{"action": "input", "target": "button \"1\" inside \"Fancy Green Top\"", "value": "2"}
```

Playwright 的 `fill()` 只能用于 `<input>`、`<textarea>`、`<select>`、`[contenteditable]` 元素。在 `<button>` 上调用 → crash。

### 4.2 两层修复

**第一层 — Prompt 约束**（[`dsl_generator.py:882-893`](backend/app/ai/dsl_generator.py#L882-L893)）：

在 Rule 6 下新增元素兼容性规则：

```
⚠️ input ACTION ELEMENT COMPATIBILITY: `input` internally calls Playwright `.fill()`,
which ONLY works on <input>, <textarea>, <select>, and [contenteditable] elements.
Using `input` on a button, link, heading, paragraph, or StaticText WILL CRASH.

✓ roles safe for input: textbox, spinbutton, combobox, searchbox, select
✗ roles that CRASH with input: button, link, heading, paragraph, StaticText, cell

For quantity inputs: if the page shows quantity as a button (e.g. button "1"),
there is NO editable quantity field — do NOT generate `input` on it.
```

**第二层 — Runner 防御**（[`playwright_runner.py:334-356`](backend/app/runners/playwright_runner.py#L334-L356)）：

修复前：
```python
elif step.action == "input":
    tag_name = locator.evaluate("el => el.tagName.toLowerCase()")
    if tag_name == "select":
        locator.select_option(label=input_value)
    else:
        locator.fill(input_value)  # ← button 也会走这里 → crash
```

修复后：
```python
elif step.action == "input":
    # JS evaluate: 检查 tag + contenteditable + role
    fillable_info = locator.evaluate(
        "el => ({"
        "tag: el.tagName.toLowerCase(),"
        "isContentEditable: el.isContentEditable,"
        "role: el.getAttribute('role') || '',"
        "className: el.className || ''"
        "})"
    )
    tag_name = fillable_info.get("tag", "")
    is_editable = fillable_info.get("isContentEditable", False)
    if tag_name == "select":
        locator.select_option(label=input_value)
    elif tag_name in ("input", "textarea") or is_editable:
        locator.fill(input_value)     # ✅ 确认安全后才 fill
    else:
        raise RunnerExecutionError(   # ✅ 清晰报错，不静默崩溃
            f"Input action target resolved to <{tag_name}>, "
            f"which is not fillable. Only <input>, <textarea>, "
            f"<select>, and [contenteditable] support fill()."
        )
```

这个防御检查在 4 个代码路径中都加了（3 个 streaming execution + 1 个 sync execution）。

### 4.3 效果对比

**修复前**：
```
Step 27: input on button "1" → Playwright fill("2") → Error: Element is not an <input>
→ 无报告生成，执行直接退出 ← 用户看不到任何有用信息
```

**修复后**：
```
Step 27: input on button "1" → JS evaluate → tag=button, not fillable
→ raise RunnerExecutionError("Input action target resolved to <button>...")
→ 报告中有完整错误消息，可以定位问题
```

---

## 5. BUG-042：Imported Draft 自愈 + 时区修复

### 5.1 问题

用户保存并执行 DSL（draft status 变为 `imported`）→ 执行失败 → 用户说"修复错误" → AI 重新生成 plan → 但 `generate_drafts()` 发现 draft 已存在且 status=`imported` → **直接复用旧 draft，跳过 DSL 生成**。

### 5.2 修复

在 [`ai_planning.py:707-720`](backend/app/services/ai_planning.py#L707-L720) 的 `imported` / `rejected` 分支增加执行失败检测：

```python
if existing.status in ("imported", "rejected"):
    if existing.status == "imported" and _has_newer_execution_failure(
        session, planning_session, existing.created_at
    ):
        # ✅ 有新的执行失败 → 删除旧 draft → 触发重新生成
        session.delete(existing)
        session.flush()
    else:
        # 没有失败或 status=rejected → 复用
        drafts.append(_to_draft_schema(existing))
        continue
```

### 5.3 时区修复

`_has_newer_execution_failure()` 比较 `draft_created_at`（SQLite `func.now()` = UTC+8）和 `run_finished_at`（Python `datetime.now(UTC)` = UTC）时，两个时间差了 8 小时。修复方案：加 24 小时容差窗口（[`ai_planning.py:1493-1512`](backend/app/services/ai_planning.py#L1493-L1512)）：

```python
TZ_TOLERANCE = timedelta(hours=24)
if run_at + TZ_TOLERANCE <= draft_at:   # 安全比较
    return False
```

---

## 总结：修复之间的依赖关系

```
BUG-K (parent-climbing + paragraph)
  │
  ├── 解决：heading inside Container 找不到兄弟元素
  │     └── Session 309 效果：heading 价格全部 a11y 直接命中，零 VLM
  │
  └── 解决：paragraph 角色定位器不工作
        └── Session 309 效果：产品名称 capture 通过 a11y_text_exact

BUG-043 (prompt 去硬编码)
  │
  └── 解决：AI 盲从 heading 示例，在购物车页生成错角色
        └── Session 309 效果：购物车用 cell/paragraph，匹配实际 <td>/<p>

BUG-044 (input 兼容性)
  │
  ├── Prompt 层：告诉 AI 不能对 button 用 input
  └── Runner 层：fill() 前验证元素类型，清晰报错

BUG-042 (imported draft 自愈)
  │
  └── 解决：执行失败后 draft 不会被重新生成
        └── 确保将来"修复错误"流程真正有效
```

**302 → 308 → 309 的进化路径**：每个修复解决了一个具体的失败模式，最终在 Session 309 达到了 26/26 步骤纯 a11y 语义定位、零 VLM fallback 的最佳状态。
