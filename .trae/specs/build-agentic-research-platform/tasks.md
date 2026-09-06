# Tasks

## 执行约定

- 所有实现与验证工作必须委派给子代理；主代理只负责拆分、协调、审查、整合和提交。
- 无依赖且文件范围不重叠的子任务并行执行。
- 每个 Stage 开始前读取本文件和 `checklist.md`，上下文压缩后从首个未勾选任务恢复。
- 每个 Stage 只允许一个聚焦提交，提交前必须通过该 Stage 全部门禁。
- 每个 Stage 完成后直接推送 `main`，确认远端 SHA；不创建 PR。
- 任一门禁失败时禁止提交，先在本文件追加修复任务。

## Stage 0：固化无提示完整链路

- [x] Task 0.1：审查并整理当前未提交的完整链路修复。
  - [x] 核对 DSL Tool Schema、Harness 工具错误恢复、OpenAI tool arguments、复合 CSS、preflight selector、空 input values 和 UTC duration 变更。
  - [x] 确保没有引入 Python Agent、自由文本执行或跳过审批。
  - [x] 补齐遗漏的单元和 PostgreSQL 集成测试。
- [x] Task 0.2：实现统一的 Agentic E2E 驱动器。
  - [x] 输入只允许自然语言 Goal，拒绝 DSL、CSS、XPath 和 candidates。
  - [x] 自动订阅/回放 Agent Event。
  - [x] 在 DSL Artifact 生成后读取并校验，再自动提交审批。
  - [x] 等待正式 Batch/Execution/Report 终态。
  - [x] 使用独立 Oracle 检查 `#product-1` 的名称、单价、数量和总价。
  - [x] 输出版本化 JSON 结果和 evidence 引用。
- [x] Task 0.3：执行 Canonical Acceptance Goal。
  - [x] 使用全新浏览器上下文和空购物车。
  - [x] 连续执行 3 次，不得提供 selector 提示。
  - [x] 每次 AgentRun、Batch、Execution、Oracle 均通过。
  - [x] 执行错误价格和错误商品的负向变异，必须失败。
- [x] Task 0.3.1：修复 Browser capability 在 FastAPI 请求线程中的 Playwright 生命周期。
  - [x] 消除 `Playwright Sync API inside the asyncio loop`，并增加通过真实 HTTP capability 路由启动浏览器的回归测试。
- [x] Task 0.3.2：修复 Agentic E2E 驱动器的 SSE 边界等待。
  - [x] 使用可靠轮询或真正有界的 SSE 等待，确保 run 已进入 `waiting_user` 时及时回放持久化 events 并到达边界。
  - [x] 失败 JSON 保留已创建的 project/session/run ID、run status、pending tool call、last seq、pending questions 和关键事件引用。
  - [x] 仅自动审批 `approve_dsl` checkpoint；其他 clarification checkpoint 输出结构化失败诊断。
  - [x] 增补单元测试，并通过聚焦测试和 Browser Worker Python 全量测试。
- [x] Task 0.3.3：修复后重新执行 Stage 0 live 验收。
  - [x] Canonical Goal 连续 3 次通过。
  - [x] `wrong-price` 和 `wrong-product` 各执行一次且独立 Oracle 失败。
- [x] Task 0.3.4：修复独立 DOM Oracle 对真实页面 HTML 的解析崩溃。
  - [x] 正确处理 `img` 等 void element、隐式闭合和页面注入内容，不得出现字段栈下溢。
  - [x] 使用 Execution 29 的真实终态 DOM 形态增加回归测试，并保持负向变异非零退出。
- [x] Task 0.3.5：统一 Generation 与正式 Execution 的 DSL SHA canonicalization。
  - [x] Generation artifact、持久化 DSL、执行快照使用同一规范化表示计算 SHA。
  - [x] 增加真实合同测试，确保审批 generation 的 SHA 与正式 report 中 `dsl_sha256` 一致。
