# Agent 全链路一致性排查与治理计划

日期：2026-09-12

状态：in_progress（Phase A 增量 1-2 已实现，等待新 Run 运行态核验）

范围：任务编排、工具调用、上下文管理、任务规划、Explore、元素信息归类、
DSL 治理、Playwright 解析、Runner 执行、报告归因。

## 1. 目标

本计划不以“让某个购物任务通过”为目标，而是建立一条可证明一致的执行链：

```text
Goal
  -> TaskPlan / PlanStep
  -> Tool Decision
  -> Explore Query
  -> BrowserObservation / ElementFact
  -> ResolvedTargetEvidence
  -> TargetBinding
  -> Draft DSL
  -> Executable DSL
  -> Playwright Locator
  -> Step Evidence
  -> Failure Attribution / Report
```

每次语义转换必须满足以下要求：

1. 输入和输出均有版本化 Schema。
2. 业务语义、页面事实、定位候选和运行结果不能互相覆盖。
3. 同一目标元素必须通过稳定 ID 贯穿探索、绑定、DSL、Runner 和报告。
4. 任一阶段不得重新用自由文本猜测上一阶段已经确定的元素。
5. 完整事实持久化，模型上下文按当前状态重建，不无限追加历史。
6. 相同状态下的相同计划和工具调用必须幂等或被确定性拒绝。

## 2. 当前结论

现有 `research-v2` 已建立 TaskPlan、BrowserObservation、TargetBinding、
Executable DSL 和统一 LocatorSpec 的主体结构，但仍存在以下断点。

| 域 | 已有能力 | 当前缺口 | 风险 |
|---|---|---|---|
| 任务编排 | TaskPlan/PlanStep 持久化和状态门控 | 相同语义计划仍创建新版本；revision 无 reason、expected version 和次数预算 | 重复规划、旧绑定失效、额外探索 |
| 工具调用 | Explore 次数和成功签名限制 | 去重只覆盖已成功写入摘要的 explore；失败/拒绝调用和其他工具没有统一账本 | 重复调用继续消耗 turn/token |
| 上下文管理 | 单条工具摘要和 explore 累计 48 KiB 上限 | Loop 仍发送完整增长 transcript；assistant reasoning、tool args 和非探索摘要无总预算 | 请求体持续增长、缓存失效、断流 |
| 任务规划 | Goal 由 RunInput 固定，Plan 控制顺序/次数/副作用 | 首次探索前要求一次性给出完整低层计划；修改计划没有结构化触发条件 | 模型先猜后改，形成 revision loop |
| Explore | disposable context、A11y/DOM 采集、动作证据 | click/input 仍使用独立字符串解析器；多匹配时可取 `.first`；未返回实际命中 candidate ID | 探索成功不等于可 grounding |
| 元素归类 | A11y、DOM、runtime 分栏，最多 120 个 ElementFact | 模型摘要仍优先使用 legacy node；`name` 可被 DOM text 改写；裁剪未按当前 PlanStep 排序 | 模型看到的元素语义与绑定事实不同 |
| DSL 治理 | Go 编译器注入候选并绑定 plan hash | Plan 的字符串 pre/completion conditions 未被确定性编译或等价校验；assert/wait 可无 binding | DSL 可改变验证语义 |
| Playwright 解析 | Runner 使用共享 `compile_locator` | Explore 不复用该编译入口；Observation count 未传 context_path；无 binding 的文本动作走全页 `.first` | Preflight、Explore、Runner 解释漂移 |
| Runner 执行 | 动作前唯一性检查、步骤 evidence、副作用状态 | 被拒 candidate 未写 trace；全部 candidate 失效时缺 failure_reason | 无法解释候选为何失败 |
| 报告归因 | StepEvidence 已含 plan_step_id/target_binding_id | FailureSignal 和模型摘要丢失这些 ID，主要靠 step_index 和错误文本归因 | 修复策略无法精确回到计划和证据 |

## 3. 已确认问题

### 3.1 Explore 与 Runner 没有共享同一个“已解析目标”

