# 04 命名与分类

用户反馈的三个具体症状：**命名奇怪、分类奇怪、结构混乱**。本文给出可执行的重命名与目录重构对照表。

## 1. 命名问题清单

| 现状 | 问题 | 建议 |
|---|---|---|
| `research-v1` / `research-v2` / `legacy-v1` | "research"是**阶段名**（研究试点），不是数据格式名；v1/v2 是同一格式的两次迭代 | 统一为 `case/v3`；删除另两个 |
| `dsl.ValidatedCase` | 名字说了"已校验"，但结构里混了 canonical/phase/profile 三种信息 | `case.Canonical` + 独立 `case.Phase` |
| `dsl.CanonicalDSLBinding` | "DSL 的 DSL"；实际是"审批时冻结的工件引用" | `case.ArtifactRef{ID, ContentHash, Version}` |
| `DSLGeneration` / `dsl_generation_runs` | 它是"一次生成尝试"，但也被当作"已批准工件"的来源 | 拆成 `GenerationAttempt`（审计）与 `CaseArtifact`（数据） |
| `generated_case_json` | 名字没说它是编译器产物还是模型产物 | `payload`（工件内） / `draft`（尝试内） |
| `plan_binding` / `observation_bindings` | "binding"一词同时指计划绑定、观测绑定、目标绑定，三义 | `plan_ref`（计划引用）、step 内 `evidence`（观测谱系） |
| `target_binding_id` / `selected_candidate_id` / `page_state_id` | 三个 id 平铺在 step 上 | 收进 `evidence` 对象 |
| `semantic_target` vs `target` | 同一概念两个名字，且分属草稿/可执行 | 统一 `target`，由编译器填充 |
| `locator_candidates` vs `candidates` | 同上 | 统一 `candidates` |
| `test_case_runs` vs `execution_jobs` | 两张表描述同一次执行的两个视角，名字无法区分 | `executions`（一次执行）/ `execution_jobs`（队列项）/ `step_results`（步骤结果） |
| `harness` | 名字不表达职责（它其实是"Agent 运行时与阶段编排"） | `agentruntime` |
| `taskplan` | 与 `planning` 并存，职责重叠 | 合并为 `plan` |
| `tools` | 装的是"控制面能力"，不是通用工具 | `capabilities` |
| `agentservice` | 与 `agent` 包职责边界不清 | `api/agentservice` |
| `platform/llm` | 单数包名 + 具体实现混在平台层 | `platform/llm`（保留，但把 provider 实现移入 `platform/llm/deepseek`） |
| `contracts/action_ir.py`、`action_ir_v2.py`、`dsl.py` | 三个文件描述同一对象，名字互不相关 | 合并为 `contracts/case.py` |
| `contracts/browser_capabilities.py` + `browser_executions.py` | 能力与执行契约混放 | 合并为 `contracts/capability.py` + `contracts/execution.py` |
| `scripts/research_e2e.py`（2759 行） | 与 `run_agentic_e2e.py` 职责重叠 | 删除或合并，只保留一个驱动 |

## 2. 包/模块重新划分

### 2.1 Go：20 个 internal 包 → 4 上下文 + 3 内核

| 现状包 | 归属 |
|---|---|
| `agent`, `harness`, `agentservice` | `agentruntime`（Agent 循环、提示词、阶段编排、Run 服务） |
| `taskplan`, `planning` | `plan`（TaskPlan 状态机、接地） |
| `dsl`, `cases` | `casegen`（草稿校验、编译器、工件） |
| `execution`, `corrections` | `execution` |
| `research`, `observation` | `reporting` |
| `projects`, `platform`（部分） | `platform`（项目/成员是薄 CRUD，不值得独立上下文） |
| `transport/http` | `api/http` |
| `browsercontract` | `contract` |
| `dbschema`, `testpg` | `store/migrations`、`testutil` |
| `integration` | 保持为跨上下文测试包 |

目标目录：