- [x] Task 0.3.6：修复 Browser capability 跨多个 Planning Session 的 Playwright 生命周期污染。
  - [x] 保证同一 Browser API 进程连续创建至少 3 个全新 Planning Session 时，不再重复启动会观察到 running asyncio loop 的 Sync Playwright manager。
  - [x] 启动失败清理不得对未完成初始化的 `PlaywrightContextManager` 访问 `_connection`。
  - [x] 增加真实 HTTP capability 路由的多 Session 串行回归测试。
  - [x] 修复后从零重跑 Canonical 3 次及两个 mutation（由 Task 0.3.3 验收）。
- [x] Task 0.3.7：修正 Stage 0 独立验收的 Go PostgreSQL 测试命令并重新执行最终门禁。
  - [x] 将 SQLAlchemy `postgresql+psycopg://` DSN 转换为 pgx 支持的 `postgres://` 后再设置 `TEST_DATABASE_URL`。
  - [x] 从 Go test/vet/build、Python 全量、迁移 head/check、compileall/diff check 开始完整重跑。
  - [x] 静态门禁全部通过后再重启三个服务并从零执行 Canonical 3 次及两个 mutation。
- [x] Task 0.3.8：修复 Agent 重复探索和预检失败耗尽 turn budget。
  - [x] 复现并定位 Run `run_847c3b804514fa7459cbd709`：空动作未采集、动作失败静默、旧 revision 胜出、required-elements 裸成功解锁 generation、preflight 扁平化 state、未验证复合 CSS 及 max-turn 覆盖末次错误共同导致失败。
  - [x] `explore_flow` 空动作采集当前页；click/input/wait_for 和整段 flow 返回结构化失败；`text=` 与 timeout 正确执行；同 URL 保留最新成功 revision。
  - [x] 对齐 Go Tool JSON Schema、Python capability 类型和 Runner 实际 `target_strategy` enum。
  - [x] `generate_dsl` 对最终 case 与 `a11y_nodes_by_state` 原子执行绑定 preflight，并校验 case/evidence digest；required-elements advisory 结果不再解锁 generation。
  - [x] preflight 按 state 匹配并写回 `page_state`；未验证复合 CSS 明确拒绝，精确 verified selector 正常通过。
  - [x] max-turn 诊断保留最后工具错误；复放 BUG-131 的 3 次 page、8 次 flow、1 次 advisory validation 轨迹，在原 20-turn 预算内第 13 turn 到达 generation、第 14 turn 进入审批。
  - [x] Go 全量 test/vet/build、Python 全量、迁移 upgrade/current/heads/check、compileall 和 diff check 通过；Canonical 3+2 live 验收仍由 Task 0.3.3/0.3.7 执行。
- [x] Task 0.3.9：修复 Canonical live generation 反复试探并超过驱动器时限。
  - [x] 复现并定位 Run `run_c77c8c19791d44bc2e761bcc` 在已取得完整页面证据后连续 13 次调用 `generate_dsl`，反复产生非法 `target_strategy`、未验证 selector 和非法 JSON，900 秒内未到达审批边界的问题。
  - [x] 明确语义 target 的省略/空值合同，使 Tool Schema、Go DSL 校验、canonicalization、Python schema 和 prompt 一致，禁止模型把 `generate_dsl` 当作诊断探针反复试错。
  - [x] 增加真实失败轨迹回归，证明在现有 20-turn 和 900 秒预算内生成一次可审批 DSL；驱动器超时时不得留下继续运行的孤立 AgentRun。
  - [x] 修复后从全部静态门禁开始，并从零重跑 Canonical 3 次及两个 mutation。
