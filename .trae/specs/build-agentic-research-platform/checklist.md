# 验收检查清单

## 全局门禁

- [x] 所有正式测试均从纯自然语言 Goal 开始，不包含 DSL、CSS、XPath 或人工 candidates。
- [x] 所有正式执行均经过 Go DSL 校验、Locator Preflight 和显式审批 Checkpoint。
- [x] Python Browser Worker 中不存在 Planning、Agent loop、Reward 或策略选择逻辑。
- [x] Agent 最终结论与 PostgreSQL 中的结构化 Report 一致。
- [x] 每步都有 observation/target/candidates/final match 或 failure reason/evidence。
- [x] 独立 Oracle 参与 task_success 判定，不能仅依赖 DSL 自身断言。
- [x] 负向变异 Goal 必须失败，且不能被 Agent 文本结论覆盖。
- [ ] 所有实验记录 Goal/Dataset/Model/Prompt/Browser/Code/Policy/seed 版本。
- [ ] VLM 默认关闭，启用时有明确策略、预算和调用记录。
- [x] 所有新增 Schema 都有版本号和兼容迁移。
- [x] 每个 Stage 通过后已更新任务、检查清单、执行日志和缺陷日志。
- [ ] 每个 Stage 有独立 commit 并已推送 `origin/main`。

## Stage 0：完整链路基线

- [x] 当前未提交的完整链路修复已审查，无无关改动。
- [x] generate_dsl Tool Schema 仅暴露受支持 action 和正确字段。
- [x] 可恢复 Tool 错误会回注 Transcript，不会立即终止 AgentRun。
- [x] 非法 JSON tool arguments 能进入 Registry/Harness 纠错流程。
- [x] 复合 CSS 可被探索器和 Runner 一致识别。
- [x] Preflight 能按 verified selector 匹配并返回具体 warning。
- [x] nil input values 被持久化为 `{}`。
- [x] 新执行记录 duration 非负且时间语义一致。
- [x] Canonical Goal 不含 selector，连续 3 次通过。
- [x] 错误价格和错误商品负向 Goal 均失败。
- [x] 审批前无 Batch，审批后 generation 与 DSL SHA 绑定。
- [x] 独立 Oracle 确认购物车只有一条 Blue Top，价格和数量正确。
- [x] Stage 0 已 commit 并 push。

## Stage 1：研究事实完整性

- [x] 所有 action 的 pre-state 在动作前采集。
- [x] Preconditions 和 Postconditions 输出逐条件结果。
- [x] `network_request` 使用真实网络事件，不再占位成功。
- [x] 已提交副作用的动作不会因验证失败自动重放。
- [x] FailureSignal v2 保留 category，增加版本、stage/code/retryable/side-effect 与可验证来源，并兼容 v1。
- [x] 同步和流式 Runner 产生一致 evidence。
- [x] Canonical Goal 连续 3 次通过。
- [x] 网络与重复副作用故障测试通过。
- [x] Stage 1 提交前准备已完成；commit/push 待后续执行。

## Stage 2：LLM 遥测

- [x] 每次模型调用记录 provider、model 和 prompt version。
- [x] input/output/total tokens 可用时准确记录。
- [x] usage 不可用时记录 unavailable，而不是 0。
- [x] latency、retry 和错误类别可追踪。
- [x] 遥测事件可通过 PostgreSQL 和 SSE 重放。
- [x] 日志不泄漏 API Key、Cookie 或用户敏感值。
- [x] 每个新遥测事件显式标记 ToolCall available/unavailable；单/多/无 ToolCall 和失败 attempt 可判定，旧事件仍可读取。
- [x] 官方 Canonical E2E 通过长期 Browser API 执行真实 Chromium，连续 3 次通过。
- [x] Stage 2 提交前准备已完成；commit/push 待主代理执行。

## Stage 3：Research Persistence

- [x] Experiment、ResearchRun、Transition Go 类型已定义。
- [x] Repository 接口和 PostgreSQL adapter 已实现。
- [x] 三张 research 表、外键、唯一约束和索引已迁移。
- [x] 空库与已有库 upgrade 通过。
- [x] downgrade/upgrade 往返通过。
- [x] 并发写入和重复写入测试通过。
- [x] Research Run 可关联 AgentRun、Generation、Batch 和 Execution。
- [x] 完整工具结果持久化可回放，模型仅接收可追溯、有界的确定性探索摘要。
- [x] Canonical Goal 通过并写入关联链。
- [x] Stage 3 已完成提交前验收；主代理随后 commit 并 push。

## Stage 4：Trajectory 与 Dataset

