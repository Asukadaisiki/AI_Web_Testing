import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, Space, Table, Tag, Typography, message } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Link, useNavigate } from "react-router-dom";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { executeCase, getCases } from "../services/api";
import type { StoredCaseSummary } from "../types/api";

const columns = (
  onExecute: (caseId: number) => void,
  runningCaseId: number | null,
): ColumnsType<StoredCaseSummary> => [
  {
    title: "用例名称",
    dataIndex: "name",
    key: "name",
    render: (value: string, record) => (
      <Space direction="vertical" size={2}>
        <Typography.Text strong>{value}</Typography.Text>
        <Typography.Text type="secondary">{record.description || "未填写描述"}</Typography.Text>
        <Typography.Text type="secondary">Base URL：{record.base_url || "未配置"}</Typography.Text>
      </Space>
    ),
  },
  {
    title: "步骤数",
    dataIndex: "steps",
    key: "steps",
    width: 110,
    render: (steps) => <Tag>{steps.length} steps</Tag>,
  },
  {
    title: "项目",
    dataIndex: "project_id",
    key: "project_id",
    width: 90,
  },
  {
    title: "操作",
    key: "actions",
    width: 220,
    render: (_, record) => (
      <Space>
        <Button
          type="primary"
          loading={runningCaseId === record.id}
          onClick={() => onExecute(record.id)}
        >
          执行
        </Button>
        <Button>
          <Link to={`/cases/${record.id}/edit`}>编辑</Link>
        </Button>
      </Space>
    ),
  },
];

export function CasesPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [messageApi, contextHolder] = message.useMessage();
  const casesQuery = useQuery({
    queryKey: ["cases"],
    queryFn: getCases,
  });
  const executionMutation = useMutation({
    mutationFn: (caseId: number) => executeCase(caseId, { actor_user_id: 1 }),
    onSuccess: (execution) => {
      queryClient.invalidateQueries({ queryKey: ["executions"] });
      void navigate(`/executions/${execution.id}`);
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  return (
    <>
      {contextHolder}
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }}>
          <div>
            <h1 className="page-title">用例列表</h1>
            <p className="page-subtitle">展示已落库的 DSL 用例，并直接触发后端同步执行。</p>
          </div>
          <Space>
            <Button>
              <Link to="/executions">执行中心</Link>
            </Button>
            <Button type="primary">
              <Link to="/cases/new">新建用例</Link>
            </Button>
          </Space>
        </Space>
      </div>
      <Card>
        {casesQuery.isLoading ? <LoadingBlock /> : null}
        {casesQuery.isError ? <ErrorBlock message={casesQuery.error.message} /> : null}
        {casesQuery.data ? (
          <Table
            rowKey="id"
            pagination={false}
            columns={columns(
              (caseId) => executionMutation.mutate(caseId),
              executionMutation.isPending ? executionMutation.variables ?? null : null,
            )}
            dataSource={casesQuery.data?.items ?? []}
          />
        ) : null}
      </Card>
    </>
  );
}
