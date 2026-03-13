import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Card, Descriptions, Empty, Space, Table, Tag, Typography, message } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Link, useNavigate, useParams } from "react-router-dom";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { getSuiteRunDetail, rerunFailedSuiteRun } from "../services/api";
import type { ContextVariableReadEvidence, ContextVariableWriteEvidence, StoredSuiteRunItem } from "../types/api";

function renderStatus(status: "running" | "passed" | "failed") {
  const color = {
    running: "processing",
    passed: "success",
    failed: "error",
  }[status];
  const label = {
    running: "运行中",
    passed: "通过",
    failed: "失败",
  }[status];
  return <Tag color={color}>{label}</Tag>;
}

function formatTime(value?: string | null) {
  return value ? new Date(value).toLocaleString() : "-";
}

function renderReadEvidence(reads: ContextVariableReadEvidence[] = []) {
  if (!reads.length) {
    return "-";
  }

  return (
    <Space direction="vertical" size={2}>
      {reads.map((item) => (
        <Typography.Text key={`${item.context_key}-${item.name}`}>
          {item.context_key} / {item.value_type} / {item.resolved ? "已解析" : "未解析"}
          {item.source_suite_run_id ? ` / 来源批次 #${item.source_suite_run_id}` : ""}
          {item.error_message ? ` / ${item.error_message}` : ""}
        </Typography.Text>
      ))}
    </Space>
  );
}

function renderWriteEvidence(writes: ContextVariableWriteEvidence[] = []) {
  if (!writes.length) {
    return "-";
  }

  return (
    <Space direction="vertical" size={2}>
      {writes.map((item) => (
        <Typography.Text key={`${item.context_key}-${item.name}`}>
          {item.context_key} / {item.value_type} / {item.source || "-"} / {item.status}
          {item.error_message ? ` / ${item.error_message}` : ""}
        </Typography.Text>
      ))}
    </Space>
  );
}

const itemColumns: ColumnsType<StoredSuiteRunItem> = [
  {
    title: "顺序",
    dataIndex: "order_index",
    key: "order_index",
    width: 80,
  },
  {
    title: "用例",
    dataIndex: "case_name_snapshot",
    key: "case_name_snapshot",
    render: (value: string, record) => (
      <Space direction="vertical" size={2}>
        <Typography.Text strong>{value}</Typography.Text>
        <Typography.Text type="secondary">Case ID: {record.case_id}</Typography.Text>
      </Space>
    ),
  },
  {
    title: "状态",
    dataIndex: "status",
    key: "status",
    width: 100,
    render: renderStatus,
  },
  {
    title: "执行详情",
    dataIndex: "execution_id",
    key: "execution_id",
    width: 160,
    render: (value: number) => <Link to={`/executions/${value}`}>#{value}</Link>,
  },
  {
    title: "上下文读取",
    dataIndex: "context_reads",
    key: "context_reads",
    render: (value: ContextVariableReadEvidence[]) => renderReadEvidence(value),
  },
  {
    title: "上下文写入",
    dataIndex: "context_writes",
    key: "context_writes",
    render: (value: ContextVariableWriteEvidence[]) => renderWriteEvidence(value),
  },
  {
    title: "解析结果",
    dataIndex: "context_resolution_error",
    key: "context_resolution_error",
    render: (value?: string | null) => value || "-",
  },
];

