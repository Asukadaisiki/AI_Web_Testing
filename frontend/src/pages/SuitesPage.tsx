import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Card, Space, Table, Tag, Typography, message } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Link } from "react-router-dom";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { executeSuite, getSuites } from "../services/api";
import type { StoredSuiteSummary, SuiteExecutionResult } from "../types/api";

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
    title: "项目",
    dataIndex: "project_id",
    key: "project_id",
    width: 90,
  },
  {
    title: "操作",
    key: "actions",
    width: 240,
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
      </Space>
    ),
  },
];

function SuiteExecutionSummary({ result }: { result: SuiteExecutionResult }) {
  return (
    <Alert
      type={result.status === "passed" ? "success" : "warning"}
      showIcon
      message={`Suite 执行完成：${result.suite_name}`}
      description={
        <Space direction="vertical" size="small" style={{ width: "100%" }}>
          <Typography.Text>
            共 {result.total_cases} 条，用例通过 {result.passed_cases} 条，失败 {result.failed_cases} 条。
          </Typography.Text>
          <Space wrap>
            {result.executions.map((item) => (
              <Link key={item.execution_id} to={`/executions/${item.execution_id}`}>
                {item.case_name} ({item.status})
              </Link>
            ))}
          </Space>
        </Space>
      }
    />
  );
}

export function SuitesPage() {
  const queryClient = useQueryClient();
  const [messageApi, contextHolder] = message.useMessage();
  const [latestExecution, setLatestExecution] = useState<SuiteExecutionResult | null>(null);
  const suitesQuery = useQuery({
    queryKey: ["suites"],
    queryFn: getSuites,
  });
  const executionMutation = useMutation({
    mutationFn: (suiteId: number) => executeSuite(suiteId, { actor_user_id: 1 }),
    onSuccess: (result) => {
      setLatestExecution(result);
      void queryClient.invalidateQueries({ queryKey: ["executions"] });
      void messageApi.success("Suite 执行完成。");
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
            <p className="page-subtitle">组合多个 Case，作为一次最小批量执行单元。</p>
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
        {latestExecution ? <SuiteExecutionSummary result={latestExecution} /> : null}
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
