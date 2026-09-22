# 01 诊断：三个结构性根因

本文用一次 Agentic Research live E2E 打通过程中的 6 个真实缺陷，反推当前代码的结构性问题。所有结论都有文件/函数级证据。

## 1. 缺陷复盘（BUG-207 ~ BUG-213）

| 缺陷 | 现象 | 直接原因 |
|---|---|---|
| BUG-207 | `assert_text` 接地后没有 `TargetBinding`，执行期退化 | 三条实现（Go 接地、Python 观测、提示词）对"哪些 action 需要绑定"各有一份判断 |
| BUG-208 | 草稿带 `page_state` 字段被拒，白烧一轮模型 | v1 拼写与 v2 字段共存，草稿/可执行两个阶段的容忍度不同 |
| BUG-209 | 已提交代码 `gofmt` 不合规 | 门禁不完整 |
| BUG-210 | `DROP DATABASE ... WITH (FORCE)` 在 Windows 挂死 | 测试基建 |
| BUG-211 | 观测里"文本推导的可访问名"与浏览器计算的可访问名不一致（Font Awesome 私有区字形） | 同一"可访问名"概念在 DOM 文本与 A11y 树之间没有单一权威定义 |
| BUG-212 | 审批通过后 `execute_dsl` **必然**失败：`test_cases.dsl` 只存 case body，却被当 executable 校验 | 同一对象两副身子（工件 vs 落库） |
| BUG-213 | 首步 `goto` 的前置条件永远判不过；`click` 的"变化型"前置条件永远判不过 | 条件没有阶段模型，且"必须至少一个前置条件"的契约把作者逼进死角 |

**6 个缺陷里有 5 个的根因不是"写错了某一行"，而是"同一件事被多处定义"。**

## 2. 根因一：数据契约被重复实现，且没有任何机制保证一致

### 2.1 Go 侧：同一个"这个 case 能不能跑"的问题，有 11 个函数在回答

> 括号内行号取自 2026-09-21 的工作区快照；重构过程中行号会变，函数名是稳定标识。

```
internal/dsl/action_ir.go
  ValidateDraftCase            (55)
  ValidateExecutableCase       (59)
  ValidateCase                 (63)
  ValidateCaseForVersion       (71)
  validateCase                 (93)
  validateResearchCase         (150)
internal/dsl/research_v2.go
  validateResearchV2Case       (14)
internal/dsl/store.go
  validateLegacyCase           (256)
internal/taskplan/service.go
  validateCaseSemantics        (915)
internal/taskplan/compiler.go
  validateDraftCaseSemantics   (128)
  validateCompiledCaseSemantics(274)
internal/taskplan/condition.go
  validateConditionPreservation(136)
  validateCompiledConditions   (compiler.go:374)
internal/execution/store.go
  validatePersistedCaseBindings(279)
  validatePersistedCaseMatchesBinding(338)
```

其中至少 4 个函数各自实现了一遍"计划条件必须被 DSL 保留"的规则（`validateConditionPreservation`、`validateCompiledConditions`、`validateCaseSemantics`、`validateCompiledCaseSemantics`），且**它们的严格程度互不相同**：BUG-213 里正是"草稿层校验通过、可执行层校验拒绝"的组合把编译产物卡死。

### 2.2 Python 侧：同一个 case 概念有 3 套契约家族

```
contracts/dsl.py            ConditionSpec, GotoStep…CaptureTextStep, DSLCase, validate_dsl_case      (legacy v1)
contracts/action_ir.py      ResearchConditionSpec, ResearchGotoStep…ResearchCaptureTextStep,
                            ResearchDSLCase, validate_research_dsl                                    (research-v1)
contracts/action_ir_v2.py   ResearchV2Step, ResearchV2Case, validate_research_v2_dsl                  (research-v2)
```

再加上 `contracts/executions.py`、`contracts/browser_executions.py` 里各自的执行契约，Python 侧 9 个 contract 模块里有 4 个在描述"case 长什么样"。

### 2.3 后果：跨语言规则漂移（实测）

- `backend-go/internal/harness/harness.go:68` 的探索提示词让模型使用
  `{"type":"value_equals","expected":"..."}`。
- 但 `value_equals` 在 Go 的 `postconditionTypes`、Python 的 `ConditionSpec` 允许集合、`ResearchV2Step` 里**都不存在**；research-v2 步骤甚至不允许 `condition` 字段。
- 模型一旦照做，草稿被拒，白烧一轮；这类漂移没有任何测试能发现，因为**没有一份"契约一致性"测试**。

同一个条件类型的合法取值集合，目前同时存在于：

| 位置 | 形态 |
|---|---|
| `internal/dsl/action_ir.go` `postconditionTypes` | Go `stringSet` |
| `internal/dsl/research_v2.go` `preconditionOnlyStateFacts`（本次新增） | Go `stringSet` |
| `browser_worker/contracts/dsl.py:67` | Python 字面量集合 |
| `browser_worker/contracts/action_ir_v2.py` `PRECONDITION_STATE_FACTS`（本次新增） | Python frozenset |
| `browser_worker/runners/postcondition_verifier.py` `_verify_single` | Python `if/elif` 分支 |
| `internal/harness/harness.go` 提示词 | 自然语言 |

**6 份定义，0 个生成器，0 个一致性测试。**

## 3. 根因二：同一业务对象有"工件形态"和"落库形态"两副身子

BUG-212 的完整链条：

