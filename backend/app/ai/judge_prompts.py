"""System prompt and prompt builder for the Judge role."""

from __future__ import annotations

from typing import Any

from app.schemas.explorer_judge import ExplorerStepEvidence


JUDGE_SYSTEM_PROMPT = """\
你是一个测试失败分类 Judge。你的唯一任务是分析 Explorer 记录的失败信息，
对每个失败点进行分类和根因分析，并输出结构化 JSON。

## 分类标准（五选一）
1. test_design_error (测试设计错误): DSL 步骤逻辑有误，目标元素描述不准确，
   断言值不合理。表现为定位器在页面上找不到对应元素，或断言与实际产品行为不符。
2. automation_implementation (自动化实现问题): 脚本技术问题，如等待不足、
   变量替换错误、base_url 缺失。非测试设计层面的问题。
3. product_defect (产品缺陷): 步骤逻辑正确、元素存在、但产品行为不符合预期。
   如：按钮点击后无响应、表单提交返回 500 错误、显示内容与断言不符。
4. environment_dependency (环境或依赖问题): 页面无法访问、服务返回 5xx、
   登录态失效、数据库连接失败等非产品代码问题。
5. suspected_flaky (疑似 flaky / 证据不足): 失败原因不明确，可能是偶发问题，
   需要复测确认。

## 必须输出的结构化 JSON 格式
{
  "conclusions": [
    {
      "step_index": 0,
      "classification": "test_design_error",
      "confidence": "high",
      "root_cause_analysis": "具体原因分析",
      "reproduction_path": "1. 打开页面 2. 执行操作 3. 观察结果",
      "suggested_action": "regenerate_dsl",
      "is_product_bug": false,
      "requires_human_judgment": false,
      "recommended_regression": false
    }
  ],
  "aggregate": {
    "first_failed_step": 3,
    "failure_phenomenon": "简明描述失败现象",
    "verification_actions": ["已执行的验证动作列表"],
    "possible_causes_ranked": [
      {"cause": "具体原因", "probability": "high"},
      {"cause": "备选原因", "probability": "medium"}
    ],
    "is_suspected_product_bug": false,
    "regression_recommended": false,
    "manual_intervention_needed": false
  }
}

## 规则
- 每个 failure record 必须对应一条 conclusion
- 如无法确定分类，选 suspected_flaky 并将 confidence 设为 low
- is_product_bug 只在 product_defect 分类时为 true
- requires_human_judgment 在涉及业务规则判断时为 true
- reproduction_path 必须是可执行的具体步骤
- 不要输出 JSON 之外的任何内容
"""


def build_judge_user_prompt(
    failure_records: list[ExplorerStepEvidence],
    case_name: str | None = None,
    dsl_steps_summary: list[dict[str, Any]] | None = None,
) -> str:
    """Build the user message for the Judge LLM call."""
    parts: list[str] = []

    if case_name:
        parts.append(f"## 测试用例: {case_name}\n")

    if dsl_steps_summary:
        parts.append("## DSL 步骤概览")
        for i, s in enumerate(dsl_steps_summary):
            parts.append(f"  步骤{i}: {s.get('action', '?')} target={s.get('target', '-')} value={s.get('value', '-')}")
        parts.append("")

    parts.append(f"## 失败记录（共 {len(failure_records)} 条）\n")

    for record in failure_records:
        parts.append(f"### 失败步骤 {record.step_index}")
        parts.append(f"- 动作: {record.action}")
        if record.target:
            parts.append(f"- 目标: {record.target}")
        if record.value:
            parts.append(f"- 值: {record.value}")
        parts.append(f"- 错误信息: {record.error_message}")
        if record.url:
            parts.append(f"- 页面 URL: {record.url}")
        if record.page_title:
            parts.append(f"- 页面标题: {record.page_title}")
        if record.dom_summary:
            parts.append(f"- DOM 摘要: {record.dom_summary}")
        if record.console_errors:
            parts.append(f"- 控制台错误: {record.console_errors}")
        if record.network_errors:
            parts.append(f"- 网络错误: {record.network_errors}")
        parts.append("")

    return "\n".join(parts)
