# AI 测试规划主路径 v2 — A11y 树驱动的精简管线

**日期**：2026-05-14
**前置 commit**：`0db5a6e refactor: cleanup phase 1 — remove dormant AI planning branches`（已完成 -1498 行清理）

## 一、背景

清理阶段 1 完成后，主路径仍残留 6 类痛点：元素过多 / 思考时间过长 / 信息压缩遗漏 / DSL 信息不足 / 执行器结果差 / 草案质量不稳定。逐文件取证表明：当前 ReAct 路径在每一步都"采集冗余 + 信息有损搬运 + 旁路质量门"，导致设计的自愈、评分、缓存等基础设施全部空转。

本设计在保留 ReAct 形态的前提下，把感知层换成无障碍树（A11y）、把数据传递改成结构化 dict 端到端，并接通缓存与 preflight 闭环。目标是把单轮"对话→草案"时间从 ~10 分钟压到 1-2 分钟，并显著提升 step.candidates 命中率。

## 二、设计目标

**主目标**：
1. 单轮"对话→DSL 草案"墙钟时间 ≤ 2 分钟
2. 草案中 step.candidates 命中率 ≥ 95%（当前估算 < 10%）
3. 探索阶段单页耗时 ≤ 1 秒（当前 6-8 秒）
4. 主路径用户可见行为零变化（API 兼容、frontend 不动）

**非目标**：
- 不切换 DSL target 字段为结构化 element_ref（保留文本 target；后续单独议题）
- 不重新规划 Explorer-Judge 缺陷探索流（独立保留）
- 不引入 anti-pattern few-shot 注入（写入路径仍保留作审计数据，但生成时不读）
- 不改前端 UI 与 API 契约

## 三、锁定的核心决策

经 brainstorm 7 轮问答确认：

| # | 决策项 | 选择 |
|---|---|---|
| 1 | DSL target 字段类型 | 保留为自由文本（不切换为 element_id） |
| 2 | `generate_case_draft` 单调用路径 | **删除**，segmented 成为唯一 DSL 生成入口 |
| 3 | Explorer-Judge 缺陷探索流 | **保留**，独立旁路 |
| 4 | Flow shape | 保留 ReAct，但 `safety_cap` 从 30 降到 5，输出 schema 从 30+ 字段降到 3-5 字段 |
| 5 | 元素来源 | **A11y 树为主，DOM 不作 fallback**（避免双扫描数据爆炸） |
| 6 | 数据传递 | **结构化 dict 端到端**（无 NL→string→dict 反序列化） |
| 7 | 草案质量门 | **Preflight + 单段重生**（不注入 anti-pattern） |

## 四、端到端管线（8 阶段）

```
[1] Session 创建 + 默认项目自动绑定
  ↓
[2] ReAct lite (safety_cap=5, schema = thought/action/action_input/missing_slots)
  ↓ action=call_tool & tool=explore_*
[3] 探索:展开折叠组件 → A11y 快照 → 视口过滤 → 输出 list[a11y_node]
  ↓
[4] Cache:AIPlanningToolResult 读写 (key=(session_id, url, viewport, storage_hash), TTL=4h)
  ↓ 回到 [2] 直到 action=generate_plan
[5] DSL 分段生成:从 cache 读 dict → per page_state 并行 _call_dsl_flash_llm
  ↓
[6] Preflight:校验每个 step.target ∈ a11y_node.name 集合 → 缺失则单段重生 (≤1 次)
  ↓
[7] 执行 (用户触发):候选驱动 + 简化 fallback
  ↓
[8] 自愈写入:LocatorCorrection + LocatorCorrectionEvent (读路径保持现状)
```

## 五、阶段详细设计

### Stage 1 — Session 创建 + 默认项目绑定

**修复设计 bug**：当前必须 `create_project` 才能 `explore_*`，AI 每次浪费 1-2 轮 ReAct。

**改动**：
- `services/ai_planning.create_session` 时若 `payload.project_id is None`，自动创建一个 `default-{session_id}` 临时项目并绑定
- 用户后续保存 case 时可重命名项目或迁移到已有项目
- `planning_tools._PROJECT_REQUIRED_TOOLS` 集合保留（兜底校验），但实际 session 已有 project_id

**文件**：`backend/app/services/ai_planning.py`、`backend/app/models/project.py`（可能加 `is_default` flag）

### Stage 2 — ReAct lite

**Schema 瘦身**：

```json
{
  "thought": "string, 你下一步打算做什么",
  "action": "ask_user | call_tool | generate_plan",
  "action_input": {
    // 当 action=ask_user
    "message": "string",
    // 当 action=call_tool
    "tool": "string",
    "params": {},
    // 当 action=generate_plan
    "scenarios": [...]
  },
  "missing_slots": ["app_under_test", "core_user_flow", ...]  // 仅 ask_user 时填
}
```

