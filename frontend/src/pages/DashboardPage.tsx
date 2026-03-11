import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Card, List, Space, Tag, Typography } from "antd";
import type { EChartsOption } from "echarts";
import { Link } from "react-router-dom";

import { OverviewChart } from "../components/OverviewChart";
import {
  FAILURE_CATEGORY_LABELS,
  buildExecutionsPath,
  formatDuration,
  formatPassRate,
  truncateText,
} from "../components/executionPresentation";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { getCases, getExecutionOverview } from "../services/api";

export function DashboardPage() {
  const casesQuery = useQuery({
    queryKey: ["cases"],
    queryFn: getCases,
  });
  const overviewQuery = useQuery({
    queryKey: ["executions-overview", "dashboard", 7],
    queryFn: () =>
      getExecutionOverview({
        project_id: 1,
        window_days: 7,
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
      yAxis: {
        type: "value" as const,
        minInterval: 1,
      },
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

  return (
    <>
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }}>
          <div>
            <h1 className="page-title">仪表盘</h1>
            <p className="page-subtitle">聚焦近 7 天执行趋势、最近失败和高频失败用例。</p>
          </div>
          <Space>
            <Button>
              <Link to={buildExecutionsPath({ window_days: 7 })}>执行中心</Link>
            </Button>
            <Button type="primary">
              <Link to="/reports?window_days=7">报告中心</Link>
            </Button>
          </Space>
        </Space>
      </div>

      {overviewQuery.isLoading || casesQuery.isLoading ? <LoadingBlock /> : null}
      {overviewQuery.isError ? <ErrorBlock message={overviewQuery.error.message} /> : null}
      {casesQuery.isError ? <ErrorBlock message={casesQuery.error.message} /> : null}

      {overviewQuery.data ? (
        <div className="summary-strip">
          <div className="summary-tile">
            <div className="summary-label">总用例数</div>
            <div className="summary-value">{casesQuery.data?.length ?? 0}</div>
          </div>
          <div className="summary-tile">
            <div className="summary-label">近 7 天总执行数</div>
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

      {overviewQuery.data ? (
        <div className="dashboard-grid">
          <Card className="dashboard-card" title="近 7 天通过 / 失败趋势">
            <OverviewChart option={trendOption} testId="dashboard-trend-chart" />
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
                        <Link
                          to={buildExecutionsPath({
                            window_days: 7,
                            status: "failed",
                            case_id: item.case_id,
                            failure_category: item.failure_category ?? undefined,
                          })}
                        >
                          {item.case_name}
                        </Link>
                        {item.failure_category ? <Tag>{FAILURE_CATEGORY_LABELS[item.failure_category]}</Tag> : null}
                      </Space>
                      <Typography.Text type="secondary">
                        {truncateText(item.error_message)}
                      </Typography.Text>
                    </Space>
                  </List.Item>
                )}
              />
            ) : (
              <EmptyBlock description="近 7 天暂无失败执行。" />
            )}
          </Card>

          <Card className="dashboard-card" title="失败最多用例">
            {overviewQuery.data.top_failed_cases.length ? (
              <List
                size="small"
                dataSource={overviewQuery.data.top_failed_cases}
                renderItem={(item) => (
                  <List.Item>
                    <Space direction="vertical" size={4} style={{ width: "100%" }}>
                      <Space wrap>
                        <Link
                          to={buildExecutionsPath({
                            window_days: 7,
                            status: "failed",
                            case_id: item.case_id,
                          })}
                        >
                          {item.case_name}
                        </Link>
                        <Tag color="error">{item.failure_count} 次失败</Tag>
                        {item.latest_failure_category ? (
                          <Tag>{FAILURE_CATEGORY_LABELS[item.latest_failure_category]}</Tag>
                        ) : null}
                      </Space>
                      <Typography.Text type="secondary">
                        最近失败执行：#{item.latest_execution_id}
                      </Typography.Text>
                    </Space>
                  </List.Item>
                )}
              />
            ) : (
              <EmptyBlock description="近 7 天暂无高频失败用例。" />
            )}
          </Card>
        </div>
      ) : null}
    </>
  );
}