- [x] Task 0.3.10：修复 Canonical 正式执行被广告插页截断后的验收失败。
  - [x] 复现 Run `run_a58e6c8888075b95f7b9dc61`：Generation 68 点击已验证的商品详情链接后被 `#google_vignette` 广告插页截断，后续 `#quantity` 定位失败，Batch 63 / Job 63 / Execution 54 均失败。
  - [x] 为跨页 click 增加足够的页面转换验证或确定性导航策略，确保进入商品详情后再执行数量输入，且不得绕过探索、DSL 校验或审批。
  - [x] 明确 Agentic E2E 对失败后新 Generation 和第二次审批 checkpoint 的合同；若允许受控修复，必须重新校验 artifact、审批绑定和每个 Batch/Execution 结果。
  - [x] 修复后从全部静态门禁开始，并从零重跑 Canonical 3 次及两个 mutation。
- [x] Task 0.4：提交并推送 Stage 0。
  - [x] 更新执行日志和缺陷日志。
  - [x] 提交信息：`fix: harden agent full-chain execution`
  - [x] 推送 `origin/main` 并确认远端 SHA、工作区干净。

## Stage 1：研究事实与验证完整性

- [x] Task 1.1：修复验证时序和结果合同。
  - [x] 动作前统一采集 pre-state。
  - [x] 持久化每个 precondition/postcondition 的期望值、实际值、状态和耗时。
  - [x] 保持同步与流式执行证据一致。
- [x] Task 1.2：实现真实 network_request verifier。
  - [x] 按步骤隔离网络事件监听。
  - [x] 支持 URL、method、status 匹配。
  - [x] 未观察到请求时返回失败。
- [x] Task 1.3：阻止非幂等动作被自动重复执行。
  - [x] 区分未执行、执行成功但验证失败、执行失败。
  - [x] click/submit/add-to-cart 已提交副作用后不得自动换候选重放。
  - [x] 将后续处理交给 Recovery Decision。
- [x] Task 1.4：扩展 FailureSignal。
  - [x] 保留现有 category。
  - [x] 增加 schema_version、stage、code、retryable、side_effect_committed 和直接执行来源；仅在真实存在时附加 Agent event 引用。
  - [x] 保持 v1 可读取与 fingerprint 稳定，并阻止 committed/unknown 副作用的原动作重放建议。
- [x] Task 1.5：完成 Stage 1 最终验收；提交与推送按本轮要求暂不执行。
  - [x] Canonical Goal 连续 3 次通过。
  - [x] network_request 正反例通过。
  - [x] Add-to-cart 验证失败注入后购物车数量不得重复增加。
  - [x] 提交信息（提交前准备完成）：`fix: make execution evidence research-safe`
- [x] Task 1.5.1：修复 Canonical 搜索控件偶发缺失导致的 clarification。
  - [x] 复现 Run `run_fa172186529e470c356e95dd`：Products 页搜索框和搜索按钮未进入该次 A11y 快照，导致预检无法验证输入/点击步骤。
  - [x] 在不编造 selector、不绕过 preflight 和审批的前提下，让已真实交互成功的搜索控件形成稳定、可绑定的探索证据。
  - [x] `explore_flow` 为每个 action 保存独立 pre/post 目标证据及对应 URL/page_state，并保持页面 latest revision 语义。
  - [x] Canonical 验收合同拒绝用 goto 搜索 URL 代替已验证的 input + click。
  - [x] 修复后从完整静态门禁开始，并从零连续重跑 Canonical Goal 3 次。
- [x] Task 1.5.2：修复 Stage 1 最终验收的 Knip 执行环境。
  - [x] 使用仓库内可写 `.npm-cache` 解决 `npx knip` 访问用户级 npm cache 时的 `EACCES/EEXIST`，未修改用户全局目录且未忽略退出码。
  - [x] 从完整静态门禁开始重新执行，全部通过后再重启三个服务并运行 Canonical 与 Stage 1 专项验收。
