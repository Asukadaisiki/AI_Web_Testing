---
name: e2e-brand-filter-cart
description: E2E 测试 skill — 使用 test_brand_filter_cart 需求文件自动化 AI 规划流程，验证 DSL 质量并修复发现的 bug。触发词: "品牌筛选测试", "购物车测试", "brand filter cart", "e2e brand".
---

# E2E 品牌筛选+购物车测试

## 概述

此 skill 自动化执行 `test_brand_filter_cart` 的 AI 规划流程，验证生成的 DSL 质量，发现并修复过程中出现的 bug。

**核心流程**: 读取需求文件 → 创建会话 → 发送需求 → AI 生成方案 → 生成 DSL 草案 → 验证质量 → 修复问题

## 前置条件

```bash
# 启动后端
cd backend && uv run backend-dev
# 验证: curl http://127.0.0.1:8000/api/v1/health
```

## 执行步骤

### Step 1: 运行 E2E 测试

```bash
cd backend && uv run pytest tests/e2e/test_e2e_brand_filter_cart.py -v -s
```

测试脚本会自动:
1. 创建 AI 规划会话
2. 创建项目并关联
3. 发送 `test_brand_filter_cart` 文件内容作为消息
4. 检查 AI 响应获取 scenarios
5. 生成 DSL 草案
6. 验证 DSL 质量并输出详细信息

### Step 2: 分析测试结果

查看输出中的关键信息:
- `next_action` — 应为 `select_scenarios`
- `scenarios` — AI 生成的测试场景列表
- `drafts` — 生成的 DSL 草案
- DSL 详细内容 — 每个步骤的 action/target/value
- 质量问题列表

### Step 3: 修复发现的 bug

根据问题类型分类处理:

| 问题类型 | 表现 | 修复方向 |
|---------|------|---------|
| AI 未返回方案 | `next_action` 始终是 `ask_followup` | 检查 prompt 模板，确保需求信息被正确解析 |
| DSL 步骤缺失 | steps 数量不足或缺少关键 action | 检查 dsl_generator 的 prompt 和 normalization 逻辑 |
| target 匹配失败 | warnings 中有 "未找到匹配" | 检查 page_explorer 的元素采集逻辑 |
| 候选定位器质量差 | candidates 为空或 pre_score 过低 | 检查 locator_preflight 的评分逻辑 |
| base_url 缺失 | DSL 无 base_url | 检查 dsl_generator 的 base_url 推断逻辑 |

### Step 4: 验证修复

修复 bug 后重新运行测试:
```bash
cd backend && uv run pytest tests/e2e/test_e2e_brand_filter_cart.py -v -s
```

重复 Step 2-4 直到 DSL 质量过关。

## 质量检查清单

DSL 质量过关的标准:
- [ ] AI 返回 `next_action: "select_scenarios"` 且包含 scenarios
- [ ] 所有草案 `status` 不是 `failed`
- [ ] 每个草案的 `dsl_case.steps` 非空
- [ ] 步骤数 >= 5
- [ ] 包含 `goto` 步骤 (入口 URL)
- [ ] 包含 `click` 步骤 (交互操作)
- [ ] 包含 `assert_text` 或 `assert_url_contains` (断言)
- [ ] `base_url` 非空且指向正确地址
- [ ] 无严重 warnings ("未找到"、"失败"、"错误")

## 关键文件

| 文件 | 作用 |
|------|------|
| `test_brand_filter_cart` | 测试需求文件 (根目录) |
| `backend/tests/e2e/test_e2e_brand_filter_cart.py` | E2E 测试脚本 |
| `backend/app/ai/dsl_generator.py` | DSL 生成逻辑 |
| `backend/app/ai/page_explorer.py` | 页面元素采集 |
| `backend/app/ai/test_planning_agent.py` | AI 规划 Agent |
| `backend/app/schemas/dsl.py` | DSL schema 定义 |
| `backend/app/schemas/ai_planning.py` | 规划 API schema |

## 注意事项

- **不要使用 `[FORCE_GENERATE]`** — 会导致 DSL 生成时跳过工具调用，产生低质量 DSL
- AI 规划首次消息可能耗时 30-120 秒 (ReAct 多轮 LLM 调用)
- 测试脚本 timeout 设为 300 秒，足够覆盖大多数场景
- 如果后端重启，之前创建的会话会丢失，需要重新运行测试
