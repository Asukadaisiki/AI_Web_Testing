# 精简 Grounding 架构：删除 groundingplan 双状态机，回归 taskplan 单一权威

- 日期：2026-09-13
- 状态：已实施
- 触发缺陷：BUG-196（grounding 状态机拒绝 `query_observation`：`mutation does not target the current grounding step`）
- 关联：BUG-191、BUG-197；`docs/plan/taskplan-state-machine-2026-09-08.md`、`docs/superpowers/specs/2026-09-13-semantic-taskplan-dynamic-grounding-plan-design.md`、`docs/superpowers/plans/2026-09-13-dynamic-grounding-plan.md`

---

## 1. 背景与问题

当前 grounding 阶段由**两套并行状态机**在记录同一件事：

| | `taskplan.Plan`（真正的权威） | `groundingplan.Plan`（半接线的影子） |
|---|---|---|
| 步骤状态 | pending → grounded / failed | pending → candidates_available → probing → grounded / failed |
| 推进方式 | `RecordToolResult` 根据 explore 结果直接置 grounded + 写 `TargetBinding` | `RecordObservationQuery` / `StartCandidateProbe` / `RecordProbeResult` |
| 顺序门禁 | `contiguousPendingSteps`（要求从下一个 pending 步连续） | `CurrentPlanStepID`（第一个非 grounded 步） |

数据库实据（`ai_web_testing`，run_a304308…）：

- `task_plan_steps`：`open_products`=grounded、`search_input`=grounded、`click_search`=pending（正确）。
- `grounding_plans`：**12 步全 pending**，`current_plan_step_id=open_products`（永远卡在第 0 步）。

根因（三条，都在 `groundingplan` 包）：

1. `EnsureForTaskPlan` 建 grounding plan 时把**全部步骤**初始化为 `pending`、`CurrentPlanStepID = steps[0].ID`，无视 task plan 已 grounded 的步骤（taskplan 有 `carryForwardGrounding` 继承逻辑，groundingplan 没有）。
2. grounding plan 对 `goto`/`assert_text`/`capture_text`（不可 query 的步骤）也建 grounding step；`query_observation` 的 action 仅限 `click`/`input`/`wait_for`，于是 `CurrentPlanStepID` 被一个**永远无法推进**的 goto 步卡死。
3. 成功路径从不调用 `groundingplan.RecordProbeResult(evidence)`（grep 确认：生产代码只有 `browser.go` 的失败分支 `recordProbeFailures` 和小范围测试调用）。grounding plan 的终态 `grounded` 生产代码中**不可达**。

结论：`groundingplan` 不是「有 bug 的组件」，而是**一段未完成的重构留下的半接线影子状态机**——它不记录任何 taskplan 不知道的东西，却用一套脱节的 `CurrentPlanStepID` 挡住了正常推进。

---

## 2. 结论摘要（TL;DR）

- **删除** `groundingplan` 的 `Service`/`Plan`/`Step`/`Repository`/`PostgresRepository`/`MemoryRepository` 及 `grounding_plans` 表与迁移。
- **保留并以 `taskplan` 为唯一权威**：它已经能通过 `RecordToolResult` → `deriveTargetBindings` / `derivePageTargetBindings` 正确地把步骤置 grounded 并写 `TargetBinding`。
- **`query_observation` 工具整体删除**——候选检索不再需要独立工具，`explore` 摘要已暴露 `source_event_seq`/`probe_id`/`observation_id`/`candidate_id`，足以直接拼出 `candidate_ref`。
- **`explore_flow` 的 `candidate_ref` 水合链路保留**（「AI 不编选择器」的安全边界），只删掉其中对 `groundingplan.StartCandidateProbe` 的两行记账调用。
- `ObservationReader`（读持久化观察、解析候选）迁至新包 `internal/observation`，仅保留 `ResolveCandidate`，不再承担查询检索。

净效果：grounding 调用复杂度从 **O(步数 × 2)** 降回 **O(页面转换) + 歧义重试**，与「每页探索一次、批量绑定该页元素」的心智模型一致。

---

## 3. 本质复杂度 vs 偶然复杂度

保留（本质，必须）：禁止 AI 编选择器（`candidate_ref` → 控制面 hydrate）；定位器运行时唯一命中；DSL 语义不可篡改（`taskplan.ValidateGenerationBinding` / 编译期校验）。

删除（偶然，可砍）：`groundingplan` 整套独立状态机；`query_observation` 工具整个；逐步隔离 probe（保留 `explore_flow` 一次多步的能力，但不再要求每步单独 probe 记账）。

---

