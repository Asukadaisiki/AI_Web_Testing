import { Alert, Progress, Typography } from "antd";

import type { AIPlanningRequirements } from "../../types/api";

type RequirementFieldMeta = {
  key: keyof AIPlanningRequirements;
  label: string;
};

const REQUIREMENT_FIELDS: RequirementFieldMeta[] = [
  { key: "app_under_test", label: "被测系统" },
  { key: "business_goal", label: "业务目标" },
  { key: "entry_url_or_page", label: "入口页面或 URL" },
  { key: "core_user_flow", label: "核心流程" },
  { key: "main_assertions", label: "关键断言" },
  { key: "test_data_or_account", label: "测试数据或账号" },
  { key: "scope_limits", label: "范围限制" },
];

function formatRequirementValue(
  value: AIPlanningRequirements[keyof AIPlanningRequirements],
) {
  if (Array.isArray(value)) {
    return value.length ? value.join("、") : null;
  }
  return value?.trim() ? value : null;
}

export function PlanningRequirementsPanel({
  requirements,
  missingSlots,
}: {
  requirements: AIPlanningRequirements;
  missingSlots: string[];
}) {
  const collectedEntries = REQUIREMENT_FIELDS.flatMap((field) => {
    const value = formatRequirementValue(requirements[field.key]);
    return value ? [{ label: field.label, value }] : [];
  });
  const progressPercent = Math.round(
    (collectedEntries.length / REQUIREMENT_FIELDS.length) * 100,
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12, flex: 1, overflow: "hidden" }}>
      <div style={{ fontWeight: 700, fontSize: 14 }}>Requirements</div>
      <Progress percent={progressPercent} size="small" />
      <Typography.Text type="secondary" style={{ fontSize: 13 }}>
        已收集 {collectedEntries.length} / {REQUIREMENT_FIELDS.length} 项
      </Typography.Text>
      <div style={{ flex: 1, overflowY: "auto" }} className="panel-scroll">
        {collectedEntries.length ? (
          collectedEntries.map((entry) => (
            <div key={entry.label} className="step-item">
              <Typography.Text strong style={{ fontSize: 13 }}>
                {entry.label}
              </Typography.Text>
              <div style={{ fontSize: 13, color: "#555", marginTop: 2 }}>
                {entry.value}
              </div>
            </div>
          ))
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 13 }}>
            当前还没有收集到明确的规划信息。
          </Typography.Text>
        )}
      </div>
      {missingSlots.length ? (
        <Alert
          type="info"
          showIcon
          message="待补充信息"
          description={missingSlots.join("、")}
          style={{ fontSize: 12 }}
        />
      ) : null}
    </div>
  );
}
