# 执行日志

用于沉淀每次任务实际做了什么，方便后续追溯、复盘和回答一致化。

## 记录规则

- 每次处理需求后按时间倒序追加一条记录。
- 记录"目标、操作、结果、验证、后续"，避免只写结论。
- 如果执行过程中发现缺陷，同时在 `docs/bug-log.md` 追加对应条目并互相引用。

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
