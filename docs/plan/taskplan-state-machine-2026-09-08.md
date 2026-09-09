# Agent TaskPlan 状态机

日期：2026-09-08
状态：implemented

## 目标

将任务语义从 system prompt 和 transcript 中提取出来，形成可版本化、可持久化、
可审计的 TaskPlan。LLM 负责提出计划；Go AgentCore 负责校验、状态迁移和工具授权。

```text
Goal
  -> set_task_plan
  -> grounding
  -> ready_for_generation
  -> awaiting_approval
  -> approved
  -> executing
  -> completed | failed | blocked
```

## 领域边界

- `TaskPlan` 拥有目标、计划版本、全局副作用上限、禁止动作和有序 PlanStep。
- `PlanStep` 拥有动作语义、参数、次数、幂等性、副作用、前置条件和完成条件。
- locator-bearing PlanStep 在 grounding 后持有版本化 `TargetBinding`，业务
  `target` 与 Playwright `LocatorSpec` 分离。
- Harness 驱动工具循环，并将工具结果转换为 TaskPlan 状态迁移。
- Policy 继续负责通用调用预算；TaskPlan Service 负责业务计划授权。
- Browser Worker 只采集 grounding evidence，不创建或修改任务语义。
- DSL generation 必须绑定当前 `plan_id/version/sha256`。
- 新计划版本会使旧 generation 和审批失效。

## 持久化

- `task_plans`：计划版本、目标、状态、plan hash 和 generation 绑定。
- `task_plan_steps`：步骤语义、顺序、次数、状态、evidence 引用和
  `target_binding_json`。
- `dsl_generation_runs.plan_id/plan_version/plan_sha256`：DSL 与计划绑定。
- `task_plan.updated`：向 SSE 和研究 trace 暴露状态快照。

## 裁决规则

1. 没有 TaskPlan 时不能探索、生成或执行。
2. 探索必须携带连续的下一组 `plan_step_ids`。
3. `explore_flow` 中的动作必须由这些 PlanStep 所有。
4. 禁止动作不能进入探索或 DSL。
5. `external_state` 和 `unknown` 动作不能在探索阶段执行，只能观察其控件或事实。
6. click/input/capture_text PlanStep 必须同时 grounded 且具有有效
   TargetBinding，计划才能生成 DSL。
7. research-v2 Draft DSL 只引用 PlanStep/TargetBinding；Go 编译器注入
   locator candidates，并保持动作、顺序、参数、次数、幂等性和副作用完全一致。
8. 只有已审批且 generation 匹配的计划才能正式执行。
9. Agent 只有在计划进入 `completed` 后才能正常结束 Run。

## 兼容策略

`harness.New` 保留给既有单元测试和非生产构造；正式服务使用
`harness.NewWithTaskPlans` 开启持久状态机。旧的 transcript-derived 探索充分性门
仅在非 TaskPlan 模式启用，生产主链以持久 PlanStep 状态为准。
