# 10 P0 基线记录

记录时间：2026-09-24

## 环境

| 项 | 值 |
|---|---|
| 分支 | `v2` tracking `origin/v2` |
| OS | macOS |
| Python 入口 | `/usr/bin/python3` (`python` 命令不存在) |
| Playwright 浏览器缓存 | `/Users/bytedance/Library/Caches/ms-playwright` |
| Web 依赖 | `web/node_modules`，通过 `npm install --cache ../data/npm-cache` 安装 |

## P0 门禁命令

```bash
python3 run_tests.py
```

结果：

| 层 | 结果 |
|---|---|
| 契约一致性（Go） | OK |
| 契约一致性（Python） | OK |
| 闭环全链路（Go，假执行器 + 脚本模型） | OK |
| 真执行器（Python + Playwright） | OK，79 tests |
| Web 类型与构建 | OK |

汇总：`RESULT: OK（5 层全通过）`

## P0 环境修正

- 移除了 `test.config.json` 里硬编码的 Windows `PLAYWRIGHT_BROWSERS_PATH`。
- 原因：该配置会在 macOS 下覆盖 Playwright 默认缓存路径，导致 Chromium 查找路径变成
  `worker/D:\PlaywrightBrowsers/...` 并使全部浏览器测试启动失败。
- 调整后：Playwright 使用平台默认缓存；如 Windows 本机需要固定路径，可在外部环境变量里设置。

## 当前能力边界

- 本地 fixture 已覆盖：搜索/筛选、进入详情、修改数量、加入购物车、进入购物车、断言数量。
- 半自动回灌已由后端测试覆盖：执行失败 → 报告信号 → feedback candidate → 同 session 下一轮。
- 真实站点仍有待 P1-P5 处理的 blocker：纯图标 `type=button` 搜索提交、重复商品卡片定位、Google vignette 插屏、观测 token 成本。

## P0 结论

当前 v2 基线可用，后续 P1-P4 可以在此基线上演进。P0 未改变动作/条件契约语义。

## P1-P4 演进记录

记录时间：2026-09-24

### P1 Observation v2

- `Observation` 增加 `structures`、`truncated`、`truncation_reason`。
- `ElementObservation` 增加 `parent_ref`、`container_ref`、`own_text`、`full_text`、`bbox`、
  `visible_in_viewport`、`z_index`、`attributes`、`form`。
- worker 观测开始收录 `card`、`list_item`、`table_row`、`form`、`dialog`、`frame` 等结构节点。
- 保留旧 `elements + verified locators` 执行语义，旧 case 不退化。

### P2 TargetSpec v2

- `Target` 可携带可选 `spec`，包含 `object`、`scope`、`relation`。
- planner 支持先解析 scope，再在 scope/后代 scope 内解析 object。
- alias/属性参与评分，但最终 locator 仍只来自观测期已验证 locator。
- 裸同名目标仍返回 `target_ambiguous`，不会默认点第一个。

### P3 Action/Condition v2

- 新增动作：`select`、`check`、`uncheck`、`scroll_into_view`、`hover`、
  `dismiss_dialog`、`upload_file`、`assert_element`、`assert_attribute`、`assert_count`。
- 新增条件：`element_state`、`attribute_equals`、`count_equals`。
- Python runner 与作者态 `/act` 已支持对应浏览器操作；Go/Python 契约一致性夹具已覆盖 `select`。

### P4 Planner v2

- `PageView` 增加 `scopes`、`truncation_reason`，元素摘要包含 `container_ref`。
- 干跑失败返回 `failed_step`、`current_url`、`repair_hints`，用于模型重观测、缩小 scope 或重建尾部步骤。

### P1-P4 门禁

```bash
python3 run_tests.py
```

结果：`RESULT: OK（5 层全通过）`

- Go 契约一致性：OK
- Python 契约一致性：OK
- Go 闭环全链路：OK
- Python + Playwright 真执行器：OK，84 tests
- Web 类型与构建：OK

