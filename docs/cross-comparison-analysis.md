# 横向对比分析：Claude Code vs AI_Web_Testing 执行 test_brand_filter_cart

> 日期：2026-06-03
> 来源：对话总结

---

## 一、对比对象

| 项目 | 执行方式 | 模型 | 定位策略 |
|------|---------|------|---------|
| `testbrand(mimov2.5pro)` | Claude Code Agent → Selenium + Python 脚本 | Claude 前端模型 | 直接写选择器代码 |
| `testbrand(dsv4pro)` | Claude Code Agent → 3 轮探索 + Playwright + Node.js 脚本 | DeepSeek v4pro | 递增探索 → 精确选择器 |
| `AI_Web_Testing` | 自研 ReAct Agent → DSL → Playwright Runner | 配置的模型 | 5 层定位器回退链（A11y-based） |

---

## 二、执行结果差距

### 量化对比

| 指标 | mimov2.5pro | dsv4pro | AI_Web_Testing |
|------|-----------|---------|----------------|
| 错误次数 | **0** | **4**（会话内全部解决） | **62+ bug 记录** |
| 解决时间 | 分钟级 | 分钟级 | 数周迭代 |
| 架构迭代 | 0 | 0 | 20+ 轮 |
| 最终通过率 | 12/12 (100%) | 12/12 (100%) | 21/21 (100%，经多次修复) |
| 探索轮次 | 1（直接写脚本） | 3（递增式探索） | 数十轮（反复探索+修复） |

### dsv4pro 的 4 个错误为何快速解决？

全部是**代码层面的问题**，修复方式极其直接：

1. **页面加载超时** → 改 `networkidle` 为 `domcontentloaded`（1 行）
2. **价格解析 NaN** → 修正正则表达式（1 行）
3. **Add to cart 严格模式** → 加 `.first()` （1 行）
4. **购物车数量不可编辑** → `page.evaluate()` 操作 DOM（几行代码）

每一个都是**直接改代码 → 立即运行验证**的循环。

---

## 三、核心差异分析

### 3.1 架构层次

```
Claude Code:
  需求 → 模型推理 → [写代码 → 运行 → 看结果 → 修复] → ✅
         ↑______________直接循环______________↑
         1 层转换

AI_Web_Testing:
  需求 → ReAct Agent → explore 工具 → Plan → DSL Generator → DSL 文本
       → Playwright Runner → locator fallback chain → 执行结果
       → 结果分析 → （失败则回退到某一步）
       4-5 层转换
```

### 3.2 工具能力

| 能力 | Claude Code | AI_Web_Testing |
|------|------------|----------------|
| 运行任意命令 | ✅ BashTool | ❌ |
| 写/读/编辑文件 | ✅ FileWrite/Read/Edit | ❌ |
| 网页抓取 | ✅ WebFetch | ❌ |
| 代码搜索 | ✅ GrepTool/GlobTool | ❌ |
| 页面探索 | 通过 BashTool + Playwright 脚本 | explore_page / explore_flow |
| **可用工具总数** | **30+** | **~15** |

### 3.3 探索页面的方式

**Claude Code 没有专门的浏览器探索工具。** 它的"探索"是：

1. **WebFetch**：HTTP GET → HTML → Markdown → 小模型提取信息（无法执行 JS，看不到动态内容）
2. **BashTool**：模型自己写 Playwright 脚本 → `node explore.js` → 读文本输出 → 决定下一步

dsv4pro 的 3 轮递增探索：

```javascript
// Round 1: explore.js — 广撒网，理解全局结构
const navLinks = await page.locator('nav a, .nav a, header a').all();
// 输出: Found 51 nav links, Brands: (6)Polo, (5)Madame...

// Round 2: explore-deep.js — 深入单个组件的 DOM
const nameSelectors = ['.productinfo p', '.productinfo h2', ...]; // 9 个候选全测
// 输出: ".productinfo p" -> "Blue Top" ✅

// Round 3: explore-cart.js — 专门研究购物车
for (const sel of ['.cart_quantity input', '.cart_quantity button', 'input', ...]) {
  // 逐个测试每个选择器
}
// 发现: 数量是 <button class="disabled">1</button> 不是 <input>
```

