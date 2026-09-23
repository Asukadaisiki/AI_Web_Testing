# 09 实验记录

本文件记录重构过程中的**实测证据**。与 01~08 的"计划/主张"区分开：这里只写跑过什么、拿到什么数字、暴露了什么。

命名上注意：**本文件的"实验 A/B"与 05 里的迁移阶段 P0~P6 无关**，不要混读。

---

## 实验 A：真实站点闭环 + 成本计量（2026-09-22）

### 目的

回答一个问题：**v2 的闭环是不是只在干净的本地夹具上成立？**

v1 的 live E2E 从未收敛，卡点之一是 `automationexercise.com`（AdSense 插页劫持点击、模型把 `click` 改写成 `goto` 被 DSL 拒绝）。所以这次直接拿**同一个站点**当靶子——真实、有广告、有动态内容。

同时补上 v1 有而 v2 缺的**成本可见性**（v1 有 `AGENTSERVICE_MAX_TOTAL_TOKENS` 熔断与 prompt-cache 身份，v2 只有调用次数上限）。

### 环境

| 项 | 值 |
|---|---|
| 模型 | `deepseek-v4.1-flash[1m]` @ `https://ark.cn-beijing.volces.com/api/plan/v3`（火山方舟） |
| 执行器 | v2 Python/Playwright 1.57.0，Chromium |
| 控制面 | `loopd` + SQLite，`LOOP_DATA_DIR=data/p0` |
| 目标站点 | `https://automationexercise.com` |
| 输入目标 | 打开 `https://automationexercise.com/products?search=Blue+Top`，点击 View Product 打开详情页，确认详情页能看到 Add to cart 按钮 |

### 结果：通过

```
planning → 4 次模型调用（零重试、零失败工具调用）
finish_case 干跑（全新浏览器上下文）通过 → awaiting_approval
审批 → executing → completed
4/4 步通过，0 失败信号，执行耗时 16 s
URL 流转：about:blank → /products?search=Blue+Top → /product_details/1
```

### 生成的 case（节选，注意第 2 步的定位器）

| # | action | 内容 | 前置 / 后置条件 |
|---|---|---|---|
| 0 | `goto` | `https://automationexercise.com/products?search=Blue+Top` | 无 / `url_contains=/products` |
| 1 | `click` | hint `View Product` → **`role=link name="\uf0fe View Product" exact=true match_count=1`**，接地于 `obs_8569d2ac9b610846` | `url_contains=/products` / `url_contains=/product_details` |
| 2 | `assert_url` | `/product_details` | `url_contains=/product_details/1` / `url_contains=/product_details` |
| 3 | `assert_text` | `Add to cart` | `url_contains=/product_details/1` / `text_visible=Add to cart` |

第 1 步是这次实验最重要的正面证据：模型只说了一个**自然语言 hint**（`View Product`），Go 把它在**真实观测**上解析成了唯一元素，并把验证过的定位器（**连同可访问名里那个 Font Awesome 字形**）落进了 case。模型全程没有写过一个 CSS 选择器、一行 JSON。

### 证据

| 检查 | 结果 |
|---|---|
| 截图 4 张 | 全部 `HTTP 200`，176 KB ~ 200 KB，`image/png` |
| 网络请求 | 44 + 83 + 1 + 2 条，**状态码 ≥400 的 0 条** |
| 控制台 | 仅站点自身的 Mixed Content 警告（它自己用 `http://` 加载字体）与一条 WebGL 弃用提示；**无我方错误** |

### 成本（新增计量的实测值）

| 指标 | 值 |
|---|---|
| 模型调用 | 4 |
| 输入 token | 20,922 |
| 输出 token | 476（其中思考 121） |
| 合计 | **21,398** |
| 缓存命中 | **12,544**（占输入 60%） |

每次调用后的累计（来自 `model_usage` 事件）：

```
#2  call=1970   total=1970   cached=1792
#5  call=3649   total=5619   cached=1792
#8  call=5825   total=11444  cached=3456
#13 call=9954   total=21398  cached=5504
```

**输入是输出的 44 倍。** 主要开销是观测负载，且每轮都要把观测重发一遍。要降成本，方向是压缩观测，不是换模型。缓存命中 60% 是唯一省下来的部分，现在看得见了。

---

## 发现（这是本文件的主要价值）

以下 F1~F6 全部来自**零模型成本的预检**（直接调执行器 `/sessions/{id}/navigate` 读观测），不是推测。

### F1. 图标按钮在契约里无法定位（阻塞级）

站点的搜索提交按钮：

