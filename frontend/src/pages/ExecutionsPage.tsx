import { useMemo } from "react";
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
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { Link, useLocation, useSearchParams } from "react-router-dom";

import {
  FAILURE_CATEGORY_LABELS,
  buildExecutionLink,
  formatDuration,
  formatPassRate,
  renderExecutionStatus,
  truncateText,
} from "../components/executionPresentation";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { getCases, getExecutionOverview, getExecutions } from "../services/api";
import type {
  ExecutionStatus,
  FailureCategory,
  OverviewWindowDays,
  ReportScopeType,
  StoredCaseExecutionSummary,
  StoredCaseSummary,
} from "../types/api";

const PAGE_SIZE = 10;
const DEFAULT_PROJECT_ID = 1;
const FAILURE_CATEGORY_VALUES: FailureCategory[] = [
  "configuration",
  "locator",
  "assertion",
  "navigation",
  "network",
  "runner",
];
const STATUS_OPTIONS: { label: string; value: ExecutionStatus | "all" }[] = [
  { label: "全部状态", value: "all" },
  { label: "通过", value: "passed" },
  { label: "失败", value: "failed" },
  { label: "待人工介入", value: "needs_intervention" },
  { label: "运行中", value: "running" },
];
const WINDOW_OPTIONS: { label: string; value: OverviewWindowDays | "all" }[] = [
  { label: "全部时间", value: "all" },
  { label: "7 天", value: 7 },
  { label: "14 天", value: 14 },
  { label: "30 天", value: 30 },
];

function parseScopeType(value: string | null): ReportScopeType {
  if (value === "global" || value === "project" || value === "case") {
    return value;
  }
  return "project";
}

function parseStatus(value: string | null): ExecutionStatus | "all" {
  if (value === "passed" || value === "failed" || value === "needs_intervention" || value === "running") {
    return value;
  }
  return "all";
}

function parseWindowDays(value: string | null): OverviewWindowDays | undefined {
  if (value === "7" || value === "14" || value === "30") {
    return Number(value) as OverviewWindowDays;
  }
  return undefined;
}

function parseCaseId(value: string | null) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
}

function parseProjectId(value: string | null) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
}

function parseFailureCategory(value: string | null): FailureCategory | "all" {
  if (value && FAILURE_CATEGORY_VALUES.includes(value as FailureCategory)) {
    return value as FailureCategory;
  }
  return "all";
}

function parsePage(value: string | null) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 1;
}

function normalizeSearchValue(value: string | null) {
  const normalized = value?.trim();
  return normalized ? normalized : undefined;
}

