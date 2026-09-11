# DSL Canonicalization Contract

当前合同版本：

- `dsl.canonical.v1`：legacy-v1。
- `dsl.canonical.v2`：research-v1，只读兼容。
- `dsl.canonical.v3`：research-v2，由 TargetBinding 编译生成。

所有新 Agent generation 默认使用 `research-v2` / `dsl.canonical.v3`。

## Canonical JSON

`dsl.ValidateCase` 必须先完成校验，再生成仅包含 `DSLCase` 语义字段的完整对象：

- 顶层始终包含 `name`、`description`、`base_url`、`input_contract`、`output_contract` 和 `steps`。
- Pydantic 中有默认值或 `default_factory` 的字段必须显式物化，包括 `null`、空数组、`required=true`、`wait_for.timeout_ms=5000` 和 `postcondition.timeout_ms=3000`。
- `_preflight`、`match_count` 和其他非 DSL 字段必须移除。
- schema 声明的字符串按 `str_strip_whitespace` 处理；candidate strategy 使用与 Worker 相同的别名归一化。
- Go `encoding/json.Marshal` 输出的 UTF-8 字节是该版本唯一的 `canonical_json`。不得重新排版后继续沿用原 SHA。

`research-v2` 分为两个边界：

1. LLM 只提交 Draft DSL，包括 `plan_step_id` 和必要的
   `target_binding_id`，不得提交 selector、candidate 或 locator confidence。
2. Go 使用当前 TaskPlan 和持久化 TargetBinding 注入
   `semantic_target`、`locator_candidates`、`plan_binding`、
   `observation_bindings`，以及 probe/observation/page-state/selected-candidate
   lineage，再生成 Executable DSL canonical bytes。

TargetBinding 中的 locator 使用结构化 `LocatorSpec`，不允许
`role="..." inside ...` 等自定义字符串语法。Python preflight 和 Runner
必须通过同一个 LocatorSpec compiler 构造 Playwright locator。

新生成的 BrowserObservation 为每个 probe 写入 `probe_id`，每个 locator hint
写入稳定 `candidate_id`。TargetBinding 保留 probe、observation、element 和
selected candidate；Runner 在 StepEvidence 中同时记录计划候选与实际解析候选。
这些新增字段对旧 `browser.observation.v2`、`grounding.target-binding.v1` 和
已持久化 research-v2 canonical payload 保持只读兼容。

`dsl_sha256` 定义为：

```text
lowercase_hex(SHA-256(UTF-8(canonical_json)))
```

## Approval And Execution Binding

generation artifact 返回并持久化 `case`、`dsl_sha256` 和 `dsl_canonical_version`。批准 generation 后，Go 从同一 `canonical_json` 创建 case，并在正式入队事务中：

1. 重新计算并验证 `dsl_sha256`。
2. 验证持久化 case 与 generation snapshot 的 JSON 语义相等。
3. 将 `dsl_snapshot`、原始 `dsl_canonical_json`、`dsl_sha256` 和版本固化到 execution job。

Worker 不重新定义 canonical JSON 序列化。正式 job 执行前必须验证版本和 canonical bytes 的 SHA，使用 `DSLCase` 验证完整默认字段及无未知字段漂移，并确认 job snapshot 与当前持久化 case 相同。任一检查失败时不得启动浏览器。

非 generation 发起的手工 case 执行没有权威绑定，暂时沿用 Worker 本地快照 SHA；它不属于 Agent 审批完整性合同。

迁移新增字段均允许 `null`，旧控制面和旧 job 可继续运行。旧 generation 首次由新 Go 控制面读取时，使用同一个 `dsl.canonical.v1` 实现规范化，并回填 canonical case、SHA 和版本；Alembic 与 Python 不实现第二套回填算法。
