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
import type { OverviewWindowDays, TopFailedCase } from "../types/api";

const WINDOW_OPTIONS: OverviewWindowDays[] = [7, 14, 30];

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

  return (
    <>
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }}>
          <div>
            <h1 className="page-title">报告中心</h1>
            <p className="page-subtitle">按时间窗口查看失败分类、失败动作和高频失败用例聚合。</p>
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

          <div className="analytics-grid">
            <Card className="dashboard-card" title="失败分类分布">
              <OverviewChart option={failureCategoryOption} testId="report-category-chart" />
            </Card>
            <Card className="dashboard-card" title="失败动作分布">
              <OverviewChart option={failureActionOption} testId="report-action-chart" />
            </Card>
          </div>

          <Card title="高频失败用例" style={{ marginBottom: 20 }}>
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

          <Card title="最近失败执行">
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
        </>
      ) : null}
    </>
  );
}