- [x] Task 1.5.3：修复 Canonical 加购弹层状态在探索步骤间丢失导致的 clarification。
  - [x] 复现 Run `run_41b69261be3e206fef61a034`：加购后弹层中的 View Cart 在同一步可见，但相邻同 URL 步骤重新导航后变为 hidden。
  - [x] 保留同一页面上的瞬态 UI 状态，使 add-to-cart、等待弹层和点击 View Cart 能形成连续且可绑定的探索证据，不得改用页头 Cart 或直接 goto 绕过用户 Goal。
  - [x] 增加真实浏览器回归，覆盖相邻同 URL 步骤不重置弹层、View Cart 可见性及点击后的 `/view_cart` 状态。
  - [x] 修复后从完整静态门禁开始，并从零连续重跑 Canonical Goal 3 次及 Stage 1 专项验收。
- [x] Task 1.5.4：修复真实 Chromium 回归结束后的 sandbox 根路径访问失败。
  - [x] 复现 `RUN_BROWSER_INTEGRATION=1` 下 Task 1.5.3 回归断言通过，但进程随后因 `Not allow operate files: /` 以 1 退出。
  - [x] 定位 managed `_collect_flow_a11y` 未退出 Playwright 的生命周期缺口；以独立且互不阻断的 `finally` 清理 context、browser 和 Playwright，共享 session 不退出共享 Playwright。
  - [x] 使用仓库内 `.sandbox-tmp`、`TMPDIR` 和 `.venv/bin/python` 完成全量门禁，真实 Chromium 命令整体退出码为 0；Canonical 3 次保留给独立 Stage 1 验收。
- [x] Task 1.5.5：修复独立验收中真实 Chromium 退出阶段的 sandbox 根路径访问复发。
  - [x] 复验 repo-local 绝对 `TMPDIR` 与 `.venv/bin/python`；业务断言输出 `OK`，未再出现 `Not allow operate files: /`。
  - [x] 在不放宽 sandbox、不忽略退出码且不使用仓库外临时目录的前提下，真实 Chromium 回归命令退出 0。
  - [x] 修复后从完整静态门禁开始，并从零连续重跑 Canonical Goal 3 次及 Stage 1 专项验收。

## Stage 2：LLM 与运行成本遥测

- [x] Task 2.1：扩展模型响应与事件。
  - [x] 记录 provider、model、prompt version。
  - [x] 记录 input/output/total tokens。
  - [x] 记录请求 latency、重试和错误类别。
  - [x] usage 缺失时记录 unavailable，不得写 0。
- [x] Task 2.2：持久化并公开遥测。
  - [x] 使用版本化 research event payload。
  - [x] 确保 SSE 重放和 PostgreSQL 查询一致。
  - [x] 对敏感字段执行现有日志脱敏规则。
- [x] Task 2.3：完成 Stage 2 最终验收与提交前准备；commit/push 待主代理执行。
  - [x] Canonical Goal 连续 3 次通过。
  - [x] 真实浏览器门禁由官方 Canonical E2E 覆盖：通过长期 Browser API 执行真实 Chromium，不以独立 Chromium unittest 的 TRAE 外层 sandbox 退出码阻断。
  - [x] 每次 LLM call 均可关联 Run/Step/ToolCall。
  - [x] Token 与 latency 可从持久化事实重算。
  - [x] 提交信息（提交前准备完成）：`feat: record llm usage telemetry`
- [x] Task 2.3.1：收敛 Stage 2 独立 Chromium unittest 的 sandbox 根路径退出问题。
  - [x] 复现 repo-local `TMPDIR` 与 `.venv/bin/python` 下测试断言通过、进程仍因 `Not allow operate files: /` 退出 1。
  - [x] 确认 unittest 业务断言稳定 1/1 `OK`，非零退出来自测试子进程结束后的 TRAE 外层 sandbox 根路径访问限制，不判定为产品失败。
  - [x] 将该测试降为环境受限诊断项：保留真实退出码和 sandbox trace，禁止使用 `|| true` 伪造成功，但不再作为 Stage 2 阻断门禁。
