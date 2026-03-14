import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, Space, Table, Tag, Typography, message } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Link, useNavigate } from "react-router-dom";

import { renderExecutionStatus } from "../components/executionPresentation";
import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { executeSuite, getSuites } from "../services/api";
import type { StoredSuiteSummary } from "../types/api";

function formatTime(value?: string | null) {
  return value ? new Date(value).toLocaleString() : "-";
}

const columns = (
  onExecute: (suiteId: number) => void,
  runningSuiteId: number | null,
): ColumnsType<StoredSuiteSummary> => [
  {
    title: "Suite 名称",
    dataIndex: "name",
    key: "name",
    render: (value: string, record) => (
      <Space direction="vertical" size={2}>
        <Typography.Text strong>{value}</Typography.Text>
        <Typography.Text type="secondary">{record.description || "未填写描述"}</Typography.Text>
      </Space>
    ),
  },
  {
    title: "用例数",
    dataIndex: "case_count",
    key: "case_count",
    width: 120,
    render: (caseCount: number) => <Tag>{caseCount} cases</Tag>,
  },
  {
    title: "最近批次",
    dataIndex: "latest_run",
    key: "latest_run",
    width: 240,
    render: (_value, record) =>
      record.latest_run ? (
        <Space direction="vertical" size={2}>
          <Space size="small">
            {renderExecutionStatus(record.latest_run.status)}
            <Link to={`/suites/${record.id}/runs/${record.latest_run.id}`}>查看历史</Link>
          </Space>
          <Typography.Text type="secondary">{formatTime(record.latest_run.started_at)}</Typography.Text>
        </Space>
      ) : (
        <Typography.Text type="secondary">暂无批次</Typography.Text>
      ),
  },
  {
    title: "操作",
    key: "actions",
    width: 260,
    render: (_, record) => (
      <Space>
        <Button
          type="primary"
          loading={runningSuiteId === record.id}
          onClick={() => onExecute(record.id)}
        >
          执行
        </Button>
        <Button>
          <Link to={`/suites/${record.id}/edit`}>编辑</Link>
        </Button>
        <Button disabled={!record.latest_run}>
          {record.latest_run ? <Link to={`/suites/${record.id}/runs/${record.latest_run.id}`}>历史</Link> : "历史"}
        </Button>
      </Space>
    ),
  },
];

export function SuitesPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [messageApi, contextHolder] = message.useMessage();
  const suitesQuery = useQuery({
    queryKey: ["suites"],
    queryFn: getSuites,
  });
  const executionMutation = useMutation({
    mutationFn: (suiteId: number) => executeSuite(suiteId, { actor_user_id: 1 }),
    onSuccess: async (result) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["suites"] }),
        queryClient.invalidateQueries({ queryKey: ["suite-runs"] }),
        queryClient.invalidateQueries({ queryKey: ["executions"] }),
      ]);
      void messageApi.success("Suite 执行完成");
      void navigate(`/suites/${result.suite_id}/runs/${result.id}`);
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  return (
    <>
      {contextHolder}
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }} wrap>
          <div>
            <h1 className="page-title">Suite 管理</h1>
            <p className="page-subtitle">组合多个 Case，查看最近批次，并从平台内直接回看结果。</p>
          </div>
          <Space>
            <Button>
              <Link to="/executions">执行中心</Link>
            </Button>
            <Button type="primary">
              <Link to="/suites/new">新建 Suite</Link>
            </Button>
          </Space>
        </Space>
      </div>
      <Space direction="vertical" size="large" style={{ width: "100%" }}>
        <Card>
          {suitesQuery.isLoading ? <LoadingBlock /> : null}
          {suitesQuery.isError ? <ErrorBlock message={suitesQuery.error.message} /> : null}
          {suitesQuery.data ? (
            <Table
              rowKey="id"
              pagination={false}
              columns={columns(
                (suiteId) => executionMutation.mutate(suiteId),
                executionMutation.isPending ? executionMutation.variables ?? null : null,
              )}
              dataSource={suitesQuery.data}
            />
          ) : null}
        </Card>
      </Space>
    </>
  );
}
