import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Spin, Empty, Typography, Tag } from "antd";

import { NotebookLMLayout } from "../layouts/NotebookLMLayout";
import { getProjects, getExecutionOverview, getExecutions, getExecutionDetail } from "../services/api";
import type { ProjectSummary, StoredCaseExecutionSummary, StepExecutionEvidence, ExecutionStatus } from "../types/api";

const { Text, Title } = Typography;

const STATUS_ICON: Record<ExecutionStatus, string> = {
  passed: "✅",
  failed: "❌",
  running: "⏳",
  needs_intervention: "⚠️",
};

function formatTime(iso: string | null) {
  if (!iso) return "-";
  return new Date(iso).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function StepRow({ step }: { step: StepExecutionEvidence }) {
  const [showScreenshot, setShowScreenshot] = useState(false);
  const isFailed = step.status === "failed";

  return (
    <div
      style={{
        display: "flex",
        alignItems: "flex-start",
        gap: 8,
        padding: "6px 0",
        borderLeft: `3px solid ${isFailed ? "#ff4d4f" : "#52c41a"}`,
        paddingLeft: 8,
        marginBottom: 4,
      }}
    >
      <span style={{ fontSize: 12 }}>{isFailed ? "✗" : "✓"}</span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <Text style={{ fontSize: 12 }}>
            Step {step.step_index + 1}: <Text code style={{ fontSize: 11 }}>{step.action}</Text>
            {step.target && (
              <Text type="secondary" style={{ fontSize: 11 }}> {step.target}</Text>
            )}
          </Text>
          {step.duration_ms != null && (
            <Text type="secondary" style={{ fontSize: 11 }}>({step.duration_ms}ms)</Text>
          )}
        </div>

        {isFailed && step.error_message && (
          <div
            style={{
              marginTop: 4,
              padding: "4px 8px",
              background: "#fff2f0",
              borderRadius: 6,
              fontSize: 12,
              color: "#cf1322",
            }}
          >
            {step.error_message}
          </div>
        )}

        {step.locator_trace?.failure_reason && (
          <Text type="secondary" style={{ fontSize: 11, display: "block", marginTop: 2 }}>
            定位失败: {step.locator_trace.failure_reason}
          </Text>
        )}

        {step.screenshot_url && (
          <div style={{ marginTop: 4 }}>
            <a
              onClick={(e) => {
                e.stopPropagation();
                setShowScreenshot(!showScreenshot);
              }}
              style={{ fontSize: 11, cursor: "pointer" }}
            >
              {showScreenshot ? "收起截图" : "查看截图"}
            </a>
            {showScreenshot && (
              <img
                src={step.screenshot_url}
                alt={`Step ${step.step_index + 1} screenshot`}
                style={{
                  maxWidth: "100%",
                  maxHeight: 300,
                  borderRadius: 8,
                  marginTop: 4,
                  border: "1px solid #f0f0f0",
                }}
              />
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function ExecutionRow({
  exec,
  expanded,
  onToggle,
  steps,
}: {
  exec: StoredCaseExecutionSummary;
  expanded: boolean;
  onToggle: () => void;
  steps: StepExecutionEvidence[] | undefined;
}) {
  return (
    <div className="nb-card" style={{ padding: 0, marginBottom: 8 }}>
      <div
        onClick={onToggle}
        style={{
          padding: "12px 16px",
          cursor: "pointer",
          display: "flex",
          alignItems: "center",
          gap: 10,
          borderRadius: expanded ? "12px 12px 0 0" : 12,
        }}
      >
        <span>{STATUS_ICON[exec.status]}</span>
        <Text strong style={{ flex: 1 }}>
          {exec.case_name}
        </Text>
        {exec.failure_category && (
          <Tag color="red" style={{ marginRight: 4 }}>
            {exec.failure_category}
          </Tag>
        )}
        <Text type="secondary" style={{ fontSize: 12 }}>
          {formatTime(exec.started_at)}
        </Text>
      </div>

      {expanded && steps && (
        <div style={{ borderTop: "1px solid #f0f0f0", padding: "8px 16px 16px" }}>
          {steps.map((step, i) => (
            <StepRow key={i} step={step} />
          ))}
        </div>
      )}
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div
      className="nb-card"
      style={{ padding: 16, display: "flex", flexDirection: "column", gap: 4 }}
    >
      <Text type="secondary" style={{ fontSize: 12 }}>{label}</Text>
      <Text strong style={{ fontSize: 20 }}>{value}</Text>
    </div>
  );
}

export function ReportPage() {
  const [selectedProjectId, setSelectedProjectId] = useState<number | null>(null);

  const { data: projects = [], isLoading: projectsLoading } = useQuery<ProjectSummary[]>({
    queryKey: ["projects"],
    queryFn: getProjects,
  });

  const activeProjectId = selectedProjectId ?? projects[0]?.id ?? null;

  const { data: overview } = useQuery({
    queryKey: ["execution-overview", activeProjectId],
    queryFn: () =>
      getExecutionOverview({ scope_type: "project", project_id: activeProjectId!, window_days: 30 }),
    enabled: activeProjectId != null,
  });

  const { data: executions = [] } = useQuery({
    queryKey: ["executions", activeProjectId],
    queryFn: () => getExecutions({ project_id: activeProjectId!, limit: 50 }),
    enabled: activeProjectId != null,
  });

  const [expandedId, setExpandedId] = useState<number | null>(null);

  const { data: executionDetail } = useQuery({
    queryKey: ["execution-detail", expandedId],
    queryFn: () => getExecutionDetail(expandedId!),
    enabled: expandedId != null,
  });

  const leftPanel = (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <Title level={5} style={{ margin: 0, marginBottom: 12 }}>
        项目
      </Title>
      {projectsLoading ? (
        <Spin />
      ) : (
        projects.map((p) => (
          <div
            key={p.id}
            onClick={() => setSelectedProjectId(p.id)}
            style={{
              padding: "8px 12px",
              borderRadius: 8,
              cursor: "pointer",
              fontSize: 13,
              background: p.id === activeProjectId ? "#1a1a2e" : "transparent",
              color: p.id === activeProjectId ? "#fff" : "#666",
              transition: "background 0.15s",
            }}
          >
            {p.name}
          </div>
        ))
      )}
    </div>
  );

  const centerPanel = activeProjectId ? (
    <div style={{ padding: 20, overflowY: "auto", height: "100%" }} className="panel-scroll">
      <Title level={4} style={{ margin: 0, marginBottom: 16 }}>
        {projects.find((p) => p.id === activeProjectId)?.name} — 报告
      </Title>

      {overview && (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 12, marginBottom: 24 }}>
          <StatCard label="通过率" value={`${(overview.pass_rate * 100).toFixed(1)}%`} />
          <StatCard label="失败数" value={String(overview.failed_count)} />
          <StatCard label="总执行数" value={String(overview.total_count)} />
          <StatCard
            label="平均耗时"
            value={overview.avg_duration_ms ? `${(overview.avg_duration_ms / 1000).toFixed(1)}s` : "-"}
          />
        </div>
      )}

      <Title level={5} style={{ margin: 0, marginBottom: 12 }}>执行结果</Title>
      {executions.length === 0 ? (
        <Empty description="暂无执行记录" />
      ) : (
        executions.map((exec) => (
          <ExecutionRow
            key={exec.id}
            exec={exec}
            expanded={expandedId === exec.id}
            onToggle={() => setExpandedId(expandedId === exec.id ? null : exec.id)}
            steps={
              expandedId === exec.id && executionDetail?.report
                ? executionDetail.report.steps
                : undefined
            }
          />
        ))
      )}
    </div>
  ) : (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "center", height: "100%" }}>
      <Empty description="请选择一个项目" />
    </div>
  );

  return <NotebookLMLayout leftPanel={leftPanel} centerPanel={centerPanel} navBottom />;
}