- [x] Task 2.3.2：确认四个 repo-local 运行目录无法规避 TRAE 外层 sandbox 限制并采用替代证据。
  - [x] 将 `HOME`、`TMPDIR`、`XDG_CACHE_HOME`、`PLAYWRIGHT_BROWSERS_PATH` 全部固定为仓库内绝对路径后仍稳定复现业务断言 1/1 `OK`、外层 sandbox 退出 1。
  - [x] 将问题边界定位为子进程退出后的 TRAE 外层 sandbox 限制，不修改业务代码、不放宽 sandbox、不忽略退出码。
  - [x] Stage 2 真实浏览器阻断证据改由官方 Canonical E2E 提供；该链路通过长期 Browser API 执行真实 Chromium，独立进程测试仅保留为诊断项。
- [x] Task 2.3.3：补齐无 ToolCall 模型调用的显式关联合同。
  - [x] `research.llm_call.v1` 每个新事件均记录 `tool_call_status`；单/多 ToolCall 为 `available` 并保留真实 `tool_call_ids`，无 ToolCall 最终文本为 `unavailable/model_returned_final_text`，失败 attempt 为 `unavailable/model_attempt_failed_without_response`，不制造伪 ID。
  - [x] 单 ToolCall 仅在事件级写对应 `tool_call_id`；多 ToolCall 的事件级字段为空但 payload 状态明确；旧事件缺少新增字段时保持可读取。
  - [x] 增加单 ToolCall、多 ToolCall、无 ToolCall 和失败 attempt 的内存持久化、REST/SSE、Harness、前端类型与 PostgreSQL 回放测试。
  - [x] 修复后完整静态门禁通过；Canonical Goal 连续 3 次与逐次 PostgreSQL 遥测重算按要求留给独立 Stage 2 验收。
- [x] Task 2.3.4：修复 Canonical 受控修复进入第二次审批时的驱动器代际校验与非法 input trigger。
  - [x] 复现 Run `run_7271a00b1898afb446109751`：Generation 129 的 Batch 123 / Execution 114 因 `trigger="Search Product textbox"` 被透传给 Playwright 而失败；Agent 生成 Generation 130 并进入第二次 `approve_dsl` 后，驱动器又因 `approved_generation_id` 已按新 generation 清空而误报上一代审批未绑定。
  - [x] 驱动器先以唯一新 Batch/Execution 及其 DSL SHA 结算上一 approval，再允许 `latest_generation_id` 前进且 `approved_generation_id` 在新审批前为空；仍逐代校验 artifact、DSL SHA、checkpoint、审批和 Batch 唯一绑定。
  - [x] Go Tool Schema、Go `ValidateCase` 和 Python `InputStep` 统一将 `trigger` 限制为可空的 `Enter|Tab`；缺省/null 兼容，空串、语义文本和其他按键在执行前拒绝，prompt 要求普通语义输入省略 trigger、搜索按钮使用独立 click。
  - [x] 增加 Generation 129 失败 Batch -> Generation 130 -> 第二次审批回归及跨 Go/Python trigger canonical 合同测试，证明不会跳过失败首批、`first_pass=false`、不会误拒合法新审批，也不会放宽审批门。
  - [x] 完整静态与专项门禁通过；按本轮要求不执行 live Canonical 3 次，最终 live 验收与逐 Run PostgreSQL 遥测重算仍归 Task 2.3。

## Stage 3：Research 数据模型

- [x] Task 3.1：定义 Go research domain types 和 repository interfaces。
  - [x] Experiment、ResearchRun、Transition、RunMetrics。
  - [x] schema/projector/metric/policy version 字段。
- [x] Task 3.2：新增兼容迁移。
  - [x] `research_experiments`
  - [x] `research_runs`
  - [x] `research_transitions`
  - [x] 外键、唯一约束和必要索引。
