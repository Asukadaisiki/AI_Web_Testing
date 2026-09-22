# 05 分阶段迁移

原则：

1. **每个阶段结束系统都必须可运行**（三条服务起得来、离线链路测试绿、E2E 能跑）。
2. **每阶段都有可执行的验收门禁**，门禁不过不进入下一阶段。
3. **不做大爆炸切换**：新形态与旧形态双写/双读一个版本周期，再删旧的。
4. **删除动作分散在各阶段内**，不集中到最后。

阶段总览：

| 阶段 | 主题 | 预计工作量 | 是否改变行为 |
|---|---|---|---|
| P0 | 冻结与可信门禁 | 0.5 天 | 否 |
| P1 | 契约收敛（schema + 单校验器） | 3~5 天 | 否（等价重构） |
| P2 | 权威形态统一（工件化） | 3~4 天 | 是（删除回退链） |
| P3 | profile 删除（v1/legacy） | 1~2 天 | 是（删除能力） |
| P4 | Go 模块与命名重构 | 3~4 天 | 否 |
| P5 | Python worker 重构 | 3~4 天 | 否 |
| P6 | 前端与文档对齐 | 1~2 天 | 是（编辑器写工件） |

---

## P0 冻结与可信门禁

**目标**：在重构开始前，先把"什么叫做绿"定义清楚，并把在途修复冻结成一次提交。

**动作**

1. 把本次为打通 E2E 的修复整理成**一次提交**（建议 `fix: unblock agentic research e2e execution path`），确认离线门禁全绿后再提交。
2. 删除临时产物：`backend-go/cmd/tmp-execute-generation/`、`_mk_draft50.py`、`_draft50.json`、`_gen50.json`、`_batch107_report.json`、`_flow*.json`、`_page_products.json`、`_ev*.json`、`_plan_dump.json`、`_steps_dump.json`、`_smoke_dsl.json`、`_probe_real.py`、`_real_probe.json`、`_migrate_dump.txt`、`_case59_dsl.json`。
3. 固化门禁脚本（`make gate` 或 `scripts/gate.ps1`）：
   - Go：`gofmt -l`、`go build ./...`、`go vet ./...`、`go test -count=1 ./...`（需 `TEST_DATABASE_URL`）
   - Python：`compileall`、`unittest discover`（含 `RUN_BROWSER_INTEGRATION=1` 的 Chromium 用例）
   - 前端：`npm test`、`npm run build`
   - 契约：`make contracts && git diff --exit-code`（P1 后生效）
4. 记录一次基线：把当前 Go/Python/前端测试数量、E2E 结果写入 `refactor-plan/baseline.md`。

**验收门禁**：`make gate` 全绿；工作区无未跟踪临时文件。

**风险**：低。**回滚**：不需要。

---

## P1 契约收敛（最高优先级）

**目标**：把"case 长什么样"从 14 个定义点收敛到 1 个 schema + 1 个校验器，并让漂移不可能发生。

**动作**

1. 写 `contracts/case.v3.schema.json`：合并现有三套 case 契约，包含
   - action 枚举与每种 action 的必填字段、是否要求 target binding；
   - condition 类型表 + **阶段表**（pre/post 是否允许，见 03 §5.1）；
   - step 字段（含 `evidence` 子对象）；
   - 明确 `draft` / `executable` 两阶段的差异（旧拼写容忍清单）。
2. 写生成器（Go + Python + 提示词片段），产物入 `contracts/generated/`，CI 校验不可手改。
3. 实现唯一入口 `casegen.Validate(payload, phase)`：
   - 计划条件保留规则只写一次（草稿与可执行共用）；
   - 条件阶段规则读生成的表，不再散落 `if type ==`；
   - 删除 `validateConditionPreservation` / `validateCompiledConditions` 双份实现。
4. 提示词中所有取值清单改由生成器注入；删除手写清单。
5. 新增**跨语言契约一致性测试**：`contracts/fixtures/case/*.json`（每个 fixture 标注期望结论与错误码），Go 与 Python 各跑一遍并比对结论。
6. 把历史 golden fixture 全部纳入该测试集（含 `dsl_research_v2_contract.json`）。

**验收门禁**

- `grep -rn "func Validate" backend-go/internal` 可人工审阅完毕（目标 ≤ 3 个）。
- 跨语言一致性测试用例数 ≥ 30，全部通过。
- `grep -rn "value_equals" .` 为空。
- 条件类型集合在全仓库只出现于 schema 与生成物。
- 行为等价证明：P1 前后跑同一批历史 fixture，结论完全一致（新增阶段规则导致的差异必须逐条记录并解释）。

**风险**：中。生成器本身是新代码，可能引入"生成物与手写逻辑不一致"。**缓解**：先只生成**常量表与类型**，业务规则仍手写在单校验器里；生成器成熟后再扩大范围。

**回滚**：保留旧校验器一个版本，通过 feature flag 切换入口；一致性测试发现差异即回退 flag。

---

## P2 权威形态统一（工件化）

**目标**：消灭 BUG-212 那一类"同一对象两副身子"的问题。

**动作**

1. 建 `case_artifacts(id, content_hash, payload, created_at)`，不可变、按 hash 去重。
2. 生成器改为写工件；`test_cases` 增加 `artifact_id` 并**双写** `dsl`（一个版本周期）。
3. 执行队列改为按 `artifact_id` 取 payload；**删除 `COALESCE(j.dsl_canonical_json, j.dsl_snapshot::text, tc.dsl::text)`**。
4. 删除 `validatePersistedCaseBindings` / `validatePersistedCaseMatchesBinding`（不再存在"落库形态 vs 工件形态"的比较需求）。
5. 审批记录独立成 `case_approvals(artifact_id, plan_id, plan_version, actor, created_at)`，`execute_dsl` 只接受已审批的 artifact_id。
6. 前端用例编辑器改走同一编译器产出工件（可与 P6 合并）。

