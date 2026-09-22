# 03 数据契约重做

本文只解决一件事：**让每份跨进程数据只有一个定义点，并且不可能悄悄漂移。**

## 1. 现状清单（要合并的对象）

| 概念 | 现状定义点 | 文件 |
|---|---|---|
| case（v1） | `DSLCase` / `validate_dsl_case` | `browser-worker/src/browser_worker/contracts/dsl.py` |
| case（research-v1） | `ResearchDSLCase` / `validate_research_dsl` | `.../contracts/action_ir.py` |
| case（research-v2） | `ResearchV2Case` / `validate_research_v2_dsl` | `.../contracts/action_ir_v2.py` |
| case（Go legacy） | `validateLegacyCase` | `backend-go/internal/dsl/store.go:256` |
| case（Go research） | `validateResearchCase` | `backend-go/internal/dsl/action_ir.go:150` |
| case（Go v2） | `validateResearchV2Case` | `backend-go/internal/dsl/research_v2.go:14` |
| 条件类型集合 | 6 处 | 见 01 §2.3 |
| 观测 v1 / v2 | `contracts/browser-observation.v2.schema.json` + Pydantic + Go struct | 多处 |
| 执行结果 | `executions.py` + `browser_executions.py` + Go `execution` 包 | 多处 |

## 2. 目标：schema 为源，代码为生成物

```
contracts/
  case.v3.schema.json          ← 唯一权威（case、step、condition、action 表）
  observation.v3.schema.json
  execution.v3.schema.json
  capability.v3.schema.json
  generated/
    go/       (由 schema 生成 Go 类型 + 常量表)
    python/   (由 schema 生成 Pydantic 模型 + 常量表)
    prompt/   (由 schema 生成提示词里的"合法取值/阶段规则"片段)
```

### 2.1 为什么连提示词也要生成

BUG 的直接成因之一就是提示词手写了不存在的 `value_equals`。提示词里凡是列举"合法 action / 合法条件类型 / 哪个字段必填"的段落，都必须来自 schema，而不是人手维护。

生成方式：schema 里为每个枚举值带一段 `description`（中文/英文各一），生成器把它们拼成提示词片段。人只写业务叙述，不写取值清单。

### 2.2 生成器选型

- Go：`quicktype` 或自写 ~300 行生成器（本仓库已有 `contracts/` 目录，建议自写，避免引入重型工具链）。
- Python：`datamodel-code-generator`（成熟、可锁定版本），或与 Go 共用同一份自写生成器。
- **必须纳入 CI**：`make contracts` 后 `git diff --exit-code`，防止有人手改生成物。

## 3. 一个校验器

### 3.1 分层

```
contract/case            ← 形状（由生成类型保证）+ 取值表（生成常量）
casegen/validate         ← 业务规则（唯一实现）
   validate(artifact, phase) error
```

规则按阶段分派，规则表来自 schema：

```go
// 由 schema 生成，禁止手写
var conditionPhases = map[string]ConditionPhaseSet{
    "url_contains":  {Pre: true, Post: true},
    "text_visible":  {Pre: true, Post: true},
    "text_gone":     {Pre: true, Post: true},
    "url_changes":   {Pre: false, Post: true},
    "dom_changed":   {Pre: false, Post: true},
    "value_changed": {Pre: false, Post: true},
    "network_request": {Pre: false, Post: true},
}
```

### 3.2 校验器唯一入口

```go
func Validate(payload json.RawMessage, phase Phase) (Canonical, error)
```

- `Validate(..., PhaseDraft)`：容忍旧拼写（`page_state`、`target`），但不容忍取值非法。
- `Validate(..., PhaseExecutable)`：严格，编译器产物必须过。
- **计划条件保留规则只实现一次**，草稿与可执行共用，差异只来自阶段参数。

删除：`validateConditionPreservation` / `validateCompiledConditions` 的双份实现、`validateCaseSemantics` / `validateCompiledCaseSemantics` 的双份实现、`validatePersistedCaseBindings` / `validatePersistedCaseMatchesBinding`（由"单一权威形态"消灭，见 §4）。

### 3.3 Python 侧只做形状

`validate_research_v2_dsl` 之类的**业务规则**校验全部删除，Python 只：

1. 用生成的 Pydantic 模型校验形状；
2. 在契约一致性测试里，对同一批 fixture 与 Go 校验器比对"接受/拒绝"结论。

## 4. 一个权威形态

### 4.1 消灭 body / 工件 / COALESCE 三重身

现状：

```
DSLGeneration.generated_case_json   （工件）
test_cases.dsl                      （body，丢绑定）
execution_jobs.dsl_snapshot         （快照）
execution_jobs.dsl_canonical_json   （canonical）
读取时：COALESCE(canonical, snapshot, test_cases.dsl)
```

目标：

