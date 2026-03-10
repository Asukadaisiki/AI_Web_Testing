import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Button,
  Card,
  Image,
  List,
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
import { getCases, getExecutionOverview, getExecutions } from "../services/api";
import type {
  ExecutionStatus,
  FailureCategory,
  StoredCaseExecutionSummary,
  StoredCaseSummary,
} from "../types/api";

const PAGE_SIZE = 10;
const FAILURE_CATEGORY_LABELS: Record<FailureCategory, string> = {
  configuration: "配置",
  locator: "定位",
  assertion: "断言",
  navigation: "导航",
  network: "网络",
  runner: "运行器",
};

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

function formatPassRate(passRate: number) {
  return `${(passRate * 100).toFixed(1)}%`;
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
      title: "失败分类",
      dataIndex: "failure_category",
      key: "failure_category",
      width: 120,
      render: (value: FailureCategory | null | undefined) =>
        value ? <Tag>{FAILURE_CATEGORY_LABELS[value]}</Tag> : <Typography.Text type="secondary">-</Typography.Text>,
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
  const [failureCategory, setFailureCategory] = useState<FailureCategory | "all">("all");
  const [page, setPage] = useState(1);
  const columns = useMemo(() => buildColumns(), []);
  const casesQuery = useQuery({
    queryKey: ["cases"],
    queryFn: getCases,
  });
  const overviewQuery = useQuery({
    queryKey: ["executions-overview", caseId],
    queryFn: () =>
      getExecutionOverview({
        project_id: 1,
        case_id: caseId,
      }),
  });
  const executionsQuery = useQuery({
    queryKey: ["executions", status, caseId, failureCategory, page],
    queryFn: () =>
      getExecutions({
        project_id: 1,
        case_id: caseId,
        status: status === "all" ? undefined : status,
        failure_category: failureCategory === "all" ? undefined : failureCategory,
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
      }),
  });

  useEffect(() => {
    setPage(1);
  }, [status, caseId, failureCategory]);

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
  const failureCategoryCounts = overviewQuery.data?.failure_categories ?? [];
  const hasNextPage = (executionsQuery.data?.length ?? 0) === PAGE_SIZE;
  const paginationTotal = hasNextPage ? page * PAGE_SIZE + 1 : (page - 1) * PAGE_SIZE + (executionsQuery.data?.length ?? 0);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">执行中心</h1>
        <p className="page-subtitle">按用例、状态和分页查看执行结果，并直接跳到失败步骤详情。</p>
      </div>
      {overviewQuery.isLoading ? <LoadingBlock /> : null}
      {overviewQuery.isError ? <ErrorBlock message={overviewQuery.error.message} /> : null}
      {overviewQuery.data ? (
        <div className="summary-strip">
          <div className="summary-tile">
            <div className="summary-label">总执行数</div>
            <div className="summary-value">{overviewQuery.data.total_count}</div>
          </div>
          <div className="summary-tile">
            <div className="summary-label">通过数</div>
            <div className="summary-value">{overviewQuery.data.passed_count}</div>
          </div>
          <div className="summary-tile">
            <div className="summary-label">失败数</div>
            <div className="summary-value">{overviewQuery.data.failed_count}</div>
          </div>
          <div className="summary-tile">
            <div className="summary-label">通过率</div>
            <div className="summary-value">{formatPassRate(overviewQuery.data.pass_rate)}</div>
          </div>
          <div className="summary-tile">
            <div className="summary-label">平均耗时</div>
            <div className="summary-value">{formatDuration(overviewQuery.data.avg_duration_ms)}</div>
          </div>
        </div>
      ) : null}

      <Card style={{ marginBottom: 20 }}>
        <Space direction="vertical" size="middle" style={{ width: "100%" }}>
          <div>
            <Typography.Title level={5} style={{ marginTop: 0 }}>
              最近失败
            </Typography.Title>
            <Typography.Text type="secondary">
              聚焦最近的失败执行，直接跳到失败步骤。
            </Typography.Text>
          </div>
          {overviewQuery.data && overviewQuery.data.latest_failed_runs.length ? (
            <List
              size="small"
              dataSource={overviewQuery.data.latest_failed_runs}
              renderItem={(item) => (
                <List.Item>
                  <Space direction="vertical" size={2} style={{ width: "100%" }}>
                    <Space wrap>
                      <Link to={buildExecutionLink(item)}>{item.case_name}</Link>
                      {item.failure_category ? <Tag>{FAILURE_CATEGORY_LABELS[item.failure_category]}</Tag> : null}
                      {item.failed_step_index !== null && item.failed_step_index !== undefined ? (
                        <Typography.Text type="secondary">Step {item.failed_step_index + 1}</Typography.Text>
                      ) : null}
                    </Space>
                    <Typography.Text type="secondary">
                      {truncateText(item.error_message)}
                      {item.latest_url ? ` · ${item.latest_url}` : ""}
                    </Typography.Text>
                  </Space>
                </List.Item>
              )}
            />
          ) : (
            <EmptyBlock description="暂无失败执行记录。" />
          )}
        </Space>
      </Card>

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
          <Space direction="vertical" size="small" style={{ width: "100%" }}>
            <Typography.Text type="secondary">失败分类</Typography.Text>
            <Space wrap>
              <Button
                type={failureCategory === "all" ? "primary" : "default"}
                onClick={() => setFailureCategory("all")}
              >
                全部失败类型
              </Button>
              {failureCategoryCounts.map((item) => (
                <Button
                  key={item.category}
                  type={failureCategory === item.category ? "primary" : "default"}
                  onClick={() => setFailureCategory(item.category)}
                >
                  {FAILURE_CATEGORY_LABELS[item.category]} ({item.count})
                </Button>
              ))}
            </Space>
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
