# v2 架构演进计划（2026-09-21，2026-09-24 调整）

本目录是 AI Web Testing 平台的结构性演进计划。最初目标是收敛 v1 的契约与执行链路；
2026-09-24 起，v2 已作为独立单闭环实现存在，本计划调整为：

**在保留 v2 单闭环的前提下，把它演进成通用 Web E2E 能力平台。**

## 为什么现在重构

一次 Agentic Research live E2E 的打通过程，连续暴露了 6 个缺陷（BUG-207 ~ BUG-213）。逐个修完之后可以看到：**它们不是 6 个独立 bug，而是同 3 个结构性问题在不同层的投影**。

| 结构性问题 | 表现 |
|---|---|
| **同一份数据契约被 4~6 处重复实现**（Go 11 个 `Validate*Case*`、Python 3 套 case 契约家族） | 规则漂移：提示词让模型用不存在的 `value_equals`；契约允许前置条件写"变化型"条件，runner 却永远判不过 |
| **同一个业务对象有"工件形态"和"落库形态"两副身子** | `test_cases.dsl` 故意丢掉编译器绑定，但校验又把它当 executable 校验 → `execute_dsl` 必然失败（BUG-212） |
| **条件/字段的"阶段语义"从未被建模** | 前置条件 vs 后置条件、草稿 vs 可执行、页面状态 vs 观测谱系，全靠各处 if 分支各自理解 |

如果不做结构收敛，下一轮还会以同样的方式再坏一次。

结构收敛只是第一步。真实站点实验又暴露出另一类问题：纯图标按钮、重复卡片、广告插屏、
观测截断、token 成本。这些不是电商专属问题，而是普通 Web 页面自动化都会遇到的通用能力缺口。
因此后续路线从“迁移旧实现”调整为：

```
契约底座 → 页面世界模型 → 通用定位语义 → 通用动作代数 → 规划修复循环 → 干扰恢复 → 跨场景基准
```

## 历史诊断体量（v1 复盘）

下表保留的是最初复盘旧实现时的体量数据，用来解释为什么不能继续在 v1 形态上叠功能。
当前 v2 已经是新的独立仓库结构，实际目录为 `backend/`、`worker/`、`web/`。

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
| [02-target-architecture.md](02-target-architecture.md) | 目标架构：通用 Web E2E 分层、页面世界模型、通用定位语义与动作代数 |
| [03-contract-redesign.md](03-contract-redesign.md) | 数据契约重做：一份 schema、一个校验器、工件与落库形态统一、条件阶段模型 |
| [04-naming-and-boundaries.md](04-naming-and-boundaries.md) | 命名与分类：重命名对照表、包/模块重新划分、目录树目标形态 |
| [05-migration-phases.md](05-migration-phases.md) | 分阶段演进：P0 基线冻结、Observation v2、TargetSpec v2、Action/Condition v2、Planner v2、Blocker、跨场景基准 |
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
- 不做行业专用 DSL；电商、SaaS、后台、内容站都必须使用同一套 Web 能力。
- 不让模型手写 CSS/XPath 作为常规路径。
- 不把真实站点 canary 作为本地必过门禁；本地必过门禁必须依赖离线 fixture。
- 不追求一次性达到目标形态：允许中间态存在，但不允许中间态破坏"每阶段可运行"。

## 当前 v2 工作区状态（重要）

当前 v2 是独立单闭环实现，代码目录为：

- `backend/`：Go 控制面、规划器、契约、报告、回灌、API、SQLite store。
- `worker/`：Python Playwright 执行器、观测、定位、动作、条件、runner。
- `web/`：React 前端四个页面。
- `fixtures/`：契约 fixture、脚本模型、静态测试站点。

P0 的第一件事是冻结当前 v2 基线：除规划文档外，工作区不应有非预期改动；离线门禁
`python run_tests.py` 必须作为后续阶段的共同回归入口。
