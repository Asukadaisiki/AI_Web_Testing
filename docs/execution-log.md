# 执行日志

用于沉淀每次任务实际做了什么，方便后续追溯、复盘和回答一致化。

## 记录规则

- 每次处理需求后按时间倒序追加一条记录。
- 记录"目标、操作、结果、验证、后续"，避免只写结论。
- 如果执行过程中发现缺陷，同时在 `docs/bug-log.md` 追加对应条目并互相引用。

## 2026-03-23 22:27

- 任务：落实“治理优先，AI visual 作为验收 sidecar”的下一阶段安排
- 执行动作：在 `backend/app/ai/dsl_generator.py` 将 `AI_DSL_PROMPT_VERSION` 升级到 `2026-03-23.governance-v3.2`，把默认治理焦点切换为 `context_mismatch / bad_contracts`，并补齐名称/描述上下文对齐、`context_key` snake_case 修正、输出契约 `source` 推断与无稳定 `source` 的过滤；在 `backend/app/services/dsl.py` 将治理焦点选择逻辑改为排除已收敛的 `wrong_actions / invalid_structure` 后按 rejected 统计滚动选前 2 项，不足时按 `context_mismatch / bad_contracts` 回退，并把 `current_prompt_version / current_governance_focus_reasons / prompt_version_observation_note` 纳入 overview 只读字段；同步更新 `backend/app/schemas/settings.py`、`frontend/src/types/api.ts`、`frontend/src/pages/AISettingsPage.tsx` 与对应测试；新增 `docs/ai-visual-gray-acceptance-baseline.md`，并同步刷新 `docs/project-plan.md`、`docs/AI 自动化测试增强项目规划.md`、`README.md`、`docs/bug-log.md`
- 结果：AI DSL 治理主线已切到 v3.2，当前默认焦点不再重复治理 `wrong_actions / invalid_structure`，而是围绕 `context_mismatch / bad_contracts` 做滚动收敛；AI settings 治理概览可以直接看到当前治理焦点、当前 prompt 版本和 prompt 版本观测口径；AI visual 灰度验收基线已有独立文档，明确了采集指标、观察窗口、通过阈值和 3 条浏览器主回归门槛；`BUG-026` 文档状态已从 `open` 对齐为 `fixed`
- 验证：执行 `uv run pytest backend/tests/unit/test_dsl_validation.py backend/tests/unit/test_ai_settings_api.py -q`，结果 `40 passed`；执行 `cd frontend && npm test -- --run src/pages/AISettingsPage.test.tsx`，结果 `4 passed`
- 后续：下一轮继续基于 `top_rejection_reasons / rejection_reason_by_variant / retry_acceptance_by_reason` 收敛后续高频拒绝原因，并按灰度验收文档补采 AI visual 观测数据；本轮仍保持 `ENABLE_AI_VISUAL_LOCATE=false` 默认策略

## 2026-03-22 20:51

