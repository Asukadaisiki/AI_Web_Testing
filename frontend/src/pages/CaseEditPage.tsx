import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import {
  Button,
  Form,
  Input,
  Space,
  Typography,
  message,
  Card,
  Select,
  InputNumber,
  Popconfirm,
} from "antd";
import { PlusOutlined, DeleteOutlined, ArrowLeftOutlined } from "@ant-design/icons";

import { ErrorBlock, LoadingBlock } from "../shared/ui/PageFeedback";
import { createCase, deleteCase, getCaseDetail, updateCase } from "../features/cases/api";
import type { DSLStep, CaseMutationPayload } from "../types/api";

const SUPPORTED_ACTIONS = [
  "navigate",
  "click",
  "fill",
  "select",
  "hover",
  "wait_for_selector",
  "assert_text",
  "assert_visible",
  "assert_url",
  "screenshot",
  "keyboard_press",
  "scroll",
  "wait",
];

export function CaseEditPage() {
  const { caseId } = useParams<{ caseId: string }>();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const queryClient = useQueryClient();
  const [messageApi, contextHolder] = message.useMessage();
  const [form] = Form.useForm();
  const isCreateMode = caseId === undefined;
  const parsedCaseId = Number(caseId);
  const createProjectId = Number(searchParams.get("project_id"));
  const hasValidCaseId = Number.isInteger(parsedCaseId) && parsedCaseId > 0;
  const hasValidProjectId = Number.isInteger(createProjectId) && createProjectId > 0;

  const caseQuery = useQuery({
    queryKey: ["case", caseId],
    queryFn: () => getCaseDetail(parsedCaseId),
    enabled: !isCreateMode && hasValidCaseId,
  });

  const updateMutation = useMutation({
    mutationFn: (payload: CaseMutationPayload) =>
      updateCase(parsedCaseId, payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["cases"] });
      queryClient.invalidateQueries({ queryKey: ["case", caseId] });
      void messageApi.success("保存成功");
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  const createMutation = useMutation({
    mutationFn: (payload: CaseMutationPayload) => createCase(payload),
    onSuccess: (createdCase) => {
      queryClient.invalidateQueries({ queryKey: ["cases"] });
      void messageApi.success("用例已创建");
      navigate(`/cases/${createdCase.id}/edit`, { replace: true });
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => deleteCase(parsedCaseId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["cases"] });
      void messageApi.success("用例已删除");
      navigate("/cases");
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  if (!isCreateMode && !hasValidCaseId) return <ErrorBlock message="用例 ID 无效" />;
  if (isCreateMode && !hasValidProjectId) return <ErrorBlock message="缺少有效的项目 ID" />;
  if (!isCreateMode && caseQuery.isLoading) return <LoadingBlock />;
  if (!isCreateMode && caseQuery.isError) return <ErrorBlock message={caseQuery.error.message} />;

  const caseData = caseQuery.data;
  if (!isCreateMode && !caseData) return <ErrorBlock message="用例不存在" />;

  const handleSave = async () => {
    try {
      const values = await form.validateFields();
      const payload: CaseMutationPayload = {
        project_id: isCreateMode ? createProjectId : caseData!.project_id,
        name: values.name,
        description: values.description || null,
        base_url: values.base_url || null,
        steps: (values.steps ?? []).map((s: DSLStep) => ({
          action: s.action,
          target: s.target || undefined,
          value: s.value || undefined,
          timeout_ms: s.timeout_ms || undefined,
        })),
        input_contract: caseData?.input_contract ?? [],
        output_contract: caseData?.output_contract ?? [],
      };
      if (isCreateMode) {
        createMutation.mutate(payload);
      } else {
        updateMutation.mutate(payload);
      }
    } catch {
      // validation errors shown inline
    }
  };

  return (
    <div style={{ padding: 24, maxWidth: 900, margin: "0 auto" }}>
      {contextHolder}

      <div style={{ marginBottom: 16, display: "flex", alignItems: "center", gap: 12 }}>
        <Button
          icon={<ArrowLeftOutlined />}
          onClick={() => navigate("/cases")}
        />
        <Typography.Title level={4} style={{ margin: 0 }}>
          {isCreateMode ? "新建用例" : "编辑用例"}
        </Typography.Title>
      </div>

      <Form
        form={form}
        layout="vertical"
        initialValues={{
          name: caseData?.name ?? "",
          description: caseData?.description || "",
          base_url: caseData?.base_url || "",
          steps: caseData?.steps ?? [],
        }}
      >
        <Card title="基本信息" style={{ marginBottom: 16 }}>
          <Form.Item
            name="name"
            label="用例名称"
            rules={[{ required: true, message: "请输入用例名称" }]}
          >
            <Input placeholder="输入用例名称" />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} placeholder="输入用例描述（可选）" />
          </Form.Item>
          <Form.Item name="base_url" label="Base URL">
            <Input placeholder="https://example.com" />
          </Form.Item>
        </Card>

        <Card
          title="步骤"
          style={{ marginBottom: 16 }}
          extra={
            <Button
              type="dashed"
              size="small"
              icon={<PlusOutlined />}
              onClick={() => {
                const steps = form.getFieldValue("steps") ?? [];
                form.setFieldValue("steps", [...steps, { action: "click", target: "", value: "" }]);
              }}
            >
              添加步骤
            </Button>
          }
        >
          <Form.List name="steps">
            {(fields, { remove }) =>
              fields.length === 0 ? (
                <Typography.Text type="secondary">暂无步骤，点击右上角添加</Typography.Text>
              ) : (
                fields.map(({ key, name, ...restField }) => (
                  <div
                    key={key}
                    style={{
                      display: "flex",
                      gap: 8,
                      alignItems: "flex-start",
                      marginBottom: 8,
                      padding: 8,
                      background: "#fafafa",
                      borderRadius: 6,
                    }}
                  >
                    <Typography.Text
                      type="secondary"
                      style={{ lineHeight: "32px", minWidth: 28, textAlign: "center" }}
                    >
                      {name + 1}
                    </Typography.Text>
                    <Form.Item {...restField} name={[name, "action"]} style={{ margin: 0, width: 160 }}>
                      <Select options={SUPPORTED_ACTIONS.map((a) => ({ label: a, value: a }))} />
                    </Form.Item>
                    <Form.Item {...restField} name={[name, "target"]} style={{ margin: 0, flex: 1 }}>
                      <Input placeholder="target" />
                    </Form.Item>
                    <Form.Item {...restField} name={[name, "value"]} style={{ margin: 0, flex: 1 }}>
                      <Input placeholder="value" />
                    </Form.Item>
                    <Form.Item {...restField} name={[name, "timeout_ms"]} style={{ margin: 0, width: 100 }}>
                      <InputNumber placeholder="ms" min={0} />
                    </Form.Item>
                    <Button
                      type="text"
                      danger
                      icon={<DeleteOutlined />}
                      onClick={() => remove(name)}
                      style={{ marginTop: 1 }}
                    />
                  </div>
                ))
              )
            }
          </Form.List>
        </Card>

        <div style={{ display: "flex", justifyContent: "space-between" }}>
          <Space>
            <Button onClick={() => navigate("/cases")}>取消</Button>
            <Button
              type="primary"
              onClick={handleSave}
              loading={createMutation.isPending || updateMutation.isPending}
              disabled={createMutation.isPending || updateMutation.isPending}
            >
              保存
            </Button>
          </Space>
          {!isCreateMode && (
            <Popconfirm
              title="确认删除该用例？"
              description="删除后不可恢复"
              onConfirm={() => deleteMutation.mutate()}
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true }}
            >
              <Button danger loading={deleteMutation.isPending} disabled={deleteMutation.isPending}>
                删除用例
              </Button>
            </Popconfirm>
          )}
        </div>
      </Form>
    </div>
  );
}