```
ref=e127 tag=button role=button name="\uf07a" text=""  visible=true enabled=true
locators: role(name="\uf07a", exact=true, n=1) | css("#submit_search", n=1)
```

可访问名是 **Font Awesome 字形**（`<i class="fa fa-search">` 没加 `aria-hidden`），`text` 为空。

而 `click` / `input` 只接受 `hint`（自然语言或可访问名），解析器只按 `name` / `text` 打分。**任何 hint 都匹配不到这个按钮**。同时执行器的 `input` 只做 `fill`、**不按回车**。

结论：**在真实站点上"在页面里搜索"这一步做不到。** 本次实验是靠把搜索结果 URL 直接写进目标绕过去的——也就是说，**这个用例的起点是人给的，不是模型走出来的**。这是本次通过的前提，必须记清楚，不能算作闭环的完整胜利。

### F2. N 个完全相同的元素无法区分（阻塞级）

商品列表页：

```
34 个 <a> role=link name="" text="View Product"        （locator 只有绝对 css）
68 个 <a> role=generic name="" text="Add to cart"
"Blue Top" 在观测里出现 0 次
```

- 商品名在 `<p>` 里，**不是交互元素**，观测不收 → 模型看不到自己要点的是哪个商品。
- 34 个候选 `name`/`text` 完全相同 → 解析器给出 `target_ambiguous`，**拒绝执行**。

所以"打开 Blue Top 的详情页"在这一页上**不可表达**。本次实验是靠搜索把结果收窄到 1 个商品才让它唯一的。

**这里要分清两件事**：
- v1 在这种情形下会**默默点错**（点第一个匹配），这正是它当年的失败模式；
- v2 **拒绝并报歧义**——设计是对的。

但结论仍然是：**"按名字定位卡片内元素"这类需求，现在做不到。**

### F3. 观测里被广告污染，且广告排在真实内容前面

```
文件里 aswift(AdSense 容器 #aswift_1_host) 出现 20 次
前 8 个元素全部来自广告 iframe：
  Learn Coding Online / Sarees / Send Money Overseas / MEN / Printed tops / Clothing ...
```

模型是在**一屏广告链接**里做决策的。本次目标没和广告撞名所以没出事，但这是 v1 被劫持的同一类环境。**观测没有任何广告/第三方域过滤。**

### F4. 模型看到的观测是截断的，解析器看的是全量

```
事件 #4: {"elements":47}
事件 #7: {"elements":60, "truncated":true}   ← 详情页实际 74 个
```

模型看到的是**上限 60 个**的紧凑视图（`PageView`），解析器在**全量** `contract.Element` 上工作。这个分离本身是合理的（省 token），但意味着**模型可能看不到它需要的东西**——而它看不到时只会去猜，猜了就撞 `target_not_found`。

### F5. 可访问名被图标字形污染

```
link  "Cart"      → 实际 name = "\uf07a Cart"
link  "Signup / Login" → 实际 name = "\uf090 Signup / Login"
button ""          → 纯字形，完全不可读
```

站点用图标字体且不隐藏，浏览器把字形算进了可访问名。解析器的**子串匹配**（80/70 分档）救回了 `Cart` 这类；但**精确匹配（100 分档）在这些元素上永远不成立**，纯字形元素则完全无法定位。

### F6. 通过的那次运行，恰好绕开了上面全部问题

把 F1~F5 和实验 A 的成功放在一起看：

| 问题 | 本次为什么没触发 |
|---|---|
| F1 图标按钮 | 起点 URL 是人给的，跳过了搜索提交 |
| F2 34 个相同链接 | 搜索把结果收窄到 1 个 |
| F3 广告污染 | 目标没和广告撞名 |
| F4 观测截断 | 需要点的元素落在前 60 个里 |
| F5 字形污染 | 用了子串匹配 |

**结论：实验 A 证明的是"闭环在真实站点上能跑通"，不是"闭环能自己走通真实站点"。** 差距就在这张表里。

---

## 本次实验顺带做掉的事

### 成本计量与熔断（整条链路）

