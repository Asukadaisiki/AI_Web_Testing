import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Checkbox, Empty, Input, Popconfirm, Space, Tag, Typography, message } from "antd";
import { Link, useNavigate } from "react-router-dom";
import { SearchOutlined, PlusOutlined, DeleteOutlined } from "@ant-design/icons";
import { useState, useMemo } from "react";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { NotebookLMLayout } from "../layouts/NotebookLMLayout";
import { executeCase, getCases, deleteCase, batchDeleteCases } from "../services/api";
import type { StoredCaseSummary } from "../types/api";

const statusTags = [
  { key: "all", label: "全部" },
  { key: "pending", label: "待执行" },
  { key: "passed", label: "已通过" },
  { key: "failed", label: "已失败" },
];

export function CasesPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [messageApi, contextHolder] = message.useMessage();
  const [searchText, setSearchText] = useState("");
  const [statusFilter, setStatusFilter] = useState<string>("all");
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());

  const casesQuery = useQuery({
    queryKey: ["cases"],
    queryFn: getCases,
  });

  const executionMutation = useMutation({
    mutationFn: (caseId: number) => executeCase(caseId, { actor_user_id: 1 }),
    onSuccess: (execution) => {
      queryClient.invalidateQueries({ queryKey: ["executions"] });
      void navigate(`/run/${execution.id}`);
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (caseId: number) => deleteCase(caseId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["cases"] });
      void messageApi.success("用例已删除");
    },
    onError: (error: Error) => {
      void messageApi.error("删除失败: " + error.message);
    },
  });

  const batchDeleteMutation = useMutation({
    mutationFn: (ids: number[]) => batchDeleteCases(ids),
    onSuccess: () => {
      setSelectedIds(new Set());
      queryClient.invalidateQueries({ queryKey: ["cases"] });
      void messageApi.success("批量删除成功");
    },
    onError: (error: Error) => {
      void messageApi.error("批量删除失败: " + error.message);
    },
  });

  const allCases = casesQuery.data?.items ?? [];

  const filteredCases = useMemo(() => {
    let cases = allCases;
    if (searchText.trim()) {
      const q = searchText.toLowerCase();
      cases = cases.filter(
        (c) =>
          c.name.toLowerCase().includes(q) ||
          (c.description || "").toLowerCase().includes(q),
      );
    }
    return cases;
  }, [allCases, searchText]);

  const totalSteps = useMemo(
    () => allCases.reduce((sum, c) => sum + c.steps.length, 0),
    [allCases],
  );

  const toggleSelect = (id: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleSelectAll = () => {
    if (selectedIds.size === filteredCases.length) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(filteredCases.map((c) => c.id)));
    }
  };

  /* ---- Left Panel ---- */
  const leftPanel = (
    <div style={{ display: "flex", flexDirection: "column", gap: 16, flex: 1 }}>
      <Typography.Text strong style={{ fontSize: 14 }}>
        Cases
      </Typography.Text>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        {allCases.length} 个用例
      </Typography.Text>

      <Input
        placeholder="搜索用例..."
        prefix={<SearchOutlined />}
        value={searchText}
        onChange={(e) => setSearchText(e.target.value)}
        allowClear
        style={{
          borderRadius: 24,
          background: "#F0F4F8",
          border: "none",
          boxShadow: "none",
        }}
      />

      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
        {statusTags.map((tag) => (
          <Tag
            key={tag.key}
            onClick={() => setStatusFilter(tag.key)}
            style={{
              cursor: "pointer",
              borderRadius: 12,
              padding: "2px 12px",
              border: "none",
              background:
                statusFilter === tag.key ? "#1a1a2e" : "#F0F4F8",
              color: statusFilter === tag.key ? "#fff" : "#666",
              fontWeight: statusFilter === tag.key ? 600 : 400,
            }}
          >
            {tag.label}
          </Tag>
        ))}
      </div>

      <div style={{ marginTop: "auto" }}>
        <Link to="/cases/new" style={{ textDecoration: "none" }}>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            block
            style={{
              background: "#1a1a2e",
              borderColor: "#1a1a2e",
              borderRadius: 8,
            }}
          >
            新建用例
          </Button>
        </Link>
      </div>
    </div>
  );

  /* ---- Center Panel ---- */
  const centerPanel = (
    <div style={{ padding: 20, overflowY: "auto", flex: 1 }}>
      {casesQuery.isLoading && <LoadingBlock />}
      {casesQuery.isError && (
        <ErrorBlock message={casesQuery.error.message} />
      )}
      {!casesQuery.isLoading && !casesQuery.isError && filteredCases.length === 0 && (
        <Empty description="暂无用例" />
      )}
      {!casesQuery.isLoading && !casesQuery.isError && filteredCases.length > 0 && (
        <>
          {/* Batch action bar */}
          {selectedIds.size > 0 && (
            <div style={{
              marginBottom: 12,
              padding: "8px 12px",
              background: "#e6f4ff",
              borderRadius: 8,
              display: "flex",
              alignItems: "center",
              gap: 12,
            }}>
              <Checkbox
                checked={selectedIds.size === filteredCases.length}
                indeterminate={selectedIds.size > 0 && selectedIds.size < filteredCases.length}
                onChange={toggleSelectAll}
              >
                已选 {selectedIds.size} 项
              </Checkbox>
              <Popconfirm
                title={`确定删除选中的 ${selectedIds.size} 个用例？`}
                onConfirm={() => batchDeleteMutation.mutate(Array.from(selectedIds))}
                okText="删除"
                cancelText="取消"
                okButtonProps={{ danger: true }}
              >
                <Button size="small" danger icon={<DeleteOutlined />}>
                  批量删除
                </Button>
              </Popconfirm>
            </div>
          )}
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "1fr 1fr",
              gap: 12,
            }}
          >
            {filteredCases.map((c: StoredCaseSummary) => (
              <div
                key={c.id}
                className="nb-card"
                style={{
                  padding: 16,
                  display: "flex",
                  flexDirection: "column",
                  gap: 8,
                  border: selectedIds.has(c.id) ? "2px solid #1677ff" : undefined,
                }}
              >
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <Checkbox
                    checked={selectedIds.has(c.id)}
                    onChange={() => toggleSelect(c.id)}
                  />
                  <Typography.Text strong style={{ fontSize: 14, flex: 1 }}>
                    {c.name}
                  </Typography.Text>
                </div>
                <Typography.Text
                  type="secondary"
                  style={{ fontSize: 12 }}
                  ellipsis
                >
                  {c.description || "未填写描述"}
                </Typography.Text>
                <Typography.Text
                  type="secondary"
                  style={{ fontSize: 12 }}
                  ellipsis
                >
                  {c.base_url || "未配置"}
                </Typography.Text>
                <Tag>{c.steps.length} steps</Tag>
                <div style={{ marginTop: "auto" }}>
                  <Space>
                    <Button
                      type="primary"
                      size="small"
                      loading={
                        executionMutation.isPending &&
                        executionMutation.variables === c.id
                      }
                      onClick={() => executionMutation.mutate(c.id)}
                    >
                      执行
                    </Button>
                    <Button type="link" size="small">
                      <Link to={`/cases/${c.id}/edit`}>编辑</Link>
                    </Button>
                    <Popconfirm
                      title="确定删除此用例？"
                      onConfirm={() => deleteMutation.mutate(c.id)}
                      okText="删除"
                      cancelText="取消"
                      okButtonProps={{ danger: true }}
                    >
                      <Button type="link" size="small" danger>
                        删除
                      </Button>
                    </Popconfirm>
                  </Space>
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );

  /* ---- Right Cards ---- */
  const rightCards = [
    /* Card 1: 统计 */
    <div key="stats">
      <Typography.Text strong style={{ fontSize: 14, display: "block", marginBottom: 12 }}>
        统计
      </Typography.Text>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
          }}
        >
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            用例总数
          </Typography.Text>
          <Typography.Text strong style={{ fontSize: 14 }}>
            {allCases.length}
          </Typography.Text>
        </div>
        <div
          style={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
          }}
        >
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            步骤总数
          </Typography.Text>
          <Typography.Text strong style={{ fontSize: 14 }}>
            {totalSteps}
          </Typography.Text>
        </div>
      </div>
    </div>,

    /* Card 2: 快速操作 */
    <div key="quick-actions">
      <Typography.Text strong style={{ fontSize: 14, display: "block", marginBottom: 12 }}>
        快速操作
      </Typography.Text>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <Link to="/" style={{ fontSize: 13 }}>
          返回 AI 规划
        </Link>
        <Link to="/cases/new" style={{ fontSize: 13 }}>
          手动补充/编辑
        </Link>
      </div>
    </div>,
  ];

  return (
    <>
      {contextHolder}
      <NotebookLMLayout
        leftPanel={leftPanel}
        centerPanel={centerPanel}
        rightCards={rightCards}
      />
    </>
  );
}
