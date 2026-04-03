import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Card, List, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { EChartsOption } from "echarts";
import { Link, useSearchParams } from "react-router-dom";

import { OverviewChart } from "../components/OverviewChart";
import {
  FAILURE_CATEGORY_LABELS,
  buildExecutionsPath,
  formatDuration,
  formatPassRate,
  truncateText,
} from "../components/executionPresentation";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import {
  getCases,
  getExecutionOverview,
  getProjects,
  getReportPreference,
  updateReportPreference,
} from "../services/api";
import type {
  FailureRootCause,
  OverviewWindowDays,
  ReportPreference,
  ReportScopeType,
  StoredCaseExecutionSummary,
  StoredCaseSummary,
} from "../types/api";

const WINDOW_OPTIONS: OverviewWindowDays[] = [7, 14, 30];
const SCOPE_LABELS: Record<ReportScopeType, string> = {
  global: "全局",
  project: "项目",
  case: "用例",
};

interface ReportSelectionState {
  scopeType: ReportScopeType;
  projectId?: number;
  caseId?: number;
  windowDays: OverviewWindowDays;
}

function parseWindowDays(value: string | null): OverviewWindowDays {
  if (value === "14") {
    return 14;
  }
  if (value === "30") {
    return 30;
  }
  return 7;
}

function parseId(value: string | null) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
}

function parseScopeType(value: string | null): ReportScopeType | undefined {
  if (value === "global" || value === "project" || value === "case") {
    return value;
  }
  return undefined;
}

function hasExplicitScope(searchParams: URLSearchParams) {
  return ["scope_type", "project_id", "case_id", "window_days"].some((key) => searchParams.has(key));
}

function getSelectionFromSearchParams(searchParams: URLSearchParams): ReportSelectionState {
  const projectId = parseId(searchParams.get("project_id"));
  const caseId = parseId(searchParams.get("case_id"));
  const explicitScope = parseScopeType(searchParams.get("scope_type"));
  const scopeType = explicitScope ?? (caseId && projectId ? "case" : projectId ? "project" : "project");
  return {
    scopeType,
    projectId,
    caseId,
    windowDays: parseWindowDays(searchParams.get("window_days")),
  };
}

function toPreferencePayload(selection: ReportSelectionState): ReportPreference {
  if (selection.scopeType === "global") {
    return {
      scope_type: "global",
      project_id: null,
      case_id: null,
      window_days: selection.windowDays,
    };
  }
  if (selection.scopeType === "project") {
    return {
      scope_type: "project",
      project_id: selection.projectId ?? null,
      case_id: null,
      window_days: selection.windowDays,
    };
  }
  return {
    scope_type: "case",
    project_id: selection.projectId ?? null,
    case_id: selection.caseId ?? null,
    window_days: selection.windowDays,
  };
}

function formatWindowRange(startDate?: string | null, endDate?: string | null) {
  if (!startDate || !endDate) {
    return "全部历史";
  }
  return `${startDate.slice(5)} ~ ${endDate.slice(5)}`;
}

function formatSignedInteger(value: number) {
  return `${value > 0 ? "+" : ""}${value}`;
}

function formatSignedDuration(value: number) {
  return `${value > 0 ? "+" : ""}${value} ms`;
}

function formatPassRateDelta(value: number) {
  return `${value > 0 ? "+" : ""}${(value * 100).toFixed(1)} pp`;
}

function buildScopeQuery(selection: ReportSelectionState) {
  return {
    scope_type: selection.scopeType,
    project_id: selection.scopeType === "global" ? undefined : selection.projectId,
    case_id: selection.scopeType === "case" ? selection.caseId : undefined,
    window_days: selection.windowDays,
  };
}

function buildFailureFingerprintLink(record: FailureRootCause, selection: ReportSelectionState) {
  return buildExecutionsPath({
    ...buildScopeQuery(selection),
    status: "failed",
    failure_fingerprint: record.fingerprint,
    root_cause_title: record.title,
  });
}