| 层 | 改动 |
|---|---|
| `internal/usage`（新包） | `Usage` 类型 + 累加；单独成包因为被三边共用（LLM 产出、Runtime 熔断、store 持久化），放任何一边都会逼出反向依赖 |
| `agentruntime` | `LLM.Next` 改为返回 `(Message, usage.Usage, error)`——用量与输出一起回来，才能**在同一轮**决定是否超预算；每次调用发 `model_usage` 事件 |
| `store` | 新表 `model_usage`（一个 run 一行，**增量 upsert**）。用新表而非给 `runs` 加列：`CREATE TABLE IF NOT EXISTS` 对已存在的库也会建出来，**不需要列迁移** |
| `api` | `usage` 同时挂在 `GET /api/runs` 与 `GET /api/runs/{id}`——列表与详情必须同一个形状 |
| `web` | 会话页与报告页显示用量；时间线新增「模型用量」；**只显示 token，不做金额换算**（单价随模型与时段变，前端算钱只会算错） |
| 熔断 | `LOOP_MAX_TOTAL_TOKENS` 默认 150 万；超限 run `failed`（不是 case 失败），错误里给已用/上限/调法；**超预算那次调用的用量同样记账** |

脚本模型不报用量 → 熔断对它天然不生效，这正是离线验证可以反复跑的前提（有测试钉住）。

### 离线验证基线（三层，零成本可重复）

```
Go     : gofmt 无输出；go vet 干净；go test ./... → 9 个包全 ok
Python : compileall exit 0；unittest discover → Ran 67 tests / OK
Web    : tsc --noEmit + vite build → ✓ built
页面   : 4 页真实浏览器验证（脚本化控制面）→ 10 项断言全过，console error 0，HTTP ≥400 为 0
```

### 本轮修掉的 5 个真实缺陷

1. **失败信号分类错**：站点连不上报成 `step_timeout`，会误导回灌建议去调超时 → 改为 `worker_error`，并加测试钉住"真超时 vs 硬失败"。
2. **`vite preview` 没有代理**：`server.proxy` 只对 dev 生效，构建产物 `/api` 全 404；顺带 Vite 默认绑 `localhost`(IPv6) 导致 `127.0.0.1` 连不上。
3. **每次 run 都在控制台报红色 404**：run 还在 `planning` 就去问 `/case` → 状态离开 planning 再请求。
4. **截图静默 404**：执行器写 `data/artifacts`，控制面从 `$LOOP_DATA_DIR/artifacts` 提供 → 默认值对齐，且执行器 `/health` 报出 `artifacts_dir`、`/api/health` 返回 `artifacts_match`，不一致时直接说明。
5. **脚本模型一次性**：跑第二个 run 会撞上回放完的脚本 → 每个 run 开头 `Restart()`。

---

## 由本次实验新增的待拍板项

建议并入 [08-open-decisions.md](08-open-decisions.md)：

### D9. 动作词汇表是否补"容器内相对定位"？（F2）

- 选项 A：新增 `scope`/`within` 概念——先定位容器（按文本），再在容器内定位目标。表达力够，但契约、工具、执行器、干跑全都要改。
- 选项 B：观测里给重复元素加**稳定的序号/父级摘要**（例如 `View Product (第 3 个商品卡片)`），让模型能用自然语言区分。改动小，但把"结构"塞进了自然语言，解析器仍然要靠猜。
- 选项 C：不补。承认"同名重复元素"是当前契约的能力边界，遇到就报 `target_ambiguous` 让人介入。

**建议 A**，但要求先在 03 里把"作用域"建模清楚，否则会重演 v1"同一概念多套实现"的老路。

### D10. 图标按钮怎么办？（F1、F5）

- 选项 A：观测阶段**清洗可访问名**（剥掉私有区/字形字符），并额外采集 `aria-label` / `title` / `id` / `name` 作为候选匹配面。
- 选项 B：允许模型在 hint 里写 CSS id（把 `#submit_search` 当 hint 传），解析器最后回退到 CSS 精确匹配。
- 选项 C：执行器的 `input` 支持回车提交，绕开"必须点提交按钮"。

**建议 A + C。** A 治根（可访问名不该是字形），C 是常见交互，代价很小。B 要谨慎——那等于把"手写选择器"这条 v1 的老路重新打开。

### D11. 观测压缩策略？（成本）

输入是输出的 44 倍，观测每轮重发。可选：按"与目标相关性"裁剪、对未变化部分做增量、或把观测摘要化（只给可交互元素 + 与目标相关的文本）。

**这条没有建议**，需要先有一次"同一目标、不同观测策略"的对照实验数据。

### D12. 观测是否收录非交互文本？（F2）

商品名在 `<p>` 里、不是交互元素，因此模型看不到。是否收录"可交互元素的邻近文本"作为**上下文**（不可点击，但可读）？

**建议收录**，且明确标注为不可点击。否则"点这个商品"在语义上就永远无从表达。

---

## 尚未验证的部分（诚实清单）

