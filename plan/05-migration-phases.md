# 05 分阶段演进

本文件替代原来的“旧系统迁移阶段”。当前 v2 已经是独立单闭环实现，后续重点不再是
搬迁 v1 代码，而是把 v2 演进成**通用 Web E2E 能力平台**。

原则：

1. **领域不进核心**：不做电商专用能力，电商只作为验收场景之一。
2. **每阶段结束系统都必须可运行**：三条服务起得来，离线门禁绿，核心闭环不退化。
3. **契约先行**：任何新增观测字段、定位语义、动作、条件都先改 `CONTRACT.md` 与一致性 fixture。
4. **模型不写 selector**：即使补 alias / scope，也必须由系统接地并验证唯一命中。
5. **失败可回灌**：新增能力必须让失败信号更结构化，而不是只让执行“看起来更能点”。

阶段总览：

| 阶段 | 主题 | 预计工作量 | 是否改变行为 |
|---|---|---|---|
| P0 | v2 基线冻结与可信门禁 | 0.5~1 天 | 否 |
| P1 | Observation v2：页面世界模型 | 3~5 天 | 是 |
| P2 | TargetSpec v2：scope 与 alias 定位 | 3~5 天 | 是 |
| P3 | Action/Condition v2：通用动作代数 | 4~6 天 | 是 |
| P4 | Planner v2：探索、压缩、修复循环 | 4~6 天 | 是 |
| P5 | Blocker 恢复层：弹层/广告/遮挡 | 2~4 天 | 是 |
| P6 | 跨场景基准矩阵与文档对齐 | 2~4 天 | 是 |

---

## P0 v2 基线冻结与可信门禁

**目标**：先把当前 v2 的可用状态钉住，形成后续演进的回归基线。P0 不新增能力、不改契约语义、
不修架构，只回答一句话：**现在什么算绿，后续不能退化什么。**

**动作**

1. 确认当前仓库状态：
   - 当前分支为 `v2`，跟踪 `origin/v2`。
   - 工作区无未提交的非文档改动。
   - `test.config.json` 是唯一测试入口配置。
2. 固化离线门禁：
   - `python3 run_tests.py --list` 能列出全部层（若本机提供 `python` 别名，也可用 `python`）。
   - `python3 run_tests.py` 覆盖 Go 契约、Python 契约、Go 闭环、Python 真执行器、Web build。
   - 门禁不依赖真实模型、不依赖外网目标站点。
3. 固化本地通用能力基线：
   - 本地 fixture 能跑通：搜索/筛选、进入详情、改数量、加购、进入购物车、断言数量。
   - `input(submit=true)` 的回车提交行为有测试。
   - 每步证据必须有 screenshot、URL before/after、console/network。
4. 固化真实站点能力边界：
   - 记录 `automationexercise.com` 已通过的路径：给定搜索结果 URL → 详情页 → `Add to cart` 可见。
   - 记录未解决 blocker：纯图标 `type=button` 搜索提交、重复商品卡片定位、Google vignette 插屏、观测 token 成本。
   - 这些 blocker 必须以测试或实验记录固定，后续修复时要把对应断言翻转。
5. 固化失败回灌基线：
   - 假执行器/脚本模型覆盖：执行失败 → 报告信号 → feedback candidate → 同 session 下一轮。
   - 明确真实站点失败回灌尚未验收，作为 P6 前必须补的 canary。
6. 记录基线文档：
   - 在 `docs/execution-log.md` 或新建 `plan/10-baseline.md` 记录：命令、测试结果、当前能力边界、真实站点样本。
   - 基线只记录事实，不写“应该能”的推测。

**验收门禁**

- `python3 run_tests.py` 退出码为 0（若本机提供 `python` 别名，也可用 `python`）。
- `git status --short` 中除本次规划文档外无非预期改动。
- `worker/tests/test_runner_local.py::RunnerEndToEndTest.test_full_flow_passes_with_evidence` 覆盖完整本地购物车 happy path。
- `worker/tests/test_capability_gaps.py` 仍固定 F1/F2/F5/D12 当前边界，且注释说明哪些断言修复后需要翻转。
- `backend/internal/agentruntime/loop_test.go::TestFailureIsFedBackAsCandidates` 与 `backend/internal/api/server_test.go::TestFeedbackConfirmStartsARunInTheSameSession` 继续证明半自动回灌链路。
- `plan/09-experiment-log.md` 中真实站点结论保持可追溯：已通过路径、未解决 blocker、成本数字都能对应到实验记录。
- P0 不允许修改 `CONTRACT.md` 的动作/条件语义；如发现必须改契约，升级为 P1。

**风险**：低。
**回滚**：仅文档和门禁脚本变更，可直接 revert。

---

## P1 Observation v2：页面世界模型

**目标**：把观测从扁平元素表升级为页面世界模型，为跨行业定位打基础。

**动作**

1. 扩展 `Observation` 契约：
   - 元素层级与父容器引用；
   - 结构节点：card/list item/table row/form/dialog/frame；
   - 邻近文本与 own/full text；
   - 几何信息：bounding box、viewport 可见性、z-index/遮挡摘要；
   - 表单信息：label、控件类型、submit 候选、是否可 Enter 提交；
   - 候选属性：`aria-label`、`title`、`placeholder`、`id`、`name`、`data-testid`。
