# 执行日志

用于沉淀每次任务实际做了什么，方便后续追溯、复盘和回答一致化。

## 记录规则

- 每次处理需求后按时间倒序追加一条记录。
- 记录"目标、操作、结果、验证、后续"，避免只写结论。
- 如果执行过程中发现缺陷，同时在 `docs/bug-log.md` 追加对应条目并互相引用。
## 2026-03-28 18:18

- 任务：修复认证收口 review 提出的默认密码、session 配置、artifacts 匿名访问、前端缓存隔离和 `/auth/me` 误报登录失效问题
- 执行动作：按 TDD 先补齐 `AUTH_SESSION_SECRET` 必填、`AUTH_SESSION_HTTPS_ONLY` 默认开启、`/artifacts/**` 必须鉴权、`LEGACY_PASSWORD_HASH` 不可再匹配公开默认密码、前端 logout/401 清空 React Query 缓存、`/auth/me` 仅在 401 时判定为未登录的失败测试；随后在后端移除公开 session secret 默认值并增加 fail-fast，新增受保护的 artifacts 下载路由替换 `StaticFiles` 匿名挂载，将认证迁移中的存量密码回填改为不可直接登录的重置占位值；前端在 API 层补充带 `status` 的 `ApiError`，在 `AuthContext` 中区分 401 与 5xx/网络故障、清空缓存并阻断启动态竞态覆盖，在 `ProtectedRoute` 中对认证加载失败展示错误块而不是跳转登录页；同步刷新 `README.md`、`docs/project-plan.md` 与 `backend/.env.example` 的安全口径
- 结果：review 中的 2 个 Critical、2 个 Important 与 1 个 Minor 均已收口；后端启动现在要求显式 session secret，Secure Cookie 默认开启，执行证据改为受登录保护访问，前端多账号切换不再残留上一账号缓存，`/auth/me` 的非 401 失败会保留错误态而不是误报为登录失效
- 验证：
  - `cd backend && uv run pytest tests/unit/test_config.py tests/unit/test_main.py tests/unit/test_auth_api.py -q`，结果 `14 passed`
  - `cd frontend && npm test -- --run src/auth/AuthContext.test.tsx src/app/AppRouter.test.tsx src/services/api.test.ts`，结果 `24 passed`
- 后续：继续执行一轮受影响后端 API、DSL 治理回归与前端构建验证，确认本次修复没有带入新的 blocker

## 2026-03-28 17:32

- 任务：实现 M1 收口计划中的“治理主线收尾 + 平台基础认证入口”，并完成最终验证与文档收口
- 执行动作：在后端新增 Cookie Session 认证配置、密码哈希与会话读写能力，落地 `POST /api/v1/auth/login`、`POST /api/v1/auth/logout`、`GET /api/v1/auth/me` 与统一登录依赖；将业务 API 切换为默认要求登录，并把 `cases / suites / executions / corrections / dsl` 等写接口中的 `actor_user_id / created_by` 改为由登录态覆盖；扩展 `users` 模型与 Alembic 迁移，新增非法 retry 不留错误审计记录的 integration 回归；前端新增 `AuthProvider`、`/login`、受保护路由、Header 当前用户与统一 `401` 回退；在验证阶段修复 `20260324_0015` 的 PostgreSQL `BOOLEAN DEFAULT 1` 兼容性、补上 `itsdangerous` 运行时依赖，并为 5 条全量运行易超时的前端页面测试补显式 timeout；同步更新 `README.md` 与 `docs/project-plan.md`
- 结果：M1 基础认证入口已可运行，主业务 API 已默认受登录保护，治理主线继续以 `2026-03-24.governance-v3.3` 收尾；clean-environment 下的迁移、后端认证测试、前端全量测试、前端构建和 3 条固定浏览器主回归均通过，README 与项目计划已改为“认证入口 + 主线收尾”的当前口径
- 验证：
  - `cd backend && uv run pytest tests/unit/test_auth_api.py tests/integration/test_dsl_retry_governance.py -q`，结果 `8 passed`
  - `cd backend && uv run alembic upgrade head`，结果成功
  - `cd backend && uv run pytest tests/integration/test_intervention_regression.py::test_local_single_case_smoke_executes_successfully tests/integration/test_intervention_regression.py::test_local_intervention_flow_rerun_hits_tier_zero tests/integration/test_intervention_regression.py::test_suite_context_rerun_failed_reuses_context_snapshot_after_manual_correction -q`，结果 `3 passed`
  - `cd frontend && npm test -- --run`，结果 `66 passed`
  - `cd frontend && npm run build`，结果成功
