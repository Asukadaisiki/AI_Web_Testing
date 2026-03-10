import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Card, List, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { EChartsOption } from "echarts";
import { Link } from "react-router-dom";

import { OverviewChart } from "../components/OverviewChart";
import {
  FAILURE_CATEGORY_LABELS,
  buildExecutionLink,
  formatDuration,
  formatPassRate,
  truncateText,
} from "../components/executionPresentation";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { getExecutionOverview } from "../services/api";
import type { FailureRootCause, OverviewWindowDays, TopFailedCase } from "../types/api";

const WINDOW_OPTIONS: OverviewWindowDays[] = [7, 14, 30];

function formatWindowRange(
  startDate?: string | null,
  endDate?: string | null,
) {
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

function buildFailureFingerprintLink(record: FailureRootCause) {
  const search = new URLSearchParams({
    status: "failed",
    failure_fingerprint: record.fingerprint,
    root_cause_title: record.title,
  });
  return `/executions?${search.toString()}`;
}

export function ReportCenterPage() {
  const [windowDays, setWindowDays] = useState<OverviewWindowDays>(7);
  const overviewQuery = useQuery({
    queryKey: ["executions-overview", "reports", windowDays],
    queryFn: () =>
      getExecutionOverview({
        project_id: 1,
        window_days: windowDays,
      }),
  });

  const trendOption = useMemo<EChartsOption>(
    () => ({
      tooltip: { trigger: "axis" },
      legend: { data: ["通过", "失败"] },
      grid: { left: 32, right: 18, top: 32, bottom: 24, containLabel: true },
      xAxis: {
        type: "category" as const,
        data: (overviewQuery.data?.trend_points ?? []).map((item) => item.date.slice(5)),
      },
      yAxis: { type: "value" as const, minInterval: 1 },
      series: [
        {
          name: "通过",
          type: "line" as const,
          smooth: true,
          data: (overviewQuery.data?.trend_points ?? []).map((item) => item.passed_count),
          lineStyle: { color: "#1f9d55", width: 3 },
          itemStyle: { color: "#1f9d55" },
        },
        {
          name: "失败",
          type: "line" as const,
          smooth: true,
          data: (overviewQuery.data?.trend_points ?? []).map((item) => item.failed_count),
          lineStyle: { color: "#c2410c", width: 3 },
          itemStyle: { color: "#c2410c" },
        },
      ],
    }),
    [overviewQuery.data?.trend_points],
  );

  const failureCategoryOption = useMemo<EChartsOption>(
    () => ({
      tooltip: { trigger: "axis" },
      grid: { left: 24, right: 12, top: 24, bottom: 24, containLabel: true },
      xAxis: {
        type: "category" as const,
        data: (overviewQuery.data?.failure_categories ?? []).map((item) => FAILURE_CATEGORY_LABELS[item.category]),
      },
      yAxis: { type: "value" as const, minInterval: 1 },
      series: [
        {
          name: "失败分类",
          type: "bar" as const,
          data: (overviewQuery.data?.failure_categories ?? []).map((item) => item.count),
          itemStyle: { color: "#2563eb" },
        },
      ],
    }),
    [overviewQuery.data?.failure_categories],
  );

  const failureActionOption = useMemo<EChartsOption>(
    () => ({
      tooltip: { trigger: "axis" },
      grid: { left: 24, right: 12, top: 24, bottom: 24, containLabel: true },
      xAxis: {
        type: "category" as const,
        data: (overviewQuery.data?.failure_step_actions ?? []).map((item) => item.action),
      },
      yAxis: { type: "value" as const, minInterval: 1 },
      series: [
        {
          name: "失败动作",
          type: "bar" as const,
          data: (overviewQuery.data?.failure_step_actions ?? []).map((item) => item.count),
          itemStyle: { color: "#c2410c" },
        },
      ],
    }),
    [overviewQuery.data?.failure_step_actions],
  );

  const topFailedColumns = useMemo<ColumnsType<TopFailedCase>>(
    () => [
      {
        title: "用例",
        dataIndex: "case_name",
        key: "case_name",
        render: (value: string, record) => <Link to={`/executions/${record.latest_execution_id}`}>{value}</Link>,
      },
      {
        title: "失败次数",
        dataIndex: "failure_count",
        key: "failure_count",
        width: 120,
        render: (value: number) => <Tag color="error">{value}</Tag>,
      },
      {
        title: "最近失败分类",
        dataIndex: "latest_failure_category",
        key: "latest_failure_category",
        width: 150,
        render: (value: TopFailedCase["latest_failure_category"]) =>
          value ? <Tag>{FAILURE_CATEGORY_LABELS[value]}</Tag> : <Typography.Text type="secondary">-</Typography.Text>,
      },
      {
        title: "最近失败执行",
        dataIndex: "latest_execution_id",
        key: "latest_execution_id",
        width: 140,
        render: (value: number) => <Link to={`/executions/${value}`}>#{value}</Link>,
      },
    ],
    [],
  );

  const rootCauseColumns = useMemo<ColumnsType<FailureRootCause>>(
    () => [
      {
        title: "失败根因",
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
        title: "失败次数",
        dataIndex: "count",
        key: "count",
        width: 120,
        render: (value: number) => <Tag color="error">{value}</Tag>,
      },
      {
        title: "影响用例",
        dataIndex: "affected_case_count",
        key: "affected_case_count",
        width: 120,
      },
      {
        title: "最近执行",
        dataIndex: "latest_execution_id",
        key: "latest_execution_id",
        width: 140,
        render: (value: number) => <Link to={`/executions/${value}`}>#{value}</Link>,
      },
      {
        title: "操作",
        key: "actions",
        width: 180,
        render: (_, record) => (
          <Space wrap>
            <Link to={`/executions/${record.latest_execution_id}`}>查看详情</Link>
            <Link to={buildFailureFingerprintLink(record)}>筛选执行</Link>
          </Space>
        ),
      },
    ],
    [],
  );

  const currentWindowRange = formatWindowRange(
    overviewQuery.data?.current_window_range?.start_date,
    overviewQuery.data?.current_window_range?.end_date,
  );
  const previousWindowRange = formatWindowRange(
    overviewQuery.data?.previous_window_range?.start_date,
    overviewQuery.data?.previous_window_range?.end_date,
  );

  return (
    <>
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }}>
          <div>
            <h1 className="page-title">报告中心</h1>
            <p className="page-subtitle">围绕时间窗口查看趋势、环比、失败分类和根因聚合，快速回流到执行明细排障。</p>
          </div>
          <Space>
            {WINDOW_OPTIONS.map((option) => (
              <Button
                key={option}
                type={windowDays === option ? "primary" : "default"}
                onClick={() => setWindowDays(option)}
              >
                {option} 天
              </Button>
            ))}
          </Space>
        </Space>
      </div>

      {overviewQuery.isLoading ? <LoadingBlock /> : null}
      {overviewQuery.isError ? <ErrorBlock message={overviewQuery.error.message} /> : null}

      {overviewQuery.data ? (
        <>
          <div className="summary-strip">
            <div className="summary-tile">
              <div className="summary-label">窗口总执行数</div>
              <div className="summary-value">{overviewQuery.data.total_count}</div>
              <div className="summary-meta">较上一窗口 {formatSignedInteger(overviewQuery.data.window_comparison.total_count_delta)}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">失败数</div>
              <div className="summary-value">{overviewQuery.data.failed_count}</div>
              <div className="summary-meta">较上一窗口 {formatSignedInteger(overviewQuery.data.window_comparison.failed_count_delta)}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">通过率</div>
              <div className="summary-value">{formatPassRate(overviewQuery.data.pass_rate)}</div>
              <div className="summary-meta">较上一窗口 {formatPassRateDelta(overviewQuery.data.window_comparison.pass_rate_delta)}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">平均耗时</div>
              <div className="summary-value">{formatDuration(overviewQuery.data.avg_duration_ms)}</div>
              <div className="summary-meta">较上一窗口 {formatSignedDuration(overviewQuery.data.window_comparison.avg_duration_ms_delta)}</div>
            </div>
          </div>

          <Card className="dashboard-card" style={{ marginBottom: 20 }} title="窗口对比">
            <Space direction="vertical" size={6}>
              <Typography.Text>当前窗口：{currentWindowRange}</Typography.Text>
              <Typography.Text>上一窗口：{previousWindowRange}</Typography.Text>
              <Typography.Text type="secondary">
                上一窗口执行 {overviewQuery.data.previous_window_stats.total_count} 次，失败 {overviewQuery.data.previous_window_stats.failed_count} 次。
              </Typography.Text>
            </Space>
          </Card>

          <div className="analytics-grid">
            <Card className="dashboard-card" title="窗口趋势">
              <OverviewChart option={trendOption} testId="report-trend-chart" />
            </Card>
            <Card className="dashboard-card" title="失败分类分布">
              <OverviewChart option={failureCategoryOption} testId="report-category-chart" />
            </Card>
            <Card className="dashboard-card" title="失败动作分布">
              <OverviewChart option={failureActionOption} testId="report-action-chart" />
            </Card>
          </div>

          <Card title="失败根因榜" style={{ marginBottom: 20 }}>
            {overviewQuery.data.failure_root_causes.length ? (
              <Table
                rowKey="fingerprint"
                pagination={false}
                columns={rootCauseColumns}
                dataSource={overviewQuery.data.failure_root_causes}
              />
            ) : (
              <EmptyBlock description="当前窗口内暂无可聚合的失败根因。" />
            )}
          </Card>

          <div className="analytics-grid">
            <Card className="dashboard-card" title="高频失败用例">
              {overviewQuery.data.top_failed_cases.length ? (
                <Table
                  rowKey="case_id"
                  pagination={false}
                  columns={topFailedColumns}
                  dataSource={overviewQuery.data.top_failed_cases}
                />
              ) : (
                <EmptyBlock description="当前窗口内暂无高频失败用例。" />
              )}
            </Card>

            <Card className="dashboard-card" title="最近失败执行">
              {overviewQuery.data.latest_failed_runs.length ? (
                <List
                  size="small"
                  dataSource={overviewQuery.data.latest_failed_runs}
                  renderItem={(item) => (
                    <List.Item>
                      <Space direction="vertical" size={4} style={{ width: "100%" }}>
                        <Space wrap>
                          <Link to={buildExecutionLink(item)}>{item.case_name}</Link>
                          {item.failure_category ? <Tag>{FAILURE_CATEGORY_LABELS[item.failure_category]}</Tag> : null}
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
                <EmptyBlock description="当前窗口内暂无失败执行。" />
              )}
            </Card>
          </div>
        </>
      ) : null}
    </>
  );
}