```
case_artifacts(id, content_hash, payload, created_at)   -- 不可变，按 hash 寻址
test_cases(id, project_id, artifact_id, name)           -- 只是引用
execution_jobs(id, artifact_id, ...)                    -- 只是引用
```

- 生成器写 `case_artifacts`。
- 前端用例编辑器保存时走**同一个编译器**，产出新工件（新 hash）。
- 队列按 `artifact_id` 取 payload，**没有回退链**。
- 因此"落库形态 ≠ 工件形态"这一整类 bug（BUG-212）从结构上不可能发生。

### 4.2 迁移兼容

- 新增 `case_artifacts`，双写一个版本周期：`test_cases.dsl` 仍写，同时写工件。
- 读取路径切换为只读工件；`COALESCE` 链删除。
- 一个版本后删除 `test_cases.dsl` 与 `dsl_snapshot` / `dsl_canonical_json`。

## 5. 条件契约（BUG-213 的正式解法）

### 5.1 阶段规则（已实测确认）

| condition type | pre | post | 说明 |
|---|---|---|---|
| `url_contains` | ✅ | ✅ | 状态事实 |
| `text_visible` / `text_gone` | ✅ | ✅ | 状态事实 |
| `url_changes` / `dom_changed` / `value_changed` | ❌ | ✅ | 变化事实，动作前无真值 |
| `network_request` | ❌ | ✅ | 事件事实，动作前无事件 |
| `element_visible` / `element_gone` | ❌ | ❌ | 未绑定选择器，禁止 |

### 5.2 两个必须同时成立的约束

1. `click` / `input` 必须至少有 1 个前置条件和 1 个后置条件（现有契约，保留）。
2. 前置条件只能是状态事实（新增契约）。

两条合起来是**可满足的**，因为 `url_contains` / `text_visible` 永远可用。但提示词必须明确告诉模型：

> 当计划的前置条件是一句无法用状态事实表达的话（例如"搜索框已输入 Blue Top"）时，改断言最接近的可检查状态事实（例如该步骤所在页面的 URL），而不是使用变化型条件。

（该段提示词已在本次修复中写入，重构时应改为由 schema 生成。）

### 5.3 首个 `goto` 步骤

runner 在首步是 `goto` 时不会预导航到 `base_url`，因此首步执行前页面是 `about:blank`。结论：

- 首步 `goto` 的**前置条件在结构上不可满足**；
- 编译器负责清空它，并记录一条 normalization note；
- 计划层的前置条件作为**任务级前提**保留在计划里，不下沉为运行时可检查条件。

这条规则必须写进 schema 的注释与提示词，而不是只活在编译器代码里。

### 5.4 计划条件（NL）→ DSL 条件的翻译

现状用正则解析自然语言（`parseConditionIntent`），只能识别 `url contains X` / `X visible` / `url changes`，其余退化成 `free_text`，再靠"至少有一个"的粗规则兜底。而粗规则与 §5.1 冲突。

目标：

- **计划条件改为结构化**：`preconditions: [{type, value}]`，与 DSL 同一套类型。自然语言只作为 `intent` 描述，不参与校验。
- 这样"计划条件必须被 DSL 保留"可以变成**确定性比对**，删除全部正则解析与 `free_text` 兜底。
- 迁移期：`parseConditionIntent` 保留一个版本用于读取历史计划，新计划只写结构化条件。

这是本次重构中**收益最大的一处**：它一次性消灭了 4 个校验函数、1 套正则、1 条自相矛盾的粗规则。

## 6. 命名与版本

| 现状 | 目标 |
|---|---|
| `legacy-v1` / `research-v1` / `research-v2` | `case/v3`（唯一） |
| `dsl.canonical.v3` | `case.v3` |
| `plan_binding` / `observation_bindings`（payload 内） | 独立 `Approval` 记录 + step 内 `evidence` |
| `dsl_sha256` | `content_hash` |
| `generated_case_json` | `payload` |

版本策略：

- 契约文件版本化（`case.v3.schema.json`），**只增不改**；
- 破坏性变更必须新增版本文件 + 迁移脚本，禁止原地改字段语义；
- 每个版本必须有 golden fixture，且**历史 fixture 永久保留在测试里**（防止"改契约顺手改 fixture"）。

## 7. 验收标准（契约部分）

1. `contracts/` 是唯一手写契约来源；`generated/` 由 CI 校验不可手改。
2. 全仓库只有 **1** 个 `Validate(payload, phase)` 入口；`grep -r "func Validate" backend-go` 结果可人工审阅完毕。
3. 跨语言一致性测试：同一批 fixture 在 Go 与 Python 校验器上给出**相同**的接受/拒绝结论与相同错误码。
4. 提示词中所有取值清单均由 schema 生成；`grep "value_equals"` 全仓库为空。
5. 执行队列不再出现 `COALESCE(... dsl ...)`。
6. 新增一个条件类型只需改 1 个 schema 文件 + 1 处运行时实现。