- [x] Task 3.3：实现 PostgreSQL adapters。
  - [x] CRUD、状态迁移、幂等 append。
  - [x] 不复制截图、完整 transcript 或报告大对象。
- [x] Task 3.4：验收 Stage 3 并准备提交。
  - [x] 空库 upgrade、现有库 upgrade、downgrade/upgrade。
  - [x] 真实 PostgreSQL 并发与唯一约束测试。
  - [x] Canonical Goal 通过并建立完整 ID 关联链。
  - [x] 提交信息已准备：`feat: add research persistence schema`；按本轮要求未 commit/push。
- [x] Task 3.4.1：修复并发相同请求创建 ResearchRun 的偶发主键冲突。
  - [x] 相同 `id`、experiment 和 idempotency key 的并发 `CreateRun` 全部返回同一持久化 Run，不得暴露主键冲突。
  - [x] 不得把不同请求的主键、幂等键或 repetition 唯一约束冲突误判为幂等成功。
  - [x] 增加稳定高并发循环回归，并重新执行完整静态、迁移、真实 PostgreSQL 与 race 门禁。
  - [x] 按本轮要求不执行 Canonical；Task 3.4 的 Canonical、正式关联链和提交推送保持未完成。
- [x] Task 3.4.2：恢复 Canonical 验收使用的 LLM 网关并重新执行 Stage 3 最终验收。
  - [x] 确认 `deepseek/deepseek-v4-flash` 不再在首次模型调用的全部重试中返回 HTTP 503。
  - [x] 从完整静态、迁移和真实 PostgreSQL 门禁重新执行 Task 3.4。
  - [x] Canonical Goal 通过后创建正式 Experiment/ResearchRun，绑定本次 AgentRun/Generation/Batch/Execution，写入并读取最小 Transition。
- [x] Task 3.4.3：修复 LLM HTTP 200 响应读取/解码失败被误分类为 `http_200` 并直接终止 Run。
  - [x] 区分非 2xx HTTP 错误、2xx 响应体读取/JSON 解码错误和已解码但无有效 choice/tool call 的协议错误，不得将 2xx 解码失败记录为 HTTP 错误。
  - [x] 为 2xx malformed/truncated/oversized JSON 增加不保存原始响应或敏感字段的安全诊断和明确重试合同，并补齐 telemetry、重试与失败回归。
  - [x] 完整静态、迁移、真实 PostgreSQL 门禁与官方 endpoint 最小/中等工具调用通过；按本轮要求不运行完整 Canonical，Task 3.4.2 的正式关联链验收保持未完成。
- [x] Task 3.4.4：修复官方 DeepSeek 大上下文工具结果导致的响应断流。
  - [x] 完整工具结果先以 `agent.tool_result.v1` 持久化到 Agent Event/PostgreSQL，保留 `content` 并增加原始 SHA-256 与字节数；`recordToolResult` 返回已持久化 event seq。
  - [x] `explore_page` / `explore_flow` 的模型 transcript 改用确定性 `agent.model_tool_summary.v1`，记录 source seq、raw hash、summary hash 和 policy version，并保留 URL/page state/revision、动作状态/target、target evidence、verified selectors、祖先/交互节点、failure 与 omission counters。
  - [x] 单摘要目标 32 KiB、硬上限 64 KiB，累计探索摘要约 160 KiB；超预算时优先将旧同 URL/state revision 降为 reference-only，错误、pending 与非探索工具结果保持既有语义。
  - [x] 增加模型请求序列化预算统计但不拒绝非探索必要消息；GenerateDSL preflight 继续只接收模型实际提交的精简证据，Stage 4 可从完整 tool.result event 重建。
  - [x] 确定性、SHA、UTF-8、限长、omission、Event 完整内容与 transcript 摘要、PostgreSQL/SSE、recoverable error/latestToolError 和 preflight 回归通过；完整静态门禁与官方中等工具调用通过，未运行 Canonical。
  - [x] 最终 Canonical live 验收通过；5 条探索摘要最大 32,470 bytes、累计 148,529 bytes，10 次 LLM 调用无 `response_read_failed`。

