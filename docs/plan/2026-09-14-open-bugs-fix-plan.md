# 2026-09-14 未修复问题整体修复方案

## 背景与范围

依据 `docs/bug-log.md` 顶部最新记录，当前 `open` 问题按主线归类如下，本方案给出逐条可执行的修复计划（先方案、后实施，避免改码污染审阅）。

| 主线 | 问题 | 严重度 | 关键文件 |
|------|------|--------|----------|
| A. Agentic E2E grounding 收敛 | BUG-198（待 live 复验）、BUG-197（空/字形名按钮）、BUG-195（MAX_TURNS）、BUG-188（candidate 引用） | high/critical | `browser-worker/.../page_explorer.py`、`backend-go/internal/agent/loop.go`、`backend-go/internal/config/config.go` |
| B. 测试与文档门禁 | BUG-192（PG 事件计数断言）、BUG-190（README 编译路径） | low | `backend-go/internal/integration/agent_event_telemetry_test.go`、`backend-go/internal/research/source_postgres_test.go`、`README.md` |
| C. 终态语义与幂等 | BUG-186（max-turn 引用旧错误）、BUG-182（条件断言）、BUG-181（工具幂等） | high/medium | `backend-go/internal/agent/loop.go`、`backend-go/internal/taskplan/compiler.go`、`backend-go/internal/taskplan/service.go` |
| D. 杂项 | BUG-179/178/177/167/155 | low~high | checklist、corrections 路由、Overview 时间窗、LLM adapter、Context 治理 |

---

## A 线：Agentic E2E grounding 收敛（优先）

### A1. 【本轮核心】BUG-198/197/188：Python 执行层 `resolved_candidate` 断链

#### 现状（已核实）

`candidate_ref → explore_flow` 的完整链路：

1. Go 摘要侧：`backend-go/internal/agent/tool_result.go` 已把每个 `VerifiedSelectors` 候选挂一等公民 `observation.selectable_candidates`（上限 64），每项携带完整 `grounding.candidate-ref.v1`（`source_event_seq/probe_id/observation_id/candidate_id`），且 `observationLabel` 已对字形/空名回退 DOM 语义标签（`#submit_search` → 可读 label）。
2. Go 工具侧：`backend-go/internal/tools/browser.go#hydrateCandidateReferences` 收到模型提交的 `candidate_ref` 后，用 `ObservationReader.ResolveCandidate`（`backend-go/internal/observation/observation_reader.go`）从当前 run 持久化事件解析出 `TrustedResolvedCandidate`（含 locator/元素元数据），经 `AuthorizeResolvedCandidates` 授权后，**把 action 里的 `candidate_ref` 删除、替换为 `resolved_candidate`** 再发给 worker。
3. Worker 合同侧：`browser-worker/src/browser_worker/contracts/browser_capabilities.py` 的 `ExploreFlowAction` 已允许 `locator / candidate_ref / resolved_candidate` 三选一（`resolved_candidate` 为新增字段，且 v1 schema 拒绝它）。
4. **断点**：worker 主执行路径 `browser-worker/src/browser_worker/exploration/page_explorer.py#_collect_flow_a11y`（约 1390–1434 行）仍然只读顶层字段：

   ```python
   locator_spec = action_def.get("locator")   # ← resolved_candidate 场景下为 None
   ...
   if not isinstance(locator_spec, dict):
       raise ValueError("explore_flow actions require a structured semantic locator")
   ```

   而本次 BUG-198 修复新增的 `_flow_action_locator(action)`（约 927 行，已实现「优先 locator，回退 `resolved_candidate.locator`」）**只被 `_flow_action_target_label`（日志标签）调用，主执行循环没有接入**。

#### 结论

模型一旦按提示词提交 `candidate_ref`，Go 水合为 `resolved_candidate` 后，worker 会 100% 抛 `ValueError("explore_flow actions require a structured semantic locator")`。**BUG-198 修复在 Python 执行层未闭合，live E2E 复跑必然失败。**

#### 修复方案

1. `page_explorer.py#_collect_flow_a11y` 动作循环内，把
   `locator_spec = action_def.get("locator")` 改为
   `locator_spec = _flow_action_locator(action_def)`；
   保持其后的 `isinstance(locator_spec, dict)` 校验不变（回退后必有 dict）。
