import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Switch,
  Table,
  Typography,
  message,
} from "antd";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import {
  getAISettings,
  getAISettingsOverview,
  getDslGenerationRunDetail,
  getDslGenerationRuns,
  updateAISettings,
} from "../services/api";
import type {
  AISettingsUpdatePayload,
  DslGenerationFeedbackStatus,
  DslGenerationPromptVariant,
  DslGenerationRejectionReasonCode,
  DslGenerationRunStatus,
  GenerateDslImportMode,
  GenerateDslMode,
  StoredDslGenerationRunDetail,
  StoredDslGenerationRunSummary,
  VLMModelFamily,
} from "../types/api";

type AISettingsFormValues = AISettingsUpdatePayload;
type GovernanceFilterFormValues = {
  status?: DslGenerationRunStatus;
  feedback_status?: DslGenerationFeedbackStatus;
  generation_mode?: GenerateDslMode;
  import_mode?: GenerateDslImportMode;
  prompt_variant?: DslGenerationPromptVariant;
  rejection_reason_code?: DslGenerationRejectionReasonCode;
  has_risk_flags?: boolean;
  model_name?: string;
  project_id?: number;
  case_id?: number;
  created_from?: string;
  created_to?: string;
};

type GovernanceQueryFilters = {
  status?: DslGenerationRunStatus;
  feedback_status?: DslGenerationFeedbackStatus;
  generation_mode?: GenerateDslMode;
  import_mode?: GenerateDslImportMode;
  prompt_variant?: DslGenerationPromptVariant;
  rejection_reason_code?: DslGenerationRejectionReasonCode;
  has_risk_flags?: boolean;
  model_name?: string;
  project_id?: number;
  case_id?: number;
  created_from?: string;
  created_to?: string;
};

const VLM_FAMILY_OPTIONS: { label: string; value: VLMModelFamily }[] = [
  { label: "gpt-4o", value: "gpt-4o" },
  { label: "gemini", value: "gemini" },
  { label: "qwen-vl", value: "qwen-vl" },
  { label: "qwen2.5-vl", value: "qwen2.5-vl" },
];

const PAGE_SIZE = 10;

const STATUS_OPTIONS = [
  { label: "全部结果", value: "" },
  { label: "成功", value: "success" },
  { label: "失败", value: "failed" },
];

const FEEDBACK_STATUS_OPTIONS = [
  { label: "全部反馈", value: "" },
  { label: "待处理", value: "pending" },
  { label: "已采纳", value: "accepted" },
  { label: "已放弃", value: "rejected" },
];

const GENERATION_MODE_OPTIONS = [
  { label: "全部模式", value: "" },
  { label: "draft", value: "draft" },
  { label: "strict_steps_only", value: "strict_steps_only" },
];

const IMPORT_MODE_OPTIONS = [
  { label: "全部导入方式", value: "" },
  { label: "replace", value: "replace" },
  { label: "steps_only", value: "steps_only" },
  { label: "contracts_only", value: "contracts_only" },
];

const PROMPT_VARIANT_OPTIONS = [
  { label: "全部 variant", value: "" },
  { label: "baseline_draft", value: "baseline_draft" },
  { label: "rewrite_from_case", value: "rewrite_from_case" },
  { label: "repair_steps", value: "repair_steps" },
  { label: "contracts_focus", value: "contracts_focus" },
];

const REJECTION_REASON_OPTIONS = [
  { label: "全部拒绝原因", value: "" },
  { label: "wrong_actions", value: "wrong_actions" },
  { label: "invalid_structure", value: "invalid_structure" },
  { label: "context_mismatch", value: "context_mismatch" },
  { label: "bad_contracts", value: "bad_contracts" },
  { label: "other", value: "other" },
];

const RISK_FILTER_OPTIONS = [
  { label: "仅高风险", value: true },
  { label: "仅无风险", value: false },
];