- 任务：落实“治理 v3 后续收敛 + AI visual 灰度基线”计划
- 执行动作：在 `backend/app/ai/dsl_generator.py` 和 `backend/app/services/dsl.py` 将当前治理焦点收敛到 `wrong_actions / invalid_structure`，新增安全 action alias 修正、DSL root/steps 包装层修复、step 字段别名归一化与结构化 normalization notes；在 `backend/app/locators/ai_visual.py`、`backend/app/locators/fallback.py`、`backend/app/services/settings.py`、`backend/app/schemas/settings.py` 增加进程内 `ai_visual_stats` 统计、overview 返回与 cache 命中/未命中/失效计数；同步更新 `frontend/src/types/api.ts`、`frontend/src/pages/AISettingsPage.tsx` 展示 AI visual 命中率、cache 复用率、locate 延迟与 skip 计数；补齐后端单测、前端页面测试，并将 `docs/bug-log.md` 中 `BUG-026` 状态同步为 `fixed`
- 结果：AI DSL 生成链路现在可以对 `open/tap/type/fill` 等白名单别名做确定性收敛，并能修复常见的 wrapped root、wrapped steps 与 step 轻微字段漂移；AI visual 默认关闭策略保持不变，但 settings overview 已可观测 locate 请求、成功率、cache 复用率、breaker/rate limit/disabled 跳过与 locate 延迟；文档状态与当前实现重新对齐，`BUG-026` 不再作为 open blocker
- 验证：执行 `cd backend && uv run pytest tests/unit/test_ai_visual.py tests/unit/test_locator_fallback.py tests/unit/test_dsl_validation.py tests/unit/test_ai_settings_api.py -q`，结果 `74 passed`；执行 `cd frontend && npm test -- --run src/pages/AISettingsPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx`，结果 `20 passed`；执行 `cd frontend && npm run build` 成功；执行 `cd backend && uv run pytest tests/integration/test_intervention_regression.py::test_local_single_case_smoke_executes_successfully tests/integration/test_intervention_regression.py::test_local_intervention_flow_rerun_hits_tier_zero tests/integration/test_intervention_regression.py::test_suite_context_rerun_failed_reuses_context_snapshot_after_manual_correction -q`，结果 `3 passed`
- 后续：下一轮继续基于治理页的 `top_rejection_reasons / rejection_reason_by_variant / retry_acceptance_by_reason` 滚动收敛高频拒绝原因；AI visual 若进入默认开启评估，再考虑把当前进程内统计升级为持久化观测

## 2026-03-22 16:20

- 任务：审查最新提交 `ba3316a feat: implement governance v3 and locator session cache`
- 执行动作：读取 `backend-call-chain-reviewer` 技能说明；审查 `backend/app/ai/dsl_generator.py`、`backend/app/services/dsl.py`、`backend/app/locators/fallback.py` 及对应单测；重点核对 governance v3 的 prompt 焦点选择、入库审计字段、locator session cache 命中/失效路径与测试覆盖；补记本次审查到 `docs/bug-log.md`
- 结果：确认本次提交在“AI visual session cache 命中后仅校验可见性、不再校验目标语义”上存在误命中风险；同时确认 governance v3 会把动态 rejection reason 注入 system prompt，但当前持久化记录未保存该动态焦点，导致 `prompt_version` 聚合与审计粒度不足
- 验证：执行 `uv run pytest backend/tests/unit/test_locator_fallback.py backend/tests/unit/test_dsl_validation.py -q`，结果 `42 passed`
- 后续：优先为 AI visual cache 增加命中后二次 DOM 语义校验和“命中到错误可见元素”的回归测试；其次补充 governance focus reasons 的持久化字段或将其纳入可审计版本标识

## 2026-03-22 15:40

- 任务：实现 AI DSL 治理收敛、固定浏览器回归口径与 Locator P4 sidecar
- 执行动作：将 `AI_DSL_PROMPT_VERSION` 升级到 `2026-03-22.governance-v3`；在 `backend/app/services/dsl.py` 基于现有 rejected 反馈选择前 2 个治理焦点原因并默认回退到 `context_mismatch / bad_contracts`；在 `backend/app/ai/dsl_generator.py` 增加治理焦点 prompt 规则、contract alias 自动修正、单边契约 preserve fallback，并修复 `_normalize_contracts()` 中未定义变量引用；在 `backend/app/locators/fallback.py` 增加会话级 AI selector LRU 缓存、命中前可见性校验、失效清理与 `cache_hit / cache_miss / cache_invalidated` debug 日志；补充 DSL / locator 单测、更新前端测试中的 `prompt_version` 口径；同步刷新 `README.md`、`backend/tests/README.md`、`docs/project-plan.md`、`docs/bug-log.md`
- 结果：AI DSL 生成链路现在会按治理数据默认收敛高频拒绝原因，`governance-v3` 能自动吸收 `label/type/isRequired/valueFrom` 等 contract alias；Locator 新增的会话缓存会优先于 semantic / AI visual 链路但仍低于 Tier 0 人工修正，重复目标场景可减少重复截图与 VLM 调用；浏览器级回归、README 与测试文档口径已统一为 3 条主回归 + 2 条扩展回归
- 验证：执行 `cd backend && uv run pytest tests/unit/test_dsl_validation.py tests/unit/test_locator_fallback.py tests/unit/test_ai_settings_api.py tests/integration/test_intervention_regression.py`，结果 `51 passed`；执行 `cd frontend && npm test -- --run src/pages/AISettingsPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx`，结果 `20 passed`；执行 `cd frontend && npm run build` 成功
- 后续：下一轮继续沿 `governance-v3` 基线滚动收敛后续高频拒绝原因，并补 AI visual 灰度验收中的缓存收益、命中率和延迟观测