- 后续：若继续推进 M2，可在当前认证基础上拆分“角色权限边界 / 账号管理 / 密码重置”与“AI visual 是否默认开启”两条独立主线；本轮不再扩张到报告系统新扩面

## 2026-03-24 23:34

- 任务：修复 governance v3.3 review 提出的 `retry_reason_code` 可伪造与稳定语义未真正保留问题
- 执行动作：在 `backend/app/services/dsl.py` 为 retry 请求新增来源校验，要求 `retry_from_generation_id` 必须属于同一 `actor_user_id`、来源记录 `feedback_status == rejected`，并且 `payload.retry_reason_code` 与来源记录的 `rejection_reason_code` 严格一致；在 `backend/app/api/routes/dsl.py` 为上述权限/状态冲突分别补上 `403 / 409`；在 `backend/app/ai/dsl_generator.py` 中让 `_resolve_base_url()` 在 `context_mismatch` 场景下拒绝 AI 漂移的 `base_url`，并在 `_stabilize_contracts_from_current()` 中补齐 `value_type`、输入 `required`、输出 `source` 的稳定语义回填；在 `backend/tests/unit/test_dsl_validation.py` 增加 4 条定向回归测试，覆盖 actor 越权重试、未 rejected 重试、reason 不匹配重试，以及 `base_url / contract semantics` 稳定化场景
- 结果：客户端无法再伪造 retry provenance 污染治理焦点、重试统计和 prompt 分流；v3.3 宣称保留的稳定 `base_url`、输入 `required`、输出 `source / value_type` 现在会在治理收敛场景中真正落到归一化结果上
- 验证：执行 `cd backend && uv run pytest tests/unit/test_dsl_validation.py -q`，结果 `44 passed`
- 后续：若继续审查治理链路，可优先补一轮 integration 级用例，验证非法 retry 请求不会留下任何失败审计记录

## 2026-03-24 22:58

- 任务：Review 最新提交 `736e044 feat: implement governance v3.3 and gray acceptance summary`
- 执行动作：按 `backend-call-chain-reviewer` 审查 `backend/app/services/dsl.py`、`backend/app/ai/dsl_generator.py`、`backend/app/schemas/settings.py` 与相关单测；补做两组最小复现实验，确认 `retry_reason_code` 可与原始拒绝原因不一致仍被接受，且 v3.3 归一化不会修正错误的 `base_url` / 契约 `source` 语义
- 验证：执行 `uv run pytest backend/tests/unit/test_dsl_validation.py backend/tests/unit/test_ai_settings_api.py`，结果 `44 passed`；最小复现实验已复现上述两项缺陷

## 2026-03-24 22:12

- 任务：将 `governance-v3.3 + AI visual 灰度结论` 变更同步到 GitHub
- 执行动作：复核当前工作区仅包含本轮实现相关代码、测试与文档变更；确认当前分支为 `main`、远端为 `origin`；补记本条执行日志后，准备按单次提交将 `backend/`、`frontend/`、`docs/` 与 `README.md` 的本轮更新一起推送
- 结果：同步前仓库状态、日志与代码变更口径保持一致，可直接执行非交互式 `git add` / `git commit` / `git push`
- 验证：执行 `git status --short`、`git branch --show-current`、`git remote -v`，确认待同步文件集、目标分支与远端配置正常
- 后续：推送完成后，如需继续推进实现，仍按 `governance-v3.3` 主线滚动收敛高频拒绝原因，并保持 AI visual 默认关闭直到样本量达标

## 2026-03-24 22:02

