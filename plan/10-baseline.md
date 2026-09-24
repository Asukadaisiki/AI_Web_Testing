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

真实站点 canary：尚未重跑；后续需用真实模型验证模型是否稳定选择 `form_submit_candidate`。