## 2026-03-22

- 任务：阅读执行日志与计划文档，规划项目下一步安排
- 执行动作：通读 `docs/project-plan.md`、`docs/execution-log.md`、`docs/bug-log.md` 与 `README.md` 的当前状态说明；核对最近两轮迭代已完成项与后续项，确认 AI DSL 治理、浏览器级回归矩阵补强、Locator P4 会话级缓存仍是主线；同时确认 `bug-log` 当前无 `open` 缺陷，auth 与 AI visual 默认开启策略仍不应抢占当前优先级
- 结果：整理出下一阶段建议顺序为“1) AI DSL 数据驱动优化第二轮，先收敛前 2 个高频拒绝原因；2) 固化浏览器级三条主回归并收敛 flaky 风险；3) 以 sidecar 形式补 Locator P4 会话级缓存与命中观测；4) AI visual 灰度验收指标；5) 登录/认证体系后置”；当前不建议重新铺大功能面
- 验证：人工核对 `project-plan`、`execution-log` 与 `README` 口径一致；确认 `docs/bug-log.md` 当前记录均为 `fixed`；本次仅做分析与规划，未运行自动化测试
- 后续：若进入实现，建议直接从“治理数据驱动的 prompt / normalization 收敛”开工，并把浏览器级固定回归作为同轮验收门槛；Locator P4 缓存应作为并行 sidecar，而不是新主线

## 2026-03-20 15:30

- 任务：修复 governance v2 review follow-up 问题并收敛前端慢测试
- 执行动作：将 `dsl_generator.py` 中 `required` 自动修正的 `is not` 比较改为值比较；为 contract normalization 提取 `ContractNormalizationContext` dataclass，收敛 `_normalize_contracts()` / `_repair_contract_payload()` 的参数；把 system/user prompt 规则提取为模板常量和构造 helper，避免继续在单个函数里累积硬编码字符串；确认 `_normalize_string()` 对 `None` / 非字符串 / 空白字符串返回 `None`，并补充单测；为 AI settings 治理页的 prompt version 指标补上“总请求 / 采纳 / 放弃 / 重试采纳”列说明；将原先 30s 的 AI settings 大测试拆成“概览渲染”和“筛选详情”两条，用更小的 15s 超时；将 CaseWorkbench 几条反馈相关测试超时从 15s 下调到 10s
- 结果：review 中的可落地代码问题已收口，后端 contract normalize 与 prompt 组织更易维护，前端 prompt version 展示更可读，治理页测试不再依赖 30s 大超时，反馈相关页面测试也回落到更合理的 10s
- 验证：执行 `uv run pytest backend/tests/unit/test_dsl_validation.py backend/tests/unit/test_ai_settings_api.py`，结果 `33 passed`；执行 `cd frontend && npm test -- --run src/pages/AISettingsPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx`，结果 `20 passed`
- 后续：commit 粒度问题属于提交流程约束，本轮已在代码结构和测试拆分上收口，但后续仍建议把 feature / bugfix / docs / test-only 变更拆成更小提交

## 2026-03-20