`explore_flow` 的 click/input 先经过 legacy 文本、DOM selector 和 semantic fallback
解析；部分路径只要求 `count() > 0` 后取 `.first`。动作成功后，Go 不接收实际命中的
element/candidate ID，而是再次用 action target 在 BrowserObservation 中做文本匹配。
Runner 最后再用 LocatorSpec 编译和 `count() == 1` 选择候选。

同一个动作因此被解释三次：

1. Explore 选择可点击元素；
2. Go 根据文本重新推导 TargetBinding；
3. Runner 根据 LocatorSpec 再解析实时 DOM。

这正是“Explore 成功，但 PlanStep 不能 grounding”以及“DSL 与 Playwright
执行不一致”的结构原因。

### 3.2 元素事实存在双视图

BrowserObservation 保留 `a11y.name` 和 `dom.text`，但 legacy A11y node 的
`name` 仍可能被 `textContent` 覆盖；模型摘要在 legacy nodes 非空时不会从
Observation v2 重建。因此：

- 模型按 DOM text 理解 target；
- Go binding 同时尝试 A11y name、DOM text 和 attrs；
- Playwright `get_by_role` 使用浏览器计算的 accessible name。

此外，Observation 的 `observed_count` 计算未使用 element 的 `context_path`，
而 Runner 使用该路径，iframe/shadow 场景的唯一性结论可能不同。

### 3.3 DSL 条件语义没有被 TaskPlan 确定性约束

TaskPlan 保存字符串 `preconditions` 和 `completion_conditions`；research-v2 DSL
使用结构化 condition。当前编译器校验 action、intent、value、side effect 等字段，
但没有确定性翻译或比较 Plan 条件与 DSL 条件。

同时，`assert_text` 和 `wait_for` 不强制 TargetBinding。无候选时 Runner：

- `wait_for` 使用全页 `get_by_text(target).first`；
- `assert_text` 忽略 semantic target，仅在全页查找 expected value。

这会把“某个业务区域内的文本断言”降级成“页面任意位置出现文本”。

### 3.4 重复规划和重复工具调用只被局部治理

- `CreateVersion` 对相同 semantic hash 仍创建新 Plan。
- `set_task_plan` 更新不要求 `expected_plan_id/version/sha256` 或 revision reason。
- 当前重复签名门只处理成功完成的 `explore_page/explore_flow`。
- 被 Policy 拒绝、Worker 失败、`set_task_plan`、`generate_dsl`、`get_report`、
  `fix_and_retry` 都没有统一的调用 ledger。

结果是相同失败可以通过新 ToolCall ID、计划改版或非 explore 工具重复消耗预算。

### 3.5 模型上下文仍是追加式 transcript

每轮调用直接发送 system prompt 加完整 `run.Transcript`。当前压缩只统计 explore
summary，总量不包括：

- assistant content 和 reasoning content；
- 历史 tool arguments；
- TaskPlan 旧版本；
- DSL/Report/Repair 摘要；
- recoverable tool failure；
- approval/resume 历史。

现有 request budget 只记录，不在调用前裁决，也没有 Run 级 LLM call/token/cost
熔断。该问题继续由 BUG-155 跟踪。

### 3.6 报告不能完整解释定位失败

Runner 只把通过 `count/visible/enabled` 检查的 candidate 加入 trace。候选编译失败、
count 为 0/多值、不可见或不可用时被静默跳过；全部失败后抛错但没有完整
`locator_trace.failure_reason`。

StepEvidence 虽有 `plan_step_id` 和 `target_binding_id`，FailureSignal 及模型可见
FailureBrief 没有这些字段，也没有 selected/rejected candidate ID，导致报告无法直接
回答“哪一个 PlanStep 的哪一个 binding 因什么事实失效”。

### 3.7 共享 BrowserObservation Schema 与实现不一致

Python `ObservationRelation` 输出 `kind/source/target`，共享 JSON Schema 要求
`kind/from/to`。现有 Schema 测试只编译三个 Schema 并验证 TargetBinding，没有使用
真实 BrowserObservation payload 验证 Observation Schema，因此没有拦截该漂移。

