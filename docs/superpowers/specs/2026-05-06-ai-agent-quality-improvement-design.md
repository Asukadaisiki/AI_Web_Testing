# AI Agent 测试用例质量提升 — 三层修复 + 自动回归循环

## 问题诊断

Session 118, Case 43, Execution 93 — 使用 `test_brand_filter_cart` 作为输入，AI 生成的 DSL 第 2 步即失败。

三个核心根因：

1. **选择器优先级反了** — CSS nth-of-type 得分 0.45（偏高），无障碍树 0.80（偏低），AI 倾向于选择低分脆弱选择器
2. **AI 跳过探索直接生成 DSL** — ReAct loop 没有强制要求在 `generate_plan` 前必须完成页面探索
3. **变量捕获链路缺失** — `${product_a_name}` 等变量在 assert_text 中使用但从未定义（无 input_contract 条目，无 capture_text 步骤）

## L1：选择器评分修复

修改 `_compute_element_stability()` 和 `format_elements_for_prompt()`：

- CSS 含 nth-of-type/nth-child 模式 → 0.10（原 0.30-0.45）
- 可重复文本（同页面多个同文本元素）→ 0.25（原 0.35-0.45）
- aria-label + role 唯一组合 → 0.90（原 0.80）
- 低分元素（<0.30）在格式化输出中标注 `[UNSTABLE]`

## L2：ReAct 流程守卫

在 `test_planning_agent.py` 中：

- `generate_plan` 分支：检查 `_has_explored_pages(tool_calls)`，无探索数据时拒绝生成，注入 system 消息引导 AI 先 explore
- DSL 生成前变量检查：扫描 steps 中的 `${xxx}` 引用，对照 input_contract + capture_text context_key 校验
- 缺失变量 → 在 draft prompt 中追加警告

## L3：自动化回归循环

新建 `scripts/e2e_regression.py`：

1. 读取 `test_brand_filter_cart` 为输入
2. 通过后端 API 创建 session → 发送需求 → 等待 AI 规划 → 生成草案 → 保存执行
3. 统计步骤通过率，记录每轮失败详情
4. 循环条件：成功率 < 80% 且 token < 50M 且轮次 < 10
5. 每轮结束后根据失败模式修复代码

## 中止条件

- AI 生成测试用例步骤通过率 ≥ 80%
- 或 Claude token 消耗 ≥ 5000 万
- 或 E2E 测试完成 10 轮