- 任务：同步计划文档基线，并落实 AI DSL 第二轮治理与浏览器回归矩阵补强
- 执行动作：对照 `docs/execution-log.md`、`README.md` 与现有代码实现，更新 `docs/project-plan.md` 到 2026-03-19 真实状态，明确 Locator P0-P3 已完成、P4 待做、auth 基本未启动；扩展 AI settings overview 的 `prompt_version_breakdown` 聚合与前端展示；将 `AI_DSL_PROMPT_VERSION` 升级为 `2026-03-20.governance-v2`，针对高频拒绝原因补强 contract / step normalization 与自动修正规则；补充浏览器级回归中的单 Case smoke、Suite Context + correction + rerun 场景；修复 suite `rerun-failed` 只接受 `failed` 不接受 `needs_intervention` 的语义缺口；同步放宽慢速前端页面测试的显式超时并补记文档缺口与重跑缺口到 `docs/bug-log.md`
- 结果：项目计划、README 与执行日志口径已统一；治理页现在可对比 prompt 版本效果；AI DSL 对单对象 contracts、缺失 `name/context_key`、别名 `value_type/source`、布尔值和单步草案的自动修正更稳；套件失败重跑已能覆盖 `needs_intervention` 场景，浏览器级固定主回归扩展为 5 条本地集成验证
- 验证：执行 `uv run pytest backend/tests/unit/test_dsl_validation.py backend/tests/unit/test_ai_settings_api.py backend/tests/unit/test_suites_api.py`，结果 `47 passed`；执行 `cd frontend && npm test -- --run src/pages/AISettingsPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx`，结果 `19 passed`；执行 `cd backend && uv run pytest tests/integration/test_intervention_regression.py`，结果 `5 passed`
- 后续：下一轮可直接基于治理页的 `top_rejection_reasons`、`rejection_reason_by_variant` 与 `prompt_version_breakdown` 收敛前 2 个高频拒绝原因，并继续补 Locator P4 会话级缓存与 AI visual 灰度验收指标

## 2026-03-18 23:05

- 任务：修复 AI DSL 治理增强提交的 Code Review 问题
- 执行动作：移除 `generate_case_draft()` 到 `_normalize_generated_case()` 间重复的 `resolve_generation_profile()` 推导，改为单次计算后透传；为 `20260318_0012` 迁移补充“冻结自应用层规则”与 `missing_name_fallback` 无法回填的注释；为 `has_risk_flags` 增加后端方言感知的 JSON 数组长度 helper；将 `AISettingsPage` 的风险筛选从字符串哨兵切换为 `boolean | undefined` + `allowClear`；拆分 `AISettingsPage.test.tsx` 中过大的集成测试为“治理页渲染/筛选/详情”和“保存配置”两条测试
- 结果：review 中的低/中优先级实现问题已收口，生成链路减少重复推导，风险筛选的后续可维护性更高，前端测试耗时下降且职责更清晰
- 验证：执行 `uv run pytest backend/tests/unit/test_dsl_validation.py backend/tests/unit/test_ai_settings_api.py backend/tests/unit/test_models.py` 结果 `40 passed`；执行 `cd frontend && npm test -- --run src/services/api.test.ts src/pages/AISettingsPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx` 结果 `28 passed`；执行 `cd frontend && npm run build` 成功
- 后续：如后续将 `risk_flags_json` 列切到 PostgreSQL `JSONB`，只需扩展当前 helper，无需改调用方

## 2026-03-18 22:31

- 任务：实现 AI DSL 可用率提升第一批（单次生成增强）
- 执行动作：为 `dsl_generation_runs` 增加 `prompt_variant / context_profile / risk_flags_json` 与迁移 `20260318_0012`；重构 `backend/app/ai/dsl_generator.py` 按 `contracts_focus / repair_steps / rewrite_from_case / baseline_draft` 选择 prompt variant，并输出结构化 risk flags；扩展 DSL 治理列表/详情/overview 接口支持 `prompt_variant / rejection_reason_code / has_risk_flags` 筛选与 variant/context 聚合；更新 `AISettingsPage` 的治理筛选、表格列、详情抽屉与概览展示；在 `CaseWorkbenchPage` 补充生成预览风险标签展示；同步更新后端单测、前端 API/页面测试与模型列检查
- 结果：AI DSL 生成链路现已可区分不同生成场景的 prompt variant，治理页可按 variant、风险标签、拒绝原因定位低质量请求，工作台可直接看到生成草案的风险标签与上下文档案
- 验证：执行 `cd backend && uv run alembic upgrade head` 成功；执行 `uv run pytest backend/tests/unit/test_dsl_validation.py backend/tests/unit/test_ai_settings_api.py backend/tests/unit/test_models.py` 结果 `40 passed`；执行 `cd frontend && npm test -- --run src/services/api.test.ts src/pages/AISettingsPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx` 结果 `27 passed`；执行 `cd frontend && npm run build` 成功
- 后续：可继续基于 `prompt_variant_breakdown / rejection_reason_by_variant` 做下一轮 prompt 文案调优，或补充更细的风险标签归因
## 2026-03-18 14:42