```
backend-go/
  cmd/
    migrate/ agentservice/ execution-worker/ pipeline-audit/
  internal/
    contract/          由 schema 生成
    plan/              TaskPlan、接地、版本
    casegen/           校验、编译、工件、审批
    execution/         队列、worker、结果
    reporting/         报告、失败信号、验收
    agentruntime/      提示词、工具分派、阶段边界、Run 服务
    store/             postgres（按上下文子目录）、migrations
    platform/          llm、http、log、config
    api/http/          薄 handler
    testutil/
```

### 2.2 Python：9 个模块 → 6 个

```
browser_worker/
  contracts/    生成物 + 形状校验
  exploration/  explorer.py / collector.py / a11y_name.py / preflight.py
  locators/     semantic.py / scoring.py / correction.py / visual.py
  runners/      runner.py / conditions.py / actions.py / click_preprocess.py
  reporting/    report.py / acceptance.py / signals.py
  server/       app.py / routes.py
  runtime/      config.py / logging.py
```

明确删除：`contracts/dsl.py`、`contracts/action_ir.py`、`contracts/action_ir_v2.py`（合并）、`scripts/research_e2e.py`（合并或删除）。

### 2.3 前端

`frontend/src` 只有 5,434 行，问题不大，但需要跟随契约变化：

- 用例编辑器改为**写工件**（走同一编译器），不再拼 body。
- 删除对 `page_state` / `candidates` 等旧字段的处理。
- `services/` 按上下文分组（plan / casegen / execution / reporting）。

## 3. 巨型文件拆分

| 文件 | 行数 | 拆分为 |
|---|---|---|
| `agent/tool_result.go` | 2014 | `tool_result.go`（类型）+ 各工具的投影器（每个 ≤200 行） |
| `taskplan/service.go` | 1607 | `service.go`（编排）+ `grounding.go` + `conditions.go` + `steps.go` |
| `research/projector.go` | 1488 | 按投影目标拆（event / metric / trace） |
| `harness/harness.go` | 1375 | `prompt.go` + `dispatch.go` + `phase.go` + `handoff.go` |
| `research/source.go` | 1231 | `source.go` + `source_query.go` |
| `research/postgres.go` | 1100 | 按仓储拆 |
| `execution/store.go` | 1000 | `batch.go` + `job.go` + `report.go` + `validate.go` |
| `scripts/research_e2e.py` | 2759 | 删除或拆为 `e2e/` 包 |
| `exploration/page_explorer.py` | 1880 | `explorer.py` + `collector.py` + `a11y_name.py` |
| `runners/playwright_runner.py` | 1681 | `runner.py` + `conditions.py` + `actions.py` |
| `scripts/run_agentic_e2e.py` | 1361 | `e2e/driver.py` + `e2e/assertions.py` + `e2e/oracle.py` |

规则：**任何新文件超过 500 行必须在评审中说明理由**；超过 800 行视为重构未完成。

## 4. 分类规则（防止再次混乱）

1. **按职责分包，不按实体分包。** 一个实体不值得一个顶层包；实体属于某个上下文。
2. **契约只在 `contract/`。** 业务包不得自定义跨进程 JSON 结构。
3. **一个概念一个名字。** 出现"同一概念两个名字"时，必须当场合并，不允许并存（`semantic_target`/`target` 就是反例）。
4. **阶段名进类型，不进函数名。** 用 `Phase` 参数，不要 `ValidateDraftCase` / `ValidateExecutableCase` / `ValidateCompiledCaseSemantics` 这种把阶段写进名字的函数族。
5. **测试文件与实现文件同名同目录**，巨型 `*_test.go` 说明被测单元过大，应拆实现而不是拆测试。

## 5. 验收标准

1. `internal/` 顶层包 ≤ 10 个。
2. 无超过 800 行的文件。
3. `grep -rn "research-v1\|legacy-v1\|action_ir\|semantic_target\|page_state\b" backend-go browser-worker/src` 结果为空（历史迁移代码除外）。
4. 新加入者能在 10 分钟内通过目录树说清"一次执行的数据从哪里来、到哪里去"。
