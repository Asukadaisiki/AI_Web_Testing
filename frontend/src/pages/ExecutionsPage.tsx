import { useQuery } from "@tanstack/react-query";
import { Card, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useState } from "react";
import { Link } from "react-router-dom";

import { EmptyBlock, ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { getExecutions } from "../services/api";
import type { ExecutionStatus, StoredCaseExecutionSummary } from "../types/api";

const STATUS_OPTIONS: { label: string; value: ExecutionStatus | "all" }[] = [
  { label: "全部状态", value: "all" },
  { label: "通过", value: "passed" },
  { label: "失败", value: "failed" },
  { label: "运行中", value: "running" },
];

function renderStatus(status: ExecutionStatus) {
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

const columns: ColumnsType<StoredCaseExecutionSummary> = [
  {
    title: "执行 ID",
    dataIndex: "id",
    key: "id",
    width: 100,
    render: (value: number) => <Typography.Text code>#{value}</Typography.Text>,
  },
  {
    title: "用例",
    dataIndex: "case_name",
    key: "case_name",
    render: (value: string, record) => <Link to={`/executions/${record.id}`}>{value}</Link>,
  },
  {
    title: "状态",
    dataIndex: "status",
    key: "status",
    width: 120,
    render: renderStatus,
  },
  {
    title: "触发人",
    dataIndex: "triggered_by",
    key: "triggered_by",
    width: 90,
  },
  {
    title: "开始时间",
    dataIndex: "started_at",
    key: "started_at",
    width: 200,
    render: (value: string) => new Date(value).toLocaleString(),
  },
  {
    title: "错误摘要",
    dataIndex: "error_message",
    key: "error_message",
    render: (value: string | null) => value || "-",
  },
];

export function ExecutionsPage() {
  const [status, setStatus] = useState<ExecutionStatus | "all">("all");
  const query = useQuery({
    queryKey: ["executions", status],
    queryFn: () =>
      getExecutions({
        project_id: 1,
        status: status === "all" ? undefined : status,
        limit: 20,
      }),
  });

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">执行报告</h1>
        <p className="page-subtitle">按执行记录查看状态、失败摘要，并跳转到步骤级报告详情。</p>
      </div>
      <Card>
        <Space direction="vertical" size="large" style={{ width: "100%" }}>
          <Space wrap>
            <Typography.Text type="secondary">状态筛选</Typography.Text>
            <Select
              value={status}
              options={STATUS_OPTIONS}
              onChange={(value) => setStatus(value)}
              style={{ width: 160 }}
            />
          </Space>
          {query.isLoading ? <LoadingBlock /> : null}
          {query.isError ? <ErrorBlock message={query.error.message} /> : null}
          {!query.isLoading && !query.isError && !query.data?.length ? (
            <EmptyBlock description="当前筛选条件下没有执行记录。" />
          ) : null}
          {query.data?.length ? (
            <Table rowKey="id" pagination={false} columns={columns} dataSource={query.data} />
          ) : null}
        </Space>
      </Card>
    </>
  );
}