**约束**：
- 删除 `collected_info` / `test_context` / `todo_list` 字段（共占输出 token ~60%）
- `safety_cap` 从 30 降到 5（`config.py:ai_planning_max_react_safety_cap` 默认值改 5）
- `ai_planning_max_react_rounds` 配置项删除（已不被读取）
- 删除 "30 字段强制规则" 系统提示词（`test_planning_prompts.py:12-186`），改为 ≤ 50 行精简版

**预期效果**：每轮 LLM completion 从 ~1000 token 降到 ~300 token，墙钟 5-15s → 2-5s。

**文件**：`backend/app/ai/test_planning_agent.py`、`backend/app/ai/test_planning_prompts.py`、`backend/app/core/config.py`

### Stage 3 — A11y 探索 + 主动展开

**入口**：保留 `explore_page` / `explore_flow` 工具名（API 不变），实现替换。

**流程**：
1. **导航 + 等待**：`page.goto(url) + wait_for_load_state('networkidle', timeout=30s)`（timeout 从 60s 降）
2. **主动展开**：扫 DOM 查询 `[aria-expanded="false"]` 与 `details:not([open])`，逐个 `click()`，等待 200ms（最多 10 个，超出忽略）
3. **A11y 快照**：CDP `Accessibility.getFullAXTree`
4. **过滤**：
   - 跳过 `ignored=true` 节点
   - 仅保留 USEFUL_A11Y_ROLES（24 种：button/link/textbox/checkbox/radio/menuitem/combobox/option/tab/heading/image/navigation/main/banner/form/search/region/dialog/alert/menu/menubar/tablist/list/listitem）
   - 视口过滤：节点 `boundingBox` 完全在视口外 → 丢弃（buttom-of-page 元素如必要由后续 step 探索触发）
5. **输出 schema**：每个节点
   ```python
   {
     "node_id": "e{nodeId}",        # CDP 提供的稳定 ID，用于 ref / 内部去重
     "role": "button",
     "name": "Login",
     "value": "...",                  # 仅 textbox / combobox 有值
     "level": 3,                      # heading 有
     "parent_id": "e42",              # 用于父子链上下文
     "bbox": {"x":..,"y":..,"w":..,"h":..},
     "focusable": true,
     "disabled": false,
     "page_state": "S0",              # explore_flow 多页时用
   }
   ```

**删除**：`collect_interactable_elements` 的 DOM 全量抽取逻辑（保留 `_verify_locators_on_page` 仅作 explorer_runner 用）；`EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT`；`format_elements_for_prompt` 的字符串拼接（保留为"渲染给 LLM 用"的工具函数）；`MAX_PROMPT_ELEMENTS_CHARS` / `explore_max_elements` 截断（A11y 天然在 50-300 之间）。

**Browser 复用**：`BrowserSessionManager` 保留不动。

**文件**：`backend/app/ai/page_explorer.py`（大改）

### Stage 4 — Cache

**Key**：`(planning_session_id, normalized_url, viewport_w, viewport_h, storage_state_hash)`
- `normalized_url`：剥离 query string 中的非业务参数（utm_*、ref、_t 等）
- `storage_state_hash`：md5 of `storage_state.json` 文件内容（首次为空字符串）

**TTL**：4 小时

**读路径（新增）**：
- `_handle_explore_page` 入口先查 `AIPlanningToolResult`，命中且 `now - created_at < 4h` → 直接返回 `raw_result_json`
- 命中时给 ReAct 注入一条 system message：`"工具 explore_page 命中缓存（创建于 X 分钟前），未重新执行 Playwright"`

**写路径**：未命中 → 跑 Stage 3 → 写入 `raw_result_json`

**清理**：旧的 `_persist_tool_result` 写入逻辑保留，新增 `_lookup_tool_cache(key) → dict | None`。

**文件**：`backend/app/services/ai_planning.py`、`backend/app/ai/planning_tools.py`

### Stage 5 — DSL 分段生成

**保留**：`generate_segmented_case_draft` 主结构（`dsl_generator.py:2259`）。

**改动**：
1. **输入**：从 `services/ai_planning.generate_planning_drafts` 读 `AIPlanningToolResult.raw_result_json` 拿到 `list[a11y_node]`，而不是 `_parse_page_elements_text(string)`
2. **`_build_segment_prompt` 输入**：直接接受 `list[a11y_node]`（dict），不再接受字符串
3. **Prompt 内的 element 渲染**：每个节点一行：
   ```
   - role=button name="Login" node_id=e3 [focusable]
   - role=link name="Products" node_id=e8 parent=e7
   - role=heading level=4 name="Blue Top" node_id=e42
   ```
