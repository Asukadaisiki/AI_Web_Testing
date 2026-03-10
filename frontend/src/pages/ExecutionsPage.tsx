import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Button,
  Card,
  Image,
  Pagination,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { Link } from "react-router-dom";

import { EmptyBlock, ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { getCases, getExecutions } from "../services/api";
import type { ExecutionStatus, StoredCaseExecutionSummary, StoredCaseSummary } from "../types/api";

const PAGE_SIZE = 10;

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

function truncateText(value: string | null, maxLength = 72) {
  if (!value) {
    return "-";
  }
  if (value.length <= maxLength) {
    return value;
  }
  return `${value.slice(0, maxLength - 1)}…`;
}

function formatDuration(durationMs?: number | null) {
  if (durationMs === null || durationMs === undefined) {
    return "-";
  }
  return `${durationMs} ms`;
}

function buildExecutionLink(record: StoredCaseExecutionSummary) {
  if (record.failed_step_index === null || record.failed_step_index === undefined) {
    return `/executions/${record.id}`;
  }
  return `/executions/${record.id}#step-${record.failed_step_index + 1}`;
}

function buildColumns(): ColumnsType<StoredCaseExecutionSummary> {
  return [
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
      render: (value: string, record) => (
        <Space direction="vertical" size={2}>
          <Link to={buildExecutionLink(record)}>{value}</Link>
          {record.failed_step_index !== null && record.failed_step_index !== undefined ? (
            <Typography.Text type="secondary">
              失败步骤：Step {record.failed_step_index + 1}
            </Typography.Text>
          ) : (
            <Typography.Text type="secondary">共 {record.total_steps} 步</Typography.Text>
          )}
        </Space>
      ),
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      width: 120,
      render: renderStatus,
    },
    {
      title: "耗时",
      dataIndex: "duration_ms",
      key: "duration_ms",
      width: 120,
      render: formatDuration,
    },
    {
      title: "最近截图",
      dataIndex: "latest_screenshot_url",
      key: "latest_screenshot_url",
      width: 140,
      render: (value: string | null | undefined, record) =>
        value ? (
          <Link to={buildExecutionLink(record)}>
            <Image
              src={value}
              alt={`execution-${record.id}-latest`}
              preview={false}
              width={96}
              height={60}
              className="execution-thumbnail"
            />
          </Link>
        ) : (
          <Typography.Text type="secondary">无截图</Typography.Text>
        ),
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
      render: (value: string | null) => (
        <Typography.Text title={value || undefined}>{truncateText(value)}</Typography.Text>
      ),
    },
  ];
}

export function ExecutionsPage() {
  const [status, setStatus] = useState<ExecutionStatus | "all">("all");
  const [caseId, setCaseId] = useState<number | undefined>(undefined);
  const [page, setPage] = useState(1);
  const columns = useMemo(() => buildColumns(), []);
  const casesQuery = useQuery({
    queryKey: ["cases"],
    queryFn: getCases,
  });
  const executionsQuery = useQuery({
    queryKey: ["executions", status, caseId, page],
    queryFn: () =>
      getExecutions({
        project_id: 1,
        case_id: caseId,
        status: status === "all" ? undefined : status,
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
      }),
  });

  useEffect(() => {
    setPage(1);
  }, [status, caseId]);

  const caseOptions = useMemo(
    () => [
      { label: "全部用例", value: 0 },
      ...((casesQuery.data ?? []).map((item: StoredCaseSummary) => ({
        label: item.name,
        value: item.id,
      })) as { label: string; value: number }[]),
    ],
    [casesQuery.data],
  );
  const hasNextPage = (executionsQuery.data?.length ?? 0) === PAGE_SIZE;
  const paginationTotal = hasNextPage ? page * PAGE_SIZE + 1 : (page - 1) * PAGE_SIZE + (executionsQuery.data?.length ?? 0);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">执行中心</h1>
        <p className="page-subtitle">按用例、状态和分页查看执行结果，并直接跳到失败步骤详情。</p>
      </div>
      <Card>
        <Space direction="vertical" size="large" style={{ width: "100%" }}>
          <Space wrap>
            <Typography.Text type="secondary">状态筛选</Typography.Text>
            <Select
              value={status}
              virtual={false}
              options={STATUS_OPTIONS}
              onChange={(value) => setStatus(value)}
              style={{ width: 160 }}
            />
            <Typography.Text type="secondary">用例筛选</Typography.Text>
            <Select
              value={caseId ?? 0}
              virtual={false}
              options={caseOptions}
              loading={casesQuery.isLoading}
              onChange={(value) => setCaseId(value === 0 ? undefined : value)}
              style={{ width: 220 }}
            />
          </Space>
          {executionsQuery.isLoading ? <LoadingBlock /> : null}
          {executionsQuery.isError ? <ErrorBlock message={executionsQuery.error.message} /> : null}
          {!executionsQuery.isLoading && !executionsQuery.isError && !executionsQuery.data?.length ? (
            <EmptyBlock description="当前筛选条件下没有执行记录。" />
          ) : null}
          {executionsQuery.data?.length ? (
            <>
              <Table rowKey="id" pagination={false} columns={columns} dataSource={executionsQuery.data} />
              <div className="table-footer-actions">
                <Pagination
                  current={page}
                  pageSize={PAGE_SIZE}
                  total={paginationTotal}
                  showSizeChanger={false}
                  onChange={(nextPage) => setPage(nextPage)}
                />
                <Typography.Text type="secondary">
                  每页 {PAGE_SIZE} 条{hasNextPage ? "，还有更多记录" : ""}
                </Typography.Text>
              </div>
            </>
          ) : null}
        </Space>
      </Card>
    </>
  );
}
