# 重构计划（2026-09-21）

本目录是 AI Web Testing 平台的结构性重构计划。它不是"重写愿望清单"，而是基于**一次完整实战失败复盘**得出的、可分批执行、每批都能保持系统可运行的迁移方案。

## 为什么现在重构

一次 Agentic Research live E2E 的打通过程，连续暴露了 6 个缺陷（BUG-207 ~ BUG-213）。逐个修完之后可以看到：**它们不是 6 个独立 bug，而是同 3 个结构性问题在不同层的投影**。

| 结构性问题 | 表现 |
|---|---|
| **同一份数据契约被 4~6 处重复实现**（Go 11 个 `Validate*Case*`、Python 3 套 case 契约家族） | 规则漂移：提示词让模型用不存在的 `value_equals`；契约允许前置条件写"变化型"条件，runner 却永远判不过 |
| **同一个业务对象有"工件形态"和"落库形态"两副身子** | `test_cases.dsl` 故意丢掉编译器绑定，但校验又把它当 executable 校验 → `execute_dsl` 必然失败（BUG-212） |
| **条件/字段的"阶段语义"从未被建模** | 前置条件 vs 后置条件、草稿 vs 可执行、页面状态 vs 观测谱系，全靠各处 if 分支各自理解 |

如果不做结构收敛，下一轮还会以同样的方式再坏一次。

## 现状体量（实测）

| 部分 | 文件数 | 行数 |
|---|---|---|
| `backend-go/` 生产代码 | 74 | 29,458 |
| `backend-go/` 测试代码 | 50 | 20,820 |
| `browser-worker/` | 71 | 22,660 |
| `frontend/src` | 41 | 5,434 |
| **合计** | **236** | **≈ 78,400** |

单体文件（超过 1,000 行的，共 17 个；超过 800 行的共 21 个）：

- Go：`agent/tool_result.go` 2014、`taskplan/service.go` 1607、`research/projector.go` 1488、`harness/harness.go` 1375、`research/source.go` 1231、`research/postgres.go` 1100、`execution/store.go` 1000
- Python：`scripts/research_e2e.py` 2759、`exploration/page_explorer.py` 1880、`runners/playwright_runner.py` 1681、`scripts/run_agentic_e2e.py` 1361

一个"单机、单用户、无鉴权"的平台，核心链路只有 `目标 → 计划 → DSL → 审批 → 执行 → 报告`，7.8 万行里相当一部分是**同一概念的多套实现**。

## 目录

| 文档 | 内容 |
|---|---|
| [01-diagnosis.md](01-diagnosis.md) | 诊断：用本次 6 个缺陷的复盘，定位 3 个结构性根因（含文件与函数级证据） |
| [02-target-architecture.md](02-target-architecture.md) | 目标架构：限界上下文、模块边界、依赖方向、单一定义点 |
| [03-contract-redesign.md](03-contract-redesign.md) | 数据契约重做：一份 schema、一个校验器、工件与落库形态统一、条件阶段模型 |
| [04-naming-and-boundaries.md](04-naming-and-boundaries.md) | 命名与分类：重命名对照表、包/模块重新划分、目录树目标形态 |
| [05-migration-phases.md](05-migration-phases.md) | 分阶段迁移（strangler）：每阶段目标、步骤、验收门禁、回滚 |
| [06-test-strategy.md](06-test-strategy.md) | 测试策略：能提前抓住这 6 个缺陷的测试形态，以及"禁止绕过契约"的硬规则 |
| [07-deletion-list.md](07-deletion-list.md) | 删除与合并清单：可减掉的代码（含行数估算）与风险 |
| [08-open-decisions.md](08-open-decisions.md) | 需要项目所有者拍板的决策点 |
| [09-experiment-log.md](09-experiment-log.md) | 实验记录：真实站点闭环的实测证据、暴露的契约能力边界、成本数据（含新增的 D9~D12） |

## 使用方式

1. 先读 `01` 确认诊断是否认同；不认同的部分直接在对应文档上改。
2. 读 `08`，把需要拍板的决策定下来（这些决定了 `02`/`03` 的最终形态）。
3. 按 `05` 的阶段推进；**每个阶段结束都必须满足该阶段的验收门禁**，否则不进入下一阶段。
4. `07` 的删除动作分散在各阶段内执行，不要集中到最后一次性删。

## 非目标

- 不引入新框架、不引入消息队列/微服务、不做分布式改造。
- 不改产品范围（仍然只做"结构化 DSL + 确定性执行 + 结构化报告"）。
- 不在重构期新增 profile、新增条件类型、新增工具。
- 不追求一次性达到目标形态：允许中间态存在，但不允许中间态破坏"每阶段可运行"。

## 当前工作区状态（重要）

本次会话为打通 E2E 已经改动了若干文件，**这些改动尚未提交**，重构计划必须把它们当作既有事实：

- 已改：`backend-go/internal/{dsl,taskplan,execution,harness,agentservice,planning,integration}`、`browser-worker/src/browser_worker/{exploration,runners,contracts}`、`browser-worker/tests`、`cmd/migrate`、`docs/bug-log.md`
- 临时文件（计划外，需要清理）：`backend-go/cmd/tmp-execute-generation/`、`_mk_draft50.py`、`_draft50.json`、`_gen50.json`、`_batch107_report.json`、`_flow*.json`、`_page_products.json`、`_ev*.json`、`_plan_dump.json`、`_steps_dump.json`、`_smoke_dsl.json`、`_probe_real.py`、`_real_probe.json`、`_migrate_dump.txt`、`_case59_dsl.json`

建议：**先把"打通 E2E 的最小修复集"整理成一次提交并冻结**，再开始阶段 0，避免重构与在途修复互相污染。