**AI_Web_Testing 的探索方式**：

- `explore_page`：CDP Accessibility.getFullAXTree → 黑名单过滤 → DOM 解析 → 标准化 JSON
- `explore_flow`：导航多个页面 → 采集各状态 A11y 节点

对比：

| 维度 | Claude Code | AI_Web_Testing |
|------|------------|----------------|
| 探索手段 | Playwright 脚本（任意 JS 代码） | CDP Accessibility API |
| 看到什么 | 完整 DOM（innerHTML/CSS/所有属性） | 仅 A11y Tree（role + name） |
| 信息密度 | 高（可打印任何 DOM 细节） | 低（受限于无障碍树） |
| 灵活度 | 无限（可写任意选择器测试） | 受限（只能用 A11y 标识） |
| 输出格式 | 人类可读文本 | 结构化 JSON |
| 迭代方式 | 模型读输出 → 写新脚本 → 继续探索 | 模型调工具 → 拿 JSON → 调更多工具 |

### 3.4 选择元素的策略

**Claude Code**：枚举候选选择器 → 逐个测试 → 看哪个返回正确内容。像有经验的开发者在 DevTools Console 里手动尝试。

**AI_Web_Testing**：把 A11y 树"翻译"给 LLM → LLM 选一个 `role="name"` 标识 → Runner 用 5 层回退链匹配。问题是：
- A11y name 字段可能不可靠（CSS `text-transform` 改变文本）
- `ignored` 元素在树中不存在（如 Quantity 输入框）
- LLM 输出和 Runner 匹配策略之间存在**语义鸿沟**

### 3.5 CSS 选择器基础 — Claude Code 用什么定位元素？

Claude Code 使用的是 **CSS 选择器**，这是 Playwright/Selenium 用来在 HTML 中找到元素的"地址"。

#### 标签选择器

```javascript
nav a
//    ↑ 找到所有 <a> 标签
// ↑ 父元素必须是 <nav> 标签
```

```html
<nav>
  <a href="/products">Products</a>  ← 匹配 ✅
  <a href="/login">Login</a>        ← 匹配 ✅
</nav>
<div>
  <a href="/about">About</a>        ← 不匹配 ❌ (父元素是 div)
</div>
```

#### Class 选择器（以 `.` 开头）

```javascript
.brands-name a
// ↑ 找到 class="brands-name" 的元素内的所有 <a>
```

```html
<div class="brands-name">
  <a href="/brand/Polo">Polo</a>    ← 匹配 ✅
  <a href="/brand/H&M">H&M</a>     ← 匹配 ✅
</div>
<div class="other-section">
  <a href="/brand/Gucci">Gucci</a>  ← 不匹配 ❌ (父元素 class 不是 brands-name)
</div>
```

#### 组合选择器（空格 = 后代关系）

```javascript
.features_items .product-image-wrapper
// ↑ 找到 class="features_items" 的元素内
//              ↓ 再找 class="product-image-wrapper" 的后代元素
```

#### 多选择器（逗号 = 或）

```javascript
page.locator('.brands-name li a, .brands-name a, .panel-body a')
//            ↑ 选择器1          ↑ 选择器2        ↑ 选择器3
//            三者任意一个匹配即可
```

#### 属性选择器（中括号）

```javascript
a[href="/products"]
// ↑ 找到所有 <a> 标签中 href="/products" 的那个
```

#### ID 选择器（以 `#` 开头）

```javascript
'#cart_info_table'
// ↑ 找到 id="cart_info_table" 的元素
```

#### 过滤器 `.filter({ hasText })`

```javascript
page.locator('.brands-name a').filter({ hasText: 'Polo' })
// ↑ 先找到所有 .brands-name a
//              ↓ 再过滤出文本包含 "Polo" 的那个
```