2. 更新 worker `observer`：
   - 收录可作为 scope 的容器节点；
   - 保留当前“locator 必须观测时唯一验证”的规则；
   - 给结构节点与交互节点建立关系。
3. 更新 Go `PageView`：
   - 模型看到的是目标相关摘要，不是完整 Observation；
   - 解析器仍使用完整 Observation。
4. 增加 fixture：
   - 重复卡片；
   - 表格行；
   - 表单；
   - 弹层；
   - iframe 最小页。

**验收门禁**

- 契约一致性测试覆盖 Observation v2 的新增字段。
- fixture 中 `Blue Top` 文本、卡片容器、卡片内 `View Product` 能在同一个结构关系里表达。
- 旧 case 继续能执行；旧扁平定位器语义不退化。
- 观测结果大小有上限，超限时返回 `truncated` 与摘要策略说明。

**风险**：中。观测字段扩展容易推高 token。
**缓解**：完整 Observation 留给解析器，给模型的 PageView 单独压缩。

---

## P2 TargetSpec v2：scope 与 alias 定位

**目标**：解决通用 Web 中最常见的定位问题：同名重复元素、纯图标控件、表格行内操作。

**动作**

1. 新增 `TargetSpec`：
   - `object`：目标元素语义，如 role/text/name；
   - `scope`：包含某文本的容器、表格行、表单、dialog、frame；
   - `relation`：`within`、`near`、`label_for`、`row_contains`；
   - `locator`：最终已验证 locator；
   - `grounding`：观测 lineage。
2. 更新 `planner.resolve`：
   - 先解析 scope，再在 scope 内解析 object；
   - alias 面参与评分，但不能直接让模型写 CSS；
   - 同分仍返回 `target_ambiguous`。
3. 更新 worker `locators`：
   - 支持 scoped locator 的执行期解析；
   - 对 alias 生成的 locator 做唯一验证。
4. 更新工具协议：
   - `click/input` 可带结构化 target hint；
   - 错误返回候选时包含 scope 解释。

**验收门禁**

- 能表达并执行“包含 Blue Top 的卡片里的 View Product”。
- 能表达并执行“搜索表单里的图标提交按钮”，即使按钮没有自然语言文本。
- 对三个同名按钮仍拒绝裸 hint，必须要求 scope。
- 不允许模型直接输出 CSS/XPath 作为常规路径。

**风险**：中高。定位语义扩展会影响 case 形态。
**缓解**：先保留现有 `Target`，新增 `TargetSpec` 后双读一个阶段。

---

## P3 Action/Condition v2：通用动作代数

**目标**：把当前 5 个动作扩展到普通 Web 应用所需的最小通用集合。

**动作**

1. 新增动作：
   - `select`：下拉/combobox；
   - `check` / `uncheck`：checkbox/radio；
   - `scroll_into_view`：长页面与懒加载；
   - `hover`：hover 菜单；
   - `dismiss_dialog`：关闭弹层；
   - `switch_frame` / `switch_tab`：frame 和新窗口；
   - `upload_file`：文件上传。
2. 新增断言：
   - `assert_element`：可见、不可见、enabled、disabled；
   - `assert_attribute`：属性/文本/值；
   - `assert_count`：列表、搜索结果、表格行数量；
   - `assert_network`：可选，用于强业务回调验证。
3. 每个动作补：
   - Go 契约；
   - Python 执行器；
   - 工具 schema；
   - 派生 pre/post 规则；
   - 跨语言 fixture。

**验收门禁**

- SaaS CRUD fixture 能覆盖表单、下拉、确认弹窗。
- 内容站 fixture 能覆盖搜索、分页、详情断言。
- 后台表格 fixture 能覆盖筛选、行内操作、删除确认。
- 所有新增动作必须经过干跑验证后才能进入审批。

**风险**：中。动作越多，契约漂移风险越大。
**缓解**：一类动作一个 PR，先 fixture 后实现。

---

## P4 Planner v2：探索、压缩、修复循环

**目标**：让模型在有限 token 内做稳定规划，并在失败后能修复而不是反复烧钱。

**动作**

1. PageView 改为目标相关视图：
   - 保留与目标词、可交互元素、scope 候选相关的信息；
   - 支持按需展开候选；
   - 明确标注 `truncated` 与省略规则。
2. 干跑失败返回结构化修复上下文：
   - 失败步骤；
   - 当前 URL；
   - 未满足条件；
   - blocker；
   - 可替代候选。
3. 规划器支持修复策略：
   - `drop_last_step` 重建尾部；
   - 重新观测；
   - 缩小 scope；
   - 改用 alias；
   - 请求用户补充必需信息。
4. 成本控制：
   - 每轮观测 payload 预算；
   - 每 run 工具调用预算；
   - 超预算失败信号可回灌。

**验收门禁**