- **样本量 1**：真实模型只跑过 2 次（1 次 DeepSeek 问 URL、1 次方舟跑通实验 A），1 种目标形态。
- **没有一次跑完整回灌闭环**：失败 → 信号 → 回灌候选 → 下一轮，只在脚本化环境验证过，真实站点上没跑过（需要一次失败的运行）。
- **没有并发**：一次一个 run。
- **成本熔断只在单测里触发过**，真实运行没接近过上限。
- **D11 无数据**：只有一个数据点。

---

# 实验 B：真实模型 + 真实站点的首次闭环（2026-09-23）

终于用真实模型（`deepseek-v4.1-flash[1m]`）在 `automationexercise.com` 上跑了。三次运行，
结论**推翻了本文件里的若干猜测**。以下全部是实测，不是推理。

## B1. 运行 1：搜索目标 —— 卡死在 planning，烧掉 138 万 token

目标：`打开 https://automationexercise.com ，用站内搜索找 Blue Top，并确认搜索结果里出现 Blue Top`
（只给首页，不给搜索结果 URL）。

结果：**33 次模型调用、1,383,141 token 后被我手工掐掉**，状态仍是 `planning`。
缓存命中 1,256,960 / 1,338,915 prompt token = **93.9%**（这一项远好于预期）。

三个独立的病因：

1. **`goto` 的 5000ms 步超时对真实站点太紧。** 三次 `open_page` 全部
   `step_timeout: goto 'https://automationexercise.com' timed out after 5000ms`。
2. **搜索提交控件是 `type="button"`，回车根本提交不了**（详见 B4）——D10-C 的
   `submit` 字段解决不了这个站。
3. **`dry_run_failed` 事件不带失败明细**，只写一句
   "the authored case did not pass a full dry run"。模型在对话里看得到哪一步没过，
   **读事件流的人看不到**——我的诊断因此瞎了一轮。已修（见 B5）。

## B2. 运行 2：导航+断言目标 —— 首次跑完整条闭环

目标：`打开 https://automationexercise.com ，点进商品列表页，确认列表里有 Blue Top 这个商品`

结果：**`awaiting_approval` → 审批 → 执行 → `completed`**。15 次调用、208,287 token。
模型自建的 case 是 4 步：`goto 首页 → click Products → goto /products → assert_text "Blue Top"`。

注意模型自己加的第 2 步 `goto /products`（意图写着"站点负载高时会出现排队提示页"）——
它在**主动给自己加冗余**来对抗它观察到的抖动。这说明抖动是可观测的，模型会试图补偿。

## B3. 真实执行失败：不是站点慢，是 `domcontentloaded` 等错了东西

运行 2 的执行结果：`run.status = completed`，但报告是
`steps_total=1, steps_passed=0, steps_failed=1`，信号
`step_timeout: goto 'https://automationexercise.com/' timed out after 20000ms`。

数据库里的取证推翻了"站点太慢"这个直觉：

```
url_after = "https://automationexercise.com/"     ← URL 是对的，页面到了
duration_ms = 20778
console: Mixed Content: ... insecure stylesheet 'http://fonts.googleapis.com/css?...'
         This request has been blocked
```

**页面早就好了，是 `domcontentloaded` 没触发。** 站点在 HTTPS 下引用
`http://fonts.googleapis.com/...`，被浏览器按 Mixed Content 拦掉，挂起的外部资源
（配合样式表之后的经典脚本）把 DOMContentLoaded 拖到 20s 之后。

于是出现最难查的一类现象：**同一份 case，干跑过、真实执行红**——因为干跑偶然在
20s 内过，真实执行 20.8s。不是站点不稳定，是等待条件选错了。

**修法**：`goto` 等 `commit`（导航已提交），页面就绪交给**后置条件轮询**——
那本来就是 v2 的就绪判据，每个条件有自己的 `timeout_ms`，独立于步超时。
步超时应当约束"动作"，不该被目标站点的外部资源绑架。

代价（诚实记录）：`commit` 返回时 DOM 可能只解析到 `<head>`，内容还没出来。
这不是缺陷，是职责划分——就绪由后置条件负责。实测验证：同一 case 连跑 3 轮，
`goto` 全过，后一步 `assert_text` 的 `text_visible` 也都等到了内容。

回归测试：`worker/tests/test_goto_wait_condition.py`。它自带一个**故意不响应**的
资源服务，并**先自证夹具真的会挂**（断言 `domcontentloaded` 在 2s 内到不了），
所以不会空过。已用变异测试验证：把 `commit` 改回 `domcontentloaded`，测试立刻红。

## B4. D10 的结论要改：`submit` 解决不了 `type="button"`