2. 该 `locator_spec` 会自然流入 `collect_action_snapshot(..., locator_spec=locator_spec, ...)`、`compile_locator(page, locator_spec)`、`build_resolved_target_evidence(..., locator_value=locator_spec)`、`_wait_for_flow_target(..., locator_spec=locator_spec, ...)`，count==1 runtime 复验（`loc.count()` 校验）原样生效，即 BUG-188 想要的「Worker 校验 runtime count=1 后执行」在现有代码里已具备，无需新加。
3. `_flow_action_locator` 返回的应是 worker 可编译的 locator；`resolved_candidate.locator` 本身来自 `TrustedResolvedCandidate`（Go 侧已用 `browsercontract` 校验），无需在 worker 端二次校验结构。

#### 测试计划（TDD，RED→GREEN）

- `browser-worker/tests/test_page_explorer.py` 新增：
  - `_flow_action_locator` 单测：仅 `resolved_candidate.locator`、仅顶层 `locator`、两者都缺、`resolved_candidate` 缺 locator 四种形态。
  - `_collect_flow_a11y` 集成（mock `compile_locator`/`click_with_precheck`）：动作只有 `resolved_candidate`（click/input/wait_for 各一），断言不再抛 ValueError 且按回退 locator 执行；`resolved_candidate` 缺失 locator 时仍按现有合同报错。
- `backend-go/internal/tools/capability_tools_test.go` 已覆盖 Go 侧水合（`TestExploreFlowHydratesCandidateReferenceForWorker`），无需改动；补一个断言：水合后 action 不含顶层 `locator` 也能被 worker 合同接受（即确认 Go 输出形态与 Python 合同一致，防止再漂移）。

#### 验收

- Python 单测/全量通过；Go 全量通过。
- live E2E（需 DeepSeek + Chromium）：`run_agentic_e2e.py --acceptance-spec …/automationexercise-blue-top-cart.v1.json`，搜索按钮 `#submit_search` 经 `selectable_candidates` 的 count=1 候选 + `candidate_ref` 收敛，不再 0/3 match 反复重试；全程不再触发 `explore_deadline_budget_exhausted`（除非任务确实需要更多墙钟）。

---

### A2. BUG-195：`AGENTSERVICE_MAX_TURNS` 默认值不足

#### 现状

`backend-go/internal/config/config.go:35` 默认 `maxTurns := 12`；`backend-go/internal/agent/loop.go:51` 用固定轮数跑循环，耗尽即失败。12 步 TaskPlan 每步至少 1 probe + 1 query，12 轮必然不够（已由 run 实据证实）。

#### 修复方案（按 TaskPlan 步数动态化，避免硬编码）

1. `loop.go` 的 `Loop` 增加动态上限能力：把「本轮是否还有剩余轮次」从固定 `for range l.maxTurns` 改为在 `TurnHandler` 回调里可查询/可调整的预算。最小侵入做法：
   - `Loop.RunWithModelContext` 每轮调用前检查一个 `turnsRemaining` 回调（由 harness 提供），默认返回 `l.maxTurns - used`；
   - harness 在 `set_task_plan` 成功 / TaskPlan 更新后，把预算提升为 `max(len(plan.Steps)*2 + reserve, current)`，其中 `reserve`（如 8）留给 DSL 生成/审批/修复阶段。
2. `config.go`：默认值从 12 提到 24（保守），并保留 env 覆盖；同时保留墙钟门禁（BUG-198 已加的 `AGENTSERVICE_MAX_WALL_TIME_SECONDS`）作为第二道防线，防止动态轮数造成无限循环。
3. 提示词/摘要中把「剩余轮次」展示给模型（复用现有探索预算摘要模式），让模型在 grounding 阶段主动收敛。

#### 测试计划

- `agent/loop_test.go`：固定轮数行为不变（回归）；新增动态预算回调测试（回调先小后大、耗尽时报 `exceeded maximum turns`）。
- `taskplan/service_test.go`：`set_task_plan` 后预算按步数提升的 harness 级测试（或在 `harness_test.go` 用 fake policy/plan 验证 `turnBudget` 计算）。
- `config_test.go`：默认 24、env 覆盖、非法 env 回落。