## Stage 4：Trajectory Projector 与 Dataset Export

- [ ] Task 4.1：定义 `research.event.v1` envelope 和新增事件类型。
- [ ] Task 4.2：实现 Event/Report 到 Transition 的版本化 Projector。
  - [ ] 使用 causation/correlation ID 建立顺序，不按时间戳猜测。
  - [ ] 保存 source seq/hash/cursor。
  - [ ] 支持删除投影后重建。
- [ ] Task 4.3：实现 schema-versioned JSONL 导出。
- [ ] Task 4.4：验收、提交并推送 Stage 4。
  - [ ] Canonical Goal 连续 3 次通过。
  - [ ] 从 seq=0 完整回放。
  - [ ] 重复投影结果逐字节一致，无重复 ordinal。
  - [ ] JSONL 每行通过 Schema 校验。
  - [ ] 提交信息：`feat: project and export agent trajectories`

## Stage 5：Metrics 与实验控制面

- [ ] Task 5.1：实现版本化 Metric Projector。
  - [ ] task、execution、verification success 分开计算。
  - [ ] grounding、invalid action、recovery、steps、retry、token、latency 和 vision 指标。
  - [ ] 零分母和缺失 ground truth 返回 null 与原因。
- [ ] Task 5.2：实现 Experiment/Run API 和调度。
  - [ ] 固定 Goal/Dataset/Model/Prompt/Browser/Code/Policy/seed。
  - [ ] 支持 repetitions、随机顺序、warm-up 标记和 clean context。
- [ ] Task 5.3：实现统一 `research-e2e` 命令入口。
  - [ ] `run`
  - [ ] `verify`
  - [ ] `export`
- [ ] Task 5.4：验收、提交并推送 Stage 5。
  - [ ] Canonical Goal 连续 3 次通过。
  - [ ] 独立 Oracle 与 Report 不一致时 task_success=false。
  - [ ] 提交信息：`feat: add reproducible research runs and metrics`

## Stage 6：DSL Action IR

- [ ] Task 6.1：定义 research-v1 Action IR。
  - [ ] Intent
  - [ ] Target
  - [ ] Preconditions
  - [ ] Action
  - [ ] Postconditions
  - [ ] Idempotency/side-effect semantics
- [ ] Task 6.2：先更新 Go 类型和校验，再更新 Python Schema 与 Runner。
- [ ] Task 6.3：保留 legacy profile，research-v1 严格拒绝未知字段。
- [ ] Task 6.4：验收、提交并推送 Stage 6。
  - [ ] Canonical Goal 连续 3 次通过。
  - [ ] 缺 Intent、未知 action、未探索 selector 均在入队前失败。
  - [ ] Go/Python golden fixtures 一致。
  - [ ] 提交信息：`feat: introduce executable action ir`

## Stage 7：Baseline 与 Ablation

- [ ] Task 7.1：实现四种 Execution Profile。
  - [ ] Direct
  - [ ] Candidate
  - [ ] DSL
  - [ ] DSL+Verification
- [ ] Task 7.2：建立至少四个版本化 Automation Exercise Goals。
- [ ] Task 7.3：实现随机执行顺序、20 repetitions 和置信区间。
- [ ] Task 7.4：验收、提交并推送 Stage 7。
  - [ ] 每个 variant 20 次正式运行。
  - [ ] 所有 variant 使用相同控制变量。
  - [ ] Direct 仍经过 DSL envelope 和审批。
  - [ ] 输出原始结果、聚合指标和可复现实验 manifest。
  - [ ] 提交信息：`feat: orchestrate agentic baseline ablations`

## Stage 8：Failure Diagnosis 与 Recovery