- 任务：同步 docs 文档口径到当前实现
- 执行动作：对照 `frontend/src/app/AppRouter.tsx`、`frontend/src/pages/SuiteRunDetailPage.tsx`、`backend/app/api/routes/settings.py` 与 `backend/app/api/routes/dsl.py` 核对已落地能力；更新 `docs/project-plan.md` 与 `docs/frontend-design.md` 中的页面路由、Suite 批次能力和 AI 设置口径；补记 `docs/bug-log.md` 与本条执行日志
- 验证：人工核对文档与代码实现一致，已覆盖 `/dashboard` 重定向、Case/Suite 新建编辑路由、Suite 批次详情页、`/settings/ai` 与 DSL 治理接口现状
- 后续：如需继续统一仓库其余说明文档，可再同步 `README.md` 与 `frontend/src/pages/README.md`

## 2026-03-18 13:58

- 任务：实现 AI DSL 治理与观测闭环
- 执行动作：扩展 `dsl_generation_runs` 模型（新增 `project_id / case_id / prompt_version / warnings_json / normalization_notes_json / rejection_reason_code / feedback_note`）；更新 service 与 routes 支持多维筛选、详情查询、rejected 必填结构化原因、overview 聚合；重构 `AISettingsPage` 为治理筛选表格 + 详情抽屉；改造 `CaseWorkbenchPage` 支持拒绝原因与备注
- 验证：后端全量 passed，前端页面/API 测试 passed，构建成功
- 后续：基于 `top_rejection_reasons / model_outcome_breakdown` 真实数据驱动 prompt 优化

## 2026-03-17 23:23

- 任务：实现 AI DSL 生成草案的采纳/放弃反馈闭环
- 执行动作：新增 `feedback_status / feedback_import_mode / feedback_recorded_at` 字段与迁移 `20260317_0010`；反馈写入服务（首次落库、幂等、冲突 409）；`PATCH /api/v1/dsl/generations/{id}/feedback`；前端导入动作上报 accepted、放弃上报 rejected；AISettingsPage 扩展反馈指标
- 验证：后端 130 passed，前端 25 passed，迁移通过，构建成功
- 后续：沿治理主线推进更细的采纳分析

## 2026-03-17 23:50 / 23:57

- 任务：将 AI DSL 反馈闭环与 feedback ownership 修复同步到 GitHub（两次推送）

## 2026-03-19

- 任务：实现下一阶段计划 M1/M2/M3 主干
- 执行动作：为 `dsl_generation_runs` 增加 retry context 字段与 Alembic 迁移；扩展 `GenerateDslRequest` / AI settings 统计 schema；在 DSL 生成器中接入“拒绝原因 -> 固定 prompt 策略”与重试版 `prompt_version`；工作台支持“拒绝后按原因重试生成”；AI Settings 页面新增重试成效概览与详情字段；fallback locator 增加 overlay 穿透、严格匹配 + Jaccard 校验、中文单字回退；`ai_visual` 落地 `deep_locate` 两阶段定位与 DOM 多候选 VLM 排序接口
- 验证：后端单测 `71 passed`，浏览器集成 `1 passed`，前端 `29 passed`，前端构建成功

## 2026-03-19（follow-up）

