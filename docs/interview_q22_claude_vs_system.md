# Q21: Claude 一句话就能生成 Playwright 脚本，你的系统意义在哪？

## 面试官追问

我直接用 Claude Desktop 或者 Claude Code，给它一句话："帮我写一个 Playwright 脚本，在 automationexercise.com 上把品牌筛选加购物车的流程跑一遍。"它几秒钟就能生成一个完整的 .spec.ts 文件，我直接 `npx playwright test` 就能跑。

你花了几个月做的这个 AI Web Testing Agent Runtime，生成的是一个 JSON DSL，还要过 Preflight、还要过 Playwright Runtime、还有 fallback 链、还有 85+ 个 Bug 需要修——结果和我一句话让 Claude 写脚本是一样的。你的系统意义是什么？

---

## 一、先承认事实

Claude 一句话就能生成一个能跑的 Playwright 脚本。如果你的需求是"跑一次这个流程"，Claude 生成脚本确实是更好的选择——零开发成本、几秒钟出结果。

---

## 二、用同一个测试场景对比

**测试需求**：在 automationexercise.com 上，按品牌 Polo 筛选商品，把两件商品加入购物车，验证购物车中名称、单价、数量、总价是否正确。

### Claude 生成的脚本长这样（大致）

```typescript
test('brand filter cart', async ({ page }) => {
  await page.goto('https://automationexercise.com/');
  await page.click('a[href="/products"]');           // ← CSS 选择器
  await page.click('a[href="/brand_products/Polo"]'); // ← CSS 选择器
  const price1 = await page.locator('.productinfo h2').first().textContent();
  await page.locator('.productinfo a[data-product-id="1"]').first().click();
  await page.click('button:has-text("Continue Shopping")');
  const price2 = await page.locator('.productinfo h2').nth(1).textContent();
  await page.locator('.productinfo a[data-product-id="2"]').first().click();
  await page.click('a:has-text("View Cart")');
  // 断言...
  expect(await page.locator('.cart_price').first().textContent()).toBe(price1);
});
```

### 我的系统生成的 DSL 长这样（实际代码）

```json
{
  "action": "capture_text",
  "target": "heading=\"Rs. 500\" inside \"Blue Top\"",
  "context_key": "product_a_price"
}
```

```json
{
  "action": "click",
  "target": "link=\"Add to cart\" inside \"Blue Top\""
}
```

```json
{
  "action": "assert_text",
  "target": "cell=\"Rs. 500\"",
  "value": "Rs. 500"
}
```

**关键区别**：Claude 用的是 CSS 选择器（`.productinfo a[data-product-id="1"]`），我用的是 **A11y 语义定位**（`heading="Rs. 500" inside "Blue Top"`）。

---

## 三、具体例子：当 UI 改版时会发生什么

### 场景：automationexercise.com 改版了

假设网站做了以下改动：
1. `Add to cart` 按钮的 `data-product-id` 属性去掉了
2. 价格的 `<h2>` 标签改成了 `<span class="price">`
3. 品牌链接从 `href="/brand_products/Polo"` 改成 `href="/brands/polo"`

**Claude 生成的脚本**：
```typescript
// 第 1 个改动就挂了
await page.locator('.productinfo a[data-product-id="1"]').first().click();
// Error: locator resolved to 0 elements
```

你需要：打开 Claude → 描述新的页面结构 → 重新生成脚本 → 复制粘贴 → 重新运行。**每次改版都重复**。

**我的系统**：

执行 `link="Add to cart" inside "Blue Top"` 时，Preflight 会这样工作（实际代码 `locator_preflight.py:347`）：

```python
# apply_preflight_to_dsl() 的实际逻辑：
# 1. 解析 target → 找到 scope "Blue Top" 对应的产品卡片容器
# 2. 在容器的子节点中找 role=link, name 包含 "Add to cart" 的元素
# 3. 生成多个候选，按 pre_score 排序：
#    - verified_role (pre_score=1.0): 之前人工验证过的选择器
#    - a11y_scoped_role_exact (pre_score=0.95): A11y 精确匹配
#    - a11y_scoped_role_fuzzy (pre_score=0.85): A11y 模糊匹配
#    - text (pre_score=0.70): 纯文本匹配
```