#### Automation Exercise 的实际 DOM 结构

根据 dsv4pro 的探索结果，网站的 HTML 结构大致是：

```html
<header>
  <nav>
    <ul class="nav">
      <li><a href="/">Home</a></li>
      <li><a href="/products">Products</a></li>
      <li><a href="/view_cart">Cart</a></li>
    </ul>
  </nav>
</header>

<div class="features_items">
  <!-- 6 个商品卡片 -->
  <div class="col-sm-4">
    <div class="product-image-wrapper">
      <div class="productinfo text-center">
        <p>Blue Top</p>                      ← 产品名称
        <h2>Rs. 500</h2>                     ← 价格
      </div>
      <div class="product-overlay">
        <a class="add-to-cart" data-product-id="1">
          Add to cart
        </a>
      </div>
    </div>
  </div>
  <!-- ... 更多商品 ... -->
</div>

<div class="brands-name">
  <ul>
    <li><a href="/brand_products/Polo">(6)Polo</a></li>
    <li><a href="/brand_products/H&M">(5)H&M</a></li>
  </ul>
</div>

<!-- 购物车 Modal -->
<div class="modal" id="cartModal">
  <div class="modal-content">
    <button>Continue Shopping</button>
    <a href="/view_cart">View Cart</a>
  </div>
</div>
```

#### Claude Code 的选择器怎么对应到这些 DOM？

```javascript
// 1. 找导航链接
'nav a'              → <nav> 内的 <a> → "Home", "Products", "Cart"...
'.nav a'             → class="nav" 的元素内的 <a> → 同上

// 2. 找品牌链接
'.brands-name a'     → class="brands-name" 内的 <a> → "(6)Polo", "(5)H&M"...

// 3. 找商品卡片
'.features_items .product-image-wrapper'
  → class="features_items" 内的 class="product-image-wrapper" 的元素

// 4. 找产品名称
'.productinfo p'     → class="productinfo" 内的 <p> → "Blue Top"

// 5. 找价格
'.productinfo h2'    → class="productinfo" 内的 <h2> → "Rs. 500"

// 6. 找加车按钮
'.add-to-cart'       → class="add-to-cart" 的元素
'a[data-product-id]' → <a> 标签中带 data-product-id 属性的元素

// 7. 找购物车表格
'#cart_info_table'   → id="cart_info_table" 的元素

// 8. 找弹窗按钮
'.modal-content button' → class="modal-content" 内的 <button>

// 9. 按文本过滤
page.locator('button').filter({ hasText: 'Continue Shopping' })
  → 找到所有 <button>，过滤出文本包含 "Continue Shopping" 的
```

#### CSS 选择器 vs A11y 标识对比

| CSS 选择器 | A11y 标识 | 区别 |
|-----------|----------|------|
| `.productinfo p` | `paragraph="Blue Top"` | CSS 用 class 定位，A11y 用 role+name |
| `.add-to-cart` | `link="Add to cart"` | CSS 用 class，A11y 用 role+name |
| `#cart_info_table` | `table=""` | CSS 用 id，A11y 用 role（丢失了 id 信息）|
| `.brands-name a` | `link="(6)Polo"` | CSS 用 class 定位容器，A11y 只看到链接文本 |

**CSS 选择器更精确**（可以用 class、id、属性区分），**A11y 更语义化**（只看 role 和 name，不关心 class/id）。

### 3.6 Claude Code 怎么知道用什么选择器？— 试探策略

**它不知道。它是试探出来的。**

看 `explore-deep.js` 第 63-82 行：

```javascript
// 模型不知道产品名称用什么选择器，所以写了 9 个候选
const nameSelectors = [
  '.productinfo p',           // 候选 1
  '.productinfo h2',          // 候选 2
  '.product-image-wrapper p', // 候选 3
  '.product-image-wrapper h2',// 候选 4
  '.single-products p',       // 候选 5
  '.single-products h2',      // 候选 6
  '.product-overlay p',       // 候选 7
  '.overlay-content p',       // 候选 8
  '.productinfo.text-center p',// 候选 9
];

// 逐个测试，看哪个能找到内容
for (const sel of nameSelectors) {
  const el = firstProductCard.locator(sel);
  const count = await el.count();
  if (count > 0) {
    const text = await el.first().textContent();
    console.log(`  "${sel}" -> "${text?.trim()}"`);
  }
}
```