## 4. 目标合同

### 4.1 ResolvedTargetEvidence

新增由 Browser Worker 产生、Go 只校验和持久化的版本化合同：

```json
{
  "schema_version": "browser.resolved-target.v1",
  "probe_id": "probe_x",
  "plan_step_id": "open_product",
  "action_index": 2,
  "observation_id": "obs_x",
  "page_state_sha256": "...",
  "element_ref": "S2:123",
  "candidate_id": "candidate_x",
  "locator": {"kind": "role", "role": "link", "name": "View Product", "exact": true},
  "context_path": {"frames": [], "shadow_hosts": []},
  "runtime_match_count": 1,
  "visible": true,
  "enabled": true,
  "action_status": "succeeded"
}
```

规则：

- Explore 必须先生成 candidate，再使用共享 `compile_locator` 验证和执行。
- `runtime_match_count != 1` 时动作不得作为 grounding 成功。
- Go 直接从 `element_ref + candidate_id` 构造 TargetBinding，不再按文本重匹配。
- 正式 Runner 只能执行 TargetBinding 中的 candidate，并记录同一 candidate ID。

### 4.2 PlanRevisionRequest

计划更新改为显式请求：

```json
{
  "expected_plan_id": "plan_x",
  "expected_version": 2,
  "expected_sha256": "...",
  "reason_code": "evidence_contradiction",
  "evidence_refs": [{"event_seq": 42, "content_sha256": "..."}],
  "definition": {}
}
```

规则：

- 相同 semantic hash 返回当前 Plan，不创建版本。
- revision 必须有旧版本 CAS、原因和证据。
- 仅允许 `goal_clarified`、`evidence_contradiction`、`unsupported_action`、
  `user_requested` 四类原因。
- 每 Run 默认最多 2 次 semantic revision；超限进入 clarification/blocked。

### 4.3 ToolCallLedger

每次调用在执行前写入：

```text
run_id + plan_version + state_epoch + tool + normalized_arguments_hash
```

并记录 `proposed/authorized/running/succeeded/failed/rejected/pending`。所有状态都计入
预算；相同 state epoch 下：

- succeeded/pending：返回已有结果或 checkpoint；
- running：返回 in-flight；
- failed/rejected：仅在 failure code 明确允许且 retry budget 未耗尽时重试；
- 非幂等动作：必须使用后端 idempotency key，不能由模型重复触发。

### 4.4 Context Materializer

模型请求不再直接使用完整 transcript，而是从持久化事实生成：

```text
Stable Prefix
  system prompt + tool schemas + immutable Goal + policy versions

Current State
  current TaskPlan binding + current/pending PlanStep + remaining budgets

Relevant Evidence
  latest observation delta + selected/rejected candidates + artifact refs

Unresolved State
  current failure + recovery decision + pending approval

Short Interaction Tail
  only protocol-required assistant/tool pairs
```

完整 transcript、raw observation、report 和 reasoning audit 留在事件存储；模型窗口只
携带引用和必要摘要。Materializer 输出自身 manifest/hash，Checkpoint 恢复后可重建
同一上下文。

### 4.5 FailureSignal v3

在兼容读取 v1/v2 的前提下增加：

- `plan_id`、`plan_version`、`plan_step_id`；
- `target_binding_id`、`observation_id`；
- `selected_candidate_id`；
- 所有 candidate 的 `runtime_count/visible/enabled/rejected_reason`；
- `classification_source` 和精确 JSON pointer；
- `recommended_recovery` 仅作为事实建议，最终重试仍由 Go policy 裁决。

## 5. 分阶段执行计划

### Phase A：诊断基线

1. 新增 `agent.pipeline.trace.v1`，贯通 run/plan/step/probe/observation/element/
   candidate/binding/generation/execution/report ID。
2. 为每次模型请求记录 materialized message bytes、各分区 bytes、累计 token/call。
3. 为所有工具记录 normalized signature、state epoch、结果状态和 retry lineage。
4. 使用最近失败 Run 做离线事件重放，不调用模型。