4. **强约束**：prompt 中明确 "target 字段必须从上述节点的 name 中选，不能编造"

**删除**：
- `generate_case_draft` 整套（决策 2）
- `_normalize_generated_case` 中 governance 分支
- `_verify_field_coverage` / `_verify_navigation_completeness` / `_auto_inject_verification_steps`（被 Preflight 替代）
- `REJECTION_REASON_STRATEGIES` 中 governance_focus_reasons 部分（保留 retry_reason_code 用于 Stage 6）
- 单独的 `/api/v1/dsl/generate` 路由内部走 segmented

**保留**：`_call_dsl_flash_llm` + 并行 ThreadPoolExecutor 结构

**文件**：`backend/app/ai/dsl_generator.py`（大改）、`backend/app/services/dsl.py`、`backend/app/services/ai_planning.py`

### Stage 6 — Preflight + 单段重生

**改动 `locator_preflight.apply_preflight_to_dsl`**：
1. **输入**：`DSLCase` + `list[a11y_node]`（不再 `list[parsed_element_dict]`）
2. **匹配**：每个 step.target 与 a11y_node.name 做精确 + 子串匹配（保留现有 `_text_matches_target`）
3. **写候选**：匹配到的 a11y_node 转成 `LocatorCandidateEvidence`：
   ```python
   {
     "strategy": "role",
     "selector": a11y_node.role,
     "semantic_value": a11y_node.name,
     "pre_score": 0.90,  # role 在 DOM_FEATURE_PRIORS 中 0.90
     "pre_features": {"verified": True, "source_strategy": "a11y"}
   }
   ```
4. **重生触发**：任何 step `match_count==0` → 调用 `_regen_segment(scenario_key, page_state, missing_targets, a11y_nodes)`
   - prompt 附 `"target [X, Y] 在节点列表中找不到，请从以下节点重新选择: [节点列表]"`
   - 调 `_call_dsl_flash_llm`，最多 1 次重试
   - 重试结果再跑一次 preflight，若仍 0 匹配 → 保留 step 但标 `locator_confidence="low"` 让用户感知

**新增函数**：`dsl_generator.py:_regen_segment(...) → list[step_dict]`

**文件**：`backend/app/ai/locator_preflight.py`、`backend/app/ai/dsl_generator.py`、`backend/app/services/ai_planning.py`

### Stage 7 — 执行

**主路径**：`_execute_step_with_candidates`（playwright_runner.py:285）已有，无需改动。

**简化项**：
- step.candidates 由 Preflight 保证非空 → `_has_candidates(step)` 几乎总是 True
- candidate.strategy="role" → 直接 `page.get_by_role(role, name=semantic_value, exact=True)`（Playwright 最稳定 API）

**fallback 链**（当 candidates 全失败）：
- Tier 0: corrections
- Tier 1: 现有 12 个 semantic builders（保留作兜底，极少触发）
- Tier 2: `ai_visual.locate_element_by_vision`
- Tier 3: `InterventionNeededError`

**不变**：click_preprocessor、postcondition_verifier、runtime_scorer。

### Stage 8 — 自愈写入

**写入**：保留
- 执行失败 → 可能引发用户人工修正 → `LocatorCorrection`
- pre_exec_review 已删（不再写入此源头），其他写入位置保留作审计

**读路径**：
- Tier 0 `corrections.find_active_correction` 保留（不变）
- `retrieve_relevant_anti_patterns` 仍存在但**不接入**新 Stage 5 流程（按决策 7）

## 六、数据契约（核心数据结构）

### `a11y_node`（贯穿 Stage 3-6）

```python
class A11yNode(TypedDict):
    node_id: str          # "e{cdp_id}", 单次 snapshot 内稳定
    role: str             # 标准 ARIA role
    name: str             # accessible name
    value: str | None     # textbox/combobox 当前值
    level: int | None     # heading 层级
    parent_id: str | None
    bbox: dict | None     # {x,y,w,h}
    focusable: bool
    disabled: bool
    page_state: str       # explore_flow 时填
```

### `AIPlanningToolResult.raw_result_json`

```python
{
    "url": "https://...",
    "viewport": {"width": 1280, "height": 720},
    "captured_at": "2026-05-14T12:00:00",
    "a11y_nodes": [A11yNode, ...],
    "expanded_elements": ["aria-expanded=false x 3", "details x 2"],
    "warnings": []
}
```

## 七、删除清单（按文件汇总）