脚本运行后输出：

```
  ".productinfo p" -> "Blue Top"          ← 找到了！
  ".productinfo h2" -> "Rs. 500"          ← 找到了！
  ".product-image-wrapper p" -> "Blue Top" ← 也找到了
```

模型读到这个输出后，就知道：
- 产品名称用 `.productinfo p` ✅
- 价格用 `.productinfo h2` ✅

#### 本质：模型是"猜"的，然后用代码验证

```
模型的知识储备:
  - ".productinfo" 是常见的产品信息容器 class 名
  - "p" 标签通常放文本（名称）
  - "h2" 标签通常放价格（大号字体）
  - ".product-image-wrapper" 也是常见的产品卡片 class
  - ".single-products" 也可能是产品容器
  - ".product-overlay" 可能是 hover 层

模型的策略:
  "我不知道这个网站用的是哪个，但我大概知道有这些可能，
   全部试一遍，看哪个返回正确内容。"
```

这就像一个有经验的前端开发者，看到一个不熟悉的网站，会：
1. 打开 DevTools
2. 右键检查元素
3. 试几个可能的选择器
4. 看哪个能找到目标元素

**Claude Code 把这个过程自动化了——模型写代码试探，而不是凭空猜测。**

#### 为什么这些候选选择器是"合理"的？

因为这些 class 名是**行业惯例**，模型在训练数据中见过无数次：

```javascript
'.productinfo'     // 电商网站几乎都用这个 class 放产品信息
'.product-image'   // 产品图片容器的标准命名
'.add-to-cart'     // 加车按钮的通用 class
'.modal'           // 弹窗的标准 class
'#cart_info_table' // id 选择器，更精确
'.brands-name'     // 品牌列表的语义化命名
```

模型不是随机猜的，而是基于**训练数据中的模式**：
- 见过 1000 个电商网站，900 个用 `.productinfo`
- 见过 1000 个弹窗，950 个用 `.modal`
- 见过 1000 个加车按钮，800 个用 `.add-to-cart`

### 3.7 DOM 信息过载问题 — 分层探索策略

#### 问题：全量 DOM 信息过多

你一开始全量提取 DOM 信息，发现信息过载：

```python
# 全量提取 DOM 信息 — 信息爆炸
all_elements = page.query_selector_all('*')
for el in all_elements:
    # 每个元素的 tag、class、id、text、children...
    # 一个页面可能有上千个元素
```

#### Claude Code 的解决方案：分层、分块、分主题

Claude Code 的探索是**每次只回答一个具体问题**：

```
Round 1 (explore.js):
  问题: "网站有哪些页面？品牌在哪？商品长什么样？"
  策略: 只找 nav a、.brands-name a、.features_items
  输出: ~30 行文本（品牌列表+5 个商品名/价格）

Round 2 (explore-deep.js):
  问题: "单个商品卡片的 DOM 结构是什么？名称和价格用什么选择器？"
  策略: 只取第一个卡片的 innerHTML，截断到 2000 字符
  输出: ~50 行（9 个候选选择器测试结果+6 个商品信息）

Round 3 (explore-cart.js):
  问题: "购物车表格的结构是什么？数量是 input 还是 button？"
  策略: 只取 #cart_info_table，截断到 3000 字符，逐行测试选择器
  输出: ~40 行（购物车行结构+9 个选择器测试结果）
```

**每次探索脚本的输出都被人为截断到合理长度：**

```javascript
console.log(cardHTML?.substring(0, 2000));   // 只取前 2000 字符
console.log(modalText?.substring(0, 500));    // Modal 内容只取 500 字符
console.log(cartText?.substring(0, 500));     // 购物车内容只取 500 字符
console.log(tableHTML?.substring(0, 3000));   // 表格 HTML 只取 3000 字符
```

