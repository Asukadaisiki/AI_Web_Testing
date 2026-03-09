import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Space,
  Tag,
  Typography,
  message,
} from "antd";
import { useNavigate, useParams } from "react-router-dom";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import {
  createCase,
  executeCase,
  getCaseDetail,
  updateCase,
  validateDslCase,
} from "../services/api";
import type { CaseMutationPayload, DSLCasePayload, DSLValidationResult, DSLStep } from "../types/api";

type WorkbenchFormValues = {
  name: string;
  description?: string;
  project_id: number;
};

function formatStepsJson(steps: DSLStep[]) {
  return JSON.stringify(steps, null, 2);
}

function buildDslPayload(values: WorkbenchFormValues, stepsJson: string): DSLCasePayload {
  let parsedSteps: unknown;
  try {
    parsedSteps = JSON.parse(stepsJson);
  } catch (error) {
    throw new Error("DSL Steps JSON 不是合法的 JSON。");
  }

  if (!Array.isArray(parsedSteps)) {
    throw new Error("DSL Steps JSON 必须是数组。");
  }

  return {
    name: values.name,
    description: values.description || null,
    steps: parsedSteps as DSLStep[],
  };
}

export function CaseWorkbenchPage() {
  const { caseId } = useParams<{ caseId: string }>();
  const isEditMode = Boolean(caseId);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<WorkbenchFormValues>();
  const [messageApi, contextHolder] = message.useMessage();
  const [stepsJson, setStepsJson] = useState<string>(formatStepsJson([{ action: "goto", value: "/login" }]));
  const [validationResult, setValidationResult] = useState<DSLValidationResult | null>(null);

  const caseQuery = useQuery({
    queryKey: ["case-detail", caseId],
    queryFn: () => getCaseDetail(Number(caseId)),
    enabled: isEditMode,
  });

  useEffect(() => {
    if (!caseQuery.data) {
      return;
    }
    form.setFieldsValue({
      name: caseQuery.data.name,
      description: caseQuery.data.description ?? "",
      project_id: caseQuery.data.project_id,
    });
    setStepsJson(formatStepsJson(caseQuery.data.steps));
  }, [caseQuery.data, form]);

  const saveMutation = useMutation({
    mutationFn: async ({ executeAfterSave }: { executeAfterSave: boolean }) => {
      const values = await form.validateFields();
      const dslPayload = buildDslPayload(values, stepsJson);
      const validated = await validateDslCase(dslPayload);
      setValidationResult(validated);

      const payload: CaseMutationPayload = {
        project_id: values.project_id,
        actor_user_id: 1,
        ...validated.case,
      };

      const storedCase = isEditMode
        ? await updateCase(Number(caseId), payload)
        : await createCase(payload);

      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["cases"] }),
        queryClient.invalidateQueries({ queryKey: ["case-detail", String(storedCase.id)] }),
      ]);

      if (executeAfterSave) {
        const execution = await executeCase(storedCase.id, { actor_user_id: 1 });
        await queryClient.invalidateQueries({ queryKey: ["executions"] });
        return { mode: "execute" as const, storedCaseId: storedCase.id, executionId: execution.id };
      }

      return { mode: "save" as const, storedCaseId: storedCase.id, executionId: null };
    },
    onSuccess: ({ mode, storedCaseId, executionId }) => {
      if (mode === "execute" && executionId) {
        void navigate(`/executions/${executionId}`);
        return;
      }
      void messageApi.success("用例已保存。");
      void navigate(`/cases/${storedCaseId}/edit`);
    },
    onError: (error: Error) => {
      void messageApi.error(error.message);
    },
  });

  const validateMutation = useMutation({
    mutationFn: async () => {
      const values = await form.validateFields();
      const payload = buildDslPayload(values, stepsJson);
      return validateDslCase(payload);
    },
    onSuccess: (result) => {
      setValidationResult(result);
      void messageApi.success("DSL 校验通过。");
    },
    onError: (error: Error) => {
      setValidationResult(null);
      void messageApi.error(error.message);
    },
  });

  if (caseQuery.isLoading) {
    return <LoadingBlock />;
  }

  if (caseQuery.isError) {
    return <ErrorBlock message={caseQuery.error.message} />;
  }

  return (
    <>
      {contextHolder}
      <div className="page-header">
        <h1 className="page-title">{isEditMode ? "用例工作台" : "新建用例"}</h1>
        <p className="page-subtitle">编辑基础元数据与 DSL Steps JSON，先校验，再保存或执行。</p>
      </div>
      <Space direction="vertical" size="large" style={{ width: "100%" }}>
        <Card>
          <Form form={form} layout="vertical" initialValues={{ project_id: 1 }}>
            <div className="workbench-grid">
              <Form.Item label="用例名称" name="name" rules={[{ required: true, message: "请输入用例名称" }]}>
                <Input placeholder="例如：登录冒烟" />
              </Form.Item>
              <Form.Item
                label="项目 ID"
                name="project_id"
                rules={[{ required: true, message: "请输入项目 ID" }]}
              >
                <InputNumber min={1} style={{ width: "100%" }} />
              </Form.Item>
            </div>
            <Form.Item label="描述" name="description">
              <Input.TextArea rows={3} placeholder="说明该用例验证的业务链路" />
            </Form.Item>
          </Form>
        </Card>

        <Card
          title="DSL Steps JSON"
          extra={
            <Typography.Text type="secondary">
              只编辑 `steps` 数组，名称和描述走上面的表单
            </Typography.Text>
          }
        >
          <Input.TextArea
            value={stepsJson}
            rows={18}
            onChange={(event) => setStepsJson(event.target.value)}
            spellCheck={false}
            style={{ fontFamily: "Consolas, 'Courier New', monospace" }}
          />
        </Card>

        {validationResult ? (
          <Alert
            type="success"
            showIcon
            message="DSL 校验通过"
            description={
              <Space wrap>
                <Typography.Text>支持动作：</Typography.Text>
                {validationResult.supported_actions.map((action) => (
                  <Tag key={action}>{action}</Tag>
                ))}
              </Space>
            }
          />
        ) : null}

        <Space wrap>
          <Button loading={validateMutation.isPending} onClick={() => validateMutation.mutate()}>
            校验 DSL
          </Button>
          <Button
            type="primary"
            loading={saveMutation.isPending}
            onClick={() => saveMutation.mutate({ executeAfterSave: false })}
          >
            保存
          </Button>
          <Button
            type="primary"
            ghost
            loading={saveMutation.isPending}
            onClick={() => saveMutation.mutate({ executeAfterSave: true })}
          >
            保存并执行
          </Button>
        </Space>
      </Space>
    </>
  );
}
