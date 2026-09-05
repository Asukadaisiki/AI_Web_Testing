import { useEffect, useMemo, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Descriptions,
  Empty,
  Input,
  Select,
  Space,
  Tag,
  Typography,
} from "antd";

import { createCorrection } from "../features/executions/api";
import type {
  CorrectionType,
  LocatorCandidateEvidence,
  StepExecutionEvidence,
  StoredCaseExecutionDetail,
} from "../types/api";

export function getCandidateCorrection(
  candidate: LocatorCandidateEvidence,
): { type: CorrectionType; value: string } | null {
  if (candidate.attributes.data_testid) {
    return { type: "test_id", value: candidate.attributes.data_testid };
  }
  return null;
}

export function LocatorEvidencePanel({
  execution,
  step,
}: {
  execution: StoredCaseExecutionDetail;
  step: StepExecutionEvidence;
}) {
  const trace = step.locator_trace;
  const [correctionType, setCorrectionType] = useState<CorrectionType>("css");
  const [correctionValue, setCorrectionValue] = useState("");
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    setCorrectionValue("");
    setSaved(false);
  }, [execution.id, step.step_index]);

  const selectedIndex = useMemo(
    () =>
      trace?.selected_candidate
        ? trace.candidates.findIndex(
            (candidate) =>
              candidate.strategy === trace.selected_candidate?.strategy
              && candidate.preview_text === trace.selected_candidate?.preview_text,
          )
        : -1,
    [trace],
  );

  const correctionMutation = useMutation({
    mutationFn: () =>
      createCorrection({
        page_url: step.url || execution.latest_url || "",
        target_description: trace?.target || step.target || "",
        correction_type: correctionType,
        correction_value: correctionValue.trim(),
        source_execution_id: execution.id,
        created_by: execution.triggered_by,
      }),
    onSuccess: () => setSaved(true),
  });

  if (!trace) {
    return <Empty description="该步骤没有定位轨迹" />;
  }

  return (
    <div className="locator-evidence">
      <Descriptions column={{ xs: 1, md: 2 }} bordered size="small">
        <Descriptions.Item label="Target">{trace.target || step.target || "-"}</Descriptions.Item>
        <Descriptions.Item label="命中策略">
          {trace.match_strategy || step.resolved_by || "-"}
        </Descriptions.Item>
        <Descriptions.Item label="Final match">
          {trace.selected_candidate
            ? `${trace.selected_candidate.strategy} / ${
                trace.selected_candidate.preview_text
                || trace.selected_candidate.role
                || "-"
              }`
            : "-"}
        </Descriptions.Item>
        <Descriptions.Item label="Failure reason">
          {trace.failure_reason || step.error_message || "-"}
        </Descriptions.Item>
      </Descriptions>

      <div className="evidence-heading">
        <Typography.Title level={5}>Candidates</Typography.Title>
        <Typography.Text type="secondary">{trace.candidates.length} 个候选</Typography.Text>
      </div>
      {trace.candidates.length ? (
        <div className="candidate-list">
          {trace.candidates.map((candidate, index) => {
            const suggestion = getCandidateCorrection(candidate);
            return (
              <div
                className={index === selectedIndex ? "candidate-row selected" : "candidate-row"}
                key={`${candidate.strategy}-${index}`}
              >
                <div>
                  <Space wrap>
                    <Typography.Text strong>#{index + 1}</Typography.Text>
                    <Tag>{candidate.strategy}</Tag>
                    <Typography.Text>
                      {candidate.preview_text || candidate.role || "无文本"}
                    </Typography.Text>
                    <Typography.Text type="secondary">
                      score={candidate.score}
                    </Typography.Text>
                  </Space>
                  <Typography.Text type="secondary" className="candidate-meta">
                    role={candidate.role || "-"} / visible={String(candidate.visible)} /
                    enabled={String(candidate.enabled)}
                  </Typography.Text>
                  {candidate.rejected_reasons.length ? (
                    <Typography.Text type="danger" className="candidate-meta">
                      rejected: {candidate.rejected_reasons.join(", ")}
                    </Typography.Text>
                  ) : null}
                </div>
                {suggestion ? (
                  <Button
                    size="small"
                    onClick={() => {
                      setCorrectionType(suggestion.type);
                      setCorrectionValue(suggestion.value);
                      setSaved(false);
                    }}
                  >
                    用于修正
                  </Button>
                ) : null}
              </div>
            );
          })}
        </div>
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有候选元素证据" />
      )}

      <div className="correction-editor">
        <Typography.Title level={5}>提交定位修正</Typography.Title>
        <Space.Compact block>
          <Select<CorrectionType>
            aria-label="修正类型"
            value={correctionType}
            onChange={setCorrectionType}
            options={[
              { label: "CSS", value: "css" },
              { label: "XPath", value: "xpath" },
              { label: "Test ID", value: "test_id" },
            ]}
            style={{ width: 130 }}
          />
          <Input
            aria-label="修正值"
            value={correctionValue}
            placeholder="输入确定性的 selector"
            onChange={(event) => {
              setCorrectionValue(event.target.value);
              setSaved(false);
            }}
          />
          <Button
            type="primary"
            loading={correctionMutation.isPending}
            disabled={
              !correctionValue.trim()
              || !(step.url || execution.latest_url)
              || correctionMutation.isPending
            }
            onClick={() => correctionMutation.mutate()}
          >
            保存修正
          </Button>
        </Space.Compact>
        {saved ? (
          <Alert type="success" showIcon message="修正已保存，可在后续执行中复用" />
        ) : null}
        {correctionMutation.isError ? (
          <Alert type="error" showIcon message={correctionMutation.error.message} />
        ) : null}
      </div>
    </div>
  );
}
