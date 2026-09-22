# 02 目标架构

## 1. 目标形态一句话

**一条链路、一份契约、一个权威形态。**

```
目标(NL) → Plan(版本化) → CaseArtifact(不可变、按内容哈希寻址) → 审批 → Execution → StepResult
```

所有跨进程数据都以 `CaseArtifact` 为中心，其余实体只持有它的引用或哈希。

## 2. 限界上下文（4 个，不是 20 个包）

当前 `backend-go/internal` 有 20 个包，其中大量包只是"一个实体一个包"的机械切分（`projects`、`cases`、`corrections`、`observation`、`planning`…），导致跨包调用关系混乱、依赖方向不可控。

目标收敛为 4 个上下文 + 3 个共享内核包：

| 上下文 | 职责 | 允许依赖 |
|---|---|---|
| **planning**（规划） | AgentRun、对话、TaskPlan 版本与接地状态机 | contract, store, platform |
| **casegen**（用例生成） | 草稿校验、编译器、CaseArtifact 持久化、审批绑定 | contract, store, planning（只读） |
| **execution**（执行） | 队列、worker 领取、浏览器 RPC、结果与证据落库 | contract, store, platform |
| **reporting**（报告） | 报告聚合、失败信号、验收 oracle、研究导出 | contract, store |

共享内核：

| 包 | 职责 |
|---|---|
| `contract` | **全部**跨进程数据契约的唯一 Go 定义（由 schema 生成） |
| `store` | PostgreSQL 访问（按上下文分子目录，不按实体分顶层包） |
| `platform` | LLM、HTTP、日志、配置等基础设施 |

**依赖规则（必须由 CI 检查）**：

- `contract` 不依赖任何业务包。
- `planning` / `casegen` / `execution` / `reporting` 之间只允许通过 `contract` 里的类型和显式接口交互，禁止互相 import 内部实现。
- 任何业务包不得直接拼 SQL；一律经 `store`。

## 3. 模块边界与"单一定义点"

| 概念 | 当前定义点数量 | 目标定义点 |
|---|---|---|
| case 的合法形态 | Go 11 个 `Validate*Case*` + Python 3 套契约 | **1**：`contract/case.schema.json` + 生成的 Go/Python 类型 + **1** 个校验器 |
| "哪些 action 需要 target binding" | 3 处（Go 接地、Python 观测、提示词） | **1**：schema 的 `action → requires[]` 表，生成到 Go/Python/提示词 |
| 条件类型 → 合法阶段 | 6 处（见 01 §2.3） | **1**：schema 的 `condition → phases[]` 表 |
| profile 数量 | 3（legacy-v1 / research-v1 / research-v2） | **1**（`case/v3`） |
| case 的权威数据来源 | 3（canonical JSON / snapshot / test_cases.dsl） | **1**：`case_artifacts.payload`，按 `content_hash` 寻址 |
| 计划条件 → DSL 条件 | 4 个校验函数 | **1**：`casegen/conditions.go`，草稿与可执行共用同一实现，按阶段取规则表 |

## 4. 阶段模型（显式化）

引入一个统一的阶段枚举，所有校验与运行时分派都以它为准：

```go
type Phase string

const (
    PhaseDraft      Phase = "draft"       // 模型产出，容忍旧拼写
    PhaseExecutable Phase = "executable"  // 编译器产物，契约严格
)

type ConditionPhase string

const (
    ConditionPre  ConditionPhase = "pre"   // 只能是状态事实
    ConditionPost ConditionPhase = "post"  // 可以是状态或变化/事件
)
```

规则表由 schema 生成，形如：

| condition type | pre | post |
|---|---|---|
| `url_contains` | ✅ | ✅ |
| `text_visible` / `text_gone` | ✅ | ✅ |
| `url_changes` / `dom_changed` / `value_changed` | ❌ | ✅ |
| `network_request` | ❌ | ✅ |
| `element_visible` / `element_gone` | ❌ | ❌（未绑定选择器，一律禁止） |

**校验器只读这张表**，不再在各处写 `if type == ...`。Python 侧只做形状校验（由生成模型承担），不再实现业务规则。

## 5. 实体收敛

当前实体链有 7 个实体、字段大量重复：

```
AgentRun → TaskPlan(多版本) → DSLGeneration → TestCase → ExecutionBatch → ExecutionJob → TestCaseRun
```