function buildInterventionLink(selection: ReportSelectionState) {
  return buildExecutionsPath({
    ...buildScopeQuery(selection),
    status: "needs_intervention",
  });
}

function syncSearchParams(
  currentParams: URLSearchParams,
  setSearchParams: ReturnType<typeof useSearchParams>[1],
  selection: ReportSelectionState,
) {
  const nextParams = new URLSearchParams(currentParams);
  nextParams.set("scope_type", selection.scopeType);
  nextParams.set("window_days", String(selection.windowDays));
  if (selection.scopeType === "global") {
    nextParams.delete("project_id");
    nextParams.delete("case_id");
  } else if (selection.scopeType === "project") {
    if (selection.projectId) {
      nextParams.set("project_id", String(selection.projectId));
    } else {
      nextParams.delete("project_id");
    }
    nextParams.delete("case_id");
  } else {
    if (selection.projectId) {
      nextParams.set("project_id", String(selection.projectId));
    }
    if (selection.caseId) {
      nextParams.set("case_id", String(selection.caseId));
    }
  }
  setSearchParams(nextParams, { replace: true });
}

function getFirstCaseForProject(cases: StoredCaseSummary[], projectId?: number) {
  const matchedCase = cases.find((item) => item.project_id === projectId);
  if (matchedCase) {
    return matchedCase;
  }
  return cases[0];
}