- 同一真实目标的规划 token 明显低于 P0 基线，且不得牺牲通过率。
- 干跑失败事件必须包含足够信息，让模型能做一次有差异的修复尝试。
- 禁止模型重复同一个失败 hint 超过一次。

**风险**：中。过度压缩会让模型缺上下文。
**缓解**：完整 Observation 不丢，只压缩给模型的 PageView。

---

## P5 Blocker + Grounding Recovery：弹层/广告/遮挡/低语义控件

**目标**：把真实 Web 的干扰归为通用 blocker，并把低语义控件编译成可验证 candidate，
而不是让每个场景单独处理或让模型裸写 selector。

**动作**

1. 识别 blocker：
   - cookie banner；
   - modal/dialog；
   - interstitial；
   - sticky header 遮挡；
   - auth wall；
   - captcha；
   - loading overlay。
2. 增加安全恢复动作：
   - 关闭明显可关闭弹层；
   - 等待 loading 消失；
   - 滚动目标到可点击区域；
   - 遇到 auth/captcha 时结构化暂停。
3. 执行结果记录恢复过程：
   - blocker 类型；
   - 尝试过的恢复动作；
   - 是否恢复成功；
   - 恢复失败时给回灌候选。
4. 增加 candidate-first 接地机制：
   - Observation 输出 `action_candidates`，包含 element/form submit/dialog dismiss/navigation/assertion 候选；
   - 每个 candidate 绑定 `candidate_id`、目标 ref、关系、属性 alias 和已验证 locator；
   - 模型可选择 candidate，但不能写 CSS/XPath；
   - 控制面校验 candidate 来自最近一次观测，且 action 类型兼容；
   - 对 `type=button` + JS 的图标提交按钮，优先通过 `form_submit_candidate` 接地。
5. 增加失败签名记忆：
   - 同一 hint/action/page_state 失败后必须换策略；
   - 同一 candidate 执行失败后不得在页面未变化时重试；
   - `input(submit=true)` 不生效时必须改用 form submit candidate 或已验证按钮；
   - 干跑同根因失败两次后停止自动修复，返回结构化失败。

**验收门禁**

- Google vignette / cookie banner fixture 能被识别。
- 可安全关闭的弹层不导致主步骤失败。
- 登录墙和验证码不绕过，必须进入 `awaiting_input` 或明确失败信号。
- 纯图标 `type=button` 搜索提交 fixture 能通过 candidate 机制稳定接地并执行。
- 真实站点 canary 至少要证明：模型不写 selector，也能通过系统给出的 candidate 选择搜索提交按钮。

**风险**：中高。自动关闭弹层可能误点业务按钮。
**缓解**：只对高置信 UI 执行恢复；低置信时返回 blocker，不强点。candidate 只能来自最近一次
Observation，且最终动作仍必须作用在已验证 locator 上。

---

## P6 跨场景基准矩阵与文档对齐

**目标**：用多行业 fixture 和少量真实站点 canary 证明平台通用性。

**动作**

1. 建立 benchmark 目录：
   - `fixtures/sites/ecommerce`；
   - `fixtures/sites/saas-crud`；
   - `fixtures/sites/content-search`；
   - `fixtures/sites/admin-table`；
   - `fixtures/sites/frame-dialog`。
2. 每类 fixture 至少覆盖一条自然语言目标到报告的完整闭环。
3. 保留少量真实站点 canary：
   - 电商真实站点；
   - 一个公开内容站；
   - 一个可控 demo SaaS。
4. 更新 README、CONTRACT、plan 文档。
5. 前端报告页明确区分：
   - run 是否走完整闭环；
   - case 是否通过；
   - 是否有 blocker；
   - 是否生成回灌候选。

**验收门禁**

- 离线 benchmark 全绿。
- 至少 2 个真实站点 canary 有记录，失败也必须有结构化报告。
- 新用户按 README 可以从零跑通本地 benchmark。
- 文档中不再把电商当作唯一目标场景。

**风险**：中。真实站点不稳定。
**缓解**：真实站点只做 canary，不作为本地必过门禁；必过门禁只依赖离线 fixture。

---

## 进度与度量

每个阶段结束时记录到 `plan/10-baseline.md` 或 `docs/execution-log.md`：

| 指标 | P0 基线 | 目标 |
|---|---|---|
| 离线门禁耗时 | 待测 | ≤ 10 分钟 |
| 本地 benchmark 场景数 | 1（电商 fixture） | ≥ 5 |
| 真实站点 canary 数 | 1 | ≥ 2 |
| 平均规划 token | 待测 | P4 后下降 |
| 目标定位失败类型 | F1/F2 已知 | 可结构化解释 |
| blocker 类型 | 未建模 | ≥ 5 类 |
| 契约定义点 | Go/Python 镜像 | 单一来源 + 一致性 fixture |

## 明确禁止的做法

- 禁止新增行业专用 action，例如 `add_to_cart`、`create_ticket`、`publish_article`。
- 禁止让模型手写 CSS/XPath 作为常规方案。
- 禁止为了绕过 blocker 直接点击页面坐标。
- 禁止只在真实站点上验证新能力；必须先有离线 fixture。
- 禁止新增契约字段但不补 Go/Python 一致性测试。