function formatPercent(value: number) {
  return `${Math.round(value * 100)}%`;
}

function formatTimestamp(value: string | null | undefined) {
  if (!value) {
    return "暂无";
  }
  return new Date(value).toLocaleString("zh-CN", {
    hour12: false,
  });
}

function formatFeedbackStatus(record: StoredDslGenerationRunSummary) {
  if (record.feedback_status === "accepted") {
    return `已采纳${record.feedback_import_mode ? ` (${record.feedback_import_mode})` : ""}`;
  }
  if (record.feedback_status === "rejected") {
    return `已放弃${record.rejection_reason_code ? ` (${record.rejection_reason_code})` : ""}`;
  }
  return "待处理";
}

function formatContextSummary(record: StoredDslGenerationRunDetail) {
  const tags: string[] = [];
  if (record.used_current_case_context) {
    tags.push("current_case");
  }
  if (record.used_current_steps_context) {
    tags.push("current_steps");
  }
  if (record.preserve_contracts_requested) {
    tags.push(`preserve_contracts${record.preserve_contracts_applied ? " (applied)" : ""}`);
  }
  return tags.length ? tags.join(" / ") : "未使用额外上下文";
}

function formatRiskFlags(flags: string[]) {
  return flags.length ? flags.join("、") : "无";
}

function normalizeFilters(values: Partial<GovernanceFilterFormValues>): GovernanceQueryFilters {
  const normalized: GovernanceQueryFilters = {};
  if (values.status) {
    normalized.status = values.status;
  }
  if (values.feedback_status) {
    normalized.feedback_status = values.feedback_status;
  }
  if (values.generation_mode) {
    normalized.generation_mode = values.generation_mode;
  }
  if (values.import_mode) {
    normalized.import_mode = values.import_mode;
  }
  if (values.prompt_variant) {
    normalized.prompt_variant = values.prompt_variant;
  }
  if (values.rejection_reason_code) {
    normalized.rejection_reason_code = values.rejection_reason_code;
  }
  if (typeof values.has_risk_flags === "boolean") {
    normalized.has_risk_flags = values.has_risk_flags;
  }
  if (values.model_name?.trim()) {
    normalized.model_name = values.model_name.trim();
  }
  if (values.project_id != null) {
    normalized.project_id = values.project_id;
  }
  if (values.case_id != null) {
    normalized.case_id = values.case_id;
  }
  if (values.created_from?.trim()) {
    normalized.created_from = values.created_from.trim();
  }
  if (values.created_to?.trim()) {
    normalized.created_to = values.created_to.trim();
  }
  return normalized;
}