export function ReportCenterPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [selection, setSelection] = useState<ReportSelectionState | null>(null);

  const projectsQuery = useQuery({
    queryKey: ["projects"],
    queryFn: getProjects,
  });
  const casesQuery = useQuery({
    queryKey: ["cases"],
    queryFn: getCases,
  });
  const preferenceQuery = useQuery({
    queryKey: ["report-preference"],
    queryFn: getReportPreference,
    enabled: !hasExplicitScope(searchParams),
  });

  const projects = projectsQuery.data ?? [];
  const cases = casesQuery.data?.items ?? [];
  const filteredCases = useMemo(
    () => cases.filter((item) => !selection?.projectId || item.project_id === selection.projectId),
    [cases, selection?.projectId],
  );

  useEffect(() => {
    if (selection) {
      return;
    }
    if (hasExplicitScope(searchParams)) {
      setSelection(getSelectionFromSearchParams(searchParams));
      return;
    }
    if (preferenceQuery.data) {
      setSelection({
        scopeType: preferenceQuery.data.scope_type,
        projectId: preferenceQuery.data.project_id ?? undefined,
        caseId: preferenceQuery.data.case_id ?? undefined,
        windowDays: preferenceQuery.data.window_days,
      });
    }
  }, [preferenceQuery.data, searchParams, selection]);

  const applySelection = (nextSelection: ReportSelectionState, persistPreference = true) => {
    setSelection(nextSelection);
    syncSearchParams(searchParams, setSearchParams, nextSelection);
    if (persistPreference) {
      void updateReportPreference(toPreferencePayload(nextSelection));
    }
  };

  const overviewQuery = useQuery({
    queryKey: [
      "executions-overview",
      "reports",
      selection?.scopeType,
      selection?.projectId,
      selection?.caseId,
      selection?.windowDays,
    ],
    enabled:
      selection !== null &&
      (selection.scopeType === "global" ||
        (selection.scopeType === "project" && Boolean(selection.projectId)) ||
        (selection.scopeType === "case" && Boolean(selection.projectId) && Boolean(selection.caseId))),
    queryFn: () =>
      getExecutionOverview({
        scope_type: selection?.scopeType,
        project_id: selection?.scopeType === "global" ? undefined : selection?.projectId,
        case_id: selection?.scopeType === "case" ? selection.caseId : undefined,
        window_days: selection?.windowDays,
      }),
  });

  const trendOption = useMemo<EChartsOption>(
    () => ({
      tooltip: { trigger: "axis" },
      legend: { data: ["通过", "失败"] },
      grid: { left: 32, right: 18, top: 32, bottom: 24, containLabel: true },
      xAxis: {
        type: "category",
        data: (overviewQuery.data?.trend_points ?? []).map((item) => item.date.slice(5)),
      },
      yAxis: { type: "value", minInterval: 1 },
      series: [
        {
          name: "通过",
          type: "line",
          smooth: true,
          data: (overviewQuery.data?.trend_points ?? []).map((item) => item.passed_count),
          lineStyle: { color: "#1f9d55", width: 3 },
          itemStyle: { color: "#1f9d55" },
        },
        {
          name: "失败",
          type: "line",
          smooth: true,
          data: (overviewQuery.data?.trend_points ?? []).map((item) => item.failed_count),
          lineStyle: { color: "#c2410c", width: 3 },
          itemStyle: { color: "#c2410c" },
        },
      ],
    }),
    [overviewQuery.data?.trend_points],
  );

  const automationOption = useMemo<EChartsOption>(
    () => ({
      tooltip: { trigger: "axis" },
      legend: { data: ["自动完成", "人工介入"] },
      grid: { left: 32, right: 18, top: 32, bottom: 24, containLabel: true },
      xAxis: {
        type: "category",
        data: (overviewQuery.data?.trend_points ?? []).map((item) => item.date.slice(5)),
      },
      yAxis: { type: "value", minInterval: 1 },
      series: [
        {
          name: "自动完成",
          type: "bar",
          stack: "execution-mode",
          data: (overviewQuery.data?.trend_points ?? []).map((item) => item.auto_completed_count),
          itemStyle: { color: "#2563eb" },
        },
        {
          name: "人工介入",
          type: "bar",
          stack: "execution-mode",
          data: (overviewQuery.data?.trend_points ?? []).map((item) => item.intervention_count),
          itemStyle: { color: "#d97706" },
        },
      ],
    }),
    [overviewQuery.data?.trend_points],
  );

  const rootCauseColumns = useMemo<ColumnsType<FailureRootCause>>(
    () => [
      {
        title: "高频错误点",
        dataIndex: "title",
        key: "title",
        render: (value: string, record) => (
          <Space direction="vertical" size={4}>
            <Typography.Text title={value}>{truncateText(value, 48)}</Typography.Text>
            <Space wrap>
              {record.latest_failure_category ? <Tag>{FAILURE_CATEGORY_LABELS[record.latest_failure_category]}</Tag> : null}
              <Typography.Text type="secondary">指纹 {record.fingerprint}</Typography.Text>
            </Space>
          </Space>
        ),
      },
      {
        title: "次数",
        dataIndex: "count",
        key: "count",
        width: 100,
        render: (value: number) => <Tag color="error">{value}</Tag>,
      },
      {
        title: "影响用例",
        dataIndex: "affected_case_count",
        key: "affected_case_count",
        width: 120,
      },
      {
        title: "操作",
        key: "actions",
        width: 180,
        render: (_, record) =>
          selection ? (
            <Space wrap>
              <Link to={buildFailureFingerprintLink(record, selection)}>筛选执行</Link>
              <Link to={`/executions/${record.latest_execution_id}`}>查看详情</Link>
            </Space>
          ) : null,
      },
    ],
    [selection],
  );

  const currentWindowRange = formatWindowRange(
    overviewQuery.data?.current_window_range?.start_date,
    overviewQuery.data?.current_window_range?.end_date,
  );
  const previousWindowRange = formatWindowRange(
    overviewQuery.data?.previous_window_range?.start_date,
    overviewQuery.data?.previous_window_range?.end_date,
  );

  const projectOptions = projects.map((item) => ({
    label: item.name,
    value: item.id,
  }));
  const caseOptions = filteredCases.map((item) => ({
    label: item.name,
    value: item.id,
  }));

  const isBootstrapping =
    !selection ||
    projectsQuery.isLoading ||
    casesQuery.isLoading ||
    (!hasExplicitScope(searchParams) && preferenceQuery.isLoading);
  const bootstrapError =
    projectsQuery.error instanceof Error
      ? projectsQuery.error
      : casesQuery.error instanceof Error
        ? casesQuery.error
        : preferenceQuery.error instanceof Error
          ? preferenceQuery.error
          : null;

  return (
    <>
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }}>
          <div>
            <h1 className="page-title">报告中心</h1>
            <p className="page-subtitle">
              按范围和时间窗口查看 AI 自动化率、成功率、高频错误点与人工介入情况。
            </p>
          </div>
          <Space>
            {WINDOW_OPTIONS.map((option) => (
              <Button
                key={option}
                type={selection?.windowDays === option ? "primary" : "default"}
                onClick={() =>
                  selection
                    ? applySelection(
                        {
                          ...selection,
                          windowDays: option,
                        },
                        true,
                      )
                    : undefined
                }
              >
                {option} 天
              </Button>
            ))}
          </Space>
        </Space>
      </div>

      <Card style={{ marginBottom: 20 }}>
        <Space wrap>
          <Typography.Text type="secondary">统计范围</Typography.Text>
          {(["global", "project", "case"] as ReportScopeType[]).map((scopeType) => (
            <Button
              key={scopeType}
              aria-label={SCOPE_LABELS[scopeType]}
              type={selection?.scopeType === scopeType ? "primary" : "default"}
              disabled={
                scopeType === "case" && cases.length === 0
                  ? true
                  : scopeType === "project" && projects.length === 0
                    ? true
                    : false
              }
              onClick={() => {
                if (!selection) {
                  return;
                }
                if (scopeType === "global") {
                  applySelection({
                    scopeType: "global",
                    windowDays: selection.windowDays,
                  });
                  return;
                }
                if (scopeType === "project") {
                  applySelection({
                    scopeType: "project",
                    projectId: selection.projectId ?? projects[0]?.id,
                    windowDays: selection.windowDays,
                  });
                  return;
                }
                const firstCase = getFirstCaseForProject(cases, selection.projectId ?? projects[0]?.id);
                if (!firstCase) {
                  return;
                }
                applySelection({
                  scopeType: "case",
                  projectId: firstCase.project_id,
                  caseId: firstCase.id,
                  windowDays: selection.windowDays,
                });
              }}
            >
              {SCOPE_LABELS[scopeType]}
            </Button>
          ))}
          {selection?.scopeType !== "global" ? (
            <Select
              style={{ width: 220 }}
              placeholder="选择项目"
              value={selection?.projectId}
              options={projectOptions}
              onChange={(projectId) => {
                if (!selection) {
                  return;
                }
                if (selection.scopeType === "project") {
                  applySelection({
                    ...selection,
                    projectId,
                  });
                  return;
                }
                const firstCase = getFirstCaseForProject(cases, projectId);
                applySelection({
                  ...selection,
                  projectId,
                  caseId: firstCase?.id,
                });
              }}
            />
          ) : null}
          {selection?.scopeType === "case" ? (
            <Select
              style={{ width: 220 }}
              placeholder="选择用例"
              value={selection.caseId}
              options={caseOptions}
              onChange={(caseId) => {
                if (!selection) {
                  return;
                }
                applySelection({
                  ...selection,
                  caseId,
                });
              }}
            />
          ) : null}
        </Space>
      </Card>

      {isBootstrapping ? <LoadingBlock /> : null}
      {!isBootstrapping && bootstrapError ? <ErrorBlock message={bootstrapError.message} /> : null}
      {!isBootstrapping && !bootstrapError && overviewQuery.isLoading ? <LoadingBlock /> : null}
      {!isBootstrapping && !bootstrapError && overviewQuery.isError ? (
        <ErrorBlock message={overviewQuery.error.message} />
      ) : null}

      {overviewQuery.data && selection ? (
        <>
          <div className="summary-strip">
            <div className="summary-tile">
              <div className="summary-label">总执行数</div>
              <div className="summary-value">{overviewQuery.data.total_count}</div>
              <div className="summary-meta">较上一窗口 {formatSignedInteger(overviewQuery.data.window_comparison.total_count_delta)}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">成功率</div>
              <div className="summary-value">{formatPassRate(overviewQuery.data.pass_rate)}</div>
              <div className="summary-meta">
                较上一窗口 {formatPassRateDelta(overviewQuery.data.window_comparison.pass_rate_delta)}
              </div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">AI 自动化率</div>
              <div className="summary-value">{formatPassRate(overviewQuery.data.automation_rate)}</div>
              <div className="summary-meta">自动完成 {overviewQuery.data.auto_completed_count} 次</div>
              <div className="summary-meta">{formatPassRate(overviewQuery.data.intervention_rate)}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">人工介入率</div>
              <div className="summary-value">{formatPassRate(overviewQuery.data.intervention_rate)}</div>
              <div className="summary-meta">人工介入 {overviewQuery.data.intervention_count} 次</div>
            </div>
          </div>

          <Card className="dashboard-card" style={{ marginBottom: 20 }} title="窗口对比">
            <Space direction="vertical" size={6}>
              <Typography.Text>当前窗口：{currentWindowRange}</Typography.Text>
              <Typography.Text>上一窗口：{previousWindowRange}</Typography.Text>
              <Typography.Text>
                较上一窗口 {formatSignedInteger(overviewQuery.data.window_comparison.failed_count_delta)}
              </Typography.Text>
              <Typography.Text>
                较上一窗口 {formatSignedDuration(overviewQuery.data.window_comparison.avg_duration_ms_delta)}
              </Typography.Text>
              <Typography.Text type="secondary">
                失败数较上一窗口 {formatSignedInteger(overviewQuery.data.window_comparison.failed_count_delta)}，
                平均耗时较上一窗口 {formatSignedDuration(overviewQuery.data.window_comparison.avg_duration_ms_delta)}
              </Typography.Text>
            </Space>
          </Card>

          <div className="analytics-grid">
            <Card className="dashboard-card" title="执行趋势">
              <OverviewChart option={trendOption} testId="report-trend-chart" />
            </Card>
            <Card className="dashboard-card" title="自动完成 vs 人工介入">
              <OverviewChart option={automationOption} testId="report-automation-chart" />
            </Card>
          </div>

          <div className="analytics-grid">
            <Card className="dashboard-card" title="高频错误点榜">
              {overviewQuery.data.failure_root_causes.length ? (
                <Table
                  rowKey="fingerprint"
                  pagination={false}
                  columns={rootCauseColumns}
                  dataSource={overviewQuery.data.failure_root_causes}
                />
              ) : (
                <EmptyBlock description="当前窗口内暂无可聚合的高频错误点。" />
              )}
            </Card>

            <Card className="dashboard-card" title="最近人工介入执行">
              {overviewQuery.data.latest_intervention_runs.length ? (
                <List
                  size="small"
                  dataSource={overviewQuery.data.latest_intervention_runs}
                  renderItem={(item: StoredCaseExecutionSummary) => (
                    <List.Item>
                      <Space direction="vertical" size={4} style={{ width: "100%" }}>
                        <Space wrap>
                          <Link to={buildInterventionLink(selection)}>{item.case_name}</Link>
                          {item.failure_category ? <Tag>{FAILURE_CATEGORY_LABELS[item.failure_category]}</Tag> : null}
                          <Tag color="warning">待人工介入</Tag>
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
                <EmptyBlock description="当前窗口内暂无人工介入执行。" />
              )}
            </Card>
          </div>
        </>
      ) : null}
    </>
  );
}