- 任务：review 最新提交 `72dbd19 docs: sync progress summary and log cleanup`
- 执行动作：按 `backend-call-chain-reviewer` 的 diff review 流程审查 `docs/bug-log.md` 与 `docs/execution-log.md`，对照前一提交 `b5c3888 fix: align governance focus selection and audit`、日志规则与当前文档引用，确认本次删除的 `open` 记录是否属于已修问题的重复残留，并检查是否仍残留乱码或重复条目
- 结果：确认该提交属于文档收口与状态对齐，没有改动后端执行链路；被删除的 `BUG-023 governance focus 选择与审计记录存在偏差` 属于已被 `BUG-027` 与 `2026-03-23 22:38` 修复记录覆盖的重复 `open` 条目，本次清理后未发现新的阻断性问题
- 验证：执行 `git show -1 72dbd19`、`git show -1 b5c3888`、`rg -n "BUG-023|BUG-027|BUG-028|2026-03-23 22:38|governance v3.2" docs backend frontend`，并人工核对 `docs/bug-log.md`、`docs/execution-log.md`；未运行自动化测试
- 后续：如需进一步治理日志质量，可单独整理 `docs/execution-log.md` 的时间倒序一致性与历史 `BUG-0xx` 编号重复问题，但它们不是本次提交新引入的问题

## 2026-03-24 21:58

- 任务：实现“AI DSL 治理 v3.3 + AI visual 灰度结论”阶段安排
- 执行动作：在 `backend/app/services/dsl.py` 将治理焦点选择升级为综合参考 `top_rejection_reasons / rejection_reason_by_variant / retry_acceptance_by_reason` 的排序逻辑，按 rejected 数量、retry 未收敛量和受影响 prompt variant 覆盖决定当前前 2 个治理焦点；在 `backend/app/schemas/settings.py`、`frontend/src/types/api.ts`、`frontend/src/pages/AISettingsPage.tsx` 与对应测试中新增“治理焦点选择口径”和“当前治理焦点明细”只读字段；在 `backend/app/ai/dsl_generator.py` 将 `AI_DSL_PROMPT_VERSION` 升级到 `2026-03-24.governance-v3.3`，补强 `context_mismatch / bad_contracts` prompt 规则，并在 `preserve_contracts=true` 且命中 `bad_contracts` 治理时对部分契约做基于当前契约的保守回填与稳定化；同步更新 `README.md`、`docs/project-plan.md`、`docs/AI 自动化测试增强项目规划.md`，新增 `docs/ai-visual-gray-acceptance-2026-03-24.md`
- 结果：AI DSL 治理主线已切到 `governance-v3.3`，治理页现在可以直接看到当前焦点的 rejected / variant / retry / retry accepted 明细；部分坏契约场景下，生成链路会优先复用当前 DSL 中同 `context_key` 的稳定名称、描述和缺失契约，避免局部低质量输出冲掉已知稳定契约；AI visual 本轮已形成独立结论文档，确认在默认关闭前提下 3 条固定浏览器主回归全部通过，但由于本地 `ai_visual_stats` 仍为零样本，当前结论仍是“继续默认关闭，不进入默认开启评估”
- 验证：执行 `uv run pytest backend/tests/unit/test_dsl_validation.py backend/tests/unit/test_ai_settings_api.py -q`，结果 `44 passed`；执行 `cd frontend && npm test -- --run src/pages/AISettingsPage.test.tsx src/pages/CaseWorkbenchPage.test.tsx`，结果 `20 passed`；执行 `cd frontend && npm run build` 成功；执行 `cd backend && uv run pytest tests/integration/test_intervention_regression.py::test_local_single_case_smoke_executes_successfully tests/integration/test_intervention_regression.py::test_local_intervention_flow_rerun_hits_tier_zero tests/integration/test_intervention_regression.py::test_suite_context_rerun_failed_reuses_context_snapshot_after_manual_correction -q`，结果 `3 passed`；执行 `cd backend && uv run pytest tests/unit/test_ai_visual.py tests/unit/test_locator_fallback.py -q`，结果 `35 passed`；本地读取 `get_ai_visual_runtime_stats()` 快照为全零样本
- 后续：继续按 `governance-v3.3` 口径滚动收敛剩余高频拒绝原因；AI visual 方向只继续补手动开启窗口样本，达到 `>= 30 locate_requests` 或连续 3 天观察记录前，不进入默认开启讨论

## 2026-03-23 22:48