export function AISettingsPage() {
  const [form] = Form.useForm<AISettingsFormValues>();
  const [filterForm] = Form.useForm<GovernanceFilterFormValues>();
  const [messageApi, contextHolder] = message.useMessage();
  const queryClient = useQueryClient();
  const [governanceFilters, setGovernanceFilters] = useState<GovernanceQueryFilters>({});
  const [page, setPage] = useState(1);
  const [selectedGenerationId, setSelectedGenerationId] = useState<number | null>(null);

  const settingsQuery = useQuery({
    queryKey: ["ai-settings"],
    queryFn: getAISettings,
  });
  const overviewQuery = useQuery({
    queryKey: ["ai-settings-overview"],
    queryFn: getAISettingsOverview,
  });
  const generationRunsQuery = useQuery({
    queryKey: ["dsl-generation-runs", governanceFilters, page],
    queryFn: () =>
      getDslGenerationRuns({
        ...governanceFilters,
        limit: PAGE_SIZE,
        offset: (page - 1) * PAGE_SIZE,
      }),
  });
  const generationDetailQuery = useQuery({
    queryKey: ["dsl-generation-run-detail", selectedGenerationId],
    queryFn: () => getDslGenerationRunDetail(selectedGenerationId as number),
    enabled: selectedGenerationId != null,
  });

  useEffect(() => {
    if (!settingsQuery.data) {
      return;
    }

    form.setFieldsValue({
      enable_ai_dsl_generate: settingsQuery.data.enable_ai_dsl_generate,
      ai_dsl_timeout_ms: settingsQuery.data.ai_dsl_timeout_ms,
      ai_dsl_base_url: settingsQuery.data.ai_dsl_base_url,
      ai_dsl_model: settingsQuery.data.ai_dsl_model ?? "",
      ai_dsl_strict_mode: settingsQuery.data.ai_dsl_strict_mode,
      ai_dsl_allow_auto_repair: settingsQuery.data.ai_dsl_allow_auto_repair,
      ai_dsl_api_key: "",
      clear_ai_dsl_api_key: false,
      enable_ai_visual_locate: settingsQuery.data.enable_ai_visual_locate,
      ai_visual_timeout_ms: settingsQuery.data.ai_visual_timeout_ms,
      ai_visual_failure_threshold: settingsQuery.data.ai_visual_failure_threshold,
      ai_visual_cooldown_seconds: settingsQuery.data.ai_visual_cooldown_seconds,
      ai_visual_rate_limit_per_minute: settingsQuery.data.ai_visual_rate_limit_per_minute,
      vlm_base_url: settingsQuery.data.vlm_base_url,
      vlm_model: settingsQuery.data.vlm_model ?? "",
      vlm_model_family: settingsQuery.data.vlm_model_family,
      vlm_api_key: "",
      clear_vlm_api_key: false,
    });
  }, [form, settingsQuery.data]);

  const saveMutation = useMutation({
    mutationFn: async () => {
      const values = await form.validateFields();
      return updateAISettings({
        ...values,
        ai_dsl_model: values.ai_dsl_model?.trim() || null,
        ai_dsl_api_key: values.ai_dsl_api_key?.trim() || null,
        vlm_model: values.vlm_model?.trim() || null,
        vlm_api_key: values.vlm_api_key?.trim() || null,
      });
    },
    onSuccess: (result) => {
      queryClient.setQueryData(["ai-settings"], result);
      void queryClient.invalidateQueries({ queryKey: ["ai-settings-overview"] });
      void messageApi.success("AI 配置已保存，并已应用到当前后端进程。");
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  const generationColumns = useMemo(
    () => [
      {
        title: "时间",
        dataIndex: "created_at",
        key: "created_at",
        render: (value: string) => formatTimestamp(value),
      },
      {
        title: "结果",
        dataIndex: "success",
        key: "success",
        render: (value: boolean) => (value ? "成功" : "失败"),
      },
      {
        title: "模型",
        dataIndex: "model_name",
        key: "model_name",
        render: (value: string | null | undefined) => value ?? "未配置",
      },
      {
        title: "模式",
        key: "modes",
        render: (_: unknown, record: StoredDslGenerationRunSummary) =>
          `${record.generation_mode} / ${record.import_mode}`,
      },
      {
        title: "项目/用例",
        key: "scope",
        render: (_: unknown, record: StoredDslGenerationRunSummary) =>
          `P${record.project_id ?? "-"} / C${record.case_id ?? "-"}`,
      },
      {
        title: "反馈",
        key: "feedback_status",
        render: (_: unknown, record: StoredDslGenerationRunSummary) => formatFeedbackStatus(record),
      },
      {
        title: "Prompt 版本",
        dataIndex: "prompt_version",
        key: "prompt_version",
      },
      {
        title: "Prompt Variant",
        dataIndex: "prompt_variant",
        key: "prompt_variant",
      },
      {
        title: "风险标签",
        key: "risk_flags",
        render: (_: unknown, record: StoredDslGenerationRunSummary) => formatRiskFlags(record.risk_flags),
      },
      {
        title: "操作",
        key: "actions",
        render: (_: unknown, record: StoredDslGenerationRunSummary) => (
          <Button type="link" onClick={() => setSelectedGenerationId(record.id)}>
            详情
          </Button>
        ),
      },
    ],
    [],
  );

  if (settingsQuery.isLoading) {
    return <LoadingBlock />;
  }

  if (settingsQuery.isError) {
    return <ErrorBlock message={settingsQuery.error.message} />;
  }

  const overviewData = overviewQuery.data;
  const generationRuns = generationRunsQuery.data ?? [];
  const canGoPrev = page > 1;
  const canGoNext = generationRuns.length === PAGE_SIZE;

  return (
    <>
      {contextHolder}
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }} wrap>
          <div>
            <h1 className="page-title">AI 配置</h1>
            <p className="page-subtitle">管理 AI DSL 生成与 AI 视觉定位，并查看 DSL 生成治理数据。</p>
          </div>
          <Button type="primary" loading={saveMutation.isPending} onClick={() => saveMutation.mutate()}>
            保存配置
          </Button>
        </Space>
      </div>

      <Space direction="vertical" size="large" style={{ width: "100%" }}>
        <Alert
          type="info"
          showIcon
          message="密钥安全说明"
          description="页面不会回显当前已保存的 API Key；如需修改，请输入新值，留空则保持原值，勾选清空后会移除现有密钥。"
        />

        <Card title="生成概览">
          <Space direction="vertical" size="large" style={{ width: "100%" }}>
            {overviewQuery.isLoading ? (
              <Typography.Text type="secondary">正在加载生成概览...</Typography.Text>
            ) : overviewQuery.isError ? (
              <Typography.Text type="danger">{overviewQuery.error.message}</Typography.Text>
            ) : overviewData ? (
              <Descriptions column={2} size="small">
                <Descriptions.Item label="当前生成状态">
                  {overviewData.ai_dsl_enabled ? "已启用" : "未启用"}
                </Descriptions.Item>
                <Descriptions.Item label="默认模型">{overviewData.ai_dsl_model ?? "未配置"}</Descriptions.Item>
                <Descriptions.Item label="严格模式">
                  {overviewData.ai_dsl_strict_mode ? "开启" : "关闭"}
                </Descriptions.Item>
                <Descriptions.Item label="自动修正">
                  {overviewData.ai_dsl_allow_auto_repair ? "开启" : "关闭"}
                </Descriptions.Item>
                <Descriptions.Item label="总请求数">
                  {overviewData.generation_stats.total_requests}
                </Descriptions.Item>
                <Descriptions.Item label="成功 / 失败">
                  {overviewData.generation_stats.success_count} / {overviewData.generation_stats.failure_count}
                </Descriptions.Item>
                <Descriptions.Item label="采纳 / 放弃 / 待处理">
                  {overviewData.generation_stats.accepted_count} / {overviewData.generation_stats.rejected_count} /{" "}
                  {overviewData.generation_stats.pending_count}
                </Descriptions.Item>
                <Descriptions.Item label="决策覆盖率">
                  {formatPercent(overviewData.generation_stats.decision_coverage_rate)}
                </Descriptions.Item>
                <Descriptions.Item label="最近使用模型">
                  {overviewData.generation_stats.last_model ?? "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="最近错误类型">
                  {overviewData.generation_stats.last_error_type ?? "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="近 24h 请求数">
                  {overviewData.generation_stats.last_24h_requests}
                </Descriptions.Item>
                <Descriptions.Item label="近 24h 成功 / 失败">
                  {overviewData.generation_stats.last_24h_success_count} /{" "}
                  {overviewData.generation_stats.last_24h_failure_count}
                </Descriptions.Item>
                <Descriptions.Item label="近 24h 自动修正率">
                  {formatPercent(overviewData.generation_stats.last_24h_auto_repair_rate)}
                </Descriptions.Item>
                <Descriptions.Item label="重试请求数">
                  {overviewData.generation_stats.retry_requests}
                </Descriptions.Item>
                <Descriptions.Item label="重试采纳 / 放弃">
                  {overviewData.generation_stats.retry_accepted_count} / {overviewData.generation_stats.retry_rejected_count}
                </Descriptions.Item>
                <Descriptions.Item label="高频错误类型">
                  {overviewData.generation_stats.top_error_types.length
                    ? overviewData.generation_stats.top_error_types
                        .map((item) => `${item.error_type} (${item.count})`)
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="采纳导入方式分布">
                  {overviewData.generation_stats.accepted_import_mode_breakdown.length
                    ? overviewData.generation_stats.accepted_import_mode_breakdown
                        .map((item) => `${item.import_mode} (${item.count})`)
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="高频拒绝原因">
                  {overviewData.generation_stats.top_rejection_reasons.length
                    ? overviewData.generation_stats.top_rejection_reasons
                        .map((item) => `${item.rejection_reason_code} (${item.count})`)
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="Variant 结果分布">
                  {overviewData.generation_stats.prompt_variant_breakdown.length
                    ? overviewData.generation_stats.prompt_variant_breakdown
                        .map(
                          (item) =>
                            `${item.prompt_variant}: ${item.total_requests} / ${item.accepted_count} / ${item.rejected_count}`,
                        )
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="Prompt 版本效果（总请求 / 采纳 / 放弃 / 重试采纳）">
                  {overviewData.generation_stats.prompt_version_breakdown.length
                    ? overviewData.generation_stats.prompt_version_breakdown
                        .map(
                          (item) =>
                            `${item.prompt_version}: ${item.total_requests} / ${item.accepted_count} / ${item.rejected_count} / ${item.retry_accepted_count}`,
                        )
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="上下文分布">
                  {overviewData.generation_stats.context_profile_breakdown.length
                    ? overviewData.generation_stats.context_profile_breakdown
                        .map(
                          (item) =>
                            `${item.context_profile}: ${item.total_requests} / ${item.accepted_count} / ${item.rejected_count}`,
                        )
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="拒绝原因按 Variant">
                  {overviewData.generation_stats.rejection_reason_by_variant.length
                    ? overviewData.generation_stats.rejection_reason_by_variant
                        .map((item) => `${item.prompt_variant} / ${item.rejection_reason_code} (${item.count})`)
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="模型结果分布">
                  {overviewData.generation_stats.model_outcome_breakdown.length
                    ? overviewData.generation_stats.model_outcome_breakdown
                        .map(
                          (item) =>
                            `${item.model_name ?? "未配置"}: ${item.total_requests} / ${item.accepted_count} / ${item.rejected_count}`,
                        )
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="模式结果分布">
                  {overviewData.generation_stats.generation_mode_breakdown.length
                    ? overviewData.generation_stats.generation_mode_breakdown
                        .map(
                          (item) =>
                            `${item.generation_mode}: ${item.total_requests} / ${item.accepted_count} / ${item.rejected_count}`,
                        )
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
                <Descriptions.Item label="重试成效">
                  {overviewData.generation_stats.retry_acceptance_by_reason.length
                    ? overviewData.generation_stats.retry_acceptance_by_reason
                        .map(
                          (item) =>
                            `${item.rejection_reason_code}: ${item.retry_requests} / ${item.accepted_count} / ${formatPercent(item.acceptance_rate)}`,
                        )
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
              </Descriptions>
            ) : (
              <Typography.Text type="secondary">暂无生成概览数据。</Typography.Text>
            )}
          </Space>
        </Card>

        <Card title="治理记录">
          <Space direction="vertical" size="middle" style={{ width: "100%" }}>
            <Form
              form={filterForm}
              layout="vertical"
              onFinish={(values) => {
                setPage(1);
                setGovernanceFilters(normalizeFilters(values));
              }}
            >
              <div className="structured-step-grid">
                <Form.Item label="结果" name="status">
                  <Select options={STATUS_OPTIONS} />
                </Form.Item>
                <Form.Item label="反馈状态" name="feedback_status">
                  <Select options={FEEDBACK_STATUS_OPTIONS} />
                </Form.Item>
                <Form.Item label="生成模式" name="generation_mode">
                  <Select options={GENERATION_MODE_OPTIONS} />
                </Form.Item>
                <Form.Item label="导入方式" name="import_mode">
                  <Select options={IMPORT_MODE_OPTIONS} />
                </Form.Item>
                <Form.Item label="Prompt Variant" name="prompt_variant">
                  <Select options={PROMPT_VARIANT_OPTIONS} />
                </Form.Item>
              </div>
              <div className="structured-step-grid">
                <Form.Item label="拒绝原因" name="rejection_reason_code">
                  <Select options={REJECTION_REASON_OPTIONS} />
                </Form.Item>
                <Form.Item label="风险标签" name="has_risk_flags">
                  <Select allowClear options={RISK_FILTER_OPTIONS} />
                </Form.Item>
                <Form.Item label="模型名" name="model_name">
                  <Input placeholder="例如：gpt-4o-mini" />
                </Form.Item>
                <Form.Item label="项目 ID" name="project_id">
                  <InputNumber min={1} style={{ width: "100%" }} />
                </Form.Item>
              </div>
              <div className="structured-step-grid">
                <Form.Item label="用例 ID" name="case_id">
                  <InputNumber min={1} style={{ width: "100%" }} />
                </Form.Item>
                <Form.Item label="开始时间 (ISO)" name="created_from">
                  <Input placeholder="2026-03-18T00:00:00" />
                </Form.Item>
                <Form.Item label="结束时间 (ISO)" name="created_to">
                  <Input placeholder="2026-03-18T23:59:59" />
                </Form.Item>
              </div>
              <Space wrap>
                <Button type="primary" htmlType="submit">
                  应用筛选
                </Button>
                <Button
                  onClick={() => {
                    filterForm.resetFields();
                    setPage(1);
                    setGovernanceFilters({});
                  }}
                >
                  重置
                </Button>
              </Space>
            </Form>

            {generationRunsQuery.isLoading ? (
              <Typography.Text type="secondary">正在加载治理记录...</Typography.Text>
            ) : generationRunsQuery.isError ? (
              <Typography.Text type="danger">{generationRunsQuery.error.message}</Typography.Text>
            ) : (
              <>
                <Table<StoredDslGenerationRunSummary>
                  rowKey="id"
                  size="small"
                  pagination={false}
                  columns={generationColumns}
                  dataSource={generationRuns}
                  locale={{ emptyText: "暂无生成记录" }}
                />
                <Space style={{ justifyContent: "space-between", width: "100%" }}>
                  <Typography.Text type="secondary">当前第 {page} 页</Typography.Text>
                  <Space>
                    <Button disabled={!canGoPrev} onClick={() => setPage((current) => Math.max(1, current - 1))}>
                      上一页
                    </Button>
                    <Button disabled={!canGoNext} onClick={() => setPage((current) => current + 1)}>
                      下一页
                    </Button>
                  </Space>
                </Space>
              </>
            )}
          </Space>
        </Card>

        <Form form={form} layout="vertical">
          <Card title="AI DSL 生成">
            <div className="workbench-grid">
              <Form.Item label="启用 DSL 生成" name="enable_ai_dsl_generate" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Form.Item label="当前 DSL API Key 状态">
                <Typography.Text>{settingsQuery.data?.has_ai_dsl_api_key ? "已配置" : "未配置"}</Typography.Text>
              </Form.Item>
            </div>
            <div className="structured-step-grid">
              <Form.Item
                label="AI DSL Base URL"
                name="ai_dsl_base_url"
                rules={[{ required: true, message: "请输入 AI DSL Base URL" }]}
              >
                <Input />
              </Form.Item>
              <Form.Item label="AI DSL Model" name="ai_dsl_model">
                <Input placeholder="例如：gpt-4o-mini" />
              </Form.Item>
              <Form.Item
                label="AI DSL Timeout (ms)"
                name="ai_dsl_timeout_ms"
                rules={[{ required: true, message: "请输入 AI DSL 超时" }]}
              >
                <InputNumber min={1000} style={{ width: "100%" }} />
              </Form.Item>
            </div>
            <div className="workbench-grid">
              <Form.Item label="严格生成模式" name="ai_dsl_strict_mode" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Form.Item label="允许自动修正" name="ai_dsl_allow_auto_repair" valuePropName="checked">
                <Switch />
              </Form.Item>
            </div>
            <div className="structured-step-grid">
              <Form.Item label="替换 DSL API Key" name="ai_dsl_api_key">
                <Input.Password placeholder="留空则保持原值" />
              </Form.Item>
              <Form.Item label="清空 DSL API Key" name="clear_ai_dsl_api_key" valuePropName="checked">
                <Switch />
              </Form.Item>
            </div>
          </Card>

          <Card title="AI 视觉定位">
            <div className="workbench-grid">
              <Form.Item label="启用视觉定位" name="enable_ai_visual_locate" valuePropName="checked">
                <Switch />
              </Form.Item>
              <Form.Item label="当前 VLM API Key 状态">
                <Typography.Text>{settingsQuery.data?.has_vlm_api_key ? "已配置" : "未配置"}</Typography.Text>
              </Form.Item>
            </div>
            <div className="structured-step-grid">
              <Form.Item
                label="VLM Base URL"
                name="vlm_base_url"
                rules={[{ required: true, message: "请输入 VLM Base URL" }]}
              >
                <Input />
              </Form.Item>
              <Form.Item label="VLM Model" name="vlm_model">
                <Input placeholder="例如：gpt-4o" />
              </Form.Item>
              <Form.Item
                label="Model Family"
                name="vlm_model_family"
                rules={[{ required: true, message: "请选择模型家族" }]}
              >
                <Select options={VLM_FAMILY_OPTIONS} />
              </Form.Item>
            </div>
            <div className="structured-step-grid">
              <Form.Item
                label="视觉超时 (ms)"
                name="ai_visual_timeout_ms"
                rules={[{ required: true, message: "请输入视觉超时" }]}
              >
                <InputNumber min={1000} style={{ width: "100%" }} />
              </Form.Item>
              <Form.Item
                label="失败阈值"
                name="ai_visual_failure_threshold"
                rules={[{ required: true, message: "请输入失败阈值" }]}
              >
                <InputNumber min={1} style={{ width: "100%" }} />
              </Form.Item>
              <Form.Item
                label="冷却时间 (s)"
                name="ai_visual_cooldown_seconds"
                rules={[{ required: true, message: "请输入冷却时间" }]}
              >
                <InputNumber min={1} style={{ width: "100%" }} />
              </Form.Item>
              <Form.Item
                label="每分钟速率上限"
                name="ai_visual_rate_limit_per_minute"
                rules={[{ required: true, message: "请输入速率上限" }]}
              >
                <InputNumber min={1} style={{ width: "100%" }} />
              </Form.Item>
            </div>
            <div className="structured-step-grid">
              <Form.Item label="替换 VLM API Key" name="vlm_api_key">
                <Input.Password placeholder="留空则保持原值" />
              </Form.Item>
              <Form.Item label="清空 VLM API Key" name="clear_vlm_api_key" valuePropName="checked">
                <Switch />
              </Form.Item>
            </div>
          </Card>
        </Form>
      </Space>

      <Drawer
        title={selectedGenerationId != null ? `治理详情 #${selectedGenerationId}` : "治理详情"}
        open={selectedGenerationId != null}
        onClose={() => setSelectedGenerationId(null)}
        width={720}
      >
        {generationDetailQuery.isLoading ? (
          <Typography.Text type="secondary">正在加载详情...</Typography.Text>
        ) : generationDetailQuery.isError ? (
          <Typography.Text type="danger">{generationDetailQuery.error.message}</Typography.Text>
        ) : generationDetailQuery.data ? (
          <Space direction="vertical" size="large" style={{ width: "100%" }}>
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="创建时间">
                {formatTimestamp(generationDetailQuery.data.created_at)}
              </Descriptions.Item>
              <Descriptions.Item label="Prompt 版本">{generationDetailQuery.data.prompt_version}</Descriptions.Item>
              <Descriptions.Item label="重试来源">
                {generationDetailQuery.data.retry_from_generation_id != null
                  ? `#${generationDetailQuery.data.retry_from_generation_id}`
                  : "首次生成"}
              </Descriptions.Item>
              <Descriptions.Item label="重试策略">
                {generationDetailQuery.data.retry_reason_code ?? "无"}
              </Descriptions.Item>
              <Descriptions.Item label="Prompt Variant">{generationDetailQuery.data.prompt_variant}</Descriptions.Item>
              <Descriptions.Item label="治理焦点">
                {generationDetailQuery.data.governance_focus_reasons.length
                  ? generationDetailQuery.data.governance_focus_reasons.join(" / ")
                  : "无"}
              </Descriptions.Item>
              <Descriptions.Item label="上下文档案">{generationDetailQuery.data.context_profile}</Descriptions.Item>
              <Descriptions.Item label="结果">{generationDetailQuery.data.success ? "成功" : "失败"}</Descriptions.Item>
              <Descriptions.Item label="反馈">{formatFeedbackStatus(generationDetailQuery.data)}</Descriptions.Item>
              <Descriptions.Item label="项目 / 用例">
                P{generationDetailQuery.data.project_id ?? "-"} / C{generationDetailQuery.data.case_id ?? "-"}
              </Descriptions.Item>
              <Descriptions.Item label="Base URL 请求值">
                {generationDetailQuery.data.request_base_url ?? "未提供"}
              </Descriptions.Item>
              <Descriptions.Item label="上下文来源">
                {formatContextSummary(generationDetailQuery.data)}
              </Descriptions.Item>
              <Descriptions.Item label="风险标签">
                {formatRiskFlags(generationDetailQuery.data.risk_flags)}
              </Descriptions.Item>
              <Descriptions.Item label="拒绝备注">
                {generationDetailQuery.data.feedback_note ?? "无"}
              </Descriptions.Item>
              <Descriptions.Item label="重试备注">
                {generationDetailQuery.data.retry_note ?? "无"}
              </Descriptions.Item>
            </Descriptions>

            <Card size="small" title="自动修正项">
              {generationDetailQuery.data.normalization_notes_json.length ? (
                <Space direction="vertical" size="small">
                  {generationDetailQuery.data.normalization_notes_json.map((note) => (
                    <Typography.Text key={note}>{note}</Typography.Text>
                  ))}
                </Space>
              ) : (
                <Typography.Text type="secondary">无</Typography.Text>
              )}
            </Card>

            <Card size="small" title="Warnings">
              {generationDetailQuery.data.warnings_json.length ? (
                <Space direction="vertical" size="small">
                  {generationDetailQuery.data.warnings_json.map((warning) => (
                    <Typography.Text key={warning}>{warning}</Typography.Text>
                  ))}
                </Space>
              ) : (
                <Typography.Text type="secondary">无</Typography.Text>
              )}
            </Card>

            <Card size="small" title="生成草案 JSON">
              <Input.TextArea
                readOnly
                rows={14}
                value={JSON.stringify(generationDetailQuery.data.generated_case_json ?? null, null, 2)}
                style={{ fontFamily: "Consolas, 'Courier New', monospace" }}
              />
            </Card>
          </Space>
        ) : (
          <Typography.Text type="secondary">暂无详情数据。</Typography.Text>
        )}
      </Drawer>
    </>
  );
}