即使 `data-product-id` 属性没了，只要页面上还有一个 `role=link, name="Add to cart"` 的元素在 "Blue Top" 这个产品卡片里，**Preflight 仍然能匹配到**。

如果 A11y 节点也匹配不到，执行时 fallback chain 启动（实际代码 `fallback.py:70`）：

```
Tier 0:   人工修正记录（URL 通用化 + casefold 归一化匹配）
Tier 0.5: AI 视觉缓存（LRU 128 条，之前成功定位过的结果）
Tier 1:   A11y 语义定位（role="name" 格式解析）
Tier 2:   VLM 视觉定位（截图发给视觉模型，返回坐标）
Tier 3:   坐标点击（用 VLM 返回的 bbox 坐标直接点）
Tier 4:   报错，请求人工干预
```

**重点**：CSS 选择器是"精确匹配"——改一个 class 就挂。A11y 语义定位是"模糊匹配"——只要元素的无障碍角色和名称没变，就能定位到。

---

## 四、具体例子：当页面有遮挡物时

### 场景：点击 "Add to cart" 时，一个 cookie banner 挡住了按钮

**Claude 生成的脚本**：
```typescript
await page.click('button:has-text("Add to cart")');
// Error: element intercepts pointer events
```

脚本直接挂了。你需要手动加 `await page.click('.cookie-banner button:has-text("Accept")')`。

**我的系统**（实际代码 `click_preprocessor.py`）：

```python
# 5 步降级链：
# wait → dismiss → avoid → force → remove

# 自动诊断遮挡物类型：
if role == 'dialog' or 'modal' in cls:
    overlayType = 'modal'; dismissible = True
elif 'toast' in cls or 'notification' in cls:
    overlayType = 'toast'; dismissible = True
elif 'cookie' in cls or 'banner' in cls or 'consent' in cls:
    overlayType = 'cookie_banner'; dismissible = True
elif 'loading' in cls or 'spinner' in cls:
    overlayType = 'loading'; dismissible = False  # 不移除，等待
```

系统自动识别出是 cookie banner → 自动点击关闭 → 重新尝试点击目标按钮。**不需要人工干预**。

---

## 五、具体例子：当同一个页面有多个相同元素时

### 场景：页面上有 3 个 "Add to cart" 按钮，你需要点的是 "Blue Top" 那个

**Claude 生成的脚本**：
```typescript
// 可能生成这种
await page.locator('a:has-text("Add to cart")').first().click();
// 点到了第一个，但不一定是 Blue Top
```

或者更精确的：
```typescript
// 需要 Claude 理解页面结构，生成嵌套选择器
await page.locator('.productinfo:has-text("Blue Top") a:has-text("Add to cart")').click();
```

但这个选择器高度依赖 DOM 结构——`.productinfo` 这个 class 改了就挂。

**我的系统**（实际 DSL）：
```json
{
  "action": "click",
  "target": "link=\"Add to cart\" inside \"Blue Top\""
}
```

`inside "Blue Top"` 是 **作用域定位**——先找到包含 "Blue Top" 文本的容器，再在容器内找 `role=link, name="Add to cart"`。这个定位方式不依赖任何 CSS class，只依赖页面的文本内容和无障碍树结构。

---

## 六、具体例子：当测试结果需要可信时

### 场景：验证购物车总价是 Rs. 1400

**Claude 生成的脚本**：
```typescript
const total = await page.locator('.cart_total_price').textContent();
expect(total).toContain('1400');
```

问题：`.cart_total_price` 可能匹配到一个空元素、一个隐藏元素、或者一个被脚本动态修改的元素。Playwright 能取到文本 ≠ 用户能看到文本。

**我的系统**（实际 DSL）：
```json
{
  "action": "assert_text",
  "target": "cell=\"Rs. 1400\"",
  "value": "Rs. 1400"
}
```

Preflight 验证时会检查（实际代码 `locator_preflight.py`）：
- 元素是否在 viewport 内（不在 → low confidence）
- 元素是否被遮挡（被遮挡 → low confidence）
- 元素是否 disabled（disabled → low confidence）
- A11y 节点是否可见（hidden → 跳过）

而且断言的是 **A11y 语义层的文本**（`cell="Rs. 1400"`），不是 DOM 的 innerText——过滤了脚本/样式噪音。

---

## 七、具体例子：当人工修过一次后

### 场景：你第一次跑测试，"Add to cart" 按钮定位失败了，你手动指定了正确选择器

**Claude 生成的脚本**：你需要重新生成整个脚本，或者手动修改 .spec.ts 文件。

**我的系统**（实际代码 `corrections.py`）：

```python
# 人工修正记录存入数据库：
# - page_url_pattern: "automationexercise.com"（URL 通用化）
# - normalized_target_description: "link=\"add to cart\" inside \"blue top\""（casefold 归一化）
# - correction_type: "css" / "xpath" / "test_id"
# - correction_value: ".product-card:nth-child(3) .add-to-cart"

# 下次执行时，Tier 0 自动匹配：
correction = correction_store.find_active_correction(
    page_url=page_url,
    target_description=target,  # 自动归一化后匹配
)
```

关键设计：
- **URL 通用化**：`https://automationexercise.com/products?page=2` → `automationexercise.com`，同站不同页面共享修正记录
- **casefold 归一化**：`link="Add to Cart" inside "Blue Top"` → `link="add to cart" inside "blue top"`，大小写不敏感匹配
- **自动失效**：连续失败 3 次自动停用（`MAX_CONSECUTIVE_FAILURES = 3`），避免过期修正干扰
- **成功计数**：`verified_count` 越高优先级越高

**修一次，永久生效**——不需要每次改版都重新生成脚本。

---

## 八、总结：Claude 生成脚本 vs 我的系统

| 维度 | Claude 生成脚本 | 我的系统 |
|------|----------------|---------|
| **定位方式** | CSS/XPath（精确匹配，改一个 class 就挂） | A11y 语义 + 5 级降级（模糊匹配，改 class 不一定挂） |
| **遮挡处理** | 无（脚本挂了就挂了） | click_preprocessor 自动诊断 + 5 步降级 |
| **重复元素** | 靠人描述或猜测选择器 | `inside "scope"` 作用域定位，不依赖 DOM 结构 |
| **结果验证** | `innerText` 取值（含脚本/样式噪音） | A11y 语义层文本 + Preflight 可操作性检查 |
| **人工修正** | 每次重新生成 | 修一次，Tier 0 永久生效 |
| **维护成本** | 200 用例 × 每次改版重新生成 | 共享 fallback chain，Tier 0 自动匹配 |
| **成本** | ~$6-12/200 用例（Claude API） | ~¥10-20/200 用例（DeepSeek，估算） |

**一句话**：Claude 生成脚本是"写一次，挂了重写"；我的系统是"写一次，自动适应"。

---

## 九、如果面试官追问"那你也有 85 个 Bug"

是的，85 个 Bug 说明系统还不够成熟。但这些 Bug 是**可以自动修复的**（单元测试发现 → 修复 → 回归测试验证）。

Claude 生成的脚本也有 Bug——每次 UI 变化都是一个"Bug"，而且是**不可自动修复的 Bug**，需要人工重新生成。

---

## 十、如果面试官追问"那 Claude 生成脚本也可以加 fallback"

是的，理论上可以。但你需要在 prompt 里描述：
1. A11y 树采集器的实现细节
2. Preflight 验证规则
3. 5 级 fallback chain 的逻辑
4. 人工修正持久化的数据库 schema
5. click_preprocessor 的遮挡诊断逻辑

这相当于用 Claude 做代码生成工具，而不是测试工具。我的系统把这些能力**固化在代码里**，代码是确定性的，prompt 是非确定性的。

---

## 十一、诚实说明：系统的不足

1. **不能自动修改 DSL**：Preflight 能检测到 mismatch，但不能自动更新 DSL 里的 target。需要人工触发重新生成或手动修正。
2. **成本数据是估算的**：端到端用例跑过 0 个，token 追踪只记日志没持久化。
3. **85 个 Bug**：70% 代码缺陷可修，10% Prompt 缺陷需更好的 engineering，20% 混合问题。

如果要做到全自动自愈，需要加一个反馈闭环：执行失败 → 自动重新探索 → 重新生成 DSL → 自动更新。这个闭环技术上可行（每个环节的代码都有），但目前没有串起来。设计上刻意选择半自动，因为自动修改 DSL 存在风险——LLM 可能把正确的 target 改错。