#### 验收

- Go 全量通过；不依赖付费模型。
- live E2E：12 步用例不再以 `agent exceeded maximum turns: 12` 终止，终态只可能由墙钟/业务失败产生。

---

### A3. live E2E 复验（BUG-198/197 的最终验收）

- 前置：A1 断链修复 + A2 动态轮数合入且门禁全绿。
- 环境：本机已有 PostgreSQL(:5432)、browser-worker(:8000)、agentservice(:8081)、execution-worker、DeepSeek key（`browser-worker/.env`），可直接复用 2026-09-14 的启动脚本。
- 指标（对齐历史 run 的 DB 实据口径）：
  - `bug197_locator_count`：搜索按钮定位不再 0/3 反复（期望归零或显著下降）；
  - `candidate_ref_refs`：期望 > 7 且 grounding 持续推进；
  - 终态：TaskPlan 全部 grounded → `ready_for_generation` → DSL 生成 → 审批 → 正式执行 → 报告。
- 若仍失败：导出 `run.json`/`pipeline-audit.json`，按 A/B/C 线归因，避免直接重复消耗预算。

---

## B 线：测试与文档门禁（低成本，可顺带清掉）

### B1. BUG-192：PostgreSQL 事件集成测试按旧事件数量断言

- 文件：`backend-go/internal/integration/agent_event_telemetry_test.go:62`（`TestPostgresResearchLLMCallToolAssociationsAndLegacyReplay`，预期 5 实际 9）、`backend-go/internal/research/source_postgres_test.go:17`（`TestPostgresSourceReaderProjectsRealAgentEvents`，预期 9 实际 11）。
- 根因：生产 telemetry 同时持久化 `research.llm_call` 与 `agent.pipeline.trace`，测试事件数量/投影预期未同步。
- 方案：把断言改为「按事件类型分组计数」或「显式过滤 trace 事件后再按旧数量断言」，并对两种事件分别断言存在性；同时在测试注释里锁定两类事件的产生条件，防止再次漂移。运行方式：设置 `TEST_DATABASE_URL` 后 `go test -count=1 ./...`。

### B2. BUG-190：README 编译验证命令路径过期

- 文件：`README.md:307`，`uv run python -m compileall -q app` → `uv run python -m compileall -q src`（browser-worker 已迁移 `src` 布局）。

---

## C 线：终态语义与幂等（较大，作为 A 线之后）

### C1. BUG-186：max-turn 终态引用已恢复的旧工具错误

- 文件：`backend-go/internal/agent/loop.go:78-82`（`latestToolError`）。
- 根因：`latestToolError` 从 transcript 尾部找最后一条 tool 消息的错误；工具成功后的摘要若被压缩/替换，历史失败文本仍被拼进 max-turn 终态错误。
- 方案：
  1. `latestToolError` 改为只在「最后一条 tool 消息本身是失败结果」时返回其错误；成功结果后置空（把「最后工具成功与否」作为 state，而非从 transcript 尾部线性找）。
  2. max-turn 终态错误独立报告：`agent exceeded maximum turns: N; current task plan status: <status>, grounded <x>/<y> steps`，不再拼接历史工具错误。
  3. 测试：构造「最后一条工具成功 + 更早一次失败」的 transcript，断言终态错误不含历史失败文本；「最后一条工具失败」仍保留其文本。

### C2. BUG-182：research-v2 条件与文本断言未保持 TaskPlan 语义

- 文件：`backend-go/internal/taskplan/compiler.go#compileDraftStep`（约 139 行）。
- 根因：Plan 的 `preconditions/completion_conditions` 仍是字符串，DSL 条件已结构化，编译时未逐字段比对；无 binding 的 `assert_text` 退化为全页文本查找。
- 方案：
  1. 引入结构化 `ConditionIntent`（contracts 新增 schema），`compileDraftStep` 对条件做确定性翻译并在 Plan→DSL 间逐字段比对（type/region/target/value/expectation）。
  2. 收紧合同：`assert_text`/`wait_for` 需要 binding 或显式 page-level 事实声明，移除自由文本 target 的模糊回退；区域断言（element/region）与页面事实（page-level）两类合同分开建模。
  3. 测试：构造 action/value 相同但 conditions 不同的 Plan/DSL，编译必须失败；无 binding 的 `assert_text` 在非目标区域同名文本出现时不得假通过。