| 文件 | 删除内容 | 原因 |
|---|---|---|
| `services/ai_planning.py` | `_parse_page_elements_text` / `_parse_page_elements_by_state` | 数据流改 dict，反序列化死代码 |
| `ai/test_planning_agent.py` | 30 字段 schema 相关解析逻辑（保留 thought/action/action_input） | Schema 瘦身 |
| `ai/test_planning_prompts.py` | 100+ 行硬规则 | 重写为精简版 |
| `ai/page_explorer.py` | `EXTRACT_INTERACTABLE_ELEMENTS_SCRIPT` / `MAX_PROMPT_ELEMENTS_CHARS` 截断逻辑 | A11y 替代 |
| `ai/dsl_generator.py` | `generate_case_draft` / `_normalize_generated_case` 中 governance 分支 / `_verify_field_coverage` / `_verify_navigation_completeness` / `_auto_inject_verification_steps` / `REJECTION_REASON_STRATEGIES`（保留 retry_reason_code）| 决策 2 + Preflight 替代 |
| `runners/locator_confidence.py` | （现仅 explorer_runner 用，可独立删除一次） | 决策 5 已删主 runner 引用 |
| `core/config.py` | `ai_planning_max_react_rounds` / `explore_max_elements` | 不再读取（`ai_planning_subagent_*` 已在本轮清理中删除） |
| `locators/__init__.py` 等 | 与上述删除项相关的 import / re-export | 跟随 |

预估再删除 ~800-1200 行（在阶段 2/3 落地时分批 commit）。

## 八、测试策略

### 8.1 单元测试

新增：
- `tests/unit/test_a11y_explorer.py`：a11y 节点抽取 + 过滤 + 视口过滤的单测，用 fake `Accessibility.getFullAXTree` 响应
- `tests/unit/test_tool_result_cache.py`：cache 命中/未命中/TTL 过期的单测
- `tests/unit/test_preflight_regen.py`：preflight 触发单段重生 + 1 次重试上限

修改：
- `tests/unit/test_dsl_validation.py`：增加 segmented 路径接受 a11y_nodes 输入的 case
- `tests/unit/test_pre_scorer.py` / `test_runtime_scorer.py`：保持，验证 candidates 流转

### 8.2 集成测试

新增：
- `tests/integration/test_main_path_v2_e2e.py`：用 `test_brand_filter_cart` 输入，端到端校验：
  - 单轮"对话→草案"墙钟 ≤ 120s
  - 草案 step.candidates 命中率 ≥ 95%
  - cache 命中后续轮次墙钟 ≤ 30s

### 8.3 回归测试

- `scripts/e2e_regression.py` 已有，继续作 nightly 跑
- 新增基线数据：保存本次 brainstorm 期间 `scripts/a11y_experiment.py` 跑出的 token / 耗时数据作为对比基线

## 九、未明确的延后决策（design 默认值已给）

| # | 项 | 默认值 | 触发重新决策的条件 |
|---|---|---|---|
| 1 | 展开折叠的扫描范围 | `aria-expanded="false"` + `<details>` | 实际站点漏召率 > 20% |
| 2 | 视口外节点是否丢 | 完全丢 | 测试用例需要 footer 元素时再开 |
| 3 | A11y `generic` 节点 | 直接丢 | 大量自定义控件场景出现时考虑二次启发 |
| 4 | DSL target 是否切到 element_id | 保留文本（决策 1） | candidates 命中率仍 < 80% 时重议 |
| 5 | anti-pattern 接入 Stage 5 | 不接入（决策 7） | 草案重生率 > 30% 时重议 |

## 十、迁移与回滚

**迁移**：
- 改动量大，拟分 3 个 PR：
  1. Stage 1-3（A11y 探索 + 缓存）—— 旧 DOM 抽取保留作 feature flag fallback
  2. Stage 4-6（数据流 dict 化 + Preflight 重生）—— 一并切换 segmented 输入
  3. Stage 7-8（清理 + ReAct schema 瘦身）—— 最后一波净删除
- 每个 PR 自带单测，验证 526+ 测试不退化

**回滚**：
- 每个 PR 独立 git revert
- 阶段 1 完成后已有干净 baseline commit `0db5a6e`，可整体回到清理后状态

---

## 实施顺序

```
PR-1 (Stage 1-3): default project + A11y explorer + cache lookup
   ↓
PR-2 (Stage 4-6): dict end-to-end + segmented input switch + Preflight regen
   ↓
PR-3 (Stage 7-8): ReAct schema slim + delete deprecated code
```

每个 PR 独立可验证、可回滚。详细任务级实施计划见 `docs/superpowers/plans/`（下一步用 writing-plans 技能生成）。