#### 三种信息获取策略对比

| | 全量 DOM | A11y Tree | Claude Code 的分层探索 |
|---|---|---|---|
| 信息量 | 太多（过载） | 刚好（但丢失 CSS） | 刚好（分块截断） |
| AI 理解难度 | 极高 | 中 | 低 |
| 信息完整性 | 高（但冗余） | 低（丢失属性） | 中（可以补充探索） |
| 探索效率 | 低 | 中 | 高 |
| 适用场景 | 不适用 | 通用 | 通用 |

**Claude Code 的成功不是因为它能处理更多信息，而是因为它只看需要的信息。**

### 3.8 AI_Web_Testing 的核心瓶颈

1. **DSL 作为中间表示**：命令式代码（`card.locator('.add-to-cart').first().click()`）vs 声明式 DSL（`{"action": "click", "target": "Add to cart"}`），后者语义精确度远低于前者
2. **A11y Tree 信息量不足**：仅 role + name，无法区分两个同名的按钮（如两个 "Add to cart"）
3. **ReAct 守卫过度保护**：覆盖度检查、元素数量检查、safety cap 等拦截逻辑占用了大量 token 和轮次
4. **探索工具抽象层级过高**：模型需要理解复杂的 DSL steps/actions 格式来调用 explore_flow

---

## 四、Claude Code 的"学习框架"

### 真相：没有自动化学习/自愈框架

Claude Code 的"学习"是纯手动的：

**1. Memory 系统**（文件级持久化）：4 种类型
- `user`：用户偏好、角色、知识水平
- `feedback`：用户的纠正和确认
- `project`：项目上下文、正在进行的工作
- `reference`：外部资源指针

**工作机制**：Session 开始时读取 MEMORY.md 索引 → 加载相关记忆文件。模型自主决定写入。不能记住"上次这个按钮用什么选择器成功了"——这类信息属于"可从代码推导"，不在记忆范围内。

**2. CLAUDE.md**：项目指令文档，手动维护。

**3. Hooks**：工具调用前后的 shell 脚本，用户手动配置。

**4. 会话内上下文**：模型在当前会话内记住对话历史。关掉终端就没了。

### Claude Code 的"自愈"

本质是**模型在手动调试**，不是自动化自愈：

```
脚本报错 → 模型读错误信息 → 推理原因 → 修改代码 → 重跑 → 通过
```

三周后网站改版 → Claude Code 完全不记得上次经验 → 重新写脚本 → 重新踩坑。

---

## 五、两种范式的长短版

### Claude Code（命令式脚本范式）

| 长处 | 短板 |
|------|------|
| 反馈速度：秒级 | 持久化：无，脚本跑完就完了 |
| 信息带宽：无限（完整 DOM/CSS/JS） | 跨会话记忆：无 |
| 灵活性：无限（Playwright 能做的都能做） | 网页变更适应：差（Class 名变了就挂） |
| 简单性：1 层转换（需求→代码） | 非技术用户：不能用 |
| 调试能力：完美（标准 dev 调试） | 回归测试：需手动维护脚本 |
| 一次性任务效率：极高 | 质量治理：无 |
| | 规模化：100 个用例=100 个散落脚本 |

### AI_Web_Testing（声明式 DSL 平台范式）

| 长处 | 短板 |
|------|------|
| 测试资产持久化：DSL 存数据库 | 单次任务效率：低 |
| 自愈能力：5 层定位器回退链 + Tier 0 | 反馈速度：慢（分钟级） |
| 跨会话学习：Tier 0 修正跨项目共享 | 信息带宽：受限（仅 A11y） |
| 网页变更适应：强（语义定位器） | 灵活性：受限（7 种 action） |
| 非技术用户可用：PM 写需求→自动执行 | 复杂度：4-5 层转换 |
| 执行审计：每步截图+证据 | 调试：困难（需逐层排查） |
| 质量治理：governance_meta | 过度工程化风险：高 |
| 规模化：批量执行+聚合分析 | |

