import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Checkbox,
  Empty,
  InputNumber,
  Progress,
  Select,
  Space,
  Spin,
  Tag,
  Typography,
  message,
} from "antd";
import { Link } from "react-router-dom";

import {
  cancelExecutionBatch,
  createExecutionBatch,
  getExecutionBatchReport,
  getExecutionBatches,
} from "../features/executions/api";
import { getCases } from "../features/cases/api";
import { getProjects } from "../features/projects/api";
import { WorkspacePageLayout } from "../layouts/WorkspacePageLayout";
import type {
  ExecutionBatchReport,
  ExecutionBatchStatus,
} from "../types/api";

const ACTIVE_STATUSES: ExecutionBatchStatus[] = ["pending", "running"];
const STATUS_META: Record<ExecutionBatchStatus, { label: string; color: string }> = {
  pending: { label: "等待中", color: "default" },
  running: { label: "执行中", color: "processing" },
  passed: { label: "通过", color: "success" },
  failed: { label: "失败", color: "error" },
  needs_intervention: { label: "需干预", color: "warning" },
  cancelled: { label: "已取消", color: "default" },
};

function formatTime(value?: string | null) {
  return value ? new Date(value).toLocaleString("zh-CN") : "-";
}

function BatchDetails({ report }: { report: ExecutionBatchReport }) {
  return (
    <section className="workspace-section" aria-label="批次详情">
      <div className="section-heading">
        <div>
          <Typography.Title level={4}>批次 #{report.id}</Typography.Title>
          <Typography.Text type="secondary">
            创建于 {formatTime(report.created_at)}，并发度 {report.concurrency_limit}
          </Typography.Text>
        </div>
        <Tag color={STATUS_META[report.status].color}>
          {STATUS_META[report.status].label}
        </Tag>
      </div>
      <Progress
        percent={Math.round(
          report.total_jobs ? (report.completed_jobs / report.total_jobs) * 100 : 0,
        )}
        status={report.failed_jobs || report.intervention_jobs ? "exception" : "active"}
        format={() => `${report.completed_jobs}/${report.total_jobs}`}
      />
      <div className="metric-strip">
        <span>通过 {report.passed_jobs}</span>
        <span>失败 {report.failed_jobs}</span>
        <span>需干预 {report.intervention_jobs}</span>
        <span>取消 {report.cancelled_jobs}</span>
        <span>通过率 {(report.pass_rate * 100).toFixed(0)}%</span>
      </div>
      <div className="run-list">
        {report.jobs.map((job) => (
          <div className="run-row" key={job.id}>
            <div>
              <Typography.Text strong>{job.case_name}</Typography.Text>
              <Typography.Text type="secondary">
                尝试 {job.attempt_count}/{job.max_attempts}
              </Typography.Text>
            </div>
            <Tag color={STATUS_META[job.status].color}>
              {STATUS_META[job.status].label}
            </Tag>
            {job.latest_execution ? (
              <Link to={`/reports/${job.latest_execution.id}`}>查看 run 报告</Link>
            ) : (
              <Typography.Text type="secondary">尚未生成报告</Typography.Text>
            )}
          </div>
        ))}
      </div>
    </section>
  );
}

