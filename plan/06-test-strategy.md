# 06 测试策略

## 1. 现状：测试很多，但抓不到这类缺陷

当前 Go 有 50 个测试文件 / 20,820 行，Python 有 223 个测试用例。体量不小，但本次 6 个缺陷里，**没有一个是被现有测试提前发现的**。原因有三条，每条都可修。

| 问题 | 具体证据 | 后果 |
|---|---|---|
| 测试可以绕过契约 | `internal/taskplan/agentic_chain_offline_test.go` 直接调 `CompileDraftCase`，从不调 `ValidateExecutableCase`；它编出的 click 步骤前置条件为空，却一路绿灯 | 编译产物能否入队从未被验证 → BUG-212 类问题可长期潜伏 |
| 测试可以伪造形态 | `internal/execution/store_postgres_test.go` 把 canonical JSON 当"落库 DSL"传入 | 真实的 body 形态从未被覆盖 → BUG-212 |
| 没有跨语言一致性测试 | 条件类型集合 6 处定义，无任何比对 | 提示词写不存在的 `value_equals` 无人发现 |

## 2. 目标测试结构

```
                     ┌──────────────────────────────┐
  L4  真机 E2E       │ 少量：1 条基线任务，手动/定时  │  ← 花钱，只在阶段门禁跑
                     ├──────────────────────────────┤
  L3  离线全链路     │ 录制真实产物回放，无模型无网络 │  ← 主力回归
                     ├──────────────────────────────┤
  L2  契约一致性     │ Go/Python 同 fixture 同结论    │  ← 防漂移
                     ├──────────────────────────────┤
  L1  单元           │ 纯函数、状态机、条件表         │
                     └──────────────────────────────┘
```

### L1 单元

- 只测纯逻辑：条件阶段表、编译器字段归一化、计划状态机、评分函数。
- **不测 JSON 形状**（形状由生成类型与 L2 保证）。
- 巨型 `*_test.go` 一律随实现拆分。

### L2 契约一致性（新建，最高价值）

```
contracts/fixtures/case/
  valid-draft-minimal.json
  valid-executable-goto-first.json
  invalid-precondition-transition.json          → 期望错误码 CASE_CONDITION_PHASE
  invalid-click-without-precondition.json       → CASE_ACTION_REQUIRES_CONDITIONS
  invalid-unknown-field-page_state.json
  invalid-unbound-element-condition.json
  ...
```

每个 fixture 带 `expected: {accept: bool, code: string}`。测试要求：

1. Go 校验器结论 == fixture 期望；
2. Python 校验器结论 == fixture 期望；
3. 两者的错误码相同。

**这套测试能直接抓住的缺陷**：`value_equals` 漂移、BUG-207（哪些 action 需要绑定）、BUG-208（旧拼写容忍度）、BUG-213（条件阶段）。

### L3 离线全链路（主力回归）

用**录制的真实产物**（而不是手写 payload）驱动完整链路，全程无模型、无网络：

```
recorded plan(v5, 10/10 grounded) + recorded draft
  → 编译器
  → ValidateExecutableCase            ← 必须断言"可入队"
  → 落库为工件
  → 队列入队（CreateBatch）
  → 断言 job 取到的 payload 字节等于工件 payload
```

硬规则：

- **任何声称"全链路"的测试都必须经过真实校验入口**，禁止直接调编译器后不校验产物。
- 每个 profile（重构后只有一个）至少一条，覆盖：首步 goto、无名字控件（CSS 候选）、`assert_text` 绑定、条件阶段。
- 录制产物放在 `research/fixtures/recorded/`，只增不改。

**这套测试能直接抓住的缺陷**：BUG-212（产物不可执行）、BUG-211（CSS 候选回放中断探测）。

### L4 真机 E2E

- 只保留 **1 条**基线任务（当前是 Blue Top 加购），跑一次要花钱，因此：
  - 只在阶段门禁或发布前跑；
  - 运行前必须先过 L3；
  - 驱动必须能把所有已知失败点一次性覆盖（本次教训：不要"跑一次发现一个"）。
- 驱动本身要能回答澄清问题、能处理重新规划，否则它会把"驱动能力不足"误报成"产品缺陷"。

## 3. 硬规则（写进 CI 与评审清单）

1. **禁止绕过契约**：测试中出现"手工构造 executable payload"时，必须同时调用真实校验入口。可用 lint 检查：`go vet` 自定义分析器，或简单的 `grep` 门禁（测试文件里出现 `plan_binding` 字面量而同一文件没有 `ValidateExecutableCase` 即失败）。
2. **禁止改 fixture 让测试变绿**：fixture 变更必须在提交信息中单独说明理由。
3. **契约变更三件套**：schema + 生成物 + fixture 同时更新，缺一不可。
4. **新增条件类型**必须补 L2 fixture（合法 + 非法各一）与 L3 覆盖。
5. **跨语言改动**（Go 与 Python 同时涉及）必须补 L2 fixture。
6. **门禁必须包含 gofmt/build/vet/test/compileall/unittest/前端 build**（BUG-209 的教训）。

## 4. 测试基建修复（顺带）

- `cmd/migrate` 的 `DROP DATABASE ... WITH (FORCE)` 在 Windows 会挂死（BUG-210），已改为 `pg_terminate_backend` + `DROP DATABASE IF EXISTS`；重构时把该 helper 提到 `testutil`，供所有 Postgres 测试复用。
- Postgres 测试目前共享同一个库、并行包之间偶发互相干扰（本次全量跑出现过一次 `TestPostgresRepositoryPersistsVersionedTaskPlan` 偶发失败，单独跑通过）。重构时应为每个测试包分配独立 schema 或独立库，消除偶发。
- Chromium 集成测试需要 `PLAYWRIGHT_BROWSERS_PATH`，应在 `make gate` 中显式设置，避免"本地绿 CI 红"。

## 5. 验收标准

1. L2 一致性测试 ≥ 30 个 fixture，Go/Python 结论与错误码完全一致。
2. L3 离线全链路覆盖首步 goto、无名字控件、`assert_text` 绑定、条件阶段四个场景，且每个都断言"可入队"。
3. 门禁 10 分钟内跑完，且包含 gofmt/build/vet/两语言测试/前端 build。
4. 引入一个故意的契约漂移（例如在提示词里写一个不存在的条件类型），L2 必须失败。**这条要作为测试策略本身的验收测试。**
