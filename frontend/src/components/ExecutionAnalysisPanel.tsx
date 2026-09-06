import { Alert, Space, Tag, Typography } from "antd";

import type { ExecutionAnalysis } from "../types/api";

const CONCLUSION_LABEL: Record<ExecutionAnalysis["conclusion"], string> = {
  all_passed: "全部通过",
  partial: "部分通过",
  all_failed: "全部失败",
  cancelled: "已取消",
};

const ACTION_LABEL: Record<ExecutionAnalysis["recommended_action"], string> = {
  targeted_retest: "定向复测",
  regression: "回归测试",
  manual: "人工处理",
  done: "无需处理",
};

export function ExecutionAnalysisPanel({
  analysis,
  compact = false,
}: {
  analysis: ExecutionAnalysis;
  compact?: boolean;
}) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: compact ? 6 : 10 }}>
      <Space wrap size={6}>
        <Tag color={analysis.conclusion === "all_passed" ? "success" : "warning"}>
          {CONCLUSION_LABEL[analysis.conclusion]}
        </Tag>
        <Tag>{analysis.source === "ai" ? "AI 分析" : "规则分析"}</Tag>
        <Tag color="blue">{ACTION_LABEL[analysis.recommended_action]}</Tag>
      </Space>
      <Typography.Text>{analysis.summary}</Typography.Text>
      {analysis.suspected_root_cause ? (
        <Alert
          type={analysis.conclusion === "all_passed" ? "success" : "warning"}
          showIcon
          message="根因判断"
          description={analysis.suspected_root_cause}
        />
      ) : null}
      {analysis.recommended_scope ? (
        <Typography.Text type="secondary">
          建议范围：{analysis.recommended_scope}
        </Typography.Text>
      ) : null}
      {analysis.failure_signals.length > 0 ? (
        <Space direction="vertical" size={4} style={{ width: "100%" }}>
          {analysis.failure_signals.map((signal) => (
            <div key={signal.fingerprint}>
              <Space wrap size={4}>
                <Tag color="red">{signal.category}</Tag>
                {signal.schema_version === "failure.signal.v2" ? (
                  <>
                    <Tag>{signal.stage}</Tag>
                    <Tag>{signal.code}</Tag>
                    <Tag color={signal.retryable ? "blue" : "default"}>
                      {signal.retryable ? "可重试" : "不可重试"}
                    </Tag>
                    {signal.side_effect_committed === true ? (
                      <Tag color="orange">副作用已提交</Tag>
                    ) : signal.side_effect_committed === null ? (
                      <Tag color="orange">副作用未知</Tag>
                    ) : null}
                  </>
                ) : null}
                <Typography.Text type="secondary">
                  {signal.action ? `${signal.action}: ` : ""}
                  {signal.title}
                </Typography.Text>
              </Space>
              {signal.schema_version === "failure.signal.v2" ? (
                <div>
                  <Typography.Text type="secondary">
                    来源：执行 #{signal.source_reference.execution_id}
                    {signal.source_reference.json_pointer}
                  </Typography.Text>
                </div>
              ) : null}
            </div>
          ))}
        </Space>
      ) : null}
    </div>
  );
}