function buildColumns(currentExecutionsPath: string): ColumnsType<StoredCaseExecutionSummary> {
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
          <Link to={buildExecutionLink(record)} state={{ fromExecutions: currentExecutionsPath }}>
            {value}
          </Link>
          {record.failed_step_index !== null && record.failed_step_index !== undefined ? (
            <Typography.Text type="secondary">失败步骤：Step {record.failed_step_index + 1}</Typography.Text>
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
      render: renderExecutionStatus,
    },
    {
      title: "失败分类",
      dataIndex: "failure_category",
      key: "failure_category",
      width: 120,
      render: (value: FailureCategory | null | undefined) =>
        value ? <span>{FAILURE_CATEGORY_LABELS[value]}</span> : <Typography.Text type="secondary">-</Typography.Text>,
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
          <Link to={buildExecutionLink(record)} state={{ fromExecutions: currentExecutionsPath }}>
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
  const location = useLocation();
  const [searchParams, setSearchParams] = useSearchParams();
  const scopeType = parseScopeType(searchParams.get("scope_type"));
  const projectId = parseProjectId(searchParams.get("project_id"));
  const activeProjectId = scopeType === "global" ? undefined : (projectId ?? DEFAULT_PROJECT_ID);
  const status = parseStatus(searchParams.get("status"));
  const caseId = parseCaseId(searchParams.get("case_id"));
  const failureCategory = parseFailureCategory(searchParams.get("failure_category"));
  const failureFingerprint = normalizeSearchValue(searchParams.get("failure_fingerprint"));
  const failureFingerprintTitle = normalizeSearchValue(searchParams.get("root_cause_title"));
  const windowDays = parseWindowDays(searchParams.get("window_days"));
  const page = parsePage(searchParams.get("page"));
  const currentExecutionsPath = `${location.pathname}${location.search}`;
  const columns = useMemo(() => buildColumns(currentExecutionsPath), [currentExecutionsPath]);

  const updateSearchState = (
    updates: {
      status?: ExecutionStatus | "all";
      case_id?: number;
      failure_category?: FailureCategory | "all";
      failure_fingerprint?: string;
      root_cause_title?: string;
      window_days?: OverviewWindowDays;
      page?: number;
    },
    { resetPage = true }: { resetPage?: boolean } = {},
  ) => {
    const nextParams = new URLSearchParams(searchParams);
    const entries = Object.entries(updates) as Array<[string, string | number | undefined]>;

    for (const [key, rawValue] of entries) {
      const shouldDelete =
        rawValue === undefined ||
        rawValue === null ||
        rawValue === "" ||
        rawValue === "all" ||
        (key === "page" && rawValue === 1);
      if (shouldDelete) {
        nextParams.delete(key);
      } else {
        nextParams.set(key, String(rawValue));
      }
    }

    if (resetPage && !("page" in updates)) {
      nextParams.delete("page");
    }

    setSearchParams(nextParams, { replace: true });
  };

  const casesQuery = useQuery({
    queryKey: ["cases"],
    queryFn: getCases,
  });
  const overviewQuery = useQuery({
    queryKey: ["executions-overview", activeProjectId, caseId, failureFingerprint, windowDays],
    queryFn: () =>
      getExecutionOverview({
        project_id: activeProjectId,
        case_id: caseId,
        window_days: windowDays,
        failure_fingerprint: failureFingerprint,
      }),
  });
  const executionsQuery = useQuery({
    queryKey: ["executions", activeProjectId, status, caseId, failureCategory, failureFingerprint, windowDays, page],
    queryFn: () =>
      getExecutions({
        project_id: activeProjectId,
        case_id: caseId,
        status: status === "all" ? undefined : status,
        window_days: windowDays,
        failure_category: failureCategory === "all" ? undefined : failureCategory,
        failure_fingerprint: failureFingerprint,
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
      }),
  });

  const caseOptions = useMemo(
    () => [
      { label: "全部用例", value: 0 },
      ...((casesQuery.data?.items ?? [])
        .filter((item: StoredCaseSummary) => !activeProjectId || item.project_id === activeProjectId)
        .map((item: StoredCaseSummary) => ({
          label: item.name,
          value: item.id,
        })) as { label: string; value: number }[]),
    ],
    [activeProjectId, casesQuery.data?.items],
  );
  const failureCategoryCounts = overviewQuery.data?.failure_categories ?? [];
  const hasNextPage = (executionsQuery.data?.length ?? 0) === PAGE_SIZE;
  const paginationTotal = hasNextPage
    ? page * PAGE_SIZE + 1
    : (page - 1) * PAGE_SIZE + (executionsQuery.data?.length ?? 0);

  const clearFailureFingerprintFilter = () => {
    updateSearchState({
      failure_fingerprint: undefined,
      root_cause_title: undefined,
    });
  };

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">执行中心</h1>
        <p className="page-subtitle">按用例、时间窗口、状态和分页查看执行结果，并直接跳到失败步骤详情。</p>
      </div>

      <Card style={{ marginBottom: 20 }}>
        <Space wrap>
          <Typography.Text type="secondary">时间窗口</Typography.Text>
          {WINDOW_OPTIONS.map((item) => (
            <Button
              key={String(item.value)}
              type={windowDays === item.value || (item.value === "all" && !windowDays) ? "primary" : "default"}
              onClick={() =>
                updateSearchState({
                  window_days: item.value === "all" ? undefined : item.value,
                })
              }
            >
              {item.label}
            </Button>
          ))}
        </Space>
      </Card>

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
                      <Link to={buildExecutionLink(item)} state={{ fromExecutions: currentExecutionsPath }}>
                        {item.case_name}
                      </Link>
                      {item.failure_category ? <span>{FAILURE_CATEGORY_LABELS[item.failure_category]}</span> : null}
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
              onChange={(value) => updateSearchState({ status: value })}
              style={{ width: 160 }}
            />
            <Typography.Text type="secondary">用例筛选</Typography.Text>
            <Select
              value={caseId ?? 0}
              virtual={false}
              options={caseOptions}
              loading={casesQuery.isLoading}
              onChange={(value) => updateSearchState({ case_id: value === 0 ? undefined : value })}
              style={{ width: 220 }}
            />
          </Space>
          {failureFingerprint ? (
            <div className="active-filter-panel">
              <Space direction="vertical" size={4} style={{ width: "100%" }}>
                <Typography.Text strong>根因筛选已启用</Typography.Text>
                <Space wrap>
                  <Typography.Text title={failureFingerprintTitle ?? failureFingerprint}>
                    {truncateText(failureFingerprintTitle ?? failureFingerprint, 96)}
                  </Typography.Text>
                  <Button size="small" onClick={clearFailureFingerprintFilter}>
                    清除根因筛选
                  </Button>
                </Space>
              </Space>
            </div>
          ) : null}
          <Space direction="vertical" size="small" style={{ width: "100%" }}>
            <Typography.Text type="secondary">失败分类</Typography.Text>
            <Space wrap>
              <Button
                type={failureCategory === "all" ? "primary" : "default"}
                onClick={() => updateSearchState({ failure_category: undefined })}
              >
                全部失败类型
              </Button>
              {failureCategoryCounts.map((item) => (
                <Button
                  key={item.category}
                  type={failureCategory === item.category ? "primary" : "default"}
                  onClick={() => updateSearchState({ failure_category: item.category })}
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
                  onChange={(nextPage) => updateSearchState({ page: nextPage }, { resetPage: false })}
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
