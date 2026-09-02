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
              <Tag color="red">{signal.category}</Tag>
              <Typography.Text type="secondary">
                {signal.action ? `${signal.action}: ` : ""}
                {signal.title}
              </Typography.Text>
            </div>
          ))}
        </Space>
      ) : null}
    </div>
  );
}
