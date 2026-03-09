import { useQuery } from "@tanstack/react-query";
import { Card, Col, Descriptions, Divider, Empty, Row, Space, Tag, Timeline, Typography } from "antd";
import { useParams } from "react-router-dom";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { getExecutionDetail } from "../services/api";
import type { ExecutionStatus, StepExecutionEvidence } from "../types/api";

function renderStatus(status: ExecutionStatus) {
  const colorMap: Record<ExecutionStatus, string> = {
    passed: "success",
    failed: "error",
    running: "processing",
  };
  const labelMap: Record<ExecutionStatus, string> = {
    passed: "通过",
    failed: "失败",
    running: "运行中",
  };
  return (
    <Tag className="status-tag" color={colorMap[status]}>
      {labelMap[status]}
    </Tag>
  );
}

function StepEvidenceCard({ step }: { step: StepExecutionEvidence }) {
  return (
    <Card
      className="step-card"
      title={`Step ${step.step_index + 1} · ${step.action}`}
      extra={renderStatus(step.status)}
    >
      <Space direction="vertical" size="middle" style={{ width: "100%" }}>
        <Descriptions column={1} size="small" bordered>
          <Descriptions.Item label="目标">{step.target || "-"}</Descriptions.Item>
          <Descriptions.Item label="值">{step.value || "-"}</Descriptions.Item>
          <Descriptions.Item label="URL">{step.url || "-"}</Descriptions.Item>
          <Descriptions.Item label="定位策略">{step.resolved_by || "-"}</Descriptions.Item>
          <Descriptions.Item label="错误信息">{step.error_message || "-"}</Descriptions.Item>
        </Descriptions>
        {step.screenshot_url ? (
          <img src={step.screenshot_url} alt={`step-${step.step_index + 1}`} />
        ) : (
          <div className="screenshot-empty">该步骤没有截图</div>
        )}
      </Space>
    </Card>
  );
}

export function ExecutionDetailPage() {
  const params = useParams<{ executionId: string }>();
  const executionId = Number(params.executionId);
  const query = useQuery({
    queryKey: ["execution-detail", executionId],
    queryFn: () => getExecutionDetail(executionId),
    enabled: Number.isFinite(executionId),
  });

  if (query.isLoading) {
    return <LoadingBlock />;
  }
  if (query.isError) {
    return <ErrorBlock message={query.error.message} />;
  }
  if (!query.data) {
    return <Empty description="执行详情不存在。" />;
  }

  const detail = query.data;
  const steps = detail.report?.steps ?? [];

  return (
    <Space direction="vertical" size="large" style={{ width: "100%" }}>
      <div className="page-header">
        <h1 className="page-title">{detail.case_name}</h1>
        <p className="page-subtitle">查看步骤时间线、截图证据、URL 与失败原因。</p>
      </div>

      <div className="summary-strip">
        <div className="summary-tile">
          <div className="summary-label">执行状态</div>
          <div className="summary-value">{renderStatus(detail.status)}</div>
        </div>
        <div className="summary-tile">
          <div className="summary-label">执行编号</div>
          <div className="summary-value">#{detail.id}</div>
        </div>
        <div className="summary-tile">
          <div className="summary-label">步骤数量</div>
          <div className="summary-value">{steps.length}</div>
        </div>
      </div>

      <Card>
        <Descriptions bordered column={2}>
          <Descriptions.Item label="用例名称">{detail.case_name}</Descriptions.Item>
          <Descriptions.Item label="项目 ID">{detail.project_id}</Descriptions.Item>
          <Descriptions.Item label="开始时间">
            {new Date(detail.started_at).toLocaleString()}
          </Descriptions.Item>
          <Descriptions.Item label="结束时间">
            {detail.finished_at ? new Date(detail.finished_at).toLocaleString() : "-"}
          </Descriptions.Item>
          <Descriptions.Item label="错误摘要" span={2}>
            {detail.error_message || "-"}
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title="步骤时间线">
        {steps.length ? (
          <>
            <Timeline
              items={steps.map((step) => ({
                color: step.status === "passed" ? "green" : "red",
                children: `Step ${step.step_index + 1} · ${step.action}`,
              }))}
            />
            <Divider />
            <Row gutter={[16, 16]}>
              {steps.map((step) => (
                <Col xs={24} xl={12} key={`${step.step_index}-${step.action}`}>
                  <StepEvidenceCard step={step} />
                </Col>
              ))}
            </Row>
          </>
        ) : (
          <Empty description="当前执行没有步骤证据。" />
        )}
      </Card>
    </Space>
  );
}