## 真实电商 canary

目标：

```text
打开真实电商站点 https://automationexercise.com/product_details/1，确认商品详情页显示 Blue Top，
并确认 Add to cart 按钮可见。不要登录，不要结账，不要下单。
```

结果：

| 项 | 值 |
|---|---|
| run | `run_4645ad8c512e1929` |
| session | `sess_9112c4b0a8804e86` |
| execution | `exec_0a347a61566286d2` |
| 状态 | `passed` / run `completed` |
| 步骤 | 4 total, 4 passed, 0 failed |
| 最终 URL | `https://automationexercise.com/product_details/1` |
| 模型用量 | 3 calls, 39,585 total tokens |

补充观察：

- 真实站点搜索路径仍会受到 `#google_vignette` 插屏与搜索按钮语义影响；本次 canary 使用
  稳定的商品详情页路径完成真实站点 E2E。
- 这些 blocker 仍属于 P5 的恢复层范围，不纳入 P1-P4 必过门禁。

## P5 Blocker + Grounding Recovery 记录

记录时间：2026-09-24

- `Observation` 增加 `blockers` 摘要，覆盖 cookie/consent、dialog/overlay/interstitial、
  auth wall、captcha、loading 等通用阻塞类型。
- worker 在 click/select/check/uncheck/hover/upload 前执行 hit-test 可达性检查；坐标只用于判断
  `elementFromPoint` 命中者，真实动作仍作用在 case 已接地 locator 上。
- 执行器只自动处理低风险恢复：等待 loading、点击已验证的 close/accept/reject/cancel 等关闭控件、
  Escape 关闭 dialog。auth wall 与 captcha 返回 `blocked_by_auth` / `blocked_by_captcha`。
- `ExecutionResult.steps[]` 增加 `blocker`、`hit_test`、`recovery`，干跑失败会把这些字段回传给 planner
  的修复上下文。
- 新增本地 fixture `worker/fixtures/site/blockers.html` 与 `worker/tests/test_blockers_p5.py`，
  覆盖 blocker 观测、安全关闭遮罩并重试、登录墙不绕过。

P5 本地门禁：

```bash
python3 run_tests.py
```

结果：`RESULT: OK（5 层全通过）`

- Go 契约一致性：OK
- Python 契约一致性：OK
- Go 闭环全链路：OK
- Python + Playwright 真执行器：OK，87 tests
- Web 类型与构建：OK

### P5 真实站点 canary

目标：

```text
打开 https://automationexercise.com，用站内搜索找 Blue Top，并确认搜索结果里出现 Blue Top。
不要登录，不要结账，不要下单。
```

结果：

| 项 | 值 |
|---|---|
| run | `run_e562008abde069d8` |
| session | `sess_639b1ce7be2d458d` |
| 状态 | `failed` |
| 失败阶段 | 规划期，未生成 case 工件，未进入审批/执行 |
| 失败原因 | token 预算用尽：`1518114 / 1500000`，28 次模型调用 |
| 模型用量 | 1,486,586 prompt / 31,528 completion / 1,518,114 total tokens |

关键观察：

- 作者态已实际到达 `https://automationexercise.com/products?search=Blue+Top`，观测中包含
  `Searched Products` 和 `Blue Top`。
- `finish_case` 干跑失败在搜索输入步骤：`input(submit=true)` 后 URL 仍为
  `https://automationexercise.com/products`，不包含 `search=Blue`。
- 后续模型尝试定位搜索图标按钮，但出现 `target_ambiguous`、`scope_not_found`、`target_not_found`
  循环，最终耗尽预算。
- 本次失败不是 auth/captcha；核心剩余问题是纯图标 `type=button` 搜索提交的 scope/ref/alias 表达
  仍不足以让模型稳定选中 `#submit_search`。

### P5 Candidate-first 修复记录

记录时间：2026-09-24

