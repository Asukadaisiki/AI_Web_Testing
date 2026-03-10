import { Tag } from "antd";

import type {
  ExecutionStatus,
  FailureCategory,
  StoredCaseExecutionSummary,
} from "../types/api";

export const FAILURE_CATEGORY_LABELS: Record<FailureCategory, string> = {
  configuration: "配置",
  locator: "定位",
  assertion: "断言",
  navigation: "导航",
  network: "网络",
  runner: "运行器",
};

export function renderExecutionStatus(status: ExecutionStatus) {
  const colorMap: Record<ExecutionStatus, string> = {
    passed: "success",
    failed: "error",
    running: "processing",
  };
  const labelMap: Record<ExecutionStatus, string> = {
    passed: "通过",
    failed: "失败",
    running: "运行中",
  };
  return (
    <Tag className="status-tag" color={colorMap[status]}>
      {labelMap[status]}
    </Tag>
  );
}

export function formatDuration(durationMs?: number | null) {
  if (durationMs === null || durationMs === undefined) {
    return "-";
  }
  return `${durationMs} ms`;
}

export function formatPassRate(passRate: number) {
  return `${(passRate * 100).toFixed(1)}%`;
}

export function truncateText(value: string | null, maxLength = 72) {
  if (!value) {
    return "-";
  }
  if (value.length <= maxLength) {
    return value;
  }
  return `${value.slice(0, maxLength - 1)}…`;
}

export function buildExecutionLink(record: Pick<StoredCaseExecutionSummary, "id" | "failed_step_index">) {
  if (record.failed_step_index === null || record.failed_step_index === undefined) {
    return `/executions/${record.id}`;
  }
  return `/executions/${record.id}#step-${record.failed_step_index + 1}`;
}