export function SuiteRunDetailPage() {
  const { suiteId, runId } = useParams<{ suiteId: string; runId: string }>();
  const numericSuiteId = Number(suiteId);
  const numericRunId = Number(runId);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [messageApi, contextHolder] = message.useMessage();

  const runQuery = useQuery({
    queryKey: ["suite-run-detail", numericSuiteId, numericRunId],
    queryFn: () => getSuiteRunDetail(numericSuiteId, numericRunId),
    enabled: Number.isFinite(numericSuiteId) && Number.isFinite(numericRunId),
  });

  const rerunMutation = useMutation({
    mutationFn: () => rerunFailedSuiteRun(numericSuiteId, numericRunId, { actor_user_id: 1 }),
    onSuccess: async (result) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["suites"] }),
        queryClient.invalidateQueries({ queryKey: ["suite-detail", String(numericSuiteId)] }),
        queryClient.invalidateQueries({ queryKey: ["suite-runs", String(numericSuiteId)] }),
        queryClient.invalidateQueries({ queryKey: ["executions"] }),
      ]);
      void messageApi.success("失败项重跑已启动");
      void navigate(`/suites/${numericSuiteId}/runs/${result.id}`);
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  if (runQuery.isLoading) {
    return <LoadingBlock />;
  }

  if (runQuery.isError) {
    return <ErrorBlock message={runQuery.error.message} />;
  }

  if (!runQuery.data) {
    return <Empty description="Suite 批次不存在。" />;
  }

  const run = runQuery.data;
  const hasFailedItems = run.items.some((item) => item.status === "failed");
  const contextSnapshot = run.context_snapshot ?? {};

  return (
    <>
      {contextHolder}
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }} wrap>
          <div>
            <h1 className="page-title">{run.suite_name}</h1>
            <p className="page-subtitle">查看 Suite 批次摘要、子执行结果与失败重跑入口。</p>
          </div>
          <Space wrap>
            <Button>
              <Link to={`/suites/${numericSuiteId}/edit`}>返回 Suite</Link>
            </Button>
            <Button
              type="primary"
              loading={rerunMutation.isPending}
              disabled={!hasFailedItems}
              onClick={() => rerunMutation.mutate()}
            >
              重跑失败项
            </Button>
          </Space>
        </Space>
      </div>

      <Space direction="vertical" size="large" style={{ width: "100%" }}>
        {!hasFailedItems ? (
          <Alert type="info" showIcon message="当前批次没有失败项，可直接查看历史结果。" />
        ) : null}

        <Card>
            <Descriptions bordered column={2}>
              <Descriptions.Item label="批次编号">#{run.id}</Descriptions.Item>
              <Descriptions.Item label="状态">{renderStatus(run.status)}</Descriptions.Item>
              <Descriptions.Item label="触发来源">{run.source}</Descriptions.Item>
            <Descriptions.Item label="来源批次">
              {run.source_suite_run_id ? (
                <Link to={`/suites/${numericSuiteId}/runs/${run.source_suite_run_id}`}>#{run.source_suite_run_id}</Link>
              ) : (
                "-"
              )}
            </Descriptions.Item>
              <Descriptions.Item label="开始时间">{formatTime(run.started_at)}</Descriptions.Item>
              <Descriptions.Item label="结束时间">{formatTime(run.finished_at)}</Descriptions.Item>
              <Descriptions.Item label="总用例数">{run.total_cases}</Descriptions.Item>
              <Descriptions.Item label="覆盖 Base URL">{run.base_url_override || "-"}</Descriptions.Item>
              <Descriptions.Item label="上下文来源">{run.context_source}</Descriptions.Item>
              <Descriptions.Item label="来源上下文批次">
                {run.context_source_suite_run_id ? (
                  <Link to={`/suites/${numericSuiteId}/runs/${run.context_source_suite_run_id}`}>
                    #{run.context_source_suite_run_id}
                  </Link>
                ) : (
                  "-"
                )}
              </Descriptions.Item>
              <Descriptions.Item label="重跑上下文模式" span={2}>
                {run.rerun_context_mode}
              </Descriptions.Item>
              <Descriptions.Item label="通过 / 失败" span={2}>
                {run.passed_cases} / {run.failed_cases}
              </Descriptions.Item>
            </Descriptions>
          </Card>

          <Card title="上下文快照">
            {Object.keys(contextSnapshot).length ? (
              <Descriptions bordered column={1} size="small">
                {Object.entries(contextSnapshot).map(([key, value]) => (
                  <Descriptions.Item key={key} label={key}>
                    <Typography.Text code>{JSON.stringify(value)}</Typography.Text>
                  </Descriptions.Item>
                ))}
              </Descriptions>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前批次没有可展示的上下文变量。" />
            )}
          </Card>

          <Card title="子执行列表">
            <Table rowKey="id" pagination={false} columns={itemColumns} dataSource={run.items} />
          </Card>
      </Space>
    </>
  );
}