- `Observation` 增加 `action_candidates`，当前实现覆盖通用 `element_candidate` 与
  `form_submit_candidate`。
- `form_submit_candidate` 由表单结构、submit 控件、邻近输入控件、placeholder/name/id 等页面事实生成；
  目标 locator 仍来自观测期已唯一验证的 locator。
- planner 工具协议支持 `candidate_id`：模型可选择最近 PageView 给出的候选，控制面校验
  candidate 来自最新 Observation、动作兼容、target_ref 存在且可见可用、locator 仍属于目标元素。
- PageView 向模型展示 candidate 的语义摘要和关系，但不暴露 locator 细节；case grounding 记录
  被选中的 `candidate_id`。
- 新增离线回归：纯图标搜索提交按钮通过 `form_submit_candidate` 接地并执行，不要求模型手写
  CSS/XPath。

本地验证：

```bash
cd backend && go test ./internal/agentruntime -run TestCandidateIDClickReachesIconOnlyFormSubmit
cd backend && go test ./internal/planner -run 'TestPageViewIncludesActionCandidatesWithoutExposingLocators|TestResolveActionCandidate'
cd worker && uv run python -m unittest tests.test_observation_v2.ObservationV2Test.test_capability_fixture_exposes_cards_and_form_relationships
```

结果：三组 targeted 测试均通过。

### P5 Candidate-first 真实站点重跑

使用相同目标重跑真实模型 + 真实站点闭环：

| 项 | 值 |
|---|---|
| run | `run_9cbd614ad9075619` |
| session | `sess_e8acf3099e046ce6` |
| execution | `exec_5c3046c87a426e34` |
| 状态 | `passed` / run `completed` |
| 步骤 | 5 total, 5 passed, 0 failed |
| 最终 URL | `https://automationexercise.com/products?search=Blue%20Top` |
| 模型用量 | 14 calls, 580,308 total tokens |
| 报告信号 | 0 |

关键观察：

- 规划通过全新浏览器干跑后进入审批，审批后的正式执行也通过。
- 输入框通过 candidate 接地；纯图标 `type=button` 搜索按钮最终使用高置信
  `element_candidate`（alias `submit_search`）接地，而不是离线 fixture 中的
  `form_submit_candidate`。
- 模型没有写 CSS/XPath；case 中保存的是观测期唯一验证过的 role locator 与 candidate lineage。
- 5 张步骤截图均可由控制面访问，最终截图可见 `Searched Products` 与 `Blue Top`。
- 相比修复前的 1,518,114 token 预算失败，本次用量降至 580,308 token，但规划成本仍然偏高。

### P5 `www` 入口真实站点重跑

将入口改为 `https://www.automationexercise.com/`，使用相同搜索目标再次运行：

| 项 | 值 |
|---|---|
| run | `run_d247f0eebb377281` |
| session | `sess_8da4c8d903075404` |
| execution | `exec_70bfd03f792f7a09` |
| 状态 | `passed` / run `completed` |
| 步骤 | 6 total, 6 passed, 0 failed |
| 最终 URL | `https://www.automationexercise.com/products?search=Blue%20Top` |
| 模型用量 | 21 calls, 1,313,390 total tokens |
| 报告信号 | 0 |

关键观察：

- `www` 入口直接返回 HTTP 200，浏览器执行期间始终保留 `www` 域名。
- case 从首页点击 `Products`，输入 `Blue Top`，点击纯图标搜索按钮，再断言结果文本与 URL。
- 6 张步骤截图均可由控制面访问，最终截图可见 `Searched Products` 与 `Blue Top`。
- 规划虽通过干跑，但用量接近 1,500,000 token 熔断线，说明相同站点与目标仍有明显规划方差。

## 电商基线测试账号与实际链路

记录时间：2026-09-24

测试账号：

