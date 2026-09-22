# 07 删除与合并清单

本清单给出可执行的减法。行数为实测（`Get-Content | Measure-Object -Line`），估算偏保守。

## 1. 直接删除（无产品价值）

| 对象 | 位置 | 行数 | 理由 | 阶段 |
|---|---|---|---|---|
| `research_e2e.py` | `browser-worker/scripts/` | 2,759 | 与 `run_agentic_e2e.py` 职责重叠，仅服务早期试点 | P5 |
| research-v1 契约 | `contracts/action_ir.py` | 364 | 无数据价值，被 v3 取代 | P3 |
| legacy-v1 契约 | `contracts/dsl.py` | 274 | 同上 | P3 |
| Go v1 校验 | `internal/dsl/store.go: validateLegacyCase` 及其分支 | ≈150 | 同上 | P3 |
| Go research-v1 校验 | `internal/dsl/action_ir.go: validateResearchCase` 及其分支 | ≈250 | 同上 | P3 |
| v1 fixture 与测试 | `testdata/`、`tests/test_action_ir.py` 中 v1 部分 | ≈400 | 保留 1 个负例 | P3 |
| `internal/planning` 中与 `taskplan` 重叠的部分 | `internal/planning/` | ≈300 | 两个包描述同一状态机 | P4 |
| 执行队列 `COALESCE` 回退链 | `internal/execution/worker_store.go` | ≈30 | 工件化后不存在多来源 | P2 |
| `validatePersistedCaseBindings` + `validatePersistedCaseMatchesBinding` | `internal/execution/store.go` | ≈120 | 工件化后无比较需求 | P2 |
| `validateConditionPreservation` 或 `validateCompiledConditions` 之一 | `internal/taskplan/condition.go` / `compiler.go` | ≈60 | 合并为单一实现 | P1 |
| `validateCaseSemantics` 或 `validateCompiledCaseSemantics` 之一 | `internal/taskplan/service.go` / `compiler.go` | ≈80 | 同上 | P1 |
| `parseConditionIntent` 正则族 | `internal/taskplan/condition.go` | ≈90 | 计划条件结构化后不再需要 | P1/P3 |
| `page_state`（v1 拼写）全部容忍分支 | Go + Python 多处 | ≈60 | 字段统一后删除 | P3 |
| 临时调试产物 | 仓库根目录 `_*.json` / `_*.py` / `_*.txt` | — | 本次会话遗留 | P0 |
| 临时命令 | `backend-go/cmd/tmp-execute-generation/` | 150 | 一次性运维工具 | P0 |

**小计：≈ 4,800 行直接消失。**

## 2. 合并（消除重复实现）

| 现状 | 合并为 | 减少行数（估） |
|---|---|---|
| `contracts/dsl.py` + `action_ir.py` + `action_ir_v2.py` | `contracts/generated/case.py` | ≈600 |
| `contracts/browser_capabilities.py` + `browser_executions.py` | `contracts/capability.py` + `contracts/execution.py` | ≈150 |
| Go 11 个 `Validate*Case*` | `casegen.Validate(payload, phase)` + 规则表 | ≈700 |
| `internal/agent` + `internal/harness` + `internal/agentservice` | `internal/agentruntime` | ≈300（重复的 Run/事件类型） |
| `internal/taskplan` + `internal/planning` | `internal/plan` | ≈400 |
| `internal/dsl` + `internal/cases` | `internal/casegen` | ≈250 |
| `internal/research` + `internal/observation` | `internal/reporting` | ≈200 |
| 三处"action 是否需要 target binding"判断 | schema 表 | ≈80 |
| 六处条件类型集合 | schema 表 | ≈120 |

**小计：≈ 2,800 行消失。**

## 3. 拆分（不减少总量，但降低复杂度）

见 04 §3 的表格（7 个 Go 千行文件 + 3 个 Python 千行文件）。拆分本身不减少行数，但配合 §1/§2 后，单个文件的目标是 ≤ 500 行。

## 4. 数据库侧删除

| 对象 | 动作 | 阶段 |
|---|---|---|
| `test_cases.dsl` | 改为 `artifact_id` 引用后删除 | P2 |
| `execution_jobs.dsl_snapshot` / `dsl_canonical_json` | 删除，改 `artifact_id` | P2 |
| `dsl_generation_runs.generated_case_json` | 拆为 `GenerationAttempt.draft`（审计）+ `CaseArtifact.payload` | P2 |
| `dsl_generation_runs` 中仅服务 v1 的列 | 删除 | P3 |
| `task_plan_steps` 中 `page_state` 相关列 | 删除 | P3 |

迁移原则：**先加列/加表 → 双写 → 切读 → 删旧**，每步一个迁移文件，每步可单独回滚。

## 5. 明确保留（不要为了"减少行数"而删）

| 对象 | 理由 |
|---|---|
| `docs/bug-log.md` / `docs/execution-log.md` | 是项目记忆，尤其 bug-log 记录了这些结构性问题的现场 |
| `research/acceptance/*.json` | 验收 oracle 是声明式契约，属于正确形态 |
| `research/fixtures/`、`testdata/` 中的 golden fixture | 契约一致性的基础，**只增不改** |
| `browser-worker/tests/test_explore_flow_chromium.py` | 真机浏览器回归，覆盖 CSS 候选回放等只能在浏览器里验证的行为 |
| 成本熔断（`AGENTSERVICE_MAX_*`） | 真实省钱机制，保留并补测试 |
| `internal/integration` | 跨上下文测试，重构后价值更高 |

## 6. 预期结果

| 指标 | 现状 | 删除+合并后 | 拆分+重命名后（目标） |
|---|---|---|---|
| Go 生产行数 | 29,458 | ≈ 24,000 | ≤ 18,000 |
| Python 行数 | 22,660 | ≈ 19,000 | ≤ 14,000 |
| 顶层业务包 | 20 | 20 | ≤ 10 |
| > 800 行文件 | 21 | 21 | 0 |
| case 契约定义点 | 14 | 1 | 1 |

## 7. 执行顺序建议

1. P0 先删临时产物与临时命令（零风险）。
2. P3 的 v1/legacy 删除**尽量早做**（在 P1 之后立即做）：它直接减少 P1/P4 需要处理的分支数量，越早删越省事。
3. P2 的数据库删除必须**最后**做（切读验证一个版本周期后）。
4. 拆分放在所有删除/合并之后，避免"拆完又删"的浪费。