## 4. 目标数据流（简单模型）

```text
1. set_task_plan            模型产出 N 个语义步骤（taskplan.CreateVersion；粒度建议收敛为「页面动作 + 对应断言」）
2. explore_page/explore_flow
   - 返回 a11y 快照 + 候选（每个候选带 element_ref / verified locator / observed_count / candidate_ref）
   - 唯一命中的步骤：taskplan.RecordToolResult 自动 deriveTargetBindings → 置 grounded（无额外 LLM 往返）
   - 歧义步骤：模型从快照选 candidate_ref，在下一次 explore_flow 里带进去（grounding.query.v2）→ 控制面 hydrate → 浏览器验证 → taskplan 置 grounded
3. generate_dsl              从 taskplan 的 target_bindings 编译 DSL（不变）
4. approve → execute         运行时唯一匹配校验，失败则 intervention（不变）
```

示例（Blue Top 加购，4 个页面状态）：

```text
set_task_plan(1 次)
explore_page(products)                                   # 绑定 open_products / search_input（若唯一命中）
explore_flow(input Blue Top + click search)              # 绑定 search_input / click_search，并发现搜索结果页
explore_flow(click Blue Top → detail)                    # 绑定 open_detail，发现详情页
explore_flow(click add_to_cart → view_cart)              # 绑定 add_to_cart / open_view_cart，发现购物车页
explore_flow(candidate_ref)             # 仅歧义时带 candidate_ref 复探
generate_dsl(1 次) → approve(1 次) → execute(1 次)
```

4 次页面转换 ≈ 4–6 次 explore，而非 20+。

---

## 5. 目标状态机（单一权威 = taskplan）

只保留 `taskplan` 的状态与步骤状态，语义不变：

- 计划状态：`grounding → ready_for_generation → awaiting_approval → approved → executing → completed/failed/blocked`。
- 步骤状态：`pending → grounded / failed`；`TargetBinding`（ProbeID/ObservationID/PageStateID/ElementRefs/Candidates/SelectedCandidateID）承载「选中了哪个元素、用什么定位器、命中几次」。
- 顺序门禁：沿用 `contiguousPendingSteps`（要求下一个 pending 步连续）——它已经存在且只依赖 taskplan。

删除 `groundingplan` 的 `CurrentPlanStepID`、`StepStatus`（pending/querying/candidates_available/candidate_selected/probing/grounded/failed/blocked）等全部派生概念。

---

## 6. 删除清单

代码（`backend-go/internal/groundingplan/` 与 `internal/tools/`）：

- `service.go`、`types.go`（状态机本体）、`postgres_repository.go`、`memory_repository.go` 及其测试：整个删除。
- `internal/tools/observation_query.go`（`query_observation` 工具）及其测试 `observation_query_test.go`：整个删除。
- `observation_reader.go` **不删**，整体迁移到新包 `internal/observation`（见 §7）。

数据/SQL：

- `internal/dbschema/groundingplan.sql` 与 `schema.go` 中的 `//go:embed groundingplan.sql`。
- `grounding_plans` 表：对本地开发库 `DROP TABLE IF EXISTS grounding_plans CASCADE`（或直接 re-initdb，本里程碑无生产数据约束）。

装配/入口：

- `cmd/agentservice/main.go` 中 `groundingplan.NewService` / `groundingplan.NewPostgresRepository` 两行装配删除；`groundingplan.NewObservationReader(runService)` 改为新包名。

--- 

## 7. 保留与最小改动

保留不变：

- `taskplan` 包（`Service`、`Plan`、`Step`、`TargetBinding`、`contiguousPendingSteps`、`RecordToolResult`、`AuthorizeResolvedCandidates`、`deriveTargetBindings`/`derivePageTargetBindings`）。**它就是唯一权威，逐字不动。**
- `browsercontract`（`CandidateRef`、`ResolvedTargetEvidence`、`TrustedResolvedCandidate`、`LocatorSpec` 等）。
- 探索工具链：`explore_page` / `explore_flow` 的 `candidate_ref` → `hydrateCandidateReferences` → `AuthorizeResolvedCandidates` → Worker 的「控制面水合」安全边界。
- DSL 编译、审批、执行、修复（`generate_dsl` / `ask_user_question` / `execute_dsl` / `get_report` / `fix_and_retry`）。

改动（三处）：

1. `internal/tools/browser.go`：
   - 删除 `groundingPlans *groundingplan.Service` 字段与构造参数。
   - `hydrateCandidateReferences` 中删除 `groundingPlans.StartCandidateProbe(...)` 循环（其余保留）。
   - 删除 `recordProbeFailures` / `withProbeFailures` 及 `Execute` 里的错误包装（Worker 失败即走 harness 的可恢复工具失败，步骤保持 pending，等模型重试）。