| 项 | 值 |
|---|---|
| 站点 | `https://www.automationexercise.com/` |
| 显示名 | `AI Web Test` |
| 邮箱 | `ai.web.testing.20260924202222@example.com` |
| 密码存储 | macOS 钥匙串 service `AI_Web_Testing/automationexercise.com` |
| 验证 | 注册成功；退出后重新登录成功；页面显示 `Logged in as AI Web Test` |

密码不写入仓库；测试时允许把账号密码作为目标输入直接交给模型，不要求凭据脱敏或 `secret_ref`。
钥匙串只用于人工保存测试账号，当前 runtime 不需要读取钥匙串。

真实页面链路：

1. `/login`：填写 email/password，点击 `Login`，验证 `Logged in as AI Web Test`。
2. `/products`：填写 `#search_product`，点击纯图标 `type=button#submit_search`。
3. 搜索结果：验证 `Searched Products` 和 `Blue Top`，进入 `/product_details/1`。
4. 商品详情：将 `input#quantity` 从 `1` 改为 `4`，再点击 `Add to cart`。
5. 加购弹窗：验证 `Added!`，点击 `View Cart`。
6. `/view_cart`：验证 `Blue Top`、数量 `4`、单价 `Rs. 500`、总价 `Rs. 2000`。

站点不支持在购物车页修改数量：数量显示为 `class="disabled"` 的按钮，没有可编辑控件。因此基线语义是
“在详情页设置数量后加入购物车”，不是“加入购物车后修改数量”。

### 完整电商目标首次 AI 自主规划

目标：使用上述测试账号登录，搜索 `Blue Top`，进入详情页将数量设为 `3`，加入购物车并验证数量。

| 项 | 值 |
|---|---|
| run | `run_12c6a72b94352d07` |
| session | `sess_a1bf438e61a37ef0` |
| 状态 | `failed` |
| 到达位置 | 已登录、已搜索、已进入详情、数量已设为 `3`、已点击加购 |
| 失败位置 | 加购弹窗内进入购物车之前 |
| 模型用量 | 23 calls, 1,604,988 total tokens |
| 失败原因 | 超过 1,500,000 token 预算 |

根因证据：

- 单次调用的 prompt 从 8,243 token 增长到 123,875 token；Runtime 每轮重发完整 `messages` 历史。
- 每次工具结果附带约 10-16 KB PageView，旧观测、旧步骤和模型回复全部继续留在后续请求中。
- 模型成功登录后曾连续删除 4 个已正确记录的步骤，随后重新构建相同路径。
- 搜索阶段出现 `candidate_incompatible`，加购弹窗内点击 `View Cart` 时出现
  `blocked_by_overlay`；重复修复进一步放大上下文。
- `View Cart` 失败不是遮挡恢复本身误判：模型选中的是页头 `Cart` candidate，而加购 modal
  正在遮挡它。作者态 `act` 点击加购后立即观测，没有等待声明的 `expect_text: Added!`，
  因而 PageView 没收录 modal 内真正的 `View Cart`，模型只能看到页头候选。
- 结论：页面动作能力已覆盖大部分链路，当前首要阻塞是跨轮上下文不裁剪和失败策略缺少硬去重，
  同时作者态动作必须等待 expectation 成立后再生成下一份观测；不是模型调用次数上限过低。

## 有界规划器电商基线

记录时间：2026-09-24

### 一次输入契约

控制器运行真实 canary 时，把已有测试账号的实际邮箱和密码直接替换进下面两个占位符，并将整段作为
唯一一次 goal 输入。账号密码是普通测试数据；runtime 不读取钥匙串，不使用 `secret_ref`、脱敏或
凭据保险库。

```text
打开 https://www.automationexercise.com/，使用账号 <测试账号邮箱> 和密码 <测试账号密码> 登录，
确认页面显示 Logged in as AI Web Test；打开 Products，搜索 Blue Top，进入 Blue Top 商品详情页，
把数量设置为 3 后加入购物车，从 Added! 弹窗点击 View Cart，最后确认购物车行显示 Blue Top 且
数量为 3。不要结账，不要下单，不要删除账号。
```