- [x] `research.event.v1` envelope 已定义。
- [x] Observation/Decision/Action/Execution/Verification/Failure/Recovery/Reward 事件可持久化。
- [x] Projector 使用因果 ID 和 source seq，不依赖 wall-clock 排序。
- [x] Transition ordinal 连续且唯一。
- [x] 删除投影后可从原始事实完整重建。
- [x] 重复投影结果一致。
- [x] JSONL 导出顺序稳定并通过 Schema 校验。
- [x] Canonical Goal 连续 3 次通过且可完整回放。
- [x] Stage 4 已完成提交前验收；按本轮要求未 commit/push。

## Stage 5：Metrics 与实验编排

- [ ] success、grounding、invalid action、verification、recovery 指标独立计算。
- [ ] steps、retries、tokens、latency、vision cost 可重算。
- [ ] 零分母和缺失真值返回 null 与原因。
- [ ] Experiment API 支持固定版本、seed、variant 和 repetition。
- [ ] 每个 repetition 使用 clean browser context。
- [ ] warm-up 与正式样本分开。
- [ ] `research-e2e run/verify/export` 可用。
- [ ] 独立 Oracle 失败时 task_success=false。
- [ ] Canonical Goal 连续 3 次通过。
- [ ] Stage 5 已 commit 并 push。

## Stage 6：Action IR

- [ ] research-v1 步骤包含 Intent、Target、Preconditions、Action、Postconditions。
- [ ] 非幂等语义和安全重试条件明确。
- [ ] Go 先定义类型和校验，Python 合同保持一致。
- [ ] Legacy DSL 仍可运行并标记 profile。
- [ ] research-v1 未知字段和缺失字段在入队前失败。
- [ ] Go/Python golden fixtures 全部通过。
- [ ] Canonical Goal 连续 3 次通过。
- [ ] Stage 6 已 commit 并 push。

## Stage 7：Ablation

- [ ] Direct、Candidate、DSL、DSL+Verification 四种 profile 可配置。
- [ ] Direct 不绕过 DSL envelope 或审批门。
- [ ] 至少四个 Automation Exercise Goal 已版本化。
- [ ] 每个 variant 至少 20 次正式 repetition。
- [ ] 执行顺序随机且控制变量一致。
- [ ] 报告包含 95% 置信区间。
- [ ] 原始运行结果与 manifest 均保留。
- [ ] Stage 7 已 commit 并 push。

## Stage 8：Diagnosis 与 Recovery

- [ ] Failure stage/code 分类有确定性测试。
- [ ] Recovery strategy、预算、成本和 attempt lineage 可追踪。
- [ ] selector drift 触发 RE_GROUND。
- [ ] stale DOM/页面变化触发 RE_EXPLORE。
- [ ] 临时网络失败触发 RETRY_TRANSIENT。
- [ ] 错误业务期望触发 MANUAL。
- [ ] DSL 变化后必须重新审批。
- [ ] 恢复过程中没有重复非幂等副作用。
- [ ] Canonical Recovery Goal 在预算内完成。
- [ ] Stage 8 已 commit 并 push。

## Stage 9：Observation Routing

- [ ] Go Controller 负责 observation strategy。
- [ ] Worker 仅提供 A11y、DOM、Vision observation capability。
- [ ] Observation 已压缩并记录原始引用/hash。
- [ ] 每次路由记录 confidence、reason、budget、cost 和 outcome。
- [ ] 默认 A11y 路径通过 Canonical Goal。
- [ ] 无 accessible name 场景正确升级到 A11y+DOM。
- [ ] Vision 禁用时调用数严格为 0。
- [ ] Vision 失败时能按策略回退。
- [ ] Stage 9 已 commit 并 push。

## Stage 10：Contextual Bandit

- [ ] Projector 重建一致性为 100%。
- [ ] 关键训练字段完整率至少 99%。
- [ ] 至少 1000 个 routing decisions。
- [ ] 每个候选 action 至少 200 个非零 propensity 样本。
- [ ] Policy Registry 支持版本、状态和回滚。
- [ ] 离线评估同时使用任务切分和时间切分。
- [ ] 策略下置信界通过且 task success 下界下降不超过 2 个百分点。
- [ ] Shadow 模式无安全违规。
- [ ] 小流量策略仍通过官方 Agent E2E 和独立 Oracle。
- [ ] Stage 10 已 commit 并 push。

## Stage 11：Sequential Gate 与最终体系

- [ ] 已完成序列依赖统计检验。
- [ ] 已积累至少 10000 条有效长轨迹，或明确记录数据不足。
- [ ] 若门槛满足，Sequential Policy 离线和灰度验收通过。
- [ ] 若门槛不满足，no-go 报告可复现且明确以 Bandit 为最终策略。
- [ ] Canonical、Ablation、Recovery、Observation 全套回归通过。
- [ ] Dataset export/reimport 后指标一致。
- [ ] 全部安全边界和审批门保持有效。
- [ ] 所有文档、迁移和运行手册已更新。
- [ ] 最终提交已推送 `origin/main`。
- [ ] `main` 与 `origin/main` 一致，工作区干净。