增量 1 完成情况：

- 已新增 `agent.pipeline.trace.v1` 及共享 JSON Schema。
- 已记录模型请求组成、累计 logical/physical call 和 token usage。
- 已记录全部工具的 normalized signature、state epoch、生命周期状态、attempt
  和 retry lineage；该增量仅观测，不执行去重裁决。
- 已新增 `pipeline-audit --run-id` 离线汇总入口。
- 历史 Run 不含新 trace，真实离线重放需在部署该增量并产生新 Run 后执行。

增量 2 完成情况：

- Go 为每个 Explore ToolCall 注入稳定 `probe_id`，BrowserObservation 保留该 ID，
  每个 observed locator 生成稳定 `candidate_id`。
- TargetBinding 直接保留 probe/observation/page state/element/candidate 引用；
  Go compiler 将这些只读字段注入 Executable DSL。
- Runner StepEvidence 同时记录 planned candidate 和实际 resolved
  candidate/element；定位失败 trace 保留所有候选的 runtime count 和拒绝原因。
- FailureSignal、模型可见 FailureBrief、Research Transition 和
  `pipeline-audit` 均保留 lineage。
- 新写数据必须具备完整 lineage；旧 Observation、TargetBinding 和 research-v2
  canonical payload 保持可读。
- 已修复 BrowserObservation relation 的 `source/target` 与共享 Schema 漂移。

退出门槛：

- 任一 PlanStep 可从单条诊断记录追溯到最终 StepEvidence。
- 能明确统计 plan revisions、重复工具调用和每轮上下文增长来源。

### Phase B：统一 Explore 与 Locator 合同

1. 修复 BrowserObservation relation Schema。
2. 新增 ResolvedTargetEvidence Schema 和 Go/Python golden。
3. Explore click/input/wait_for 全部走 `ElementQuery -> LocatorSpec -> compile_locator`。
4. 禁止 research-v2 Explore 对多匹配结果使用 `.first`。
5. `observed_count` 使用与 Runner 相同的 context_path。
6. ElementFact 裁剪改为“当前 PlanStep 相关元素优先 + 交互元素 + 必要祖先/关系节点”。
7. 移除 research-v2 主链对 legacy `node.name` 和字符串 parser 的依赖。

退出门槛：

- Explore 命中 candidate ID 与 Runner 最终 candidate ID 一致率 100%。
- 0/1/N、多 frame、open shadow、重复文本、SPA revision 用例全部通过。

### Phase C：收紧 TaskPlan 与 DSL

1. 实现 PlanRevisionRequest、相同 hash 幂等和 revision budget。
2. 将 Plan 条件升级为结构化 `ConditionIntent`，由 Go 确定性编译到 DSL condition。
3. `assert_text/wait_for` 必须二选一：
   - 绑定明确 Element/Region；或
   - 声明为 page-level fact，并使用独立 action/condition 类型。
4. 编译器逐字段验证 PlanStep、condition、TargetBinding 和 occurrence。
5. 删除 research-v2 的自由文本 locator fallback。

退出门槛：

- 相同计划提交不会增加版本。
- 改 action/order/value/condition 任一字段都会使旧 DSL 和审批失效。
- Runner 不再忽略 semantic target。

### Phase D：统一工具去重与预算

1. 落地 ToolCallLedger，覆盖成功、失败、拒绝和 pending。
2. 为 plan revision、explore、generation、report、repair 分别设置预算。
3. `get_report` 对同一 Batch 只允许一个 terminal wait；禁止模型轮询。
4. `generate_dsl` 相同 plan hash + draft hash 幂等返回原 generation。
5. 在 Harness 中以状态机允许集限制下一工具，而不只依赖 prompt。

退出门槛：

- 同一 state epoch 无重复副作用调用。
- 重复失败签名最多进入一次受控 retry，之后确定性 blocked/clarification。

### Phase E：Context Materializer 与成本熔断