**验收门禁**

- `grep -rn "COALESCE" backend-go/internal/execution` 为空。
- 新增集成测试：从"生成 → 审批 → 入队 → worker 领取"全链路取到的 payload **字节等于**工件 payload。
- 手工验证：直接改 `test_cases` 行不再影响执行结果（因为它已不参与）。

**风险**：中高（涉及数据迁移）。**回滚**：双写期内可回退读取路径；迁移脚本可逆（工件表只增不减）。

---

## P3 profile 删除

**目标**：删掉没有产品价值的并行实现，直接减少代码量与分支。

**动作**

1. 确认 `legacy-v1` / `research-v1` 无生产数据（当前 `execution_batches` 为空、research 结果为试点产物）→ 归档 `research/results/*` 后删除读取路径。
2. 删除 Go：`validateLegacyCase`、`validateResearchCase`、v1 相关常量与迁移分支。
3. 删除 Python：`contracts/dsl.py`、`contracts/action_ir.py`、v1 runner 分支。
4. 删除 v1 相关 fixture 与测试（保留 1 个"历史 payload 必须被明确拒绝"的负例）。
5. 数据库：删除仅服务 v1 的列/表（`page_state` 相关、v1 snapshot 列）。

**验收门禁**

- `grep -rn "research-v1\|legacy-v1" backend-go browser-worker/src` 为空。
- Go/Python 测试数下降但全绿；E2E 仍能跑通。
- 代码量：Go 生产代码 ≤ 24,000 行。

**风险**：低（无数据）。**回滚**：git revert。

---

## P4 Go 模块与命名重构

**目标**：20 个包 → 4 上下文 + 3 内核；拆开 7 个千行文件；执行 04 的重命名表。

**动作**

1. 按 04 §2.1 移动包（先移动、后重命名、最后拆文件，三步分开提交）。
2. 依赖方向由 CI 检查：新增 `scripts/check-deps`（解析 import 图，禁止跨上下文直接 import 内部实现）。
3. 拆巨型文件（04 §3 表），每次只拆一个文件并保持测试绿。
4. 统一命名（`semantic_target`→`target` 等），同步改 DB 列名与迁移。

**验收门禁**

- `internal/` 顶层包 ≤ 10。
- 无 > 800 行文件。
- 依赖检查脚本通过。
- `make gate` 全绿。

**风险**：中（大范围移动容易冲突）。**缓解**：移动与逻辑修改**不放在同一个提交**里。

**回滚**：逐包提交，可单包回退。

---

## P5 Python worker 重构

**目标**：9 模块 → 6 模块；拆开 3 个巨型文件；合并契约家族。

**动作**

1. 合并 `contracts/dsl.py` + `action_ir.py` + `action_ir_v2.py` → 生成物 `contracts/case.py`。
2. 拆 `exploration/page_explorer.py` → `explorer.py` / `collector.py` / `a11y_name.py`；**可访问名计算收敛到 `a11y_name.py`**（BUG-211 的根治点，需为私有区字形补测试）。
3. 拆 `runners/playwright_runner.py` → `runner.py` / `conditions.py` / `actions.py`；条件评估唯一化并显式接收 `phase`。
4. 合并或删除 `scripts/research_e2e.py`（2759 行）与 `run_agentic_e2e.py` 的重复部分，驱动逻辑进 `e2e/` 包。
5. 契约形状校验改由生成模型承担，删除手写业务规则。

**验收门禁**

- Python 无 > 800 行文件。
- 契约一致性测试（P1 建立）仍全绿。
- Chromium 集成测试（`RUN_BROWSER_INTEGRATION=1`）全绿。
- 代码量：Python ≤ 15,000 行。

**风险**：中（runner 是执行正确性的关键路径）。**缓解**：拆分为纯移动，行为改动单独提交；保留 `tests/test_playwright_runner.py` 作为行为锚。

---

## P6 前端与文档对齐

**目标**：前端跟随契约变化；文档与代码一致。

**动作**

1. 用例编辑器写工件（走同一编译器），删除旧字段处理。
2. `services/` 按上下文分组。
3. 重写 `docs/architecture-guide.md` 与 DSL 规范，使其与新契约一致；删除已失效的 profile 文档。
4. 把本目录的 `01`~`08` 归档为"重构完成报告"，记录实际与计划的差异。

**验收门禁**

- `npm test`、`npm run build`、Playwright smoke 全绿。
- 文档中提到的每个契约名都能在 `contracts/` 找到。
- 端到端手工验收：新用户按 `docs/` 从零跑通一次"目标 → 报告"。

---

## 进度与度量

每个阶段结束时记录到 `refactor-plan/progress.md`：

| 指标 | 基线（P0） | 目标 |
|---|---|---|
| Go 生产行数 | 29,458 | ≤ 18,000 |
| Python 行数 | 22,660 | ≤ 14,000 |
| > 800 行文件数 | 21 | 0 |
| case 契约定义点 | 14 | 1 |
| profile 数 | 3 | 1 |
| `internal/` 顶层包 | 20 | ≤ 10 |
| 门禁耗时 | 待测 | ≤ 10 分钟 |

## 明确禁止的做法

- 禁止"边重构边加功能"：重构期内只允许修 bug，不允许新增 profile/条件类型/工具。
- 禁止"重写而不是迁移"：不允许新建一套并行实现然后切换（这正是当前混乱的来源）。
- 禁止在没有跨语言一致性测试的情况下修改任何契约。
- 禁止为了让某个测试通过而放宽契约；契约变更必须同时更新 schema、生成物、fixture 与文档。
