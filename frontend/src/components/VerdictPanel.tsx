import React from "react";
import { Tag, Descriptions, Collapse, Alert, Button, Space } from "antd";
import type { ExplorerJudgeVerdict, JudgeConclusion, FailureClassification } from "../types/api";

const CLASSIFICATION_CONFIG: Record<FailureClassification, { label: string; color: string }> = {
  test_design_error: { label: "测试设计错误", color: "orange" },
  automation_implementation: { label: "自动化问题", color: "blue" },
  product_defect: { label: "产品缺陷", color: "red" },
  environment_dependency: { label: "环境问题", color: "default" },
  suspected_flaky: { label: "疑似 Flaky", color: "gold" },
};

const STATUS_ICON: Record<string, string> = {
  all_passed: "✅",
  has_defects: "🐛",
  has_flaky: "⚡",
  environment_blocked: "🚫",
  needs_fix: "🔧",
};

interface VerdictPanelProps {
  verdict: ExplorerJudgeVerdict;
  caseName?: string;
  onUserAction?: (action: string, data?: unknown) => void;
}

const VerdictPanel: React.FC<VerdictPanelProps> = ({ verdict, caseName, onUserAction }) => {
  const { conclusions } = verdict;

  const collapseItems = conclusions.map((c: JudgeConclusion, idx: number) => {
    const config = CLASSIFICATION_CONFIG[c.classification] ?? { label: c.classification, color: "default" };
    return {
      key: String(idx),
      label: (
        <span>
          <Tag color={config.color}>{config.label}</Tag>
          步骤 {c.step_index} — {c.root_cause_analysis.slice(0, 60)}
          {c.root_cause_analysis.length > 60 ? "…" : ""}
        </span>
      ),
      children: (
        <Descriptions column={1} size="small" bordered>
          <Descriptions.Item label="分类">
            <Tag color={config.color}>{config.label}</Tag>
            <Tag>{c.confidence}</Tag>
          </Descriptions.Item>
          <Descriptions.Item label="根因分析">{c.root_cause_analysis}</Descriptions.Item>
          <Descriptions.Item label="复现路径">
            <pre style={{ margin: 0, whiteSpace: "pre-wrap", fontSize: 12 }}>{c.reproduction_path}</pre>
          </Descriptions.Item>
          <Descriptions.Item label="建议动作">{c.suggested_action}</Descriptions.Item>
          {c.is_product_bug && <Descriptions.Item label="产品 Bug"><Tag color="red">是</Tag></Descriptions.Item>}
          {c.requires_human_judgment && <Descriptions.Item label="需人工判断"><Tag color="orange">是</Tag></Descriptions.Item>}
        </Descriptions>
      ),
    };
  });

  return (
    <div style={{ padding: "8px 0" }}>
      {/* Summary card */}
      <Alert
        type={verdict.test_point_status === "all_passed" ? "success" : verdict.is_suspected_product_bug ? "error" : "warning"}
        message={
          <span>
            {STATUS_ICON[verdict.test_point_status] ?? "📋"}{" "}
            {caseName ? `${caseName} — ` : ""}
            {verdict.passed_steps}/{verdict.total_steps} 步通过
            {verdict.failed_steps > 0 && `，${verdict.failed_steps} 步失败`}
          </span>
        }
        description={verdict.failure_phenomenon}
        showIcon={false}
        style={{ marginBottom: 8 }}
      />

      {/* Conclusions */}
      {conclusions.length > 0 && (
        <Collapse items={collapseItems} size="small" style={{ marginBottom: 8 }} />
      )}

      {/* Causes ranked */}
      {verdict.possible_causes_ranked.length > 0 && (
        <div style={{ fontSize: 12, color: "#666", marginBottom: 8 }}>
          <strong>可能原因排序：</strong>
          {verdict.possible_causes_ranked.map((c, i) => (
            <span key={i}>
              {i > 0 && " → "}
              <Tag>{c.probability}</Tag> {c.cause}
            </span>
          ))}
        </div>
      )}

      {/* User actions */}
      {verdict.manual_intervention_needed && onUserAction && (
        <Space style={{ marginTop: 8 }}>
          <Button size="small" type="primary" onClick={() => onUserAction("acknowledge")}>
            已知悉
          </Button>
          <Button size="small" onClick={() => onUserAction("provide_info")}>
            补充信息
          </Button>
        </Space>
      )}
    </div>
  );
};

export default VerdictPanel;