- 任务：修复 review 指出的 locator/测试可维护性问题
- 执行动作：在 `semantic.py` 暴露公共候选收集接口，移除 `fallback.py` 对私有 `_` 函数的依赖；为 `deep_locate` 引入总超时预算；`_crop_and_scale` 改为 lazy import Pillow；补充无效 base64 的明确报错、负索引防御校验、debug 日志与阈值注释；恢复 JSON 提取注释；修正 `AISettingsPage.test.tsx` 缩进
- 验证：后端相关单测 `62 passed`，浏览器集成 `1 passed`，前端测试 `3 passed`

## 2026-03-16 11:35

- 任务：AI 生成 DSL 深化第一批
- 执行动作：扩展生成请求（`generation_mode / import_mode / current_case / current_steps / preserve_contracts`）；重写 `dsl_generator.py` 补上下文注入与自动修正；`GET /api/v1/settings/ai/overview` 生成观测；前端工作台展示修正/warning/导入控制
- 验证：后端 119 passed，前端 19 passed，构建成功

## 2026-03-16 10:29

- 任务：AI 生成 DSL 最小闭环
- 执行动作：新建 `dsl_generator.py`，`POST /api/v1/dsl/generate` 接入 OpenAI 兼容接口；前端 CaseWorkbenchPage 自然语言生成区域；回归覆盖 prompt 约束/503/502
- 验证：后端 114 passed，前端 13 passed，构建成功

## 2026-03-15（合并记录）

本日完成以下工作：

1. **corrections 运营增强**：新增 `locator_correction_events` 模型与迁移，corrections overview/events/bulk API，前端命中趋势图与批量启停
2. **corrections review 修复**：移除死代码、修正缩进、补充双事件语义注释、统一前端判空风格
3. **v3.4 延后加固全部完成**：`RUNTIME_STATE` 线程安全、JSON 提取健壮化、`correction_value` 格式校验、日志收敛、`deep_locate` 死参数清理（新增 25 条回归）
4. **corrections 并发安全修复**：`CorrectionConflictError` + 409 映射 + PostgreSQL `FOR UPDATE` 锁 + 规范化修复迁移
5. **本地夹具页真实回归闭环**：`needs_intervention -> correction -> rerun -> Tier 0 hit` 端到端集成测试
6. 多次 GitHub 同步推送

验证：后端最终 109 passed（+25），前端 39 passed，浏览器集成 2 passed

## 2026-03-14 23:20

- 任务：v3.4 第一批稳定化：修正管理入口、AI 视觉运行保护与 runner 解耦
- 执行动作：引入 `CorrectionStore` 解耦 runner 与 db；AI visual 限流/熔断/超时；新增 CorrectionsPage 与前端路由
- 验证：后端 74 passed，前端 36 passed，构建成功

## 2026-03-08（合并记录）

- 新增 `docs/execution-log.md` 与 `docs/bug-log.md`
- 在 `AGENTS.md` 增加日志沉淀规则与 GitHub 同步追问规则
## 2026-03-22 16:06

- �����޸� AI visual session cache �������� governance focus ���ȱ��
- ִ�ж������� `backend/app/locators/fallback.py` Ϊ AI visual cache ���в��� DOM snapshot ���帴�ˣ���ƥ��ʱ����ʧЧ���沢����������λ��·���� `backend/app/models/dsl_generation_run.py`��`backend/app/services/dsl.py`��`backend/app/schemas/dsl.py` ���� `governance_focus_reasons_json / governance_focus_reasons` �־û���ӿ�ӳ�䣬������Ǩ�� `backend/alembic/versions/20260322_0014_dsl_generation_governance_focus_audit.py`��ͬ�����º�˵��⡢ǰ�������� AI ��������չʾ/����
- ��֤��ִ�� `cd backend && uv run pytest tests/unit/test_locator_fallback.py tests/unit/test_dsl_validation.py tests/unit/test_models.py`����� `54 passed`��ִ�� `cd frontend && npm test -- --run src/pages/AISettingsPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx`����� `20 passed`��ִ�� `cd frontend && npm run build` �ɹ�
