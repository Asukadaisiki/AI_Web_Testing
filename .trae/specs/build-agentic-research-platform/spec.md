# 构建 Agentic Web Testing Research Platform Spec

## Why

当前项目已经验证了从自然语言 AgentRun 到 DSL、审批、正式执行和报告的主链可行，但还缺少研究所需的事实完整性、可重建 Trajectory、可复现实验、策略评估和学习闭环。需要在不引入第二 Agent、不绕过 DSL 审批门的前提下，按阶段把现有工程升级为可实验、可度量、可复现的 Agentic Web Testing Research Platform。

## What Changes

- 将当前完整链路修复固化为 Stage 0 基线，并在无人工 DSL、selector 或 candidates 提示下完成真实 E2E。
- 修复前置/后置验证、网络验证和非幂等动作重复执行等测量可信度问题。
- 在 Go 控制面增加版本化 Research Experiment、Research Run、Transition、Metric 和 Dataset Export。
- 从现有 Agent Event、LLM 调用、DSL Generation、Execution Report 和 Evidence 投影可重建 Trajectory。
- 记录模型、Prompt、Token、延迟、浏览器、代码版本、观察模态和恢复成本。
- 将 DSL 升级为可执行 Action IR，增加 Intent、Preconditions、Postconditions 和幂等语义。
- 增加 Direct、Candidate、DSL、DSL+Verification 的可重复消融实验。
- 增加确定性 Failure Diagnosis、受预算约束的 Recovery Policy 和 retry lineage。
- 增加 A11y、A11y+DOM、Vision 的确定性 Observation Routing。
- 在数据门槛满足后增加 Contextual Bandit；Sequential RL 仅在序列依赖得到实验证明后实施。
- 每个阶段都以真实自然语言 Goal 的官方 Agent 链路 E2E 为验收标准。
- 每个阶段通过门禁后在 `main` 创建独立提交并直接推送。
- 实施阶段全部使用子代理；无依赖任务并行执行，主代理负责整合、验收和提交。
- 以本规范、任务清单和检查清单作为上下文压缩后的恢复锚点。

## Impact

- Affected specs:
  - AgentRun/Event/Checkpoint 协议
  - DSL 与 Action IR
  - Browser Worker Observation/Execution/Verification
  - Execution Evidence、FailureSignal 和 Report
  - Research Experiment、Trajectory、Metrics、Dataset
  - Recovery Controller、Observation Router、Bandit Policy
- Affected code:
  - `backend-go/internal/agent`
  - `backend-go/internal/harness`
  - `backend-go/internal/agentservice`
  - `backend-go/internal/tools`
  - `backend-go/internal/execution`
  - `backend-go/internal/research`（新增）
  - `backend-go/internal/transport/http`
  - `browser-worker/app/ai`
  - `browser-worker/app/locators`
  - `browser-worker/app/runners`
  - `browser-worker/app/schemas`
  - `browser-worker/app/services`
  - `browser-worker/alembic/versions`
  - `frontend/src/features/research`（后期）
  - `research/goals`
  - `research/datasets`

## Architecture Constraints

1. Go AgentService SHALL 是唯一 Agent、实验编排、策略、Reward 和数据集导出控制面。
2. Python Browser Worker SHALL 只负责观察、定位、确定性执行、验证和证据采集。
3. 正式执行 SHALL 经过结构化 DSL 校验与用户审批，不允许自由文本直接驱动 Playwright。
4. 原始 Agent Event、LLM 调用事实和 Execution Report SHALL 是事实源。
5. Trajectory、Metric 和 Reward SHALL 是带版本号、可删除重建的投影，不得成为第二执行真相。
6. Vision SHALL 默认关闭，只能由显式实验配置或 Controller 决策启用。
7. 所有实验 SHALL 固定 Goal、数据集、模型、Prompt、代码 SHA、浏览器版本、viewport、seed 和策略版本。
8. 外部真实网站用于 clean-lane 验收；故障注入使用可控代理或固定 fixture，避免把第三方波动误判为产品能力。

## Stage Delivery Protocol

每个 Stage SHALL 按以下顺序交付：

1. 子代理实现互不冲突的代码或测试任务。
2. 执行聚焦测试、全量相关测试和真实 Agent E2E。
3. 按 `checklist.md` 逐项验收。
4. 更新 `tasks.md`、`checklist.md`、`docs/execution-log.md`；发现缺陷时更新 `docs/bug-log.md`。
5. 仅在全部阶段门禁通过后创建一个原子提交。
6. 直接推送 `main`，确认远端 SHA 和工作区状态。
7. 门禁失败时禁止提交或推送，先追加修复任务并重新验收。

上下文压缩或会话恢复后，执行者 SHALL 先读取：

1. 本 `spec.md`
2. `tasks.md`
3. `checklist.md`
4. `docs/execution-log.md` 最新记录
5. `git status --short --branch`

## Canonical Acceptance Goal

所有阶段至少重复执行以下自然语言 Goal：

> 匿名访问 Automation Exercise，从 Products 页面搜索 Blue Top，确认搜索结果，进入商品详情，将数量保持为 1，加入购物车，通过加购弹层打开 View Cart，并验证购物车中商品名为 Blue Top、单价和总价均为 Rs. 500、数量为 1。不得注册、登录、结账或填写个人信息；默认不使用 Vision。

验收输入 SHALL 只包含业务自然语言：

- 不得包含 DSL。
- 不得包含 CSS、XPath、DOM selector。
- 不得包含人工 candidates。
- 不得引用历史 generation。

统一官方执行链 SHALL 为：

```text
Natural Language Goal
  -> Go AgentRun
  -> LLM Tool Calling
  -> explore_page / explore_flow
  -> Browser Worker A11y/DOM Observation
  -> validate_page_elements
  -> generate_dsl
  -> Go DSL Validation
  -> Browser Worker Locator Preflight
  -> DSL Artifact
  -> Approval Checkpoint
  -> approve_dsl=true
  -> execute_dsl
  -> PostgreSQL Batch/Job
  -> Execution Worker
  -> Playwright
  -> Step Evidence / FailureSignal / Report
  -> get_report
  -> Agent Final Answer
```

统一成功条件：

- 审批前不存在该 Run 对应的新 Batch。
- 审批绑定当前 `generation_id` 和 DSL SHA256。
- AgentRun 最终状态为 `completed`。
- Batch、Job 和 Execution 均为 `passed`。
- 所有 DSL 步骤均有 evidence。
- 最终 URL 为 `/view_cart`。
- 独立 Oracle 精确验证单一 `#product-1` 行中的名称、单价、数量和总价。
- `vision_calls=0`，除非该 Stage 明确测试 Vision。
- Agent 最终回答与持久化 Report 一致。

## ADDED Requirements

### Requirement: Stage 0 Full-Chain Baseline

系统 SHALL 固化当前自然语言到正式执行链路，并消除已发现的工具合同、恢复、CSS、preflight、空输入和时间语义问题。

#### Scenario: 无提示完整成功

- **WHEN** 仅提交 Canonical Acceptance Goal
- **THEN** Agent 自主探索并生成合法 DSL
- **AND** DSL 在用户审批前不得执行
- **AND** 审批后正式执行 18/18 通过
- **AND** 连续三次 live run 均满足独立 Oracle

#### Scenario: 模型生成非法工具参数

- **WHEN** LLM 生成未知 action、缺失字段或非法 JSON arguments
- **THEN** 系统记录 `tool.failed`
- **AND** 将结构化错误回注 Transcript
- **AND** Agent 在 max-turn 预算内自我修正
- **AND** 不因可恢复业务校验错误立即终止 Run

### Requirement: Research Fact Completeness

系统 SHALL 在构建 Trajectory 前保证每项研究指标都可由持久化原子事实重算。

#### Scenario: LLM 使用量

- **WHEN** 模型完成一次推理
- **THEN** 记录 provider、model、prompt version、input tokens、output tokens、total tokens 和 latency
- **AND** 缺失 usage 时显式标记 unavailable，不得记录为 0

#### Scenario: 前后置验证

- **WHEN** 执行带 Preconditions 或 Postconditions 的步骤
- **THEN** 在动作前采集 pre-state
- **AND** 持久化每个条件的期望值、实际值、状态和耗时

#### Scenario: 网络验证

- **WHEN** DSL 声明 `network_request` 条件
- **THEN** 使用真实步骤级网络事件匹配 URL、方法和状态
- **AND** 未观察到请求时验证失败，不得占位成功

#### Scenario: 非幂等动作保护

- **WHEN** click、submit 或 add-to-cart 已执行但验证失败
- **THEN** Runner 不得自动换候选重复执行
- **AND** 将结果交给 Recovery Policy 决定后续动作

### Requirement: Versioned Research Persistence

系统 SHALL 在 PostgreSQL 中持久化版本化实验、运行和 Transition。

#### Scenario: 创建实验

- **WHEN** 创建一个研究实验
- **THEN** 保存 Goal/Dataset/Model/Prompt/Browser/Code/Policy 版本、seed、variant 和 repetition 配置

#### Scenario: 关联完整因果链

- **WHEN** 一个 Research Run 完成
- **THEN** 可从 `research_run_id` 追踪到 `agent_run_id`、`generation_id`、`batch_id` 和 `execution_id`

#### Scenario: 幂等投影

- **WHEN** 对相同来源事件和报告重复执行 Projector
- **THEN** Transition 数量和内容保持一致
- **AND** 不产生重复 ordinal

### Requirement: Versioned Trajectory

系统 SHALL 将原始事件和执行报告投影为可用于离线分析的 Transition。

#### Scenario: 完整回放

- **WHEN** 从 Agent Event `seq=0` 投影一个已完成 Run
- **THEN** 每个 Transition 包含 state、observation、candidate、decision、action、execution、verification、failure、recovery、reward、cost 和 done
- **AND** 每个字段携带来源引用或明确的 unavailable 原因

#### Scenario: JSONL 导出

- **WHEN** 导出一个实验或 Research Run
- **THEN** 输出 schema-versioned `trajectory.jsonl`
- **AND** 顺序稳定
- **AND** 每行通过 JSON Schema 校验

### Requirement: Research Metrics

系统 SHALL 从持久化事实计算指标，而不是从 Agent 文本结论推断。

#### Scenario: 指标计算

- **WHEN** Research Run 终止
- **THEN** 分别计算 task success、grounding accuracy、invalid action rate、execution success、verification success、recovery rate、steps、retries、LLM calls、tokens、latency 和 vision calls

#### Scenario: 缺失分母

- **WHEN** 指标没有 eligible population 或 ground truth
- **THEN** 指标值为 null 并记录原因
- **AND** 不得默认为 0 或 100%

### Requirement: Executable Action IR

研究 DSL v1 SHALL 表达 Intent、Target、Preconditions、Action、Postconditions 和幂等语义。

#### Scenario: Research DSL 校验

- **WHEN** research-v1 DSL 缺失 Intent 或必需 Preconditions/Postconditions
- **THEN** Go 和 Python 在入队前拒绝

#### Scenario: Legacy DSL 兼容

- **WHEN** 执行现有 legacy DSL
- **THEN** 保持现有行为
- **AND** 报告明确记录 DSL profile

### Requirement: Reproducible Ablation

系统 SHALL 支持 Direct、Candidate、DSL、DSL+Verification 四个初始实验变体。

#### Scenario: 公平比较

- **WHEN** 执行同一实验
- **THEN** 四种 variant 使用同一 Goal、模型、Prompt 基线、浏览器和数据集版本
- **AND** 每个 variant 至少执行 20 次
- **AND** 顺序随机化且 warm-up 单独标记

#### Scenario: Direct 变体

- **WHEN** 执行 Direct 变体
- **THEN** primitive action 仍包装在合法 DSL envelope 中
- **AND** 不允许自由文本直接调用 Playwright

### Requirement: Independent E2E Oracle

每个阶段 SHALL 使用独立于被测 DSL 的确定性 Oracle 判断业务成功。

#### Scenario: 防止假通过

- **WHEN** DSL 自身断言全部通过但独立 Oracle 失败
- **THEN** `task_success=false`
- **AND** 保存不一致证据

#### Scenario: 负向变异

- **WHEN** 期望价格改为 `Rs. 999` 或商品改为不存在项
- **THEN** E2E 必须失败
- **AND** 不得被 Agent 最终文本覆盖

### Requirement: Failure Diagnosis and Recovery