- [ ] Task 8.1：实现 Go Failure Diagnosis 和 Recovery Decision。
- [ ] Task 8.2：实现策略枚举、预算、attempt lineage 和成本。
- [ ] Task 8.3：建立可控故障注入。
  - [ ] selector drift
  - [ ] stale DOM
  - [ ] transient network failure
  - [ ] missing modal
  - [ ] wrong business expectation
- [ ] Task 8.4：验收、提交并推送 Stage 8。
  - [ ] 可恢复故障采用预期策略并完成 Goal。
  - [ ] 不可恢复故障进入 MANUAL。
  - [ ] 每次恢复重新生成 DSL 时必须重新审批。
  - [ ] 不得重复非幂等副作用。
  - [ ] 提交信息：`feat: add bounded recovery policies`

## Stage 9：Adaptive Observation Routing

- [ ] Task 9.1：实现 Go Controller 和 Observation Profile。
- [ ] Task 9.2：把 Worker 观察能力收敛为 A11y、A11y+DOM、Vision providers。
- [ ] Task 9.3：实现 Observation Compression、预算和决策事件。
- [ ] Task 9.4：验收、提交并推送 Stage 9。
  - [ ] Canonical Goal 在 A11y 默认路径通过。
  - [ ] 无 accessible name 故障触发 A11y+DOM。
  - [ ] Vision 仅在显式策略允许时触发。
  - [ ] 每次路由均有 confidence、reason、cost 和 outcome。
  - [ ] 提交信息：`feat: add deterministic observation routing`

## Stage 10：Contextual Bandit

- [ ] Task 10.1：实现版本化 Policy Registry 和训练数据快照。
- [ ] Task 10.2：实现 propensity logging、离线评估和 shadow mode。
- [ ] Task 10.3：达到数据门槛后训练 Contextual Bandit。
- [ ] Task 10.4：验收、提交并推送 Stage 10。
  - [ ] Projector 一致性 100%，关键字段完整率至少 99%。
  - [ ] 至少 1000 个 routing decisions，每个 action 至少 200 个有效样本。
  - [ ] 离线置信界通过，task success 下界下降不超过 2 个百分点。
  - [ ] shadow 模式无安全违规。
  - [ ] 提交信息：`feat: add contextual bandit policy`

## Stage 11：Sequential Policy Gate 与整体验收

- [ ] Task 11.1：评估历史决策是否显著影响后续收益。
- [ ] Task 11.2：按门槛执行一个分支。
  - [ ] 门槛满足：实现离线 Sequential Policy 和受控灰度。
  - [ ] 门槛不满足：产出可复现 no-go 报告，以 Bandit 作为最终策略层。
- [ ] Task 11.3：执行完整回归和研究复现。
  - [ ] Canonical Goal。
  - [ ] Ablation suite。
  - [ ] Recovery fault suite。
  - [ ] Observation routing suite。
  - [ ] Dataset export/reimport。
- [ ] Task 11.4：提交并推送最终阶段。
  - [ ] 提交信息：`feat: complete adaptive web agent research platform`
  - [ ] 确认所有 checklist 已勾选。
  - [ ] 确认 `main` 与 `origin/main` 一致且工作区干净。

# Task Dependencies

- Task 0 是所有后续任务的稳定基线。
- Task 1 和 Task 2 可由不同子代理并行实现，但必须分别验收和提交。
- Task 3 依赖 Task 0；可与 Task 1、Task 2 的实现并行，合入顺序保持独立。
- Task 4 依赖 Task 1、Task 2、Task 3。
- Task 5 依赖 Task 4。
- Task 6 依赖 Task 1 和 Task 5。
- Task 7 依赖 Task 5 和 Task 6。
- Task 8 依赖 Task 1、Task 4、Task 7。
- Task 9 依赖 Task 5 和 Task 8。
- Task 10 依赖 Task 9 及数据门槛。
- Task 11 依赖 Task 10；Sequential Policy 实现取决于决策门槛。
