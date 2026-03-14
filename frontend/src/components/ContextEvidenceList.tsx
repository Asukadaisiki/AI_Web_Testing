import type { ReactNode } from "react";
import { Space, Typography } from "antd";

import type { ContextVariableReadEvidence, ContextVariableWriteEvidence } from "../types/api";

type ContextEvidenceListProps = {
  emptyContent?: ReactNode;
};

export function ContextReadEvidenceList({
  reads = [],
  emptyContent = <Typography.Text type="secondary">无</Typography.Text>,
}: ContextEvidenceListProps & {
  reads?: ContextVariableReadEvidence[];
}) {
  if (!reads.length) {
    return <>{emptyContent}</>;
  }

  return (
    <Space direction="vertical" size={2}>
      {reads.map((item) => (
        <Typography.Text key={`${item.context_key}-${item.name}`}>
          {item.context_key} / {item.value_type} / {item.resolved ? "已解析" : "未解析"}
          {item.source_suite_run_id !== null && item.source_suite_run_id !== undefined
            ? ` / 来源批次 #${item.source_suite_run_id}`
            : ""}
          {item.error_message ? ` / ${item.error_message}` : ""}
        </Typography.Text>
      ))}
    </Space>
  );
}

export function ContextWriteEvidenceList({
  writes = [],
  emptyContent = <Typography.Text type="secondary">无</Typography.Text>,
}: ContextEvidenceListProps & {
  writes?: ContextVariableWriteEvidence[];
}) {
  if (!writes.length) {
    return <>{emptyContent}</>;
  }

  return (
    <Space direction="vertical" size={2}>
      {writes.map((item) => (
        <Typography.Text key={`${item.context_key}-${item.name}`}>
          {item.context_key} / {item.value_type} / {item.source || "-"} / {item.status}
          {item.error_message ? ` / ${item.error_message}` : ""}
        </Typography.Text>
      ))}
    </Space>
  );
}
