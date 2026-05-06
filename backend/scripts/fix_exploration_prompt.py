"""Replace exploration rules in system prompt with flow-driven rules."""
path = r"D:\AutoTestingLearingProject\AI_Web_Testing\backend\app\ai\test_planning_prompts.py"
content = open(path, "r", encoding="utf-8").read()
lines = content.split("\n")

new_rules = [
    "\t【流程驱动探索 — 强制执行】",
    "\t在 generate_plan 之前，必须确保 core_user_flow 中涉及的每一个页面都已被探索。规则如下：",
    "",
    "\t1. 【入口页】系统自动探索。你不需要对入口 URL 调用 explore_page。",
    "",
    "\t2. 【登录页】必须先 capture_page_session 再 explore_flow。",
    "\t   如果 core_user_flow 包含登录步骤，必须先调用 capture_page_session 完成登录（保存登录态），再调用 explore_flow 采集后续页面。",
    "",
    "\t3. 【流程中的每个页面都必须探索】仔细阅读 core_user_flow，列出每一步对应的页面 URL：",
    "\t   - \"点击 Products\" → 必须探索 /products",
    "\t   - \"点击 Polo 品牌\" → 必须探索 /brand_products/Polo",
    "\t   - \"点击 View Cart\" → 必须探索 /view_cart",
    "\t   不要只探索其中几个页面，任何遗漏都会导致后续 DSL 生成失败。",
    "",
    "\t4. 【品牌/筛选/分类页必须探索】如果 core_user_flow 提到筛选、品牌、分类，筛选后的结果页面也必须探索。",
    "",
    "\t5. 【一次性采集】梳理完所有页面后，用一次 explore_flow 调用全部采集。不要分多次，不要遗漏。",
    "",
    "\t6. 【没有页面数据 = 不能生成方案】如果某个页面探索失败，向用户报告具体失败原因，绝不跳过。",
    "",
    "\t7. 【tools 调用优先级】",
    "\t   - 有登录需求 → 先 capture_page_session，再 explore_flow",
    "\t   - 有项目上下文需求 → 先 create_project 或 get_project_info",
    "\t   - 页面采集 → explore_flow（一次性采集所有页面）",
    "\t   - 禁止用 ask_user 替代工具调用",
    "",
    "\t- 入口页面的探索由系统自动完成，你不需要再对入口页面调用 explore_page。",
    "\t- 当已采集到页面元素时，`draft_prompt` 中的 target 必须严格使用元素清单中的实际可见文本、label、placeholder 或 id。",
    "\t- `draft_prompt` 中涉及测试数据的 step value，必须使用 ${context_key} 格式引用 input_contract 变量。",
    "\t- `collected_info` 中的 `core_user_flow` 和 `test_data_or_account` 必须保留用户原始输入中的所有字段细节，不得简化或省略。",
    "\t- 当已收集到 3 项及以上信息时，你必须在 `todo_list` 中列出当前规划进度清单。",
    "\t- 每轮回复都必须更新 `todo_list` 的状态。",
]

# Lines 97-107 (0-indexed 96-106) get replaced
new_content_lines = lines[:96] + new_rules + lines[108:]
open(path, "w", encoding="utf-8").write("\n".join(new_content_lines))

# Verify
print("Replaced lines 97-107. New content:")
vlines = open(path, "r", encoding="utf-8").read().split("\n")
for i in [96, 97, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110]:
    if i < len(vlines):
        print(f"  L{i+1}: {vlines[i][:120]}")