- 任务：阅读 docs 并总结最近工作进度，确认项目当前所处阶段
- 执行动作：通读 `docs/execution-log.md`、`docs/bug-log.md`、`docs/project-plan.md`、`docs/AI 自动化测试增强项目规划.md` 与 `docs/ai-visual-gray-acceptance-baseline.md`；按时间线梳理最近迭代的主线、阶段状态与下一里程碑；同时核对日志文档一致性，清理 `execution-log` 与 `bug-log` 中残留的乱码重复条目和过期 `open` 记录，并补记 `BUG-028`
- 结果：确认项目当前主线已从“补功能入口”切换到“AI DSL 数据驱动治理 v3.2 + AI visual 灰度验收 sidecar”；核心平台、DSL 生成闭环、治理观测、混合定位 P0-P4 与三条固定浏览器主回归均已落地；当前 blocker 以文档误报形式残留的问题已清理，项目状态口径重新一致
- 验证：人工核对 `docs/project-plan.md`、`docs/execution-log.md`、`docs/bug-log.md` 与灰度验收文档，未运行自动化测试
- 后续：若继续推进实现，优先围绕 `top_rejection_reasons / rejection_reason_by_variant / retry_acceptance_by_reason` 继续收敛高频拒绝原因，并按灰度验收基线补采 AI visual 观测数据

## 2026-03-23 22:38

- 任务：修复 review 提出的 governance v3.2 两个 major findings
- 执行动作：在 `backend/app/services/dsl.py` 将治理焦点选择逻辑补上 `other` 排除条件，避免其挤占当前治理焦点名额；在 `backend/app/ai/dsl_generator.py` 与 `backend/app/schemas/dsl.py` 将最终生效的 `active_governance_focus_reasons` 纳入 `GenerateDslMeta`，并在 `backend/app/services/dsl.py` 落库时优先持久化生成链路实际生效的焦点列表，失败场景则复用同一套 active reasons 解析逻辑；同步补充 `backend/tests/unit/test_dsl_validation.py` 中对 `other` 排除、retry 追加焦点落库以及新增 `generation_meta` 字段的回归测试
- 结果：治理焦点选择不再被 `other` 抢占；retry 场景下 prompt 实际生效的治理焦点现在会和 `governance_focus_reasons_json`、详情接口保持一致，后续治理统计和排障口径不再失真
- 验证：执行 `uv run pytest backend/tests/unit/test_dsl_validation.py backend/tests/unit/test_ai_settings_api.py -q`，结果 `42 passed`
- 后续：如果继续收到治理相关 review，可优先围绕“焦点选择口径”和“生成链路审计字段一致性”做增量补强，而不需要重开 schema 主线

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

## 2026-03-23 23:20

- 任务：review 最新提交 `f9eba07 feat: implement governance v3.2 and ai visual baseline`
- 执行动作：按 `backend-call-chain-reviewer` 审查后端变更，聚焦 `backend/app/ai/dsl_generator.py`、`backend/app/services/dsl.py`、`backend/app/schemas/settings.py` 以及对应单测，核对治理焦点选择、重试 prompt 生效路径与审计落库一致性
- 验证：静态代码审查，未执行自动化测试

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
## 2026-03-28 18:05

- 任务：审查 git range `09096c8102e2ecade17d27c2e7fc9d4ec0d9fcc2..e07cb6c38752f9608fbb4a5bc3943559019cacfe` 的生产可用性，重点核对 M1 基础认证入口、业务 API 默认登录保护、前端登录态恢复与统一 401 回退
- 执行动作：按 diff review 路径检查后端认证路由、SessionMiddleware 配置、用户迁移、静态 artifacts 暴露面、前端 AuthContext / ProtectedRoute / React Query 缓存边界，并补跑认证相关后端单测、受影响业务 API 回归与前端认证测试
- 结果：确认认证主链路和主要回归测试均可通过，但发现 3 个生产风险点：迁移将所有存量用户回填为固定公开默认密码、会话密钥与 Secure Cookie 默认值不安全、执行证据 artifacts 仍可匿名访问；另外前端在登出或 401 后未清理 React Query 缓存，存在同浏览器多账号切换时的数据残留风险
- 验证：
  - `cd backend && uv run pytest tests/unit/test_auth_api.py`
  - `cd backend && uv run pytest tests/unit/test_cases_api.py tests/unit/test_suites_api.py tests/unit/test_corrections_api.py tests/unit/test_case_executions_api.py tests/unit/test_ai_settings_api.py tests/unit/test_dsl_validation.py tests/integration/test_dsl_retry_governance.py`
  - `cd frontend && npm test -- --run src/auth/AuthContext.test.tsx src/app/AppRouter.test.tsx src/pages/LoginPage.test.tsx src/services/api.test.ts`
- 后续：优先修复 `BUG-032` 中记录的认证与证据暴露问题，再决定是否同步当前改动到 GitHub
