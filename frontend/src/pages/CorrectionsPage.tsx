import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, Input, Pagination, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Link, useSearchParams } from "react-router-dom";

import { EmptyBlock, ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { getCorrections, updateCorrectionState } from "../services/api";
import type { StoredLocatorCorrection } from "../types/api";

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

function formatCorrectionType(value: StoredLocatorCorrection["correction_type"]) {
  if (value === "test_id") {
    return "Test ID";
  }
  if (value === "xpath") {
    return "XPath";
  }
  return "CSS";
}

function buildColumns(
  onToggle: (record: StoredLocatorCorrection) => void,
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
      width: 120,
      render: (_, record) => (
        <Button
          size="small"
          loading={togglingId === record.id}
          onClick={() => onToggle(record)}
        >
          {record.is_active ? "停用" : "启用"}
        </Button>
      ),
    },
  ];
}

export function CorrectionsPage() {
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const page = parsePage(searchParams.get("page"));
  const activeFilter = parseActiveFilter(searchParams.get("is_active"));
  const targetDescription = searchParams.get("target_description")?.trim() || "";
  const pageUrl = searchParams.get("page_url")?.trim() || "";
  const [targetDraft, setTargetDraft] = useState(targetDescription);
  const [pageUrlDraft, setPageUrlDraft] = useState(pageUrl);

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

  const toggleMutation = useMutation({
    mutationFn: (record: StoredLocatorCorrection) =>
      updateCorrectionState(record.id, { is_active: !record.is_active }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["corrections"] });
    },
  });

  const updateSearchState = (
    updates: {
      target_description?: string;
      page_url?: string;
      is_active?: ActiveFilter;
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

  const columns = useMemo(
    () => buildColumns((record) => toggleMutation.mutate(record), toggleMutation.variables?.id ?? null),
    [toggleMutation],
  );
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
          <Space direction="vertical" size="large" style={{ width: "100%" }}>
            <Table rowKey="id" pagination={false} columns={columns} dataSource={query.data} />
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
          </Space>
        </Card>
      ) : null}
    </>
  );
}