### 核心洞察：不是竞争，是互补

```
Claude Code 擅长:  "第一次就写对" → 探索式、一次性任务
AI_Web_Testing 擅长: "第一百次也对" → 重复执行、持续维护、规模化
```

**场景互补示例：**

- 新测试想法，想快速验证 → Claude Code（10 分钟写脚本跑一下）
- 确定需要持续运行的回归测试 → 导入 AI_Web_Testing（花时间调通，之后自动跑）
- 探索不熟悉的网站 → Claude Code（3 轮递增探索，自动发现 DOM 结构）
- 维护 50+ 测试用例的测试套件 → AI_Web_Testing（数据库管理、批量执行、修正记录共享）

---

## 六、Selenium vs Playwright 对比

从两个 Claude Code 项目直接对比：

| | Selenium (mimov2.5pro) | Playwright (dsv4pro) |
|---|---|---|
| **通信协议** | WebDriver HTTP (JSON Wire) | Chrome DevTools Protocol (WebSocket) |
| **速度** | 慢（每操作一个 HTTP 往返） | 快（WebSocket 长连接） |
| **自动等待** | 需手动 WebDriverWait | 默认自动等待元素可操作 |
| **元素定位** | 按策略分类（By.CSS_SELECTOR 等），即时执行 | 懒加载 locator，可链式、筛选、组合 |
| **网络拦截** | 困难（需代理） | 原生 page.route() |
| **并发隔离** | 每测试一个浏览器实例 | 一个浏览器多个 BrowserContext（不同 Cookie/Storage） |
| **浏览器支持** | Chrome/Firefox/Safari（通过各厂驱动） | Chromium/Firefox/WebKit（自带二进制） |

### 具体代码对比

**等待弹窗**：

```python
# Selenium: 必须显式等
self.wait.until(EC.visibility_of_element_located((By.ID, "cartModal")))
```

```javascript
// Playwright: 自动等
await page.locator('button').filter({ hasText: 'Continue Shopping' }).click();
```

**元素定位**：

```python
# Selenium: 僵化
self.driver.find_element(By.CSS_SELECTOR, ".productinfo.text-center")
```

```javascript
// Playwright: 灵活链式
page.locator('.features_items .col-sm-4').first().locator('.add-to-cart').first()
```

### 为什么 mimov2.5pro 零错误？

不是因为 Selenium 更好，而是因为用的是更强大的推理模型，一次性就想到了所有边界情况（eager 加载策略、更大的超时设置、简单的字符串解析避开正则陷阱）。

dsv4pro 的 Playwright 代码质量实际上更高——有错误收集机制、结构化日志、断言工具函数。

---

## 七、对你的项目的建议

1. **你的技术选型（Playwright）是对的**：Browser Context 天然支持多页面状态隔离
2. **但你的探索方式（CDP A11y）太受限**：应该考虑保留 A11y Tree 信息的同时，增加直接 DOM 探索能力
3. **Tier 0 修正记录是 Claude Code 完全没有的差异化能力**——这是你真正的护城河
4. **把 Claude Code 当作输入源**：让它生成 Playwright 脚本 → 你的系统提取定位器信息 → 存入修正记录 → DSL 受益
5. **减少守卫逻辑**：让模型有更多试错空间，而不是用大量 guard 拦截它

---

## 八、结论

**Claude Code 的成功不是因为它比你聪明，而是因为它做的事比你简单**——它只需要生成能跑通的代码，而你的系统需要生成一种受限 DSL 来精确描述每一个浏览器操作，然后还要正确解释它。

**你的项目的价值不在于"比 Claude Code 更快写好测试"**。它的价值在于：
- 自愈能力（Claude Code 完全没有）
- DSL 持久化和管理（Claude Code 完全没有）
- 非技术用户入口（Claude Code 完全没有）
- 执行审计和治理（Claude Code 完全没有）

两者分工，不是竞争。