目标：

```
Run ─┬─ Plan(version, 不可变)
     └─ CaseArtifact(content_hash, 不可变, 含 evidence)
            └─ Approval(谁在何时批准了哪个 hash)
                   └─ Execution(batch) → Job → StepResult
```

具体收敛动作：

1. **删除 `DSLGeneration` 与 `TestCase` 的重复**：生成器直接产出 `CaseArtifact`；用例库里的"用例"就是指向某个 `CaseArtifact` 的引用（可编辑时产生新 hash 的新工件）。前端用例编辑器写的就是工件本身，不再有 body 形态。
2. **删除 `test_cases.dsl`**：改为 `test_cases.artifact_id`。
3. **删除执行队列的 `COALESCE` 回退链**：job 只持有 `artifact_id`，执行时按 id 取不可变 payload。
4. **审批与绑定合并**：`Approval(artifact_id, plan_id, plan_version, approved_by, approved_at)`，取代 `plan_binding` / `observation_bindings` 在 payload 内部自描述的做法（payload 内部仍保留 lineage，但审批是独立记录）。
5. **证据统一**：step 上的 `probe_id` / `observation_id` / `observation_sha256` / `page_state_id` / `selected_candidate_id` / `locator_candidates` 全部收进一个 `evidence` 对象，只保留一处。

目标 step 形态（示意）：

```json
{
  "plan_step_id": "search_submit",
  "action": "click",
  "intent": "提交搜索",
  "value": null,
  "idempotency": "idempotent",
  "side_effect": "browser_state",
  "evidence": {
    "observation_id": "obs_...",
    "observation_sha256": "...",
    "page_state_id": "S1",
    "candidates": [ ... ],
    "selected_candidate_id": "cand_..."
  },
  "preconditions": [{ "type": "url_contains", "value": "/products" }],
  "postconditions": [{ "type": "url_contains", "value": "/products?search=" }]
}
```

## 6. Python worker 目标形态

当前 9 个模块、4 个契约家族、3 个巨型文件。目标：

```
browser_worker/
  contracts/        由 schema 生成，只做形状校验
    case.py         (取代 dsl.py + action_ir.py + action_ir_v2.py)
    observation.py
    execution.py
    capability.py   (取代 browser_capabilities.py + browser_executions.py)
  exploration/      页面探索 + 观测采集（page_explorer.py 拆分）
    explorer.py     导航与探测编排
    collector.py    A11y/DOM 采集
    a11y_name.py    可访问名计算的唯一实现（BUG-211 的根治点）
  locators/         定位器解析、候选评分、纠正协议
  runners/          DSL 执行（playwright_runner.py 拆分）
    runner.py       步骤循环与分派
    conditions.py   条件评估（唯一实现，阶段感知）
    actions.py      单个 action 的实现
  reporting/        报告与验收 oracle
  server/           FastAPI 入口与路由
```

关键约束：

- `contracts/` 只做形状，**不含业务规则**；业务规则在 Go 一处定义。
- **可访问名只有一处计算实现**（`exploration/a11y_name.py`），DOM 文本与 A11y 树的分歧必须在那一处解决（BUG-211）。
- 条件评估只有一处（`runners/conditions.py`），且必须显式接收 `phase`。

## 7. 规模目标

| 指标 | 现状 | 目标 |
|---|---|---|
| Go 生产代码 | 29,458 行 | ≤ 18,000 行 |
| Python 代码 | 22,660 行 | ≤ 14,000 行 |
| 超过 800 行的文件 | 21 个（其中 >1000 行 17 个） | 0 个 |
| case 契约定义点 | 14（Go 11 + Python 3 套家族） | 1 |
| profile 数 | 3 | 1 |
| 顶层业务包 | 20 | 4 + 3 |

行数不是目的，但它是最难伪造的指标：**当前 7.8 万行里，真正承载产品价值的只是那条单链路。**

## 8. 明确不做的事

- 不引入事件总线、不引入 CQRS/ES。
- 不把 Go 控制面拆成微服务。
- 不引入 ORM；继续用显式 SQL（但要收进 `store`）。
- 不为"将来可能的多用户/多租户"预留抽象（现在没有鉴权，就不要假装修建好了）。
- 不保留 research-v1 的兼容读取路径（该 profile 无实际数据价值，见 07）。
