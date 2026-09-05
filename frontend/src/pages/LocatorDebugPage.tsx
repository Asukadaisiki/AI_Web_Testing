import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Empty, Select, Space, Spin, Tag, Typography } from "antd";
import { Link } from "react-router-dom";

import { LocatorEvidencePanel } from "../components/LocatorEvidencePanel";
import { getExecutionDetail, getExecutions } from "../features/executions/api";
import { getProjects } from "../features/projects/api";
import { WorkspacePageLayout } from "../layouts/WorkspacePageLayout";
import type { ExecutionStatus, StepExecutionEvidence } from "../types/api";

const STATUS_OPTIONS: Array<{ value: "all" | ExecutionStatus; label: string }> = [
  { value: "all", label: "全部状态" },
  { value: "failed", label: "失败" },
  { value: "needs_intervention", label: "需干预" },
  { value: "passed", label: "通过" },
  { value: "running", label: "运行中" },
  { value: "cancelled", label: "已取消" },
];

export function getLocatorSteps(steps: StepExecutionEvidence[]) {
  return steps.filter((step) => step.locator_trace != null);
}

export function LocatorDebugPage() {
  const [projectId, setProjectId] = useState<number | null>(null);
  const [status, setStatus] = useState<"all" | ExecutionStatus>("all");
  const [executionId, setExecutionId] = useState<number | null>(null);
  const [stepIndex, setStepIndex] = useState<number | null>(null);

  const projectsQuery = useQuery({ queryKey: ["projects"], queryFn: getProjects });
  const activeProjectId = projectId ?? projectsQuery.data?.[0]?.id ?? null;
  const executionsQuery = useQuery({
    queryKey: ["locator-debug-executions", activeProjectId, status],
    queryFn: () =>
      getExecutions({
        project_id: activeProjectId!,
        status: status === "all" ? undefined : status,
        limit: 100,
      }),
    enabled: activeProjectId != null,
    refetchInterval: 5000,
  });
  const detailQuery = useQuery({
    queryKey: ["execution-detail", executionId],
    queryFn: () => getExecutionDetail(executionId!),
    enabled: executionId != null,
  });

  useEffect(() => {
    setExecutionId(null);
    setStepIndex(null);
  }, [activeProjectId, status]);

  useEffect(() => {
    if (!executionId && executionsQuery.data?.length) {
      setExecutionId(executionsQuery.data[0].id);
    }
  }, [executionId, executionsQuery.data]);

  const locatorSteps = useMemo(
    () => getLocatorSteps(detailQuery.data?.report?.steps ?? []),
    [detailQuery.data],
  );

  useEffect(() => {
    if (locatorSteps.length && !locatorSteps.some((step) => step.step_index === stepIndex)) {
      setStepIndex(locatorSteps[0].step_index);
    }
  }, [locatorSteps, stepIndex]);

  const selectedStep = locatorSteps.find((step) => step.step_index === stepIndex);

  return (
    <WorkspacePageLayout
      title="定位调试"
      description="从执行 evidence 还原定位决策，并将人工确认结果写入修正库。"
    >
      <section className="workspace-section filter-bar" aria-label="执行筛选">
        <Select
          aria-label="项目筛选"
          value={activeProjectId}
          loading={projectsQuery.isLoading}
          onChange={setProjectId}
          options={(projectsQuery.data ?? []).map((project) => ({
            value: project.id,
            label: project.name,
          }))}
          placeholder="选择项目"
        />
        <Select
          aria-label="状态筛选"
          value={status}
          onChange={setStatus}
          options={STATUS_OPTIONS}
        />
        <Select
          aria-label="执行筛选"
          value={executionId}
          loading={executionsQuery.isLoading}
          onChange={setExecutionId}
          options={(executionsQuery.data ?? []).map((execution) => ({
            value: execution.id,
            label: `#${execution.id} ${execution.case_name}`,
          }))}
          placeholder="选择执行"
          showSearch
          optionFilterProp="label"
        />
        {executionId ? <Link to={`/reports/${executionId}`}>打开完整报告</Link> : null}
      </section>

      {executionsQuery.isError ? (
        <Alert type="error" showIcon message={executionsQuery.error.message} />
      ) : detailQuery.isLoading ? (
        <div className="workspace-loading"><Spin /></div>
      ) : !detailQuery.data ? (
        <Empty description="选择一条执行记录开始调试" />
      ) : (
        <div className="debug-grid">
          <section className="workspace-section step-selector">
            <Typography.Title level={4}>定位步骤</Typography.Title>
            {locatorSteps.length ? (
              locatorSteps.map((step) => (
                <button
                  type="button"
                  key={step.step_index}
                  className={step.step_index === stepIndex ? "step-debug-row active" : "step-debug-row"}
                  onClick={() => setStepIndex(step.step_index)}
                >
                  <span>
                    <strong>Step {step.step_index + 1}</strong>
                    <small>{step.action} / {step.target || "-"}</small>
                  </span>
                  <Tag color={step.status === "passed" ? "success" : "error"}>
                    {step.status === "passed" ? "PASS" : "FAIL"}
                  </Tag>
                </button>
              ))
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="该执行没有定位 evidence" />
            )}
          </section>
          <section className="workspace-section">
            <div className="section-heading">
              <div>
                <Typography.Title level={4}>决策证据</Typography.Title>
                <Space>
                  <Typography.Text type="secondary">
                    执行 #{detailQuery.data.id}
                  </Typography.Text>
                  <Typography.Text type="secondary">
                    {detailQuery.data.case_name}
                  </Typography.Text>
                </Space>
              </div>
            </div>
            {selectedStep ? (
              <LocatorEvidencePanel execution={detailQuery.data} step={selectedStep} />
            ) : (
              <Empty description="选择一个定位步骤" />
            )}
          </section>
        </div>
      )}
    </WorkspacePageLayout>
  );
}
