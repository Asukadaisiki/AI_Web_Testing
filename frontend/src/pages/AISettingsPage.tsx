import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Descriptions,
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
import { getAISettings, getAISettingsOverview, getDslGenerationRuns, updateAISettings } from "../services/api";
import type { AISettingsUpdatePayload, StoredDslGenerationRunSummary, VLMModelFamily } from "../types/api";

type AISettingsFormValues = AISettingsUpdatePayload;

const VLM_FAMILY_OPTIONS: { label: string; value: VLMModelFamily }[] = [
  { label: "gpt-4o", value: "gpt-4o" },
  { label: "gemini", value: "gemini" },
  { label: "qwen-vl", value: "qwen-vl" },
  { label: "qwen2.5-vl", value: "qwen2.5-vl" },
];

function formatPercent(value: number) {
  return `${Math.round(value * 100)}%`;
}

function formatTimestamp(value: string) {
  return new Date(value).toLocaleString("zh-CN", {
    hour12: false,
  });
}

export function AISettingsPage() {
  const [form] = Form.useForm<AISettingsFormValues>();
  const [messageApi, contextHolder] = message.useMessage();
  const queryClient = useQueryClient();

  const settingsQuery = useQuery({
    queryKey: ["ai-settings"],
    queryFn: getAISettings,
  });
  const overviewQuery = useQuery({
    queryKey: ["ai-settings-overview"],
    queryFn: getAISettingsOverview,
  });
  const generationRunsQuery = useQuery({
    queryKey: ["dsl-generation-runs", "recent", 10],
    queryFn: () => getDslGenerationRuns({ limit: 10 }),
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

  if (settingsQuery.isLoading) {
    return <LoadingBlock />;
  }

  if (settingsQuery.isError) {
    return <ErrorBlock message={settingsQuery.error.message} />;
  }

  const overviewData = overviewQuery.data;
  const generationColumns = [
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
      title: "自动修正",
      key: "repairs",
      render: (_: unknown, record: StoredDslGenerationRunSummary) =>
        `action ${record.repaired_invalid_actions} / steps ${record.removed_invalid_steps} / contracts ${record.removed_invalid_contracts}`,
    },
    {
      title: "错误",
      key: "error",
      render: (_: unknown, record: StoredDslGenerationRunSummary) =>
        record.error_type ? `${record.error_type}: ${record.error_message ?? ""}` : "无",
    },
    {
      title: "Prompt 摘要",
      dataIndex: "prompt_preview",
      key: "prompt_preview",
    },
  ];

  return (
    <>
      {contextHolder}
      <div className="page-header">
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }} wrap>
          <div>
            <h1 className="page-title">AI 配置</h1>
            <p className="page-subtitle">管理 AI DSL 生成与 AI 视觉定位的运行时配置。</p>
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
                <Descriptions.Item label="默认模型">
                  {overviewData.ai_dsl_model ?? "未配置"}
                </Descriptions.Item>
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
                  {overviewData.generation_stats.last_24h_success_count} / {overviewData.generation_stats.last_24h_failure_count}
                </Descriptions.Item>
                <Descriptions.Item label="近 24h 自动修正率">
                  {formatPercent(overviewData.generation_stats.last_24h_auto_repair_rate)}
                </Descriptions.Item>
                <Descriptions.Item label="近 24h 高频错误类型">
                  {overviewData.generation_stats.top_error_types.length
                    ? overviewData.generation_stats.top_error_types
                        .map((item) => `${item.error_type} (${item.count})`)
                        .join("、")
                    : "暂无"}
                </Descriptions.Item>
              </Descriptions>
            ) : (
              <Typography.Text type="secondary">暂无生成概览数据。</Typography.Text>
            )}

            <div>
              <Typography.Title level={5} style={{ marginTop: 0 }}>
                最近生成记录
              </Typography.Title>
              {generationRunsQuery.isLoading ? (
                <Typography.Text type="secondary">正在加载最近生成记录...</Typography.Text>
              ) : generationRunsQuery.isError ? (
                <Typography.Text type="danger">{generationRunsQuery.error.message}</Typography.Text>
              ) : (
                <Table<StoredDslGenerationRunSummary>
                  rowKey="id"
                  size="small"
                  pagination={false}
                  columns={generationColumns}
                  dataSource={generationRunsQuery.data ?? []}
                  locale={{ emptyText: "暂无生成记录" }}
                />
              )}
            </div>
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
    </>
  );
}
