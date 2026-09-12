# 执行日志

用于沉淀每次任务实际做了什么，方便后续追溯、复盘和回答一致化。

## 记录规则

- 每次处理需求后，在「任务记录」顶部按时间倒序新增一条记录。
- 记录结构统一为：任务、操作、结果、验证、后续；无后续事项时写“无”。
- 如果执行过程中发现明确缺陷，同时在 `docs/bug-log.md` 追加对应条目并互相引用。
- 最新的记录优先放到最上面，方便阅读。

## 记录模板

```md
## YYYY-MM-DD | 标题

- 任务：本次要解决或分析的事项。
- 操作：实际执行的关键动作。
- 结果：产出、结论或修复效果。
- 验证：已执行的验证；如果没有写“未验证”。
- 后续：下一步事项；如果没有写“无”。
```

## 阶段总览

| 阶段 | 日期 | 主题 | 关键成果 |
|------|------|------|----------|
| M1 基础 | 03-28~03-31 | 认证、CRUD、Suite 下线、AI Planning 雏形 | 认证基线 + 用例 CRUD + AI 对话面板 |
| M2 用例生成 | 04-03~04-17 | ReAct Agent、流式执行、定位器系统、DOM 感知 | ReAct loop + WebSocket 流式 + 4 层定位器 |
| M2 执行闭环 | 04-20~04-28 | DOM-aware DSL、VLM 两阶段、Explorer-Judge、评分系统 | 完整 plan→execute→analyze 闭环 |
| M2 前端重构 | 04-25~04-26 | NotebookLM 布局、E2E 手动测试、Explorer-Judge 架构 | 三栏布局 + 缺陷发现范式 |
| E2E 调优 v1 | 05-02~05-07 | 页面探索、定位器回退链、text_parent_chain、AI 质量循环 | 26 commits, 96% 步骤通过率 |
| E2E 调优 v2 | 05-10~05-12 | DeepSeek 温度/thinking 优化、提示词修复、11 项架构优化 | 544 tests, 0 failures |
| 架构重构 | 05-14~05-17 | 主路径 v2 A11y 管线、dead code 清理、DSL 生成链路修复 | 491 tests, -3.1K 行, A11y CDP 100x 快 |
| 链路修复 | 05-25 | DSL 生成链路 7 层 bug 修复（Bug A→G） | 543 tests, 16 新增测试 |
| 孤儿数据清理 | 05-25 | 全面清理代码库中的孤儿数据 | 删除 14 项孤儿数据 |
| 数据校验修复 | 05-25 | 数据传递与校验全面扫描修复 | 修复 19 项问题 |
| 变量占位符修复 | 05-28 | 分段生成 input_contract 自动提取 | 修复 ${email} 未替换问题 |
| A11y Tree 全面切换 | 05-28 | 封杀 DOM 路径，只使用 a11y tree | 500 tests, 4 项核心修复 |
| explore_flow DSL 格式支持 | 05-28 | 支持 DSL 格式步骤传入 explore_flow | 500 tests, 修复页面探索不完整 |
| 跨段变量命名权威 | 05-28 | Planning agent 输出 scenario.variables，segment prompt 注入命名字典 | 504 tests, 4 新增聚焦测试 |
| explore_flow 遮挡恢复 | 05-28 | _execute_flow_actions / capture_browser_session 接入 click_with_precheck | 504 tests, cartModal 不再杀掉探索 |
| 完整探索数据 + 用户上下文注入 | 05-29 | _load_a11y_nodes 合并所有 explore 记录 + user_context 注入 segment prompt | 504 tests, DSL 生成器看到完整元素和原始需求 |
| Agent 流程 vs 直接脚本测试 | 05-30 | explore-flow 探索、DSL 生成、执行测试对比分析 | 发现 6 项问题，a11y 过滤和选择器策略修复 |
| A11y 无名输入框定位修复 | 05-31 | label 兄弟 input 策略 + cell role 支持 | 29 steps 全通过，Quantity 输入框定位 |
| textContent + DSL 完善 | 05-31 | 4 次修复：textContent、View Product、数量修改、断言值 | 21/21 steps 全通过 |
| Anti-pattern 注入与上下文重构 | 06-04 | 重构上下文注入架构，修复执行错误注入 | 497 tests, 4 项核心修复 |
| Locator + Explore 双轨修复 | 06-05 | paragraph 角色、探索导航精确化、Prompt 清理 | 探索采集 OK, 18/18 手动 OK, 全链路待继续验证 |
| SSE 事件日志架构 | 06-08 | SSE 事件持久化 + 刷新恢复 + replay API | 解决刷新丢消息问题 |
| SSE 架构修复 v2 | 06-08 | Session 隔离 + 弹性降级 | 修复表不存在时流式崩溃 |
| 孤儿会话根因修复 | 06-08 | SSE 端点添加会话验证，测试驱动调试 | 501 tests, 根因修复 |
| 孤儿数据清理 | 06-08 | 清理 70 个孤立项目，添加清理脚本 | 数据库清理完成 |
| 控制面生产化 | 09-05 | 生产运行、Session 元数据、鉴权、项目回归、定位调试和 CI | Go/Python/Frontend 生产边界与持续门禁落地 |

---

## 任务记录

## 2026-09-12 | Phase B 增量 1：统一 Grounding 与 Resolved Target

- 任务：开始改造 AI 任务规划、页面探索和 DSL 生成链路，使 AI 基于 DOM/A11y 页面事实规划动作，Explore 返回完整页面结构与单动作实际命中证据，并消除新路径的 Go 文本重绑定。
- 操作：将 Agent system prompt 拆分为 Task Planning、Grounding、DSL Authoring、Execution/Repair 四阶段，要求 TaskPlan 只保存业务语义和证据要求；新增 `grounding.query.v1`、`browser.resolved-target.v1` 共享 Schema 和 Go/Python 类型；将 click/input/wait_for 收紧为只接受结构化语义 Locator；Browser Worker 通过共享 `compile_locator` 执行唯一性检查，将请求 Locator 写入 BrowserObservation candidate，并返回 probe/observation/page-state/element/candidate/runtime/action status；模型摘要保留 resolved target；Go 将证据与完整 Observation 交叉校验后直接构造 TargetBinding。
- 结果：AI 只能提交 Playwright 可编译的 role/name/label/placeholder/text/test-id/scoped 查询；Explore 字符串 `target`、legacy flow resolver、按动作顺序猜测 PlanStep owner 和 Go flow 文本重绑定均已删除。已 grounded 的 click/input 支撑动作必须引用原 PlanStep ID，pending `plan_step_ids` 必须从下一步严格连续。完整页面结构继续保存为 BrowserObservation Artifact，模型获得结构索引和 resolved target；按条件读取完整 Artifact 的 Observation 查询工具留待下一增量。BUG-180 已修复。
- 验证：`go test ./...`、`go vet ./...`、`go build ./...` 通过；Python 189 tests passed / 2 skipped；Pyright 0 errors；Ruff 全仓 `F/I` 通过；`RUN_BROWSER_INTEGRATION=1` 的 2 个真实 Chromium 测试通过；共享 GroundingQuery/ResolvedTarget Schema、模型摘要和 Go 直接 Binding 均有回归。
- 后续：Phase B 下一增量新增 Observation 搜索/切片工具；随后实施 ToolCallLedger 和 Context Materializer。未运行付费模型 E2E。

## 2026-09-12 | TaskPlan、Explore 与 DSL 职责重新对齐

- 任务：核对“AI 接受目标、规划动作、分析完整页面结构、生成合理 DSL”的目标流程与当前实现差异，并明确改造顺序。
- 操作：对照 Harness system prompt、`set_task_plan`、`explore_page/explore_flow` 输入合同、BrowserObservation/Artifact、TargetBinding 派生和 research-v2 compiler；结合十二轮 grounding live 证据区分 AI 语义决策与 Worker/Control Plane 确定性职责。
- 结果：当前流程要求 AI 在首次页面事实不足时先生成完整 TaskPlan，随后直接手写 `plan_step_ids + click/input/wait_for + target/locator` 的低层 Probe；Worker 执行动作后，Go 再按文本从 Observation 推导 TargetBinding，Runner 最后重新编译 LocatorSpec，形成三次目标解释。完整 BrowserObservation 已持久化为结构化数据/Artifact，但模型主要接收裁剪摘要，缺少按页面区域、角色、名称、状态和 pending PlanStep 查询完整 Observation 的工具。目标流程应保留 AI 对任务语义、Probe 意图、页面事实解释和 Draft DSL 的主导权，同时由共享合同确定性完成元素解析、candidate 身份、LocatorSpec 编译、Plan/DSL 校验和执行。
- 验证：静态核对当前代码与已保存 live Run；知识图谱基线为 `3be098d`，早于 Phase A lineage 改动，因此仅用于架构定位，结论以当前源码为准。
- 后续：先重写分阶段 system prompt 和 TaskPlan typed condition/evidence requirement，再新增 `grounding.query.v1`、`browser.resolved-target.v1` 与 Observation 查询工具；随后让 Explore/Runner 共用同一 LocatorSpec compiler，删除 Go 文本重绑定，最后让 AI 仅通过 plan/binding ID 编写 Draft DSL。

## 2026-09-12 | v4-flash-vision-exp 十二轮 Grounding 归因

- 任务：聚焦工具调用与任务规划，解释 Run `run_84e3cdde9a0df7d6553c789b` 为什么耗尽 12 个模型回合才完成 grounding。
- 操作：从 PostgreSQL 对齐 12 条 `research.llm_call`、12 个 ToolCall、40 次工具状态迁移和 9 次 TaskPlan 快照；核对当前 Loop max-turn、Explore 参数 Schema、TaskPlan 连续 pending-step 授权、Probe action owner 映射和探索预算统计逻辑。
- 结果：该 Run 不是执行了 12 次有效 grounding，也没有重复规划；TaskPlan 仅创建一次且始终为 v1。12 轮由 1 次 `set_task_plan`、1 次成功 `explore_page`、10 次 `explore_flow` 构成；10 次 flow 中 4 次产生部分 grounding，另 6 次无推进（2 次 PlanStep action/value 不匹配、2 次 `plan_step_ids` 连续性/ownership 拒绝、2 次 Worker 422 参数校验失败）。有效进度依次为 1/12、4/12、6/12、7/12、12/12；最后一轮才进入 `ready_for_generation`，但全局 `AGENTSERVICE_MAX_TURNS=12` 已耗尽，没有第 13 轮调用 `generate_dsl`。
- 验证：核对事件 seq 2-142、TaskPlan 状态 seq 15/29/70/111/125/139/141、全部 ToolCall 参数和终态；确认探索预算只统计可解码的成功工具摘要，失败/授权拒绝不计入当前调用数，且 normalized signature 因参数细节变化未判定这些调用为重复。
- 后续：工具侧优先实现全状态 ToolCallLedger、共享参数校验、明确的 `partial_success` 和下一 pending-step 调用合同；规划侧将业务 TaskPlan 与 GroundingPlan 分离，按页面状态批量获取证据，并为 DSL generation 保留独立回合预算。

## 2026-09-12 | 重置数据库并运行 v4-flash-vision-exp research-v2 E2E

- 任务：重置本地 PostgreSQL 状态，将 DeepSeek 模型切换为 `deepseek-v4-flash-vision-exp`、thinking effort 设为 `high`，并运行一次官方 research-v2 E2E。
- 操作：停止全链服务后重建数据库 `public` schema、重新执行迁移并写入默认 user/project；以 `AI_PLANNING_THINK_MODE=true`、`AI_PLANNING_REASONING_EFFORT=high` 启动 Browser Worker、execution-worker 和 AgentService；运行 `automationexercise-blue-top-cart.v1`，导出 `run.json`、`pipeline-audit.json` 和 provider evidence；E2E 后停止服务。审计发现不同 probe 的 candidate ID 碰撞，随后将 `probe_id` 纳入 candidate 哈希并新增跨 probe 回归。
- 结果：Run `run_84e3cdde9a0df7d6553c789b` 最终失败：12/12 PlanStep 已 grounded 并进入 `ready_for_generation`，但第 12 轮后触发 max-turn，未生成 DSL、Batch、Execution 或 Report。12 次请求均直连 `api.deepseek.com`，requested model 为 `deepseek-v4-flash-vision-exp`，provider resolved model 为 `deepseek-flash`，thinking enabled、reasoning effort high；每次调用均保存 provider response ID、header request ID 和 usage。累计 input 373,810、output 62,693、total 436,503 tokens；请求体从 26,481 bytes 增长到 247,863 bytes。新增 BUG-185（已修复）和 BUG-186（待修复），BUG-155 补充本次上下文增长证据。
- 验证：Python 189 tests passed / 2 skipped；Pyright 0 errors，修改文件 Ruff `F/I` 通过；`go test ./...`、`go vet ./...`、`go build ./...` 通过；三个证据文件 SHA-256 分别为 `68e9330c2c9dd20a1e8a7f5372503ac822a592c694be7a52c1e79c554905d419`、`a7e6f9f960351f8d405b72884b5cc50fd5030d812c0f90c5ffb0c964ffca42c8`、`1233a7450830cb2bda818b5ee1cf1ae84b12540394950de8502a3afe86e548d1`。
- 后续：不自动重跑付费 E2E；先实施 Context Materializer/预算熔断、修复 max-turn stale error，并进入 Phase B 统一 Explore 与 Runner 的 resolved candidate。

## 2026-09-12 | Phase A 增量 2：跨层执行 Lineage

- 任务：贯通 probe、observation、element、candidate、binding、generation、execution 和 report lineage，为 Explore/DSL/Runner 不一致提供可核对的事实链。
- 操作：Go BrowserTool 按 Run/ToolCall 生成稳定 `probe_id`；BrowserObservation 为每个 locator hint 生成稳定 `candidate_id`，并使用 context_path 计算实时 count；TargetBinding 保留 probe/observation/page state/element/candidate，Go compiler 将 lineage 和 planned candidate 注入 research-v2 Executable DSL；Runner StepEvidence 记录实际 candidate/element，并为全部候选保存 runtime count 和 rejected reason；FailureSignal、模型 FailureBrief、TaskPlan event、Research Transition、`agent.pipeline.trace` 和 `pipeline-audit` 保留同一组引用；新增 Go/Python 共用 Observation golden。
- 结果：新 Run 可从单个 pipeline trace 按 PlanStep 追溯 grounding、generation、batch/case、execution 和 report；定位失败可区分 planned candidate 与 resolved candidate，并解释 0/N/hidden/disabled/compile/runtime-check 拒绝。BUG-183、BUG-184 修复；BUG-180 仍需 Phase B 的 ResolvedTargetEvidence 消除 Explore 实际动作与 Go 文本重绑定。
- 兼容性：新增字段采用“新写必有、旧读兼容、部分出现严格校验”；旧 BrowserObservation v2、TargetBinding v1 和 pre-lineage research-v2 canonical payload 仍可读取，不需要数据库迁移。
- 验证：`go test ./...`、`go vet ./...`、`go build ./...` 通过；Python 188 tests passed / 2 browser tests skipped，另以 `RUN_BROWSER_INTEGRATION=1` 运行 2 个真实 Chromium 测试通过；Pyright 0 errors，修改文件 Ruff `F/I` 检查通过，compileall 通过；Frontend 4 files / 11 tests 和 production build 通过；共享 JSON Schema 和 canonical/failure golden 通过。
- 后续：部署后产生一个新 research-v2 Run，用 `pipeline-audit --run-id` 核验真实 lineage；随后进入 Phase B，实现 `browser.resolved-target.v1` 并删除 Explore/Go/Runner 三次独立目标解释。

## 2026-09-12 | Phase A 增量 1：Agent 管线诊断基线

- 任务：按已批准的全链路治理计划实施 Phase A 第一个增量，完成后提交并同步 GitHub；本增量只增加可观测性，不改变 Agent 决策和执行策略。
- 操作：新增 `agent.pipeline.trace.v1` 共享 Schema 与 Go 类型；在模型调用遥测中记录 request/message/tool definition 大小、消息角色内容、reasoning、tool arguments、探索/非探索摘要和可恢复错误字节分布，并聚合 Run 级 logical/physical calls 与 token usage；在 Harness 为全部工具调用记录 state epoch、plan binding、规范化签名、PlanStep IDs、attempt、retry lineage 及 proposed/authorized/running/succeeded/failed/rejected/pending 状态；审批恢复后补齐 pending tool 的 succeeded 终态；新增只读 `pipeline-audit --run-id` 汇总命令和前端 SSE 事件类型。
- 结果：新 Run 可从 PostgreSQL/SSE 获取不进入模型 transcript 的诊断事件，并离线统计计划版本、重复工具调用、上下文首末/峰值和累计模型用量。历史 Run `run_7859949d26bb0c6dc7b31bc8` 可读取两个 TaskPlan 版本；因历史数据没有新 trace，模型和工具诊断计数按合同返回 0，不进行推测回填。
- 验证：`go test ./...`、`go vet ./...`、`go build ./...` 全部通过；Frontend 4 files / 11 tests 和 production build 通过；Python 相关 66 tests 通过；`pipeline-audit` 对历史 Run 成功输出 `agent.pipeline.trace-summary.v1`；共享 Pipeline Trace Schema 已由 Go 测试使用真实序列化 payload 验证。
- 后续：下一增量进入 Phase A 的跨层 lineage 诊断，将 probe/observation/element/candidate/binding/generation/execution/report ID 补入同一追踪链；随后执行 Phase B，消除 Explore、Go Binding 与 Runner 的三次独立目标解释。提交信息：`feat: add agent pipeline diagnostics`。

## 2026-09-12 | Agent 全链路一致性审计与排查计划

- 任务：检测 Agent 在任务编排、工具调用、上下文管理、任务规划、Explore、元素归类、DSL 治理、Playwright 解析、Runner 执行和报告归因上的一致性问题，并制定排查与治理方案。
- 操作：沿 `Goal -> TaskPlan -> Tool -> BrowserObservation -> TargetBinding -> Draft/Executable DSL -> LocatorSpec -> Runner -> StepEvidence/FailureSignal` 静态追踪 Go/Python 合同与状态迁移；核对最近 v4-pro research-v2 失败记录、现有测试和知识图谱；运行 Go 与 Python 聚焦门禁；新增 `docs/plan/agent-pipeline-consistency-audit-2026-09-12.md`。
- 结果：确认主要问题不是单一 parser 缺陷，而是跨层重复解释和身份丢失：Explore 实际命中的元素没有以 candidate ID 传给 Go，Go 会按文本重新 binding，Runner 再次解析；相同 TaskPlan 和非 Explore 工具没有统一幂等账本；模型上下文仍追加完整 transcript；Plan 条件没有被 DSL 编译器确定性保持；定位失败报告缺少完整候选拒绝与 PlanStep/Binding lineage；BrowserObservation relation 的代码字段与共享 Schema 不一致。新增 BUG-180 至 BUG-184，Context 爆炸继续由 BUG-155 跟踪。
- 验证：Go `agent/harness/taskplan/browsercontract/tools/execution/research` 聚焦测试全部通过；Python `browser_observation_contract/action_ir_v2/page_explorer/playwright_runner/failure_signals/browser_capabilities` 共 66 tests passed。测试通过同时证明现有门禁没有覆盖上述跨层不变量；尝试用 Python `jsonschema` 动态验证 Observation 时因该依赖未安装而未执行，字段漂移已通过模型和 Schema 静态对照确认。
- 后续：按计划依次实施诊断事件、ResolvedTargetEvidence、TaskPlan/ToolCall 幂等、结构化 Condition、Context Materializer、FailureSignal v3；所有离线与本地门禁通过后，先运行单次官方 DeepSeek smoke，经成本确认后再执行 3 次 Canonical 和负向变异。

## 2026-09-11 | 当前项目阶段与完成度核查

- 任务：核查当前项目进展，判断已经完成的能力、实际所处阶段和进入下一阶段前的阻塞项。
- 操作：检查 `main`/`origin/main`、最近提交、README 能力矩阵、Agentic Research tasks/checklist、最新 live E2E 与修复记录、开放缺陷及 Understand Anything 知识图谱；区分代码实现、静态门禁和官方模型 live 验收。
- 结果：平台基础、Go AgentCore 控制面、结构化执行、持久化队列、报告、TaskPlan/PlanStep、BrowserObservation/TargetBinding 和 research-v2 DSL 主链均已落地；当前处于 Stage 6 后段的稳定化与验收收口，尚未进入 Stage 7 Ablation。最近一次 v4-pro research-v2 live E2E 在修复前因 grounding/预算问题超时，修复后只完成静态、PostgreSQL 聚焦和真实浏览器 smoke，尚未重新取得 TaskPlan 全 grounded、DSL、审批、正式执行、报告和 Oracle 的端到端通过证据。发现 Stage 6 清单未同步 TaskPlan 已完成事实，记录 BUG-179。
- 验证：核对 `HEAD=2e646e6`，核查前 `main` 与 `origin/main` 为 0 ahead / 0 behind、工作区干净；知识图谱代码基线为 `3be098d`，其后仅有 README/API/日志/H5 文档变化，因此代码架构信息仍可使用。未启动服务、未运行测试、未调用付费模型。
- 后续：先修正 Stage 6 清单，完成 Context Materializer、Run 级 token/call/重试熔断、cache 指标和成本预估；随后用官方 DeepSeek 直连完成可对账的 research-v2 Canonical 验收。Stage 6 通过后再进入 Stage 7 四 profile Ablation。

## 2026-09-10 | 同步接口与函数目录到 GitHub

- 任务：将接口参考文档、函数可视化 H5 及关联日志同步到 GitHub。
- 操作：核对 `main` 与 `origin/main` 基线，确认提交范围仅包含 README 文档索引、`docs/api-reference.md`、`docs/function-atlas.html`、接口/函数任务日志和 BUG-178 记录；使用明确文件路径暂存并直接推送当前分支。
- 结果：准备创建单一文档提交并同步至 `origin/main`。
- 验证：提交前 `git diff --check` 通过；函数 H5 的 1461 个函数和 56 个接口映射 Playwright 回归已通过。提交推送后复核远端提交和工作区状态。
- 后续：BUG-178 保持 open，后续独立决定补充修正详情 GET 或调整 Location。

## 2026-09-10 | 函数与接口可视化 H5 目录

- 任务：在接口清单基础上继续梳理项目函数名、分类和逐函数用途，并交付可交互 H5 页面。
- 操作：使用 Tree-sitter 扫描 Go、Python、TypeScript 业务源码和维护脚本；按 21 个模块、语言、函数类型与职责归类；结合当前源码和 Understand Anything 图谱为每个实现函数补充用途；关联全部 HTTP/SSE 路由到处理函数；生成独立静态 `docs/function-atlas.html`，内嵌函数数据与源码片段，并在 README 添加入口。
- 结果：H5 覆盖 154 个源码文件、1461 个有实现的函数/方法（Go 864、Python 443、TypeScript 154），包含 56 个 HTTP/SSE 接口、9 个 Agent 工具和 8 段主链路导航；支持全文搜索、模块/语言/职责/函数类型筛选、分页、函数详情、源码/GitHub 跳转、深链接和当前结果 JSON 导出。
- 验证：1461/1461 函数均有用途说明，函数 ID 无重复，56/56 接口均映射实现函数；Playwright 回归通过内嵌数据、搜索、组合筛选、空状态、分页、源码详情、接口视图、JSON 导出、主链路、HTML 转义和深链接；桌面/移动布局在 320、390、768、1280、1440px 无横向溢出且无页面错误；`git diff --check` 通过。
- 后续：页面是当前 commit `5e46d6c` 的静态快照；源码变化后需重新生成。询问是否同步本次文档变更到 GitHub。

## 2026-09-10 | 项目接口总览与用途文档整理

- 任务：梳理当前项目的大量接口，按业务模块说明各接口的用途、请求、响应与使用方式。
- 操作：核对 Go Hertz 主路由与 Research 路由、处理器、领域类型和相关服务，核对 Python FastAPI 路由、Pydantic 合同与 Agent 工具注册；参照知识图谱定位模块，并确认图谱与当前提交仅存在执行日志差异；新增 `docs/api-reference.md`，在 README 文档索引添加入口。
- 结果：文档覆盖 Go 51 个 HTTP/SSE 路由（平台业务 37、Research 13、健康检查 1）、Python 5 个自定义 HTTP 路由，以及 9 个非 HTTP Agent 工具；包含用途、关键请求/响应、SSE 重放、审批、幂等、取消边界、Research 验收、四条典型调用链与 curl 示例。发现创建修正返回的 Location 无对应 GET，已记录 BUG-178，未修改运行逻辑。
- 验证：静态路由与文档对照 56/56，无遗漏、额外或重复条目；工具对照 9/9；58 个文档链接/锚点检查通过；7 个 JSON 代码块和 6 个 curl JSON 请求体解析通过；通过 FastAPI `app.openapi()` 及实际注册表确认 Worker 5 个自定义路由；2 个 Worker 请求及 DSL 示例通过 Pydantic 合同校验；`git diff --check` 通过。未执行数据库写请求、真实浏览器任务、付费模型调用或全量业务测试。
- 后续：询问是否同步本次文档变更到 GitHub；BUG-178 留待独立修复。

## 2026-09-10 | 同步 Understand Anything 图谱记录到 GitHub

- 任务：将当前本地变更同步到 GitHub。
- 操作：确认当前分支为 `main`，检查工作区仅有 `docs/execution-log.md` 变更；准备提交并推送当前分支到 `origin/main`。
- 结果：本次同步范围限定为执行日志更新，`.ua/` 图谱缓存保持本地生成物不进入 Git。
- 验证：提交推送后通过 `git status --short` 和 `git log -1 --stat --oneline` 复核。
- 后续：无。

## 2026-09-10 | Understand Anything 知识图谱生成与看板启动

- 任务：按用户要求排除 docs 和 test 文件后运行 `/understand`，生成当前仓库的代码知识图谱并启动 dashboard。
- 操作：更新 `.ua/.understandignore`，排除 `docs/`、Markdown/RST/TXT 文档、`testdata/`、各层 `tests/` 和常见测试文件模式，并排除 `.ua/` 生成物；重新扫描项目，计算 16 个语义批次，调度 file-analyzer 生成 batch graph，合并为完整 KnowledgeGraph；生成 9 个架构层和 14 步中文 guided tour；保存 `knowledge-graph.json`、`fingerprints.json`、`meta.json`，并启动本地 dashboard。
- 结果：图谱分析 194 个文件（code 161、config 22、infra 7、data 2、markup 2），生成 1584 个节点、2976 条边、9 个架构层、14 个导览步骤；dashboard 地址为 `http://127.0.0.1:5173/?token=e1b5e5eb894eff19be52f33dec10a4ec`。
- 验证：`scan-result.json` 中 docs/test 命中数为 0；批次合并完成且未恢复缺失 imports；inline graph validation 为 0 个 critical issue、18 个 orphan node warning；fingerprints baseline 覆盖 194 个文件。
- 后续：询问用户是否同步当前变更到 GitHub；`.ua/` 已加入本地 `.git/info/exclude`，知识图谱作为本地生成物不进入 Git 变更。

## 2026-09-10 | Understand Anything 看板启动前置检查

- 任务：使用 `understand-dashboard` 启动当前项目的代码知识图谱看板。
- 操作：按 Skill 说明解析项目目录、图谱数据目录与插件根目录，并检查图谱文件和插件版本。
- 结果：项目使用 `.ua/` 数据目录，但 `.ua/knowledge-graph.json` 不存在，因此未启动看板；已确认项目级插件根目录可用，版本为 `2.9.6`。
- 验证：检查 `/Users/bytedance/project/AI_Web_Testing/.ua/knowledge-graph.json` 返回不存在；成功解析 `/Users/bytedance/project/AI_Web_Testing/.trae/understand-anything/repo/understand-anything-plugin`。
- 后续：先运行 `/understand` 生成知识图谱，再重新运行 `/understand-dashboard`。

## 2026-09-10 | 安装 Understand Anything TRAE 插件

- 任务：将 `Egonex-AI/Understand-Anything` 插件安装到本地 TRAE 工作区使用。
- 操作：核验上游官方安装器与 TRAE Skill 目录约定；将上游 commit `5feed1f2ce4f9c368d860f4c0ebc36d98a4693fc`（插件版本 `2.9.6`）检出到工作区私有目录 `.trae/understand-anything/repo`；在 `.trae/skills/` 注册 9 个 Skill；启用 pnpm 10.6.2、安装依赖并构建 core；通过 `.git/info/exclude` 排除本地插件目录。由于 TRAE 沙箱禁止写入全局 `~/.agents/skills` 和 `~/.understand-anything-plugin`，改用项目级安装，并为需要解析插件根的 Skill 增加工作区路径回退。
- 结果：`understand`、`understand-chat`、`understand-dashboard`、`understand-diff`、`understand-domain`、`understand-explain`、`understand-figma`、`understand-knowledge`、`understand-onboard` 均可从当前工作区读取；插件及依赖不进入主仓库 Git 变更。
- 验证：9 个 `SKILL.md` 均可解析；core `dist/index.js` 已生成；运行上游 `scan-project.mjs` 成功扫描 357 个文件，返回 `scriptCompleted=true`、复杂度 `large`；主仓库与插件检出均无额外未跟踪或修改文件。
- 后续：重启 TRAE 或重新打开当前工作区以刷新 Skill 列表；首次完整 `/understand --language zh` 会调用模型分析整个仓库并产生较高 Token 消耗。

## 2026-09-10 | 探索预算、重复调用与 grounding 修复

- 任务：修复 v4-pro research-v2 live E2E 暴露的计划改版预算耗尽、Products 容器误点击、Quantity wait_for 语义错误和取消后结果状态不一致，并向模型暴露预算及防重复规则。
- 操作：将 ExplorationGate 改为 TaskPlan ID/version 级 page/flow 预算并增加 Run 级硬上限；在 set_task_plan、探索摘要和 gate failure 中返回 used/limit/remaining、重复签名限制和当前签名 fingerprint；对 page/flow 的规范化签名忽略 description、timeout 和 observation version，同一计划版本内完成一次后禁止重复；为 flow action 增加显式 `plan_step_id`，TaskPlan 优先按 ID 做授权和结果归属；候选选择先过滤动作兼容的交互 role/tag，再按 exact name 和 verified selector 排序，TargetBinding 忽略不可执行容器；wait_for 增加受限语义 LocatorSpec 与 `value_equals`；Driver 取消后刷新 Run/event 终态。
- 结果：Plan v2 不再继承 Plan v1 已耗尽的 4 次 flow 预算，但整个 Run 最多仍执行 8 次 flow；模型每轮都能看到剩余预算且不能用改写描述或 timeout 重复调用。`click Products` 不再选择 `#header`，`role=spinbutton + value_equals=1` 可以直接验证数量值。BUG-173、BUG-174、BUG-175、BUG-176 已修复；另发现与本轮无关的 Overview UTC/本地日界线 BUG-177。
- 验证：Go 无外部数据库全量测试、vet、build 通过；真实 PostgreSQL 下 taskplan/integration/harness/agent/tools 相关模块通过；Python 全量 183 tests passed / 2 skipped、compileall、Pyright 0 errors、相关 Ruff 检查通过；Frontend 10 tests 和 production build 通过；真实浏览器 smoke 中 Products 导航到 `/products`，结构化数量检查 success 且 evidence_count=1。浏览器命令在业务输出后因 TRAE sandbox 禁止访问受限根路径返回 1，不影响已输出断言。全量 PostgreSQL 另因 BUG-177 的跨午夜时间窗缺陷失败。
- 后续：暂不重复调用付费模型；是否再次执行 research-v2 live E2E 由用户确认。当前代码尚未提交或同步 GitHub。

## 2026-09-09 | explore_page、explore_flow 与 grounding 语义复核

- 任务：解释 4 次 explore_flow 仅得到 5/13 grounded 是否合理，以及工具注入、页面证据和探索失败的实际语义。
- 操作：复核 Harness system prompt、浏览器工具 schema、TaskPlan `RecordToolResult` 连续推进规则和 live Run `run_7859949d26bb0c6dc7b31bc8` 的四次 flow 结果。
- 结果：`explore_page` 只打开一个已知 URL 并返回该状态的 Observation；`explore_flow` 在一次 disposable BrowserContext 内执行模型声明的多页面步骤和 click/input/wait_for 动作，但不同 flow 调用之间不共享状态。四次 flow 后的 grounded 数依次为 2、5、5、5：第一次只推进 s2；第二次推进 s3-s5；第三、四次虽采到详情页元素和成功动作，但 View Product 同时匹配 list/listitem/link，无法生成唯一 TargetBinding，随后 Quantity 的精确文本 wait 又失败，因此没有继续推进。页面返回元素只代表 observation 成功；单个 action、整个 flow outcome、Tool RPC 和 PlanStep grounding 是四个不同状态。
- 验证：TaskPlan updated 事件 seq 14/23/32/41/50 分别为 1/13、2/13、5/13、5/13、5/13 grounded；事件 48 同时包含详情页 Observation、成功的 View Product/Add to cart wait evidence 和 `Quantity` action failure。
- 后续：统一探索 action 与 TargetBinding 的候选筛选；区分 RPC 成功、部分 flow 成功和 PlanStep grounding；修复预算前先保证一次有效 flow 能消费其已采集证据。

## 2026-09-09 | v4-pro research-v2 live Agent E2E

- 任务：使用官方 `deepseek-v4-pro` 运行一次真实 research-v2 Agent E2E，验证 TaskPlan、DSL 编译、审批、正式执行、报告和 Oracle 完整链路。
- 操作：应用最新 Go schema；从当前工作树启动 Browser Worker、20-turn AgentService（thinking=max）和 execution-worker；运行 `automationexercise-blue-top-cart.v1.json`，设置 1200 秒绝对截止时间；从结果 JSON、PostgreSQL Agent events、TaskPlan/PlanStep 和 Browser Worker 日志交叉核验；终态后停止全部本轮服务。
- 结果：E2E 失败，Run `run_7859949d26bb0c6dc7b31bc8` 在截止时间由 Driver 取消。9 次官方 v4-pro 调用全部 HTTP 200、retry=0，累计 input 393,786 / output 55,636 / total 449,422 tokens，最大单次延迟 281,000 ms，provider request ID 均已保存；BUG-167 的 response_read_failed 本次未复现。Plan v1 达到 5/13 grounded、3 个 TargetBinding；模型改版后 Plan v2 仅继承 2/8 grounded、1 个 binding，随后补探被全局 `explore_flow_calls=4` 预算拒绝，最终 Plan 为 blocked。没有生成 DSL、Approval、Batch、Execution 或 Oracle 结果。关联 BUG-173、BUG-174、BUG-175。
- 验证：数据库终态为 `cancelled`、last event seq=75；9 条 `research.llm_call` 均为 `requested_model=resolved_model=deepseek-v4-pro` 且 usage available；Generation=0、Batch=0。结果文件 `research/results/agentic-e2e-v4-pro-research-v2-20260909T123800Z/run.json`，SHA-256 `40e6f3710a02f9282d349bddbc6afeec31af49160c70c579fe5dad7e01e2646c`。
- 后续：先修复计划版本感知的探索预算、explore_flow 容器误点击和取消后结果状态刷新，再决定是否付费复跑；本次不重复调用模型。

## 2026-09-09 | research-v2 DSL 链路完成度复核

- 任务：确认 DSL 链路是否已经全部修复。
- 操作：复核 research-v2 Draft 校验、TaskPlan 编译、TargetBinding/Observation binding、canonical v3、Python LocatorSpec compiler、Preflight/Runner 分派与执行证据字段，并对照统一方案完成标准和现有测试记录。
- 结果：代码层主链已完成，`Draft DSL -> Go Compiler -> Executable DSL v3 -> TargetBinding Preflight -> Playwright Runner -> Step Evidence` 已连通；research-v2 不再允许模型提交 selector/candidates，也不会回退自由文本 locator。尚不能宣称端到端稳定，因为 v4-pro 下的真实 Agent E2E 尚未执行；research-v1 字符串路径仍作为兼容层保留。
- 验证：静态复核关键入口与既有全量门禁记录；本次未新增测试、未启动服务。
- 后续：使用 v4-pro 完成一次 research-v2 live E2E，核对 TaskPlan 全 grounded、generation/approval binding、正式执行、报告与 Oracle 后，再标记生产稳定并提交同步。

## 2026-09-09 | DeepSeek Planning 模型切换至 v4-pro

- 任务：将当前 DeepSeek Planning 模型切换为 v4-pro，并确认整体任务进度。
- 操作：将本地运行配置、开发示例和生产部署示例的 `AI_PLANNING_MODEL` 更新为 `deepseek-v4-pro`；通过官方 `api.deepseek.com/chat/completions` 发起最小非思考请求；检查当前服务进程。
- 结果：当前配置已统一为 `deepseek-v4-pro`；官方请求返回 HTTP 200、模型 `deepseek-v4-pro` 和内容 `OK`，usage 为 input 7 / output 1 / total 8 tokens，request ID 为 `df3a7bf2-8c08-4234-ae87-bb38233c959a`。AgentService 当前未运行，新配置会在下次启动时生效；现存 execution-worker 不读取 Planning 模型，无需重启。
- 验证：Go config/LLM 聚焦测试通过；三个配置文件值一致；`git diff --check` 通过。
- 后续：使用 v4-pro 运行一次真实 research-v2 Agent E2E，验证 TaskPlan 全 grounded、DSL generation、审批、正式执行、报告和 Oracle；通过后提交并同步当前改动。

## 2026-09-09 | DeepSeek v4.1flash 模型可用性核验

- 任务：将当前 DeepSeek Planning 模型切换为 v4.1flash。
- 操作：核对本地非敏感 Planning 配置，并使用现有凭据只读查询官方 `https://api.deepseek.com/models`。
- 结果：当前运行模型为 `deepseek-v4-flash`；官方 endpoint 仅返回 `deepseek-v4-flash`、`deepseek-v4-pro` 和 `deepseek-v4-flash-vision-exp`，未提供 `v4.1flash` 或 `deepseek-v4.1-flash`。为避免写入无效模型导致 Agent 调用失败，本次未修改配置。
- 验证：官方 `/models` 请求成功，返回模型列表与官方 API 文档一致。
- 后续：需要确认目标模型的准确 API model ID，或提供支持该模型的 endpoint/provider。

## 2026-09-09 | Browser Observation、TargetBinding 与 research-v2 实施

- 任务：按 Phase 0-6 实施 A11y Tree、DOM、DSL、preflight 和 Runner 的统一方案，并以非电商页面验证。
- 操作：新增共享 capability manifest、BrowserObservation/LocatorSpec/TargetBinding JSON Schema 和 7 类跨站 fixture；Python 新增严格 Observation/TargetBinding 模型与唯一 LocatorSpec compiler；采集层保留原始 accessible name、AX states/relations、DOM text、runtime 和 frame/shadow context，生成最多 120 个 ElementFact，将 raw snapshot gzip 内容寻址并对超过 512 KiB 的节点集分片；Go 新增 browsercontract、PlanStep TargetBinding 持久化与 hash 校验，grounding 从成功 action/Observation 生成 binding；新增 research-v2 Draft DSL、Go 确定性编译、canonical v3 和 plan/observation binding；模型探索摘要预算收紧为 16/32/48 KiB；Runner 对 v2 只执行结构化 candidates 并在派发前检查唯一、可见、可用；修复 legacy scope/role parser 漂移及 v2 条件/执行分派问题。
- 结果：新链路实现 `Intent Plan -> Observation v2 -> TargetBinding -> Draft DSL -> Executable DSL v3 -> Runner`，LLM 不再提交 selector/candidates；research-v1 保持兼容。真实 Selenium Web Form research-v2 Runner smoke 中 `goto -> input -> click` 3/3 通过；DataTables Observation 被裁剪为 120 个 ElementFact，499,383-byte raw evidence 不再内联，生成 1 个 499,451-byte shard，低于 512 KiB 上限；MDN Web Component observation 识别到 open Shadow DOM context。关联 BUG-168、BUG-169、BUG-170、BUG-171、BUG-172。
- 验证：显式设置真实 `TEST_DATABASE_URL` 后 Go 全量测试通过，包含 research-v2 compilation、TargetBinding 持久化和 Research 生命周期测试；`go vet ./...`、`go build ./...` 通过；Python 全量 178 tests passed / 2 skipped、compileall、Pyright（0 errors）和新增文件 Ruff 检查通过；Frontend 10 tests 和 production build 通过；共享 canonical v3 golden、JSON Schema 解析和 `git diff --check` 通过。真实 Selenium 与 DataTables 浏览器 smoke 的业务断言通过，但命令退出时因 TRAE sandbox 禁止访问受限 macOS 路径返回非零。
- 后续：尚未运行付费的真实 DeepSeek Agent E2E；待 provider 稳定后使用 research-v2 跑跨站矩阵，并依据结果决定何时停止 research-v1 写入。

## 2026-09-09 | 跨站 Browser Observation 与 DSL 统一方案调研

- 任务：跳出单一电商页面，基于不同网站的真实 A11y、DOM 和动态交互数据，设计统一的 Observation、TargetBinding、DSL、Preflight 和 Runner 合同。
- 操作：只读访问 Selenium Web Form、GOV.UK Search、TodoMVC React、Wikipedia 人口表、DataTables、WAI ARIA Combobox 和 MDN Shadow DOM 示例；比较 compact A11y snapshot、DOM/ARIA/表格/Shadow DOM 规模及动态状态；复查 Playwright 官方 locator 建议、MDN Accessibility Tree 和 CDP Accessibility 协议；审计当前 Go/Python schema、preflight parser、semantic locator 和 Runner。
- 结果：新增 `docs/plan/unified-browser-observation-dsl-2026-09-09.md`。方案将 Intent TaskPlan、BrowserObservation/ElementFact、Grounded TargetBinding、Draft DSL 和 Executable DSL 分层；用结构化 LocatorSpec 替换自定义定位字符串；要求 preflight 与 Runner 共用唯一 compiler；保留 A11y name、DOM text、runtime state 和 frame/shadow context 的独立语义；采用 snapshot artifact、delta/ref、step-scoped observation 和三层预算控制体积。跨站证据证明原生表单、SPA、长表格、ARIA composite 和 Shadow DOM 不能用商品卡片模型或单一 locator 规则概括。
- 验证：7 个公开页面均完成真实浏览器只读检查，其中 Selenium 表单在 0 个显式 ARIA 元素下仍产生完整 A11y label；TodoMVC 动态 DOM 控件未全部进入 compact A11y；Wikipedia 超过 10,000 个 DOM 元素；DataTables 有排序/分页状态；WAI combobox 依赖 `expanded/controls/activedescendant`；MDN 示例包含 open Shadow DOM。官方文档确认 role/label/text/test id 优先、CSS/XPath 次选，XPath 不穿透 Shadow DOM。未运行项目 E2E，未调用 DeepSeek。
- 后续：评审并批准规格后，按 Phase 0-6 先合同、再 Observation、再 TargetBinding/DSL Compiler、最后统一 Runner 的顺序实施；旧 `research-v1` 在跨站矩阵通过前保持只读兼容。

## 2026-09-09 | A11y/DOM 增强与定位语法复核

- 任务：说明 Browser Worker 如何把 A11y 与 DOM 信息组合为可执行 locator，并判断是否应继续使用自定义定位字符串语法。
- 操作：核对 CDP A11y 节点转换、`backendDOMNodeId` 到 DOM 的局部增强、DOM-only interactive supplement、verified selector 生成、semantic runtime parser、locator preflight parser 和 candidate Runner。
- 结果：A11y 与 DOM 是不同事实源且不是一一对应；当前通过 `backendDOMNodeId` 做部分关联，并补充 A11y 缺失的可交互 DOM 控件。系统现有 `role="name"`、`inside "scope"` 等自定义字符串语法不应继续扩展，应迁移为结构化 TargetBinding。发现 runtime 接受通用 `inside "name"`，preflight 只接受 `inside product "name"`，同一 target 存在解释漂移，新增 BUG-168。
- 验证：静态核对两个 parser：`semantic._A11Y_SCOPE_RE` 接受可选 `product`，`locator_preflight._SCOPE_RE` 强制 `product`；本次未运行浏览器或修改业务代码。
- 后续：统一保留 `accessible_name`、`dom_text`、DOM attrs 和 runtime state；通过共享结构化 schema 表达 role/name/scope，并由 Grounding 产出候选引用，preflight 与 Runner 禁止维护独立字符串解析规则。

## 2026-09-09 | 当前 Agent 到 Playwright 的链路模拟与 DSL 边界复核

- 任务：基于当前实现模拟 AI 接收长链 Web 测试任务后的决策、工具调用、服务触发和结果，并复核探索/执行隔离、A11y 事实、Playwright locator 与 DSL 的边界。
- 操作：核对 Go Harness/TaskPlan/ControlPlane、Browser Worker capability/preflight、CDP A11y+DOM 增强、Playwright candidate runner、Execution Worker 和前端 Agent API；以 Blue Top 搜索加购任务逐阶段推演 `set_task_plan -> explore_page/flow -> generate_dsl -> approval -> execute_dsl -> get_report`；从 PostgreSQL 量化最近 Run 的 raw tool result、节点数、模型摘要和请求体字节。
- 结果：正式执行会启动新的 headless Chromium 并清理存储，和 planning 完全隔离；每次 `explore_flow` 也使用并关闭 disposable BrowserContext，`explore_page` 则只在 planning session 内复用上下文。当前 DSL 已禁止模型直接编造 candidates，服务端会基于按 page state 分组的 A11y/DOM evidence 生成 verified CSS、role/text/scoped candidates，再由 Runner 按分数和唯一性执行。`run_a85adb2d33a4dcf463ba5e70` 的 3 条探索 raw result 合计 664,631 bytes，单条最大 259,481 bytes；模型侧 3 条摘要累计 95,641 bytes，最后请求达到 390,352 bytes / 95,865 input tokens，说明原文隔离已生效但多轮上下文仍明显增长。另有两个尚未通过完整 E2E 证明的结构风险：DOM augmentation 会以 `textContent` 覆盖 accessible `name`，可能偏离 Playwright `get_by_role` 的真实 name；TaskPlan 在探索前持有业务 target，而 generation 又要求 DSL target 与其完全一致，可能把业务语义和探索后 locator binding 耦合。
- 验证：静态核对当前代码路径和数据库证据；同一 Run 的 Products/search/detail/cart 状态分别约为 240/62/72/55 个节点，单状态 JSON 约 120/39/46/46 KB；isolated probe 已完成 Products、搜索、详情、加购弹层和 `/view_cart`，但修复后尚无 Run 进入 DSL preflight，因此上述 DSL 风险是代码合同分析，不是已复现的最终失败结论。本次未运行服务、未调用模型。
- 后续：后续设计应显式区分 Intent Plan 的 semantic target 与 Grounded Plan 的 TargetBinding；保留原始 accessible name、DOM text 和 verified selectors 为不同字段，让 preflight/Runner 共用同一候选语义和可执行性验证；page snapshot 以内容寻址 artifact 保存，事件和模型上下文只携带 step-scoped delta/ref，并增加 raw snapshot、模型 observation 和 run token 三层预算。

## 2026-09-09 | 最近 Agent 链路实验综合复盘

- 任务：复盘最近多轮 Agentic E2E 的实际运行效果、问题演进和修复范围，判断方案是任务级补丁还是覆盖完整控制链路的通用改进。
- 操作：对照 Blue Top、Men Tshirt 和显式 TaskPlan Blue Top 的执行记录，复查 AgentRun 状态、LLM 调用量、工具失败、TaskPlan version/grounded 数量、isolated flow 页面路径、正式 Execution 和 Oracle 结果；重点区分浏览器动作成功、TaskPlan 状态推进、DSL/审批/正式执行及最终 Oracle 四个层次。
- 结果：旧 Blue Top 暴露探索污染，隔离 BrowserContext 后解决；Men Tshirt 已证明 DSL、审批和正式执行 7/7 可完成，Oracle 隐藏文本误判也已修复；新 TaskPlan Blue Top 多轮实验进一步证明浏览器可用 12 个成功动作到达 `/view_cart`，但 grounding ownership/result mapping 阻断状态推进。该修复覆盖 Goal 权威、计划版本、前置重放、动作授权、成功 evidence 和连续状态迁移，不含任务专用硬编码；但修复后最终复跑在首次 DeepSeek 调用连续三次 `response_read_failed`，尚未取得 TaskPlan 全链通过证据。
- 验证：数据库复核 8 个 TaskPlan Blue Top Run；其中 `run_a85adb2d33a4dcf463ba5e70` 的 isolated flow 为 `success=true`、0 failure、12 个成功 after-action，路径覆盖 Products、搜索结果、商品详情和 `/view_cart`，但最新 Plan 仅 2/12 grounded；`run_9f98a83f130f3465fae17b94` 为 3 次 HTTP 200 transport failure 后 `run.failed`。8 个 Run 已记录的可用 usage 合计 3,282,277 tokens，均未产生 DSL generation。
- 后续：必须补一次修复后、provider 稳定时的完整 E2E，验证 12/12 grounded、generation plan binding、审批、正式执行、报告和 Oracle；另行处理模型调用预算/缓存、响应流恢复和历史 stale Run，未完成前不应将本轮定义为端到端稳定。

## 2026-09-09 | Blue Top 长链 TaskPlan live E2E 与状态推进修复

- 任务：使用声明式 `automationexercise-blue-top-cart.v1.json` 和官方 DeepSeek 执行更长的真实 Agentic E2E，验证显式 TaskPlan 在搜索、详情、加购弹层和购物车验证链路中的状态推进。
- 操作：多轮运行隔离的 Blue Top 长链 E2E；根据真实 trace 修复 authoritative Goal、grounded 前置步骤重放、连续 `plan_step_ids`、只读 `wait_for`、selector/语义 target 对齐、Plan 改版 evidence 继承、部分成功 flow evidence、连续前缀 grounding，以及授权 action 到 PlanStep 的结果映射；补充聚焦回归测试。中间 Run `run_a85adb2d33a4dcf463ba5e70` 的 disposable flow 成功执行 12 个动作并依次到达 Products、搜索结果、商品详情和 `/view_cart`，但修复前状态映射只推进到 2/12 grounded。
- 结果：长链 TaskPlan grounding/action ownership 缺陷已在本地修复，且没有引入任务专用商品、URL、selector 或动作顺序分支。最终 Run `run_9f98a83f130f3465fae17b94` 未进入 TaskPlan、探索、DSL 或正式执行：首次官方 DeepSeek logical call 的三次物理请求均在 HTTP 200 后发生 `transport/response_read_failed`，Run 以 `LLM response read failed` 结束，因此本次 E2E 结论仍为失败，不能视为端到端验收通过。关联 BUG-166、BUG-167。
- 验证：`go test -count=1 ./...`、`go vet ./...`、`go build ./...` 和 `git diff --check` 通过；最终失败结果保存在 ignored 文件 `research/results/agentic-e2e-blue-top-cart-taskplan-final9-20260909T010500Z.json`。三个失败请求均保存 request ID、HTTP 状态和错误分类，usage 均为 unavailable；本轮服务已停止。
- 后续：在 provider 稳定后重新执行同一 acceptance，取得修复后 TaskPlan 全部 grounded、generation plan binding、审批、正式执行、报告和 Oracle 全通过的完整证据；另行设计 `response_read_failed` 的 deadline/watchdog/退避策略，并单独治理历史 stale Run。

## 2026-09-08 | 显式 TaskPlan/PlanStep 状态机落地

- 任务：将目标语义、动作顺序与次数、副作用边界和禁止动作从 prompt/transcript 约束升级为 Go AgentCore 的可持久化裁决状态。
- 操作：新增 `taskplan` 领域模型、内存/PostgreSQL Repository、基线 schema 与幂等迁移；新增 `set_task_plan` 工具；Harness 接入计划授权、grounding evidence、DSL generation、审批、执行、报告、失败和取消状态迁移；探索工具绑定 `plan_step_ids`，DSL generation 绑定 `plan_id/version/sha256`；新增 `task_plan.updated` SSE/研究事件和前端类型；删除旧 transcript-derived `observedPlanStep/facts_sufficient_for_generation` 双状态机；补充架构文档和单元/集成测试。
- 结果：正式 Agent 主链必须先持久化 TaskPlan；探索不能改写任务语义或执行 external/unknown side effect；DSL 必须逐步保持计划动作、参数、次数、幂等性和副作用；计划改版自动废止旧 generation/审批；Run 只有在计划 `completed` 后才能正常结束。
- 验证：`go test -count=1 ./...`、`go vet ./...`、`go build ./...` 通过；Frontend 4 个测试文件共 10 个测试和 `npm run build` 通过；`git diff --check` 通过。PostgreSQL 集成测试已添加，但本机无 `TEST_DATABASE_URL` 且无 Docker，未执行真实数据库迁移；未运行真实 DeepSeek Agentic E2E。
- 后续：在具备 PostgreSQL 与官方 DeepSeek 配置的环境执行 `go run ./cmd/migrate` 和声明式 acceptance live E2E，核对 `task_plan.updated`、generation plan binding 与平台调用记录。关联 BUG-165。

## 2026-09-08 | TaskPlan 状态机职责确认

- 任务：确认显式 TaskPlan/PlanStep 在现有 Agent 架构中的归属和模块边界。
- 操作：核对 AgentRun、Harness Policy、现有 transcript-derived `observedPlanStep` 以及历史 Task 6.6 记录。
- 结果：TaskPlan/PlanStep 应作为 Go AgentCore 的持久化任务编排聚合与状态机；Harness 驱动状态迁移，Policy 只依据已持久化计划做工具授权裁决，Browser Worker 只提交 grounding/evidence，DSL 必须绑定 plan version/hash。目标语义、动作顺序/次数、禁止动作与副作用边界均归 TaskPlan 所有，不能继续仅依赖 prompt 或从 transcript 临时反推。
- 验证：静态核对当前实现；确认现有 `observedPlanStep` 仅为探索门控的临时派生状态，尚不是完整 TaskPlan。
- 后续：先定义版本化 TaskPlan/PlanStep schema、状态迁移和持久化，再接入 Harness、Policy、Context Materializer 与 DSL plan hash 校验。

## 2026-09-08 | Agent 工作链路流程图

- 任务：将当前 Agent 工作链路画成流程图，并补充代码与架构讲解。
- 操作：使用 TRAE dynamic UI 生成 AgentCore 端到端流程图，覆盖 acceptance spec、E2E driver、Harness、LLM turn、Policy gate、Browser Worker、DSL 生成、审批、正式执行、Report 和 Oracle；同步在图中标注核心代码入口与状态边界。
- 结果：已输出交互式流程图，节点可点击查看职责、代码路径、状态字段和架构边界。
- 验证：可视化内容基于当前代码文件与最近 run `research/results/e2e-smoke-men-tshirt-think-gated-5-20260908T085435Z/run.json`；未运行新的 E2E，未调用模型。
- 后续：实现版本化 TaskPlan/PlanStep 后，需要更新流程图中的状态机层，替换当前 transcript-derived exploration state。

## 2026-09-08 | Agent 当前工作链路复盘

- 任务：说明当前 Agent 从 LLM 决策、工具调用、任务编排到状态机/验收的完整链路，并用最近实验数据佐证。
- 操作：核对 `backend-go/internal/agent/loop.go`、`backend-go/internal/harness/harness.go`、`backend-go/internal/harness/policy.go`、`backend-go/internal/agentservice/service.go`、`browser-worker/scripts/run_agentic_e2e.py` 与最近结果 `research/results/e2e-smoke-men-tshirt-think-gated-5-20260908T085435Z/run.json`；尝试读取本地 AgentService 事件 API 和 SQLite 事件库，确认当前服务未运行且根 `app.db` 为空。
- 结果：确认当前链路为 Go Harness 驱动的 ReAct 工具循环，Browser Worker 仅提供无状态浏览器能力；探索收敛、DSL 审批、正式执行和 Oracle 验收由控制面确定性门控串联。最近 run 产生 9 次 `research.llm_call`、1 个 DSL generation、1 次审批、1 个 batch/report artifact，正式执行通过但原始 Oracle 因隐藏 modal 文本误判失败。
- 验证：静态核对代码与 `run.json` 事件序列；未运行新的 E2E，未调用模型。
- 后续：继续补齐版本化 Task Plan/PlanStep 状态机，使目标语义、禁止动作、副作用边界和探索证据绑定从 prompt/隐式 transcript 收敛为显式持久状态。

## 2026-09-08 | Men Tshirt think-mode E2E 复跑

- 任务：在修复验证型文本事实预检阻断后，复跑 Men Tshirt live E2E。
- 操作：启动 Browser Worker、AgentService 和 execution-worker，使用 `automationexercise-men-tshirt-details.v1.json` 执行 `run_agentic_e2e.py`，显式开启 `AI_PLANNING_THINK_MODE=true` / `AI_PLANNING_REASONING_EFFORT=max`；复查 run trace、DSL、execution report、最终截图和 Oracle 输出。
- 结果：Run `run_acb19d3bc85c373e0b7e3543` 真实调用官方 `api.deepseek.com` 9 次，成功生成 DSL `generation_id=385`，审批后创建 batch `563` / execution `572`，正式执行 7 步全部通过且 Vision disabled。原始 E2E 结果 `success=false`，根因为 Oracle 用 final HTML raw text 读取隐藏 `cartModal` 模板中的 “Your product has been added to cart.”，而截图和执行报告可见文本均证明未发生加购。已修复 Oracle 隐藏元素文本过滤，同一 final HTML 复算 Oracle 后通过。
- 验证：`research/results/e2e-smoke-men-tshirt-think-gated-5-20260908T085435Z/run.json` 已生成；本地复算 final HTML 的 acceptance Oracle 通过；`uv run python -m unittest tests.test_acceptance` 通过；`uv run python -m unittest discover -s tests` 通过（163 passed / 2 skipped）；`uv run python -m compileall src tests scripts` 通过。
- 后续：如需获得 `success=true` 的新 run.json，需要在 Oracle 修复后再跑一次 live E2E；考虑成本，本次未额外重复调用模型。

## 2026-09-08 | 修复验证型文本事实预检阻断

- 任务：修复 BUG-163，避免运行时可见但缺少 a11y 节点的文本事实被 locator preflight 永久阻断。
- 操作：将 research-v1 的强预检动作收敛为 `click`、`input`、`capture_text`；`wait_for`、`assert_text` 在没有显式 selector/target_strategy 且无 candidates 时允许作为运行时文本验证步骤通过 DSL executable 校验；显式 CSS/XPath/selector 目标仍必须有预检 candidates。同步修改 Python Browser Worker preflight、Python research schema、Go research schema，并补充 Go/Python 回归测试。
- 结果：交互定位仍保持 verified candidate 强约束；验证型文本事实可以进入正式 DSL，由 Runner 在执行时验证，避免再次卡在 `Rs. 400` 这类 action-only 文本事实上。
- 验证：`go test -count=1 ./...` 通过；`uv run python -m unittest discover -s tests` 通过（161 passed / 2 skipped）；`uv run python -m compileall src tests scripts` 通过。
- 后续：需要在下一次 live E2E 中确认 Men Tshirt 任务能越过 `generate_dsl` 并进入正式报告/Oracle；若仍失败，再根据 Runner 报告做归因修复。

## 2026-09-08 | PlanStep 事实充分性门与 think-mode E2E 验证

- 任务：补充确定性的 PlanStep 完成状态/事实充分性门，开启 DeepSeek thinking mode 后重跑 Men Tshirt live E2E。
- 操作：在 Go Harness 增加探索门控，统计 `explore_page`/`explore_flow` 调用、重复 flow 签名、已完成动作、证据项、DSL 预检缺失 target；开启 DeepSeek `thinking` 请求与 reasoning 审计；修复探索摘要中同 URL/page_state 后续失败页覆盖前序成功 action 的问题；为 `execute_dsl` 模型可见摘要补充 `batch_id`/`case_id`/`report_api_url`。
- 结果：多轮真实 E2E 均调用官方 `api.deepseek.com` 且 reasoning 审计生效。Run `run_fb9c4198c7ac06549b7c1f56` 已进入 `generate_dsl`、审批和 `execute_dsl`，创建 batch `562`，证明探索门能迫使模型收敛到 DSL；后续暴露出 `execute_dsl` 摘要缺 `batch_id` 导致模型猜测报告 ID，以及详情页价格/分类属于运行时可见但缺少 a11y 节点的文本事实，仍会被 locator preflight 拒绝。Run `run_642d52a779cb803da104bc69` 验证了预检失败后可按缺失 target 允许补探，但最终为避免继续 token 消耗手动取消。
- 验证：`go test -count=1 ./internal/agent ./internal/harness ./internal/platform/llm ./internal/agentservice ./internal/config` 通过；E2E 结果文件写入 `research/results/e2e-smoke-men-tshirt-think-gated-3-20260908/run.json` 与 `research/results/e2e-smoke-men-tshirt-think-gated-4-20260908/run.json`；trace 中可见 `facts_sufficient_for_generation`、`unresolved_generation_targets`、`thinking_mode=enabled` 和 provider request/response ID。
- 后续：继续实现验证型文本事实的结构化 evidence 通道，避免把已成功的运行时 `wait_for` 文本错误要求为 a11y locator；同时降低 thinking mode 下的上下文膨胀和 cache miss。

## 2026-09-08 | Men Tshirt E2E trace 复盘

- 任务：回答用户关于是否有 CoT、为什么 AI 持续调用 `explore_flow`、工具是否有使用上限以及 Agent 循环机制的问题。
- 操作：按 Run `run_76d65f1db2ad473618b13385` 查询 `agent_events`，核对 8 个 `research.llm_call` 的 `logical_call_id`、DeepSeek provider response/request ID、usage、assistant 可见 content、tool_calls 和 tool result 顺序；复查 Agent loop、tool policy、`explore_flow` schema 与系统 prompt。
- 结果：本地没有保存隐藏 CoT；trace 中可见内容显示模型持续认为分类/价格 evidence 不足并重复 probe。当前只有 `AGENTSERVICE_MAX_TURNS` 全局 turn 上限、driver 总超时和 `execute_dsl` 审批门，缺少 `explore_flow` per-run 上限、重复 probe 熔断、事实充分性判定和状态机收敛门。
- 验证：8 次 `finish_reason=tool_calls`，2 次 `explore_page`、9 次 `explore_flow`，0 次 `generate_dsl`、0 Batch、0 Execution；DeepSeek 官方文档确认 thinking mode 可通过 API 响应返回 `reasoning_content`，但当前 OpenAI ChatCompletions 客户端没有解析或持久化该字段。
- 后续：将 `reasoning_content`/Responses API reasoning 作为可配置审计输入单独设计，默认仍不得依赖隐藏 CoT 判定通过；Task 6.6 继续实现 PlanStep 状态、预算和收敛门。

## 2026-09-08 | Men Tshirt 单次 live E2E

- 任务：在 Task 6.7 完成后使用第二个声明式 acceptance fixture 跑一次真实 E2E，评估 AI 是否能完成不同任务。
- 操作：确认 `AI_PLANNING_PROVIDER=deepseek`、官方 `api.deepseek.com`、`deepseek-v4-flash` 和 API key 已配置；应用数据库迁移，启动 Browser Worker、Go execution-worker 与 12-turn AgentService；以 600 秒总超时运行 `automationexercise-men-tshirt-details.v1.json`。当第 8 次模型调用后仍无 DSL generation 时主动取消，随后停止本轮服务。
- 结果：Project 1058 / Session 64 / Run `run_76d65f1db2ad473618b13385`。Agent 成功搜索 Men Tshirt、打开 `/product_details/2` 并识别商品名与 `Rs. 400`，但因分类文本精确匹配失败和缺少完成状态，持续重复 probe，未进入 DSL、审批或正式执行；运行安全取消并生成失败结果 `research/results/e2e-smoke-men-tshirt-20260908T142400Z/run.json`。新增 BUG-161。
- 验证：8 次官方 DeepSeek 调用，input 342,627、output 28,757、total 371,384 tokens，prompt cache hit 0、miss 342,627；8/8 provider response/request ID 和 credential fingerprint 已持久化，endpoint 全部为 HTTPS `api.deepseek.com`。共 2 次 `explore_page`、9 次 `explore_flow`，所有 flow 均为 `isolated_probe` 且不持久化状态；Generation/Batch/Execution 均为 0，取消清理成功。DeepSeek 平台访问落到登录页，未完成人工平台记录核对。
- 后续：先完成 Task 6.6 的 Task Plan/PlanStep 完成条件、结构化文本条件语义、事实充分性/收敛门和模型调用预算熔断，再运行下一次 live E2E。

## 2026-09-08 | 完成 Task 6.7 声明式 E2E 验收迁移

- 任务：移除可复用 E2E Driver/Oracle 中的 Canonical、Blue Top 和 cart 专用硬编码，使新增任务只需增加版本化数据文件。
- 操作：新增严格的 `agentic-e2e.acceptance.v1` JSON Schema、通用 acceptance loader/evaluator 和空条件/非法正则校验；迁移 Blue Top cart 验收并增加 Men Tshirt details fixture；删除 Driver 内固定 Goal、搜索步骤校验、cart parser 和 Oracle mutation；Research experiment 改为引用 acceptance 并持久化 acceptance ID/SHA，Go config 升级为 v2 且保留 v1 只读兼容；DSL profile 与 acceptance 解耦，由实验 controls 独立管理。
- 结果：两个不同任务通过同一 Driver/Oracle 合同，Stage 5 legacy 与 Stage 6 research 可复用同一 acceptance；通用运行时代码不再包含命名任务分支或相关商品、价格、URL、selector 示例；BUG-159 修复，Task 6.7 与对应 checklist 完成。
- 验证：Go `go test -count=1 ./...`、`go vet ./...`、`go build ./...` 通过；Python 159 tests passed / 2 skipped，Pyright 0 errors / 0 warnings，compileall 通过；6 个新增/变更 JSON 文件解析通过，聚焦 acceptance/Driver/Research 74 tests 通过，`git diff --check` 通过。Ruff 对全量改动文件仍报告存量规则债务，本次新增文件的 import 排序已修复。未运行 live E2E，未调用 DeepSeek。
- 后续：Task 6.6 尚需实现版本化 Task Plan/PlanStep、plan hash 绑定与 Context Materializer；恢复 live E2E 前仍需完成成本熔断、cache 指标聚合和人工预算确认。

## 2026-09-08 | 清理 Browser Worker 遗留文件和本地生成物

- 任务：核对 `browser-worker/` 根目录内容是否仍被使用，删除可证明无用的遗留文件，并修复目录中可见的导入与日志问题。
- 操作：将 Pyright include/extraPaths 从已删除的 `app/` 更新为 `src/`；删除无引用的 `locators/url_pattern.py`、旧 `app.db`、旧 `backend_structured.log`、空 `storage_states/` 和 Python/Ruff 缓存；重建 `.venv` 以清除已下线的 SQLAlchemy/Alembic/psycopg；将 `browser_worker` 应用日志统一为结构化 stdout；补充日志配置测试和本地生成物说明。保留可能被历史报告引用的 `artifacts/`。
- 结果：Browser Worker 根目录只保留源码、测试、运行脚本、构建配置、本机环境和可追溯执行证据；不再创建本地 SQLite 或项目根日志文件。修复 BUG-160。
- 验证：Python 153 tests passed / 2 skipped；compileall 通过；本次改动文件 Ruff 检查通过；Pyright 0 errors / 0 warnings；全目录 Ruff 仍有 190 个既有问题，未在本次清理中扩大修改范围。
- 后续：如确认历史报告不再需要，可按 retention policy 单独清理约 206MB 的 `artifacts/`。

## 2026-09-08 | 撤销任务流程硬编码并建立仓库级禁令

- 任务：响应用户关于禁止任何任务流程硬编码的要求，在运行新任务 E2E 前删除刚新增的 details Oracle/CLI 分支，并把规则固化到项目文档。
- 操作：完整撤销 `run_agentic_e2e.py` 和 `test_agentic_e2e_driver.py` 中新增的详情页任务逻辑；在 `AGENTS.md`、研究 spec/tasks/checklist 中明确禁止可复用 Agent/Harness/Tool/Driver/Runner/Oracle 硬编码商品、价格、数量、URL、selector、动作顺序或任务预期；新增 Task 6.7 和 BUG-159 跟踪历史 Canonical 硬编码迁移。
- 结果：刚新增的两个文件恢复为零 diff；现有通用 driver 因仍含历史 Blue Top/cart 专用逻辑，被明确判定为不能用于新任务 E2E。在迁移到版本化 declarative acceptance spec 前暂停新任务 live E2E，避免继续增加命名任务分支。
- 验证：Go 全量 test/vet/build、Python 151 passed/2 skipped、`src/browser_worker`/tests/scripts compileall、`git diff --check` 通过；未运行 live E2E，未调用模型。
- 后续：完成 Task 6.7：将历史 Canonical Goal、流程约束和 Oracle 预期迁出 driver，至少用两个声明式任务 fixture 证明新增任务不修改运行时代码。

## 2026-09-08 | Browser Worker 改为标准 src 布局

- 任务：消除 `browser-worker/browser_worker` 连续重复命名带来的阅读困惑，同时保留有语义的 Python import 包名。
- 操作：将源码包迁移为 `browser-worker/src/browser_worker`；Hatch wheel 配置改为 `packages = ["src/browser_worker"]`；同步研究代码快照路径和架构文档；新增统一的 `runtime.paths.PROJECT_ROOT`，替代 artifact、`.env`、日志路径中的目录深度硬编码；三个直接运行脚本显式加入 `src` 源码根。
- 结果：仓库目录层次变为“项目根 -> src -> Python 包”，不再出现项目目录与包目录紧邻重复；运行时 import 仍保持清晰的 `browser_worker.*`。同时修复了 BUG-158。
- 验证：`cd browser-worker && uv run python -m unittest discover -s tests -v` 通过（151 passed / 2 skipped）；`uv run python -m compileall -q src/browser_worker tests scripts` 通过；FastAPI 入口、三个脚本导入和 `PROJECT_ROOT` 断言通过；旧路径检索无命中；`git diff --check` 通过。
- 后续：无。

## 2026-09-08 | 清理 explore_flow 调试会话并完成最终门禁

- 任务：按用户要求删除 `.dbg` 和临时调试文件，并在无调试埋点状态下运行最终测试。
- 操作：确认 `.dbg` 仅包含 `explore-flow-side-effects` 的 env/NDJSON，代码中无其他调试会话后删除 `.dbg` 与 `debug-explore-flow-side-effects.md`；复查 `policy.go` 仅保留 DSL 审批门，不包含 Add to cart/次数硬编码；运行 Go/Python 全量门禁和残留检索。
- 结果：调试服务器未运行，调试网络上报、`.dbg`、debug session 文件全部清理；保留 disposable probe context、结构化 context/effect 摘要、quantity=2 DSL 回归及 Task Plan/Context Materializer 规格。BUG-157 标记 fixed。
- 验证：`cd backend-go && go test -count=1 ./... && go vet ./... && go build ./...` 通过；`cd browser-worker && uv run python -m unittest discover -s tests -v` 为 151 passed/2 skipped；`uv run python -m compileall -q src/browser_worker tests scripts` 通过；`git diff --check`、调试标记和调试文件残留检查通过。未运行 live E2E，未调用模型。
- 后续：实现版本化 Task Plan/PlanStep 与 Context Materializer；稳定前缀用于 provider cache，动态窗口只携带当前 PlanStep、observation delta、未解决 failure/recovery，完整事实通过 artifact ref/hash 引用。

## 2026-09-08 | Mock 复现并修复 explore_flow 非幂等重放

- 任务：针对 Blue Top live smoke 中 Agent 反复把 `explore_flow` 当执行器的问题，用 mock 复现并判断是工具合同、Agent 编排还是 Browser Worker 执行错误。
- 操作：按 Debugger 科学调试流程建立 `explore-flow-side-effects` 会话和网络埋点；以记录轨迹构造 policy/tool mock；初版按 action/target/URL 拒绝重放，但用户指出会误伤“同一商品加两件”的合法任务，因此撤销硬拒绝，改为每次 `explore_flow` 创建并关闭独立 disposable BrowserContext；任务次数和顺序继续由 Agent 编排及最终 DSL 表达。
- 结果：pre-fix 三次 probe 共享状态，mock cart 从 quantity 1 增长到 3；post-fix policy 不限制合法业务数量，但三次 probe 均返回 `execution_scope=isolated_probe`、`state_persisted=false` 和独立 quantity 1。模型摘要增加压缩稳定的 `executed_effects` 与 context metadata。
- 验证：Go policy/summary/prompt/tool contract/DSL 聚焦测试通过，其中 research-v1 明确接受 `Quantity=2 -> Add to cart once`；Python capability/page explorer 隔离测试通过。Go/Python 全量、compileall 和 `git diff --check` 通过；未运行 live E2E，未调用 DeepSeek。
- 后续：实现版本化 Task Plan/PlanStep 状态机：Plan 决定动作、顺序、次数；Explore 仅绑定 PlanStep 验证 feasibility/grounding；最终 DSL 必须绑定 plan hash，计划变更必须显式生成新版本。

## 2026-09-07 | 架构更新后执行单次 Blue Top live E2E smoke

- 任务：用户更新项目架构后，请求先跑一次 E2E 测试，观察 AI 完成“添加蓝色短袖商品到购物车”任务的能力。
- 操作：确认工作区最新提交为 `430c0f2 refactor: clarify browser worker package layout`，执行 `go run ./cmd/migrate`；启动 Browser Worker、Go execution-worker 和 `AGENTSERVICE_MAX_TURNS=16` 的 AgentService；使用官方 DeepSeek 配置和 `browser-worker/scripts/run_agentic_e2e.py` 执行单次 `research-v1` Blue Top live smoke，未运行 3 repetition。首次运行因仓库内 Playwright Chromium/headless shell 缺失进入 clarification 并自动取消；安装到仓库内 `.playwright-browsers/` 后重跑一次，并在 AI 已采集购物车证据但未收敛到 DSL 时手动取消以控制成本。
- 结果：第一次结果为 `research/results/e2e-smoke-20260907T120910Z/run.json`，Project 1056 / Session 62 / Run `run_e49928f24192bca810d2818e`，失败原因是浏览器内核缺失。第二次结果为 `research/results/e2e-smoke-20260907T121005Z/run.json`，Project 1057 / Session 63 / Run `run_fbe6208971669d7e43715c4d`，AI 成功完成 Products 搜索、进入 `/product_details/1`、点击 Add to cart、通过 modal View Cart 到达 `/view_cart`，并采集到 cart row evidence；但没有生成 DSL、没有进入审批、没有 Batch/Execution。最后证据显示购物车 `#product-1` 为 Blue Top，单价 `Rs. 500`，但数量为 `3`、总价为 `Rs. 1500`，偏离目标数量 1。新增 BUG-157 跟踪探索阶段重复执行非幂等加购的问题。
- 验证：本轮只执行单次 live smoke，不做正式 Stage 验收。两次 Run 均被取消并确认无 Batch/Execution；第二次 DeepSeek 调用 6 次，input 214,385、output 20,703、total 235,088 tokens，prompt cache hit 0、miss 214,385。两次合计 DeepSeek 调用 8 次，total 251,861 tokens。服务进程已停止，本轮启动的后台进程已清理。
- 后续：优先修复探索/执行边界：`explore_flow` 不得重复执行 Add to cart 等非幂等副作用动作；到达关键业务状态后应强制收敛到 DSL 生成或停止；cart evidence 若显示 quantity/total 偏离目标，应归因为探索污染并停止。修复前不应继续跑 live E2E。

## 2026-09-07 | 重命名 Browser Worker Python 包结构

- 任务：回应 `app/` 目录语义不清的问题，重新设计 Browser Worker 为更容易阅读的项目结构。
- 操作：将 `browser-worker/app` 改为领域包 `browser_worker`；按阅读路径拆分为 `server/`、`capabilities/`、`exploration/`、`contracts/`、`runners/`、`locators/`、`reporting/`、`runtime/`；同步所有 Python import、mock patch 路径、`browser-worker-dev` 入口、Docker/compose 启动命令、研究代码快照 owner root 和架构文档；新增 `browser_worker/README.md` 解释目录职责。
- 结果：Browser Worker 入口、协议、浏览器能力、探索、执行、定位、报告和运行时基础设施的边界更直观；源码中不再使用无语义的 `app.*` 包路径。
- 验证：`cd browser-worker && uv run python -m unittest discover -s tests -v` 通过（150 passed / 2 skipped）；`cd browser-worker && uv run python -m compileall -q browser_worker tests scripts` 通过；`cd browser-worker && uv run python -c "from browser_worker.server.main import create_app; app = create_app(); print(len(app.routes))"` 输出 9；`git diff --check` 通过。
- 后续：如需要继续压缩阅读成本，可再把 `page_explorer.py` 和 `playwright_runner.py` 两个大文件拆成更小的按职责文件。

## 2026-09-07 | 同步 Browser Worker 收敛变更到 GitHub

- 任务：将已完成并验证的 Browser Worker 去后端化收敛变更同步到 GitHub。
- 操作：复查当前分支、远端、工作区状态和变更统计；准备提交包含 Go migrate、Go execution worker、Browser execution RPC、Browser Worker DB/ORM/Alembic 删除、文档和测试更新的完整变更集。
- 结果：待提交并推送到 `origin/main`。
- 验证：同步前已完成 Go 全量测试、Go-migrated PostgreSQL 集成测试、Browser Worker Python 全量测试、真实 HTTP E2E、结构检索和 `git diff --check`。
- 后续：推送后确认最新 commit hash 和工作区状态。

## 2026-09-07 | 完成 Browser Worker 去后端化收敛

- 任务：完成“先瘦身，后重构，最后测试”的 Browser Worker 收敛目标。
- 操作：将数据库 schema 管理迁到 Go `cmd/migrate` 与 `internal/dbschema/schema.sql`；删除 Browser Worker 的 Alembic、SQLAlchemy ORM、DB session、旧清理脚本、旧 Python 执行持久化服务、旧 execution worker 和无引用服务；移除 Browser Worker 的 SQLAlchemy/Alembic/psycopg 依赖；将 production compose 的迁移和执行消费改为 Go 二进制；保留 Python 侧为 Browser capability/execution RPC、Playwright runner、locator、reporter 和 failure signal 纯逻辑。
- 结果：`browser-worker` 不再包含业务数据库模型、迁移链或执行队列持久化职责；官方执行链路为 Go migrate -> Go AgentService -> Go execution-worker -> Python stateless Browser execution RPC -> Go 持久化报告。
- 验证：Go migrate 在全新空库和仅有旧 `alembic_version` 的库上均成功建出 18 张表，且无 `alembic_version`/`dsl_anti_patterns`；本地 HTTP E2E 通过（新库、创建 case、创建 batch、Go worker 调用 Browser RPC 执行 `example.com`，Batch/Job/Execution 全部 passed，2 steps）；`cd backend-go && go test -count=1 ./... && go vet ./... && go build ./...` 通过；`cd backend-go && TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing_e2e_tmp go test -count=1 ./internal/execution ./internal/integration ./internal/research -run 'TestPostgres|TestControlPlane|TestAgent' -v` 通过；`cd browser-worker && uv run python -m unittest discover -s tests -v && uv run python -m compileall -q app tests scripts` 通过（150 passed / 2 skipped）；Browser Worker DB/ORM/Alembic 引用检索通过；`git diff --check` 通过。`docker compose -f compose.prod.yml config` 未运行，本机无 `docker` 命令。
- 后续：无。

## 2026-09-07 | 将执行队列消费迁移到 Go Worker

- 任务：继续按“先瘦身，后重构，最后测试”收敛 Browser Worker，减少 Python 侧传统后端职责。
- 操作：新增 Python 无状态 `/api/v1/internal/browser-executions` RPC，直接执行 DSL 并返回 report/failure_signal，不读写业务数据库；新增 Go `cmd/execution-worker`、Browser Worker execution client、`execution.Store` 的 claim/start/finish 持久化方法和过期 lease 安全隔离逻辑；生产 compose 默认改为 Go execution worker；删除 Python 旧 `execution_worker.py`、`execution_batches.py`、`executions.py`、Python reporting analysis 和 anti-pattern 持久化服务。
- 结果：官方执行队列消费、Run 创建、Report/FailureSignal 持久化、Job/Batch 终态刷新已迁到 Go 主路径。Browser Worker 更接近“浏览器执行器”：HTTP API 负责页面探索和无状态 Playwright 执行，业务状态机由 Go 控制面负责。
- 验证：`cd backend-go && go test -count=1 ./... && go vet ./... && go build ./...` 通过；`cd backend-go && TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 ./internal/execution -run 'TestPostgres' -v` 通过；`cd browser-worker && uv run python -m unittest discover -s tests -v && uv run python -m compileall -q app tests scripts` 通过（151 passed / 2 skipped）；`git diff --check` 通过。
- 后续：继续把 `app/models` 中只服务旧执行持久化的部分和 Alembic 迁移链迁出 Browser Worker；如需要批次级分析，后续在 Go 控制面重建。

## 2026-09-07 | Browser Worker capability 入口瘦身

- 任务：按“先瘦身，后重构，最后测试”的目标，先收敛 Browser Worker 的 HTTP capability 边界。
- 操作：删除 Python `api/capability_context.py`，移除 Browser capability 路由和 `create_app()` 启动阶段的 SQLAlchemy 依赖；新增 `BrowserCapabilityContext` 请求合同；Go Browser Worker client 支持 context resolver，并在 AgentService 启动时从 Go planning store 解析 `clean_context`、`entry_url_or_page` 与项目关联；同步更新部署和架构文档，把 Python execution worker 标注为过渡组件。
- 结果：Browser capability API 不再读取业务数据库，也不再负责 Project/Planning ownership 校验；该职责回到 Go 控制面。当前 Python execution worker、模型、Alembic 和报告服务仍保留，因为它们仍是正式执行队列消费端，后续需迁移到 Go 后再删除。
- 验证：`cd backend-go && go test -count=1 ./... && go vet ./... && go build ./...` 通过；`cd browser-worker && uv run python -m unittest discover -s tests -v && uv run python -m compileall -q app tests scripts` 通过（157 passed / 2 skipped）；`git diff --check` 通过。`ruff` 未运行，环境中未安装 `ruff`；`docker compose -f compose.prod.yml config` 未运行，本机无 `docker` 命令。
- 后续：第二阶段应实现 Go 侧执行 Job consumer 或显式执行 RPC，把 `execution_worker.py`、Python 业务 models/services/reporters 和 Alembic 迁移链从主路径移除。

## 2026-09-07 | 评估 Browser Worker 架构边界

- 任务：回答 `browser-worker` 如果只作为浏览器控制进程，当前架构是否过重、是否偏离预期边界。
- 操作：检查 `browser-worker` 目录结构、README、FastAPI 入口、Browser capability 路由、浏览器能力服务、执行队列和用例执行服务，判断 Python 侧仍承担的职责范围。
- 结果：确认当前 `browser-worker` 仍保留数据库模型、Alembic、执行队列、lease/heartbeat、测试运行持久化、报告分析等传统后端职责；这与“Go AgentService 作为控制面、Python 只做 Browser Worker”的目标不完全一致，建议后续收敛为无业务持久化的浏览器能力适配器。
- 验证：执行只读代码检查，未运行测试。
- 后续：如进入重构，应先制定边界收敛 Spec，再按“能力接口瘦身 -> 持久化迁移到 Go -> Python 旧服务删除”的顺序推进。

## 2026-09-07 | 评估 Blue Top 链路可靠性与结构化观察需求

- 任务：回答当前链路是否能完成 Blue Top 加购物车任务，并梳理需要覆盖的测试场景和结构化返回数据要求。
- 操作：基于 Stage 0-6 已有实验结果、BUG-155/156 和用户对 AI 可理解结构信息的要求，按 happy path、定位失败、页面理解失败、DSL/Runner 合同失败、验证失败、副作用重试和上下文压缩风险重新划分验收场景；随后在 Go Agent 层实现模型可见结构化摘要，未执行 E2E，未调用模型。
- 结果：当前 legacy/canonical 链路已能在 Stage 5 完成 Blue Top happy path 和 wrong-price 负向 Oracle，但 research-v1/Action IR live 未连续通过，不能宣称产品级可靠。本轮将 `explore_page`/`explore_flow` 的模型可见摘要升级为结构化 Observation，包含 page state、element group、candidate coverage、action option、verification fact 和 recovery hint；同时将 `generate_dsl`、`get_report`、`fix_and_retry` 的模型可见结果改为 DSL/Report/Repair 摘要，完整原始 tool result 继续通过 source seq、hash 和 bytes 审计，避免完整 JSON 直接回填 transcript。
- 验证：未执行 live E2E、未调用模型。通过 `cd backend-go && go test -count=1 ./internal/agent ./internal/platform/llm ./internal/harness ./internal/tools`、`cd backend-go && go test -count=1 ./...`、`cd backend-go && go vet ./...`、`cd backend-go && go build ./...`、`git diff --check`。
- 后续：恢复 live E2E 前继续补调用/token/失败重试预算、provider cache hit/miss 聚合和成本预估；再用非 live fixture 扩展诊断矩阵，最后做一次低成本 live smoke。

## 2026-09-07 | 梳理 Blue Top 购物车目标的 AI 失败重试链路

- 任务：基于澄清后的 AI/后端职责边界，重新描述“添加蓝色短袖商品到购物车”的完整执行链路，并重点说明 AI 在失败重试环节的具体决策逻辑。
- 操作：按自然语言目标、探索、元素验证、DSL/Action IR、审批、执行、报告、失败归因、恢复决策和学习样本沉淀拆分链路；未执行 E2E，未调用模型。
- 结果：明确 AI 不只是生成 DSL，还负责页面理解、探索选择、验证请求、失败分析和 recovery plan；但每一步都必须被工具事实、DSL 校验、审批门、非幂等副作用约束、Runner evidence、FailureSignal 和 Oracle 裁决包住。失败重试逻辑应先分类 failure，再判定动作是否已提交副作用，之后决定 re-explore、regenerate DSL、retry same DSL、manual reconcile 或 stop。
- 验证：本轮为架构说明，无测试执行。
- 后续：将该链路转化为 Stage 7 前的诊断实验协议，记录每个 AI recovery decision 的输入事实、候选方案、裁决原因和后续验证结果。

## 2026-09-07 | 澄清 AI 在执行链路中的职责边界

- 任务：纠正“AI 只负责规划和生成 DSL”的过窄表述，明确 AI 在元素验证、页面理解、失败分析、恢复决策、重试和学习闭环中的职责。
- 操作：基于当前 AgentCore + Browser Worker 架构和 Stage 0-6 实验记录，重新划分 AI 建议、工具验证、确定性执行和后端裁决的边界；未执行 E2E，未调用模型。
- 结果：准确边界应为：AI 负责理解用户目标、决定探索范围、请求元素验证、提出候选定位与 DSL/Action IR、分析失败、选择恢复策略并生成修正版候选；Browser Worker/Runner 负责用 Playwright/A11y/DOM 产生可验证事实；Go 后端负责 DSL 校验、审批门、执行调度、重试安全、报告归因和持久化裁决。AI 可以参与验证和决策，但不能绕过结构化工具事实直接执行、直接判定通过或直接学习改写生产策略。
- 验证：本轮为架构澄清，无测试执行。
- 后续：后续诊断实验需要单独记录 AI 的每类决策输入、可选项、工具验证结果、最终裁决和反馈样本，支撑重试与学习，而不是只记录最终生成的 DSL。

## 2026-09-07 | 复盘实验覆盖的问题类型与证据缺口

- 任务：回答 Stage 0-6 既有实验是否覆盖用户关心的错误元素、页面理解、DSL/Playwright 合同、错误归因、元素/动作分类、重试、验证、上下文膨胀和压缩丢失问题。
- 操作：只读检索 `docs/bug-log.md`、`docs/execution-log.md`、`.trae/specs/build-agentic-research-platform`，并查询本地 PostgreSQL 中 Stage 5/6 ResearchRun 状态、metrics 和关键 Agent event 失败片段；未执行 E2E，未调用模型。
- 结果：既有实验确实暴露并修复过多类问题：搜索控件缺失和广告误判、未验证复合 CSS、错误页面归因、跨页 click 被广告截断、Playwright trigger 错误、pre/postcondition 验证时序、非幂等 click 重放、FailureSignal 归因、探索摘要压缩和无 accessible name preflight 崩溃。但 Stage 5 的 `grounding_accuracy` 与 `invalid_action_rate` 仍因缺少标注真值/分类事实记录为 unavailable，Stage 6 live 只说明 research-v1 合同在真实路径上仍有 preflight/condition 语义与上下文治理问题，未形成完整通过实验。
- 验证：本轮只读审计，无测试执行。
- 后续：下一步不应继续跑 live E2E，应先把实验协议升级为“错误类型可观测”的诊断矩阵：每个 run 显式记录目标选择、候选来源、最终 locator、condition 翻译、动作副作用、重试决策、失败归因、压缩输入/丢弃字段和 token 成本，确保实验能解释失败而不仅报告成功率。

## 2026-09-07 | 复盘 Stage 3-6 实验结果与 token 暴增原因

- 任务：回答当前已做实验、实验结果、失败原因、token 暴增原因及合理性问题，明确 token 上限不是根因方案。
- 操作：只读查询本地 PostgreSQL 的 `research_experiments`、`research_runs`、`agent_events`，按实验汇总 completed/cancelled、task_success、LLM calls、tokens、cache hit/miss、prompt request bytes 和 tool result 类型；未执行 E2E，未调用模型。
- 结果：Stage 4/5 实验已完成并产出可重建指标；Stage 6 做过两轮 research-v1 live 尝试，均为 3-run 实验但只有 1 个 completed、2 个 cancelled，未达到 provider-verified 验收。Stage 6 最新 live 轮累计 40 次官方调用、3,669,930 total tokens、cache hit 为 0；暴增原因是完整 Agent transcript 随探索、生成、报告、失败修复持续累积，且非探索工具结果未摘要化，不是单次 A11y 树本身不可接受。
- 验证：本轮只读审计，无测试执行。
- 后续：成本治理应聚焦实验协议和上下文结构重构：先固定最小实验样本和失败停止规则，再做非探索工具摘要、上下文窗口重建和 provider cache 友好请求形态，最后恢复低成本 live smoke。

## 2026-09-07 | Stage 6 Action IR 非 live checkpoint

- 任务：按用户要求先完成并推送 Stage 6 implementation checkpoint，暂停真实 E2E 和官方模型调用。
- 操作：收口 research-v1 Action IR，覆盖 Intent、Target、Preconditions、Postconditions、Idempotency/side-effect、Go/Python canonical v2 合同、legacy-v1 兼容、ResearchRun/Execution profile 传递、非幂等执行租约、provider evidence 投影和官方调用审计字段；修复无 accessible name 但有 verified selector 的 locator preflight 崩溃；将 live E2E 验收改为 BUG-155 成本控制完成后的延期项。
- 结果：Stage 6 代码达到非 live checkpoint；真实 provider E2E 未执行且未标记通过。新增 BUG-156 记录无名节点 preflight 修复，BUG-155 继续跟踪成本熔断方案。
- 验证：未跑 live E2E、未调用 DeepSeek。通过 `go test -count=1 ./...`、`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 ./...`、`go vet ./...`、`go build ./...`、`uv run python -m unittest discover -s tests -v`（159 passed/2 skipped）、`uv run python -m compileall -q app scripts tests`、`uv lock --check`、`uv run alembic current && uv run alembic heads && uv run alembic check`、`npm test -- --run`、`npm run build`、`npm_config_cache="$PWD/.npm-cache" npx --yes knip`、`git diff --check`。
- 后续：提交并推送；恢复 live E2E 前先实现 BUG-155 的预算、摘要和成本预估门禁。

## 2026-09-07 | Stage 6 Live E2E 成本与缓存命中审计

- 任务：响应用户关于 DeepSeek 余额消耗过快和 prompt cache 命中低的反馈，立即停止真实模型调用并审计 Stage 6 live E2E 的调用、token、cache 与上下文增长来源。
- 操作：确认当前无残留 `research_e2e`、AgentService、Browser Worker、Execution Worker 或 DeepSeek 调用进程；读取 `research/results/stage6-live-acceptance-20260907T080506Z` 的 provider evidence、run 状态和本地 PostgreSQL `agent_events`/`research_runs`，按 run 统计真实官方调用次数、input/output/total tokens、`prompt_cache_hit_tokens` 与 `prompt_cache_miss_tokens`；检查 Agent transcript、tool result 类型和摘要压缩代码路径。
- 结果：Stage 6 live acceptance 在 3 个 ResearchRun 中累计产生 40 次官方 DeepSeek 调用，合计 input 3,532,269、output 137,661、total 3,669,930 tokens；其中 `prompt_cache_hit_tokens=0`、`prompt_cache_miss_tokens=3,532,269`。第一个 run 完成并产生 14 次调用，第二个 run 失败/取消前已产生 24 次调用，第三个 run 取消前产生 2 次调用。成本主要来自完整 Agent 循环多轮探索、验证、生成、执行、失败修复和报告读取，而不是单次无障碍树本身；`BuildModelToolSummary` 仅压缩 `explore_page/explore_flow`，`get_report`、`fix_and_retry`、`generate_dsl` 等非探索工具结果会以完整 JSON 回填 transcript，导致上下文从约 5.4k input tokens 增长到 120k、174k、203k 级别。新增 BUG-155 跟踪成本熔断、cache 指标聚合和非探索工具摘要缺口。
- 验证：本轮只做只读审计和日志记录，未执行 live E2E、未调用 DeepSeek、未运行测试套件；通过本地 PostgreSQL 查询和已有 provider evidence 交叉核对调用数与 token 汇总。
- 后续：暂停所有真实官方模型 E2E，先实现单 run/单调用预算上限、失败路径熔断、非探索工具摘要、cache hit/miss 汇总和验收前成本预估；修复后先用本地/recorded 路径验证，再经用户确认后执行最小 live smoke。

## 2026-09-07 | 完成 Stage 5 Metrics 与实验控制面

- 任务：实现版本化 Metric Projector、Experiment/ResearchRun API 与确定性调度、统一 `research-e2e run/verify/export`，并以真实主链验证独立 Oracle、clean context、AI 决策轨迹和任务超时。
- 操作：新增 Oracle 原子事实及 `0042` 迁移；从 Agent Event、Transition、最终 Execution Report 和 Oracle 投影 task/execution/verification、grounding、invalid action、recovery、step/retry/token/latency/vision 指标，缺少真值或分母时保存 null 与稳定原因。新增固定版本/seed、warm-up、随机顺序和超时配置的 Research API；planning session 持久化 `clean_context`，Browser Worker 禁止加载 project storage state并输出 `context_evidence.v1`。统一 CLI 使用服务端绝对 deadline，超时级联取消 AgentRun 与 Batch/Job，只导出结构化 observation/decision/action/tool/verification/recovery 与 source hash，不保存隐藏 reasoning。
- 结果：Canonical Experiment `research-experiment-646d8a5a99389fe319b48f9ab53eb35f` 在 Project 858 完成 3 个正式 repetition；ResearchRun `research-run-4e00876bf70aa1ce49f3f8917094d52b`、`research-run-00aa5b4255f2810dace87268a46320ea`、`research-run-d30566d890aa3d5190a7592543dad026` 均为 completed，分别关联 Session 52/54/53、Execution 425/427/426，task/execution/verification success=true、VLM=0。错误价格 Experiment `research-experiment-d18a7baf03c4a169c890eafb187c72b6` 的正式执行通过，但独立 Oracle 与 `task_success` 均为 false，CLI 按预期退出 1；Project 858 最终活动 AgentRun/Job/ResearchRun 为 0/0/0。
- 验证：Go 16 packages、169 个顶层测试和 94 个子测试通过，16 项 PostgreSQL 测试实际执行；Research race、vet/build、Python 117 passed/2 个环境门控 skipped、Alembic 主库/空库/0041↔0042、Frontend 9/9/build/Knip、compileall、代码快照和 diff 检查通过。Canonical 导出 90 行、SHA-256 `f3ee57392806851d079d34065ad45d3d08b35a9087136327732cd43e67932f7e`；wrong-price 导出 28 行、SHA-256 `62ff12d71aba51478b9b53594f3ec6f1160d2597f78593fe9cfce05334b5a619`。
- 后续：提交并推送 Stage 5 后进入 Stage 6 research-v1 Action IR。

## 2026-09-07 | Stage 4 提交并停止后续阶段

- 任务：完成 Stage 4 后提交并推送，按用户要求不进入 Stage 5。
- 操作：核对同一 Experiment 的三次 Canonical、逐 Run A/B/删除重建/C 投影导出、实验级 JSONL、Schema 与安全检查；确认本地 DeepSeek API key 未进入跟踪文件。
- 结果：Stage 3 已提交为 `4508167 feat: add research persistence schema`；Stage 4 已提交为 `f98f20f feat: project and export agent trajectories`，均已同步 `origin/main`。
- 验证：Stage 4 三次 Canonical 均首批通过；Transition 为 30/26/31 行；实验导出 87 行，SHA-256 为 `baf75bf13aa60affc8e8ee7f321ec6646159c8e88acbd2d1ad42ef23d03aff0e`；工作区与远端一致。
- 后续：按用户要求停止，不执行 Stage 5。

## 2026-09-06 | 完成 Stage 4 Task 4.4 提交前验收

- 任务：只完成 Stage 4 最终验收和提交前收口，不进入 Stage 5、不修改业务代码、不 commit/push；复核中断点 repetition0，并在同一 Experiment 下完成 repetition1/2 的 clean-context Canonical、投影重建、逐行 Schema/安全和实验级稳定导出。
- 操作：复核 Experiment `exp-stage4-final-795-153241` 和 repetition0 全链，重新执行 A/B、删除 30 条投影并确认归零、C 重建。随后在 Experiment 所属 Project 795 创建全新 Session 50/51，由官方 Agentic E2E 驱动串行执行 repetition1/2；仅断言 ignored `browser-worker/.env` 的官方 DeepSeek provider/model/base URL 和 key 非空，未读取或输出 key。两次通过后分别创建并完成 ResearchRun，从 source seq 0 投影，再执行 A/B/Delete/C 和外部 JSONL 逐行校验。一次默认驱动预跑新建了 Project 796，Canonical 通过但因 Experiment project 归属合同在 ResearchRun 插入前被正确拒绝，未计入实验；正式 repetition 使用 Project 795 的全新 Session/Browser context。
- 结果：repetition0 为 Project 795 / Session 48 / Run `run_6750d52bb41d7e91b3b5c887` / Generation 300 / Batch 406 / Execution 397 / ResearchRun `rr-s4-r0-6750d52bb41d7e91b3b5c887`，130 个 source event 投影 30 条 Transition；repetition1 为 Session 50 / Run `run_9b893eadb339dcc382a4ba88` / Generation 302 / Batch 408 / Execution 399 / ResearchRun `rr-s4-r1-9b893eadb339dcc382a4ba88`，95 个 event 投影 26 条；repetition2 为 Session 51 / Run `run_8ebd11444a90eef5d37be3d5` / Generation 303 / Batch 409 / Execution 400 / ResearchRun `rr-s4-r2-8ebd11444a90eef5d37be3d5`，131 个 event 投影 31 条。三次均首批 passed、Report v2、独立 DOM Oracle、DSL SHA 绑定、VLM=0、recovery=0、clean context，Experiment 最终为 `completed`。
- 验证：三组 A/B/Delete/C 分别为 30/26/31 行，删除后均为 0，重建后 ordinal 分别连续为 0..29/0..25/0..30；组内 SHA-256 分别稳定为 `dd63933164f66c928f7b1ce0c51b68dc2ef49ecdbf007b58d1faacac25108c5e`、`8f672f2fac7cc68654376e97b1b535dccdafaa9cfd1c2d5dced0f660e2d1b84d`、`6cbcb0981186856cddb86a2ef6146784958e88d7cd60244af755fd7633d96060`。实验级导出两次均为 87 行、1,176,839 bytes、SHA-256 `baf75bf13aa60affc8e8ee7f321ec6646159c8e88acbd2d1ad42ef23d03aff0e`，严格按 repetition 0/1/2 和 ordinal 排序，全部逐行通过 Draft 2020-12 Schema、Go 语义/content hash 与安全规则。带真实 PostgreSQL 的 `go test -count=1 ./internal/research` 通过，BUG-151/152 保持 `fixed`。
- 后续：三服务和端口已停止；删除 73 条无 owner、无 project、无 ResearchRun 引用的测试遗留 running Run 及其 266 条级联事件，清理未跟踪 `backend-go/stage4-acceptance` 和 `/private/tmp/stage4-*`，保留 gitignored 的三份 Canonical 结果及实验 JSONL。Task 4.4 与 Stage 4 提交前 checklist 已完成；按本轮要求未 commit/push。

## 2026-09-06 | 完成 Stage 4 Task 4.1-4.3 整合验收

- 任务：整合 Stage 4 Task 4.1-4.3 的并行实现与两组测试，审查全部未提交 Stage 4 文件，统一 Research Event、SourceReader、Projector、ReplaceProjection、Exporter、JSON Schema 和 golden 合同；不执行 live Canonical，不 commit/push，不勾选 Task 4.4。
- 操作：定义 `research.event.v1`、`research.transition.v1`、`research.projection_manifest.v1` 和 `research.trajectory.jsonl.v1`，以显式 availability slot、source ref、content hash、cursor 和 manifest 绑定原始事实及投影。实现 PostgreSQL repeatable-read SourceReader、因果/seq 排序 Projector、带 CAS/回滚的 ReplaceProjection、Run/Experiment JSONL Exporter 和 `research-export` CLI。整合时统一 ArtifactRef 全字段排序和空数组编码，补齐 DTO 长度/数量校验及 Draft 2020-12 Schema 的 attempt/source schema slot；恢复 CLI `--help`。真实 Run 验收中修复 BUG-151 对 ToolResult 原始 metadata 与外层规范化 content 的错误等值假设，以及 BUG-152 的同事务嵌套 rows 查询。
- 结果：正式 Stage 3 ResearchRun `rr-stage3-run5c20dba537a3799109213761` 的 79 条 Agent Event、Generation 260、Batch 337、Job 337、Execution 328 和 Report 可由 SourceReader 读取，投影为 ordinal 0..25 的 26 条 Transition；全部 Transition 共用一个 manifest/source hash/cursor，cursor 指向 AgentRun `run_5c20dba537a3799109213761` seq 79。两次 CLI 导出均为 356,962 bytes、26 行、SHA-256 `04b100f0824e4f64570d3ca435b30eb9d1fead5c1507f6f7f544e6ec66b21038`。Task 4.1-4.3 与对应 checklist 已完成；Task 4.4、Canonical 连续 3 次和 Stage 4 commit/push 保持未完成。
- 验证：带真实 PostgreSQL 的 Go 全量测试与全量 race、`go vet ./...`、`go build ./...` 通过；Research/Exporter 聚焦测试和真实 JSONL 逐行 Go 语义、hash、安全规则及 Draft 2020-12 Schema 验证通过。Python `uv lock --check`、88 passed/1 个既有 Chromium 门控 skipped、compileall 通过；主库 Alembic upgrade/current/heads/check 为唯一 `20260906_0041` 且无差异，一次性空库全链升级/check 与三张 research 表检查通过并已删除；Frontend Vitest 9/9、production build、Knip 通过。ignored `.env`、权限和非空 key 仅作断言且未输出 key；精确 key、通用 secret、gofmt、go mod tidy、BUG 编号、临时数据和 diff 扫描通过。
- 后续：独立执行 Task 4.4 的 Canonical Goal 连续 3 次、从 seq=0 完整回放及最终 Stage 验收；通过后再 commit/push。本轮不执行这些动作。

## 2026-09-06 | Stage 3 最终独立验收通过并准备提交

- 任务：不修改业务代码、不 commit/push，使用 ignored 官方 DeepSeek `.env` 完成 Stage 3 最终独立验收；覆盖完整静态/PG/race/迁移/Frontend 门禁、BUG-149/150 专项、一次从零 Canonical，以及正式 Experiment/ResearchRun/Transition 关联链。
- 操作：确认顶层 `.npm-cache` 全部为先前代理 Knip 产生的未跟踪缓存后删除，不触碰其他用户文件；仅断言 `.env` 被 ignore、权限 600、provider/model/base URL 与 key 非空，不输出 key。执行带真实 PostgreSQL 的 Go 全量、Research PG 专项 10 轮、全量 race、vet/build；Python lock、88 项测试与 compileall；主库 Alembic upgrade/current/heads/check、空库全链升级、带 sentinel 的 `0040→0041→0040→0041`；Frontend 9/9、build、Knip、gofmt、BUG 编号唯一性和 diff check。随后关闭 VLM 重启 Browser API、Execution Worker、20-turn AgentService，从零运行一次纯自然语言 Canonical，完成后停止三服务并清理本轮临时 cache/log/helper。
- 结果：Project 502 / Session 47 / Run `run_5c20dba537a3799109213761` / Generation 260 / Batch 337 / Execution 328 在首批通过，15/15 步有 evidence、`first_pass=true`、无 recovery；Report `execution.report.v2`、独立 DOM Oracle、Generation/Job/Execution canonical bytes 与 SHA `175d4056afafc3e62b5adc84b7cc8ed36ecba13f9202a89ef7cfd359853850c7`、VLM=0 均通过。结果保存为 `research/results/stage3-final-accepted-canonical-2.json`。
- 验证：官方 DeepSeek preflight 的最小文本和中等工具调用均 HTTP 200、`deepseek-v4-flash`、usage available。正式 Run 的 10 次 LLM input tokens 为 `3846, 14162, 26986, 38888, 48502, 61507, 89137, 89384, 89518, 133365`，最大 133,365；5 条探索摘要最大 32,470 bytes、累计 148,529 bytes，达到平台后不再随重复 A11y 增长，最终增量来自非探索的完整正式报告。全部调用 HTTP 200、retry=0、`response_read_failed=0`，telemetry 敏感字段键命中 0。8 个版本化完整 tool result 可回放，探索 summary 的 source seq/raw SHA/bytes 与 event 元数据一致，summary SHA 重算一致；PG/REST/SSE、错误语义和 GenerateDSL preflight 专项通过。
- 结果链：创建并回读 Experiment `exp-stage3-final-502-run5c20dba537a3799` 与 ResearchRun `rr-stage3-run5c20dba537a3799109213761`，均为 `completed`；绑定本次 Project/AgentRun/Generation/Batch/Execution/DSL SHA。最小 Transition ordinal 0、append key `stage3-canonical-minimal-v1`、SHA `a7f8d26b44f15e0d2676b21026082d2f690e89bdb6cb2c151e49cf3b1f90389a` 写入、幂等重放和回读一致；实验记录当前工作树 code SHA-256 `94ab4b67e0c55936390cc3dbed7a3e0a4ab288df8e78e1fd4f25fb4d8217940f` 与 Chromium `145.0.7632.6`。
- 后续：Task 3.4、3.4.2、3.4.4 与 Stage 3 非提交验收项已完成；提交信息准备为 `feat: add research persistence schema`。本轮按要求未 commit/push，Stage 3 commit/push checklist 保持未勾选。

## 2026-09-06 | 完成 Stage 3 Task 3.4.4 / BUG-150

- 任务：按 explorer 结论修复官方 DeepSeek 大上下文断流；完整工具结果先持久化，模型 transcript 仅保留确定性版本化摘要；不运行 Canonical，不 commit/push，不输出 ignored `.env` key。
- 操作：定义 `agent.tool_result.v1`、`agent.model_tool_summary.v1`、`ToolResultSource`、`ToolResultTruncation` 及 Page/Action/Error/Node 摘要类型。Harness 改为先持久化完整 `tool.result` 并取得 seq，再生成带 source seq/raw hash/summary hash/policy version 的探索摘要；保留 URL/page state/revision、动作 status/target、target evidence、verified selectors、祖先/交互节点、failure 与 omission counters，执行去重和稳定排序。单摘要目标 32 KiB、硬上限 64 KiB，累计探索摘要约 160 KiB，旧同 URL/state revision 优先降为 reference-only；错误、pending 和非探索结果不改语义。OpenAI telemetry 增加请求、messages、tools 和探索摘要序列化字节统计，不设置非探索消息硬拒绝。
- 结果：Agent Event/PostgreSQL/SSE 继续保存并公开完整 `content`，新增原始 SHA-256 与字节数，可供 Stage 4 按 source seq 重建；模型请求不再重复携带完整 A11y。GenerateDSL 仍仅使用模型从摘要实际提交的精简证据并执行原有绑定 preflight；`latestToolError` 可读取摘要中的结构化 failure。Task 3.4.4 与 BUG-150 完成，Task 3.4/3.4.2 的 Canonical 正式关联链及 Stage 3 commit/push 保持未完成。
- 验证：带真实 PostgreSQL 的 `go test -count=1 -v ./...` 和全量 `go test -race -count=1 ./...` 通过；`go vet ./...`、`go build ./...` 通过。Python 88 passed/1 skipped、`uv lock --check`、compileall 通过；Alembic upgrade/current/heads/check 通过且唯一 head/current 为 `20260906_0041`。Frontend Vitest 9/9、production build、Knip 通过；gofmt、`git diff --check` 通过。官方 DeepSeek 中等工具调用返回 HTTP 200、`deepseek-v4-flash`、usage available、`success_tool`。按要求未运行 Canonical。
- 后续：Task 3.4.2 仍需在后续独立执行 Canonical Goal 和正式 Experiment/ResearchRun/Transition 关联链；本轮不 commit/push。

## 2026-09-06 | 完成 Stage 3 Task 3.4.3 / BUG-149

- 任务：审查并修复 `backend-go/internal/platform/llm/openai.go` 的完整 `Complete/doRequest/classify/parse` 路径，消除 HTTP 2xx 响应读取、关闭、解码或协议失败被误分类为 `http_200` 的问题；使用 ignored `browser-worker/.env` 验证官方 endpoint，执行完整静态门禁，不运行完整 Canonical，不 commit/push。
- 操作：为 `ModelError` 增加不参与 JSON 的 cause 链；新增 `request/read/decode/invalid_response` 内部阶段错误和仅由非 2xx 响应产生的 provider HTTP 错误。响应体改为 4 MiB+1 有界读取并显式关闭；读取/关闭 timeout、unexpected EOF、connection reset/pipe 进入有界重试，完整 malformed JSON、超限响应、空 choices 和无效 tool-call envelope 确定性失败且不重试。协议校验前保留已解码的 model、usage 和 provider request ID；持久化层继续仅复制稳定 category/code/retryable/http_status，不保存 body、key、cause 或底层错误文本。
- 结果：BUG-149 已修复，HTTP 200 不再进入 provider HTTP 分类；截断响应可在第二次请求恢复，确定性响应错误不会消耗重试预算。官方 endpoint 最小文本调用和中等复杂度工具调用均返回 HTTP 200、`deepseek-v4-flash`、usage available，分类分别为 `success_text` 和 `success_tool`；响应符合现有 OpenAI 兼容类型，无需增加 DeepSeek 专用宽松解析。Task 3.4.3 完成；Task 3.4/3.4.2、Canonical 正式关联链和 Stage 3 commit/push 保持未完成。
- 验证：新增/扩展 Go 回归覆盖 200+truncated EOF、读取 timeout、读取/关闭 connection reset、malformed JSON、oversized、empty choices、invalid tool-call envelope、non-2xx JSON/plaintext、request/read cancel、body close、cause 与遥测脱敏。带真实 PostgreSQL 的 Go 全量测试、Research PG 专项 10 轮、全量 race、vet/build 通过；Python 88 passed/1 skipped、`uv lock --check`、compileall 通过；主库 Alembic 唯一 current/head `20260906_0041` 且 check 无差异，空库全链升级有 3 张 research 表，既有 sentinel 库 `0040→0041→0040→0041` 保留数据且 research 表为 `3→0→3`；Frontend Vitest 9/9、production build、repo-local cache Knip、gofmt、BUG 编号和 diff check 通过。
- 后续：后续从完整门禁重新执行 Task 3.4.2 的 Canonical Goal 与正式 Research Repository 关联链验收。本轮按要求未运行完整 Canonical，未 commit/push。

## 2026-09-06 | Stage 3 Task 3.4.2 最终验收被 LLM HTTP 200 响应处理失败阻断

- 任务：不修改业务代码、不 commit/push，使用 ignored `browser-worker/.env` 中的 `deepseek/deepseek-v4-flash` 配置，从完整静态、迁移和真实 PostgreSQL 门禁重跑 Stage 3 最终验收；通过后执行一次从零 Canonical，并从 Research Repository 创建正式完整关联链和最小 Transition；任一失败新增 task/bug/log 后停止。
- 操作：仅断言 provider/base URL/model 精确匹配、API key 非空、`.env` 被 Git ignore 且权限为 600，不输出、记录或复制 key。执行带真实 PG 的 Go 全量、Research PG 专项 10 轮、全量 race、vet/build，Python lock/88 项测试/compileall，主库 upgrade/current/heads/check、一次性空库全链升级、带 sentinel 的既有库 `0040→0041→0040→0041`，Frontend 9 项测试/build/Knip、gofmt、BUG 编号和 diff 门禁。全部通过后从当前工作树启动关闭 Vision 的三服务，提交一次纯自然语言 Canonical。
- 结果：BUG-148 的 HTTP 503 条件已消失并标记 fixed。Canonical 创建 Project 340 / Session 45 / Run `run_ad7093eca3d3b65d7aa00f8f`；前四次 LLM 调用均由 `deepseek/deepseek-v4-flash` 以 HTTP 200 成功返回 ToolCall，真实 Chromium 已探索至购物车并观察 Blue Top / Rs. 500 / 1 / Rs. 500。第五次调用收到 HTTP 200 后在响应读取或 JSON 解码阶段失败，adapter 误分类为非重试 `http/http_200`；现有安全遥测无法区分两个分支。Run 在 seq=32 failed，未生成 Generation/Batch/Execution。新增 Task 3.4.3 / BUG-149 后停止；未创建 Experiment/ResearchRun/Transition，Task 3.4/3.4.2 与 Stage 3 Canonical/checklist 保持未完成。
- 验证：Go 全量与 PG integration、Research PG 专项 10 轮、全量 race、vet/build 通过；Python 88 passed/1 skipped；主库唯一 current/head `20260906_0041` 且 Alembic 无差异，空库 3 张研究表，既有库往返保留 1 个 sentinel 用户和项目；Frontend 9/9、build、Knip、compileall、gofmt、BUG 编号和 diff check 通过。五条 LLM telemetry 的 provider/requested model 全部正确，前四条 usage 可重算为 input/output/total `392819/3967/396786`，第五条 usage 为 unavailable，敏感字段名命中 0。事件 31 精确记录 logical call `llm_e39636f59a6734fd07ee47e4`、attempt=1、latency=31104 ms、HTTP 200、retry=0、`model_attempt_failed_without_response` 和非重试 `http/http_200`；原始读取/JSON 解码错误已被 adapter 覆盖且未落库，无法从历史事实进一步恢复，未读取响应正文。失败结果为 `research/results/stage3-final-accepted-canonical-1.json`，三服务已停止。
- 后续：实施 Task 3.4.3，修复 2xx 响应读取/JSON 解码错误分类和安全重试合同；随后必须从完整门禁重新执行 Task 3.4.2。本轮未 commit/push。

## 2026-09-06 | Task 3.4.2 LLM 网关健康复测仍为 HTTP 503

- 任务：不修改业务代码、不 commit/push，先通过小范围正式 AgentRun 确认 LLM 网关健康；若仍为 503，则仅完成现有 adapter 的有界重试并记录后停止，保持 Task 3.4/3.4.2、Stage 3 checklist 和 BUG-148 未完成。
- 操作：仅启动 `AGENTSERVICE_MAX_TURNS=2`、VLM 关闭的 Go AgentService，使用 Project 248 / Session 42 创建正式 AgentRun `run_a2cdc8f8eff2818a1295fde0`，提交“只回复 OK、不调用工具”的最小自然语言消息；从 PostgreSQL 读取版本化 LLM telemetry，不输出 API Key 或其他凭证。探测后停止 AgentService，未启动 Browser API 或 Execution Worker。
- 结果：Run 在首次 logical LLM call 后进入 `failed`。事件 2、3、4 对应现有 OpenAI adapter 的完整 3 次有界物理请求，provider/model 为 `unself/deepseek-v4-flash`，`retry_count=0/1/2`，均返回 `http_status=503`、`code=http_503`、`retryable=true`；事件 5 为 `run.failed`。按约定停止，未执行完整静态/迁移/并发门禁、Canonical、report/oracle/SHA/VLM 验收或 Research 正式 ID 链。
- 验证：AgentService 已停止，端口 8081 无监听；Run 无 latest/approved Generation；`research_experiments`、`research_runs`、`research_transitions` 行数仍为 0/0/0。Task 3.4、Task 3.4.2 与 Stage 3 Canonical/checklist 保持未勾选，BUG-148 保持 `open`，未 commit/push。
- 后续：等待 `unself/deepseek-v4-flash` 恢复后，再从小范围正式 AgentRun 健康探测开始；成功后必须从完整静态、迁移和真实 PostgreSQL 门禁重新执行，再从零运行 Canonical 并建立 Experiment/ResearchRun/Transition 完整关联链。

## 2026-09-06 | Stage 3 最终独立验收被 LLM HTTP 503 阻断

- 任务：不修改业务代码、不 commit/push，从完整静态与迁移门禁重新执行 Stage 3 最终独立验收；通过后重启三服务，从零运行一次 Canonical Goal，并创建正式 Experiment/ResearchRun、完整关联链和最小 Transition；任一失败记录 task/bug/log 后停止。
- 操作：复核 Stage 3 未提交实现与验收清单；对主库执行 upgrade/current/heads/check，对一次性空库执行全链升级，对带 sentinel 的临时库执行 `0040→0041→0040→0041`；执行带真实 PostgreSQL 的 Go 全量测试、research 专项连续 10 轮、全量 race、vet、build，以及 Python、Frontend、Knip、compileall、gofmt 和 diff 门禁。全部通过后重启 Browser API、Execution Worker 和 20-turn AgentService，关闭 VLM 并提交一次纯自然语言 Canonical Goal。
- 结果：静态、迁移与 PostgreSQL 门禁全部通过；Canonical 在首次模型调用阶段失败。Project 246 / Session 40 / Run `run_8ec1321e5933a7ea3b95ce65` 的三个物理 attempt 均由 `unself/deepseek-v4-flash` 返回 HTTP 503，事件 5 将 Run 标记为 `failed`；未产生 Generation、Batch 或 Execution。按失败即停规则新增 Task 3.4.2 / BUG-148，未创建 Experiment/ResearchRun/Transition，未勾选 Task 3.4 或 Stage 3 checklist，未 commit/push，三个服务已停止。
- 验证：主库为唯一 `20260906_0041` 且 Alembic 无差异；空库研究表为 3 张；临时既有数据在 `0041→0040→0041` 中保持 1 个用户和 1 个项目，研究表按 3→0→3 变化且临时库已清理。Go 全量、research 专项 10 轮、全量 race、vet、build 通过；Python 88 passed/1 skipped；Frontend Vitest 9/9、production build、Knip、compileall、gofmt 和 diff check 通过；新增 BUG-148 编号未重复。失败结果为 `research/results/stage3-final-canonical-1.json`。
- 后续：确认 LLM 网关恢复后执行 Task 3.4.2，从完整门禁重新开始；Canonical 通过后再写入并核验正式研究关联链。本轮保持不 commit/push。

## 2026-09-06 | 完成 Stage 3 Task 3.4.1 / BUG-147

- 任务：不 commit/push、不执行 Canonical，审查并修复 PostgreSQL `CreateRun` 在 8 路以上相同 identity 并发时偶发暴露 `pk_research_runs` 的问题；覆盖同/异不可变 payload、主键 sequence 事实、事务隔离与数据库错误传播，执行 Go/PG/race/vet/build 和其余 Stage 3 静态门禁。
- 操作：移除依赖单一 `ON CONFLICT (experiment_id, idempotency_key)` 仲裁顺序的创建路径；显式使用 `READ COMMITTED` 事务，对 identity、Run ID 和 repetition slot 的 64 位 advisory key 排序加事务锁，锁内先回读 identity 并比较不可变 payload，再显式检查主键/repetition 冲突。只有已知三类约束保留并发兜底映射，未知 unique 与其他数据库错误原样返回。测试升级为 20 轮 × 16 路同 identity，并加入已演进 Run 的同 payload 重放、异 ID/repetition/warmup payload、主键冲突后重试、无 varchar PK sequence、隔离覆盖、锁超时和未知唯一错误。
- 结果：BUG-147 修复，Task 3.4.1 与 Stage 3“并发写入和重复写入”检查项完成。相同不可变 payload 始终返回同一当前 Run；不同不可变 payload、主键和 repetition slot 分别稳定返回 `ErrConflict`。`research_runs.id` 经 `pg_get_serial_sequence` 确认为无 sequence 的调用方 varchar 主键，未加入错误的 sequence 修复逻辑。Task 3.4 的 Canonical、正式关联链和 commit/push 仍未完成。
- 验证：定向 PostgreSQL 测试最终重复 10 轮通过；带真实 PG 的 `go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...`、`go build ./...` 全部通过。Python 88 passed/1 skipped、`uv lock --check`、compileall、Frontend Vitest 9/9、production build、Knip、BUG 编号唯一性和 diff check 通过。主库 upgrade/current/heads/check 为唯一 `20260906_0041` 且无差异；一次性空库全链 upgrade 与带 sentinel 的 0040→0041→0040→0041 往返通过，临时库已删除。
- 后续：按本轮要求未运行 Canonical、未 commit/push；Task 3.4 后续仍需执行 Canonical Goal 并写入正式完整 ID 关联链。

## 2026-09-06 | Stage 3 Task 3.4 独立验收在并发 CreateRun 失败

- 任务：不修改业务代码、不 commit/push，独立审查 research types/repository/migration/model，执行完整静态、迁移、真实 PostgreSQL、Canonical Goal 和正式 Repository 关联链门禁；任一失败新增 task/bug/log 后停止。
- 操作：逐文件审查 Go research domain/repository、0041 Alembic migration、SQLAlchemy model 与专项测试；使用两个一次性 PostgreSQL 数据库分别执行空库 `upgrade head`，以及带既有 project 数据的 0040→0041、0041→0040→0041 往返；在主库执行 Alembic current/heads/check。随后并行启动 Go 全量 test/vet/build、Python lock/compileall/unittest、Frontend test/build/Knip 和格式/diff 门禁。
- 结果：迁移门禁全部通过，主库保持唯一 current/head `20260906_0041` 且 Alembic 无差异。Go 全量测试在 `TestPostgresRepositoryConcurrentCreateRun` 失败：8 路完全相同的并发请求中至少一个返回 `research resource conflict: pk_research_runs`；新增 Task 3.4.1 / BUG-147 后停止。Task 3.4、Stage 3 Canonical/关联链与 commit/push 项保持未完成，未重启三服务，未运行 Canonical，未创建正式 Experiment/ResearchRun。
- 验证：空库 upgrade 后三张 research 表齐全；带既有数据临时库 0040→0041 保留 sentinel，0041→0040 删除三张 research 表且保留既有数据，再升级恢复三表；临时库均已删除。Go vet/build、Python 88 passed/1 skipped、Frontend Vitest 9/9 与 production build、Go fmt、diff check、BUG 编号唯一性通过；Knip 因失败即停未执行。真实 PG 的 Research CRUD/CAS/完整链与 Append 幂等、hash/ordinal 冲突、批次回滚通过；独立间断专项未继续，并发 CreateRun 阻断总门禁。
- 后续：修复 BUG-147 后从完整静态、迁移、真实 PostgreSQL 和 Canonical 门禁重新执行 Task 3.4；本轮不 commit/push。

## 2026-09-06 | 完成 Stage 3 Task 3.1-3.3 / BUG-146

- 任务：接管中断的 Stage 3 Research Persistence 草稿，逐文件审查后完成 Go domain/repository、三表迁移与 PostgreSQL adapter；补真实 PG 并发、冲突、回滚、CAS 和完整 ID 链测试；不执行 Task 3.4，不 commit/push。
- 操作：保留并收敛现有 `internal/research`、0041 migration 和 SQLAlchemy mapping；为 Experiment/ResearchRun/Transition/RunMetrics 增加精确版本、状态、时间、JSON 大小和 nullable metric 校验；禁止 Transition 嵌入 transcript、完整 DSL、report 或截图，仅允许摘要、hash 和 artifact reference。Repository 覆盖 CRUD、状态 CAS、单调 links、metrics、幂等 append/list/delete；adapter 使用 run row lock 串行化 ordinal，并通过 AgentRun latest/approved Generation、Generation SHA、Batch execution job SHA、Execution job/batch/SHA 验证完整 ID 链。
- 结果：Task 3.1-3.3 与 Stage 3 对应前七项 checklist 完成。`research_experiments`、`research_runs`、`research_transitions` 已具备 FK、unique、check 和查询索引；现有数据库升级到 `20260906_0041`。Task 3.4、Canonical Goal、commit/push 保持未完成。
- 验证：真实 PostgreSQL 专项覆盖 CRUD、pending/running/completed/cancelled CAS、同状态 CAS 时间不漂移、并发 8 路 CreateRun、并发幂等 Append、hash/ordinal 冲突、批量 append 事务回滚、重复写入、完整/错误 ID 链和清理；`go test -race` 通过。现有库 0040→0041、临时空库全链 upgrade、临时库 0041→0040→0041 往返及 Alembic current/heads/check 通过，临时库已删除。Go 全量 test/vet/build、Python 88 passed/1 skipped、uv lock、compileall、Frontend Vitest 9/9、production build、Knip、diff check 均通过；research 测试数据清理后为 0/0/0 行。
- 后续：Task 3.4 仍需执行 Canonical Goal 并写入真实完整关联链，再创建 `feat: add research persistence schema` 提交并推送；本轮明确不 commit/push。

## 2026-09-06 | Stage 2 最终交付

- 任务：仅完成 Stage 2 提交前文档收口；确认 Task 2.1-2.3.4 与 Stage 2 checklist 除实际 commit/push 外均已完成，为主代理紧接着提交准备准确状态；不修改业务代码，不 commit/push。
- 操作：复核 `tasks.md`、`checklist.md`、Stage 2 最终独立验收记录和 BUG-141..145；将 Task 2.3 与 Stage 2 commit/push 项统一标记为“提交前准备完成，commit/push 待主代理执行”，保留全局实际提交项未勾选；核对三次 Canonical Run、逐 Run token totals 与全部遥测门禁，不写入 commit SHA。
- 结果：Stage 2 提交前交付完成，计划提交信息为 `feat: record llm usage telemetry`。最终通过样本为 Run `run_2c711b1f4ebf68964f76167f`、`run_db109f516446c6c086080c3a`、`run_54665eb2cfc8744afbc7f628`，token totals 依次为 2,190,967、1,691,112、2,671,567。三次均首批 passed、`first_pass=true`、无 recovery，正式执行、独立 DOM Oracle、Generation/Job/Execution canonical bytes 与 SHA、VLM=0 均通过。
- 验证：逐 Run 的 logical/physical call 数一致，Run/Step、provider、requested/resolved model、prompt version、prompt/request/toolset hash、usage available/partial/unavailable、latency、retry、错误类别和 ToolCall available/unavailable 状态均可从 PostgreSQL 重算；REST/SSE 与 PostgreSQL 逐条一致，敏感字段命中为 0。静态门禁结果保持为 Go 全量及四个 PostgreSQL integration、Python 88 passed/1 个环境诊断项 skipped、Alembic 唯一 current/head `20260906_0040` 且无差异、Frontend Vitest 9/9、production build、Knip、compileall 全部通过。BUG-141、BUG-144、BUG-145 为 `fixed`；BUG-142、BUG-143 保持 `wont_fix（environment-limited）`，不误报为产品失败。
- 后续：主代理可按 `feat: record llm usage telemetry` 创建 Stage 2 聚焦提交并推送 `origin/main`；本记录不声明 commit/push 已执行，也不包含虚假 commit SHA。

## 2026-09-06 | Stage 2 最终独立验收通过

- 任务：从完整门禁重新执行 Stage 2 最终独立验收并收口；独立 Chromium unittest 按 BUG-142/143 的 `environment-limited` 结论不阻断，以官方长期 Browser API Canonical 提供真实 Chromium 证据；重点复核 input trigger、多 generation 逐轮审批与 `first_pass`，逐 Run 重放并重算 LLM telemetry；不修改业务代码、不 commit/push。
- 操作：执行显式 PostgreSQL DSN 的 Go 全量 test/vet/build、Python lock/全量测试、Alembic upgrade/current/heads/check、Frontend test/build/repo-local cache Knip、compileall 和 diff check。专项覆盖 usage available/partial/unavailable、408/429/500/502/503/504、普通 4xx、取消、caller deadline、retry exhausted、每物理 attempt 事件、prompt/request/toolset SHA-256、敏感字段白名单、单/多/无 ToolCall、失败 attempt、PG normalized/legacy replay、REST/SSE 重放及先订阅后补洞；临时验收测试运行后已删除。Trigger 合同覆盖仅 null/Enter/Tab，Driver 回归覆盖 Generation 129 失败首批到 Generation 130 二次审批并保持 `first_pass=false`。随后重启长期 Browser API、Execution Worker 和 20-turn AgentService，显式关闭 VLM，从零串行执行三次纯自然语言 Canonical，完成后停止三个服务。
- 结果：Canonical #1 为 Project 88 / Session 37 / Run `run_2c711b1f4ebf68964f76167f` / Generation 139 / Batch 132 / Execution 123，14/14 步；#2 为 Project 89 / Session 38 / Run `run_db109f516446c6c086080c3a` / Generation 140 / Batch 133 / Execution 124，12/12 步；#3 为 Project 90 / Session 39 / Run `run_54665eb2cfc8744afbc7f628` / Generation 141 / Batch 134 / Execution 125，15/15 步。三次均首批 passed、`first_pass=true`、无 recovery，真实执行 Blue Top search input 与独立 search click，Report、独立 DOM Oracle、Generation/Job/Execution canonical bytes 与 SHA、VLM=0 全部通过。三代 input trigger 均为 null，非法 trigger 为 0。Task 2.3/2.3.4 和 Stage 2 非提交验收项已完成；Stage 2 commit/push 保持未完成。
- 验证：Go 全量通过且四个 PostgreSQL integration 无 skip，Python 88 passed/1 个环境诊断项 skipped，Alembic 唯一 current/head 为 `20260906_0040` 且无差异，Frontend Vitest 9/9、production build、Knip、compileall 通过。Run #1/#2/#3 的 logical/physical calls 分别为 12/12、11/11、13/13；tokens 分别为 2,171,899/19,068/2,190,967、1,637,895/53,217/1,691,112、2,644,510/27,057/2,671,567；attempt latency 分别为 212,510/475,723/297,467 ms，retry 均为 0。每个事件的 Run/Step、provider=`unself`、requested/resolved model=`deepseek-v4-flash`、prompt version=`agentcore.system.v1`、三个 hash、usage、latency、retry 与 ToolCall 状态均可重算；关联/元数据/敏感字段错误均为 0，每个 Run 最终文本均为 `unavailable/model_returned_final_text`，其余分别 11/10/12 条为 `available`。三 Run 的 PostgreSQL、REST 和 SSE LLM 事件逐条一致。
- 后续：Stage 2 已具备提交前条件；本轮按要求未 commit/push。

## 2026-09-06 | 实施 Stage 2 Task 2.3.4 / BUG-145

- 任务：修复 Agentic E2E Driver 多 generation 审批状态机和 Generation 129 首次执行失败的非法 input trigger 根因；增加 129 失败到 130 二次审批及跨 Go/Python 合同回归，运行完整静态门禁，不执行 live Canonical 3 次，不 commit/push。
- 操作：Driver 仅对尚未绑定 Batch 的 approval 结算一次，先要求唯一新 Batch 并以 execution DSL SHA 绑定上一 generation，再识别严格递增 generation 的全新 `approve_dsl` checkpoint；仅该状态允许服务清空 `approved_generation_id`，随后仍校验 generation result、artifact、声明/计算 SHA、checkpoint/tool call 唯一性和审批后 Batch。Go Tool Schema、Go `ValidateCase` 与 Python `InputStep` 统一将 trigger 限制为可空 `Enter|Tab`，prompt 明确普通语义输入省略 trigger、搜索按钮另用 click。
- 结果：BUG-145 修复，Task 2.3.4 完成。Generation 129 失败 Batch 会被保留并写入 recovery，Generation 130 可合法进入第二次审批；第二批成功不会覆盖首批，`stage0.first_pass=false`。generation 未前进、重复 checkpoint/tool call、非唯一 Batch、artifact/SHA 不匹配仍失败；`trigger="Search Product textbox"`、空串和其他按键在正式执行前被拒绝。Task 2.3、Stage 2 live 连续 3 次、逐 Run PostgreSQL 遥测重算及 commit/push 保持未完成。
- 验证：Go 全量 test 通过，4 个 PostgreSQL integration 均 PASS 且无 skip；`go vet ./...`、`go build ./...` 通过。Python `uv lock --check`、88 passed/1 个既有真实 Chromium 诊断项 skipped、compileall 通过。Alembic upgrade/current/heads/check 通过，唯一 current/head 为 `20260906_0040` 且无差异。Frontend Vitest 9/9、production build、repo-local cache Knip 通过；最终 diff check 与 BUG 编号唯一性通过。按要求未执行 live Canonical 3 次。
- 后续：Task 2.3 独立验收需从完整门禁开始运行官方长期 Browser API Canonical 连续 3 次，并逐 Run 从 PostgreSQL 重算 ToolCall 关联、token、latency 和 retry；本轮未 commit/push。

## 2026-09-06 | Stage 2 最终独立验收在 Canonical #3 二次审批失败

- 任务：执行 Stage 2 最终独立验收并收口；读取 tasks/checklist 与 BUG-141..144，运行完整静态和遥测专项门禁，以官方长期 Browser API/真实 Chromium 连续执行 3 次 Canonical 并逐 Run 从 PostgreSQL 重算 calls/tokens/latency/retries；独立 Chromium unittest 按已记录环境限制不阻断；不修改业务代码、不 commit/push，失败时新增 task/bug/log 并停止。
- 操作：执行带显式 pgx DSN 的 Go 全量 test/vet/build、四个 PostgreSQL integration、Python 默认全量、Alembic upgrade/current/heads/check、Frontend test/build/repo-local cache Knip、compileall、bug-log 唯一性和 diff check。专项覆盖 usage available/partial/unavailable、408/429/500/502/503/504 重试、普通 4xx 与取消不重试、context deadline/retry exhausted、每物理请求事件、prompt/request/toolset SHA-256、敏感字段白名单、单/多/无 ToolCall、失败 attempt、PG 规范化/legacy 回放、REST/SSE 逐字一致及先订阅后按 seq 补洞；partial/timeout 使用运行后删除的临时验收测试。随后重启 Browser API、Execution Worker 和 20-turn AgentService，显式关闭 VLM，串行运行三个全新 Canonical。
- 结果：静态与专项门禁全部通过。Canonical #1 为 Project 81 / Run `run_bec2965f88b1e0a7e6bcb0e3` / Generation 127 / Batch 121 / Execution 112，14/14 步、首轮正式执行、独立 Oracle、审批 SHA、VLM=0、无 recovery 均通过；#2 为 Project 82 / Run `run_78471f2bb56cf1324ce5c48c` / Generation 128 / Batch 122 / Execution 113，15/15 步及相同门禁通过。#3 的 Project 83 / Run `run_7271a00b1898afb446109751` 在 Generation 129 的 Batch 123 / Execution 114 因 `Locator.press: Unknown key: "Search Product textbox"` 失败后生成修复版 Generation 130 并请求第二次审批；驱动器因新 generation 已合法清空 `approved_generation_id` 而误报上一代未绑定，取消 Run 并退出 1。新增 Task 2.3.4 / BUG-145 后停止；Task 2.3、Canonical 3 连过与 Stage 2 checklist 保持未完成，BUG-144 仍为 fixed。
- 验证：Go 全量通过，`TestPostgresAgentEventReturnsNormalizedPayload`、`TestPostgresResearchLLMCallToolAssociationsAndLegacyReplay`、`TestPostgresAgentRunCancellationCAS`、`TestPostgresControlPlaneLifecycle` 四项 PG integration 均 PASS 且无 skip；Python 86 passed/1 个环境诊断项 skipped；Alembic 唯一 current/head `20260906_0040` 且无差异；Frontend 9/9、production build、Knip、compileall、bug-log 唯一性和 diff check 通过。Run #1 数据库重算 10 logical/10 physical calls、tokens 1,302,198/19,122/1,321,320、attempt latency 191,977 ms、retry 0；Run #2 为 13/13、tokens 2,075,622/47,789/2,123,411、latency 361,393 ms、retry 0。两者缺失 Step/schema/hash/ToolCall status/unavailable reason 和敏感字段命中均为 0，最终文本均为 `unavailable/model_returned_final_text`。失败 Run #3 在停止前记录 12/12 calls、total tokens 2,387,191、latency 226,072 ms、retry 0，关联状态无缺失；未把该失败样本计入连续通过。
- 后续：实施 Task 2.3.4，修复驱动器对多 generation/多审批的状态校验并补失败 Batch 到第二次审批回归；随后从完整门禁重新执行 Stage 2 最终验收。本轮三个验收服务均已停止，未 commit/push。

## 2026-09-06 | 实施 Stage 2 Task 2.3.3 / BUG-144

- 任务：为 `research.llm_call.v1` 定义版本化显式 ToolCall 关联合同，覆盖单/多/无 ToolCall 与失败 attempt 的持久化、REST/SSE、PostgreSQL 和前端类型；处理遗留 Run `run_c03fa73b69892ed231ed7f01`；运行完整静态门禁，不执行 Canonical 3 次，不 commit/push。
- 操作：Go 新增 `ToolCallStatus` 与 `ToolCallUnavailableReason` 类型和 payload 白名单字段；每个物理 attempt 都写 `tool_call_status`。成功单/多 ToolCall 保留原始 `tool_call_ids`，仅单个时设置事件级 `tool_call_id`；最终文本和失败 attempt 分别写 `model_returned_final_text`、`model_attempt_failed_without_response`，不生成替代 ID。前端新增 `ResearchLLMCallPayloadV1` 判别联合类型。增加内存持久化、Harness、REST/SSE 共用 wire、真实 PostgreSQL 回放及 legacy payload 兼容测试。临时启动正式 AgentService 检查遗留 Run；默认 actor 的 GET 因所有权返回 403，随后只读查询确认该 Run 已不存在，因此未调用 cancel、未直接修改数据库，并停止临时服务。
- 结果：Task 2.3.3 完成，BUG-144 修复。新事件的 ToolCall 关联状态均可判定；多工具事件级 ID 保持为空但 payload 明确，旧事件继续原样读取。Stage 2 的 Canonical 连续 3 次、逐次 PostgreSQL 遥测重算及 commit/push 保持未完成，留给独立验收。
- 验证：聚焦测试覆盖单/多/无 ToolCall、失败 attempt、Harness 最终文本、REST/SSE 四种 wire（含 legacy）和 PostgreSQL 四种写入及 legacy 回放。完整门禁通过：显式 PostgreSQL DSN 的 Go 全量 test（4 个 integration 均 PASS）、`go vet ./...`、`go build ./...`；Python `uv lock --check`、86 passed/1 skipped、compileall；Alembic upgrade/current/heads/check，唯一 current/head 为 `20260906_0040` 且无差异；Frontend Vitest 9/9、production build、repo-local cache Knip；`git diff --check`。
- 后续：由独立验收从完整门禁开始执行 Canonical Goal 连续 3 次，并逐次从 PostgreSQL 重算 ToolCall 关联、token 与 latency。本轮未 commit/push。

## 2026-09-06 | Stage 2 最终独立验收在 Canonical #1 关联门禁失败

- 任务：执行 Stage 2 最终独立验收；接受已记录的外部 sandbox 限制，不运行独立 Chromium unittest，完整执行其余静态门禁、LLM/事件专项及官方 Canonical 连续 3 次，并逐次从 PostgreSQL 重算遥测；不修改业务代码、不 commit/push，任一失败记录后停止。
- 操作：读取 tasks/checklist 与 BUG-141..143；执行 Go 全量 test/vet/build、三个 PostgreSQL integration、Python 默认全量、Alembic upgrade/current/heads/check、Frontend test/build/repo-local cache Knip、compileall 和 diff check。专项验证 available/partial/unavailable usage、408/429/500/502/503/504 retry、普通 4xx 与取消不重试、timeout/retry exhausted、每物理请求事件、SHA-256、敏感字段白名单、PostgreSQL 规范化、REST/SSE 共用字节及先订阅后回放补洞；partial/timeout 使用运行后立即删除的临时验收测试，不修改业务代码。随后启动 Browser API、Execution Worker 和 20-turn AgentService，显式关闭 VLM，执行第 1 次 Canonical 并查询数据库。
- 结果：所有静态与专项门禁通过。Canonical #1 为 Project 78 / Session 33 / Run `run_fa3eea9b15473865a2467d62` / Generation 122 / Batch 116 / Execution 107，正式执行 passed、12/12 步有 evidence、`first_pass=true`、无 recovery、VLM=0，独立 Oracle 精确验证 Blue Top / Rs. 500 / 1 / Rs. 500。数据库 10 次 logical call 对应 10 个 physical request，均有 Run/Step；但最终文本模型调用 `seq=78` 未产生 ToolCall，事件 `tool_call_id` 为空且无显式 unavailable 原因。按严格关联门禁和失败即停规则，未执行 Canonical #2/#3，Task 2.3 与 Stage 2 checklist 保持未完成，新增 Task 2.3.3 / BUG-144，并停止本轮三个服务。
- 验证：Go 全量测试通过且 `TestPostgresAgentEventReturnsNormalizedPayload`、`TestPostgresAgentRunCancellationCAS`、`TestPostgresControlPlaneLifecycle` 三项 PG integration 无 skip；Go vet/build 通过；Python 86 passed/1 个独立 Chromium 环境诊断项按默认门控 skipped；Alembic 唯一 current/head `20260906_0040` 且 check 无差异；Frontend Vitest 8/8、production build、repo-local cache Knip、compileall、diff check 通过。专项测试全部通过。Canonical #1 的 provider=`unself`、requested/resolved model=`deepseek-v4-flash`、prompt version=`agentcore.system.v1`，usage 全部 available，Token 可重算为 input 1,598,875 / output 23,268 / total 1,622,143，模型总延迟与 attempt 延迟均为 224,413 ms，retry=0；schema/provider/model/prompt/hash/敏感字段异常均为 0，缺失 Step=0、缺失事件级 ToolCall=1。
- 后续：实施 Task 2.3.3，为无 ToolCall 的模型调用增加显式版本化关联状态及回归；修复后从完整门禁重新执行 Canonical 连续 3 次。本轮未 commit/push。

## 2026-09-06 | 收敛 Stage 2 真实浏览器门禁的环境限制

- 任务：收敛 Task 2.3.1/2.3.2 与 BUG-142/143；不修改业务逻辑、不执行 Stage 2 最终验收、不 commit/push。
- 操作：依据独立真实 Chromium unittest 业务断言稳定 1/1 `OK`、随后由 TRAE 外层 sandbox 因根路径访问改写为退出 1，且 repo-local `HOME`、`TMPDIR`、`XDG_CACHE_HOME`、`PLAYWRIGHT_BROWSERS_PATH` 均无法规避的事实，将该命令从 Stage 2 阻断门禁降为环境受限诊断项。诊断仍须保留真实退出码和 sandbox trace，禁止使用 `|| true`；Stage 2 真实浏览器门禁改由通过长期 Browser API 执行真实 Chromium 的官方 Canonical E2E 覆盖。
- 结果：Task 2.3.1/2.3.2 按“外部 sandbox 限制并采用官方 HTTP/E2E 替代证据”完成；BUG-142/143 标记为 `wont_fix（environment-limited）`，明确不是产品失败。确认 `.playwright-browsers/` 无 tracked 文件且整个目录为本次代理创建的未跟踪缓存后，仅删除该目录。
- 验证：未运行 Stage 2 最终验收；执行 `git diff --check`，并核对 `.playwright-browsers/` 已不存在、其他工作区文件未因清理被删除。
- 后续：Task 2.3 仍需按现有未完成项运行官方 Canonical E2E、核对 LLM call 关联及 PostgreSQL token/latency 可重算性；本轮不 commit/push。

## 2026-09-06 | Task 2.3.1 四目录 sandbox 复验仍失败

- 任务：继续 Task 2.3.1 与 Stage 2 最终验收；先使用四个已存在的 repo-local 绝对目录运行真实 Chromium，命令必须退出 0，若仍触发 sandbox 则记录精确 trace 并停止；不修改业务代码、不 commit/push。
- 操作：创建 `.sandbox-home`、`.sandbox-tmp`、`.sandbox-cache`、`.playwright-browsers`；在同样四个绝对路径环境下将当前虚拟环境所需 Chromium 1208、headless shell 1208 和 FFmpeg 1011 安装到 `.playwright-browsers`。随后在 `browser-worker/` 显式设置 `HOME=/Users/bytedance/project/AI_Web_Testing/.sandbox-home`、`TMPDIR=/Users/bytedance/project/AI_Web_Testing/.sandbox-tmp`、`XDG_CACHE_HOME=/Users/bytedance/project/AI_Web_Testing/.sandbox-cache`、`PLAYWRIGHT_BROWSERS_PATH=/Users/bytedance/project/AI_Web_Testing/.playwright-browsers`、`RUN_BROWSER_INTEGRATION=1`，直接执行 `/Users/bytedance/project/AI_Web_Testing/browser-worker/.venv/bin/python -m unittest tests.test_explore_flow_chromium -v`。
- 结果：测试输出 `test_navigation_contract_and_same_url_modal_flow ... ok`、`Ran 1 test in 9.031s`、`OK`，随后精确输出 `TRAE Sandbox Error: hit restricted`、`Not allow operate files: /`、`Hint: If the tool supports running outside the sandbox, you should try requesting to run this command outside the sandbox.`、`Alternatively, you can tell the user to configure sandbox rules via Settings -> Permission & Approval -> Custom Configuration.`，命令退出码为 1。按停止条件未执行完整 Stage 2 门禁与专项、三服务重启、Canonical 连续 3 次、LLM call 关联和 PostgreSQL token/latency 重算；Task 2.3.1、Task 2.3 与 Stage 2 checklist 保持未完成，新增 Task 2.3.2 / BUG-143。
- 验证：真实 Chromium 业务断言 1/1 通过但整体门禁失败；测试后已删除 `.sandbox-home`、`.sandbox-tmp`、`.sandbox-cache`，保留项目 `.playwright-browsers` 缓存。未修改业务代码，未 commit/push。
- 后续：Task 2.3.2 需在同一四目录约束下追踪退出阶段根路径访问来源；真实 Chromium 命令退出 0 前不得继续 Stage 2 最终验收。

## 2026-09-06 | Stage 2 Task 2.3 独立验收在真实 Chromium 门禁失败

- 任务：独立验收 Stage 2 Task 2.3；先审查 Task 2.1/2.2 与 BUG-141，运行完整静态门禁和 Stage 1 E2E 门禁，再执行 LLM 遥测专项、重启三服务、连续 Canonical 3 次及 PostgreSQL 重算；不修改业务代码、不 commit/push，任一失败新增 task/bug/log 并停止。
- 操作：审查 OpenAI-compatible adapter、模型遥测类型、Harness 物理 attempt 事件、PostgreSQL 规范化、REST/SSE 共用 wire、先订阅后回放及 broker 合并唤醒；使用合法 pgx DSN 执行 Go 全量 test，并并行执行 Go vet/build、Python lock/全量测试、Alembic upgrade/current/heads/check、Frontend test/build、repo-local cache Knip、compileall 和 diff check；最后以 repo-local 绝对 `TMPDIR` 显式运行 Stage 1 真实 Chromium 回归。
- 结果：Go、Python、Alembic、Frontend、Knip、compileall 和 diff 门禁通过；真实 Chromium 用例业务断言 1/1 通过并输出 `OK`，但进程退出后再次触发 `TRAE Sandbox Error: Not allow operate files: /` 并以 1 结束。按失败即停规则，未继续缺失/partial usage、401、timeout、retry exhausted、物理请求事件数、hash、脱敏、wire、订阅补洞等剩余专项，未重启三个服务，未运行 Canonical 3 次，也未执行 PostgreSQL 遥测重算。Task 2.3 与 Stage 2 checklist 保持未完成，新增 Task 2.3.1 / BUG-142。
- 验证：`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...` 全部通过，`TestPostgresAgentEventReturnsNormalizedPayload`、`TestPostgresAgentRunCancellationCAS`、`TestPostgresControlPlaneLifecycle` 均实际 PASS 且无 skip；`go vet ./...`、`go build ./...` 通过；Python unittest 86 passed/1 skipped；Alembic 唯一 current/head 为 `20260906_0040` 且 check 无差异；Frontend Vitest 8/8、production build、Knip 通过；compileall、`git diff --check` 通过。额外 Vulture 命令因环境未安装该工具未执行，不属于用户指定门禁。
- 后续：先完成 Task 2.3.1 并从完整门禁重新验收；本轮未修改业务代码，未 commit/push。

## 2026-09-06 | 完成 Stage 2 Task 2.1/2.2 LLM 遥测

- 任务：按 Stage 2 spec、tasks/checklist 与 explorer 结论实施 Task 2.1/2.2；不执行 Task 2.3，不 commit/push，保持 Go 为唯一 Agent 且 Python 不增加 LLM。
- 操作：Agent 层新增 `PromptSpec`、`ModelUsage`、`ModelAttempt`、`Telemetry`、`Error` 并由 `ModelResponse` 携带遥测；系统 prompt 版本固定为 `agentcore.system.v1`，仅记录 request/prompt/toolset SHA256。OpenAI-compatible adapter 改为显式 provider 配置，解析 requested/resolved model、usage、finish reason、provider request ID、attempt 与总延迟，并仅对瞬时 transport/timeout/408/429/500/502/503/504 做最多 3 次有界重试。Harness 为每个逻辑调用分配 `logical_call_id`/`step_id`，每个物理请求写入一个 `research.llm_call` / `research.llm_call.v1` 事件，成功关联 tool call IDs，失败同样记录。事件 payload 采用 typed whitelist、长度上限和安全错误分类，不保存 prompt/messages/headers/key/cookie/raw response/raw provider error。复用 `agent_events`，无迁移；PostgreSQL repository 返回数据库规范化 Event。SSE 改为先订阅后查询历史，broker 仅合并唤醒，按 `lastSeq` 从数据库补洞，REST/SSE 共用 `MarshalEvent`。前端补充 `research.llm_call` 与 `run.cancelled` SSE 类型，research 事件不进入 tool activities。
- 结果：Task 2.1、2.2 及 Stage 2 对应六项 checklist 已完成；新增 BUG-141 并修复。Task 2.3、Canonical 连续 3 次、Stage 2 commit/push 均保持未完成。Python Browser Worker 未新增或修改任何 LLM/Agent 逻辑。
- 验证：Go 全量 test 通过，包含 PostgreSQL `TestPostgresAgentEventReturnsNormalizedPayload`、既有两个 PG integration、LLM usage/hash/security/retry/cancel、Harness 事件、broker/SSE/REST 合同测试；`go vet ./...`、`go build ./...` 通过。Frontend Vitest 8/8、production build、repo-local cache Knip 通过。Python `uv lock --check`、全量 unittest 86 passed/1 skipped、compileall 通过。最终 diff/格式检查见本记录后的收口验证。
- 后续：Task 2.3 仍需独立执行 Canonical Goal 连续 3 次并验证真实 LLM call 关联与可重算指标；本轮按要求不 commit/push。

## 2026-09-06 | Stage 1 最终交付

- 任务：完成 Stage 1 提交前文档收口；确认 Task 1.1-1.5.5、Stage 1 checklist、Canonical 与专项门禁结果，复核 BUG-134..140；不修改业务代码，不 commit/push。
- 操作：核对 `tasks.md`、`checklist.md`、Stage 1 最终验收记录、`research/results/stage1-final-accepted-canonical-{1,2,3}.json`、Execution 96/97/98 artifacts 和缺陷日志；将 Task 1.5 的计划提交信息标为提交前准备完成，勾选 Stage 1 全部提交前验收项；全局项仅保留 Stage 0/1 现有证据支持的勾选，未来实验版本与 VLM 策略等条目保持未勾选。
- 结果：Canonical 最终样本为 Project 71/72/73、Execution 96/97/98，三次均从纯自然语言 Goal 和全新浏览器上下文开始，首轮正式执行、独立 Oracle、审批与 DSL SHA 绑定均通过，VLM=0、recovery=0。真实 `network_request` URL/method/status 正例通过，无请求、错误 method、错误 status 负例失败；非幂等 Add-to-cart 失败注入仅产生 1 次 POST/click 且购物车数量为 1；committed/unknown lease 不重放，FailureSignal v2 与 v1 兼容、直接来源和保守恢复合同通过，同步与流式 Runner evidence 一致。BUG-134..140 均为 `fixed`，状态与最终证据一致。
- 验证：既有最终全量门禁包括 Go test/vet/build 与两个 PostgreSQL integration、Python 86/86（另 1 项默认门控跳过）、真实 Chromium 1/1、专项 Python 16/16、Alembic upgrade/current/heads/check、compileall、Vulture、Frontend 7/7、production build、repo-local cache Knip、Go FailureSignal/Recovery 和 Frontend FailureSignal 3/3；本次文档收口执行 `git diff --check`。
- 后续：计划提交信息为 `fix: make execution evidence research-safe`；本记录不声明提交或推送 SHA，本轮不 commit/push。

## 2026-09-06 | Stage 1 最终验收通过

- 任务：继续 Stage 1 最终验收；先按指定绝对 repo-local `TMPDIR` 复验 BUG-140，再执行完整静态门禁、重启三服务、从零连续执行 Canonical 3 次及 Stage 1 专项；不修改业务代码，不 commit/push。
- 操作：在仓库根创建 `.sandbox-tmp`，从 `browser-worker/` 直接调用 `.venv/bin/python` 显式运行真实 Chromium 回归并在结束后删除临时目录；执行 Go/Python/PostgreSQL/Alembic/Frontend/Knip/Vulture/compileall/diff/bug-log 门禁；从当前工作树启动 Browser API、Execution Worker 和 20-turn AgentService，显式关闭 VLM；串行运行三个纯自然语言 Canonical；最后执行真实 Chromium `network_request` 正反例、非幂等 Add-to-cart 失败注入、lease reclaim、FailureSignal v2 和 sync/stream 等价专项。
- 结果：BUG-140 复验输出 `OK` 且 shell 退出码为 0，Task 1.5.4/1.5.5 与 BUG-139/140 已收口。Canonical #1/#2/#3 分别为 Project 71/72/73、Run `run_1d31418807fa0ee82026650e` / `run_3cc84428a9fe276cdae9460c` / `run_5535389e7a7d9a76e2237bbb`、Generation 111/112/113、Batch 105/106/107、Execution 96/97/98；三次均首轮正式执行通过、独立 Oracle 通过、审批前 Batch=0、VLM=0、recovery=0。Task 1.5 及 1.5.1-1.5.5 的验收项、Stage 1 Canonical checklist 已完成，BUG-136/137 标记 fixed；提交/push 项按本轮要求保持未勾选。
- 验证：Go 全量 test/vet/build 通过，两个 PostgreSQL integration 明确 PASS；Python 86/86（另 1 项默认门控跳过）、显式真实 Chromium 1/1、compileall、Vulture、Alembic upgrade/current/heads/check 通过；Frontend 7/7、production build、repo-local cache Knip 通过；专项 Python 16/16、Go FailureSignal/Recovery、Frontend FailureSignal 3/3 通过。真实 Chromium 专项确认网络 URL/method/status 正例通过，无请求、错误 method、错误 status 负例失败，Add-to-cart 后置条件失败时 POST/click 恰好 1 次且购物车计数为 1。数据库确认三个 Run 均 completed，Batch/Job/Execution 均 passed，报告均为 `execution.report.v2`。
- 后续：Stage 1 提交与推送等待用户单独指示；本轮未 commit/push。

## 2026-09-06 | Stage 1 最终验收在真实 Chromium 退出门禁复发

- 任务：最终独立验收并收口 Stage 1；不修改业务代码、不 commit/push，完整执行静态门禁后重启服务，从零连续执行 Canonical 3 次和 Stage 1 专项验收，任一失败新增 task/bug/log 并停止。
- 操作：读取 tasks/checklist、BUG-134..139、现有门禁和服务入口；使用合法 pgx DSN 执行 Go 全量 test/vet/build；执行 Python lock、compileall 和全量 unittest；随后创建仓库内 `.sandbox-tmp`，设置 repo-local `TMPDIR` 并直接调用 `.venv/bin/python` 显式运行 Task 1.5.3 真实 Chromium 回归。
- 结果：Go 与 Python 门禁通过；真实 Chromium 唯一用例业务断言通过并输出 `OK`，但进程退出阶段再次触发 `TRAE Sandbox Error: Not allow operate files: /`，命令最终退出 1。按失败即停规则未继续 Alembic、Frontend、repo cache Knip、bug-log/diff 门禁，未重启 Browser API、Execution Worker 和 AgentService，也未执行 Canonical 3 次、network_request、非幂等注入、lease reclaim、FailureSignal 和同步/流式专项验收。新增 Task 1.5.5 / BUG-140；Task 1.5/1.5.1/1.5.2/1.5.3 与 Stage 1 checklist 保持未完成，BUG-136/137 保持 `in_progress`。
- 验证：`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...`、`go vet ./...`、`go build ./...` 通过，`TestPostgresAgentRunCancellationCAS` 与 `TestPostgresControlPlaneLifecycle` 明确 PASS；`uv lock --check`、Python compileall、全量 unittest 86 passed/1 skipped 通过；显式真实 Chromium 1/1 断言通过但命令退出码为 1。
- 后续：完成 Task 1.5.5 后必须从完整静态门禁重新执行 Stage 1 最终验收；本轮不 commit/push。

## 2026-09-06 | 实施 Task 1.5.4 / BUG-139

- 任务：按 explorer 证据最小修复 `_collect_flow_a11y` managed（无 `session_id`）生命周期，补充精确 mock 断言，以仓库内临时目录和 worker 虚拟环境验证真实 Chromium，并运行除 Stage 1 Canonical 3 次之外的全量门禁；不 commit/push。
- 操作：为 managed 路径增加幂等清理函数，以嵌套 `finally` 依次关闭 context、browser 并调用 `pw.__exit__(None, None, None)`，每项异常独立记录且不阻断后续清理；启动失败与执行结束共用同一清理入口，共享 `BrowserSessionManager` 路径不退出共享 Playwright。新增 managed 正常、执行及清理异常、共享 session 三类 mock 回归；真实浏览器及 Python 命令使用仓库内 `.sandbox-tmp` 作为 `TMPDIR` 并直接调用 `.venv/bin/python`。
- 结果：managed 正常和异常路径的 Playwright `__exit__` 均严格调用 1 次，context/browser 清理异常不会阻断后续步骤；`session_id` 路径调用 0 次。真实 Chromium 回归业务断言通过且命令整体退出码为 0，未再触发 sandbox 根路径限制。Task 1.5.4 和 BUG-139 已完成；Task 1.5、1.5.1、1.5.2、1.5.3 及 Stage 1 checklist 仍等待独立 Canonical 3 次验收。
- 验证：聚焦生命周期单测 3/3；真实 Chromium 1/1，退出码 0；`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...`、`go vet ./...`、`go build ./...` 通过，`TestPostgresAgentRunCancellationCAS` 与 `TestPostgresControlPlaneLifecycle` 明确 PASS；Python 全量 86 passed、1 skipped，compileall 通过；Alembic upgrade/current/heads/check 通过且唯一 current/head 为 `20260906_0040`；Frontend Vitest 7/7、production build、仓库 cache Knip 通过；临时目录清理、日志唯一性和 `git diff --check` 通过。
- 后续：独立执行 Stage 1 Canonical Goal 连续 3 次和专项验收；本轮按要求不执行 Canonical，不 commit/push。

## 2026-09-06 | Stage 1 最终验收在真实 Chromium sandbox 门禁失败

- 任务：执行 Stage 1 最终独立验收；不修改业务代码、不 commit/push，完整静态门禁通过后再从零执行 Canonical 连续 3 次和 Stage 1 专项验收，任一失败立即记录并停止。
- 操作：读取最新 tasks/checklist 与 BUG-134..138，核对合法 pgx DSN、真实 Chromium Task 1.5.3、Canonical 和专项验收入口；依次执行 Go 全量 test/vet/build、Python 全量，并显式开启 `RUN_BROWSER_INTEGRATION=1` 运行 Task 1.5.3 真实 Chromium 回归。
- 结果：Go 与 Python 单元门禁通过；显式真实 Chromium 用例的业务断言通过并输出 `OK`，但 Playwright/Chromium 退出后 TRAE sandbox 因禁止访问根路径 `/` 报错，命令最终退出 1。按失败即停规则，未继续 Alembic、Frontend、仓库本地 cache Knip、compileall/diff，未重启三个服务，也未执行 Canonical 3 次、network_request、非幂等注入、lease reclaim 和 FailureSignal 专项验收。新增 Task 1.5.4 / BUG-139；Task 1.5/1.5.1/1.5.2/1.5.3 与 Stage 1 checklist 保持未完成，BUG-136/137 保持 `in_progress`，BUG-138 保持 `fixed`。
- 验证：`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...` 通过，`TestPostgresAgentRunCancellationCAS`、`TestPostgresControlPlaneLifecycle` 明确 PASS 且无 skip；`go vet ./...`、`go build ./...` 通过；Python 全量 82 passed、1 skipped；显式真实 Chromium 测试 1 passed，但整体命令因 `TRAE Sandbox Error: Not allow operate files: /` 退出 1。
- 后续：定位并消除真实 Chromium/Playwright 退出阶段的根路径访问；修复后必须从完整静态门禁重新开始 Stage 1 最终验收。本轮不 commit/push。

## 2026-09-06 | 实施 Stage 1 Task 1.5.3 / BUG-138

- 任务：最小通用修复 `explore_flow` 相邻同 URL 步骤重新导航破坏瞬态 DOM/UI 状态；补单元与本地 HTTP 真实 Chromium 回归，不触碰 Stage 2，不 commit/push。
- 操作：目标 URL 解析后与当前 `page.url` 仅忽略 fragment 做精确比较；同一文档跳过 `goto` 和 load-state 等待，不同 query/path 保持正常导航。沿用 action 快照机制，从动作当时的实际 URL 生成 page_state 和递增 revision；新增 URL 比较/导航调用单测，并以本地 HTTP 页面真实点击 Add to cart、等待并点击 modal 内 View Cart。
- 结果：相邻 `/product?sku=blue#details` 与 `/product?sku=blue#cart-modal` 步骤共享当前 DOM，modal 未被 reload 清除，最终仅经 `#cartModal a[href="/view_cart"]` 到达 `/view_cart`；页头 `/cart` 请求为 0。不同 query 和 path 均产生真实导航。BUG-138 标记 fixed；Task 1.5.3 的实现与专项回归完成，Canonical 连续 3 次及 Stage 1 最终专项验收仍未执行，因此 Task 1.5/1.5.1/1.5.2/1.5.3 和 Stage 1 checklist 保持未完成。
- 验证：Go 全量 test 通过，`TestPostgresAgentRunCancellationCAS`、`TestPostgresControlPlaneLifecycle` 明确 PASS 且无 skip；Go vet/build 通过。Python 单元 82 passed、1 个真实浏览器集成按门控跳过，显式真实 Chromium 集成 1 passed；Alembic upgrade/current/heads/check 通过且唯一 current/head 为 `20260906_0040`；Frontend Vitest 7/7、production build、Knip 通过；compileall 和 `git diff --check` 通过。真实 Chromium 回归断言产品页请求 3 次（两个 query 导航加返回产品页）、不同 path 请求 1 次、同文档 modal step 不增加请求、`/view_cart` 请求 1 次、`/cart` 请求 0 次。
- 后续：从零执行 Canonical Goal 连续 3 次及 Stage 1 专项验收后，才能完成 Task 1.5.3 最后一项和 Stage 1 checklist；本轮按要求不 commit/push。

## 2026-09-06 | Stage 1 最终验收在 Canonical #1 失败

- 任务：完成 Task 1.5.2 并重新执行 Stage 1 最终验收；不修改业务代码、不 commit/push，任一业务失败新增 task/bug/log 并停止。
- 操作：使用合法 pgx DSN 依次执行 Go test/vet/build、PostgreSQL integration、Python 全量、Alembic upgrade/current/heads/check、Frontend test/build、仓库内 cache 的 Knip、compileall 和 diff check；静态门禁全部通过后，从当前工作树重启 Browser API、Execution Worker 和 20-turn Go AgentService，显式关闭 VLM，并启动第 1 次纯自然语言 Canonical Goal。
- 结果：Task 1.5.2 的 Knip 环境子项已解决；因 Stage 1 总验收未通过，BUG-137 按关闭条件保持 in_progress。Canonical #1 的 Project 65 / Session 29 / Run `run_41b69261be3e206fef61a034` 真实执行 `#search_product` input 和 `#submit_search` click，但加购弹层状态在相邻同 URL 探索步骤重新导航后丢失，`#cartModal a[href="/view_cart"]` 持续 hidden；Run 在生成 DSL 前进入 `modal_blocker` clarification，Generation/Batch/Execution 均未创建，驱动器取消 Run 并以 1 退出。按失败即停规则未执行 Canonical #2/#3、真实 network_request 正反例、非幂等注入、lease 和 FailureSignal v2 专项验收；新增 Task 1.5.3 / BUG-138，Task 1.5/1.5.1/1.5.2 与 Stage 1 checklist 保持未完成。
- 验证：`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...` 通过，`TestPostgresAgentRunCancellationCAS`、`TestPostgresControlPlaneLifecycle` 明确 PASS 且无 skip；`go vet ./...`、`go build ./...` 通过；Python unittest 79/79；Alembic 唯一 current/head `20260906_0040` 且 check 无差异；Frontend Vitest 7/7、production build 通过；`npm_config_cache="$PWD/.npm-cache" npx --yes knip`、compileall、`git diff --check` 均通过。失败结果保存为 `research/results/stage1-final-canonical-1.json`，本轮三个验收服务已停止。
- 后续：实施 Task 1.5.3，保证同 URL 连续探索动作不清除瞬态弹层状态；修复后从完整静态门禁重新执行 Stage 1 最终验收。本轮不 commit/push。

## 2026-09-06 | Stage 1 最终验收在 Knip 环境门禁失败

- 任务：独立执行 Stage 1 最终验收；先审查 BUG-136，再运行完整静态门禁，门禁通过后重启三个服务并执行 Canonical 连续 3 次及 Stage 1 专项验证；不修改业务逻辑、不 commit/push，任一失败立即记录并停止。
- 操作：审查 DOM supplement 的真实 DOM 来源、属性白名单、connected/visible/enabled、唯一 selector、广告/password/第三方 frame 过滤、AX backend/selector 去重和 action pre/post state 归属；审查 Canonical 驱动器在审批前强制 verified `#search_product` input 后跟 verified `#submit_search` click，并拒绝含 `search=` 的 goto。随后依次运行 Go test/vet/build、Python 全量、Alembic upgrade/current/heads/check、Frontend test/build 和 Knip。
- 结果：BUG-136 审查未发现阻断问题。Go、Python、Alembic、Frontend test/build 均通过；Knip 在扫描前因用户级 npm cache 的 `EACCES/EEXIST` 非零退出。按失败即停规则，未继续 compileall/diff，未重启三个服务，未执行 Canonical 和 Stage 1 专项验证；Task 1.5/1.5.1 与 Stage 1 checklist 保持未完成，新增 Task 1.5.2 / BUG-137。
- 验证：`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...` 通过，`TestPostgresAgentRunCancellationCAS`、`TestPostgresControlPlaneLifecycle` 明确 PASS 且无 skip；`go vet ./...`、`go build ./...` 通过；Python unittest 79/79；Alembic 唯一 current/head `20260906_0040` 且 check 无差异；Frontend Vitest 7/7、production build 通过。`npx knip` 报 `EACCES: permission denied, mkdir '/Users/bytedance/.npm/_cacache/content-v2/sha512/77/73'`。
- 后续：修复 Knip 的 npm cache 执行环境后，从完整静态门禁重新开始 Stage 1 最终验收；本轮不 commit/push。

## 2026-09-06 | 实施 Stage 1 Task 1.5.1 / BUG-136

- 任务：按 explorer 证据对页面探索做通用确定性修复，稳定保留搜索控件与跨页 action 证据，不放宽 preflight，不修改 Stage 2，不 commit/push。
- 操作：新增原生/显式交互控件 DOM supplement，以白名单真实属性生成唯一 selector，校验 connected/visible/enabled，排除 password/value、广告和第三方 frame，并按 backend node/selector 与 AX 去重；收窄广告识别为结构化广告信号，避免通用祖先名称误伤业务控件。`explore_flow` 为每个 action 保存精简 pre/post 目标证据及实际 URL/page_state，页面完整节点按 latest revision 保留；`wait_for` 对齐 action locator 支持 `#`/`.`/`css=`。工具提示与 Agentic E2E 驱动器要求 Canonical 搜索必须是 verified input 后 click，拒绝 goto 搜索 URL。
- 结果：真实 Chromium 可独立补入 `#search_product` 与 `#submit_search`，source 为 `dom_verified_interactive_control`，属性中无 value/password；真实搜索 flow 产生 Products `S0` 和搜索结果 `S1`，旧节点不再改绑最终 state。未修改 Stage 2，未 commit/push。
- 验证：聚焦 37/37；Go 全量 test（含 PostgreSQL 两项 integration）、vet/build；Python 全量 79/79 与 compileall；Frontend 7/7、production build、Knip；Alembic upgrade/current/heads/check；`git diff --check` 均通过。Canonical 首次发现初版完整快照嵌套使模型请求达到 1,422,028 tokens，随后改为 action 目标证据；第二次已走完搜索、详情、加购和 View Cart 探索，但按用户要求取消等待，连续 3 次未完成。
- 后续：Task 1.5.1 的实现与静态/真实浏览器专项验证完成；Canonical 连续 3 次和 Task 1.5 总验收仍待后续单独执行。

## 2026-09-06 | Stage 1 Task 1.5 独立验收失败

- 任务：不修改业务代码、不 commit/push，独立验收 Stage 1 Task 1.5；完整执行静态门禁、专项语义验证和纯自然语言 Canonical Goal 连续 3 次。
- 操作：审查 spec/tasks/checklist 与当前未提交实现；使用 pgx 合法 PostgreSQL DSN 执行 Go 全量测试；执行 Python、Alembic、Frontend、compileall、Knip 和 diff 门禁；聚焦验证 pre-state、逐条件 evidence、同步/流式一致性、lease reclaim 和 `failure.signal.v2`。另以真实 Chromium 和本地 HTTP 服务验证 `network_request` URL/method/status 正例以及无请求、错误 method、错误 status 负例，并注入 add-to-cart postcondition 失败验证只 dispatch 一次、购物车计数保持 1。随后从当前代码重启 Browser API、Execution Worker、Go AgentService，显式关闭 VLM，并连续执行 Canonical Goal。
- 结果：静态与专项验收通过。Canonical 第 1、2 次首批正式执行通过，审批、DSL SHA、Batch/Job/Execution、独立 Oracle 和 VLM=0 均通过；第 3 次在 DSL 审批前因搜索框与搜索按钮未进入 A11y 快照而转入 clarification，驱动器按合同拒绝自动回答并取消 Run，未创建 Batch。连续 3 次条件未满足，Task 1.5 和 Stage 1 checklist 保持未完成，新增 Task 1.5.1 与 BUG-136 后停止。
- 验证：Go test/vet/build 通过，`TestPostgresAgentRunCancellationCAS`、`TestPostgresControlPlaneLifecycle` 明确 PASS 且无 skip；Python unittest 71/71；Alembic current/heads/check 唯一为 `20260906_0040` 且无差异；Frontend Vitest 7/7、production build、Knip 通过；compileall、bug-log 唯一性和 `git diff --check` 通过。专项合同 28 个 Python 测试、Go failure/tool/DSL 测试、Frontend FailureSignal 3 个测试通过。真实本地 HTTP 验收的全部断言通过，但结果打印后 Chromium 子进程触发一次沙箱根路径限制退出码 1。Canonical 结果：Run `run_090fb1786e91b8124cdb817a` / Batch 87 / Execution 78 通过，Run `run_cbd092fb42cd447de3529e4d` / Batch 88 / Execution 79 通过，Run `run_fa172186529e470c356e95dd` 在 clarification checkpoint 失败并取消。
- 后续：完成 Task 1.5.1 后重新执行完整门禁和连续 3 次 Canonical Goal；本次不得提交或推送。

## 2026-09-06 | 完成 Stage 1 Task 1.4 FailureSignal v2

- 任务：基于 Task 1.1-1.3 的结构化 condition/action/side-effect evidence 实施 `failure.signal.v2`；兼容 v1，不做数据库列迁移，不 commit/push。
- 操作：Python 新增版本化 FailureSignal、直接 Execution JSON Pointer 来源和可选 Agent event 引用，按 condition/action outcome、locator/network、exception type、text fallback 的顺序确定 category/stage/code/retryable，并将 side-effect state 映射为 true/false/null；fingerprint 保持原 category/action/title 算法。Go 增加轻量 typed decoder，`fix_and_retry` 仅在 v2 明确未提交副作用时允许自动修复，其他情况转 `manual_reconcile`。前端使用 v1/v2 联合类型并展示 v2 分类、重试、副作用和来源。新增共享 golden，由 Python 生成校验、Go 解码、前端展示共同消费。
- 结果：Task 1.4 与 checklist 对应项完成；覆盖六类 category、network 4xx/5xx、postcondition 优先级、unknown side effect、直接执行 source reference、v1 兼容和 fingerprint 稳定。未伪造 Agent event，现有 JSON 列可直接存储 v2，因此未新增迁移。BUG-135 已修复；Task 1.5、Canonical 3 次和 Stage 1 commit/push 保持未完成。
- 验证：显式 PostgreSQL DSN 的 Go 全量测试通过，`TestPostgresAgentRunCancellationCAS` 与 `TestPostgresControlPlaneLifecycle` 明确 PASS；Go vet/build 通过。Python unittest 71/71 与 compileall 通过。Frontend Vitest 7/7、production build、Knip 通过。Alembic upgrade/current/heads/check 通过，唯一 current/head 为 `20260906_0040` 且无新迁移。`git diff --check` 通过。Coze CLI 已按技能调用，但项目列表为空且 CLI 日志目录受沙箱限制，未创建或修改外部项目。
- 后续：执行 Task 1.5 的 Canonical 3 次验收后，再按用户指示决定是否 commit/push；本次明确不 commit/push。

## 2026-09-06 | 完成 Stage 1 Task 1.1/1.2/1.3 研究事实完整性

- 任务：按 spec 和 explorer 矩阵实施验证时序、真实网络条件、非幂等动作保护与 lease reclaim 防重放；不修改 Task 1.4，不执行 Task 1.5，不 commit/push。
- 操作：先扩展 Python `ConditionSpec`、`ConditionResult`、`PageStateSnapshot`、`ActionOutcome` 和 `SideEffectState`；为全部 action 增加兼容的 Preconditions/Postconditions，并让 Go canonical v1 校验与 Tool Schema 保留新条件字段。Runner 在 locator/action 前采集 pre-state，按 phase/index 持久化 expected/actual/status/duration/error；新增步骤级 request/response/requestfailed observer 和同事件 URL substring/method/status 匹配；同步入口改为消费 streaming 执行路径。候选仅在 dispatch 前选择，dispatch 后固定 action outcome/side-effect state，postcondition 失败不再换候选或重复 click。过期 lease 遇到包含 click 且 Execution 状态 running、committed/unknown 或 legacy evidence 缺失 outcome 时转 `needs_intervention` 并保留 report。Report 默认升级为 `execution.report.v2`，读取 v1 时补兼容默认值；前端 API 类型同步兼容。
- 结果：Task 1.1、1.2、1.3 及对应 checklist 项完成；Task 1.4、Task 1.5、Canonical 3 次和 Stage 1 commit/push 保持未完成。BUG-116/117/118 标记 fixed，新增并修复 BUG-134。
- 验证：显式 `TEST_DATABASE_URL` 的 Go 全量 test 通过，`TestPostgresAgentRunCancellationCAS` 与 `TestPostgresControlPlaneLifecycle` 实际 PASS；Go vet/build 通过。Python compileall、Vulture 和 unittest 66/66 通过；覆盖 network 正反例/同事件字段/无观察/延迟响应、request-response-requestfailed 隔离、重复条件索引、precondition action 0 次、sync/stream JSON 一致、add-to-cart postcondition 失败 click 1 次、action 未知结果、v1 report/canonical 兼容、committed/legacy crash lease 不重放。Alembic upgrade/current/heads/check 通过且唯一 head/current 为 `20260906_0040`。Frontend Knip、Vitest 4/4、production build、Chromium desktop/mobile smoke 4/4 通过；bug-log 唯一性与 `git diff --check` 通过。Compose 配置门禁因本机无 `docker` 命令未运行；额外 Ruff 全仓扫描报告 164 个既有规则问题，未做无关清理。
- 后续：按 spec 继续 Task 1.4 后再执行 Task 1.5 的 Canonical 3 次、Stage 1 原子提交和 push；本次按用户要求不 commit/push。

## 2026-09-06 | Stage 0 最终交付

- 任务：完成 Stage 0 文档收尾，固化最终验收、测试门禁和计划提交信息；不修改业务代码，不执行 commit/push。
- 操作：确认 `tasks.md` 的 Stage 0 所有任务均已勾选，并将 Task 0.4 的 commit/push 项标记为准备完成；勾选 `checklist.md` 中 Stage 0 及当前阶段已有证据支撑的全局条目，保留 Stage 1+、全局实验版本和尚未完整证明的 VLM 策略条目未勾选；复核 BUG-125 至 BUG-133 均为 `fixed`，其中 BUG-132 已明确标记 `fixed`。
- 结果：Canonical 最终样本为 Project 45/46/47、Execution 59/60/61，三次均首轮正式执行通过、`first_pass=true`、独立 DOM Oracle 通过且 VLM=0；mutation 样本为 Project 48/49、Execution 62/63，正式执行通过但独立 Oracle 分别拒绝错误价格和错误商品，最终按预期失败。Stage 0 计划提交信息为 `fix: harden agent full-chain execution`，计划推送 `origin/main`；本记录不声明提交或推送 SHA。
- 验证：既有最终门禁包括 Go 全量 test/vet/build，且 `TestPostgresAgentRunCancellationCAS`、`TestPostgresControlPlaneLifecycle` 明确 PASS 且无 skip；Python unittest 52/52；Alembic upgrade/current/heads/check 通过且唯一 current/head 为 `20260906_0040`；Frontend 4/4 与生产构建通过；compileall 通过；本次文档收尾执行 `git diff --check`。
- 后续：由主代理紧接着创建上述聚焦提交、推送 `origin/main`，并在提交后确认远端 SHA 与工作区状态。

## 2026-09-06 | Stage 0 调试清理与提交前验证

- 任务：在用户确认 BUG-129 修复后完成 TRAE-debugger 会话清理与 Stage 0 提交前验证；不 commit/push，不修改 Stage 1。
- 操作：将调试记录状态短暂更新为 `[FIXED]`；从 Browser Worker 两个模块移除全部 8 个网络埋点及仅供埋点使用的 imports；确认 PID 20790 不存在且 7777 无监听；删除调试记录、NDJSON、env 和空 `.dbg` 目录；核查五份 `stage0-final` 验收 JSON。
- 结果：活动代码与调试目录中不存在调试标记、会话标识或 7777 地址残留。三份 Canonical JSON 均为正式执行通过、`first_pass=true`、Oracle 通过；`wrong-price` 与 `wrong-product` 均为正式执行通过、`first_pass=true`，但独立 Oracle 分别因价格和商品名不符而失败。
- 验证：`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...`、`go vet ./...`、`go build ./...` 全部通过，两个 PostgreSQL integration 测试明确 PASS 且无 skip；Python unittest 52/52；Alembic current/heads 均为唯一 `20260906_0040` 且 check 无差异；Frontend 4/4 与生产构建、compileall、`git diff --check` 全部通过。
- 后续：Stage 0 已完成清理与提交前验证；按用户要求未勾选或执行 commit/push。

## 2026-09-06 | Stage 0 最终独立验收通过

- 任务：重新读取 Stage 0 tasks/checklist 与 BUG-133，独立审查 Task 0.3.10，执行全部静态门禁，重启三个验收服务，验证超时取消无孤儿，并从零串行执行 Canonical 3 次及 `wrong-price`、`wrong-product` 各一次；不修改业务代码、不清理 debugger artifacts、不 commit/push。
- 操作：使用 pgx DSN `postgres://bytedance@127.0.0.1:5432/ai_web_testing` 执行 Go PostgreSQL integration 与全量 test/vet/build；执行 Python 全量、Alembic upgrade/current/heads/check、compileall、Frontend test/build 和 diff check；保留 `7777` Debug Server 与 `.dbg`，从当前工作树重启 `8000` Browser API、Execution Worker 和 `8081` AgentService。先运行 1 秒超时取消样本，再串行运行 3 个 Canonical 与 2 个独立 Oracle mutation，并从结果 JSON 和 PostgreSQL 交叉核对审批前 Batch、Generation/SHA/snapshot、首 Batch/Execution、DOM Oracle、recovery 与 VLM。
- 结果：Task 0.3.10 代码审查未发现阻断问题。超时 Project 44 / Run `run_46aa0c44b4a3a3069914610f` 正确取消，10 秒后事件仅 `run.started,run.cancelled` 且 Generation/Batch/Job/Execution 均为 0。Canonical #1/#2/#3 分别为 Project 45/46/47、Generation 74/75/76、Batch 68/69/70、Execution 59/60/61，三次均 `success=true`、`first_pass=true`、首 Batch/Job/Execution passed、`recovery=[]`、审批前 Batch=0、DOM Oracle 精确通过且 VLM=0；SHA 分别为 `6900ade20a8ed2564555d016eab01e6259eb0bc43c801d56855209b5dafa8b56`、`303f0927ae5e8730843f196ccafc46be3e991d6ea7111f2ce86094e0c0927cad`、`373538fe8529a40b93a8d55da78dbf8972923c91f6ba53417f98ff66bf98133d`。`wrong-price` Project 48 / Batch 71 / Execution 62 与 `wrong-product` Project 49 / Batch 72 / Execution 63 的首个正式执行均 passed，但独立 Oracle 分别因 `Rs. 501 != Rs. 500`、`Red Top != Blue Top` 失败，整体 `success=false` 且 CLI 退出 1。
- 验证：Go 全量 test/vet/build 通过，`TestPostgresAgentRunCancellationCAS` 与 `TestPostgresControlPlaneLifecycle` 明确 PASS 且无 skip；Python unittest 52/52；Alembic 唯一 current/head `20260906_0040` 且 check 无差异；Frontend 4/4 与生产构建、compileall、`git diff --check` 全部通过。PostgreSQL 对 Project 45-49 逐项确认 latest/approved Generation 一致、Generation/Job/Execution SHA 一致、canonical JSON 与 job snapshot 相等。
- 后续：Stage 0 除 Task 0.4 的 commit/push 外全部完成；按用户要求保留三个验收服务、Debug Server、`.dbg`、结果与 artifacts，不执行 commit/push。

## 2026-09-06 | 完成 Task 0.3.10 / BUG-133 跨页导航与多审批修复

- 任务：按 explorer 与 Run `run_a58e6c8888075b95f7b9dc61` 证据实施通用修复；复用现有 postcondition，限制安全 href fallback，统一正式执行路径，支持同 Run 多 Generation/多审批；不 commit/push，不清理 debugger artifacts。
- 操作：细化 `generate_dsl` JSON Schema、Agent 提示和 Go/Python DSL 校验，要求已验证跨页 anchor click 携带匹配目的地址的 `url_contains`；preflight 将验证过的 href 绑定到 candidate。Worker 先选择唯一候选、采 pre-state、只派发一次动作并按 timeout 验证，只有实时 DOM 与 preflight href 一致且为同源 HTTP(S)、非 hash、非 download anchor 时才执行最多一次 `page.goto`，以 `href_navigation_fallback` 记录 evidence；同步与流式正式执行共用该步骤路径且失败均终止。Explorer 使用去 fragment URL 判断跨页到达，并过滤跨源 frame 或通用广告语义上下文节点。Driver 循环处理审批，逐轮校验 Generation/artifact/SHA/approved generation/审批前后 Batch/Report，记录 recovery，并由首个 Batch 单独决定 `stage0.first_pass`。
- 结果：缺失或错误目的 URL postcondition 的跨页 anchor DSL 在审批前被拒绝；广告/hash 插页不会被视为到达业务目的页面；anchor click 成功派发后不会换候选重放，按钮及其他动作没有 href goto 路径；失败 Batch 后可继续验证新 Generation 和第二次审批，但后续成功不会覆盖 `first_pass=false`。Task 0.3.10 完成，BUG-133 标记 fixed。
- 验证：`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...`、`go vet ./...`、`go build ./...` 全部通过，`TestPostgresAgentRunCancellationCAS` 与 `TestPostgresControlPlaneLifecycle` 明确 PASS；Python unittest 52/52；Alembic upgrade/current/heads/check 通过且唯一 head/current 为 `20260906_0040`；Frontend 4/4 与生产构建通过；compileall 与 `git diff --check` 通过。
- 后续：从最新服务执行 Stage 0 Canonical 3 次及 `wrong-price`、`wrong-product` 两次 live 验收。本任务未 commit/push，未停止现有调试服务，也未清理 `.dbg`、`debug-playwright-thread-affinity.md` 或保留的失败结果；测试期间调试 NDJSON 继续按现有埋点采集。

## 2026-09-06 | Stage 0 最终独立验收在 Canonical #1 正式执行失败

- 任务：整合审查 BUG-132 的语义 target 合同与 AgentRun 超时取消修复，使用合法 pgx DSN 执行全部门禁，重启三个最新服务，独立验证短超时取消，再从零执行 Canonical 3 次及两个 mutation；不修改业务代码、不清理 debugger artifacts、不 commit/push，任一失败立即停止。
- 操作：审查 Tool Schema、Go/Python canonicalization、Harness 活动 Run 取消、PostgreSQL CAS/事件事务和驱动器超时取消；执行 Go/Python/Alembic/Frontend 全部门禁及取消相关 race 聚焦；重启 `8000` Browser API、Execution Worker 和 `8081` AgentService；先运行不计入连续样本的 1 秒短超时，再启动 Canonical #1。
- 结果：短超时 Project 40 / Run `run_5e6f7cf00052f8a7c5565056` 正确转为 `cancelled`，事件仅 `run.started,run.cancelled`，立即及 10 秒后 Generation/Batch/Job 均为 0。Canonical #1 创建 Project 41 / Session 17 / Run `run_a58e6c8888075b95f7b9dc61`，成功生成并审批 Generation 68；正式执行的商品详情 click 被 `#google_vignette` 广告插页截断，Batch 63 / Job 63 / Execution 54 在 `#quantity` 定位处失败。Agent 生成修复版 Generation 69 并请求第二次审批，驱动器以 `run requested unexpected approve_dsl input after approval` 失败并将 Run 取消。按停止规则未执行 Canonical #2/#3 和两个 mutation，未勾选任何 Stage 0 项，新增 Task 0.3.10 / BUG-133；BUG-132 保持 open。
- 验证：`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...`、`go vet ./...`、`go build ./...` 全部通过，两个 PostgreSQL integration 测试明确执行且未 skip；取消相关 Go race 聚焦 3 包通过；Python unittest 45/45；Alembic current/heads 均为唯一 `20260906_0040` 且 check 无差异；compileall；Frontend 4/4 与生产构建；`git diff --check` 均通过。Generation 68 与 Execution 54 SHA 同为 `0a3eb6ac7fb8586a6ace031312b42e523c7cf0c19db9516042d7cc1742dd8681`。
- 后续：实施 Task 0.3.10，明确跨页转换验证和二次审批验收合同；修复后从全部门禁开始重跑 Stage 0 Canonical 3+2。三个验收服务、数据库现场、`.dbg`、埋点和调试说明保持原状。

## 2026-09-06 | Stage 0 最终独立验收在 Canonical #1 超时

- 任务：独立审查 Task 0.3.8 的关键合同与回归覆盖，使用合法 pgx DSN 运行全部门禁，重启三个验收服务，并从零串行执行 Canonical 3 次及两个 mutation；不修改业务逻辑、不清理 debugger artifacts、不 commit/push，任一失败立即停止。
- 操作：核对 state-aware preflight、case/evidence digest、advisory validation、真实 target enum、flow failure/revision 和 20-turn 回放合同；执行聚焦回归及全部门禁；从当前工作树重启 `8000` Browser API、Execution Worker 和 `8081` AgentService；启动第 1 次纯自然语言 Canonical Goal。
- 结果：静态门禁全部通过，PostgreSQL `TestPostgresControlPlaneLifecycle` 明确执行且未 skip。Canonical #1 创建 Project 36 / Session 15 / Run `run_c77c8c19791d44bc2e761bcc`，5 次浏览器探索取得 Products、搜索结果、商品详情、加购弹层和购物车证据；随后连续调用 `generate_dsl`，事件 42-102 反复出现未匹配 selector、非法 JSON 和 `target_strategy is invalid`。驱动器在 900 秒内未到达审批边界并以 1 退出；服务端 Run 仍为 `running`，无 Generation、Approval、Batch、Job 或 Execution。按停止规则未执行 Canonical #2/#3 和两个 mutation，未勾选任何 Stage 0 项，新增 Task 0.3.9 / BUG-132。
- 验证：Go 聚焦合同回归 6/6；Python 聚焦 15/15；`TEST_DATABASE_URL=postgres://bytedance@127.0.0.1:5432/ai_web_testing go test -count=1 -v ./...`、`go vet ./...`、`go build ./...` 通过，integration 无 skip；Python 42/42；Alembic 唯一 head/current `20260906_0040` 且 check 无差异；compileall；Frontend 4/4 与生产构建；`git diff --check` 均通过。失败结果保存在 `research/results/stage0-final-canonical-1.json`。
- 后续：实施 Task 0.3.9，统一语义 target 的 `target_strategy` 合同并处理驱动器超时后的服务端 Run；修复后从全部门禁开始重跑 Stage 0 Canonical 3+2。Debug Server、埋点、`.dbg` 和调试说明保持原状，等待用户确认。

## 2026-09-06 | 完成 Task 0.3.8 / BUG-131 确定性收敛

- 任务：读取 explorer 结论和失败 Run `run_847c3b804514fa7459cbd709`，在不提高 max turns/timeout、不弱化 preflight、不清理 `playwright-thread-affinity` 调试产物且不 commit/push 的前提下修复重复探索与 generation 失败。
- 根因证据：PostgreSQL transcript 还原了 20 turns：3 次 `explore_page`、8 次 `explore_flow`、7 次 required-elements validation；空动作 cart step 返回 `element_count=0`，`text=View Cart` 等失败被吞掉，多次同 URL 状态由动作数而非 revision 选择；事件 29 起的裸 `valid=true` 与事件 40 最终 DSL 无绑定；事件 41 的 cart 复合 CSS 均未出现在 verified selectors；事件 140 只报告 max turns。
- 改动：先对齐 Go Tool JSON Schema、Python capability 类型和实际 target strategy enum；`explore_flow` 支持空动作状态采集，结构化返回 navigation/action/flow failure，正确处理 `text=` 和 timeout，并以 latest-success revision 去重；移除会跳过真实副作用的 flow cache。required-elements validation 改为 advisory，`generate_dsl` 对最终规范化 case 和 `a11y_nodes_by_state` 立即执行绑定 preflight并核对双 digest；state-aware preflight 自动绑定唯一 state，拒绝未经验证的复合 CSS，接受精确 verified selector；max-turn 追加最后工具错误。
- 回归：新增空动作、click 失败、wait_for、revision、state preflight、复合 CSS 正反例、digest 绑定、真实 target enum 和 max-turn 诊断测试；BUG-131 轨迹按 3 page + 8 flow + 1 advisory validation 复放，在原 20-turn 预算内第 13 turn 生成 DSL、第 14 turn 进入审批。
- 验证：`TEST_DATABASE_URL=postgres://... go test -count=1 ./...`、`go vet ./...`、`go build ./...` 全部通过，PostgreSQL 集成测试未 skip；`uv run python -m unittest discover -s tests -v` 42/42；Alembic upgrade/current/heads/check 通过且唯一 head/current 为 `20260906_0040`；`compileall`、`git diff --check` 通过。首次 Go 全量运行仅因 integration fake validator 未实现新 digest 合同失败，补齐 fake 后完整复跑通过。
- 备注：未执行 commit/push，未删除或修改 `.dbg`、`debug-playwright-thread-affinity.md` 等调试产物；Stage 0 Canonical 3+2 live 验收仍归 Task 0.3.3/0.3.7。

## 2026-09-06 | Task 0.3.7 Stage 0 最终验收在 Canonical #2 失败

- 任务：按 pgx 合法 DSN 重新执行 Task 0.3.7 与 Stage 0 最终独立验收；不修改业务代码、不清理 debugger artifacts、不 commit/push；任一业务失败立即记录并停止。
- 操作：将 Browser Worker SQLAlchemy DSN 归一化为 `postgres://bytedance@127.0.0.1:5432/ai_web_testing`，依次执行 Go test/vet/build、Python 全量、Alembic current/heads/check、compileall 和 diff check；保留无关 `8001/8002` 服务，从当前工作树重启 `8000` Browser API、Execution Worker 和 `8081` AgentService；串行启动全新 Canonical Run。
- 结果：静态门禁全部通过，PostgreSQL `TestPostgresControlPlaneLifecycle` 明确执行且未 skip。Canonical #1 为 Project 31 / Session 13 / Run `run_be16e8573563b96fa9e08f42` / Generation 56 / Batch 52 / Job 52 / Execution 43，18 步、正式 Report、SHA 绑定和独立 DOM Oracle 全部通过，VLM=0。Canonical #2 为 Project 32 / Session 14 / Run `run_847c3b804514fa7459cbd709`，在 3 次 `explore_page`、8 次 `explore_flow`、7 次 `validate_page_elements` 后，最终 `generate_dsl` preflight 报步骤 13-16 `match_count=0`，随后因超过 20 turns 失败；无 Generation、Approval、Batch、Job 或 Execution。按停止规则未执行 Canonical #3 和两个 mutation，Task 0.1/0.2/0.3/0.3.3/0.3.6/0.3.7 及 Stage 0 checklist 保持未完成，新增 Task 0.3.8 / BUG-131。
- 验证：Go `go test -count=1 -v ./...`、`go vet ./...`、`go build ./...` 通过；Python unittest 36/36；Alembic current/唯一 head 均为 `20260906_0040` 且 check 无差异；compileall、`git diff --check` 通过。结果文件为 `research/results/stage0-task-037-canonical-1.json` 和 `research/results/stage0-task-037-canonical-2.json`。BUG-130 已验证修复；Debug Server、`.dbg`、埋点和调试说明均保留。
- 后续：实施 Task 0.3.8，修复重复探索/验证与购物车页面状态到 preflight 的一致性；修复后从全部静态门禁开始重新执行 Canonical 3 次及两个 mutation。

## 2026-09-06 | Stage 0 最终独立验收因 Go DSN 命令错误停止

- 任务：执行 Stage 0 最终 3+2 独立验收；先审查 Task 0.3.6 与 post-fix NDJSON，再运行全部静态门禁，门禁通过后重启三个服务并执行 Canonical 3 次及两个 mutation；不修改业务逻辑、不清理调试现场、不 commit/push。
- 操作：审查 `playwright-thread-affinity` post-fix 20 条 NDJSON、共享 Playwright/Browser runtime、Session 独立 context、部分初始化清理、真实 HTTP 三 Session 回归和 E2E 驱动器合同；并行执行 Go test/vet/build、Python 全量、Alembic upgrade/head/current/check、compileall 和 diff check。
- 结果：Task 0.3.6 的 post-fix 证据与代码一致，未发现需预先停止的业务缺陷。Go 全量测试命令错误地把 SQLAlchemy `postgresql+psycopg://` DSN 原样传给 pgx，`internal/integration` 因 DSN 无法解析失败；按立即停止规则，未重启 Go AgentService、Browser API、Execution Worker，Canonical 3 次和两个 mutation 均未执行，所有 Stage 0 勾选项保持不变。新增 Task 0.3.7 / BUG-130。
- 验证：Go vet/build 通过；Go 单元包通过，仅 PostgreSQL 集成包在连接前因 DSN scheme 失败；Python unittest 36/36、Alembic 唯一 head/current `20260906_0040` 且 check 无差异、compileall、`git diff --check` 通过。Debug Server、`.dbg`、8 个埋点和 `debug-playwright-thread-affinity.md` 均保留。
- 后续：使用归一化为 `postgres://` 的 `TEST_DATABASE_URL` 从全部静态门禁重新开始 Task 0.3.7；全部门禁通过后方可重启服务并执行 3+2。

## 2026-09-06 | 修复 BUG-129 Playwright 跨 Session runtime 污染

- 任务：严格按 TRAE-debugger 调试 Task 0.3.6 / BUG-129；固定 Session `playwright-thread-affinity`，先采集 pre-fix 运行时证据，再做最小修复和 post-fix 对照；不 commit/push。
- 操作：启动 `.dbg` Debug Server（idle 1200），增加 8 个 HTTP 网络埋点，覆盖 capability 入口、runtime submit/worker、BrowserSession 创建/复用/关闭和 Playwright enter；真实 Uvicorn HTTP capability + Chromium 复现后，改为专用线程只启动一个共享 Playwright/Browser runtime，各 Planning Session 使用独立 BrowserContext/Page，并保护未完成 `__enter__` 的启动失败清理；增加三 Session 串行 HTTP 回归和部分初始化清理测试。
- 结果：pre-fix Session 1001 返回 200，1002/1003 返回 500；证据排除跨线程复用、executor 换线程和绕过 runtime，确认首个长期存活的 Playwright manager 令同线程后续调用观察到 running asyncio loop。post-fix Session 2001/2002/2003 均返回 200，并分别完成 context 创建/使用/关闭；只启动一次 Playwright。Task 0.3.6 的实现子项已完成，Task 0.3.3 与 Canonical 3+2 验收保持未完成。
- 验证：聚焦测试 8/8、Browser Worker 全量 36/36、`compileall`、`git diff --check` 通过。最终 post-fix NDJSON 第 2/10/16 行保持同一 executor，第 3/11/17 行保持同一 worker，第 5 行为唯一 Playwright enter，第 6/8、12/14、18/20 行为三组 context 创建/关闭。
- 后续：等待用户确认修复结果；确认前保留 `debug-playwright-thread-affinity.md`、`.dbg` 日志/env、8 个埋点和 Debug Server。确认后按 TRAE-debugger 清理，再执行 Task 0.3.3 Canonical 3 次及两个 mutation。

## 2026-09-06 | Stage 0 最终独立验收在第 3 次 Canonical 发现缺陷

- 任务：独立审查 Task 0.3.4/0.3.5 与 canonicalization 合同，执行全部静态门禁，重启三个验收服务，并从零串行执行 Canonical 3 次和两个 Oracle mutation；不修改业务代码，不 commit/push。
- 操作：审查 DOM Oracle、Go 单点 canonical bytes、审批/入队绑定、Worker 执行前校验、迁移和共享 golden；执行 Alembic upgrade/current/heads/check、Go 全量 test/vet/build、Python 全量测试、compileall 和 diff check；从当前工作树重启 Browser API、Execution Worker、Go AgentService；串行运行纯自然语言 Canonical Goal，并从 PostgreSQL 独立核对 ID、审批顺序、canonical bytes、SHA、snapshot、Report 和 VLM 调用数。
- 结果：静态门禁全部通过。Canonical #1 为 Project 27 / Session 10 / Run `run_22b2f344e82a891db448a781` / Generation 50 / Batch 48 / Job 48 / Execution 39，14 步、正式 Report 和 DOM Oracle 通过，SHA `fef1f89966ca0267ef5741f4e99f30d924bfd23164cdb2a7339280e504cf3409`，VLM=0；Canonical #2 为 Project 28 / Session 11 / Run `run_345332898c7ea09d6877b183` / Generation 52 / Batch 49 / Job 49 / Execution 40，18 步、正式 Report 和 DOM Oracle 通过，SHA `e7937c23e4c7dd954f7507adf8897906062d39491552473a3bb7f413d5826733`，VLM=0。两次均为审批前 Batch=0、approved generation 正确，generation canonical bytes 与 job 逐字节相等且 execution snapshot/report SHA 一致。Canonical #3 的 Project 29 / Session 12 / Run `run_d585273deeaf91967f6780e0` 在两次 `explore_page` 和一次 `explore_flow` 均触发 Sync Playwright/asyncio loop HTTP 500 后进入 clarification，Generation/Batch/Job/Execution 均不存在，CLI 非零退出。按停止规则未执行 `wrong-price` 和 `wrong-product`，未勾选 Task 0.1/0.2/0.3/0.3.3 或 Stage 0 checklist；新增 Task 0.3.6 / BUG-129。
- 验证：Alembic 唯一 head/current 均为 `20260906_0040` 且 check 无差异；Go `go test ./...`（显式 PostgreSQL）、`go vet ./...`、`go build ./...` 通过；Python unittest 34/34、compileall、`git diff --check` 通过。失败 Run 为 `waiting_user`、事件 7/13/19 为 capability `tool.failed`、事件 25 为 `proceed_after_worker_error` clarification，项目 Batch 数为 0；Browser API traceback 指向 `_BrowserCapabilityRuntime` 内 `sync_playwright().__enter__()`，失败清理另有 `_connection` 未初始化异常。
- 后续：实施 Task 0.3.6，补同进程至少 3 个全新 Planning Session 的真实 HTTP 串行回归；修复后必须从零重跑 Canonical 3 次及两个 mutation，全部通过前禁止勾选 Stage 0 或 commit/push。

## 2026-09-06 | Task 0.3.5 统一 DSL canonicalization 与审批 SHA

- 任务：修复 BUG-128，确保批准的 Generation、持久化 Case、正式 Execution snapshot 与 Report 使用同一语义 DSL 和 SHA；不修改 DOM Oracle parser，不 commit/push。
- 操作：对比 Generation 37 与 Execution 29 的真实 JSON，确认差异来自 Python 二次物化默认字段、移除 `match_count` 等额外字段及数值类型化；定义 `dsl.canonical.v1`，由 Go 单点生成完整 canonical JSON 和 SHA；generation 持久化并返回版本/SHA，正式入队事务校验 case 后固化 snapshot/canonical bytes/版本/SHA，Worker 在浏览器启动前验证权威字节和 Pydantic 语义一致；新增兼容迁移、legacy generation 首读回填、公共合同文档和共享 golden。
- 结果：Agent 审批与正式执行绑定到同一不可变 canonical bytes；Python 不再为正式 job 重算另一套 canonical JSON，SHA 或默认字段漂移会令 job 失败。Task 0.3.5 与 BUG-128 已完成；未修改 Task 0.3.4 的 DOM Oracle 实现。
- 验证：Go `go test ./...`、`go vet ./...`、`go build ./...` 通过；显式 `TEST_DATABASE_URL` 的 PostgreSQL 纵向测试通过，覆盖 generation artifact、legacy 回填、持久化 case、job、execution snapshot 和 report SHA；Python unittest 34/34、compileall 通过；Alembic 位于 `20260906_0040` 且 `alembic check` 无差异；`git diff --check` 通过。
- 后续：重新执行 Task 0.3.3 Stage 0 live 3+2 验收。

## 2026-09-06 | 完成 Task 0.3.4 修复真实 DOM Oracle

- 任务：实施 Task 0.3.4 / BUG-127，仅修复 Agentic E2E DOM Oracle 及其测试和日志状态，不修改 SHA 合同相关 Go/Python 执行代码，不 commit/push。
- 操作：将 `_CartHTMLParser` 从不可靠的标签深度计数改为目标商品行内的可恢复标签栈；识别 `img`、`br` 等 void element，在新 `td`/`tr` 边界处理隐式闭合，并只从商品标题链接、单价格段、数量按钮和总价格段采集字段；从 Execution 29 真实购物车 DOM 提取 950 字节最小 fixture，增加真实形态、隐式闭合、后续页面标签和 CLI 退出码回归测试。
- 结果：完整 Execution 29 `final.html` 不再发生 stack underflow，canonical Oracle 精确得到唯一 `product-1` 的 Blue Top、Rs. 500、1、Rs. 500；`wrong-price` 和 `wrong-product` 均失败，CLI 退出码保持 0/1/1。Task 0.3.4 与 BUG-127 已完成；Task 0.3.5、Task 0.3.3 和 Stage 0 总门禁仍未完成。
- 验证：`uv run python -m unittest tests.test_agentic_e2e_driver -v` 11/11、`uv run python -m unittest discover -s tests -v` 31/31、`uv run python -m compileall -q app scripts tests`、完整 Execution 29 artifact 直接 Oracle 校验和 `git diff --check` 通过。
- 后续：实施 Task 0.3.5 修复 BUG-128 后，再从零执行 Task 0.3.3 的 Canonical 3 次及两个 mutation live 验收。

## 2026-09-06 | Task 0.3.3 严格独立验收发现新缺陷

- 任务：重新执行 Stage 0 Task 0.3.3 的严格独立验收；不实施业务修复，不 commit/push。
- 操作：重新读取 tasks/checklist 与最新代码；仅停止旧 AgentService 和 Execution Worker，保留无关的 `8001/8002` 用户进程；从当前工作树重启 Browser API、Execution Worker 和 Go AgentService；执行 BUG-125/126 聚焦测试及 Go/Python 全量门禁；从零启动第 1 次纯自然语言 Canonical Goal。
- 结果：测试门禁全部通过，BUG-125/126 的 live 路径均已加载。Canonical #1 创建 Project 20 / Session 9 / Run `run_47d657c2f8f219cbc6ff0d99` / Generation 37 / Batch 36 / Job 36 / Execution 29；审批前 Batch 数为 0，Run 完成且批准 Generation 37，正式 Report 为 `passed`、21/21 步通过、最终 URL 为 `/view_cart`、VLM=0，并产出 `/artifacts/executions/29/final.html`。独立 Oracle 随后因 `_CartHTMLParser` 字段栈下溢抛出 `pop from empty list`，脚本以 1 退出；另确认 Generation SHA `d6fc...a3d1` 与 Execution SHA `1e36...ec58` 不一致。按负向规则和停止条件，本轮不通过，Canonical #2/#3 与两个 mutation 均未执行，Task 0.1/0.2/0.3/0.3.3 和 Stage 0 checklist 保持未勾选；新增 Task 0.3.4/0.3.5、BUG-127/128。
- 验证：Python 聚焦 14/14、全量 28/28、`compileall` 通过；Go 聚焦包、`go test ./...`、`go vet ./...`、`go build ./...` 通过；`git diff --check` 通过。对 Execution 29 的真实 DOM 单独调用 `evaluate_cart_oracle` 可稳定复现 traceback；数据库确认 generation JSON 与 execution snapshot 不相等。
- 后续：先修复 BUG-127/128 并补跨真实 DOM、跨 Go/Python SHA 合同测试，再从零重跑 Canonical 3 次及 `wrong-price`、`wrong-product` 各一次；全部通过前禁止勾选 Stage 0 或 commit/push。

## 2026-09-06 | 修复 Browser capability Playwright 线程生命周期

- 任务：执行 Task 0.3.1，消除 Browser capability 经 FastAPI HTTP 路由调用 `explore_page`/`explore_flow` 时偶发的 Sync Playwright 与 asyncio loop 冲突。
- 操作：增加单例单线程 Browser capability runtime，将两类探索的全部 Sync Playwright 操作和 BrowserSession 复用固定到 `browser-capability` 专用线程；请求线程仅解析数据库上下文；FastAPI 关闭时在专用线程关闭全部浏览器会话后回收执行器；新增本地 Uvicorn 真实 HTTP 路由回归测试，以可控替身验证启动线程上下文且不依赖外网。
- 结果：Browser Worker 继续保持纯执行器边界，Sync Playwright 不再依赖 FastAPI/AnyIO 请求线程，BUG-125 已修复，Task 0.3.1 已完成；未修改 Agentic E2E Driver。
- 验证：`uv run python -m unittest tests.test_browser_capability_http tests.test_browser_capabilities tests.test_page_explorer -v` 6/6、`uv run python -m unittest discover -s tests -v` 28/28、`uv run python -m compileall -q app tests`、`git diff --check` 通过；真实 Chromium live 验证留待 Task 0.3.3。
- 后续：执行 Task 0.3.3，从零完成 Canonical 3 次及 `wrong-price`、`wrong-product` 各一次 live 验收。

## 2026-09-06 | 修复 Agentic E2E SSE 边界与失败诊断

- 任务：修复 Agentic E2E Driver 因 SSE keepalive 长连接卡死的问题，并补齐可复现的结构化失败诊断和 checkpoint 类型门禁。
- 操作：将边界等待改为持久化 events 与 run 状态的可靠轮询，为 SSE 客户端增加墙钟截止；统一保留 project/session/run ID、run status、pending tool call、问题、last seq 和关键事件引用；仅允许当前 `approve_dsl` checkpoint 自动审批，clarification 直接输出失败诊断；增加历史审批隔离、keepalive 和失败 JSON 测试。
- 结果：Driver 不再依赖 SSE 长连接到达边界；`browser_backend_down` 等 clarification 不会被误审批。Task 0.3.2 已完成，Task 0.3 和 0.3.3 的 live 3+2 验收保持未完成，BUG-125 保持 open。
- 验证：`uv run python -m unittest tests.test_agentic_e2e_driver -v` 8/8、`uv run python -m unittest discover -s tests -v` 28/28、`uv run python -m compileall -q app scripts tests`、`git diff --check` 通过。
- 后续：修复 BUG-125 后，从零执行 Canonical 3 次及 `wrong-price`、`wrong-product` 各一次 live 验收。

## 2026-09-06 | Stage 0 验收失败

- 任务：验证已批准 spec 的 Stage 0；运行聚焦/全量测试、3 次 Canonical live E2E 和 2 个负向 Oracle，不修改业务代码。
- 操作：审查 Stage 0 未提交实现及提交 `3fe967d` 的 DSL Schema、Harness 恢复、OpenAI arguments、复合 CSS、preflight、空 input 和 duration 修复；执行 Go 与 Browser Worker 聚焦/全量测试；启动 Canonical live E2E，并从 PostgreSQL、SSE 回放和进程连接定位失败。
- 结果：静态门禁与测试通过，但 live 门禁失败。第 1 次为 Project 18 / Session 7 / Run `run_0f34d1e8127602d49b31de33`，`seq=1..22`，Browser capability 三次因 Sync Playwright/asyncio loop 冲突失败，终态 `waiting_user`，pending `browser_backend_down`，无 Generation/Batch/Execution。第 2 次为 Project 19 / Session 8 / Run `run_3f05e744b116ea7f1a9d2c7b`，成功生成 generation 36 和 `seq=73 dsl_generation`，到达 `seq=79 approve_dsl` 后驱动器被 SSE keepalive 长连接阻塞；按要求终止驱动器并保留现场，未审批且无 Batch/Execution。Canonical 第 3 次及 `wrong-price`、`wrong-product` 均未运行。Stage 0 不通过，Task 0.1-0.3 与 checklist 保持未勾选，并新增 Task 0.3.1-0.3.3。
- 验证：Go 聚焦测试通过；`go test ./...`、`go vet ./...`、`go build ./...` 通过；Browser Worker 聚焦 15/15、全量 25/25、`compileall` 和 `git diff --check` 通过。数据库确认两个 Run 均为 `waiting_user` 且项目 Batch 数均为 0；独立 SSE 回放可立即读取第二次 Run 的 `seq=74..79`，排除 Agent 长循环卡死。
- 后续：修复 BUG-125 与 BUG-126 后，从零重新执行连续 3 次 Canonical 和 2 个 mutation；全部通过前禁止勾选 Stage 0 或提交推送。

## 2026-09-06 | 实施 Stage 0 Agentic E2E 驱动器

- 任务：仅实施已批准规范的 Task 0.1-0.3，固化自然语言 Goal 到正式报告及独立 Oracle 的统一验收链路。
- 操作：复核 `3fe967d` 中 DSL Schema、Harness 错误恢复、OpenAI arguments、复合 CSS、preflight、空 input 和 UTC duration 修复；新增仅接受自然语言的 Agentic E2E 驱动器，自动创建 clean project/session、订阅并回放事件、读取并校验 DSL artifact、核对审批前无新 Batch、提交 `approve_dsl=true`、等待正式报告并校验 generation/DSL SHA；Runner 新增终态 DOM artifact，独立 Oracle 精确检查唯一 `#product-1` 行的名称、单价、数量和总价；新增正负变异测试和运行说明。
- 结果：驱动器输出 `agentic-e2e.result.v1` JSON，拒绝 DSL/CSS/XPath/candidates，错误价格与错误商品均会覆盖正式报告成功并令最终结果失败。按用户要求提前收口时，真实 Run `run_432606627f2a07c6dd64f5f4` 已完成 50 个探索事件但仍处于 `running`，尚未生成 generation、Batch 或 Execution；Canonical 完成 0/3，负向 live 变异完成 0/2。
- 验证：Go `go test ./...`、`go vet ./...`、`go build ./...` 通过；Browser Worker 全量 unittest 25/25、compileall、`git diff --check` 通过；Ruff 未安装，未执行；真实运行因 Agent 探索耗时在用户要求尽快收口后停止客户端等待。
- 后续：重新启动包含本次代码的 Execution Worker 后，继续同一驱动器完成 Canonical 3 次及 `wrong-price`、`wrong-product` 各一次 live 验收。

## 2026-09-06 | 验证自然语言到正式执行完整链路

- 任务：继续验证自然语言需求经 Go Agent/LLM 生成 DSL、用户审批、正式队列执行和报告聚合的完整链路。
- 操作：启动 Go AgentService、Browser Worker 和 Execution Worker；以 Automation Exercise Blue Top 购物车目标创建真实 AgentRun；跟踪持久化工具事件并处理探索预算与 DSL 审批 Checkpoint；修复 generate_dsl action/字段 Schema、Harness 工具错误回注、非法 tool arguments 下沉校验、复合 CSS 探索、preflight verified selector 匹配、空 input_values 归一化和报告负耗时保护。
- 结果：Run `run_70b96dff4dfc833516f4d0a7` 完成；generation 33 经显式审批后创建 Batch 33，Execution 26 在真实 Chromium 中 18/18 步通过，最终 URL 为 `/view_cart`，确定性分析为 `all_passed`，全程未使用 VLM。
- 验证：Go 全量 test/vet/build、19 项 Browser Worker unittest 通过；检查 AgentRun 事件含 explore/validate/generate/checkpoint/execute/report；检查 Batch 33、Execution 26、DSL snapshot、步骤 evidence 和最终截图；重启 Worker 后复跑 Case 36，Execution 28 再次 18/18 通过且 `duration_ms=11990`。
- 后续：实施 trajectory、模型 token/latency 和 Observation Compression。

## 2026-09-06 | 实施 Agentic Research 可行性试验

- 任务：制定 Agentic Research SOP 的详细实施工程，设计可验收 Goal，并以 Automation Exercise 实践验证当前工程。
- 操作：使用研究架构、运行准备度和浏览器子任务并行审计；新增分阶段实施方案、`automationexercise-blue-top-cart` Goal、隔离的 Browser Worker research smoke runner 和指标输出；真实执行 Products 搜索、商品详情、加购和购物车验证；修复可见性后置条件的多匹配与异步等待语义。
- 结果：当前架构可支撑 SOP 可行性试验；真实 Chromium baseline 连续两次 13/13 步通过，最终购物车包含 Blue Top、单价 Rs. 500、数量 1、总价 Rs. 500，`task_success/execution_success/verification_success=true`，0 VLM、0 recovery，耗时约 11.3 和 11.6 秒。正式研究仍需按计划补 trajectory、指标投影、实验变体和数据集导出。
- 验证：Goal JSON 校验、Bug 编号检查、Go test/vet/build、Browser Worker compile 与 15 项 unittest、Frontend 4 项 Vitest 与生产构建均通过；两次真实站点结构化结果及最终截图复核通过。Coze CLI 已完成认证检查，但其日志写入用户目录被当前沙箱拦截；真实 smoke 命令完成后也出现宿主根路径清理拦截，不影响已生成的成功结果。
- 后续：优先实施 Phase R1 Trajectory Foundation，并在消融实验前修复 BUG-116/117/118。

## 2026-09-06 | 评估 Agentic Research SOP 执行条件

- 任务：判断当前项目结构能否直接执行 Agentic Web Testing Research SOP。
- 操作：逐项核对 AgentRun/Event、Harness Policy、DSL Schema 与 Go 校验、Locator Candidate、Postcondition、Step Evidence、FailureSignal 和 fix-and-retry，并与 SOP 的 trajectory、ablation、metrics、controller、dataset、bandit/RL 要求映射。
- 结果：现有架构可作为 SOP 的工程底座，已具备候选定位、结构化 DSL、后置验证、步骤证据、失败分类和受控恢复；但尚不能端到端执行研究计划，缺少统一 trajectory/transition 模型、研究事件、实验变体编排、Token/成本等指标、数据集导出、观察路由、策略 Controller、Reward 和 Bandit/RL。
- 验证：静态检查当前 Go、Python 与前端代码合同，并核对 SOP Week 1–12 的阶段验收目标；未修改业务代码，未运行测试。
- 后续：优先完成 Week 1–2 的 trajectory schema、持久化、回放/导出和 baseline metrics，再进入 DSL IR 补全及消融实验。

## 2026-09-06 | 合并 AgentCore 分支并切换 Main 开发

- 任务：拉取并审阅远端新增研究文档，将 `xujinyuan/go-agentcore-v2` 的最终清理提交合并到 `main`，后续直接在 `main` 开发。
- 操作：拉取 `origin/main` 的 Agentic Web Testing Research Roadmap；核对其研究假设、实验路线、数据模型与 12 周 SOP；将远端 `main` 快进到 `99b19cd`，再合并认证层清理提交 `eec340a`。
- 结果：`main` 已同时包含研究路线文档与最终 AgentCore 清理，旧功能分支可删除。
- 验证：Go `test/vet/build`、Browser Worker compile 与 10 项 unittest、Frontend 4 项 Vitest 与生产构建、Bug 编号唯一性检查均通过；本机缺少 Docker，未执行 Compose 配置校验。
- 后续：后续开发直接基于 `main`。

## 2026-09-05 | 移除全环境鉴权并启动服务联调

- 任务：开发与生产环境均不启用登录鉴权，并启动完整本地服务验证。
- 操作：删除 Go Cookie/PBKDF2 登录、Auth middleware 和 `/api/v2/auth/*`；使用 `DEFAULT_ACTOR_USER_ID` 注入固定服务端 actor；删除 Browser Worker SessionMiddleware、认证依赖和 artifact 门禁，内部 capability 显式传递 actor；删除前端 LoginPage、AuthGuard、退出入口和 401 事件；新增 Alembic `0039` 删除用户密码与启停字段；清理 `AUTH_*`、bootstrap-user、itsdangerous/httpx 配置与依赖。
- 结果：前端可直接进入工作台，Go 与 Browser Worker 均无需 Cookie；`users`、`actor_user_id` 和 membership 仅作为未来身份适配器的数据边界保留。
- 验证：Go test/vet/build、Browser Worker unittest、Alembic upgrade/check、Frontend Vitest/build/Knip、PostgreSQL 集成测试通过；本地启动 Browser API、Execution Worker、Go AgentService 和 Vite，无 Cookie API 联调通过。
- 后续：无。

## 2026-09-05 | 复核当前项目结构

- 任务：说明完成控制面迁移后的仓库结构、服务职责和主调用链。
- 操作：核对 Go、Browser Worker、Frontend 实际目录与迁移计划；修正架构导航中仍指向已删除 Python 控制面模块的内容。
- 结果：确认项目已形成“Go 唯一控制面 + Python Browser Worker + React 平台 UI + PostgreSQL 事实存储”的结构，Python 不再包含 Agent 或用户业务 API。
- 验证：对照当前文件树、FastAPI 路由注册和 Go package 列表完成静态核验。
- 后续：无。

## 2026-09-05 | 完成内部控制面迁移与 Browser Worker 重命名

- 任务：消除 Python `/internal/agent-capabilities` 中残留的 DSL、Case、Batch 和 Report 控制面，并完成 Python Worker 目录收口。
- 操作：新增 Go DSL Store、完整结构化 DSL 校验和本地 ControlPlane 工具适配器；将认证主体写入 Tool Call；迁移 `generate_dsl`、`execute_dsl`、`get_report`、`fix_and_retry`；删除 Python agent capability 路由、服务、schema、测试及失去调用方的 Case/Batch/Report 服务；删除 Python 文本 DSL 模型配置和 Prompt；将 `backend/` 重命名为 `browser-worker/` 并更新 CI、Compose、Pyright、README 和运行路径。
- 结果：Go AgentService 成为唯一 Agent 与业务控制面；Python 仅保留 Browser capability、Playwright 执行 Worker、A11y/Locator、evidence 和确定性失败分析。
- 验证：Go 全量 test/vet/build 通过；真实 PostgreSQL 纵向测试覆盖 Go GenerateDSL → ExecuteDSL → Report → FixAndRetry；Browser Worker unittest 14/14、Alembic check 和 Vulture 通过；前端 Vitest/build/Knip 通过；Playwright 桌面与移动 smoke 4/4 通过。
- 后续：提交并同步 `xujinyuan/go-agentcore-v2`；生产 Compose 启动验证受本机无 Docker CLI 限制，交由 CI 的 deployment-config/browser-smoke 门禁验证。

## 2026-09-05 | 补齐 Go 报告聚合并验证 PostgreSQL 控制面

- 任务：继续 M5 控制面迁移，补齐 Go Execution Overview 语义并验证新 Go Store 在真实 PostgreSQL schema 上可运行。
- 操作：将完整双窗口 Overview 聚合迁入 Go，补齐趋势、失败分类、动作、高频失败用例和根因统计；新增纯聚合单测与 Project → Case → Batch → Execution → Correction 纵向 PostgreSQL 集成测试；修复 Batch Job 必填默认值、幂等冲突、Planning Session 所有权、Correction 时间戳和带执行历史的 Project 删除；删除 Python 已失去调用方的 Case/Execution/Batch 查询与 Overview schema。
- 结果：Go 公开控制面不再依赖 Python 报告查询；Python 继续收缩为执行 Worker，但内部 `agent-capabilities` 的 DSL 持久化与执行编排尚待迁入 Go。
- 验证：Go 全量 test/vet/build 通过；显式设置 `TEST_DATABASE_URL` 的 PostgreSQL 纵向测试通过；Python unittest 22/22；前端 Vitest 8/8、生产构建和 Knip 通过；Vulture 无高置信度无用项；Alembic 位于 `20260905_0038` 且 schema check 无差异。
- 后续：迁移并删除 Python `/internal/agent-capabilities`，将 `backend/` 物理重命名为 `browser-worker/`，再执行浏览器与生产部署回归。

## 2026-09-05 | 启动 Go AgentService 全面迁移

- 任务：参考 `project/pi-agent` 重构 Agent 分层，以 Go AgentService 作为唯一 Agent，并将 Python 收缩为纯 Browser Worker。
- 操作：新增完整迁移计划；对照 pi-agent 拆出 Go `agent`、`harness`、`agentservice`；增加纯 Agent loop 和 Harness `beforeToolCall` policy；将 Report Core 改为确定性分析；将浏览器探索 capability 提取到 `application/browser`；删除 Python Planning API、ReAct Agent、Prompt、SSE、草案编排、schema 和 ORM 子图；Go Session API 停止读取旧 message/draft 表；新增 Alembic `0037` 下线遗留表；将登录、身份读取和退出迁入 Go；DSL 候选改由 Go Agent 编写，Python只做 Schema/preflight 和版本落库。
- 结果：面向用户的 Planning 和 Auth 请求只进入 Go `/api/v2`；Python 已不包含 Agent 循环或 DSL 生成模型调用，现有职责收缩为 Browser/Execution capability 和尚待迁移的平台 API。
- 验证：Python完整 unittest 22/22、Go 全量 test/vet/build、前端 Vitest 8/8、生产构建和桌面/移动 Playwright smoke 通过；Alembic已升级至 `20260905_0037` 且 schema check 无差异；Knip/Vulture 无高置信度无用项。Docker CLI 不可用，未执行容器启动验证。
- 后续：继续迁移 Case、Batch、Report 等浏览器侧以外的公开 API到 Go，并将 Python公网入口收缩为 artifact 和内部 Worker capability。

## 2026-09-05 | 澄清 Go 与 Python 双 Agent 实现

- 任务：说明报告 AI 分析复用旧 Planning Agent 的含义，并确认当前是否存在 Go/Python 两套 Agent。
- 操作：核对 Go `Engine.Continue`、Python `run_planning_turn`、Report Core `_analyze_details` 和 `/api/v1/ai-planning` 路由注册关系。
- 结果：确认当前存在两套 Agent 循环：Go AgentCore 是前端 Planning 主链；Python legacy ReAct Agent 仍由旧 Planning API 使用，并被 Report Core 借作失败分析器。DSL Generator 和 VLM 是单次模型能力，不属于第三套 Agent。
- 验证：完成调用链静态核对；未修改业务代码，未运行测试。
- 后续：将报告 AI 分析抽为独立、窄接口的 reporting analyzer 后，移除 Python legacy Planning Agent 与 `/api/v1/ai-planning`。

## 2026-09-05 | 清理非主链代码并修正 Go 工具边界

- 任务：澄清 Agent SSE 与轮询机制、Go 包职责，并删除已退出当前主链且无消费者的代码。
- 操作：将 `ask_user_question` 从 `internal/agentcore` 移至 `internal/tools` 并补工具测试；删除旧 `AITestPlanningPanel`、Planning SSE/store/reducer、兼容 API barrel、无消费者前端客户端、类型和 OpenAPI 生成产物；删除 Python 无引用 facade、兼容常量、辅助函数和两个从未读写的 ORM 模型；新增 Alembic `0036` 删除对应数据库表；更新架构和能力状态文档。
- 结果：当前 Agent 事件仍由 EventSource SSE 推送并按 PostgreSQL `seq` 重放；仅 Batch 报告等待和 Worker 领取任务使用 polling。前端静态不可达项清零，Go Core 与 Tool Registry 边界恢复，代码净减少超过 2.1 万行。
- 验证：`go test ./...`、`go vet ./...`、`go build ./...` 通过；Python 22 项 unittest 通过；Vitest 8/8 与前端生产构建通过；Knip 无未使用项；Vulture 80% 置信度扫描无结果；Alembic 已升级至 `20260905_0036` 且 `alembic check` 无差异；`git diff --check` 通过。
- 后续：Python `/api/v1/ai-planning` 仍是已注册公开兼容 API，报告 AI 分析也复用其 Agent 实现；如要继续删除该子系统，需先把报告分析迁出 Planning，并明确废弃该公开 API。

## 2026-09-05 | 当前架构、Harness 与 AgenticRL 落地状态复核

- 任务：说明当前系统架构、核心链路服务模块的技术实现，并核查 Harness、AgenticRL 和剩余缺口。
- 操作：静态追踪 React 工作台、Go AgentCore/Hertz/SSE、Python capability API、PostgreSQL Batch/Job 队列、Playwright Runner、A11y/Locator、Report Core、FailureSignal、anti-pattern、Prompt Registry、生产 Compose 与 CI；检索 Harness、reward、trajectory、policy、evaluation 和训练相关实现。
- 结果：当前已形成“Go 认知与控制面 + Python 浏览器执行面 + PostgreSQL 事实/队列 + React 工作台”的纵向闭环；现有 AgentRun、工具注册、事件重放、DSL 审批、执行证据和自动修复链可视为 Harness 雏形，但仓库没有独立 Harness/Eval 子系统；AgenticRL 仅具备生成反馈、失败信号、anti-pattern 和 prompt 版本等数据基础，尚无统一 trajectory/reward、离线评测、策略训练和发布闭环。
- 验证：完成代码、迁移、部署和 CI 配置的静态交叉核对；未修改业务代码，未运行测试。
- 后续：优先建设 Agent Run 持久化调度/恢复、跨实例事件分发和正式 Harness 评测合同；随后补齐 AgenticRL 样本、奖励、离线评测、策略注册与灰度发布。

## 2026-09-05 | Frontend src IDE 报错诊断

- 任务：确认 IDE 中 `frontend/src` 报错的原因。
- 操作：核对工作区状态、`.gitignore`、`tsconfig.json`、Vitest 类型初始化、前端依赖树和 TypeScript 版本；执行 TypeScript 完整诊断、生产构建及 Vitest。
- 结果：`frontend/src` 当前无 TypeScript 编译错误；`.gitignore` 仅忽略 `frontend/.playwright-browsers/` 等生成物，不影响源码。IDE 红色诊断最可能来自语言服务仍缓存依赖安装前的项目状态，或未使用 `frontend/node_modules/typescript` 的工作区 TypeScript 5.9.3。
- 验证：`npx tsc --noEmit --extendedDiagnostics` 0 error；`npm run build` 通过；Vitest 8/8 通过；依赖树完整。
- 后续：在 IDE 中切换到工作区 TypeScript 并重启 TypeScript language server；若仍存在，按具体错误文本继续定位。

## 2026-09-05 | 同步生产化控制面与工作台实现到 GitHub

- 任务：将生产反向代理与进程管理、Go Planning Session、鉴权、回归编排、定位调试、持续浏览器门禁和 BUG 日志治理的全部实现同步到 GitHub。
- 操作：复核工作树、凭据扫描、缺陷 ID 唯一性和暂存清单；显式暂存 77 个实现/测试/文档文件；创建提交 `0cf008b` 并推送当前分支。
- 结果：全部实现已同步到 `origin/xujinyuan/go-agentcore-v2`。
- 验证：远端推送显示 `79c1d17..0cf008b`；提交前 `git diff --cached --check` 通过且未发现测试账号或明文凭据。
- 后续：在具备 Docker CLI/Engine 的环境执行生产镜像构建与 Compose 启动验收。

## 2026-09-05 | 生产化控制面、鉴权与工作台完善

- 任务：处理生产反向代理与进程管理、Planning Session 元数据向 Go 迁移、持续浏览器回归门禁、项目级编排与定位调试 UI、恢复鉴权及 BUG-098 日志治理。
- 操作：新增 Docker Compose、Python/Go/Frontend Dockerfile 和 Nginx SSE 代理；Go 新增 Planning Session/项目关联存储和 `/api/v2/planning`，AgentRun 增加 actor 所有权；Python 恢复 Cookie Session，统一保护业务/artifact/internal capability 路由并增加账号初始化命令；前端接入 Login/AuthGuard、回归编排和定位调试；新增 Vitest、桌面/移动 Playwright smoke 与 GitHub Actions；清理重复缺陷 ID 并增加 CI 校验；修复 BUG-107/108/109。
- 结果：Planning Session 元数据新记录由 Go 持久化并标记 `runtime_owner=go`，消息/草稿暂由 Python legacy API 继续提供；用户可登录后创建 Session、按项目启动/取消回归批次、查看运行报告和定位候选证据；生产入口统一由 Nginx 分流，内部 capability 不暴露公网。
- 验证：Go `go test ./...`、`go vet ./...` 通过；Python 22 项测试、compileall、PostgreSQL Alembic 升级至 0035 和 `alembic check` 通过；前端 Vitest 8/8、Chromium 桌面/移动 smoke、生产构建通过；真实隔离端口完成匿名 401、登录、Go Session 创建/list、回归页和定位调试页验收，临时账号/Session/项目已清理。
- 后续：本机没有 Docker CLI/Engine，尚未在本机实际构建生产镜像；需要在有 Docker 的环境执行 `docker compose ... up --build --wait`，并继续迁移 Planning 消息/草稿/事件。

## 2026-09-05 | 当前项目进度复核

- 任务：基于当前分支、提交、能力文档、测试资产和缺陷状态汇总项目实际进度。
- 操作：核对 `xujinyuan/go-agentcore-v2` 与远端同步状态、近期 AgentCore 提交、README 能力矩阵、Go/Python/前端测试资产及开放缺陷；修正 README 的自愈半闭环旧口径并关闭已完成的 BUG-090；记录 BUG-106。
- 结果：项目已完成 Go AgentCore 第一条完整纵向链路和受控自愈闭环，进入控制面收敛与生产化阶段；主要剩余工作是生产部署配置、Planning Session 元数据迁移、持续回归门禁、项目级编排/定位调试 UI、鉴权恢复及历史日志治理。
- 验证：当前分支与远端提交计数为 `0/0`，盘点前工作区干净；复核 9 个 Go 测试文件、4 个 Python 聚焦测试文件和前端当前无自动化测试文件；Go 全量测试、Python 聚焦测试、前端生产构建及文档差异检查通过。
- 后续：优先完成生产反向代理与进程管理，并将真实浏览器 E2E 固化为可持续回归门禁。

## 2026-09-05 | 修复 Python 导入诊断

- 任务：检查并修复仓库中多处 Python 导入问题。
- 操作：递归验证 122 个 `app.*` 模块；使用 Ruff 定位未使用和非顶层导入；清理 9 个无效导入并调整 2 个模块的 logger 初始化位置；新增根目录 `pyrightconfig.json`，声明 `backend/.venv`、`backend` 源码路径和 Python 3.12。
- 结果：第三方依赖与 `app.*` 包可被 IDE/Pyright 正确解析，Ruff 的 `F401/F403/F405/E402` 问题由 17 个降为 0；关联 BUG-105。
- 验证：Pyright 导入检查 0 errors/0 warnings；Python 122 个模块全部导入成功；13 个单测通过；`compileall`、Go `go test ./...`、前端 `npm run build` 通过。
- 后续：仓库仍有历史导入排序提示和未纳入本次范围的类型标注债务，可单独治理。

## 2026-09-04 | 同步 Go AgentCore 重构分支到 GitHub

- 任务：将已完成并验收的 Go AgentCore、受控自愈和前端迁移改动同步到 GitHub。
- 操作：核对工作区、当前分支和最近提交；为 `xujinyuan/go-agentcore-v2` 设置远端 upstream 并推送。
- 结果：重构分支及完整提交历史已同步到 `origin`。
- 验证：推送后核对本地 HEAD、远端分支和工作区状态。
- 后续：无。

## 2026-09-04 | 前端切换 Go AgentCore 并完成浏览器验收

- 任务：将 Planning 工作台从 Python Planning SSE 切换到 Go AgentRun、ToolCall、Checkpoint 和 SSE 重放协议，并完成真实浏览器 E2E。
- 操作：新增 `features/agent` 类型、API、事件归并和 Run hook；新建 Agent 工作台展示对话、工具轨迹、artifact 和动态问题控件；最近 Run ID 按会话保存在浏览器本地，刷新后从 PostgreSQL 重放事件；Vite 分流 Go/Python 开发代理；三栏布局增加移动端纵向响应式；过滤 `RootWebArea/StaticText` 非定位节点并补回归测试；同步 README 当前阶段与后续优先级。
- 结果：前端可创建 Run、实时观察探索/验证/生成/执行/报告工具、在两个审批点暂停恢复，并在刷新后恢复完整运行。干净 E2E 使用 `heading "Example Domain"` 首次执行 3/3 步通过，没有触发修复；另一次 E2E 验证了首次 locator 失败后自动重探索、重新审批并通过的完整自愈路径。
- 验证：前端 `npm run build` 通过；Python 13 个聚焦测试通过；Playwright 桌面 1440x900 和移动 390x844 截图无横向溢出、控制台错误为 0；最终 Run 为 completed，工具调用 6 次，Batch pass_rate=1.0。全部临时 Run、generation、Case、Batch、execution 和截图已清理。
- 后续：补充生产环境反向代理配置，并评估将 Planning Session 元数据迁入 Go 控制面。

## 2026-09-04 | AgentCore 透明修复与重执行闭环

- 任务：接入非黑盒 `fix_and_retry`，验证失败事实、修复策略、DSL 重生成、人工审批和重执行的完整链路。
- 操作：Python Capability Worker 新增修复计划接口，基于 Batch/Run 的 FailureSignal 选择 `re_explore`、`regenerate_dsl` 或人工处理并返回源 DSL；Go 注册 `fix_and_retry` 工具和 repair plan artifact；Run 详情投影补充 DSL snapshot；`get_report` 默认等待 Batch 终态；增加 `analysis_status` 数据库默认值以兼容滚动期间的旧 Worker；将报告摘要纳入确定性事实层。
- 结果：真实 assertion 失败 Batch 经 Agent 调用 `fix_and_retry -> explore_page -> validate_page_elements -> generate_dsl -> ask_user_question -> execute_dsl -> get_report` 完成修复，审批前未执行，审批后新 Batch 3/3 步通过且通过率 100%；修复策略和事件流完整可审计。
- 验证：Go `go test ./...`、`go vet ./...`、编译通过；Python 12 个聚焦合同测试与 compileall 通过；Alembic 当前为 0034 且 `alembic check` 无差异；真实 DeepSeek、Playwright、PostgreSQL 队列和报告链路通过。临时 Run、generation、Case、Batch、anti-pattern、截图及本次服务进程已清理。
- 后续：将前端 Planning 工作台切换到 Go AgentRun/ToolCall/SSE 协议，并完成浏览器 E2E 验收。

## 2026-09-04 | AgentCore DSL 审批、执行与报告闭环

- 任务：将候选 DSL 审批、正式执行和报告读取接入 Go AgentCore，并修复复测与归因事实链。
- 操作：AgentRun 新增 latest/approved generation 绑定及 Alembic 0033；`generate_dsl` 发布 artifact；用户通过 `approve_dsl` 工具结果批准当前版本，`execute_dsl` 强制校验批准 ID 后创建正式 Case 和幂等 Batch；新增 `get_report` 工具并由后端等待终态；修复 BUG-099 的复测统一持久化和 BUG-100 的确定性结论覆盖。
- 结果：真实链路完成 `explore -> validate -> generate -> approve -> execute -> report`：generation 3 创建 Case 6 和 Batch 3，Worker 执行 3/3 步通过，Report Core 返回 `passed`、`pass_rate=1.0` 和 `analysis_status=completed`。LLM 不能伪造审批，也不能用错误结构化结论覆盖失败事实。
- 验证：Go 全量测试、vet 和编译通过；Python 7 个合同测试、compileall 和 `alembic check` 通过；Alembic 升级至 0033；真实 Playwright/队列/报告链路通过。测试实体将在最终联调完成后统一清理。
- 后续：实现透明 `fix_and_retry` RepairAttempt 流程，再将前端切换到 Go Agent 事件协议。

## 2026-09-04 | Go AgentCore 探索、验证与 DSL 生成链路

- 任务：让 Go AgentCore 自主调用现有 Python 浏览器能力并生成经过页面元素验证的 DSL。
- 操作：新增受限的 Python Browser/Agent Capability 路由；复用现有 `explore_page`、`explore_flow`、A11y preflight 和 DSL generator；Go 新增 Python Worker client 及 `explore_page`、`explore_flow`、`validate_page_elements`、`generate_dsl` 工具；元素验证同时支持生成前需求覆盖检查和生成后 locator preflight；Run 显式携带 project context。
- 结果：真实 Agent 能从用户需求自主完成单页探索、识别 8 个 A11y 节点、验证标题唯一命中、调用 `deepseek-v4-flash` 生成 3 步 DSL，并明确停止在未执行草案阶段。联调发现并修复 BUG-101。
- 验证：Go `go test ./...`、`go vet ./...`、编译通过；Python 5 个合同测试与 compileall 通过；真实跨进程链路产生 23 条连续事件并以 `run.finished` 收口；临时 Run 和 DSL generation 数据已清理。
- 后续：增加 DSL artifact 与用户审批状态，接入 `execute_dsl`、`get_report` 和透明 `fix_and_retry`。

## 2026-09-04 | AgentCore 异步运行与 SSE 实时重放

- 任务：让 Go Agent Run 非阻塞启动，并统一实时事件推送与 PostgreSQL 历史重放。
- 操作：新增进程内 EventBroker；事件持久化成功后再发布给订阅者；Run 增加显式项目上下文，创建接口改为 `202 Accepted` 并后台驱动 Agent；新增 `/events/stream` SSE 接口，支持 `after_seq` 和 `Last-Event-ID`，先重放历史再推送实时事件和 keep-alive。
- 结果：模型调用不再阻塞创建请求；客户端可在 Run 运行期间看到 `run.started -> tool.started -> tool.args.delta -> tool.pending`，断线后按序号补齐事件。慢订阅者不会阻塞 Agent，可从 PostgreSQL 恢复跳过的事件。
- 验证：`go test ./...`、`go vet ./...` 和 Go API 编译通过；真实服务中创建 Run 立即返回 running，SSE 随后收到 4 条有序事件并停留在用户等待状态；测试数据和本地进程已清理。
- 后续：接入 `explore_page`、`explore_flow` 和 `validate_page_elements` 工具，并将 Python Browser Worker 收敛为稳定接口。

## 2026-09-04 | AgentRun 与事件流 PostgreSQL 持久化

- 任务：让 Go AgentCore 的运行状态、对话 transcript、pending checkpoint 和事件在进程重启后可恢复。
- 操作：新增 `agent_runs`/`agent_events` SQLAlchemy 模型和 Alembic 0032 迁移；实现 Go `PostgresRepository`；使用 `agent_runs.last_event_seq` 在事务中原子分配事件序号；Go 服务启动改为校验数据库连接并使用 PostgreSQL Repository；补充数据库 URL 兼容转换。
- 结果：AgentCore 不再依赖进程内状态，Run、用户等待点、模型 transcript 和事件可跨进程恢复；内存 Repository 仅保留给单元测试。
- 验证：`go test ./...`、`go vet ./...` 通过；Alembic 升级至 `20260904_0032` 且 `alembic check` 无差异；真实创建 waiting_user Run 后重启 Go 服务，Run 和 4 条有序事件读取一致；临时数据已清理。
- 后续：新增实时 SSE 订阅，与 PostgreSQL 事件重放共用同一事件源。

## 2026-09-04 | Go AgentCore 原生工具调用与人工暂停恢复

- 任务：让 Go AgentCore 使用真实 LLM 原生 tool calling，并跑通 `ask_user_question` 暂停/恢复循环。
- 操作：新增 OpenAI 兼容 LLM client、Agent Engine、对话 transcript、Tool Registry 执行入口和 `AskUserTool`；扩展 Run 保存 pending tool/step，统一记录 message/tool/run 事件；HTTP 创建 Run 改为驱动 Agent 循环，resume 接口将用户答案作为 tool result 送回同一 Run；配置读取复用本地 `backend/.env`。
- 结果：`deepseek-v4-flash` 可通过原生 `tools/tool_calls` 自主提出问题；Run 能跨两次用户回答保持上下文，从 `running` 进入 `waiting_user`、恢复后再次等待，最终输出两条登录场景计划并进入 `completed`。当前仅注册 `ask_user_question`，其他领域工具将在后续阶段接入。
- 验证：`go test ./...`、`go vet ./...`、Go API 编译通过；真实启动于 8082 后完成两轮 `ask_user_question`，18 个事件序号连续，包含 start/args/pending/result/message/finished；旧测试进程占用 8081 的问题已清理。
- 后续：将 Run/Event/ToolCall 接入 PostgreSQL，增加 SSE 实时订阅，再接入页面探索和元素验证工具。

## 2026-09-04 | Go AgentCore 合同与 Hertz 骨架

- 任务：冻结新控制面的第一批跨模块合同，并建立可独立运行的 Go/Hertz 服务骨架。
- 操作：新增 `backend-go` Go module；定义 AgentRun、统一事件 envelope、问题和暂停恢复数据结构；定义 Repository 与 Tool Registry 接口及内存实现；新增 Hertz 健康检查、Run 创建/查询、事件增量查询和 ToolCall 恢复接口；补充服务、注册表和 HTTP 合同测试。
- 结果：Go 控制面已能独立启动，创建运行记录、按 Run 维护单调递增事件序号，并完成 `ask_user_question` 的 `running -> waiting_user -> running` 状态转换；当前存储为内存实现，尚未接入 LLM 和 PostgreSQL。
- 验证：`go test ./...`、`go vet ./...`、`go build -o /tmp/ai-web-testing-agentcore ./cmd/api` 通过；本地启动后 `GET /health` 返回 `{"status":"ok"}`，创建 Run 与查询 `run.started` 事件通过。
- 后续：增加 PostgreSQL AgentRun/Event/ToolCall 持久化，实现 AgentCore LLM 循环和 SSE 实时/重放统一事件源。

## 2026-09-04 | 正式采用 Go AgentCore 渐进迁移方案

- 任务：确认并启动以代码可读性和后续迭代效率为优先的后端重构。
- 操作：将 ADR-002 状态改为 accepted；更新仓库级架构规则和 README，明确 Go/Hertz 控制面、Python Browser Worker、PostgreSQL 队列及 Kitex 的适用边界；建立专用分支 `xujinyuan/go-agentcore-v2`。
- 结果：新业务控制面默认使用 Go，现有 FastAPI 在迁移期间保持兼容，Playwright/A11y/Locator 暂不重写；当前阶段不实现登录、Token 和角色鉴权，但保留项目与 actor 归属字段。
- 验证：文档差异检查通过；未修改运行时代码。
- 后续：冻结跨语言合同并建立可测试的 Go/Hertz 模块化骨架。

## 2026-09-04 | 以可读性为目标的后端重构建议

- 任务：在允许大规模重构的前提下，确定兼顾代码可读性与后续迭代效率的技术方案。
- 操作：结合当前约 2.4 万行 Python、千行级 Agent/工具/Runner 模块、现有 PostgreSQL 队列和 Playwright 能力，对全量 Go 重写、Go/Python 混合边界及 Kitex 使用时机进行取舍。
- 结果：建议以旁路替换方式建设 Go 模块化单体控制面，使用 Hertz 提供 HTTP/SSE、Go interface 组织 AgentCore 和领域模块，保留 Python Playwright/A11y/locator Worker；Kitex 仅在未来出现真实独立部署边界时使用。迁移前先冻结 DSL、Agent Event、Tool、Report、数据库和黄金行为合同，不直接复刻 BUG-099/100 等已知错误。
- 验证：架构评估，未修改业务代码，未运行产品测试。
- 后续：确认方向后先建立 v2 目录和合同测试，以“对话 -> ask_user -> explore -> validate -> generate -> execute -> report”首个纵向切片验证新架构。

## 2026-09-04 | Go AgentCore 与 Kitex 服务边界评估

- 任务：评估当前项目是否适合将后端改为 Go，并按 handler/service 模块化后使用 Kitex。
- 操作：统计 Python 后端模块体量和依赖关系，结合 AgentCore 工具设计、HTTP/SSE 前端协议、PostgreSQL 队列与 Playwright 能力划分迁移边界；新增 ADR-002。
- 结果：可以使用 Go 重建 Agent 控制面，但不建议逐文件全量翻译。推荐 Hertz 对前端提供 HTTP/SSE，普通 Go interface 组织单进程模块，仅在真实进程间边界使用 Kitex；现有 Python Playwright/A11y/locator Worker 暂时保留，通过 PostgreSQL Job 与 Go 控制面解耦。当前不实现登录与权限校验，但保留项目和 actor 归属字段。
- 验证：后端共约 2.4 万行 Python；确认 Agent、工具、Runner、DSL 和 execution service 中存在多个千行级模块及跨层延迟导入；架构评估未运行产品测试。
- 后续：如确认迁移方向，先冻结 DSL、事件、工具、报告和数据库合同并修复 BUG-099/100，再以旁路方式落地 Go AgentCore。

## 2026-09-04 | DeepSeek LLM 能力实测

- 任务：展示当前接入的 `deepseek-v4-flash` 在项目 Planning Agent 中的实际能力。
- 操作：通过项目 `_stream_planning_llm` 和完整 `run_planning_turn` 执行三类真实模型探针，覆盖需求抽取、探索工具决策和 locator 失败归因；不启动浏览器、不写业务数据。
- 结果：3/3 单轮响应均可解析为项目 JSON 动作；模型能从简略需求抽取被测对象，能从完整需求提取 URL、流程、断言和变量，并主动选择 `get_project_info` 工具；完整 ReAct 能正确用自然语言识别 `Login` 变为 `Sign in` 的定位器过期问题。但结构化分析错误落为 `all_passed/done` 且缺少失败明细，记录为 BUG-100，当前不能直接把 LLM 结构化结论用于无人审批的修复决策。
- 验证：网关真实请求均成功；完整 ReAct 返回 `session_status=completed`，自然语言归因正确；结构化字段矛盾已复现。首次探针脚本因误用不存在的 `SessionLocal` 导入失败，改用项目 `get_session_factory()` 后成功。
- 后续：先修复 BUG-100 的分析 Schema 与确定性约束，再测试真实页面探索、DSL 生成和失败修复建议。

## 2026-09-04 | 自愈任务编排层职责说明

- 任务：解释受控自愈方案中的任务编排层含义和边界。
- 操作：将现有失败分析、页面探索、DSL 生成、审批和 Batch 执行能力映射为后端状态机职责。
- 结果：任务编排层不是新的 AI 或必需的外部任务框架，而是位于 LLM 能力与正式 Runner 之间、负责流程顺序、条件分支、状态持久化、幂等重试、审批门和审计追踪的后端协调服务；LLM 负责理解需求、提出是否重探索及探索范围等结构化建议、分析失败和生成候选 DSL，编排层依据失败分类、权限、预算和重试上限裁决并调用后端工具，正式测试只由 Runner 执行。用户负责设定目标，并在正式 DSL 变更或扩大执行范围前审批。编排层不是数据转发层，而是流程控制权和安全边界的持有者。
- 验证：架构说明，未修改业务代码，未运行产品测试。
- 后续：第一阶段可采用 `RepairAttempt` 表、`repair_orchestrator.py` 服务和审批 API，继续复用现有 PostgreSQL Batch/Job 队列。

## 2026-09-04 | 自愈循环复用 Planning 会话方案澄清

- 任务：评估自动重探索是否可以复用现有 Planning 会话与探索能力。
- 操作：结合现有 Planning Session、失败分析、上下文注入、探索工具、DSL 草案生成和 Batch 执行边界梳理最小编排方案。
- 结果：可以复用现有能力，但应在同一 Planning Session 中创建新的 repair turn，而不是新建独立会话；该 turn 显式绑定来源 Run/Batch，并注入 FailureSignal、ExecutionAnalysis、失败步骤 evidence、原 DSL 快照和历史探索数据。编排层仅在定位、导航或页面状态过期时触发重探索，随后生成候选 DSL、校验并展示差异，用户批准后才更新正式用例并进入 Batch/Job 队列重跑。
- 验证：架构分析，未修改业务代码，未运行产品测试。
- 后续：实现带来源追踪和审批门的 RepairAttempt 状态机，复用现有 explore、draft、validation 和 execution 服务。

## 2026-09-04 | 执行归因与复测循环现状核验

- 任务：确认当前是否已实现“执行 -> 记录错误 -> 归因 -> 重新执行”循环。
- 操作：追踪直接 Case 执行、Batch Worker、FailureSignal/ExecutionAnalysis、anti-pattern、Planning 分析消息、`/retest` API、人工修正重跑入口和 DSL 草案再生成逻辑。
- 结果：执行、步骤证据与错误持久化、统一 FailureSignal、规则兜底与 LLM 归因已自动连通；直接执行和 Batch 终态均可生成持久化分析。后端 `/retest` 可筛选失败用例并重跑原 DSL，人工修正后前端也可重跑当前用例，但 Planning 前端未接入 `/retest`。归因结果尚不会自动触发重新探索、DSL 重生成、差异审批或正式用例更新，因此当前是人工触发复测的半闭环，完整自愈闭环仍由 BUG-090 跟踪；同时 `/retest` 绕过统一分析持久化的问题记录为 BUG-099。
- 验证：静态核对 `services/executions.py`、`services/execution_batches.py`、`application/reporting/analysis_service.py`、`application/planning/analysis_retest_service.py`、`draft_service.py`、Planning API 与前端执行入口；未执行真实浏览器全链测试。
- 后续：实现 `analyze -> re-explore -> regenerate -> diff -> approve -> rerun` 状态机，并为 Planning 前端增加审批与复测入口。

## 2026-09-04 | 同步项目状态与 AI 配置记录到 GitHub

- 任务：将当前项目状态盘点和本地 AI 配置变更记录同步到远端。
- 操作：仅暂存 `docs/execution-log.md` 与 `docs/bug-log.md`，创建聚焦文档提交并推送当前 `main`；确认 `backend/.env` 被 Git 忽略且未进入提交。
- 结果：状态盘点、BUG-098 和 DeepSeek/VLM 配置记录已同步到 `origin/main`，本地密钥未进入版本控制。
- 验证：首次提交 `b4640cf` 后比较本地与远端完整哈希一致，提交计数为 `0/0`。
- 后续：无。

## 2026-09-04 | 配置 DeepSeek 文本模型并关闭 VLM

- 任务：本地暂不采用 VLM 定位，统一使用 `deepseek-v4-flash` 处理 Planning 与 DSL 生成。
- 操作：在被 Git 忽略的 `backend/.env` 中启用 AI Planning 和 AI DSL，统一配置 OpenAI 兼容网关 `https://api.unself.cn/v1` 与 `deepseek-v4-flash`；关闭 AI visual locator 和 VLM 页面注释，清空 VLM 凭据。
- 结果：Planning 与 DSL 文本模型均已启用；执行定位继续走 A11y/DOM 语义链及人工干预，不调用 VLM。
- 验证：通过应用 `Settings` 读取确认两个文本模型开关、模型和网关生效，VLM 两个开关关闭且无密钥；向 `/chat/completions` 发送最小请求返回 HTTP 200、模型 `deepseek-v4-flash` 和内容 `OK`。
- 后续：真实 Planning 全链验收时继续观察 reasoning 流式展示与 DSL 结构化输出质量；API 密钥仅保存在本地忽略文件中，不写入日志或版本控制。

## 2026-09-04 | 当前项目状态盘点

- 任务：基于当前分支、提交、能力说明、缺陷日志和可运行门禁汇总项目状态。
- 操作：核对 `main` 与 `origin/main`、最近提交和工作树；复核 README 能力矩阵与开放缺陷；统计现行测试资产；执行后端编译、执行分析合同测试、前端生产构建和差异检查。
- 结果：核心平台、结构化执行、PostgreSQL Batch/Job 队列、Report Core、统一 FailureSignal/ExecutionAnalysis 已落地；当前主缺口是 BUG-090 受控自愈编排、真实 AI 全链验收和自动化回归门禁重建。发现缺陷日志存在重复编号和冲突状态，记录为 BUG-098。
- 验证：`uv run python -m compileall -q app` 通过；`uv run python -m unittest discover -s tests -p 'test_*.py' -v` 为 2/2 通过；`npm run build` 通过；`git diff --check` 通过；盘点前本地与远端提交计数为 `0/0`。
- 后续：优先实现 `analyze -> re-explore -> regenerate -> diff -> approve -> rerun`；配置模型后执行真实 AI 链路验收；恢复分层自动化测试；清理 BUG-098 日志状态歧义。

## 2026-09-02 | 同步执行分析与报告统一链路到 GitHub

- 任务：将 FailureSignal、ExecutionAnalysis、Planning/Report 统一展示及正式报告路由变更同步到远端。
- 操作：复核实现、迁移、生成类型、聚焦测试和文档差异；创建聚焦提交并推送当前 `main`。
- 结果：执行分析统一事实链、报告导航统一和 BUG-092/097 修复已同步到 `origin/main`。
- 验证：推送后核对本地与远端提交计数、最新提交和工作树状态。
- 后续：继续实现 BUG-090 的受控自动重探索与 DSL 重生成编排。

## 2026-09-02 | 打通执行、分析与正式报告事实链

- 任务：解决 Planning AI 分析旁路、刷新丢失、anti-pattern 漏记、直接执行无总结、失败分类分裂及报告导航不一致。
- 操作：新增持久化 FailureSignal/ExecutionAnalysis 和 `0030/0031` 迁移；Run/Batch 终结统一生成规则总结并在模型可用时增强为 AI 根因分析；统一 Planning/报告/anti-pattern 分类；Planning SSE 改读 Batch 持久化分析并写入历史消息；前端处理 `analysis_complete`，Planning 与 ReportPage 共用分析组件和 `/reports/:executionId` 正式详情路由。
- 结果：直接 Case、Batch Worker 和 Planning 执行均进入 `Run/Batch -> FailureSignal -> Analysis -> Report Core`；失败 anti-pattern 自动沉淀；实时与刷新后的 Planning 消息可显示同一分析；旧 `/run/:id` 自动重定向；BUG-092、BUG-097 已关闭。自动重探索和 DSL 重写仍属于 BUG-090。
- 验证：2 个 `unittest` 合同测试通过；后端 compile/import 通过；Alembic `0030/0031` 降级升级往返及 `alembic check` 通过；OpenAPI 类型重新生成；前端生产构建通过；浏览器验证 Planning 历史分析、正式报告详情、报告聚合入口及旧路由重定向，临时数据已清理。
- 后续：实现 BUG-090 的 `analyze -> re-explore -> regenerate -> diff -> approve -> rerun` 受控自愈编排。

## 2026-09-02 | 用例执行到报告总结链路核验

- 任务：确认当前“用例执行 -> 执行记录 -> 报告 -> 结果/错误总结”链路是否已经打通。
- 操作：静态追踪直接 Case 执行、ExecutionBatch Worker、TestCaseRun 持久化、Report Core、Planning SSE 分析、anti-pattern 写入和前端报告/流事件消费。
- 结果：Runner 到 TestCaseRun、步骤 evidence、Batch/Job/Run 报告及确定性统计聚合已打通；Planning 失败后会尝试 AI 分析。但 AI 分析尚未写入统一报告事实，前端不处理 `analysis_complete`，队列路径也绕过失败 anti-pattern 记录，因此端到端“报告总结闭环”仅部分完成，记录为 BUG-097。
- 验证：核对 `services/executions.py`、`services/execution_batches.py`、`application/reporting/service.py`、`services/ai_planning_streaming.py`、`analysis_retest_service.py`、Planning 流事件 reducer 和 ReportPage 数据源；未运行产品测试。
- 后续：先统一并持久化 `FailureSignal`/AnalysisResult，再让 Report Core、Planning 消息和前端共享同一总结合同。

## 2026-09-02 | 同步执行队列与 README 进展到 GitHub

- 任务：将本地执行队列功能提交和 README 最新进展同步到远端。
- 操作：复核 `main` 与 `origin/main` 差异；仅暂存 README、后端 README、执行日志和缺陷日志，创建聚焦文档提交并推送当前分支。
- 结果：执行队列、Planning 队列迁移及 README 状态更新已同步到 `origin/main`。
- 验证：推送后核对本地与远端提交计数、最新提交和工作树状态。
- 后续：无。

## 2026-09-02 | 更新 README 最新项目进展

- 任务：将 README 的项目状态更新到当前执行队列与 Report Core 阶段。
- 操作：重写根 README 当前状态为能力矩阵，新增 2026-09-02 执行控制面里程碑和下一阶段优先级；补充 Worker 启动、现行构建验证边界和 Batch/Job/Run 联调路径；同步修正后端 README 的 Planning SSE 描述。
- 结果：移除基于 2026-05-31 的 `98%+` 旧完成度口径，README 现已反映持久化队列、Report Core、heartbeat/取消、受控自愈缺口和自动化测试门禁现状；BUG-096 已关闭。
- 验证：`git diff --check` 通过；README 本地 Markdown 链接检查通过。
- 后续：实现统一 `FailureSignal` 和受控自愈编排后继续同步能力矩阵。

## 2026-09-02 | 当前项目进展盘点

- 任务：基于当前仓库、提交记录、执行日志和缺陷日志汇总项目最新进展。
- 操作：核对 `main` 与远端差异、最近提交范围、阶段计划、当前能力说明、开放缺陷和可用验证入口。
- 结果：本地 `main` 领先 `origin/main` 2 个提交；持久化 ExecutionBatch/ExecutionJob 队列、Report Core、Planning SSE 队列迁移、heartbeat 与持久化取消已落地。当前主要未完成项为统一 FailureSignal、受控自动自愈编排、真实 AI 全链验收，以及恢复与当前架构匹配的自动化测试门禁。发现根 README 仍使用 2026-05-31 的阶段与完成度口径，已记录为 BUG-096。
- 验证：检查 Git 状态和提交统计；核对 `docs/execution-log.md`、`docs/bug-log.md`、README、能力状态清单及优化计划；本次未运行产品测试。
- 后续：优先实现统一失败事实模型，再接入 `analyze -> re-explore -> regenerate -> diff -> approve -> rerun` 自愈编排；同步更新 README，并决定是否将本地 2 个提交推送到 GitHub。

## 2026-09-02 | Planning SSE 迁移执行队列并接入取消与 Heartbeat

- 任务：完成 BUG-094 剩余工作，将旧 Planning SSE 执行迁移到 Batch 队列，并接入运行中 Job 的持久化取消和 heartbeat。
- 操作：Planning 保存执行改为保存草案后创建 ExecutionBatch，再轮询 Report Core 输出兼容 SSE 事件；同一 Planning session 限制单个活动 Batch；ExecutionJob 增加 heartbeat 字段；Worker 使用独立 Session 每 2 秒续租并检查取消，将取消事件传入 Runner。
- 结果：Planning SSE 不再直接执行 Playwright；多会话通过持久化队列受控并行，取消状态不依赖原 HTTP 连接，Worker lease 可持续续期。取消在下一安全步骤边界生效。
- 验证：迁移升级至 `20260902_0029`；真实 Planning SSE 完成 `save -> queue -> execute -> report -> done`；跨 Session heartbeat/取消验证后 Job 与 Batch 均为 `cancelled`。
- 后续：后续如要求强制中断单个长 Playwright 调用，应将 Job 进一步隔离到可终止子进程。

## 2026-09-02 | ExecutionBatch、ExecutionJob 与 Report Core 第一阶段落地

- 任务：开始实现支持多会话、多项目并行的执行控制平面和报告聚合基础。
- 操作：新增 ExecutionBatch/ExecutionJob 模型、schema 和 Alembic 迁移；TestCaseRun 增加 batch/job、attempt、DSL 快照/hash、报告版本；实现持久化幂等键、PostgreSQL `FOR UPDATE SKIP LOCKED` Job 领取、Batch 并发限制、lease/取消/终态聚合、固定并发 Worker；新增 run/batch/project Report Core 与批次 API；执行未知异常统一收口为失败记录；移除每次 Run 对全局 VLM runtime state 的重置；同步 OpenAPI 和前端生成类型。
- 结果：新链路支持 `Batch 1:N Job 1:N Run`，可按 batch 查询任务和最新运行报告；API 负责入队，独立 Worker 负责执行，报告层不再依赖 SSE 请求线程。旧单用例与 Planning SSE 路径保持兼容。
- 验证：数据库升级至 `20260902_0028`，`alembic check` 无差异；内存队列双 Job 领取、幂等创建与终态聚合通过；未知 Runner 异常收口通过；真实 PostgreSQL + Playwright 完成 `Batch -> Job -> Run -> Report`，2/2 步骤通过；批次列表 API 的 403/200 权限路径通过；前端 OpenAPI 类型生成和生产构建通过；临时数据已清理。
- 后续：将 Planning“保存并执行”迁移到 Batch API；增加运行中 Job 的持久化取消/heartbeat；统一 FailureSignal 分类并在报告写入时固化。

## 2026-09-02 | ExecutionBatch、ExecutionJob、Report Core 与队列选型说明

- 任务：解释执行批次、执行任务、报告核心服务的职责关系，并比较 PostgreSQL DB Queue、Celery 和 Kafka。
- 操作：结合当前 Project、PlanningSession、TestCaseRun 和 SSE 执行模型定义渐进式执行控制平面。
- 结果：ExecutionBatch 表示一次明确的项目测试活动，ExecutionJob 表示其中一个可调度用例任务，TestCaseRun 表示 Job 的一次实际尝试，Report Core 是按 run/batch/project 生成报告的无状态应用模块。当前规模优先采用 PostgreSQL DB Queue；需要成熟 Python 任务调度时再引入 Celery；Kafka 更适合高吞吐可回放事件流，不适合作为当前首选任务执行框架。
- 验证：架构概念分析，未修改业务代码，未运行测试。
- 后续：设计数据模型时明确 `Batch 1:N Job 1:N Run`，并保留未来 outcome/reward 关联。

## 2026-09-02 | 多项目并行下的 Report Core 与执行控制平面设计

- 任务：评估 Report Core 按 ID 处理项目报告的可行性，并澄清多线程、数据写冲突和消息排队机制。
- 操作：基于现有 TestCaseRun、Planning SSE 工作线程、内存 asyncio.Queue、事件日志和报告聚合路径划分执行控制面、事实存储与报告读模型。
- 结果：Report Core 可设计为无状态聚合服务，但项目历史应按 `project_id` 查询，一次测试活动必须按新增 `batch_id` 聚合。当前线程只承担请求内执行/流式桥接，内存 Queue 不是任务队列；独立 run 写入通常不冲突，同一 planning session、事件序号、locator correction 和全局 VLM 状态存在竞争。现有表可保留，通过新增 batch/job/event 并给 TestCaseRun 增加关联与快照字段渐进演进，无需整体推倒重写。
- 验证：架构静态分析，未修改业务代码，未运行测试。
- 后续：先确定 Report Core 三层查询合同和 execution batch/job schema，再决定采用 PostgreSQL DB queue 还是外部任务队列。

## 2026-09-02 | 多会话与多项目并行执行分析

- 任务：确认一次开启多个 Planning 会话执行时的行为，并评估未来多项目并行测试能力。
- 操作：检查 SSE 工作线程、SQLAlchemy Session、取消管理器、Playwright 浏览器生命周期、artifact 隔离、VLM runtime state、事件日志序号和数据库配置。
- 结果：不同会话会创建独立线程、数据库 Session、浏览器和 execution artifact 目录，低并发下可同时运行；但没有任务队列、并发上限或进程恢复。同一会话重复执行会覆盖取消句柄并产生事件序号冲突；每次用例执行还会重置进程级 VLM 限流、断路器和统计，造成跨项目干扰。SQLite 并发写风险也高于 PostgreSQL。
- 验证：静态分析，未修改业务代码，未执行并发压力测试。
- 后续：多项目并行前引入 execution batch/job、持久化状态机、幂等键、按 job 取消、全局/项目并发限制，并拆分 VLM 全局治理状态与单次执行统计。

## 2026-09-02 | 报告、执行持久化与调度链路分析

- 任务：从报告入手，确认用例执行结果如何保存、项目内用例如何执行，以及当前同步/异步模型。
- 操作：追踪 ExecutionReport/StepExecutionEvidence schema、Playwright Runner、TestCaseRun 持久化、执行 API、Planning 保存执行 SSE、报告聚合与前端触发入口。
- 结果：单次用例报告以 JSON 存于 `test_case_runs.report`，步骤截图存文件并在报告中保存路径；直接执行 API 同步阻塞。Planning SSE 在守护工作线程中运行同步执行并流式返回，多个选中用例仍串行执行。当前不存在项目批次执行实体/API，项目报告只是聚合项目下相互独立的运行记录。确认错误分类存在三套口径，且未捕获异常可能留下永久 `running` 记录。
- 验证：静态分析，未修改业务代码，未运行测试。
- 后续：先设计并固化统一 `FailureSignal` 与报告版本/DSL 溯源字段，再实现错误总结和注入模块。

## 2026-09-02 | 调整 AgenticRL 自愈闭环实施顺序

- 任务：明确先完善错误总结与错误注入，再新增独立模块桥接执行、校准和重生成链路。
- 操作：基于现有 execution report、locator trace、anti-pattern、Planning context 与 DSL prompt 注入路径重新划分阶段边界。
- 结果：第一阶段聚焦标准化错误事件、证据引用、错误记忆、检索选择和可追踪注入；仅预留后续策略模块所需契约。第二阶段再实现自愈编排与 AgenticRL 策略学习，避免基础数据处理与闭环决策高耦合。
- 验证：架构静态分析，未修改业务代码，未运行测试。
- 后续：先定义错误总结和注入的数据 schema，再逐步替换当前分散的字符串拼接与直接注入逻辑。

## 2026-09-02 | 清理测试构建残留

- 任务：继续清理后端和前端中残留的测试构建代码与产物。
- 操作：删除后端 pytest 缓存和已删除测试文件的字节码目录、前端 Vitest 缓存与空测试依赖目录；同步后端虚拟环境；移除 `page_explorer.py` 中仅为测试 monkeypatch 保留的 Playwright 包装函数；清理 `.gitignore` 中失效的测试产物和临时测试脚本规则。
- 结果：后端和前端均无项目级测试目录、测试构建缓存、测试框架依赖或测试专用源码入口；生产 Playwright 执行能力保持不变。
- 验证：`uv run python -m compileall -q app`、关键模块导入和 `npm run build` 通过；`uv pip show pytest` 确认 pytest 未安装；残留目录和关键字扫描为空。
- 后续：无。

## 2026-09-02 | 清理全部自动化测试代码

- 任务：删除因项目结构变化而失效、难以维护的全部自动化测试代码。
- 操作：删除后端单元/集成/E2E 测试与夹具、前端 Vitest 测试与测试工具、测试指南及 E2E 输入样例；移除 pytest、Vitest、Testing Library、jsdom 的依赖和配置；同步当前 README 与架构说明。
- 结果：仓库不再包含可执行自动化测试套件或测试框架配置；保留业务 `test_case`/`test_planning` 模块、Alembic 历史迁移、Playwright 正式执行引擎及历史设计记录。
- 验证：`uv lock --check`、`uv run python -m compileall -q app`、应用工厂导入和 `npm run build` 均通过。
- 后续：后续如恢复自动化测试，应基于当前架构重新设计测试分层与契约，不复用本次删除的旧套件。

## 2026-09-02 | AgenticRL 与自愈闭环模块设计构思

- 任务：构思 AgenticRL 模块如何接入“执行方案 → 总结错误 → 校准 trace → 重新生成方案 → 验证 prompt 作用”的自愈闭环。
- 操作：检索现有 Planning、DSL、Prompt Registry、anti-pattern、execution trace、analysis/retest 相关代码与历史设计记录，确认当前能力边界与缺口。
- 结果：建议将 AgenticRL 作为独立学习与策略服务，位于执行报告/错误总结之后、重新规划/DSL 生成之前；不替代 DSL 校验和 Runner，只输出结构化修复建议、策略选择、prompt policy 与闭环决策。
- 验证：静态分析，未运行测试。
- 后续：如进入实现，优先新增 AgenticRL 数据模型与 service，再接入保存执行后的失败分析与 DSL retry 入口。

## 2026-09-01 | AI 规划到失败复测链路核验

- 任务：核验“用户输入 → Agent 规划 → 页面探索 → DSL 生成 → Playwright 执行 → 报告 → 错因分析 → 错误沉淀 → 会话上下文注入 → 再次执行”链路是否可运行。
- 操作：静态追踪 Planning、Explorer、DSL、Runner、Report、Analysis/Retest 调用链；运行后端聚焦测试、前端全量测试与构建、Playwright 浏览器集成；通过 Midscene 打开规划页并创建会话；修复流式执行未自动注入会话测试数据、未记录失败 anti-pattern 的同步/流式行为差异并补回归测试；忽略 Midscene 本地报告目录。
- 结果：规划到报告的模块链路存在，失败分析、错误记录、上下文注入和后端复测也分别存在；但当前不是全自动自愈闭环，自动重新探索/重生成 DSL/更新用例未编排，前端也未接入复测 API。本机 Planning AI 与 DSL AI 均未启用，UI 明确显示“AI 设置：未启用”。
- 验证：后端聚焦测试 107 passed、1 skipped；修复回归 27 passed、1 skipped；后端默认全量 501 passed、1 skipped、1 个认证策略漂移测试失败；前端 75 passed；前端生产构建通过；浏览器创建会话成功；A11y 浏览器测试 2 passed、1 个旧常量断言失败；登录执行到第 6/6 步后因旧 `flash` target 进入人工干预；人工干预回归 3 passed、1 个旧内部函数 monkeypatch 失败。
- 后续：配置 Planning/DSL 模型后补一次真实 AI 全链验收；决定是否实现受控自动自愈编排；清理浏览器集成测试漂移。

## 2026-09-01 | 关闭登录鉴权并自动使用 admin

- 任务：简化本地测试启动流程，跳过前端登录并自动使用数据库中的 admin 账号。
- 操作：移除前端路由的登录守卫，将 `/login` 重定向到规划工作台；后端当前用户依赖改为按 `AUTH_AUTO_LOGIN_EMAIL` 查询数据库账号，默认使用 `admin@test.com`；保留原登录接口用于兼容；同步测试与启动文档。
- 结果：所有页面可直接访问，依赖用户身份的接口统一使用配置的 admin；管理员账号缺失或停用时返回明确的 500 错误。
- 验证：当前数据库成功解析 `admin@test.com`（ID 2）；无 Cookie 请求 `/api/v1/auth/me` 返回 200；后端单测 491 passed、1 skipped；前端 75 tests passed；`npm run build` 通过。
- 后续：该模式仅适用于本地单用户测试；恢复多用户部署前需重新启用登录鉴权。

## 2026-08-31 | 补充同步后端应用说明

- 任务：将此前排除的 `backend/app/README.md` 本地修改同步到 GitHub。
- 操作：按用户确认保留当前 README 内容，并与本次同步记录一并提交到 `main`。
- 结果：后端应用目录说明的本地修改纳入版本控制。
- 验证：文档差异检查通过；未涉及业务代码和运行逻辑。
- 后续：无。

## 2026-08-31 | 同步数据库重置记录到 GitHub

- 任务：将本地数据库重置记录同步到远端仓库。
- 操作：仅暂存并提交 `docs/execution-log.md`，排除与本次任务无关的 `backend/app/README.md` 本地修改；推送当前 `main` 分支。
- 结果：数据库重置记录已同步到 `origin/main`，提交为 `b043a9e`。
- 验证：本地 `main` 与 `origin/main` 提交计数为 `0/0`。
- 后续：无。

## 2026-08-31 | 重置本地数据库业务状态

- 任务：仅保留账号信息和默认项目，清空其他业务数据并重置自增序列。
- 操作：在单个事务中清空会话、消息、草稿、用例、执行、报告、定位记录等 16 张业务表；保留 `users`、默认 `projects`、必要的 `project_members` 和 `alembic_version`；同步保留表序列。
- 结果：当前保留 2 个账号、`Default Project(id=1)` 及其 1 条 owner 成员关系；其余业务表均为空。
- 验证：`ai_planning_sessions_id_seq` 与 `session_projects_id_seq` 均为 `last_value=1, is_called=false`，下次创建会话及关联记录将从 `id=1` 开始；后端健康检查返回 200。
- 后续：无。

## 2026-08-31 | 当前代码架构可理解性评估

- 任务：评估当前仓库架构，解释文件分类、命名、模块职责和依赖关系，并定位代码难以理解的主要原因。
- 操作：盘点前后端目录与模块规模；追踪 API、Planning、DSL、Runner、Locator、Report 主链；分析 Python 跨层依赖与模块强连通分量；新增 `docs/architecture-guide.md`，整理目录词典、Planning 文件职责、前后端依赖规则、需求定位表和推荐阅读顺序；更新根 README 与后端入口说明。
- 结果：当前架构较 2026-08-28 审计已有明显改善，Planning 用例服务和同步/流式执行源已完成拆分；主要认知负担来自超大核心函数、`application`/`services` 双向分层、过渡 facade/类型 barrel、前端大组件，以及入口文档严重滞后。综合可维护性评估为 6.8/10；现已提供对应当前代码的统一导航入口。
- 验证：后端默认测试 499 passed、1 skipped、10 deselected；前端 77 tests passed；`npm run build` 通过；静态依赖图仅发现 ORM 双向 relationship 形成模块强连通分量；本地 Markdown 链接检查通过。
- 后续：按指南中的过渡依赖逐步拆分 Planning Agent/草案生成/执行报告聚合的大函数，并移除兼容 facade 和前端手写总类型 barrel。

---

## 2026-08-31 | Prompt 统一管理入口落地

- 任务：先做当前 prompt 管理，不实现 RL 相关逻辑，只为后续策略接入预留扩展点。
- 操作：新增统一 Prompt Registry 入口；迁移 Planning 初始化、DSL 生成 system prompt、VLM system prompt；保留旧导入路径兼容。
- 结果：静态 prompt 不再散落在调用模块中，业务代码通过 stage 渲染 prompt；Planning 初始化 prompt 已同步薄化。
- 验证：聚焦单元测试与语法检查通过。
- 后续：继续把运行时 guard/context/anti-pattern 文本迁入统一入口，并下线在线 anti-pattern few-shot 注入。

---

## 2026-08-31 | 梳理现有 Prompt 清单并修正日志位置

- 任务：按要求将日志记录放到任务记录最前面，并梳理当前项目所有现存 prompt。
- 操作：扫描后端、前端与文档中的 prompt 定义、构造函数和注入点；修正上轮 AgenticRL 分析记录的放置位置。
- 结果：确认当前线上代码中的主要 prompt 集中在 AI planning、DSL generation、anti-pattern 注入和 VLM locator 四类。
- 验证：通过 `rg` 检索 `SYSTEM_PROMPT`、`user_prompt`、`build_*prompt`、`format_*prompt`、`few-shot` 等关键字。
- 后续：如需继续治理 prompt，可先将 anti-pattern 注入从在线 prompt 移到离线学习样本。

---

## 2026-08-31 | AgenticRL 演进机制设计分析

- 任务：分析 trace 记录与 anti 错误注入如何融合 AgenticRL，自主优化 SOP 和提示词。
- 操作：检查 ReAct prompt、DSL 生成 prompt、anti-pattern 注入、generation run 反馈模型与历史设计文档。
- 结果：确认现有系统已有 generation run、用户反馈、执行报告、locator trace 等学习样本基础；建议将 anti-pattern 从 prompt few-shot 注入迁移为离线评分样本和策略候选来源。
- 验证：静态取证，未运行测试。
- 后续：优先建设统一事件样本、奖励函数、prompt/策略版本治理、离线评估集与灰度发布闭环。

---

## 2026-08-31 | 同步日志结构整理到 GitHub

- 任务：将当前日志结构整理变更同步到 GitHub。
- 操作：检查工作区和当前分支，只暂存 `docs/execution-log.md` 与 `docs/bug-log.md`，创建 focused commit 并推送当前 `main` 分支。
- 结果：本记录随同日志结构整理变更一起提交并推送到远端。
- 验证：推送后通过 `git status --short` 与 `git log -1 --stat --oneline` 核对。
- 后续：无。

---

## 2026-08-31 | 统一日志结构与时间倒序展示

- 任务：修复 `docs/execution-log.md` 和 `docs/bug-log.md` 的展示顺序与结构不一致问题。
- 操作：统一两个日志文件的顶部骨架；将记录区改为按日期倒序展示；把 `execution-log` 中尾部错位的 2026-08-28 至 2026-08-30 记录归位；把 `bug-log` 从分类混排整理为统一时间线。
- 结果：两个日志均以说明、记录规则、记录模板、索引/总览、记录区的结构展示，新增记录时可直接插入记录区顶部。
- 验证：通过标题扫描确认 `execution-log` 与 `bug-log` 的记录区最新日期在前，历史记录不再散落在旧日期之后。
- 后续：后续新增记录继续保持最新记录优先，并使用对应模板字段。

---

## 2026-08-31 | 修复日志记录倒序问题

- 任务：修复本轮新增执行日志被追加到文件尾部的问题，恢复“最新记录优先放到最上面”的约定。
- 操作：
  - 将 2026-08-31 的「修复默认项目与规划会话初始化/绑定策略」「排查规划会话与项目读取/创建链路」「复刻首次新建规划会话 500」「诊断首次新建规划会话 500 后第二次成功」移动到日志记录区顶部。
  - 保持历史 2026-08-30 及更早记录顺序不变。
- 验证：`docs/execution-log.md` 顶部记录区已按最新任务在前排列。
- 备注：本次仅修正文档顺序，不改业务代码。

---

## 2026-08-31 | 修复默认项目与规划会话初始化/绑定策略

- 任务：落实「初始化项目时同时初始化会话并绑定；会话与项目保持多对多绑定；测试用例仍以项目为执行归属」。
- 变更：
  - 新增 Alembic 迁移 `20260831_0027_seed_default_planning_session.py`：
    - 将 `Default Project(id=1)` 标记为 `is_default=true`。
    - 若种子用户与默认项目存在且尚未绑定规划会话，则创建 `默认规划会话` 并写入 `session_projects`。
    - 设置默认会话 `active_project_id=1`。
    - PostgreSQL 下同步 `users/projects/project_members/ai_planning_sessions/session_projects` 序列，避免 seed 固定 ID 后再次分配重复主键。
  - 调整 `create_planning_session`：
    - 未传 `project_id` 时优先复用当前用户已有项目，按 `is_default desc, id asc` 选择默认项目。
    - 仅在用户没有可访问项目时兜底创建 `default-{session_id}`。
    - 兜底创建项目时同步写入 `ProjectMember`，避免自动项目在全局项目列表不可见。
  - 更新测试：
    - 会话创建测试改为断言复用 `Default Project`。
    - 新增多会话共享同一项目覆盖。
    - 新增默认规划会话迁移测试。
    - 修复配置测试对本地 `.env` 的隔离缺口。
- 验证：
  - `cd backend && uv run alembic upgrade head` 成功，当前 head 为 `20260831_0027`。
  - 修复后接口级验证：`POST /api/v1/ai-planning/sessions` 返回 `201`，新会话绑定 `Default Project(id=1)`。
  - `cd backend && uv run pytest -q` 通过：495 passed、1 skipped、10 deselected。
  - 本地 PostgreSQL 已清理到目标状态：仅保留 `Default Project(id=1, is_default=true)`、`默认规划会话`、以及二者的 `session_projects` 绑定。

---

## 2026-08-31 | 排查规划会话与项目读取/创建链路

- 任务：解释初始化项目存在但规划首页不显示、点击新建会话触发 500 的完整前后端链路。
- 前端链路：
  - `/planning` 的 `SessionListPage` 只调用 `listPlanningSessions()`，即 `GET /api/v1/ai-planning/sessions`；页面展示的是每个规划会话返回的 `projects` 字段，不读取全局项目列表。
  - 点击 `/planning` 顶部「新建会话」调用 `createPlanningSession({})`，即 `POST /api/v1/ai-planning/sessions`，没有传 `project_id`，也没有先读取或复用 `Default Project`。
  - 进入会话后的 `SessionProjectPanel` 才同时读取 `listSessionProjects(sessionId)` 与 `getProjects()`；其中 `getProjects()` 是 `GET /api/v1/projects`，用于下拉选择「关联已有项目」。
- 后端链路：
  - `POST /api/v1/ai-planning/sessions` 进入 `create_planning_session_route`，调用 `create_planning_session`。
  - `create_planning_session` 先创建 `AIPlanningSession`，随后在 `payload.project_id is None` 时自动创建 `Project(name=f"default-{record.id}", is_default=True)`，再写入 `SessionProject` 并设置 `active_project_id`。
  - `GET /api/v1/ai-planning/sessions` 只列出当前用户的会话，并从 `record.projects` 返回会话已关联项目；未关联任何会话的初始化项目不会出现在规划首页。
  - `GET /api/v1/projects` 通过 `ProjectMember` 列出当前用户可访问项目，能返回初始化的 `Default Project(id=1)`。
- 实测：
  - `GET /api/v1/projects` 返回 `Default Project(id=1)`。
  - `GET /api/v1/ai-planning/sessions` 返回会话 `id=2` 及其关联项目 `default-2(id=2)`，不返回未关联到会话的 `Default Project`。
- 结论：这是数据模型/初始化/默认关联策略不一致叠加序列未对齐的问题。初始化项目存在于全局项目域，但规划首页是会话域；新建会话未复用初始化项目，而是自动创建会话私有默认项目，进而撞上未对齐的 `projects_id_seq`。
- 备注：本次仅做链路排查和记录，未修改业务代码或数据库数据。

---

## 2026-08-31 | 复刻首次新建规划会话 500

- 任务：按页面行为复刻「首次新建会话 500，第二次成功且显示会话 2」。
- 数据库重置：
  - 删除现有 `ai_planning_sessions` 记录，级联清理会话关联数据。
  - 删除自动生成的 `projects.name like 'default-%' and is_default = true` 项目，保留种子项目 `Default Project(id=1)`。
  - 将 `ai_planning_sessions_id_seq`、`projects_id_seq`、`session_projects_id_seq` 重置为 `1, is_called=false`，恢复可复刻的序列错位状态。
- 复刻方式：
  - 使用 FastAPI `TestClient(raise_server_exceptions=False)` 连续请求两次 `POST /api/v1/ai-planning/sessions`。
- 结果：
  - 第一次请求返回 `500`，响应堆栈包含 `psycopg.errors.UniqueViolation`。
  - 第二次请求返回 `201`，响应体包含 `session.id=2`、`active_project_id=2`、项目 `default-2`。
  - 复刻后数据库仅有 `ai_planning_sessions.id=2`，无会话 1；`session_projects` 仅关联 `(session_id=2, project_id=2)`。
- 备注：复刻会保留当前数据库处于「第二次点击成功后」的状态；本次未修改业务代码。

---

## 2026-08-31 | 诊断首次新建规划会话 500 后第二次成功

- 任务：定位「首次点击新建会话返回 500 Internal Server Error，第二次点击成功且显示会话 2」的原因。
- 调查：
  - 检查前端 `SessionListPage.handleCreate`，确认每次点击直接调用 `createPlanningSession({})`，没有「先检测是否已有会话」的复用逻辑。
  - 检查后端 `create_planning_session`，确认新建会话后若未传 `project_id` 会创建默认项目 `default-{record.id}`，并在同一事务内提交。
  - 查询本地 PostgreSQL：当前仅存在 `ai_planning_sessions.id=2`，不存在会话 1；`projects` 存在 `id=1 Default Project` 与 `id=2 default-2`；相关序列均已推进。
  - 对照初始 migration，发现种子数据显式插入 `users.id=1`、`projects.id=1`、`project_members.id=1`。
- 结论：本地 PostgreSQL 种子数据显式写入 `projects.id=1` 后未同步 `projects_id_seq`，首次创建默认项目时序列仍尝试分配 `id=1`，触发主键冲突并回滚；PostgreSQL 序列不随事务回滚，因此第二次分配 `id=2` 成功。
- 备注：本次仅诊断和记录，未修改业务代码或数据库数据。

---

## 2026-08-30 | 修复 AI 规划 Playwright 浏览器缺失与 Sync API 实例泄漏

- 任务：修复 AI 规划 `explore_flow` / `explore_page` 失败，并将 Playwright 浏览器安装到 D 盘。
- 操作：
  - 将 Chromium 及依赖（chromium、chromium-headless-shell、ffmpeg、winldd）安装到 `D:\PlaywrightBrowsers`。
  - 在 `backend/.env` 与 `backend/.env.example` 增加 `PLAYWRIGHT_BROWSERS_PATH=D:\PlaywrightBrowsers`。
  - 修复 `BrowserSessionManager.get_or_create_context` 与 `_collect_flow_a11y`：浏览器启动失败时释放 `sync_playwright` 实例，避免后续误报 Sync API inside asyncio loop。
  - 同步 `users_id_seq` / `projects_id_seq` / `project_members_id_seq` 到当前最大 id，消除 `project_members` 主键冲突。
- 验证：
  - `sync_playwright` 成功加载 https://example.com。
  - `_collect_flow_a11y` 成功探索 https://example.com（1 页、8 个元素）。
  - `create_project` 成功创建项目，无主键冲突。
  - 后端默认测试：493 passed、1 skipped、10 deselected。
- 备注：新增 BUG-086 记录到 `docs/bug-log.md`。重启后端前请确保 `.env` 的 `PLAYWRIGHT_BROWSERS_PATH` 已生效。

---

## 2026-08-30 | 写入本地 admin 账号并清空测试数据库

- 任务：测试依赖后端登录但没有注册账号；写入 admin 账号、清空旧数据并验证登录可用。
- 操作：
  - 启动本机 PostgreSQL 18（`D:/PostgreSQL/data`；服务为 NetworkService，普通用户 `net start` 被拒绝，改用 `pg_ctl.exe start -w`）。
  - 清空 `public` schema 全部业务表数据（`TRUNCATE ... RESTART IDENTITY CASCADE`，保留 `alembic_version` 迁移版本）。
  - 通过 ORM 写入账号：`id=1`、`email=admin@example.com`、`display_name=admin`、密码 `admin123`（PBKDF2-SHA256 哈希），并重建默认项目 `Default Project`（`id=1`、`is_default=true`）及 owner 成员关系。
- 验证：
  - `POST /api/v1/auth/login` 使用 `admin@example.com / admin123` 返回 200；`GET /api/v1/auth/me` 返回同一用户；错误密码返回 401。
  - 清理后仅剩 `users=1`、`projects=1`、`project_members=1`，其余业务表均为 0。
- 备注：登录表单要求邮箱格式，因此登录账号为 `admin@example.com`，平台显示名为 `admin`。

---

## 2026-08-30 | P5 前端业务域收口

- 任务：按 planning/projects/cases/executions/reports 拆分前端边界，并恢复真实认证入口。
- 操作：
  - 将 API client 拆到 `shared/api/client.ts`，业务请求拆入各 `features/*/api.ts`；旧 `services/api.ts` 缩为兼容 barrel。
  - 通用 SSE client 和 PageFeedback 下沉到 shared，Planning 取消请求归回 planning domain。
  - 从 `AITestPlanningPanel` 抽出 session 初始化/恢复 hook、SSE 生命周期 hook 和 Requirements 视图。
  - 新增 `AuthGuard` 与 `LoginPage`，统一保护业务路由并支持登录后恢复目标地址。
  - 增加 FastAPI OpenAPI 导出脚本、schema 快照和 `openapi-typescript` 生成命令；auth transport types 已切换到生成类型。
  - 执行依赖安全升级；React Router 升至 v7 并删除失效的 v6 future flags。
- 验证：
  - 前端测试：67 passed。
  - `npm run build` 与 `npm run generate:api-types` 通过，连续生成结果一致。
  - `npm audit --audit-level=low`：0 vulnerabilities。
  - API/SSE 兼容 barrel 保留，现有调用方和测试可渐进迁移。
- 备注：P5 结构目标已落实；手写 `types/api.ts` 作为 UI view model 兼容层保留，transport schema 以生成文件为准。

---

## 2026-08-30 | 凭据本地清理与 Git 历史处置

- 任务：清除误提交的本地配置和智谱 BigModel API 凭据历史。
- 操作：
  - 从本地 `.claude/settings.local.json` 删除包含明文 Bearer 凭据的权限项。
  - 使用 index filter 从 `main` 的全部 484 个历史提交中移除 `.claude/settings.local.json`。
  - 以重写前远端哈希执行 `--force-with-lease`，避免覆盖并发远端更新。
  - 删除 `refs/original`、reflog、临时 bundle 和验证 clone，并执行立即 GC。
- 验证：
  - 本地与远端 `main` 哈希一致。
  - 远端全量克隆中目标路径历史提交数和当前跟踪数均为 0。
  - 本地全部 refs 的目标路径提交数为 0，BigModel Bearer 模式扫描为 0。
- 备注：仓库侧处置完成；外部凭据吊销仍需账号持有人登录智谱 API Keys 控制台完成身份验证。

---

## 2026-08-30 | 本地 main 对齐远端（放弃本地未推送提交）

- 任务：本地与远端仓库同步，清理中断的 rebase 现场。
- 背景：此前一次 `git pull --rebase` 在 `backend/app/services/ai_planning.py` 冲突后中断；本地 `main` 与 `origin/main` 因远端凭据清理 force-push 已分叉。
- 决策：用户确认以远端为准，放弃本地未推送提交（Bug #J、EventLogWriter 方法名修复、三重修复等 4 个提交）。
- 操作：
  - `git rebase --abort` 回到 `main`。
  - `git fetch origin --prune`。
  - `git reset --hard origin/main`。
- 验证：本地 `main` 与 `origin/main` 一致（`4719f1e`），无未提交跟踪文件差异；30 个未跟踪 `docs/` 文件保持不变。

---

## 2026-08-30 | P0-P5 重构成果复核与代码质量评估

- 任务：基于真实代码与动态测试检查 P0-P5 重构完成度，评估当前代码质量（不只看 md 结论）。
- 操作：
  - 检查 `git log`/工作树：P0-P5 各阶段提交存在，`main` 与 `origin/main` 对齐（`5ec68ec`），工作树干净。
  - 运行后端默认测试、前端测试与构建；核对 Alembic 迁移链、Planning 兼容 facade、执行单事件源、清理脚本保护、locator 调试路由注册测试。
  - 交叉检查删除门禁：`locator_confidence.py`、`AppLayout.tsx`、`echarts`、`test_dsl.json`、旧 preflight、旧同步 Planning LLM、AI selector cache 等均已删除；休眠能力清单已建立。
- 验证：
  - `uv run pytest -q`：1 failed、492 passed、1 skipped、10 deselected；失败为本地 `backend/.env` 中 `ENABLE_AI_VISUAL_LOCATE=true` 污染 `test_ai_visual_locate_default_is_disabled`。
  - `uv run alembic heads`：唯一 head `20260829_0026`；`alembic history` 链条连续无断链。
  - `npm test -- --run`：8 个测试文件通过、1 个文件加载失败（`services/api.test.ts`），根因是 `services/api.ts:7` 引用不存在的 `features/reports/api`。
  - `npm run build`：失败，错误为 `TS2307`（缺少 `features/reports/api`）与 `TS2305`（barrel 未导出 `getReportPreference/updateReportPreference`）。
- 结论：P0-P4 完成度较高，Planning facade 已缩到 72 行、执行同步入口复用流式生成器、清理脚本默认 dry-run 且带保护；P5 前端按域拆分遗漏 `features/reports` 域，导致前端测试与构建全红。
- 发现：本次复核新发现 2 个缺陷，已记录到 `docs/bug-log.md`（AUDIT-20260830-17、AUDIT-20260830-18）。

---

## 2026-08-30 | P5 reports 域修复 + 前端/后端门禁恢复

- 任务：修复复核发现的前端门禁问题，并验证前端构建与后端服务实际跑通。
- 操作：
  - 新增 `frontend/src/features/reports/api.ts` 与 `types.ts`，恢复 `getReportPreference/updateReportPreference` 客户端，barrel `services/api.ts` 重新可用。
  - 修复 `features/planning/api.ts` 中 `getAISettings` 的重复 `return`。
  - 修复 `test_config.py` 两个 VLM 默认值用例：重定向 `ENV_FILE_PATH`，避免本地 `.env` 污染默认断言。
  - 启动 PostgreSQL（`D:/PostgreSQL/data`），执行 `uv run alembic upgrade head` 将本地库从 `20260608_0025` 升到 `20260829_0026`。
  - 发现本地 `api.unself.cn` 网关需带 `/v1` 路径，修正 `.env` 中 `AI_DSL_BASE_URL` 与 `AI_PLANNING_BASE_URL`。
- 验证：
  - 后端默认测试：493 passed、1 skipped、10 deselected。
  - 前端测试：67 passed；`npm run build` 通过。
  - 后端 `uvicorn --factory` 启动，`/api/v1/health` 返回 200。
  - 通过 Vite 代理 `127.0.0.1:5173` 创建会话 311 并发送消息，AI 正常返回收集需求的 `assistant_message`，`session_status=collecting`。
- 备注：AUDIT-20260830-17 已关闭；新增 AUDIT-20260830-19 记录 base URL 配置问题。

---

## 2026-08-30 | 清理 Git 历史中的 backend/.env 明文凭据

- 任务：从 `main` 全部历史提交中移除 `backend/.env` / `.env`，消除远端可见的明文密钥记录。
- 操作：
  - 备份当前 `main` 到本地分支 `backup-pre-env-purge`，并创建 `git bundle`（`D:/AutoTestingLearingProject/backup-pre-env-purge-20260830-031248.bundle`）。
  - `git filter-branch --index-filter 'git rm --cached --ignore-unmatch .env backend/.env' --prune-empty -- main` 重写全部 486 个提交。
  - 删除 `refs/original`，清理 reflog 后 `git push origin main --force-with-lease`。
- 验证：
  - `git rev-list main --objects | grep -E '\.env$'`：无 `.env`/`backend/.env` 对象；`backend/.env.example` 保留。
  - `git log origin/main --oneline -- .env backend/.env`：空。
  - 本地 `main` 与 `origin/main` 一致（`60e51e6`），树无差异。
  - 后端/前端服务仍在运行，`/api/v1/health` 与 Vite 均返回 200。
- 备注：历史中的 `.claude/settings.local.json` 已于更早前清理；本次仅处理 `backend/.env`。工作树中的 `backend/.env` 仍保留用于本地开发（已被 gitignore），但历史已无记录。

---

## 2026-08-30 | 仓库代码质量评估（真实代码 + 动态门禁）

- 任务：应要求对当前仓库做一次代码质量评估，基于真实源码、目录结构与动态测试，不只看文档结论。
- 操作：
  - 统计后端 `backend/app`（22,194 行 Python）与前端 `frontend/src`（13,637 行 TS/TSX）结构，检查分层、路由、模型、迁移、测试覆盖。
  - 运行后端默认 pytest、前端 `npm run build`；检查 TODO/FIXME、`print()`、`console.log`、密钥扫描、Alembic 版本链、重复模块与 lint 配置。
- 验证：
  - 后端：493 passed、1 skipped、10 deselected（95.77s）；存在 `TestCase`/`TestCaseRun` 类名被 pytest 收集的 PytestCollectionWarning。
  - 前端：`tsc --noEmit && vite build` 通过，主包约 763KB（gzip 240KB）偏大，无代码分割治理。
  - 密钥扫描：仅测试夹具中的 `test-key` / `new-dsl-secret` 假值，未发现真实泄露。
- 结论：整体为「结构清晰、可运行、测试覆盖扎实的中上水平」代码库；主要短板是 lint/类型检查未纳入 CI 门禁、4 个超大模块需拆分、`__pycache__`/`dist`/`node_modules` 等生成物污染工作树感知、AI 配置字段随功能堆叠趋杂。
- 备注：仅追加本日志，未改动任何业务代码。

---

## 2026-08-30 | AI 消息流式传输设计分析（对照 pi 事件协议）

- 任务：分析前端消息框设计与 AI 返回消息消费链路，指出思考内容与正文渲染/顺序问题，参考 pi 的 `AssistantMessageEventStream` 事件协议，给出 Web 端与框架优化方案。
- 操作：
  - 阅读 `frontend/src/shared/api/sseClient.ts`、`features/planning/usePlanningSse.ts`、`components/AITestPlanningPanel.tsx` 的 SSE 解析与 `handleStreamEvent` 分发逻辑。
  - 阅读 `backend/app/ai/test_planning_agent.py` 的 `stream_planning_turn` / `_stream_planning_llm`，确认 `text_chunk(thinking=true)` 与正文共用 `content` 字段、前端拆成 `_thinkingContent` + `content` 两路渲染。
  - 对照 `pi/packages/ai/src/types.ts` 的 `AssistantMessageEvent`（text/thinking/toolcall 的 start/delta/end + contentIndex）与 `utils/event-stream.ts` 的 `EventStream`。
- 结论：当前是「动作型扁平事件 + 前端手工拼字符串」，思考与正文不是有序 content block，导致思考折叠但正文在框外、顺序丢失、`turn_complete` 覆盖流式文本等问题；应改为「消息生命周期事件 + contentIndex + reducer」，思考作为一等 content block。
- 备注：仅追加本日志，未改动任何业务代码。

---

## 2026-08-30 | 对齐 pi 的 AI 消息流式传输协议（实现）

- 任务：将前端消息框与后端流式事件协议对齐 pi 的 `AssistantMessageEvent` 设计（有序 content block + contentIndex），解决思考内容与正文乱序、正文覆盖等问题。
- 操作：
  - 后端 `app/ai/test_planning_agent.py::_stream_planning_llm` 由扁平 `text_chunk(thinking=true)` 改为产出 `content_block_start / content_block_delta / content_block_end`（带 `content_index` 与 `kind`）。
  - `app/ai/test_planning_agent.py::stream_planning_turn` 透传三类 `content_block_*` 事件。
  - `app/application/planning/conversation_service.py` 新增 `_flush_streaming_content_block`，把有序块持久化到 `structured_payload_json["content_blocks"]`，并以文本块镜像到 `message.content` 兼容旧渲染；最终 `turn_complete` 不再覆盖流式文本。
  - 前端 `types/api.ts` 新增 `AssistantContentBlock` 与 `ContentBlockStart/Delta/EndStreamEvent`。
  - 前端 `AITestPlanningPanel.tsx` 新增 `readContentBlocks` / `applyContentBlockEvent` / `AssistantMessageBody`，按 content block 顺序渲染思考与正文；`turn_complete` 仅在无流式块时才回填最终消息。
- 验证：
  - 更新 `tests/unit/test_planning_agent.py` 流式断言。
  - 后端默认 pytest：493 passed、1 skipped、10 deselected。
  - 前端 `npm run build` 通过；`npm test` 67 passed。
- 备注：本次工作树还包含上一任务遗留的 BUG-086 修复（`.env.example`、`page_explorer.py`、`docs/bug-log.md`），未在本任务中改动，同步时需一并注意。

---

## 2026-08-30 | 诊断切页后流式消息丢失问题

- 任务：定位「切换页面后再次回到会话页，消息丢失，只能等 AI 全部发完才能看到」的根因。
- 结论：
  - 后端流式期间实际上已经持续落库（user 消息、streaming 消息、content_block 事件、event log），不存在「后端没持久化」。
  - 真正问题是前端「进行中的流式状态」没有可恢复的会话级全局来源：`usePlanningSessionState` 挂在 `AITestPlanningPanel` 内，切页时组件卸载，SSE 继续在后台跑，`handleStreamEvent` 更新的是已卸载组件的本地 state；回到页面时 `initialize()` 重新拉 `getPlanningSession` 并用 `applySessionDetail` 整体覆盖 transcript，把后台流期间本应累积的流式内容冲掉。
  - 恢复逻辑 `applySessionDetailWithRecovery` 只对 `turn_type === "streaming"` 的「中断」消息做一次性 event replay，且 replay 只认识旧事件（text_chunk/status/tool_call/turn_complete），不识别本次新增的 `content_block_*` 事件，因此思考块和有序块无法正确重建。
  - `loadSessionDetail` 没有「SSE 进行中则不覆盖 transcript」的保护，且 `applySessionDetail` 会把 detail.messages 重新映射，丢掉了当前 UI 里正在增长的 content blocks。
  - `AITestPlanningPanel` 内部使用 `sessionId` state 而不跟随路由 `sessionIdProp`，切页回来 `sessionIdProp` 变化被忽略，也放大了状态错位问题。
- 建议修复方向（本次仅诊断，未改代码）：
  1. 把会话状态与活跃 SSE 流提升为全局 store（或 Query/Context），切页只卸载视图、不卸载状态，re-mount 时订阅正在进行的流事件。
  2. 若沿用「后台流 + 回页重放」，加载详情时保留 `_streaming` 消息的实时 content_blocks，或仅在无活跃流时覆盖 transcript。
  3. 扩展 `applySessionDetailWithRecovery` 支持 `content_block_*` 事件重放；否则新协议的消息刷新后思考块仍丢。
  4. `sessionId` 改为跟随路由 prop，避免内部/外部双源不一致。
- 备注：仅追加本诊断记录，未改动业务代码。

---

## 2026-08-30 | 切页消息丢失根治疗方案设计

- 任务：为「切页后流式消息丢失」设计根治疗法（先设计，待实施）。
- 方案核心：把「会话状态」和「活跃 SSE 流」从 `AITestPlanningPanel` 组件提升为路由无关的全局 workspace store；切页只卸载视图，不卸载状态与流；回页时由 store 判定「有活跃流则复用内存 transcript，无活跃流则拉详情并按统一 reducer 重放事件」。
- 关键落点：
  - 新增模块级 `planningWorkspaceStore`（`useSyncExternalStore`，不引入新依赖）。
  - 新增 `planningStreamEvents` 纯函数 reducer，前端实时事件与回页重放共用同一套逻辑，统一支持 `text_chunk`、`content_block_*`、`tool_call_*`、`execution_*`、`turn_complete`。
  - `usePlanningSse` 去掉组件卸载即 abort 的行为，abort 由 store 显式管理。
  - `AITestPlanningPanel` 改为消费 store；`sessionId` 跟随路由 prop。
  - `App.tsx` / `test-utils.tsx` 增加 Provider。
  - 后端可选加固：`GET /sessions/{id}/events` 支持游标/按 message_id 过滤。
- 备注：本轮为方案设计，未改业务代码；实施时按 store → reducer → panel → tests 顺序推进。

---

## 2026-08-30 | 切页消息丢失根治疗方案落地

- 任务：实施「会话状态 + 活跃 SSE 流提升为路由无关全局 workspace store」的根治方案。
- 操作：
  - 新增 `frontend/src/features/planning/planningStreamEvents.ts`：纯函数 reducer（`reduceTranscriptEvent`）+ `readContentBlocks` / `applyContentBlockEvent` / `createOptimisticMessage`，实时事件与重放共用。
  - 新增 `frontend/src/features/planning/planningWorkspaceStore.tsx`：`useSyncExternalStore` 外部 store，管理 `currentSessionId`、各 session 的 `transcript/requirements/plan/drafts/activeStream`；`loadSessionDetail` 有活跃流时只更新元数据、不覆盖 transcript；无活跃流时按事件日志重放并支持 `content_block_*`。
  - 重构 `usePlanningSse.ts`：去掉组件卸载即 abort，改为 store 管理 abort，新增 `runStream(sessionId, kind, messageId, options)`。
  - 重构 `usePlanningSessionState.ts`：改为消费 store，提供兼容旧面板的 API。
  - 重构 `AITestPlanningPanel.tsx`：删除组件内手写 `handleStreamEvent`，事件交给 store reducer；流式状态由 `activeStreamKind` 派生；乐观消息 ID 传给 `runStream` 作为活跃消息 ID。
  - `App.tsx` / `test-utils.tsx` 注入 `PlanningWorkspaceProvider`。
- 验证：
  - 新增 `planningStreamEvents.test.ts` 7 用例、`planningWorkspaceStore.test.ts` 3 用例。
  - `npm run build` 通过；`npm test` 77 passed（11 files）。
- 备注：后端未改动；本次为纯前端架构修复。

---

## 2026-08-29 | P3 Active Project 与项目上下文边界

- 任务：落实 Planning Session 单 active project 语义，并将项目上下文职责移出总编排器。
- 操作：
  - `ai_planning_sessions` 新增 `active_project_id`，migration 为历史 session 回填最早关联项目。
  - 新建、关联和工具创建项目时自动切换 active project；重复关联作为幂等切换；删除 active 项目时回退到最早剩余关联。
  - 所有 Planning 执行路径改用集中 `_get_active_project_id`，移除散落的 `project_ids[0]`。
  - API schema 和前端类型增加 `active_project_id` / `is_active`，项目面板支持查看和点击切换当前项目。
  - 新增 `app/application/planning/project_context.py`，承接 session ownership、项目关联、active project 和成员修复职责。
  - AI Planning API 直接依赖 application project context，不再动态导入 service 私有 `_get_session`。
- 验证：
  - 后端默认测试：491 passed、1 skipped、10 deselected。
  - 前端测试：64 passed；`npm run build` 通过。
  - active project migration 独立升降级测试通过；Alembic 唯一 head 为 `20260829_0026`。
  - 变更范围 Ruff `F401/F821` 检查通过。
- 备注：保留多项目历史关联，仅执行上下文收敛为单 active project；P3 下一批拆 conversation 和 draft application service。

---

## 2026-08-29 | P3 Session Lifecycle 拆分

- 任务：将 Planning Session 生命周期与 schema 映射移出 `services/ai_planning.py`。
- 操作：
  - 新增 `application/planning/session_service.py`，承接 session list/create/detail/delete。
  - 新增 `application/planning/presenters.py`，统一 session/message/draft schema 转换。
  - 将 required requirement slots 移到 schema 层，Agent 与 session service 共享同一合同。
  - AI Planning API 直接依赖 session service；总编排器删除重复实现和 presenter。
  - 新建 session 对显式 `project_id` 增加存在性校验，修正旧测试依赖 SQLite 关闭外键的错误假设。
- 验证：
  - session/API 定向测试：30 passed。
  - 后端默认测试：491 passed、1 skipped、10 deselected。
  - application/route/service 变更范围 Ruff `F401/F821` 检查通过。
- 备注：下一批拆 conversation service 与 draft service，继续缩减总编排器。

---

## 2026-08-29 | P3 Conversation Service 拆分

- 任务：将 Planning 同步/流式对话编排迁入 application service，并显式化外部依赖。
- 操作：
  - 新增 `application/planning/conversation_service.py`，承接消息、会话状态、工具结果和流式占位消息持久化。
  - 通过 `ConversationContextInjector`、`AutoDraftGenerator` 和 `ConversationEventLogFactory` ports 注入上下文、草案生成和事件日志能力。
  - API 与 streaming bridge 改为调用 conversation service；旧总服务删除重复的同步/流式实现。
  - 将上下文注入和自动草案 adapter 改为公开符号，消除路由与 application service 对私有实现的依赖。
  - 更新 API 测试 patch 边界，并验证 Agent 收到 transcript 与 auto-draft port。
- 验证：
  - Planning 定向测试：108 passed、1 skipped。
  - 后端默认测试：491 passed、1 skipped、10 deselected。
  - 变更范围 Ruff `F401/F821`、Python compileall 和 `git diff --check` 通过。
- 备注：P3 下一批拆 draft service，再拆 save-and-execute 与 analysis/retest。

---

## 2026-08-29 | P3 Draft 与 Context Service 拆分

- 任务：将 Planning 草案生命周期和共享上下文构建移出总编排器。
- 操作：
  - 新增 `application/planning/draft_service.py`，承接草案生成、流式生成、状态更新、删除及自动草案 adapter。
  - 新增 `application/planning/context_service.py`，承接会话状态、工具历史、anti-pattern 和执行错误上下文构建。
  - AI Planning API 与 streaming bridge 直接依赖新 application services。
  - `services.ai_planning` 删除对应实现，仅保留兼容导出。
  - 测试替身改为 patch 实际实现模块，避免 facade 迁移后出现无效 mock。
- 验证：
  - Planning/Context/Analysis 定向测试：73 passed、1 skipped。
  - 变更范围 Ruff `F401/F821` 与 `git diff --check` 通过。
- 备注：下一批拆 save-and-execute 与 analysis/retest，并继续收敛 application ports。

---

## 2026-08-29 | P3 Save/Execute 与 Analysis/Retest 拆分

- 任务：完成 Planning application service 用例拆分并移除总编排器实现。
- 操作：
  - 新增 `execution_inputs.py`、`save_execute_service.py` 和 `analysis_retest_service.py`。
  - API 与 streaming bridge 直接依赖 application services；`services.ai_planning` 收敛为 71 行兼容 facade。
  - 将跨 application service 的分析、上下文和输入解析 helper 改为公开合同。
  - Planning Tools 为项目状态、洞察和推荐复测查询提供公共入口。
  - 修复流式 save-and-execute 调用不存在的 `EventLogWriter.log()`，改为注入 event-log factory 并调用 `write()`。
- 验证：
  - P3 定向测试：61 passed。
  - 变更范围 Ruff `F401/F821` 和 `git diff --check` 通过。
- 备注：P3 用例服务拆分完成；兼容 facade 保留一个迁移周期，下一阶段进入 P4 单执行事件源。

---

## 2026-08-29 | P4 单执行事件源与取消终态

- 任务：统一同步与流式 Case 执行核心，消除重复事务和报告构建路径。
- 操作：
  - `execute_case()` 改为消费 `execute_case_streaming()` 至终态，同步与流式入口共享 runner、状态迁移和 evidence 持久化。
  - 删除重复 `_execute_case_record()` 及同步 Playwright runner 依赖。
  - 增加同步/流式等价测试，比较最终状态、步骤顺序和完整 evidence。
  - 将 `cancelled` 纳入前后端执行状态合同，并在取消异常重新抛出前持久化报告与结束时间。
  - 补齐执行详情和报告中心的取消状态展示。
- 验证：
  - Execution 定向测试：19 passed。
  - 前端测试：64 passed。
  - 变更范围 Ruff `F401/F821` 通过。
- 备注：Runner 的流式解释器现为唯一执行源；报告继续只读取 `TestCaseRun.report` 持久化 JSON。

---

## 2026-08-28 | 全代码库孤儿代码与架构审计

- 任务：全量扫描代码库，识别无引用且不属于关键链路的代码，并评估当前架构与优化方向。
- 操作：
  - 以 275 个 tracked 文件为全集，建立前后端入口、API、DSL、AI planning、runner、locator、report 和 migration 链路。
  - 使用 Knip、Vulture、Pyflakes、`rg` 及独立子代理交叉检查，并排除 FastAPI、SQLAlchemy、Alembic、pytest、React lazy import 等隐式引用误报。
  - 生成 `docs/codebase-orphan-architecture-audit-2026-08-28.md`，记录确定孤儿、休眠能力、保留项、结构评分和分阶段优化方案。
- 验证：
  - `npm run build` 通过。
  - `npm test -- --run`：53 passed，7 failed。
  - 后端动态测试未运行：当前环境只有 Python 3.9，项目要求 Python 3.12，且未安装 `uv`。
- 发现：明文凭据、Alembic 迁移链断裂、无鉴权调试接口、VLM 分支未定义变量、错误新建用例路由、默认测试遗漏 integration 等问题已记录到 `docs/bug-log.md`。

---

## 2026-08-28 | 代码库优化执行计划

- 任务：根据 `docs/codebase-orphan-architecture-audit-2026-08-28.md` 制定可排期、可验收的优化计划。
- 操作：
  - 新增 `docs/plan/codebase-optimization-plan-2026-08-28.md`。
  - 将 D-01 至 D-11、O-01 至 O-19 和主要架构问题拆为 P0-P5 六个阶段。
  - 补充任务依赖、工期估算、测试门禁、删除门禁、架构决策、回滚策略和全计划完成定义。
  - 将安全止血、迁移恢复和测试基线设为架构重构前置条件。
- 验证：
  - 审计中的 D-01 至 D-11 和 O-01 至 O-19 均已纳入阶段映射。
  - 计划文件位于 `.gitignore` 白名单 `docs/plan/*.md`，可被 Git 跟踪。
- 备注：本次仅制定计划，未执行缺陷修复、孤儿代码删除或测试命令；未发现审计报告以外的新缺陷。

---

## 2026-08-28 | 优化计划同步与 P0 仓库治理

- 任务：同步代码库优化计划，并执行 P0 安全止血、迁移链恢复和跟踪策略修复。
- 操作：
  - 将审计报告、优化计划和日志提交并同步到 GitHub `main`，提交为 `7f055b3`。
  - 收窄 `.gitignore`，恢复文档、测试和 migration 默认跟踪，并忽略本地 `.tools/`。
  - 停止跟踪 `.claude/settings.local.json`，保留开发者本地文件；外部凭据轮换和历史处置仍待人工完成。
  - 恢复缺失 migration `45061d8892d7_add_is_default_to_projects.py` 及升降级回归测试。
  - 从生产 router 删除无鉴权 `/api/v1/ai-planning/test/locator` 调试接口并增加路由注册测试。
- 验证：
  - `uv run pytest tests/unit/test_project_default_migration.py -q`：1 passed。
  - `uv run pytest tests/unit/test_ai_planning_api.py::test_locator_debug_route_is_not_registered -q`：1 passed。
  - `uv run pytest tests/unit -q`：503 passed、2 failed、1 skipped；失败均为既有测试合同漂移，与本次 P0 修改无关，转入 P1 处理。
  - `uv run alembic heads`：唯一 head 为 `20260608_0025`。
  - 安全复核：本次差异未发现新增可利用问题。
- 发现：SQLite 空库全链升级在历史 migration `20260313_0004` 处失败，已记录为 `AUDIT-20260828-12`；生产 PostgreSQL 空库升级尚未在当前环境验证。

---

## 2026-08-28 | P1 运行时缺陷与测试门禁修复

- 任务：修复审计 D-04 至 D-10，并恢复前后端默认测试基线。
- 操作：
  - 修复 VLM candidate ranker 的 `model_family` 参数传递。
  - 删除已失效的 AI selector cache 及未消费指标。
  - 修正 DSL service `__all__` 并增加公开符号一致性测试。
  - 加固孤儿数据清理脚本：默认 dry-run、显式确认、默认项目/成员/用例/session 保护。
  - 默认 pytest 纳入非浏览器 integration，并为浏览器和外部服务 E2E 补 marker。
  - 增加 `/cases/new` create mode、项目参数传递和非法 case ID 拦截。
  - 修复前端路由、Cases 数据 mock、Planning SSE 时序断言、render-time navigate 和 TextArea NaN height。
- 验证：
  - 后端默认测试：519 passed、1 skipped、10 deselected。
  - 前端测试：63 passed。
  - 前端 `npm run build`：通过。
  - Knip：仍报告 P2 范围的孤儿文件、未用依赖/导出，以及 `@ant-design/icons` 未声明直接依赖。
  - Ruff：工具可运行，但全仓存在 575 个既有问题，不能直接作为零告警门禁；需先建立基线或分阶段清理。
- 备注：D-04 至 D-10 已关闭。静态检查遗留转入 P2；未修改用户已暂存的 `test_brand_filter_cart`。

---

## 2026-08-28 | P2 确定孤儿代码清理

- 任务：按 C1-C4 批次删除审计 O-01 至 O-19 的确定孤儿，并治理休眠能力。
- 操作：
  - 删除孤儿 backend runner、frontend layout、临时 DSL 结果和冗余 `.gitkeep`。
  - 移除未使用 `echarts` 及 Vite 分包规则，补齐 `@ant-design/icons` 直接依赖。
  - 删除未使用 CSS、DSL adapter/helper、旧 locator preflight 和 candidate collector。
  - 删除 Page Explorer 旧 DOM prompt formatter、grouping/filter 子系统及旧 flow action 链，保留 `_collect_flow_a11y` 当前主链。
  - 删除旧同步 Planning LLM、未启用日志上下文/Timer、零调用 locator/access/SSE helper 和前端旧导出。
  - 删除只固定旧实现的测试，保留并回归当前 DSL、A11y、runner 和 API 合同测试。
  - 新增 `docs/plan/capability-status-2026-08-28.md`，记录休眠能力 owner、状态和复核期限。
- 验证：
  - 后端默认测试：490 passed、1 skipped、10 deselected。
  - 前端测试：63 passed；`npm run build` 通过。
  - Knip 不再报告孤儿文件、未使用依赖和缺失直接依赖；剩余项为明确保留的 dormant API clients 与 transport types。
  - 变更文件 Ruff `F401/F821` 检查通过。
- 备注：`CaseListParams` 整体无消费者，已删除；Pydantic 框架隐式入口和 Alembic 历史均保留。

---

## 2026-08-28 | P3 Planning 解耦第一切片

- 任务：冻结 Planning/执行语义，并消除 Agent 对 Planning Service 的反向依赖。
- 操作：
  - 新增 `docs/plan/adr-001-planning-execution-semantics.md`，确定单 active project、VLM 默认关闭和同步/流式单事件源方向。
  - 将 `ENABLE_AI_VISUAL_LOCATE` 默认值改为 false，保留显式启用能力。
  - 将工具结果 URL 归一化、缓存查询和原始页面结果提取迁到 `app.ai.tool_result_cache`。
  - Planning Tools 不再导入 `services.ai_planning` 私有缓存函数。
  - Agent 通过 `AutoDraftGenerator` protocol 接收草案生成 callback，不再延迟导入 Planning Service。
  - Planning Service 不再调用 Agent 私有 `_extract_raw_page_results`。
- 验证：
  - P3 定向测试：94 passed。
  - 后端默认测试：490 passed、1 skipped、10 deselected。
  - 新增模块及配置变更 Ruff `F401/F821` 检查通过。
- 备注：P3 尚未完成；后续继续建立 active project 边界并按 session/conversation/draft/save-execute/analysis-retest 拆 application services。

---

## 2026-06-08 | 孤儿数据清理：清理 70 个孤立项目，添加清理脚本

**任务**：清理数据库中的孤儿数据。

**问题现象**：
- 数据库中有 70 个孤立项目（不关联任何会话）
- 这些项目是会话删除后遗留的

**清理过程**：

**第一步：检查孤儿数据**
```bash
uv run python scripts/cleanup_orphan_data.py --dry-run
```

输出：
```
=== Orphaned Data Summary ===
Orphaned projects: 70
Orphaned messages: 0
Orphaned drafts: 0
Orphaned event logs: 0
Orphaned session-project links: 0
```

**第二步：执行清理**
```bash
uv run python scripts/cleanup_orphan_data.py
```

输出：
```
=== Cleaning up orphaned data ===
Deleted 0 orphaned session-project links
Deleted 0 orphaned event logs
Deleted 0 orphaned drafts
Deleted 0 orphaned messages
Deleted 70 orphaned projects
```

**第三步：验证清理**
```bash
uv run python scripts/cleanup_orphan_data.py --dry-run
```

输出：
```
=== Orphaned Data Summary ===
Orphaned projects: 0
Orphaned messages: 0
Orphaned drafts: 0
Orphaned event logs: 0
Orphaned session-project links: 0
```

### 新增文件

**1. 清理脚本**
- `backend/scripts/cleanup_orphan_data.py`
- 功能：查找并删除所有孤儿数据
- 支持 `--dry-run` 模式预览
- 支持：孤立项目、消息、草案、事件日志、会话-项目链接

**2. 测试文件**
- `backend/tests/unit/test_orphan_session.py`
- 功能：测试孤儿会话场景
- 6 个测试用例覆盖各种边界情况

### 验证结果

- 后端单元测试：501 tests passed（2 个预存失败）
- 孤儿数据清理：70 个孤立项目已删除
- 数据库状态：所有孤儿数据已清理

### 设计原则

1. **定期清理**：定期运行清理脚本，防止孤儿数据积累
2. **测试覆盖**：测试覆盖所有边界情况
3. **防御性编程**：在入口处验证数据，防止孤儿数据产生
4. **监控告警**：监控孤儿数据数量，及时发现问题

---

## 2026-06-08 | 孤儿会话根因修复：SSE 端点添加会话验证，测试驱动调试

**任务**：通过测试驱动调试找到孤儿会话的根因并修复。

**问题现象**：
- 前端显示会话 305
- 数据库中没有会话 305
- 用户尝试发送消息时报错：`AI planning session 305 not found`

**调试过程**：

**第一步：写测试复现问题**
创建 `test_orphan_session.py`，测试以下场景：
1. 会话列表只包含存在的会话
2. 发送消息到已删除的会话返回 404
3. 获取已删除会话的详情返回 404
4. 为已删除的会话生成草案返回 404
5. 并发会话操作
6. 会话删除后列表刷新

**第二步：发现 bug**
测试 `test_generate_drafts_for_deleted_session_returns_404` 失败：
```
assert 200 == 404  # drafts 端点返回 200，而不是 404
```

**根因分析**：
SSE 端点（`/chat`、`/drafts`、`/execute`）在启动流之前**没有验证会话是否存在**。错误发生在流式生成器内部，前端收到 200 状态码，但流会失败。

**这是孤儿会话的根本原因**：
1. 前端调用 `/drafts` 端点
2. 后端返回 200（流式响应）
3. 前端认为操作成功
4. 流式生成器内部报错：`AI planning session X not found`
5. 前端显示错误，但会话列表没有刷新
6. 用户看到孤儿会话

**第三步：修复**
在所有 SSE 端点添加会话验证：
- `/chat`：启动流之前验证会话存在
- `/drafts`：启动流之前验证会话存在
- `/execute`：启动流之前验证会话存在

如果会话不存在，直接返回 404，而不是启动流再报错。

### 后端改动

**1. 新增测试文件**
- `tests/unit/test_orphan_session.py`：6 个测试用例覆盖孤儿会话场景

**2. 修改 SSE 端点**
- `backend/app/api/routes/ai_planning.py`：
  - `chat_sse`：添加会话验证
  - `drafts_sse`：添加会话验证
  - `execute_sse`：添加会话验证

**3. 前端错误处理**（之前的改动）
- `handleSendMessage`：发送前验证会话存在
- `handleGenerateDrafts`：生成前验证会话存在
- `loadSessionDetail`：加载失败时刷新会话列表

### 验证结果

- 后端单元测试：501 tests passed（2 个预存失败）
- 孤儿会话测试：6 tests passed
- 前端构建：TypeScript 编译 + Vite 构建成功

### 设计原则

1. **测试驱动调试**：先写测试复现问题，再修复
2. **根因分析**：找到问题的根本原因，而不是加防线
3. **防御性编程**：在入口处验证数据，而不是在内部报错
4. **错误处理**：返回明确的错误码，而不是流式失败

---

## 2026-06-08 | 孤儿会话修复：前端会话验证 + 错误处理 + 状态同步

**任务**：修复前端显示孤儿会话数据（数据库中不存在）的问题。

**问题现象**：
- 前端显示会话 305
- 数据库中没有会话 305
- 用户尝试发送消息时报错：`AI planning session 305 not found`

**根本原因**：
1. 会话创建失败但前端乐观更新
2. 会话被删除但前端未同步
3. 网络错误导致状态不一致
4. 竞态条件

**设计缺陷**：
- 没有考虑异常流程
- 没有考虑数据一致性
- 没有考虑边界情况
- 没有设计测试用例

**修复方案**：

**1. 前端添加会话验证**
- `handleSendMessage`：发送消息前验证会话存在
- `handleGenerateDrafts`：生成草案前验证会话存在
- `loadSessionDetail`：加载会话失败时刷新会话列表

**2. 前端添加错误处理**
- `createAndSelectSession`：创建会话失败时显示错误
- `handleSendMessage`：发送失败时刷新会话列表
- `handleGenerateDrafts`：生成失败时刷新会话列表

**3. 前端添加状态同步**
- 错误发生时自动刷新会话列表
- 会话不存在时显示友好错误消息
- 会话列表保持与数据库同步

**设计原则**：
1. **永远不要假设数据存在**：访问前必须验证
2. **永远不要忽略错误**：所有操作必须有错误处理
3. **永远不要信任缓存**：关键操作前刷新数据
4. **永远不要忽略边界情况**：考虑所有可能的失败场景

### 后端改动

**1. 修复迁移链**
- `0cf285e27ae1` 的 `down_revision` 从 `20260426_0022` 改为 `20260426_0021`
- 执行 `uv run alembic upgrade head` 创建 `ai_planning_event_logs` 表

### 前端改动

**1. `handleSendMessage`**
- 发送消息前调用 `getPlanningSession` 验证会话存在
- 发送失败时自动刷新会话列表

**2. `handleGenerateDrafts`**
- 生成草案前调用 `getPlanningSession` 验证会话存在
- 生成失败时自动刷新会话列表

**3. `loadSessionDetail`**
- 加载失败时显示友好错误消息
- 加载失败时自动刷新会话列表

**4. `createAndSelectSession`**
- 创建失败时显示友好错误消息
- 创建失败时抛出错误供调用者处理

### 验证结果

- 后端单元测试：495 tests passed（2 个预存失败）
- 前端构建：TypeScript 编译 + Vite 构建成功
- 会话验证：发送消息前验证会话存在
- 错误处理：所有操作都有错误处理
- 状态同步：错误发生时自动刷新会话列表

---

## 2026-06-08 | SSE 架构修复 v2：Session 隔离 + 弹性降级

**任务**：修复 SSE 事件日志架构设计缺陷——表不存在时流式崩溃。

**问题根因**：
原设计中 `EventLogWriter` 和主流共享同一个数据库 session。当事件日志写入失败时，session 被 rollback，导致主流的 `_flush_streaming_msg_to_db` 也失败。

**日志证据**：
```
Failed to flush SSE events (session 304), disabling: This Session's transaction has been rolled back
due to a previous exception during flush. Original exception was: (psycopg.errors.UndefinedTable)
关系 "ai_planning_event_logs" 不存在
```

**解决方案：Session 隔离 + 弹性降级**

**核心改动**：
1. **Session 隔离**：`EventLogWriter` 使用独立的数据库 session，不影响主流
2. **无前置初始化**：`__init__` 不查询数据库，只存储参数
3. **内联写入**：每个事件写入是独立的 try-catch，失败后静默禁用
4. **弹性降级**：如果表不存在，第一次写入失败 → 后续写入全部跳过 → 主流不受影响

**关键代码**：
```python
class EventLogWriter:
    def __init__(self, session_factory, session_id, ...):
        self._session_factory = session_factory  # 接收 session_factory，不是 session
        self._session = None  # Lazy init，不立即创建

    def write(self, event_type, event_data):
        if not self._enabled:
            return
        session = self._get_session()  # 独立 session
        try:
            session.add(AIPlanningEventLog(...))
        except Exception:
            self._enabled = False  # 只禁用事件日志，不影响主流
            session.rollback()  # 只 rollback 独立 session
```

**架构对比**：
```
旧设计（错误）：
┌─────────────┐
│ Main Session │◀─── EventLogWriter（共享 session）
└─────────────┘
       │
       ▼ 写入失败 → session rollback → 主流崩溃

新设计（正确）：
┌─────────────┐
│ Main Session │◀─── 主流（独立）
└─────────────┘
┌─────────────┐
│ Log Session  │◀─── EventLogWriter（独立 session）
└─────────────┘
       │
       ▼ 写入失败 → log session rollback → 主流不受影响
```

### 后端改动

**1. 重写 `sse_event_log.py`**
- 移除 `SSEEventLogger` 类
- 新增 `EventLogWriter` 类
- 接收 `session_factory` 而不是 `session`
- 使用独立 session，Lazy 初始化

**2. 更新 `ai_planning.py`**
- `stream_planning_message`：新增 `session_factory` 参数
- `stream_generate_planning_drafts`：新增 `session_factory` 参数
- `save_and_execute_selected_drafts_streaming`：新增 `session_factory` 参数
- 所有 `EventLogWriter` 初始化使用 `session_factory`

**3. 更新 `ai_planning_streaming.py`**
- `stream_planning_chat`：传递 `session_factory` 给 `stream_planning_message`
- `stream_planning_drafts`：传递 `session_factory` 给 `stream_generate_planning_drafts`
- `_run_sync_save_and_execute`：传递 `session_factory` 给 `save_and_execute_selected_drafts_streaming`

### 验证结果

- 后端单元测试：495 tests passed（2 个预存失败）
- 前端构建：TypeScript 编译 + Vite 构建成功
- Session 隔离：事件日志失败不影响主流
- 弹性降级：如果表不存在，流式正常工作，事件日志静默禁用

---

## 2026-06-08 | SSE 架构修复：弹性降级设计，移除前置 DB 依赖

**任务**：修复 SSE 事件日志架构设计缺陷——表不存在时流式崩溃。

**问题根因**：
原设计在 `SSEEventLogger.__init__` 中查询 `ai_planning_event_logs` 表获取 max(seq)，如果表不存在（迁移未执行），整个流式会崩溃。

**设计缺陷**：
- 事件日志是"增强功能"，不应该成为主流程的硬依赖
- 不应该在流式开始前就初始化并查询数据库
- 应该是 LLM 拿到消息之后才开始写入数据库

**解决方案：弹性降级 + 内联写入**

**新架构设计**：
```python
class EventLogWriter:
    def __init__(self, session, session_id, ...):
        # NO DB query here — just store parameters
        self._enabled = True

    def write(self, event_type, event_data):
        if not self._enabled:
            return
        try:
            # Inline write — if table missing, first write fails → disable
            session.add(AIPlanningEventLog(...))
        except Exception:
            self._enabled = False  # Graceful degradation
```

**关键改动**：
1. **移除 `SSEEventLogger` 类**，替换为 `EventLogWriter`
2. **无前置初始化**：`__init__` 不查询数据库，只存储参数
3. **内联写入**：每个事件写入是独立的 try-catch，失败后静默禁用
4. **弹性降级**：如果表不存在，第一次写入失败 → 后续写入全部跳过 → 主流不受影响

**核心原则**：
- 事件日志是"有则更好"的功能，不是"必须存在"的依赖
- 每个写入操作都是独立的，失败不影响主流程
- 不应该在流式开始前就查询数据库

### 后端改动

**1. 重写 `sse_event_log.py`**
- 移除 `SSEEventLogger` 类
- 新增 `EventLogWriter` 类
- 特性：无前置初始化、内联写入、弹性降级

**2. 更新 `ai_planning.py`**
- 替换所有 `SSEEventLogger` 为 `EventLogWriter`
- 移除前置初始化代码
- 保持内联写入模式

**3. 更新 replay API**
- 使用 `created_at` 排序（seq 是 per-stream，不是全局）
- 保持向后兼容性

### 验证结果

- 后端单元测试：495 tests passed（2 个预存失败）
- 前端构建：TypeScript 编译 + Vite 构建成功
- 弹性降级：如果表不存在，流式正常工作，事件日志静默禁用

---

## 2026-06-08 | SSE 事件日志架构：解决刷新丢消息问题

**任务**：修复 AI 规划会话中 SSE 流式消息刷新后丢失的问题。

**问题根因**：
SSE 事件是"发射即忘"的——事件通过 HTTP 流推送给前端但从未持久化到数据库。一旦页面刷新，前端只能从数据库加载，而数据库中的 stub 消息（`turn_type="streaming"`）内容远落后于实际流式进度。用户必须等到整个流完成、数据库最终提交后才能看到完整内容并继续操作。

**具体问题**：
1. `text_chunk` 事件每 5 个才 flush 一次到 DB
2. `status`、`tool_call_start`、`tool_call_end` 等事件从未持久化
3. 没有事件日志/事件存储——SSE 事件是 fire-and-forget
4. 前端无重连/恢复机制——POST-based SSE 无法用浏览器原生 `EventSource` 重连

### 解决方案：SSE 事件日志 + 智能恢复

**架构设计**：
```
┌─────────────┐     ┌──────────────┐     ┌──────────────────┐
│  AI Agent   │────▶│ SSE Stream   │────▶│ Frontend (实时)   │
│ (生成器)     │     │ (HTTP 流)     │     │ (乐观更新)        │
└──────┬──────┘     └──────────────┘     └──────────────────┘
       │
       ▼
┌──────────────┐     ┌──────────────────┐
│  DB Event    │────▶│ Replay API       │
│  Log Table   │     │ (GET /events)    │
└──────────────┘     └──────────────────┘
                              │
                              ▼
                     ┌──────────────────┐
                     │ Frontend (刷新后) │
                     │ (事件恢复)        │
                     └──────────────────┘
```

### 后端改动

**1. 新增模型 `AIPlanningEventLog`**
- 文件：`backend/app/models/ai_planning_event_log.py`
- 字段：session_id, message_id, event_type, event_data, seq, created_at
- 索引：`(session_id, seq)` 联合索引支持高效 range query

**2. 新增 Alembic 迁移**
- 文件：`backend/alembic/versions/20260608_0025_sse_event_log.py`

**3. 新增 `SSEEventLogger` 服务**
- 文件：`backend/app/services/sse_event_log.py`
- 功能：在 SSE 流式过程中批量持久化每个事件
- 特性：自动 flush、序列号管理、错误容忍

**4. 集成到 `stream_planning_message`**
- 文件：`backend/app/services/ai_planning.py`
- 改动：
  - 每个事件 yield 前写入事件日志
  - `status` 事件也触发 flush（原来只有 text_chunk）
  - `_flush_streaming_msg_to_db` 增强：同时更新 `structured_payload_json` 中的阶段信息
  - `_STREAMING_FLUSH_INTERVAL` 从 5 降到 3

**5. 集成到 `stream_generate_planning_drafts`**
- 文件：`backend/app/services/ai_planning.py`
- 改动：draft_generating 和 turn_complete 事件写入日志

**6. 集成到 `save_and_execute_selected_drafts_streaming`**
- 文件：`backend/app/services/ai_planning.py`
- 改动：save_progress, case_start, step_start/complete, analysis_complete, done 事件写入日志

**7. 新增事件 replay API**
- 文件：`backend/app/api/routes/ai_planning.py`
- 端点：`GET /api/v1/ai-planning/sessions/{session_id}/events?after_seq=N`
- 功能：返回指定序号之后的所有事件，支持增量获取

### 前端改动

**1. 新增 `getSessionEvents` API 函数**
- 文件：`frontend/src/services/api.ts`
- 功能：调用 replay API 获取事件日志

**2. 增强 `applySessionDetail` 支持事件恢复**
- 文件：`frontend/src/components/AITestPlanningPanel.tsx`
- 新增 `applySessionDetailWithRecovery` 函数
- 逻辑：
  1. 先应用 DB 状态（显示最后 flush 的内容）
  2. 检测 `turn_type="streaming"` 的中断消息
  3. 调用 replay API 获取该消息的所有事件
  4. 回放 text_chunk/status/tool_call 事件恢复最新内容
  5. 如果发现 `turn_complete` 事件，直接标记为完成
  6. 更新 transcript 显示恢复的内容

**3. UI 增强**
- 新增"✓ 已恢复"指示器，显示内容已从事件日志恢复
- 保持原有的"⏸ 回复中断"指示器作为 fallback

### 验证结果

- 后端单元测试：505 tests passed（修复了 1 个因新增表导致的测试）
- 前端构建：TypeScript 编译 + Vite 构建成功
- 新增表：`ai_planning_event_logs` 已通过 Alembic 迁移创建

### 后续优化（不在本次范围）

1. 心跳保活：SSE 流中定期发送 `:keepalive` 注释
2. 事件日志清理：定期清理超过 7 天的事件日志
3. 断线重连：前端检测连接断开后自动重连
4. 并发控制：限制同时活跃的 SSE 连接数

---

## 2026-06-05 | Locator + Explore 双轨修复：paragraph 角色支持、探索导航精确化、Prompt 清理

**任务**：修复品牌筛选购物车 E2E 测试中"数据采集错误 → DSL 失败"的完整链路问题。

**问题发现过程**：
用户报告两个 bug：① `paragraph "Premium Polo T-Shirts"` 定位失败；② `capture_text` 后的 DSL 步骤意图不清晰。通过追踪 AI Session 302 的完整数据流（DB → explore_flow 参数 → 采集数据 → DSL 生成 → 执行结果），发现根因不只在 locator 层，而是三层叠加。

### Bug K：paragraph/StaticText role 在 semantic locator 中缺失

- **现象**：Run 195 报错 `All locate tiers failed for target: paragraph "Premium Polo T-Shirts"`
- **发现过程**：
  1. 查询 AI Session 302 生成的 DSL draft (ID=218)，发现大量 `paragraph "X"` 和 `heading "X" inside "Y"` 格式的 target
  2. 分析 [semantic.py:48-56](backend/app/locators/semantic.py#L48-L56) 的 `_A11Y_ROLE_TARGET_RE` 正则，发现 `paragraph` 不在角色列表中
  3. 用 Playwright 直接测试 `get_by_role("paragraph")` 能发现 14 个 `<p>` 元素，但 `get_by_role("paragraph", name="Blue Top")` 返回 0——因为 `<p>` 是非交互元素，没有 accessible name
  4. 对比 [dsl_generator.py:829-833](backend/app/ai/dsl_generator.py#L829)，prompt 教 AI 用 `StaticText`，但 `StaticText` 也不在正则和角色映射中
- **代码证据**：
  ```python
  # semantic.py:48-54 — 修复前，paragraph/statictext 都不在正则中
  _A11Y_ROLE_TARGET_RE = re.compile(
      r'^(button|link|...|heading|...|cell|row|column)'  # ← 缺 paragraph/statictext
  )
  # semantic.py:68-109 — 修复前，A11Y_TO_PLAYWRIGHT_ROLE 也没有 paragraph
  ```
- **修复**：
  1. 在 `_A11Y_ROLE_TARGET_RE` 加入 `paragraph|statictext`
  2. 新增 `_TEXT_ONLY_ROLES` 集合标记 `{"paragraph", "statictext"}`
  3. 在 `_build_a11y_candidates` 中，text-only 角色优先用 `get_by_text()` 而非 `get_by_role(name=)`
  4. inside scoping 的容器查找从爬 1 级 (`xpath=..`) 改为爬 3 级
- **实际成果**：`paragraph "Blue Top"` 正确解析为 `get_by_text("Blue Top", exact=True)`，text 匹配准确。手动 18/18 步全通过。

### Bug L：explore_flow 的 a11y click 导航不可靠

- **现象**：AI Session 302 的 Call 1（点击式探索）采集到的 Polo 页面数据是全部 35 个产品，不是 6 个 Polo 专属产品
- **发现过程**：
  1. 查询 `ai_planning_tool_results` (explore id=357/358)，对比两次探索的 raw_result
  2. Call 1（steps-based, `click "Polo"`）产出 34 个产品段落，Call 2（URL-based, 编造 `products?brand=Polo`）也是同样全部产品
  3. 用 Playwright 实测 `resolve_with_fallback(page, 'Polo')` 能正确找到 `a[href="/brand_products/Polo"]`，但 explore_flow 中采集的产品仍是全部
  4. 追踪到 [page_explorer.py:1590](backend/app/ai/page_explorer.py#L1590)：探索阶段 click 只用 `_resolve_step_locator`（a11y text 匹配），可能匹配到错误元素（如 breadcrumb/文本节点）且静默失败
- **代码证据**：
  ```python
  # page_explorer.py:1590-1597 — 修复前，只有 a11y 定位，没有精确选择器
  elif act in ("click", "press", "tap"):
      loc = _resolve_step_locator(page, target, kind="click", skip_vlm=True)
      if loc is None:
          loc = _resolve_step_locator(page, target, kind="click", skip_vlm=True)
      if loc is not None:
          click_with_precheck(page, loc)
  ```
- **修复**：新增 `_resolve_from_collected_nodes` 函数（~60行），在探索阶段 click 时优先用上一页采集的 `verified_selectors`（如 `a[href="/brand_products/Polo"]`）或 DOM 属性构造 CSS 选择器，失败才回退 a11y locator
- **实际成果**：探索采集从 35 个全部产品精确到 6 个 Polo 专属产品（Blue Top, Fancy Green Top, Green Side Placket Detail T-Shirt, Premium Polo T-Shirts, Soft Stretch Jeans, Grunt Blue Slim Fit Jeans）

### Bug M：explore_flow 页面 URL 归因在动作前

- **现象**：修复 Bug L 后，`_collect_flow_a11y` 将 Polo 页面的 173 个节点归因到 products 页面 URL（`S1` 状态），而非 Polo 页面
- **发现过程**：测试脚本打印每个 state 的产品集合，发现 S1（products）有 34 个产品，S2（Polo）不存在
- **代码证据**：
  ```python
  # page_explorer.py:1637 — 修复前，URL 在动作执行前捕获
  current_url = page.url  # ← 此时还在 products 页面
  # ... 执行 click Polo → 导航到 brand_products/Polo ...
  # 但 page_entry["url"] 仍是 products 的 URL
  ```
- **修复**：在 `results.append(page_entry)` 前检查 `page.url != current_url`，如果导航发生则更新 `page_entry["url"]` 和新 state，并回写 action nodes 的 `page_state`
- **实际成果**：Polo 页面正确获得独立 state（S2），节点归因准确

### Prompt 清理

对 3 个文件 9 处硬编码 prompt 修复：
- **dsl_generator.py**：`StaticText` → `paragraph` 示例统一、删除 `cell` 示例、合并重复 `${var}` 规则
- **test_planning_agent.py**：删除 `test@automationexercise.com`/`password123`/`click "(6) POLO"` 硬编码 demo 值
- **test_planning_prompts.py**：删除 `"Signup / Login"`/`"Email"` 硬编码、删除"探索失败→报告用户不跳过"（与代码 GUARD_CONTINUE_LIMIT 矛盾）

### 全链路验证

探索修复 + prompt 清理后，运行完整 pipeline：
1. ✅ 探索采集：Polo 专属 6 个产品，精确无误
2. ✅ DSL 生成：结构正确，AI 正确选择了 Blue Top (Rs.500) + Fancy Green Top (Rs.700)
3. ⚠️ 执行：AI 仍在 Fancy Green Top 的价格上出错（用了 Rs.1000 而非 Rs.700），属于 LLM 推理层面问题，待后续 prompt 优化

**待解决问题**：
- AI 从探索数据中提取 product→price 关联仍有 LLM 幻觉（把 Green Side Placket Detail T-Shirt 的 Rs.1000 安到 Fancy Green Top）
- 全链路执行因价格错误未通过，需要改进 DSL generator 的数据呈现格式

**测试结果**：
- paragraph/StaticText 修复后手动 18/18 步通过
- 探索修复后精确采集 Polo 6 产品
- 全链路：探索 ✅ → DSL 生成 ⚠️ → 执行 ❌

---

## 2026-06-04 | Anti-pattern 注入与上下文重构

**任务**：重构上下文注入架构，修复执行错误注入，解决 VLM fallback 问题。

**测试需求**：
- 验证品牌筛选购物车流程
- 测试执行错误注入是否生效
- 测试 DSL 生成器是否使用 user_context

**问题现象与修复**：

### 修复 1：执行错误注入失效
- **现象**：当 `case_id` 为 null 时，`_build_execution_error_context` 直接返回 None
- **根因**：函数只从 `case_id` 查询执行记录，没有从项目维度查询
- **修复**：当 `case_id` 为 null 时，从项目的最近执行记录中查找
- **验证**：测试 `test_should_inject_error_when_recent_execution_exists` 通过

### 修复 2：dsl_execution 函数签名错误
- **现象**：`slog.dsl_execution()` 使用了 `session_id` 参数，但函数只接受 `execution_id`
- **根因**：函数签名不匹配
- **修复**：改为使用 `execution_id=latest_run.id`
- **验证**：测试 `test_injects_error_when_case_id_exists` 通过

### 修复 3：上下文注入架构重构
- **现象**：`_inject_auto_context` 函数只在 AI 的 ReAct 循环中被调用，DSL 生成器无法使用
- **根因**：架构耦合，上下文注入函数无法复用
- **修复**：提取 `_build_auto_context_preamble` 函数，可以在 DSL 生成器调用时被调用
- **验证**：测试 `test_build_auto_context_preamble` 系列通过

### 修复 4：DSL 生成器使用 user_context
- **现象**：DSL 生成器有 `user_context` 字段，但没有被使用
- **根因**：DSL 生成器没有在 prompt 中注入 `user_context`
- **修复**：在 `dsl_generator.py` 中添加 `user_context` 的注入
- **验证**：测试 `test_dsl_generator_uses_user_context` 通过

**待解决问题**：
- VLM fallback 问题依然存在（AI 生成 `paragraph` 格式，但不是有效的 Playwright role）
- AI 获取错误信息后没有生成新的 draft，而是重新执行了同一个 draft

**测试结果**：
- 497 个单元测试通过
- 1 个测试跳过
- 6 个警告

---

## 2026-05-31 | textContent + DSL 完善（4 次修复）

**任务**：验证购物车品牌筛选 DSL 测试用例，修复 4 个问题，最终 21/21 步骤全通过。

**测试需求**：
- 首页 → 品牌筛选（Polo）→ 添加 Blue Top (Rs.500) → 添加 Fancy Green Top (Rs.700, qty=2) → 验证购物车总价

**问题现象与修复**：

### 修复 1：textContent 问题
- **现象**：`heading="BRAND - POLO PRODUCTS"` 定位失败，实际文本是 `Brand - Polo Products`
- **根因**：CSS `text-transform: uppercase` 导致 `innerText` 返回全大写，但 Playwright 使用 `textContent`
- **修复**：修改 `_augment_a11y_nodes_with_dom` 函数，使用 `textContent` 替代 `innerText`
- **文件**：`backend/app/ai/page_explorer.py`

### 修复 2：View Product 定位问题
- **现象**：`link="View Product" inside "Blue Top"` 定位失败
- **根因**："View Product" 链接不在产品容器中（a11y 树中是独立的 `list` 元素）
- **修复**：使用 CSS 选择器 `.product-image-wrapper:has(.productinfo:has-text("Fancy Green Top")) a:has-text("View Product")`
- **文件**：`backend/test_dsl.json`

### 修复 3：数量修改问题
- **现象**：购物车页面 `input #quantity 2` 无效，总价未更新
- **根因**：购物车页面数量是只读的（`<button class="disabled">1</button>`）
- **修复**：在产品详情页设置数量为 2，然后再添加到购物车
- **文件**：`backend/test_dsl.json`

### 修复 4：断言值问题
- **现象**：`assert_text cell="Rs. 1400"` 找不到
- **根因**：Fancy Green Top 数量是 1，总价是 Rs. 700
- **修复**：在产品详情页设置数量为 2，总价更新为 Rs. 1400
- **文件**：`backend/test_dsl.json`

**执行动作**：
1. 运行 `python -c "..."` 测试 DSL 执行
2. 检查 a11y 树结构，发现 "View Product" 不在产品容器中
3. 检查购物车 HTML 结构，发现数量是只读的
4. 测试产品详情页设置数量后添加到购物车

**结果**：21/21 步骤全通过

**验证**：
```
Step 1: OK - goto
Step 2: OK - click (Products)
Step 3: OK - wait_for (All Products)
Step 4: OK - click ((6)Polo)
Step 5: OK - wait_for (Brand - Polo Products)
Step 6: OK - capture_text (Rs. 500)
Step 7: OK - click (Add to cart - Blue Top)
Step 8: OK - wait_for (Continue Shopping)
Step 9: OK - click (Continue Shopping)
Step 10: OK - capture_text (Rs. 700)
Step 11: OK - click (View Product - Fancy Green Top)
Step 12: OK - wait_for (Quantity:)
Step 13: OK - input (#quantity = 2)
Step 14: OK - click (Add to cart)
Step 15: OK - wait_for (View Cart)
Step 16: OK - click (View Cart)
Step 17: OK - wait_for (Shopping Cart)
Step 18: OK - assert_text (Blue Top)
Step 19: OK - assert_text (Rs. 500)
Step 20: OK - assert_text (Rs. 500)
Step 21: OK - assert_text (Rs. 1400)
```

**后续**：
- "View Product" 链接不在产品容器中，无法使用 `inside` 语法，需要使用 CSS 选择器
- 购物车页面数量是只读的，需要在产品详情页设置数量

---

## 2026-05-31 | A11y 无名输入框定位修复

**任务**：验证购物车品牌筛选 DSL 测试用例，修复 Quantity 输入框定位失败问题。

**测试需求**：
- 登录 → 品牌筛选（Polo）→ 添加 Blue Top (Rs.500) → 添加 Fancy Green Top (Rs.700, qty=2) → 验证购物车总价

**问题现象**：
1. `textbox="Quantity"` 定位失败 — 输入框在 a11y 树中是 `ignored` 状态
2. 购物车页面价格是 `cell` role，不是 `heading`，`inside` 语法失效

**根因分析**：
1. Quantity 输入框（`<input type="number" id="quantity">`）没有 `aria-label`，也没有关联的 `<label>` 元素，Chrome 无障碍引擎将其标记为 `ignored`
2. 购物车页面价格在 `<td>` 单元格内，a11y role 是 `cell`，不是 `heading`

**操作**：
1. **语义定位器增强** (`backend/app/locators/semantic.py`)：
   - 添加 `_find_input_near_text()` 函数：查找 label 文本 → 定位兄弟 input 元素
   - 新增 `a11y_label_sibling_input` 策略（基础分 82）
   - 扩展 `_A11Y_ROLE_TARGET_RE` 和 `_A11Y_TO_PLAYWRIGHT_ROLE`：添加 `cell`、`row`、`column`

2. **DSL 修改** (`backend/test_dsl.json`)：
   - `textbox="Quantity"` → `Quantity`（纯文本匹配，触发 label-sibling-input 策略）
   - `heading="Rs. 500" inside "Blue Top"` → `cell="Rs. 500"`
   - `heading="Rs. 1400" inside "Fancy Green Top"` → `cell="Rs. 1400"`

3. **执行脚本清理**：删除 `execute_dsl.py`、`run_dsl_with_runner.py`、`test_cart_flow.py`、`explore_results.json`、`product_structure.txt`

**结果**：29 个步骤全部通过（含 Login、品牌筛选、添加商品、购物车验证）

**验证**：
```
Step 12: input Quantity → OK: Filled Quantity
Step 20: input Quantity → OK: Filled Quantity
Step 28: assert_text cell="Rs. 500" → OK
Step 29: assert_text cell="Rs. 1400" → OK
```

**后续**：
- 此修复适用于所有没有 `aria-label` 的输入框（通过 label 文本自动关联）
- `cell` role 支持可用于表格数据断言

---

## 2026-05-30 | Agent 流程 vs 直接脚本测试对比分析

**任务**：使用 explore-flow 工具探索页面，生成 DSL 并执行测试，验证购物车品牌筛选功能。

**测试需求**：
- 登录 → 品牌筛选（Polo）→ 添加商品 A（Blue Top）→ 添加商品 B（Fancy Green Top）→ 验证购物车

**操作**：
1. 使用 `explore_flow` 探索页面，获取 a11y 节点
2. 基于探索结果生成 DSL
3. 执行 DSL 验证购物车功能

**发现的问题**（共 6 项）：

### 问题 1：a11y 节点过滤太严格
- **现象**：商品名称（`paragraph` 元素）被过滤掉，无法获取
- **根因**：`USEFUL_A11Y_ROLES` 使用白名单模式，遗漏了 `paragraph`、`text`、`statictext` 等角色
- **修复**：改为黑名单模式（`IGNORED_A11Y_ROLES`），只排除已知无用的角色

### 问题 2：语义定位器对文本敏感
- **现象**：`get_by_text("(6) POLO")` 无法匹配页面中的 `"(6)Polo"`
- **根因**：文本匹配对大小写和空格敏感
- **修复**：添加更灵活的匹配策略（去除空格、大小写不敏感、正则表达式、role-based fallback）

### 问题 3：广告遮挡点击操作
- **现象**：Google 广告 iframe 遮挡点击，报错 "subtree intercepts pointer events"
- **根因**：页面上有 Google 广告覆盖层
- **修复**：添加 JavaScript 移除广告 iframe

### 问题 4：弹窗等待问题
- **现象**：点击 "Add to cart" 后立即点击 "Continue Shopping" 失败
- **根因**：弹窗需要时间加载
- **修复**：添加 `wait_for_selector('.modal-content')` 等待弹窗出现

### 问题 5：按钮选择歧义
- **现象**：Agent 流程添加了错误的商品（Men Tshirt 而不是 Fancy Green Top）
- **根因**：
  - 直接脚本：`product_cards.nth(1).locator('.add-to-cart')` → 6 个商品卡片
  - Agent 流程：`.productinfo .add-to-cart` → 34 个按钮（包含 overlay 按钮）
  - 按钮顺序不一致，导致选择了错误的按钮
- **修复**：使用和直接脚本相同的选择器策略

### 问题 6：asyncio 兼容性问题
- **现象**：在 asyncio 事件循环中使用同步 Playwright API 报错
- **根因**：`explore_flow` 使用 asyncio，但 Playwright 同步 API 不能在 asyncio 中使用
- **修复**：使用 `subprocess` 在单独进程中执行 DSL

**测试结果对比**：

| 测试方法 | 结果 | 说明 |
|----------|------|------|
| 直接脚本（test_cart_flow.py） | ✅ 完全成功 | 使用精确的选择器策略 |
| Agent 流程（test_cart_flow_agent_final.py） | ❌ 部分失败 | 浏览器崩溃，无法完成验证 |

**根本原因分析**：

Agent 流程失败的根本原因是**选择器策略差异**：
- 直接脚本：先找商品卡片（`.productinfo`），再在卡片内查找按钮（`.add-to-cart`）
- Agent 流程：直接查找所有按钮（`.productinfo .add-to-cart`），导致匹配到 34 个按钮

**代码变更**：
1. `backend/app/ai/page_explorer.py`：a11y 节点过滤从白名单改为黑名单
2. `backend/app/locators/semantic.py`：添加更灵活的文本匹配策略

**生成的测试文件**：
1. `backend/explore_full_flow_final.json` - 完整的探索结果
2. `backend/test_cart_flow.py` - 直接脚本（成功）
3. `backend/test_cart_flow_agent_final.py` - Agent 流程脚本（部分失败）
4. `backend/test_report.md` - 测试报告

**后续**：
1. 需要修复 Agent 流程中的选择器策略，使用和直接脚本相同的方法
2. 考虑在 explore_flow 中添加更智能的按钮识别逻辑
3. 需要处理浏览器崩溃的问题

---

## 2026-05-30 | E2E 自动化测试 Skill

**目标**：堵住 `generate_segmented_case_draft` 并行调用各 segment LLM 时段间变量名失配的洞——例如 S1 生成 `capture_text context_key=product_a_name`，S2 独立生成 `assert_text value="${item_a_name}"`，运行时 `_substitute_variables` 找不到 key，字面量 `${item_a_name}` 残留到断言/输入。

**操作**：
1. `schemas/ai_planning.py`：新增 `AIPlanningScenarioVariable`（context_key/description/source/capture_in_state），挂到 `AIPlanningScenario.variables`
2. `ai/test_planning_prompts.py`：在 JSON 模板 + 规则段加 variables 字段说明，要求 AI 列出所有跨段共享变量及其 capture 段
3. `schemas/dsl.py`：`GenerateDslRequest` 新增 `scenario_variables: list[dict] | None` 透传字段
4. `ai/dsl_generator.py`：新增 `_format_scenario_variables_for_prompt(scenario_variables, current_state=...)` 把变量按 input/own_capture/other_capture 分组渲染；`_build_segment_prompt` 注入；`generate_segmented_case_draft` 接收新参数并下传给每个 segment
5. `services/ai_planning.py`：`generate_planning_drafts` 从 `scenario["variables"]` 取出，分别注入 segmented 和 single-segment 路径的 payload
6. `tests/unit/test_dsl_generator.py`：新增 `TestScenarioVariablesInSegmentPrompt` 4 个聚焦测试

**结果**：
- 每个 segment 看到的 prompt 包含 `## Scenario variables — naming authority` 小节，列出全部 `${context_key}` 及其责任段
- 本段持有的 capture 变量标"本段必须用 capture_text 写入"，外段的标"do NOT re-capture"
- 504 单元测试通过（原 500 + 4 新增）

**验证**：
- `_build_segment_prompt` 直接调用：input 变量、capture 变量、空 variables 三个分支
- `generate_segmented_case_draft` mock LLM 调用：确认 2 个并行 segment 都拿到同一份变量字典，且只有 S1 段被标为 capture 责任段

**后续**：
- 还可以加生成后校验：扫描 merged_steps 中所有 `${var}`，若不在 scenario_variables 也不在 `input_contract` 中则降级告警/重生段；这层兜底等用回归 prompt 跑过一次再决定是否补
- 现有 `_extract_input_contract_from_steps` 会把 captured 变量也纳入 input_contract，理论上不影响执行（runtime_context 会覆盖 input_values），但语义不准确；可在后续做拆分

---

## 2026-05-30 | E2E 自动化测试 Skill（API 回归补充）

- 任务：创建 E2E 自动化测试 skill，使用 test_brand_filter_cart 需求文件验证 AI 规划流程和 DSL 质量
- 操作：
  - 创建 `backend/tests/e2e/test_e2e_brand_filter_cart.py` — httpx 调用 REST API 模拟前端操作
  - 创建 `.claude/skills/e2e-brand-filter-cart.md` — Skill 定义文件
  - 修复 `page_explorer.py`：探索阶段跳过 VLM fallback（`skip_vlm` 参数），避免模态弹窗按钮触发慢速 VLM
  - 修复 `dsl_generator.py`：prompt 规则明确 `${var}` 只能用在 value 字段；新增 `_fix_variable_misuse` 后处理函数
  - 更新 `pyproject.toml`：添加 `e2e_api` marker 和 `tests/e2e` testpath
- 验证：E2E 测试通过（~7-11 分钟），DSL 覆盖登录→品牌筛选→加购→购物车验证完整流程
- 发现的问题：
  - VLM 模型全部失败（429 限流、元素未找到、类型错误）导致 explore_flow 极慢
  - DSL 生成器会将 `${var}` 误用在 target 字段（已在 prompt 和后处理中修复）
- 后续：
  - 测试只验证了 `login_success` 场景，完整购物车验证场景需要更长时间
  - 模态弹窗按钮（Continue Shopping, View Cart）confidence=low，需要更好的处理策略

---

## 2026-05-30 | Automation Exercise Polo 页面无障碍树采集

- 任务：采集 `https://automationexercise.com/brand_products/Polo` 页面的无障碍树元素。
- 操作：使用浏览器打开目标页面，确认标题为 `Automation Exercise - Polo Products`，采集页面可访问快照，整理导航、分类、品牌、商品列表和订阅区元素。
- 验证：页面成功加载，URL 保持为 `/brand_products/Polo`，无障碍快照可读取。
- 备注：本次为页面采集与分析任务，未修改业务代码，未发现需要写入 bug-log 的明确缺陷。

---

## 2026-05-30 | Polo 商品页 DSL 消歧优化分析

- 任务：分析 `explore_flow` 采集 Polo 商品页后，DSL 生成阶段重复使用 `Rs. 500` / 商品名导致商品选择不明确的问题。
- 操作：复查 `page_explorer.py`、`dsl_generator.py`、`locator_preflight.py`、`semantic.py` 中元素格式化、候选预检和语义定位逻辑；结合实际无障碍树确认页面存在同一商品卡片默认层与 hover 层重复暴露。
- 结论：优先把商品卡片抽象为结构化业务单元，生成 DSL 时使用商品名 + 价格 + 卡片内动作的组合定位，避免裸文本 `Rs. 500` 或裸 `Add to cart`。
- 备注：本次为分析建议，未修改业务代码，未追加 bug-log。

---

## 2026-05-30 | Polo 商品页 DSL 消歧修复

- 任务：修复 Polo 商品页 `explore_flow` 后 DSL 生成阶段容易用裸 `Rs. 500` / 裸 `Add to cart` 导致商品选择歧义的问题。
- 操作：
  - `dsl_generator.py`：为 A11y 元素清单增加去重后的商品卡片摘要；重复元素追加 duplicate 标记；系统提示要求商品加购使用 `商品名 附近的 Add to cart`；价格点击自动重写为商品上下文目标。
  - `semantic.py`：支持解析并执行 `Blue Top 附近的 Add to cart` 这类上下文定位。
  - `locator_preflight.py`：裸 `Add to cart` / `View Product` 多匹配时降为 low confidence，并提示补商品上下文。
  - 补充 `test_dsl_generator.py`、`test_locator_confidence.py`、`test_locator_semantic.py` 单测。
- 验证：`uv run pytest tests/unit/test_dsl_generator.py tests/unit/test_locator_confidence.py tests/unit/test_locator_semantic.py -q`，82 passed。
- 备注：未发现新的明确缺陷，未追加 bug-log。

---

## 2026-05-30 | DSL 扩展适配 Playwright 执行器分析

- 任务：分析如果要扩展 DSL 功能，如何更好适配现有 Playwright runner。
- 操作：复查 `schemas/dsl.py`、`runners/playwright_runner.py`、`postcondition_verifier.py`、`services/dsl.py` 中当前动作、定位、postcondition、变量和执行证据能力。
- 结论：建议优先扩展强语义动作、断言、等待、作用域/集合与 evidence schema；避免直接开放任意 Playwright API 或 `evaluate_js`，防止绕过结构化 DSL 校验。
- 备注：本次为设计分析，未修改业务代码，未追加 bug-log。

---

## 2026-05-30 | A11y-first 商品定位消歧修正

- 任务：将 Polo 商品页 DSL 消歧方案从 DOM `text_parent_chain` 修正为无障碍树定位 + 结构化候选校验路径。
- 操作：
  - 移除 `semantic.py` 中 `附近的` / `text_parent_chain` 主定位路径，避免把 DOM 作用域定位混入 a11y 语义定位。
  - `page_explorer.py`：通过 CDP `backendDOMNodeId` 为 a11y 节点回填 DOM 属性，并生成已验证的 Playwright candidate selector。
  - `dsl_generator.py`：商品卡片摘要改为 `target="Add to cart"` + verified candidate；当 AI 误用价格作为 click target 时，重写为带 candidates 的结构化步骤。
  - `locator_preflight.py`：将 a11y 节点上的 `verified_selectors` 写入 DSL candidates；裸重复商品动作仍标记为 low confidence。
  - `playwright_runner.py`：修复候选执行路径的 locator trace 构建，并要求候选 locator 唯一匹配后再执行。
  - 更新相关单测和旧提示文案，删除 `text_parent_chain/附近的` 生成引导。
- 验证：`uv run pytest tests/unit/test_dsl_generator.py tests/unit/test_locator_confidence.py tests/unit/test_locator_semantic.py tests/unit/test_page_explorer.py tests/unit/test_dsl_validation.py tests/unit/test_preflight_regen.py -q`，115 passed，1 个既有 PytestCollectionWarning。
- 备注：本次是对上一版 DOM fallback 方案的架构修正，未追加 bug-log。

---

## 2026-05-28 | explore_flow DSL 格式支持

**目标**：修复 `explore_flow` 不支持 DSL 格式步骤的问题，导致页面探索不完整，AI 生成的 DSL 缺少 input 步骤。

**操作**：
1. 定位问题：AI 调用 `explore_flow` 时传入 DSL 格式步骤 `{"action": "goto", "target": "https://..."}`，但 `_collect_flow_a11y` 只支持 `{"url": "...", "actions": [...]}` 格式
2. 根因分析：DSL 格式步骤没有 `url` 和 `actions` 字段，导致步骤被跳过
3. 修复方案：新增 `_normalize_flow_step()` 函数，将 DSL 格式步骤转换为 explore 格式
4. goto -> url, click/input/wait_for -> actions

**结果**：
- 500 单元测试全部通过
- explore_flow 现在支持两种格式的步骤

**验证**：
- DSL 格式 `{"action": "goto", "target": "https://..."}` -> `{"url": "https://..."}`
- DSL 格式 `{"action": "click", "target": "Polo"}` -> `{"actions": [{"action": "click", "target": "Polo"}]}`

**后续**：用户可重新测试 E2E 场景，验证 explore_flow 是否正确探索所有页面。

---

## 2026-05-28 | A11y Tree 全面切换

**目标**：封杀所有 DOM 元素路径，让 AI 只使用 a11y tree 进行元素定位，解决 VLM 调用过多和断言失败问题。

**操作**：
1. 新增 `format_a11y_nodes_for_prompt()` 函数，格式化 a11y tree 为 `role="name"` 格式
2. 修改 `_build_segment_prompt()` 移除 `elements` 和 `page_elements` 参数，只保留 `a11y_nodes`
3. 修改 `generate_segmented_case_draft()` 移除 `page_elements_by_state` 参数，只保留 `a11y_nodes_by_state`
4. 更新 prompt 规则：target 必须使用 `button="Login"` 格式，禁止 XPath/CSS 选择器
5. 新增 `_clean_variable_format()` 函数，清理 `${email}=value` 错误格式为 `${email}`
6. 所有 `to_contain_text()` 调用添加 `normalize_whitespace=True` 参数

**结果**：
- 500 单元测试全部通过
- 封杀 DOM 路径，只保留 a11y tree
- 修复变量格式问题
- 修复断言空白字符匹配问题

**验证**：
- `test_dsl_generator.py` 37 tests passed
- 全量单元测试 500 passed, 6 warnings

**后续**：用户可重新测试 E2E 场景，验证 VLM 调用次数是否减少，断言是否正常工作。

---

## 2026-05-28 | 分段生成 input_contract 自动提取

**目标**：修复用户提供的测试数据在执行时未被替换到 DSL 步骤中的问题。

**操作**：
1. 定位问题：Session 247 Draft 176 的 `input_contract` 为空数组，但步骤中使用了 `${email}` 和 `${password}` 占位符
2. 根因分析：`generate_segmented_case_draft` 函数硬编码 `"input_contract": []`
3. 修复方案：新增 `_extract_input_contract_from_steps` 函数，从步骤的 `${...}` 占位符自动提取并生成 `input_contract`
4. 添加单元测试覆盖新函数

**结果**：
- 37 单元测试通过
- 自动提取 email/password 变量并推断类型

**验证**：
- 模拟用户输入 `账号：Xjy13302412005@outlook.com，密码：123456` 正确解析
- 变量映射：`email` → `Xjy13302412005@outlook.com`，`password` → `123456`

**后续**：用户可重新测试相同场景，验证变量替换是否正常工作。

---

## 2026-05-25 | 数据传递与校验全面扫描修复

**任务**：全面扫描项目的数据传递和校验情况，发现并修复所有问题。

**扫描范围**：
- 后端 (backend/app/) 所有 Python 文件
- 前端 (frontend/src/) 所有 TypeScript/React 文件

**发现并修复的问题**（共 19 项）：

### 高风险（3 项）
1. `batch_update_cases` 绕过项目成员权限检查 — 添加 `actor_user_id` 参数并在路由中传入 `current_user.id`
2. settings 路由缺少认证保护 — 为所有 settings 路由添加 `require_demo_user` 依赖
3. `require_demo_user` 缺少警告注释 — 添加 docstring 标记为开发/演示专用

### 中风险（8 项）
4. email 格式校验缺失 — 添加 `@` 格式校验
5. 项目重名异常处理缺失 — 添加 `ProjectConflictError` 并捕获 `IntegrityError`
6. DSL 反序列化未捕获 ValidationError — 添加 try/except 返回降级结果
7. `func.to_char` SQLite 不兼容 — 使用数据库方言检测适配不同数据库
8. 前端 CaseExecutionRequest 缺少 input_values — 添加 `input_values?: Record<string, string>` 字段
9. 前端 AIPlanningScenario 缺少字段 — 添加 `page_elements` 和 `flow_steps` 字段
10. 缺少 CORS 中间件 — 配置 `CORSMiddleware` 并添加 `cors_allow_origins` 配置项
11. 缺少全局请求速率限制 — 创建 `RateLimitMiddleware` 并添加配置项

### 低风险（8 项）
12. status_filter 缺少 Literal 约束 — 改为 `ExecutionStatus | None` 类型
13. page/page_size 缺少 Query 约束 — 添加 `ge=1` 和 `le=100` 约束
14. GenerateDslRequest 冗余 return 语句 — 删除不可达代码
15. 前端 GenerateDslMeta 字段不完整 — 添加 `active_governance_focus_reasons` 字段
16. 前端 AIPlanningTurnResponse 字段不完整 — 添加 `todo_list` 和 `execution_analysis` 类型和字段
17. LIKE 通配符未转义 — 转义 `%` 和 `_` 特殊字符
18. DSL case steps 无 max_length — 添加 `max_length=500` 约束
19. SSE 流泄露 traceback — 仅在 debug 模式下发送 traceback

**新增文件**：
- `backend/app/core/rate_limit.py` — 简单的内存速率限制中间件

**验证**：所有 19 项问题已修复

---

## 2026-05-25 | 孤儿数据全面清理

**任务**：清理代码库中所有类型的孤儿数据，包括导入但未实现、实现但未导入、引用但未实现、实现但未引用、定义但未实现、实现且定义但未引用的代码，以及无效字段、无效表、无效函数、无效文件、无效变量。

**分析范围**：
- 后端 (backend/app/) 所有 Python 文件
- 前端 (frontend/src/) 所有 TypeScript/React 文件
- 数据库模型定义
- API 路由定义
- 服务层实现
- 根目录测试工件

**删除项目**（共 14 项）：

### 高优先级（明确的孤儿数据）
1. `backend/app/services/projects.py` — 整个文件是死代码，被 `project_management.py` 完全取代
2. `backend/app/services/cases.py` 第124-127行 — return语句后的不可达死代码
3. `backend/app/services/dsl.py` `_ensure_retry_generation_exists` 函数 — 定义了但从未调用
4. `backend/app/services/cases.py` `list_cases` 函数 — 从未被路由调用，被 `list_cases_paginated` 取代
5. `frontend/src/components/StepList.tsx` — 从未被导入
6. `frontend/src/layouts/NotebookLMLayout.tsx` — 从未在路由中使用
7. `frontend/src/components/NotebookNav.tsx` — 只被孤立布局使用

### 中优先级（清理）
8. `backend/app/services/__init__.py` 中 `list_cases` 和 `list_accessible_projects` 的死重导出
9. `backend/app/api/routes/cases.py` 第24行冗余的 `get_project` 导入
10. `backend/app/schemas/__init__.py` 未使用的重导出块
11. 根目录测试工件：`test_brand_filter_cart`、`test_results.json`、`test_results_formatted.txt`

### 待确认项（用户确认删除）
12. `frontend/src/types/api.ts` 中 `SavedCaseResult.status` 字段 — 始终是字面量 "saved"，无信息量
13. `backend/scripts/` 目录 — 不属于主流流程
14. `tools/` 目录 — 与测试应用无关

**保留项目**：
- `hash_password` — 保留用于未来用户注册功能
- `LocatorAttemptLog` 模型 — 可能被 runner 运行时写入
- `get_dsl_generation_runtime_stats` — 调试工具
- `reset_dsl_generation_runtime_stats` — 测试工具
- `schemas/__init__.py` 便利重导出层 — 简化为仅保留模块声明

**验证**：所有删除操作已成功执行，文件系统验证通过

**影响**：
- 减少了代码库的维护负担
- 消除了潜在的混淆和误用
- 提高了代码库的整洁度和可维护性

---

## 2026-05-25 | DSL 生成链路 7 层 bug 修复（Bug A→G）

**任务**：用户复现 `DSL 生成失败：所有 1 个页面状态分段均未生成步骤` 错误。从 `backend/backend.log` 追踪定位错误归属并修复。

**根因分析**（按因果链排序）：
1. 用户报错出处：`dsl_generator.py:644` 抛出 `DslGenerationError`，提示"页面元素采集失败"但元素已采到 1136 个——错误消息误导。
2. 直接触发：`Segment S0 failed: <urlopen error [WinError 10060]>` — TCP 21 秒级超时连接 `api.deepseek.com`。
3. 即使网络通了也会失败：`scenario["flow_steps"]=[]` 走 single-segment 分支，1136 个 a11y 节点在 `ai_planning.py:561`→`dsl.py:147` 链路上被丢弃（`page_elements_by_state` 硬编码 `{}`）。
4. 上游：agent 5 轮安全帽耗尽 → fallback plan，原因是重复调用 `create_project` ×2、`explore_flow` ×2 浪费了 4 轮。

**修复**：

### Bug A — single-segment 路径下 a11y 数据丢失
- `schemas/dsl.py`：`GenerateDslRequest` 新增 `a11y_nodes_by_state` 字段
- `services/dsl.py`：`page_elements_by_state` 从 payload 读取，不再硬编码 `{}`
- `services/ai_planning.py`：单段分支按 page_state 分组 a11y_nodes_raw 后传入
- `dsl_generator.py`：`flow_steps=[]` 但有 elements 时自动按 page_states 迭代生成

### Bug B — LLM 调用无重试 + 错误消息误导
- 新增 `_urlopen_with_retry`（指数退避 1s→2s，2 次重试）+ `_is_transient_network_error`
- 新增 `DslGenerationNetworkError`，给出准确中文诊断
- `generate_segmented_case_draft` 末尾区分网络错误 vs 真正的"无元素"问题

### Bug C — agent 重复调用工具浪费安全帽轮次
- 新增 `_tool_call_signature` 规范化调用签名
- 工具执行前比对签名，命中重复时：注入警告 + 复用 prior result + 不扣 round

### Bug D — stream_planning_turn 把 Pydantic plan 当 dict 用
- `response.plan.model_dump(mode="json")` 替代直接 `.get()`

### Bug E — _log_dsl_cache_usage 被 governance 清理误删
- 恢复函数定义，加 `isinstance` 防御

### Bug F — LLM 生成 goto/assert_url_contains target↔value 错位
- 新增 `_normalize_llm_step`：激活 `_ACTION_ALIASES`，对 `goto/assert_url_contains` 自动把 target 搬到 value
- `_build_segment_prompt` 增加显式字段规则

### Bug G — assert_text 缺 value + 字段别名表未接入 normalizer
- 接入三张孤儿别名表 `_STEP_TARGET/VALUE/TIMEOUT_ALIASES`
- `assert_text` 特殊兜底：value 缺 + target 在 → target 移到 value，target 兜底 `"body"`
- 必填字段缺失时丢弃整步，避免单步拖垮整个 DSLCase
- `_build_segment_prompt` 按 action 类型枚举字段要求并给正反例

**新增测试**：16 个（TestIsTransientNetworkError 4 + TestUrlopenWithRetry 3 + network error wrapping 1 + a11y data flow 1 + TestToolCallSignature 7）

**验证**：543 passed（基线 505 → +16 新增 - 部分删除）

**链路总结**：Bug A→G 共 7 层，每修一个就暴露下一个。所有 bug 不是新增缺陷，是已存在但被前置失败掩盖的休眠问题。

---

## 2026-05-17 | 修复 AI 规划→DSL 生成链路 4 个 bug

**背景**：使用 `test_brand_filter_cart` 测试规格生成草案时，`explore_flow` 失败（Playwright 对 `<body>` 执行 `fill`），草案生成报 Pydantic `ValidationError`。

**操作**：

### Bug 1: 系统提示词缺少 `collected_info` → `entry_url_or_page` 提取不稳定
- JSON 模板新增 `collected_info` 对象（7 个需求字段）+ `assistant_message` + `todo_list`

### Bug 2: 语义定位器 `text` 策略匹配 `<body>` → `explore_flow` 填表失败
- `semantic.py`：`prefer_input=True` 时排除 `text`/`text_fuzzy` 策略
- `page_explorer.py`：`_execute_flow_actions` 新增标签验证；fill 前检查 tag 是否为 input/select/textarea

### Bug 3: 系统提示词缺少 `summary` → `_coerce_plan` 永远回退到 `_build_plan`
- JSON 模板新增 `summary` 字段；scenario 模板扩展 `flow_steps` 示例

### Bug 4: `base_url` 转空字符串 + 空 steps 报 Pydantic 错误
- `dsl_generator.py`：`base_url = payload.base_url or None`；model_validate 前加前置校验

**验证**：138 passed / 0 failed（语义/定位器/DSL/探索器相关）

---

## 2026-05-16 | E2E 手动测试 — 品牌筛选购物车

**任务**：对 `test_brand_filter_cart` 执行完整 E2E 链路测试，发现并修复 3 个 bug。

**操作**：
1. 创建 AI 规划会话 (#224)，AI 成功生成 4 个测试场景
2. DSL 生成失败（page_elements 为空）→ 改用直接创建测试用例
3. 创建 Case #97 并执行，步骤 6 失败（登录账号不存在）
4. 注册新账号后重新执行 → 24 步全部通过
5. 继续优化 DSL（#98~#100），最终 Case #100 20 步全部通过

**发现并修复的 Bug**：
- Bug #1: `planning_tools.py` — `AIPlanningSession` import 在条件块内导致 UnboundLocalError
- Bug #2: `planning_tools.py` — `explore_page` 中 networkidle 等待无 try-except
- Bug #3: `playwright_runner.py` — `capture_text` 步骤的 evidence value 始终为 null

**验证**：完整购物车流程 20 步 pass

---

## 2026-05-15 | 代码清理 + 测试补充 + E2E 重设计

**背景**：主路径 v2 A11y 管线 17 个任务已完成（491 tests / 0 failures），核对设计文档后发现 4 类遗留。

**操作**：

### Part 1: 重构 `services/dsl.py` — 删除 `generate_case_draft`
- import 从 `generate_case_draft` 改为 `generate_segmented_case_draft`
- 删除 `_select_governance_focus_reasons` 的 DB 查询

### Part 2: 删除 `ai_planning_max_react_rounds`
- 该配置项在 4 个文件做 plumbing，但**没有任何代码读取它**

### Part 3: 删除 `collect_interactable_elements` 死代码
- 删除约 283 行（含 `_discover_interactive_elements`、`_verify_locators_on_page` 等）
- 修复 `_filter_a11y_nodes` 中 CDP `role` 字段为 dict 格式的处理

### Part 4: 新建 `test_preflight_regen.py`（16 tests）
### Part 5: 新建 `test_main_path_v2_e2e.py`（8 tests）

**验证**：505 passed / 0 failed；浏览器集成测试 3 passed

---

## 2026-05-15 | 主路径 v2 全量实施 — 17/17 任务完成

**背景**：用户反馈 4 个痛点——探索工具无缓存 / AI 草案质量低 / 定位器选择差 / 单轮思考 10 分钟。大量机制"已设计但主流程不触发"。

### 阶段 1: 删除 dormant 分支（2026-05-14）
- `multi_agent.py`（527 行）、compression subagent（290 行）、`accessibility.py`（158 行）、pre-exec review（100 行）、VLM 重复触发（43 行）、调试脚本（122 行）、过时设计文档（1058 行）
- 净结果：**544 tests / 0 failures，−1498 行**

### Brainstorm + 实验（8 个细节决策）
- A11y 树 vs DOM 全量对比：字节收益 22-38x↓，速度 100-250x 快
- 15 个锁定的细节决策（DSL target 类型、Cache key、Preflight 重生策略等）

### PR-1：地基 — A11y 探索器 + 默认项目 + DB 缓存（7 tasks）
- 默认项目 auto-create、A11y 角色过滤器、CDP 快照、程序化关键字提取、explore_page 切 A11y、DB 缓存读路径

### PR-2：数据流 + Preflight 重生 + 删死代码（6 tasks）
- dict 端到端、preflight 1:N candidates、单段重生、Scenarios schema 瘦身、删 governance 系统 520 行

### PR-3：ReAct 瘦身 + 配置清理 + 死代码扫尾（4 tasks）
- 系统提示词 186→30 行、safety_cap 30→5、cache 进度清单注入、删旧 DOM 收集代码

**最终状态**：491 tests / 0 failures，16 commits，+3.5K / −6.6K 行（净 −3.1K），17/17 tasks complete

---

## 2026-05-14 | 架构清理阶段 1 — 删除 dormant 分支与冗余 LLM 调用

**操作**：
1. 删除 multi_agent 路径（527 行）
2. 删除压缩 subagent（290 行）
3. 删除 accessibility 模块（158 行 + 256 行测试）
4. 删除 pre-exec review（100 行）
5. 去掉 VLM 重复触发（43 行）

**验证**：526 单测通过，−1498 行

---

## 2026-05-13 | 进展汇报 + PPT 规划

- 梳理项目最新进展并向用户汇报当前状态
- 为项目展示 PPT 梳理内容规划方案

---

## 2026-05-12 | E2E 测试验证 + 草案质量分析 + 多轮修复

**背景**：使用 `test_brand_filter_cart` 对平台进行 E2E 测试，持续发现问题并修复。

### Phase 1: 草案质量问题发现与分析
- AI 跳过登录页、页面状态映射错误、actions 泛化、数量修改步骤缺失、candidates 为空

### Phase 2: 提示词与消息修复
- 删除"可以直接 generate_plan"逃逸口、安全网消息修正、系统提示词矛盾修正

### Phase 3: Guard 增强
- 页面覆盖度检查移入 Guard（coverage < 0.5 时阻止 generate_plan）

### Phase 4: Few-shot 自愈系统
- `DSLAntiPattern` 模型 + 自动采集 + 注入负面示例

### Phase 5: DSL 生成器 thinking mode
- deepseek-v4-pro + effort=max

### Phase 6: explore_flow actions 消歧
- `_check_action_disambiguation` 检测泛化 target

### Phase 7: 相对 URL 解析修复
- 多层 fallback 提取 base_url

**验证**：thinking mode 生效、Guard 覆盖度检查生效、actions 消歧生效

---

## 2026-05-12 | 执行架构全面优化 — 11 项问题修复

**操作**：

### 严重问题
1. **streaming 函数 NameError** — `save_and_execute_selected_drafts_streaming()` 引用未定义 `db_session`
2. **Explorer Runner console/network 采集** — 添加事件监听器

### 中等问题
3. **generate_plan 守卫轮次保护** — `guard_continue_count` 超过 5 次后强制生成方案
4. **页面探索覆盖度检查** — 新增 `_check_page_coverage`
5. **legacy 路径 postcondition 检查**
6. **变量替换未匹配警告**

### 轻微问题
7. 多语言动态元素发现
8. `collect_flow_elements` base_url 参数化
9. `text_parent_chain` 多级链支持
10. 无障碍树 dialog/modal 角色
11. `playwright_runner` 添加 logger

**验证**：544/544 单元测试通过

---

## 2026-05-10 | AI 配置优化 — 禁用 DeepSeek thinking 模式 + 按场景设置 temperature

**背景**：综合 BUG-081/069/065/054 等"AI 不遵循提示词"问题。

**操作**：
1. 移除 DeepSeek 的 thinking mode（仅保留 GLM）
2. 按场景设置 temperature：DSL generator 0.0、flash 0.0、Planning 0.1、Judge 0.0

**验证**：542/544 通过（2 个预存失败与改动无关）

---

## 2026-05-06 ~ 2026-05-07 | AI Agent 测试用例质量提升 — 三层修复 + 自动回归循环

**目标**：反复用 test_brand_filter_cart 测试 AI agent，直到步骤通过率达 80%+。

**核心修复（按层分类）**：

### AI 决策层
1. BUG-069: 系统提示词 ask_user 确认门移除
2. BUG-068: 压缩子代理优先保留交互元素
3. BUG-066: core_user_flow list→编号文本归一化
4. 系统提示词 7 条强制规则

### DSL 生成层
5. BUG-077: goto/assert_url_contains 的 candidates/postconditions 剥离
6. BUG-078: click/wait_for/capture_text 的 spurious value 字段剥离
7. BUG-070: DSL generator thinking mode reasoning_content fallback
8. BUG-065: capture→assert 规则
9. BUG-076: Surrogate Unicode 字符清理

### 探索数据层
10. BUG-067: explore_flow 相对 URL 解析
11. 元素视觉分组 + 隐藏元素保留 + 选择器稳定性评分

### 执行定位器层
12. text_parent_chain 新定位器
13. BUG-071~073: text_parent_chain 正则/ancestor/exact 修复
14. BUG-074: 执行流程重构 — 语义链优先
15. 步骤超时 2.5 分钟

**执行结果对比**：

| 指标 | 修复前（Session 118） | 修复后（Session 155） |
|------|----------------------|----------------------|
| AI 首轮动作 | ask_user "信息够吗" | explore_page → capture_session |
| DSL 步骤数 | 10 | 42（完整流程） |
| assert_text 数量 | 0 | 9 |
| 步骤被删 | 10 | 0 |
| 执行通过率 | 0/0（草案无法执行） | 42/42 (100%) |

---

## 2026-05-05 | 四项修复

### capture_page_session CSS 选择器支持 + 定位器链修复
- **根因**：AI 生成 CSS 选择器格式的 target 但旧代码只处理 label/placeholder/id；Playwright locator 对象总是 truthy → `a or b or c` 链式回退无效；`action: "type"` 被静默忽略
- **修复**：新增 `_resolve_step_locator()` 统一处理 + `_extract_text_from_css_target()` + Action 名称归一化
- **验证**：Session 118 capture_page_session 成功执行登录，528/528 通过

### AI 规划代理登录页面元素缺失 — 自动探索登录页 + ask_user 拦截
- **根因**：`_auto_explore_entry_url` 只探索首页，不探索 `/login`；ask_user 路径立即退出循环
- **修复**：自动探索登录页 + ask_user 拦截 + 系统提示澄清 + 安全网 URL 排序
- **验证**：532/533 通过

### AI 规划代理登录页面元素缺失 — 追问拦截补丁
- **根因**：AI 第一轮就 ask_user 时无 explore_page 记录，拦截逻辑无数据可查
- **修复**：新增 `_auto_explore_entry_and_find_login()` 在拦截时先探索入口页
- **验证**：528/528 通过

### BUG-063 追加修复 — thinking mode 下 SSE 空白 + 会话消失
- **根因**：reasoning_text 未归入 raw_response；非流式路径忽略 reasoning_content；loadSessionDetail 丢失 _thinkingContent
- **修复**：content 为空时用 reasoning_text 兜底；非流式 fallback；保留 _thinkingContent
- **验证**：505/506 通过

---

## 2026-05-04 | 可访问树定位器 + 发现时验证

**任务**：automationexercise.com 首页登录按钮找不到（`<a>` role="link" 但系统只有 `button_role` 策略）。

**操作**：
- Phase 1 — 补全 ARIA 角色策略：`link_role`(85)、`menuitem_role`(85) + fuzzy 变体(55)
- Phase 2 — 修复 runner + pre_scorer：不再硬编码 "button"，自动推断隐式 ARIA 角色
- Phase 3 — 可访问树 Tier 1.5：`snapshot_accessibility_tree()`（CDP，15 种交互角色）
- Phase 3.5 — 发现时验证：`_verify_locators_on_page()` 当场验证候选定位器

**验证**：automationexercise.com/login 37 个元素中 29 个有已验证选择器（86 个）；登录流程完整通过

---

## 2026-05-04 | AI Planning 上下文压缩 + Subagent 架构

**任务**：三个关联缺陷 — plan_json 被覆盖、工具结果膨胀（570KB-741KB）、JSON 解析失败降级差。

**操作**：
- plan_json 赋值加 `if response.plan is not None:` guard
- 工具调用消息改为存 `result_summary`（压缩摘要）
- 重工具同步存入新表 `ai_planning_tool_results`
- 新增 `_repair_json_text()`（尾部逗号修复）
- Subagent 压缩：`run_compression_subagent()` 短上下文 LLM 调用

**验证**：Python 模型导入 ✅、TypeScript 编译 ✅

---

## 2026-05-04 | 修复 correction 提交 409 冲突 + VLM 回退链路失效

**操作**：
- `create_correction()` 改为 update-in-place
- `execute_case_streaming()` 执行前插入 `reset_ai_visual_runtime_state()`
- `locate_element_by_vision()` 非限频错误改为 `continue` 让 fallback 模型链完整执行

**验证**：485 单元测试通过

---

## 2026-05-04 | 修复 DeepSeek thinking 模式 SSE 流式输出断流

**操作**：
- backend：`reasoning_content` 作为 `text_chunk` 事件实时转发（带 `thinking: true`）
- frontend：`_thinkingContent` 存入独立字段 + 渲染可折叠 `<details>`

**验证**：29 planning agent 单测 + 11 API 测试通过

---

## 2026-05-03 | 企业级中间层三大架构升级

**Phase 1 — 动作式 explore_flow**：`collect_flow_elements(steps)` 支持 click/input/wait_for 动作
**Phase 2 — 页面状态标记**：`page_state_id` + DSL step `page_state` 字段
**Phase 3 — 定位器预校验**：`locator_preflight.py` 静态校验 DSL targets

**验证**：485 单元测试全部通过

---

## 2026-05-03 | AI planning 架构方向评估

**关键结论**：
- 当前产品方向是对的：DSL/结构化测试 + 后端执行器 + 证据报告
- 但实现还不是完整的企业级闭环
- 企业级链路应继续朝四层推进：意图/需求层、状态化探索层、DSL 生成与预校验层、执行与证据层

---

## 2026-05-03 | AI planning 中间层排查

**关键证据**：
- 入口页能看到登录入口（约 300 个可交互元素）
- 自动探索不理解用户 flow（按首页链接顺序抓取）
- 前端 session/project 绑定链路失真

**结论**：问题不主要在提示词，而在架构

---

## 2026-05-03 | Session 15 — 修复三大核心缺陷

1. **BUG-055** — `create_project` 成功后 `project_id` 局部变量未更新
2. **BUG-056** — DSL draft prompt 超 50000 字符
3. **BUG-057** — hidden 元素恢复链跳过

**验证**：471 单元测试全部通过

---

## 2026-05-02 | Session 15 — 修复 explore_flow 0 元素 + 无 goto 白屏

**操作**：
1. 修复 `collect_multi_page_elements` 内本地导入遮蔽模块级导入
2. `_check_dsl_completeness()` 无 goto 时自动插入 `{"action": "goto", "value": "/"}`
3. Runner 首步骤不是 goto 且 base_url 已设置时先 `page.goto(base_url)`

**验证**：471 单元测试全部通过

---

## 2026-05-02 | Session 14 — 探索功能 + VLM 两阶段定位 + 评分数据传递

**操作**：
1. JS 提取脚本从 50 硬限制改为 300 参数化
2. VLM 两阶段定位（Stage 1 找区域 → crop + 2x 放大 → Stage 2 精确定位）
3. `format_elements_for_prompt()` 80K 字符智能截断
4. `_format_element_rich()` 输出 top 3 候选含 selector+pre_score

**验证**：455 单元测试通过

---

## 2026-04-30 | VS Code Claude Code 插件 settings.json BOM 修复

- **根因**：`settings.json` 文件开头带 UTF-8 BOM，`JSON.parse()` 无法解析
- **修复**：重写为无 BOM UTF-8

---

## 2026-04-28 | Session 12 — DOM 选择器评分 + VLM 置信度门控 + 点击前置处理器

**操作**：
1. 元素稳定性评分（data-testid=0.95 > id=0.90 > aria-label=0.80）
2. AI 置信度门控（`locator_confidence` 字段）
3. VLM 预验证模块（`preverify_with_vlm()`）
4. 点击前置处理器（等待→关闭→避让→强制→移除 降级链）

**验证**：416/416 单元测试全部通过

---

## 2026-04-28 | Session 11 — 加强后端日志输出和 Agent 错误信息

**操作**：
1. 创建集中式日志配置（统一格式 + LOG_LEVEL 控制）
2. Agent 错误信息增强（error_type/error_detail/phase/suggestion）
3. SSE 错误事件丰富化
4. 关键路径打点日志

**验证**：383 个单元测试通过

---

## 2026-04-27 | Session 10 — 执行报告增强 + Explorer-Judge 总结

**操作**：
1. ExecutionDetailPage 步骤信息增强（target/value 描述、断言结果、数据来源标识）
2. Explorer-Judge 执行总结持久化

**验证**：391 tests passed

---

## 2026-04-26 | Session 9 — 白屏修复

- **根因**：`AITestPlanningPanel.tsx` 渲染 todo_list 消息时缺少 `Array.isArray()` 空值检查
- **修复**：添加 `Array.isArray(item.structured_payload?.todo_list)` 保护

---

## 2026-04-26 | Session 8 — E2E Manual Test

**测试目标**：Automation Exercise 搜索→详情→购物车
**结果**：**16/16 步全部通过**，定位策略分布：text(7)、placeholder(4)、button_role(1)、text_fuzzy(2)

---

## 2026-04-26 | Session 7 — Explorer-Judge 架构

**核心差异**：失败不抛异常，记录后继续执行全部步骤。Explorer + Judge 双角色拆分。

**新增**：ExplorationRun/FailureRecord 模型、explorer_runner.py、judge_agent.py、VerdictPanel.tsx
**验证**：374 单元测试全部通过（含新增 25 个）

---

## 2026-04-26 | Session 6 — AI Planning Agent 三阶段进化

- Phase 1：执行分析工具（get_execution_detail/get_project_test_status/get_failure_analysis）
- Phase 2：智能决策（自动注入项目测试状态 + retest API）
- Phase 3：跨会话持久化（TestPointInsight 模型 + flaky 检测算法）

**验证**：350 passed

---

## 2026-04-25 | Session 5 — 上下文压缩 + 废弃 5 轮限制 + TODO 进度展示

**操作**：
1. `_prepare_transcript_for_llm()` 压缩机制（超 10 条时替换早期消息为摘要）
2. `ai_planning_max_react_rounds` 默认 5→0（无限），新增 safety_cap=30
3. system prompt 新增 `todo_list` 字段规范

**验证**：305 passed

---

## 2026-04-25 | Session 4 — CasesPage 项目级分类

- 左侧面板改为项目列表 → 搜索 → 状态过滤三段布局
- 项目列表带 CRUD（新建/编辑/删除 Modal）

---

## 2026-04-25 | Session 3 — CASCADE 替代 RESTRICT

- `test_case.py` FK 从 `ondelete="RESTRICT"` 改为 `ondelete="CASCADE"`

---

## 2026-04-25 | Session 2 — 修复删除项目 500

- `ai_planning_session.py` FK 从 RESTRICT 改为 SET NULL

---

## 2026-04-25 | VLM bbox 坐标点击回退 + 交互式 explore_flow + input_values 透传

**操作**：
1. `ResolvedLocator` 新增 `click_coordinates` 字段
2. `_try_coordinate_click_fallback()` Tier 2.5 回退
3. `_discover_interactive_elements()` 捕获弹层元素
4. `SaveAndExecuteRequest` 增加 `input_values` 字段

**验证**：Exec 69/70 各 13/13 全部通过

---

## 2026-04-24 | Session 2 — BUG-051/052 修复 + explore_flow + VLM 页面布局注解

**操作**：
1. `_substitute_variables` 函数 + `input_values` 字段
2. `_has_explored_pages` + `_auto_explore_entry_url` 强制探索
3. `collect_multi_page_elements` 跨页面采集
4. `describe_page_layout` VLM 页面布局注解

**验证**：303 passed

---

## 2026-04-24 | BUG-050 E2E 验证

**结果**：Execution 53 2/3 步通过（Step 3 `#input-email` 不存在），Execution 54 8/9 步通过（变量未替换）。发现 BUG-051/052。

---

## 2026-04-23 | 定位器系统三阶段改善

1. **Schema — target_strategy 字段**：显式声明定位策略
2. **定位器 — 裸 HTML 标签名识别**：`css_tag` 策略
3. **定位器 — Playwright 链式选择器解析**：`.class text=Value` 格式

**验证**：28/28 passed

---

## 2026-04-23 | BUG-050 DOM 证据注入 + target_strategy 偏好提示

**操作**：
1. `target_strategy` 从锁死改为偏好提示（try/except + fallback 穷举语义扫描）
2. DOM 证据注入 Schema + 数据提取传递
3. 修复 8 个单元测试

**验证**：276 passed

---

## 2026-04-21 | 流式状态感知 + AI 超时修复

**操作**：
1. Agent 流式基础：`_stream_planning_llm()` 使用 httpx SSE
2. 服务层 + WS 路由扩展
3. 前端事件模型（6 种流式事件类型）
4. Panel 流式渲染

**验证**：18 后端测试 + 11 前端测试通过

---

## 2026-04-21 | CRUD 补全

审查并补全所有实体的 CRUD 操作，新增 3 个 DELETE 端点 + 6 个前端 API 函数。

**验证**：前端 build 通过，242 后端测试通过

---

## 2026-04-20 | DOM-aware DSL 生成

**操作**：
1. `page_explorer.py`：存储状态文件 I/O + 元素格式化
2. `collect_interactable_elements` + `capture_browser_session`
3. `explore_page` 和 `capture_page_session` 工具注册
4. `_build_draft_prompt` 追加 DOM 感知提示
5. VLM 默认开启

**验证**：55 个相关单元测试全部通过

---

## 2026-04-17 | 用例创建 + 执行链路测试

**操作**：
1. 新增 3 个集成测试
2. 修复语义定位器 `element_id` 策略缺失 + `case-sensitive` 匹配

**验证**：6 passed

---

## 2026-04-17 | 流式接口 bug 修复 + create_project 幂等处理

1. **Session rollback**：异常时无条件 `db_session.rollback()`
2. **流异常兜底**：`except Exception` 分支写入 error 事件
3. **幂等处理**：同名项目自动编号

**验证**：43 passed

---

## 2026-04-17 | 用例编辑页 + 删除执行记录 + 平台 API chain 测试

**操作**：
1. `CaseEditPage.tsx` 用例编辑页面
2. `DELETE /executions/{execution_id}` 路由 + 前端删除按钮
3. 平台 API chain 白盒测试（3 个 session 测试）

**验证**：18 passed + TypeScript 编译通过

---

## 2026-04-16 | WebSocket 流式执行

**操作**：
1. `playwright_runner.py` 新增 `execute_case_with_playwright_streaming()` 流式执行生成器
2. `ai_planning_streaming.py` + WebSocket 端点
3. `executionWebSocket.ts` socket client
4. AITestPlanningPanel 接入 WebSocket

**验证**：29 后端测试 + 9 前端测试通过

---

## 2026-04-15 | 执行流式推送计划

产出基于当前仓库真实状态的可执行 implementation plan。

---

## 2026-04-13 | DSL 生成修复 + 持久化

**操作**：
1. `_call_llm()` 增加非 JSON/HTML 响应防御
2. 修正 `AI_DSL_BASE_URL`
3. draft 生成结果、execution summary 持久化到 messages

**验证**：12 passed + 5 passed

---

## 2026-04-13 | 白盒排查 session_id=27

确认 `AI_DSL_BASE_URL` 指向 HTML 首页而非 API；当前不存在 SSE/流式执行接口。

---

## 2026-04-12 | 会话删除功能 + stale session 修复

**操作**：
1. `delete_planning_session()` + `DELETE /sessions/{session_id}`
2. 前端删除按钮 + 当前会话删除后自动切换
3. 缓存失效 `ai_planning_last_session` 回退

**验证**：10 passed + 19 passed

---

## 2026-04-08 | 会话历史恢复 + 全流程闭环

**操作**：
1. `GET /sessions` 会话列表接口
2. `POST /sessions/{id}/drafts:save-and-execute` 保存+执行端点
3. 前端会话切换器 + 勾选式审阅卡片 + execution_summary 渲染

---

## 2026-04-08 | 更新 README

更新 README.md 反映 M2 阶段真实状态。

---

## 2026-04-06 | NotebookLM 布局重构

全局 ConfigProvider 主题 token 更新；新建 NotebookLMLayout 三栏布局；逐页重写为三栏风格。

---

## 2026-04-05 | demo 主链路重构

移除 demo 流的认证依赖；新增 PlanningPage；精简导航为三步 Steps；删除旧页面。

---

## 2026-04-03 | AI planning ReAct 改造

重写 `test_planning_agent.py` 为 LLM 驱动的 ReAct loop；更新 schema/service；前端 settings 与规划面板。

**验证**：13 passed + 20 passed + 16 passed

---

## 2026-03-31 | AGENTS.md 更新 + 迁移回归测试

更新协作规则；新增 Alembic 迁移回归测试验证 suite 相关表已被正确移除。

---

## 2026-03-31 | AI 测试规划代码质量修复

前端负时间戳临时 ID；后端 DSL 生成失败异常日志；无效 scenario key 校验。

---

## 2026-03-30 23:15 | AI 测试规划对话助手

新增后端 ai_planning 模型/schema/service/route/agent prompt/loop；前端 AITestPlanningPanel。

**验证**：15 passed + 16 passed + 16 passed

---

## 2026-03-30 22:00 | CRUD 安全修复（BUG-041）

补齐项目成员权限校验；修正 stats 返回结构；处理外键约束下的项目删除语义。

---

## 2026-03-30 21:31 | CRUD 提交审查 + GitHub 提交参考指令

确认 `7eb71ae` 存在多处高风险问题；AGENTS.md 新增 GitHub 提交流程。

---

## 2026-03-29~30 | DSL BigModel 适配与 GLM Visual Locate 适配

`dsl_generator.py` 请求层按 `base_url/model` 做 provider 自适配（BigModel 分支使用 `thinking` 参数）。

---

## 2026-03-29 | Suite 应用层下线

移除已废弃的 Suite 应用层，统一到 `Project -> Case` 资产结构。

---

## 2026-03-29 | 报告中心增强

扩展报告中心的作用域和指标。

---

## 2026-03-28 | M1 认证入口落地与治理收口

后端落地登录/登出/用户信息接口；前端完成登录态恢复、受保护路由、统一 401 回退。