2. `internal/tools/observation_query.go`：整个文件删除（含 `observation_query_test.go`）。
3. `ObservationReader` 迁移到新包 `internal/observation`（package `observation`），仅保留 `ResolveCandidate` 解析侧；`ObservationQuery`/`ObservationQueryResult`/`ObservationQueryMatch` 等查询侧类型随 `query_observation` 一并删除，`capability_tools_test.go` 的引用同步改。

随动测试更新：

- `internal/tools/capability_tools_test.go`：删除 `groundingplan.StepProbing`/`StatusFailed`/`NewMemoryRepository` 等断言；保留候选水合、`resolved_candidate` 禁传等契约测试。
- `internal/tools/observation_query_test.go`：随源码删除。
- `cmd/migrate/main_test.go`、`internal/dbschema/taskplan_test.go`：删除 `grounding_plans` 相关断言（或改为「表已删除」）。
- 新增正向测试：`explore_flow(candidate_ref)` 成功后 taskplan 步骤置 grounded 且无 groundingplan 依赖；`contiguousPendingSteps` 顺序门禁仍生效。

---

## 8. 迁移步骤（分阶段，每步可独立验证）

- **Phase 0｜基线**：`go test -count=1 ./...` 记录当前结果（应全绿，作为回滚基线）。
- **Phase 1｜摘除 browser.go 记账**：删 `StartCandidateProbe` + `recordProbeFailures` 调用。此时 `groundingplan` 仍存在但不再被 browser 工具调用。验证：`go test ./internal/tools/...` + `go build ./...`。
- **Phase 2｜删除 query_observation 工具**：删 `observation_query.go` 及其测试；同步删除 `tool_result.go` 中 query_observation 摘要死代码及其测试。验证：`go test ./internal/agent/... ./internal/tools/...`。
- **Phase 3｜迁移 ObservationReader**：`internal/groundingplan/observation_reader.go` → `internal/observation/`（仅保留 `ResolveCandidate`），更新 `capability_tools_test.go` 引用。验证：全量 `go test ./...`。
- **Phase 4｜删除状态机与表**：删 `service.go`/`types.go`/两个 repository/`groundingplan.sql`/表；更新 `main.go` 装配与 migrate/dbschema 测试。验证：`go test -count=1 ./...`、`go vet ./...`、`go build ./...`。
- **Phase 5｜端到端回归**：重跑 `run_agentic_e2e.py`（Blue Top 加购），确认 `query_observation` 不再报 `mutation does not target…`，grounding 调用次数显著下降，流程能走到 DSL 生成/审批（是否最终跑绿取决于 BUG-197 的空无障碍名按钮定位，另案处理）。

回滚：每阶段提交一个 Conventional Commit；`git revert` 单阶段即可回到上一阶段，且 Phase 0 基线可整体对照。

---

## 9. 验证标准

- [x] `taskplan` 是唯一 grounding 权威，`groundingplan` 整包已删除、无引用残留。
- [x] `grounding_plans` 表已从 schema 与 migrate（`DROP TABLE IF EXISTS`）移除，`migrate`/`dbschema` 测试更新一致。
- [x] `query_observation` 工具本体及其查询侧类型/摘要/测试全部删除。
- [ ] Blue Top E2E 中 `query_observation(click_search)` 不再报 `mutation does not target the current grounding step`；`contiguousPendingSteps` 顺序门禁仍拦截乱序绑定。
- [ ] grounding 阶段 explore 工具调用次数较现状（20+）显著下降，趋近「页面转换数 + 歧义重试」。

---

## 10. 风险与注意事项

- `query_observation` 删除后，模型仍能从 `explore` 摘要暴露的 `source_event_seq`/`probe_id`/`observation_id`/`candidate_id` 直接拼出 `candidate_ref`，候选检索能力不损失（改为内嵌在 explore 摘要中）。
- 删除 `recordProbeFailures` 后，Worker 失败不会再往 grounding plan 写 failed——这本来就是死代码（写进一套将被删除的表），改由 harness 的「可恢复工具失败 + 步骤保持 pending」承接，语义不退化。
- `ObservationReader` 依赖的是 `agent_events`（持久化工具结果）而非 `grounding_plans` 表，迁移无数据依赖；仅包路径变化。
- 本里程碑无生产数据，`grounding_plans` 可直接 DROP；若未来需要「候选/探针审计」，应从 `agent_events` 重建，而非继续维护影子状态机。