系统 SHALL 将失败表示为稳定的 category、stage 和 code，并由 Go 选择受预算约束的恢复策略。

#### Scenario: 可恢复故障

- **WHEN** 注入 selector drift、stale DOM、临时网络失败或缺失 modal
- **THEN** 选择匹配的 RE_GROUND、RE_EXPLORE 或 RETRY_TRANSIENT
- **AND** 恢复次数不超过配置预算
- **AND** 保存旧新 DSL diff、审批和 retry lineage

#### Scenario: 不可恢复故障

- **WHEN** 错误属于业务期望、权限或无法安全回滚的副作用
- **THEN** 策略返回 MANUAL
- **AND** 不得重复执行非幂等动作

### Requirement: Adaptive Observation Routing

系统 SHALL 由 Go Controller 选择 A11y、A11y+DOM 或 Vision observation profile。

#### Scenario: 默认路径

- **WHEN** A11y/DOM 候选足够且置信度满足门槛
- **THEN** 不调用 Vision

#### Scenario: 路由记录

- **WHEN** Controller 选择 observation strategy
- **THEN** 记录 state summary、candidate count、confidence、reason code、budget、cost 和 outcome

### Requirement: Contextual Bandit

系统 SHALL 仅在数据质量门槛满足后训练和评估 Contextual Bandit。

#### Scenario: 启动门槛

- **WHEN** Projector 一致性低于 100%、关键字段完整率低于 99%、routing decision 少于 1000 或任一 action 有效样本少于 200
- **THEN** 禁止发布学习策略

#### Scenario: 安全发布

- **WHEN** 离线策略在任务切分和时间切分上通过置信界门槛
- **THEN** 先进入 shadow 模式
- **AND** 无安全违规后才能小流量启用

### Requirement: Sequential Policy Decision Gate

系统 SHALL 将 Sequential RL 作为证据驱动的条件阶段。

#### Scenario: 满足序列依赖

- **WHEN** history-aware 策略跨至少三个数据切分稳定优于 Contextual Bandit，成功率提升至少 3 个百分点或 regret 降低至少 10%，并且有效长轨迹不少于 10000
- **THEN** 实施离线 Sequential Policy 和受控灰度

#### Scenario: 不满足序列依赖

- **WHEN** 上述门槛不满足
- **THEN** 产出可复现的 no-go 报告
- **AND** 以 Contextual Bandit 作为完整平台的最终策略层

## MODIFIED Requirements

### Requirement: Agent Event Protocol

现有 Agent Event SHALL 增加版本化 research event：

- `research.run.started`
- `research.observation.recorded`
- `research.decision.recorded`
- `research.action.recorded`
- `research.execution.recorded`
- `research.verification.recorded`
- `research.failure.classified`
- `research.recovery.recorded`
- `research.metric.recorded`
- `research.run.finished`

事件 SHALL 复用现有 `agent_events`、原子 `seq` 和 SSE replay，不新增平行消息总线。

### Requirement: FailureSignal

现有 category SHALL 保持兼容，并增加：

- `stage`
- `code`
- `retryable`
- `side_effect_committed`
- `source_event_seq`

### Requirement: Execution Evidence

现有 Step Evidence SHALL 增加：

- precondition results
- postcondition results
- observation reference
- full candidate attempt list
- selected candidate
- action outcome
- side-effect state
- modality and cost

## REMOVED Requirements

### Requirement: 用最终成功率替代研究证据

**Reason**: 最终成功不能解释 grounding、verification、recovery 和成本，也容易产生假通过。

**Migration**: 保留现有 pass rate 作为业务指标，同时新增基于 Transition 的研究指标。

### Requirement: 在正式执行器中允许自由文本动作

**Reason**: 违反结构化 DSL、安全审批和可复现要求。

**Migration**: Direct baseline 仍使用最小 DSL envelope，仅减少 candidate 和 verification 组件。

### Requirement: 无门槛直接实施 PPO/GRPO

**Reason**: 当前问题首先是 observation/recovery strategy 选择，缺少证明其为 Sequential Decision Process 的数据。

**Migration**: 先完成确定性策略和 Contextual Bandit；仅在 Sequential Policy Decision Gate 通过后进入 RL。