```
DSLGeneration.generated_case_json   ← 编译器产物（含 plan_binding / observation_bindings）
        │  caseMutation() 只取 case body
        ▼
test_cases.dsl                      ← 丢掉编译器绑定（cases/postgres.go encodeDSL）
        │  CreateBatch 校验：ValidateExecutableCase(test_cases.dsl)
        ▼
"case 59 persisted DSL is not executable"  ← research-v2 要求 plan_binding，必然失败
```

而执行队列读 DSL 时又是另一套逻辑：

```sql
COALESCE(j.dsl_canonical_json, j.dsl_snapshot::text, tc.dsl::text)   -- internal/execution/worker_store.go
```

即：**同一个 case 在系统里有 3 个可能的数据来源，且"哪个才是权威"取决于一个 SQL COALESCE。** 前端用例编辑器写的是 body 形态，队列执行的是 canonical 形态，校验函数一会儿看这个一会儿看那个。这是"数据契约搞不定"的最直接证据。

同类问题还有字段谱系的三重表达：

| 概念 | 表达 1 | 表达 2 | 表达 3 |
|---|---|---|---|
| 页面状态 | `page_state`（v1 拼写） | `page_state_id`（编译器自有） | `observation_bindings[].page_state_id` |
| 语义目标 | `target`（计划字段） | `semantic_target`（可执行字段） | `TargetBinding.SemanticTarget` |
| 候选定位器 | `candidates`（legacy） | `locator_candidates`（v2） | `TargetBinding.Candidates` |
| 观测谱系 | step 上的 `probe_id/observation_id/...` | `observation_bindings[]` | `TargetBinding` 内字段 |

## 4. 根因三：阶段语义从未被建模

系统里至少有 5 组"阶段"概念，全部靠散落的 if 分支表达：

| 阶段对 | 目前表达方式 | 后果 |
|---|---|---|
| 草稿 vs 可执行 | `ValidationPhase` + 每个校验函数内部 `if phase == ...` | 容忍度不一致（BUG-208） |
| 前置条件 vs 后置条件 | 两个字段 + runner 里 `phase=` 字符串 | 契约允许"变化型"条件作前置条件，运行时永远判不过（BUG-213） |
| 计划条件（NL）vs DSL 条件（结构化） | `parseConditionIntent` 正则解析自然语言 | 自由文本条件无法确定性校验，只能退化成"至少有一个"的粗规则，而这条粗规则又和上一条冲突 |
| 页面状态 vs 观测谱系 | 见上表 | 字段重复 |
| profile（legacy-v1 / research-v1 / research-v2） | 3 套并行实现 + 大量 `if profile ==` | research-v1 已无实际使用价值，却仍占约 50 处引用 |

### 4.1 BUG-213 的完整推理（说明"阶段模型缺失"如何制造死局）

1. DSL 契约规定：`click` / `input` **必须**至少有 1 个前置条件和 1 个后置条件（`dsl/research_v2.go:474`、`contracts/action_ir_v2.py:121`）。
2. runner 的前置条件阶段：先 `capture_pre_state()`，**紧接着** `verify(preconditions)`（`runners/playwright_runner.py:551`），且此时没有任何网络事件。
3. 因此 `value_changed` / `dom_changed` / `url_changes` / `network_request` 作为前置条件**在结构上永远为假**。
4. 计划里的前置条件是一句自然语言（"搜索框中已输入 Blue Top"），DSL 里没有"元素值等于 X"这种**状态型**条件类型。
5. 模型被规则 1 逼着写一个前置条件，只能用最接近的 `value_changed` → 必然失败。

结论：**规则 1（契约）与规则 3（运行时）互相矛盾，而矛盾点没有任何一处代码或文档承认它的存在。** 本次只在 Go/Python 契约里加了阶段规则（前置条件只能是状态事实），但这只是把矛盾显式化，并没有建立阶段模型本身。

## 5. 附带问题：测试无法发现上述问题

| 问题 | 证据 |
|---|---|
| 测试可以绕过契约 | `internal/taskplan/agentic_chain_offline_test.go` 直接调用 `CompileDraftCase`，**从不调用** `ValidateExecutableCase`，因此它编译出的"click 无前置条件"的产物一路绿灯 |
| 测试可以伪造形态 | `internal/execution/store_postgres_test.go` 把 canonical JSON 当"落库 DSL"传入，因此 BUG-212 存在数月未被发现 |
| 无跨语言一致性测试 | 见 §2.3 |
| 门禁不完整 | BUG-209（已提交代码 gofmt 不合规） |
| 巨型测试文件 | `taskplan/service_test.go` 2602 行、`agent/tool_result_test.go` 1043 行、`harness/harness_test.go` 1450 行——测试与实现同构地膨胀，说明被测单元本身过大 |

## 6. 诊断结论

要修的不是 6 个 bug，而是：

1. **让每个契约只有一个定义点**，并用生成/一致性测试保证 Go、Python、提示词三方不漂移。
2. **让每个业务对象只有一个权威形态**，删掉 body/工件/COALESCE 的三重身。
3. **把阶段显式建模**：`draft|executable`、`pre|post`、`plan|dsl`、`observed|bound`，每个阶段一张表，一处定义。
4. **删掉不再有产品价值的并行实现**（legacy-v1、research-v1），并拆开超过 1,000 行的单体文件。
5. **让测试只能走真实契约路径**，把"绕过契约"从测试风格问题升级为门禁失败。