### C3. BUG-181：相同 TaskPlan 与非 Explore 工具调用缺少统一幂等治理

- 文件：`backend-go/internal/taskplan/service.go#CreateVersion`（约 29 行）、`backend-go/internal/harness`。
- 根因：相同 `PlanSHA256` 仍创建新版本；重复调用门只覆盖成功完成的 explore，未覆盖失败/拒绝及 `generate_dsl/get_report/fix_and_retry`。
- 方案：
  1. `CreateVersion`：相同 `PlanSHA256` 返回当前版本（no-op），仅当内容变化才递增版本；revision 增加 `reason` 与证据引用。
  2. 新增 `ToolCallLedger`（持久化表 + 内存缓存）：`state_epoch + normalized_signature + outcome` 三重键去重；`explore_page/explore_flow/set_task_plan/generate_dsl/get_report/fix_and_retry` 全部工具结果状态（succeeded/failed/rejected）入账；失败/拒绝重试仅在签名变化或显式修复指令下允许。
  3. 测试：相同 plan 提交两次版本数不变；相同失败工具签名重试被 ledger 拒绝；`generate_dsl` 重复调用去重；现有探索预算测试回归。

---

## D 线：杂项（按优先级排期）

| 问题 | 方案要点 |
|------|----------|
| BUG-179 | 回填 `.trae/specs/build-agentic-research-platform/checklist.md` Stage 6（113/117 行）与 `tasks.md` Task 6.6（278-281 行）：版本化 TaskPlan、PlanStep 绑定、Stage 6 提交勾为完成；Context Materializer、成本熔断、live Canonical 验收保持未完成。 |
| BUG-178 | `internal/transport/http/corrections.go` 返回的 `Location` 要么注册只读 GET 路由（`Store.Get` 已有），要么创建响应改回不带 Location；二选一后更新 `docs/api-reference.md`。 |
| BUG-177 | 统一时间语义：数据库 session 与应用均用 UTC（或 Overview 显式按产品业务时区建窗口），不能只改测试。涉及 `test_case_runs.started_at` 时区语义与 `internal/.../overview` 窗口构造。 |
| BUG-167 | LLM adapter（`backend-go/internal/platform/llm/openai.go`）增加 attempt deadline、流式 watchdog、Retry-After 上限与调用级熔断；保留 provider request ID 归因；本轮 BUG-198 已顺手把 `ECONNABORTED` 加入 retryable，属该方向的一部分。 |
| BUG-155 | Context Materializer：把完整探索结果从模型 transcript 移出、按需查询；Run 级 token/call 成本熔断与缓存指标（与 C3 ToolCallLedger 协同）。 |

---

## 实施顺序与提交策略

1. **A1（断链修复）** 单提交，`fix: wire resolved_candidate into explore_flow execution path`（含 Python 单测 + Go 断言），独立验证。
2. **A2（动态轮数）** 单提交，`fix: scale agent turn budget with task plan steps`。
3. **B1+B2** 单提交，`test: align postgres event count assertions` + `docs: fix browser-worker compileall path`（可拆两个 commit）。
4. **C1** 单提交，`fix: report max-turn terminal state without stale tool error`。
5. **C2/C3/D** 各自独立提交（规模大，建议单独排期，不并入本轮）。
6. 全部合入本地 `main`，门禁全绿后按仓库约定询问是否推送 GitHub（`git push origin main`）。

> 说明：本方案仅落地于文档，未改动任何业务代码；工作区现有 BUG-198 未提交修改在 A1 验证后与 A1/A2 一起提交。

## 验证清单（最终）

- [ ] `cd backend-go && go build ./... && go vet ./... && go test -count=1 ./...`
- [ ] 设置 `TEST_DATABASE_URL` 后 `go test -count=1 ./...`（覆盖 BUG-192 修复）
- [ ] `cd browser-worker && uv run pytest tests/unit -q`（或等价全量）
- [ ] `cd frontend && npm run build && npm test -- --run`
- [ ] live E2E 复验（A3），输出 run.json/pipeline-audit.json 归档到 `research/results/`
