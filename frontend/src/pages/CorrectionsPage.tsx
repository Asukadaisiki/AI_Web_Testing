import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, Drawer, Input, List, Pagination, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { EChartsOption } from "echarts";
import { Link, useSearchParams } from "react-router-dom";

import { OverviewChart } from "../components/OverviewChart";
import { EmptyBlock, ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import {
  batchUpdateCorrectionState,
  getCorrectionEvents,
  getCorrections,
  getCorrectionsOverview,
  updateCorrectionState,
} from "../services/api";
import type {
  LocatorCorrectionsOverview,
  OverviewWindowDays,
  StoredLocatorCorrection,
  StoredLocatorCorrectionEvent,
} from "../types/api";

const PAGE_SIZE = 10;

type ActiveFilter = "all" | "active" | "inactive";

function parsePage(value: string | null) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : 1;
}

function parseActiveFilter(value: string | null): ActiveFilter {
  if (value === "active" || value === "inactive") {
    return value;
  }
  return "all";
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

function formatCorrectionType(value: StoredLocatorCorrection["correction_type"]) {
  if (value === "test_id") {
    return "Test ID";
  }
  if (value === "xpath") {
    return "XPath";
  }
  return "CSS";
}

function formatEventType(value: StoredLocatorCorrectionEvent["event_type"]) {
  switch (value) {
    case "created":
      return "已创建";
    case "activated":
      return "已启用";
    case "deactivated":
      return "已停用";
    case "tier0_hit":
      return "Tier 0 命中";
    case "tier0_miss":
      return "Tier 0 未命中";
    case "auto_deactivated":
      return "自动停用";
    default:
      return value;
  }
}

function buildColumns(
  onToggle: (record: StoredLocatorCorrection) => void,
  onViewEvents: (record: StoredLocatorCorrection) => void,
  togglingId: number | null,
): ColumnsType<StoredLocatorCorrection> {
  return [
    {
      title: "目标",
      dataIndex: "target_description",
      key: "target_description",
      render: (value: string, record) => (
        <Space direction="vertical" size={2}>
          <Typography.Text strong>{value}</Typography.Text>
          <Typography.Text type="secondary">{record.page_url_pattern}</Typography.Text>
        </Space>
      ),
    },
    {
      title: "修正",
      dataIndex: "correction_value",
      key: "correction_value",
      render: (value: string, record) => (
        <Space direction="vertical" size={2}>
          <Tag color="blue">{formatCorrectionType(record.correction_type)}</Tag>
          <Typography.Text code>{value}</Typography.Text>
        </Space>
      ),
    },
    {
      title: "状态",
      dataIndex: "is_active",
      key: "is_active",
      width: 120,
      render: (value: boolean) => (
        <Tag color={value ? "green" : "default"}>{value ? "生效中" : "已停用"}</Tag>
      ),
    },
    {
      title: "命中/失败",
      key: "metrics",
      width: 140,
      render: (_, record) => `${record.verified_count} / ${record.consecutive_failures}`,
    },
    {
      title: "来源执行",
      dataIndex: "source_execution_id",
      key: "source_execution_id",
      width: 120,
      render: (value: number | null) =>
        value ? <Link to={`/executions/${value}`}>#{value}</Link> : <Typography.Text type="secondary">-</Typography.Text>,
    },
    {
      title: "更新时间",
      dataIndex: "updated_at",
      key: "updated_at",
      width: 180,
      render: (value: string) => new Date(value).toLocaleString(),
    },
    {
      title: "操作",
      key: "actions",
      width: 180,
      render: (_, record) => (
        <Space size="small">
          <Button
            size="small"
            loading={togglingId === record.id}
            onClick={() => onToggle(record)}
          >
            {record.is_active ? "停用" : "启用"}
          </Button>
          <Button size="small" type="link" onClick={() => onViewEvents(record)}>
            事件
          </Button>
        </Space>
      ),
    },
  ];
}

function buildTrendOption(overview: LocatorCorrectionsOverview | undefined): EChartsOption {
  return {
    tooltip: { trigger: "axis" },
    legend: { data: ["命中", "未命中"] },
    grid: { left: 32, right: 18, top: 32, bottom: 24, containLabel: true },
    xAxis: {
      type: "category" as const,
      data: (overview?.trend_points ?? []).map((item) => item.date.slice(5)),
    },
    yAxis: {
      type: "value" as const,
      minInterval: 1,
    },
    series: [
      {
        name: "命中",
        type: "line" as const,
        smooth: true,
        data: (overview?.trend_points ?? []).map((item) => item.hit_count),
        lineStyle: { color: "#1f9d55", width: 3 },
        itemStyle: { color: "#1f9d55" },
      },
      {
        name: "未命中",
        type: "line" as const,
        smooth: true,
        data: (overview?.trend_points ?? []).map((item) => item.miss_count),
        lineStyle: { color: "#c2410c", width: 3 },
        itemStyle: { color: "#c2410c" },
      },
    ],
  };
}

function CorrectionEventsDrawer({
  correction,
  onClose,
}: {
  correction: StoredLocatorCorrection | null;
  onClose: () => void;
}) {
  const eventsQuery = useQuery({
    queryKey: ["correction-events", correction?.id],
    queryFn: () => getCorrectionEvents(correction!.id, { limit: 20, offset: 0 }),
    enabled: correction !== null,
  });

  return (
    <Drawer
      title={correction ? `${correction.target_description} 的事件时间线` : "事件时间线"}
      width={560}
      open={correction !== null}
      onClose={onClose}
      destroyOnClose
    >
      {eventsQuery.isLoading ? <LoadingBlock /> : null}
      {eventsQuery.isError ? <ErrorBlock message={eventsQuery.error.message} /> : null}
      {!eventsQuery.isLoading && !eventsQuery.isError && !eventsQuery.data?.length ? (
        <EmptyBlock description="当前修正记录还没有事件。" />
      ) : null}
      {eventsQuery.data?.length ? (
        <List
          dataSource={eventsQuery.data}
          renderItem={(item) => (
            <List.Item>
              <Space direction="vertical" size={4} style={{ width: "100%" }}>
                <Space wrap>
                  <Tag color={item.event_type.includes("hit") ? "green" : item.event_type.includes("miss") ? "orange" : "blue"}>
                    {formatEventType(item.event_type)}
                  </Tag>
                  <Typography.Text type="secondary">
                    {new Date(item.created_at).toLocaleString()}
                  </Typography.Text>
                  {item.execution_id ? (
                    <Link to={`/executions/${item.execution_id}`}>执行 #{item.execution_id}</Link>
                  ) : null}
                </Space>
                <Typography.Text type="secondary">
                  命中 {item.verified_count_after} 次，连续失败 {item.consecutive_failures_after} 次，状态
                  {item.is_active_after ? "生效中" : "已停用"}
                </Typography.Text>
              </Space>
            </List.Item>
          )}
        />
      ) : null}
    </Drawer>
  );
}

export function CorrectionsPage() {
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parsePage(searchParams.get("page"));
  const activeFilter = parseActiveFilter(searchParams.get("is_active"));
  const windowDays = parseWindowDays(searchParams.get("window_days"));
  const targetDescription = searchParams.get("target_description")?.trim() || "";
  const pageUrl = searchParams.get("page_url")?.trim() || "";
  const [targetDraft, setTargetDraft] = useState(targetDescription);
  const [pageUrlDraft, setPageUrlDraft] = useState(pageUrl);
  const [selectedRowKeys, setSelectedRowKeys] = useState<number[]>([]);
  const [eventCorrection, setEventCorrection] = useState<StoredLocatorCorrection | null>(null);

  useEffect(() => {
    setTargetDraft(targetDescription);
    setPageUrlDraft(pageUrl);
  }, [pageUrl, targetDescription]);

  const isActive =
    activeFilter === "all" ? undefined : activeFilter === "active";

  const query = useQuery({
    queryKey: ["corrections", targetDescription, pageUrl, activeFilter, page],
    queryFn: () =>
      getCorrections({
        target_description: targetDescription || undefined,
        page_url: pageUrl || undefined,
        is_active: isActive,
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
      }),
  });

  const overviewQuery = useQuery({
    queryKey: ["corrections-overview", windowDays],
    queryFn: () => getCorrectionsOverview(windowDays),
  });

  const toggleMutation = useMutation({
    mutationFn: (record: StoredLocatorCorrection) =>
      updateCorrectionState(record.id, { is_active: !record.is_active }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["corrections"] }),
        queryClient.invalidateQueries({ queryKey: ["corrections-overview"] }),
        queryClient.invalidateQueries({ queryKey: ["correction-events"] }),
      ]);
    },
  });

  const batchMutation = useMutation({
    mutationFn: ({ correctionIds, isActive }: { correctionIds: number[]; isActive: boolean }) =>
      batchUpdateCorrectionState({ correction_ids: correctionIds, is_active: isActive }),
    onSuccess: async () => {
      setSelectedRowKeys([]);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["corrections"] }),
        queryClient.invalidateQueries({ queryKey: ["corrections-overview"] }),
        queryClient.invalidateQueries({ queryKey: ["correction-events"] }),
      ]);
    },
  });

  const updateSearchState = (
    updates: {
      target_description?: string;
      page_url?: string;
      is_active?: ActiveFilter;
      page?: number;
      window_days?: OverviewWindowDays;
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
        (key === "page" && rawValue === 1) ||
        (key === "window_days" && rawValue === 7);
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

  const columns = useMemo(
    () =>
      buildColumns(
        (record) => toggleMutation.mutate(record),
        (record) => setEventCorrection(record),
        toggleMutation.variables?.id ?? null,
      ),
    [toggleMutation.mutate, toggleMutation.variables?.id],
  );
  const trendOption = useMemo(() => buildTrendOption(overviewQuery.data), [overviewQuery.data]);
  const hasNextPage = (query.data?.length ?? 0) === PAGE_SIZE;
  const paginationTotal = hasNextPage
    ? page * PAGE_SIZE + 1
    : (page - 1) * PAGE_SIZE + (query.data?.length ?? 0);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">修正记录</h1>
        <p className="page-subtitle">集中查看人工修正的命中情况、失效率和生效状态。</p>
      </div>

      {overviewQuery.isLoading ? <LoadingBlock /> : null}
      {overviewQuery.isError ? <ErrorBlock message={overviewQuery.error.message} /> : null}

      {overviewQuery.data ? (
        <>
          <div className="summary-strip" style={{ marginBottom: 20 }}>
            <div className="summary-tile">
              <div className="summary-label">修正总数</div>
              <div className="summary-value">{overviewQuery.data.total_count}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">生效中</div>
              <div className="summary-value">{overviewQuery.data.active_count}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">已停用</div>
              <div className="summary-value">{overviewQuery.data.inactive_count}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">窗口命中</div>
              <div className="summary-value">{overviewQuery.data.hit_count}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">窗口未命中</div>
              <div className="summary-value">{overviewQuery.data.miss_count}</div>
            </div>
            <div className="summary-tile">
              <div className="summary-label">自动停用</div>
              <div className="summary-value">{overviewQuery.data.auto_deactivated_count}</div>
            </div>
          </div>

          <Card style={{ marginBottom: 20 }} title={`近 ${windowDays} 天 Tier 0 命中 / 未命中趋势`}>
            <OverviewChart option={trendOption} testId="corrections-trend-chart" />
          </Card>
        </>
      ) : null}

      <Card style={{ marginBottom: 20 }}>
        <Space direction="vertical" size="middle" style={{ width: "100%" }}>
          <Space wrap>
            <Input
              value={targetDraft}
              placeholder="按目标描述筛选"
              style={{ width: 240 }}
              onChange={(event) => setTargetDraft(event.target.value)}
            />
            <Input
              value={pageUrlDraft}
              placeholder="按页面 URL 筛选"
              style={{ width: 320 }}
              onChange={(event) => setPageUrlDraft(event.target.value)}
            />
            <Button
              type="primary"
              onClick={() =>
                updateSearchState({
                  target_description: targetDraft.trim() || undefined,
                  page_url: pageUrlDraft.trim() || undefined,
                })
              }
            >
              应用筛选
            </Button>
            <Button
              onClick={() => {
                setTargetDraft("");
                setPageUrlDraft("");
                updateSearchState({
                  target_description: undefined,
                  page_url: undefined,
                  is_active: "all",
                });
              }}
            >
              清空
            </Button>
          </Space>
          <Space wrap>
            <Typography.Text type="secondary">状态筛选</Typography.Text>
            <Select<ActiveFilter>
              value={activeFilter}
              virtual={false}
              style={{ width: 160 }}
              options={[
                { label: "全部状态", value: "all" },
                { label: "仅生效中", value: "active" },
                { label: "仅已停用", value: "inactive" },
              ]}
              onChange={(value) => updateSearchState({ is_active: value })}
            />
            <Typography.Text type="secondary">时间窗口</Typography.Text>
            <Select<OverviewWindowDays>
              value={windowDays}
              virtual={false}
              style={{ width: 160 }}
              options={[
                { label: "近 7 天", value: 7 },
                { label: "近 14 天", value: 14 },
                { label: "近 30 天", value: 30 },
              ]}
              onChange={(value) => updateSearchState({ window_days: value }, { resetPage: false })}
            />
          </Space>
          <Space wrap>
            <Typography.Text type="secondary">已选择 {selectedRowKeys.length} 条</Typography.Text>
            <Button
              disabled={!selectedRowKeys.length}
              loading={batchMutation.isPending && batchMutation.variables?.isActive === true}
              onClick={() => batchMutation.mutate({ correctionIds: selectedRowKeys, isActive: true })}
            >
              批量启用
            </Button>
            <Button
              disabled={!selectedRowKeys.length}
              loading={batchMutation.isPending && batchMutation.variables?.isActive === false}
              onClick={() => batchMutation.mutate({ correctionIds: selectedRowKeys, isActive: false })}
            >
              批量停用
            </Button>
          </Space>
        </Space>
      </Card>

      {query.isLoading ? <LoadingBlock /> : null}
      {query.isError ? <ErrorBlock message={query.error.message} /> : null}
      {!query.isLoading && !query.isError && !query.data?.length ? (
        <Card>
          <EmptyBlock description="当前筛选条件下没有修正记录。" />
        </Card>
      ) : null}

      {query.data?.length ? (
        <Card>
          <Table
            dataSource={query.data}
            rowKey="id"
            columns={columns}
            pagination={false}
            rowSelection={{
              selectedRowKeys,
              onChange: (keys) => setSelectedRowKeys(keys.map((item) => Number(item))),
            }}
          />
          <div style={{ display: "flex", justifyContent: "flex-end", marginTop: 16 }}>
            <Pagination
              current={page}
              pageSize={PAGE_SIZE}
              total={paginationTotal}
              showSizeChanger={false}
              onChange={(nextPage) => updateSearchState({ page: nextPage }, { resetPage: false })}
            />
          </div>
        </Card>
      ) : null}

      <CorrectionEventsDrawer correction={eventCorrection} onClose={() => setEventCorrection(null)} />
    </>
  );
}