1. 模型调用前从事件和当前状态重建上下文，不直接发送完整 transcript。
2. 分离 audit transcript 与 model working context。
3. 增加可配置硬门：
   - 单请求 bytes/tokens；
   - Run 累计 input/output/total tokens；
   - LLM logical/physical calls；
   - plan revisions、tool retries、repair attempts。
4. 超限前写 checkpoint 和 budget failure，不再继续调用 provider。
5. 聚合 cache hit/miss 与预估费用；正式 live run 前输出预算并等待批准。

退出门槛：

- 同一状态重建上下文 hash 稳定。
- 历史轮次增加时，请求体保持在预算内，不线性增长。
- 任一硬上限触发后无新增 provider 请求。

### Phase F：Runner Trace 与报告归因

1. Runner 记录每个候选的编译、count、visible、enabled 和拒绝原因。
2. 全部候选失败时仍生成完整 LocatorTrace。
3. FailureSignal v3 带回 PlanStep/Binding/Observation/Candidate lineage。
4. Repair Decision 按 stage/code/side effect/lineage 决策，不按宽泛 category 猜测。
5. Projector 和 Metrics 直接计算 grounding accuracy、invalid action rate 和恢复结果。

退出门槛：

- 每个失败都能回答“失败在哪一层、依据什么事实、是否执行过副作用、允许何种恢复”。
- `grounding_accuracy` 和 `invalid_action_rate` 不再因事实缺失返回 unavailable。

### Phase G：分层验收

执行顺序：

1. Schema/golden/属性测试。
2. 离线历史 Run 重放。
3. 本地受控页面 0/1/N locator 与副作用故障注入。
4. 七类跨站 BrowserObservation/Locator 矩阵。
5. 单次官方 DeepSeek smoke，核对 request ID、usage 和预算。
6. 用户确认成本后执行 3 次 Canonical + 负向变异。

在 Phase A-F 全部通过前，不执行多 repetition 付费验收。

## 6. 必须新增的测试

| 测试 | 断言 |
|---|---|
| identical-plan | 相同 hash 返回同一 plan/version |
| revision-cas | stale expected version 被拒绝 |
| failed-tool-repeat | 同一失败签名不会无限重试 |
| explore-ambiguous | count=N 不得产生 grounded binding |
| explore-runner-identity | Explore 与 Runner 使用同一 candidate ID |
| frame-shadow-count | Observation count 与 Runner count 一致 |
| relation-schema | 实际 BrowserObservation 通过共享 JSON Schema |
| task-relevant-truncation | 120 节点预算内保留当前 PlanStep 必需元素 |
| condition-preservation | Plan condition 变化使旧 DSL 无效 |
| scoped-assertion | assert_text 不得在错误区域因全页文本误通过 |
| candidate-rejection-trace | 0/N/hidden/disabled/compile error 全部可见 |
| context-rebuild | resume 前后 materialized context hash 一致 |
| context-budget | 超限前停止且没有额外 provider call |
| failure-lineage | FailureSignal 可回到 plan/step/binding/candidate |

## 7. 优先级与提交边界

实施顺序固定为：

1. **P0**：Phase A、BrowserObservation Schema、Explore/Runner identity。
2. **P1**：TaskPlan 幂等/revision、DSL condition 治理、统一 tool ledger。
3. **P1**：Context Materializer 与成本熔断。
4. **P2**：FailureSignal v3、Projector/Metrics。
5. **验收**：离线和本地全部通过后再运行官方模型。

建议保持一阶段一个聚焦提交：

```text
test: add agent pipeline consistency diagnostics
fix: unify explore and runner target identity
fix: make task plans and tool calls idempotent
feat: materialize bounded agent context
feat: preserve execution failure lineage
test: validate research v2 end to end
```

## 8. 本轮不做

- 不通过扩大 turn 或 token 上限绕过问题。
- 不增加任务专用 selector、URL、商品名或动作顺序。
- 不让模型直接生成 locator candidates。
- 不在合同门禁完成前执行多轮付费 E2E。
- 不把 Vision 作为默认定位补丁。