规划器行为基线：

- 每轮从原始 goal、不可由模型修改的 committed prefix、一个当前有界 PageView、紧凑结果和最多
  8 个失败签名重建上下文；完整 Observation 只留在服务端。
- 会改变页面的作者态步骤必须等派生 expectation 通过后才提交；这类失败尝试不进入 case，动作
  可能改变页面时由后端重放已提交前缀。页面断言也必须先对当前完整 Observation 验证通过才提交；
  文本按可见 element 的 Name/Text/FullText 和可见 structure 的 FullText 逐节点规范化匹配，
  不跨节点拼接，已提交断言再由全新干跑复验。
- 新鲜干跑在第 `k` 步失败时，后端删除 `k..end` 并重放 `0..k-1`；模型不能删除已提交步骤。
- 生产 planner 没有电商专用 action 或硬编码 selector；离线脚本只用于确定性回归。

### Task 8 离线验证

离线验证不读取任何模型密钥，也不调用真实模型或真实站点。

格式与编译：

| 命令 | 结果 |
|---|---|
| `cd backend && test -z "$(gofmt -l .)"` | exit 0；没有 Go 文件名输出 |
| `cd worker && uv run python -m compileall -q loop_worker tests` | exit 0；无错误输出 |

首次完整回归：

```bash
python3 run_tests.py
```

结果：`RESULT: OK（6 层全通过）`。

- Go 契约一致性：OK。
- Python 契约一致性：OK，10 tests。
- Go 闭环全链路：OK，全部 package 通过。
- Python + Playwright 真执行器：OK，94 tests。
- 完整电商闭环：OK，12 steps passed；cart screenshot 存在且可通过控制面读取。
- Web TypeScript + Vite build：OK，46 modules transformed。

聚焦不变量：

```bash
cd backend && go test ./internal/agentruntime -run 'Context|Committed|Replay|Fresh|Repeated'
cd backend && go test ./internal/planner -run 'PageView|Fingerprint|Replay'
cd worker && uv run python -m unittest tests.test_api tests.test_ecommerce_baseline
```

结果：两个 Go package 均为 `ok`；Python 为 `OK`，13 tests。

最终 `git diff --check`、状态检查和第二次完整离线回归的命令摘要记录在 `docs/execution-log.md`，
精确退出码与状态快照记录在 Task 8 report。这些结果只证明 fixture、脚本模型和本地浏览器闭环，
不代表真实模型/真实站点 canary。

### 控制器追加：真实 quantity-3 canary

本工作树不运行真实 canary，下面字段明确留给持有 staged credentials 的控制器追加。控制器的
首次尝试暴露了 `committed_prefix_replay_failed` 的 planner 生命周期缺陷，因此没有可接受的本次
基线指标；代码与离线回归修复后，仍需控制器重跑。上文历史 canary 不能替代本次有界规划器
quantity-`3` 验收，也不构成本次通过证据。

必过条件：

```text
planning -> awaiting_approval -> executing -> completed
report: steps_failed=0
cart: Blue Top, quantity=3
model_calls <= 25
prompt_tokens per call <= 30000
total_tokens <= 600000
repeated failure signatures = 0
```

| 项 | 控制器实测值 |
|---|---|
| 验证状态 | `PENDING — offline fix complete; controller live rerun required` |
| run id | `<append after live canary>` |
| session id | `<append after live canary>` |
| execution id | `<append after live canary>` |
| 状态序列 | `<append after live canary>` |
| 单次 prompt 最大值 | `<append after live canary>` |
| raw prompt / completion / total | `<append after live canary>` |
| cached prompt total | `<append after live canary>` |
| fresh prompt / total | `<append after live canary>` |
| report summary | `<append after live canary>` |
| final URL | `<append after live canary>` |
| cart 与 screenshot 检查 | `<append after live canary>` |
| repeated failure signatures | `<append after live canary>` |