export function RegressionPage() {
  const queryClient = useQueryClient();
  const [messageApi, contextHolder] = message.useMessage();
  const [projectId, setProjectId] = useState<number | null>(null);
  const [selectedCaseIds, setSelectedCaseIds] = useState<number[]>([]);
  const [concurrency, setConcurrency] = useState(2);
  const [selectedBatchId, setSelectedBatchId] = useState<number | null>(null);

  const projectsQuery = useQuery({ queryKey: ["projects"], queryFn: getProjects });
  const activeProjectId = projectId ?? projectsQuery.data?.[0]?.id ?? null;
  const casesQuery = useQuery({
    queryKey: ["cases", activeProjectId],
    queryFn: () => getCases({ project_id: activeProjectId! }),
    enabled: activeProjectId != null,
  });
  const batchesQuery = useQuery({
    queryKey: ["execution-batches", activeProjectId],
    queryFn: () => getExecutionBatches(activeProjectId!),
    enabled: activeProjectId != null,
    refetchInterval: (query) =>
      query.state.data?.some((batch) => ACTIVE_STATUSES.includes(batch.status))
        ? 2000
        : false,
  });
  const reportQuery = useQuery({
    queryKey: ["execution-batch-report", selectedBatchId],
    queryFn: () => getExecutionBatchReport(selectedBatchId!),
    enabled: selectedBatchId != null,
    refetchInterval: (query) =>
      query.state.data && ACTIVE_STATUSES.includes(query.state.data.status)
        ? 2000
        : false,
  });

  useEffect(() => {
    setSelectedCaseIds([]);
    setSelectedBatchId(null);
  }, [activeProjectId]);

  useEffect(() => {
    if (!selectedBatchId && batchesQuery.data?.length) {
      setSelectedBatchId(batchesQuery.data[0].id);
    }
  }, [batchesQuery.data, selectedBatchId]);

  const createMutation = useMutation({
    mutationFn: () =>
      createExecutionBatch({
        project_id: activeProjectId!,
        case_ids: selectedCaseIds,
        concurrency_limit: concurrency,
        idempotency_key: crypto.randomUUID(),
      }),
    onSuccess: async (batch) => {
      setSelectedBatchId(batch.id);
      await queryClient.invalidateQueries({
        queryKey: ["execution-batches", activeProjectId],
      });
      void messageApi.success(`回归批次 #${batch.id} 已启动`);
    },
    onError: (error: Error) => void messageApi.error(error.message),
  });
  const cancelMutation = useMutation({
    mutationFn: (batchId: number) => cancelExecutionBatch(batchId),
    onSuccess: async (batch) => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ["execution-batches", activeProjectId],
        }),
        queryClient.invalidateQueries({
          queryKey: ["execution-batch-report", batch.id],
        }),
      ]);
      void messageApi.info(`批次 #${batch.id} 已请求取消`);
    },
    onError: (error: Error) => void messageApi.error(error.message),
  });

  const cases = casesQuery.data?.items ?? [];
  const allSelected = cases.length > 0 && selectedCaseIds.length === cases.length;
  const selectedBatch = useMemo(
    () => batchesQuery.data?.find((batch) => batch.id === selectedBatchId),
    [batchesQuery.data, selectedBatchId],
  );

  return (
    <WorkspacePageLayout
      title="项目回归编排"
      description="选择项目用例与并发度，启动可追踪、可取消的执行批次。"
    >
      {contextHolder}
      <div className="workspace-grid">
        <section className="workspace-section">
          <Typography.Title level={4}>执行配置</Typography.Title>
          <Space direction="vertical" size="large" style={{ width: "100%" }}>
            <label>
              <Typography.Text strong>项目</Typography.Text>
              <Select
                aria-label="选择项目"
                value={activeProjectId}
                loading={projectsQuery.isLoading}
                onChange={setProjectId}
                style={{ width: "100%", marginTop: 8 }}
                options={(projectsQuery.data ?? []).map((project) => ({
                  value: project.id,
                  label: project.name,
                }))}
                placeholder="选择项目"
              />
            </label>
            <div>
              <div className="section-heading compact">
                <Typography.Text strong>用例</Typography.Text>
                <Checkbox
                  checked={allSelected}
                  indeterminate={selectedCaseIds.length > 0 && !allSelected}
                  onChange={(event) =>
                    setSelectedCaseIds(
                      event.target.checked ? cases.map((item) => item.id) : [],
                    )
                  }
                >
                  全选
                </Checkbox>
              </div>
              {casesQuery.isLoading ? (
                <Spin />
              ) : cases.length ? (
                <Checkbox.Group
                  className="case-picker"
                  value={selectedCaseIds}
                  onChange={(values) => setSelectedCaseIds(values as number[])}
                  options={cases.map((item) => ({
                    value: item.id,
                    label: `${item.name} (${item.steps.length} 步)`,
                  }))}
                />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="项目下暂无用例" />
              )}
            </div>
            <label>
              <Typography.Text strong>并发度</Typography.Text>
              <InputNumber
                aria-label="并发度"
                min={1}
                max={16}
                value={concurrency}
                onChange={(value) => setConcurrency(value ?? 1)}
                style={{ width: "100%", marginTop: 8 }}
              />
            </label>
            <Button
              type="primary"
              block
              loading={createMutation.isPending}
              disabled={!activeProjectId || selectedCaseIds.length === 0}
              onClick={() => createMutation.mutate()}
            >
              启动回归
            </Button>
          </Space>
        </section>

        <section className="workspace-section">
          <div className="section-heading">
            <Typography.Title level={4}>执行批次</Typography.Title>
            {batchesQuery.isFetching ? <Spin size="small" /> : null}
          </div>
          {batchesQuery.isError ? (
            <Alert type="error" showIcon message={batchesQuery.error.message} />
          ) : batchesQuery.data?.length ? (
            <div className="batch-list">
              {batchesQuery.data.map((batch) => (
                <button
                  type="button"
                  key={batch.id}
                  className={batch.id === selectedBatchId ? "batch-row active" : "batch-row"}
                  onClick={() => setSelectedBatchId(batch.id)}
                >
                  <span>
                    <strong>#{batch.id}</strong>
                    <small>{formatTime(batch.created_at)}</small>
                  </span>
                  <span>{batch.passed_jobs}/{batch.total_jobs} 通过</span>
                  <Tag color={STATUS_META[batch.status].color}>
                    {STATUS_META[batch.status].label}
                  </Tag>
                </button>
              ))}
            </div>
          ) : (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无回归批次" />
          )}
          {selectedBatch && ACTIVE_STATUSES.includes(selectedBatch.status) ? (
            <Button
              danger
              style={{ marginTop: 16 }}
              loading={cancelMutation.isPending}
              onClick={() => cancelMutation.mutate(selectedBatch.id)}
            >
              取消当前批次
            </Button>
          ) : null}
        </section>
      </div>
      {reportQuery.isLoading ? (
        <div className="workspace-loading"><Spin /></div>
      ) : reportQuery.data ? (
        <BatchDetails report={reportQuery.data} />
      ) : null}
    </WorkspacePageLayout>
  );
}