零模型成本直接问执行器（真实站点，3 个变体）：

| 做法 | 结果 |
|---|---|
| `input(submit=true)`（回车） | ❌ URL 不变 |
| 点 `#submit_search` | ✅ 跳 `/products?search=Blue%20Top` |
| `form.requestSubmit()` | ❌ URL 不变 |

原因在 HTML 里：`<button type="button" id="submit_search"><i class="fa fa-search"></i></button>`
——是 `type="button"`，不是 `type="submit"`。表单里没有 submit 按钮，回车不做隐式提交。

**所以 D10-C（回车）只覆盖"表单能被回车提交"这一类站点；D10-A（别名匹配面）才是
这个站的唯一出路**——因为提交控件就是那个纯图标按钮。本文件第 218 行"建议 A + C"
依然成立，但**优先级要反过来：A 是必需，C 是补充**。

`#submit_search` 的可访问名是 `"\uf002"`（私有区码位），`text` 也是同一个码位，
且两个面完全相同的字形按钮会互相冲突、无法通过唯一性校验——F1 精确复现。

## B5. 已修 / 已确认

- **步超时 5000 → 20000，条件超时 3000 → 10000**（Go 与 Python 两侧常量 + 文档）。
  依据：冷启动 goto 实测 5.0–6.1s，站内跳转 `/products` 实测 6.3s。
- **`goto` 等 `commit`**（B3）+ 变异验证过的回归测试。
- **`dry_run_failed` 事件带上失败步骤与未满足条件**（`payload.failure`）+ 测试。
- **`input` 的 `submit` 字段**（B4 的边界已写进 CONTRACT.md，不再声称"回车是唯一通用方式"）。

## B6. F5 的真实形状：是前缀字形，不阻塞

模型自建的 case 里，导航链接的定位器是：

```json
{"kind":"role","role":"link","name":"\ue8f8 Products","exact":true,"match_count":1}
```

可访问名带一个私有区字形**前缀**，但后半截是有意义的文本。解析器的子串匹配（80 分）
唯一命中，存的精确定位器 `match_count == 1` 也是对的。**所以 F5 单独不阻塞**——
真正阻塞的只有"名字完全由字形构成"的 F1。

## B7. 仍未解决：谷歌广告插屏（F3 的真实机制）

把运行 2 的 case 直接投给执行器连跑 3 轮（零模型成本）：

| 轮次 | 结果 |
|---|---|
| 0 | ❌ `click Products`：`condition_unmet: url_contains='products'`，当前 URL `https://automationexercise.com/#google_vignette...` |
| 1 | ✅ 4 步全过 |
| 2 | ✅ 4 步全过 |

`#google_vignette` 是 Google AdSense 的插屏广告。点击没有导航，广告脚本改了 URL 片段。
**这就是模型在运行 1 里说的"Google ad interstitial that intercepts the click"。**

2/3 通过。这是真实的站点侧条件，v2 目前的表现是诚实的（`condition_unmet` +
回灌候选），但没有缓解手段。**本文件第 6 行 F3 记的"广告 iframe 元素排在前面"机制
是错的**——`_COLLECT_JS` 只走主文档，不采 iframe；真实机制是**点击被插屏拦截**。

## B8. 成本与缓存的真实数字

| 指标 | 运行 1（失败） | 运行 2（成功） |
|---|---|---|
| 模型调用 | 33 | 15 |
| 总 token | 1,383,141 | 208,287 |
| 缓存命中 | 93.9% | 88.4% |
| 推理 token | 44,226 | 6,346 |

缓存命中率**远高于**本文件原先假设的水平。输入仍然是输出的数十倍（运行 2：
prompt 200,853 vs completion 7,434 = 27 倍），所以 D11 的压缩问题依然存在。

## B9. 诚实清单更新

- **样本量从 1 涨到 3 次真实运行**，2 种目标形态，1 个站点。
- **跑通过一次完整闭环**（规划 → 干跑 → 审批 → 执行 → 报告）。
- **失败回灌闭环仍然没有在真实站点上跑过**——需要一个真实失败的 run 再确认候选与下一轮。
  运行 1 失败了但没进到回灌（卡在 planning 就被掐掉）。
- **`run.status = completed` 与"用例通过"是两件事**：运行 2 的 run 是 completed，
  而用例是 failed。这是契约设计（只有 `ExecutionError` 才让 run 失败），但
  前端和运维读起来容易误解，值得再看一眼。
- 并发仍然没有；熔断仍然没在真实运行里触发过（运行 1 被掐在 138 万 / 150 万）。
