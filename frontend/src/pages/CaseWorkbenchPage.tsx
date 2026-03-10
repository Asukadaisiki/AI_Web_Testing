import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Select,
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

type EditorMode = "structured" | "json";
type StepAction = "goto" | "click" | "input" | "wait_for" | "assert_text" | "assert_url_contains";

const ACTION_OPTIONS: { label: string; value: StepAction }[] = [
  { label: "goto", value: "goto" },
  { label: "click", value: "click" },
  { label: "input", value: "input" },
  { label: "wait_for", value: "wait_for" },
  { label: "assert_text", value: "assert_text" },
  { label: "assert_url_contains", value: "assert_url_contains" },
];

const STEP_TEMPLATES: { label: string; value: string; steps: DSLStep[] }[] = [
  {
    label: "登录冒烟模板",
    value: "login-smoke",
    steps: [
      { action: "goto", value: "/login" },
      { action: "input", target: "用户名输入框", value: "admin" },
      { action: "input", target: "密码输入框", value: "123456" },
      { action: "click", target: "登录按钮" },
      { action: "assert_url_contains", value: "/dashboard" },
    ],
  },
  {
    label: "跳转断言模板",
    value: "goto-assert",
    steps: [
      { action: "goto", value: "/" },
      { action: "assert_text", target: "欢迎文案", value: "欢迎使用" },
    ],
  },
];

function createDefaultStep(action: StepAction = "goto"): DSLStep {
  switch (action) {
    case "goto":
      return { action, value: "/" };
    case "click":
      return { action, target: "按钮" };
    case "input":
      return { action, target: "输入框", value: "" };
    case "wait_for":
      return { action, target: "目标元素", timeout_ms: 5000 };
    case "assert_text":
      return { action, target: "目标元素", value: "期望文本" };
    case "assert_url_contains":
      return { action, value: "/expected" };
  }
}

function formatStepsJson(steps: DSLStep[]) {
  return JSON.stringify(steps, null, 2);
}

function parseStepsJson(stepsJson: string): DSLStep[] {
  let parsedSteps: unknown;
  try {
    parsedSteps = JSON.parse(stepsJson);
  } catch {
    throw new Error("DSL Steps JSON 不是合法的 JSON。");
  }

  if (!Array.isArray(parsedSteps)) {
    throw new Error("DSL Steps JSON 必须是数组。");
  }

  return parsedSteps as DSLStep[];
}

function buildDslPayload(values: WorkbenchFormValues, stepsJson: string): DSLCasePayload {
  return {
    name: values.name,
    description: values.description || null,
    steps: parseStepsJson(stepsJson),
  };
}

function actionNeedsTarget(action: StepAction) {
  return action === "click" || action === "input" || action === "wait_for" || action === "assert_text";
}

function actionNeedsValue(action: StepAction) {
  return action === "goto" || action === "input" || action === "assert_text" || action === "assert_url_contains";
}

function actionNeedsTimeout(action: StepAction) {
  return action === "wait_for";
}

function normalizeStepForAction(step: DSLStep, action: StepAction): DSLStep {
  const nextStep = createDefaultStep(action);
  if (actionNeedsTarget(action) && typeof step.target === "string") {
    nextStep.target = step.target;
  }
  if (actionNeedsValue(action) && typeof step.value === "string") {
    nextStep.value = step.value;
  }
  if (actionNeedsTimeout(action) && typeof step.timeout_ms === "number") {
    nextStep.timeout_ms = step.timeout_ms;
  }
  return nextStep;
}

export function CaseWorkbenchPage() {
  const { caseId } = useParams<{ caseId: string }>();
  const isEditMode = Boolean(caseId);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<WorkbenchFormValues>();
  const [messageApi, contextHolder] = message.useMessage();
  const [editorMode, setEditorMode] = useState<EditorMode>("structured");
  const [templateValue, setTemplateValue] = useState<string>(STEP_TEMPLATES[0].value);
  const [structuredSteps, setStructuredSteps] = useState<DSLStep[]>([createDefaultStep()]);
  const [stepsJson, setStepsJson] = useState<string>(formatStepsJson([createDefaultStep()]));
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
    setStructuredSteps(caseQuery.data.steps);
    setStepsJson(formatStepsJson(caseQuery.data.steps));
  }, [caseQuery.data, form]);

  const syncStructuredSteps = (nextSteps: DSLStep[]) => {
    setStructuredSteps(nextSteps);
    setStepsJson(formatStepsJson(nextSteps));
  };

  const buildStepsJsonForSubmit = () => (editorMode === "json" ? stepsJson : formatStepsJson(structuredSteps));

  const changeEditorMode = (nextMode: EditorMode) => {
    if (nextMode === editorMode) {
      return;
    }
    if (nextMode === "structured") {
      try {
        const parsedSteps = parseStepsJson(stepsJson);
        setStructuredSteps(parsedSteps);
      } catch (error) {
        void messageApi.error((error as Error).message);
        return;
      }
    } else {
      setStepsJson(formatStepsJson(structuredSteps));
    }
    setEditorMode(nextMode);
  };

  const updateStructuredStep = (index: number, updater: (step: DSLStep) => DSLStep) => {
    const nextSteps = structuredSteps.map((step, stepIndex) => (stepIndex === index ? updater(step) : step));
    syncStructuredSteps(nextSteps);
  };

  const moveStep = (index: number, direction: -1 | 1) => {
    const targetIndex = index + direction;
    if (targetIndex < 0 || targetIndex >= structuredSteps.length) {
      return;
    }
    const nextSteps = [...structuredSteps];
    const [removed] = nextSteps.splice(index, 1);
    nextSteps.splice(targetIndex, 0, removed);
    syncStructuredSteps(nextSteps);
  };

  const templateOptions = useMemo(
    () => STEP_TEMPLATES.map((template) => ({ label: template.label, value: template.value })),
    [],
  );

  const saveMutation = useMutation({
    mutationFn: async ({ executeAfterSave }: { executeAfterSave: boolean }) => {
      const values = await form.validateFields();
      const dslPayload = buildDslPayload(values, buildStepsJsonForSubmit());
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
      return validateDslCase(buildDslPayload(values, buildStepsJsonForSubmit()));
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
        <p className="page-subtitle">默认使用结构化步骤编辑；必要时可切到原始 JSON 模式。</p>
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
          title="步骤编辑器"
          extra={
            <Space.Compact>
              <Button
                type={editorMode === "structured" ? "primary" : "default"}
                aria-pressed={editorMode === "structured"}
                onClick={() => changeEditorMode("structured")}
              >
                结构化编辑
              </Button>
              <Button
                type={editorMode === "json" ? "primary" : "default"}
                aria-pressed={editorMode === "json"}
                onClick={() => changeEditorMode("json")}
              >
                原始 JSON
              </Button>
            </Space.Compact>
          }
        >
          <Space direction="vertical" size="large" style={{ width: "100%" }}>
            <Space wrap>
              <Select
                value={templateValue}
                options={templateOptions}
                style={{ width: 220 }}
                onChange={(value) => setTemplateValue(value)}
              />
              <Button
                onClick={() => {
                  const selectedTemplate = STEP_TEMPLATES.find((item) => item.value === templateValue);
                  if (!selectedTemplate) {
                    return;
                  }
                  syncStructuredSteps(selectedTemplate.steps);
                  setEditorMode("structured");
                }}
              >
                应用模板
              </Button>
              <Button
                onClick={() => {
                  syncStructuredSteps([...structuredSteps, createDefaultStep()]);
                  setEditorMode("structured");
                }}
              >
                新增步骤
              </Button>
            </Space>

            {editorMode === "structured" ? (
              <Space direction="vertical" size="middle" style={{ width: "100%" }}>
                {structuredSteps.map((step, index) => {
                  const action = step.action as StepAction;
                  return (
                    <Card
                      key={`step-${index}`}
                      size="small"
                      title={`Step ${index + 1}`}
                      extra={<Tag>{action}</Tag>}
                    >
                      <Space direction="vertical" size="middle" style={{ width: "100%" }}>
                        <div className="structured-step-grid">
                          <div>
                            <Typography.Text type="secondary">动作</Typography.Text>
                            <Select
                              value={action}
                              options={ACTION_OPTIONS}
                              style={{ width: "100%", marginTop: 8 }}
                              onChange={(nextAction) =>
                                updateStructuredStep(index, (currentStep) =>
                                  normalizeStepForAction(currentStep, nextAction),
                                )
                              }
                            />
                          </div>
                          {actionNeedsTarget(action) ? (
                            <div>
                              <Typography.Text type="secondary">目标</Typography.Text>
                              <Input
                                style={{ marginTop: 8 }}
                                value={typeof step.target === "string" ? step.target : ""}
                                onChange={(event) =>
                                  updateStructuredStep(index, (currentStep) => ({
                                    ...currentStep,
                                    target: event.target.value,
                                  }))
                                }
                              />
                            </div>
                          ) : null}
                          {actionNeedsValue(action) ? (
                            <div>
                              <Typography.Text type="secondary">
                                {action === "goto" || action === "assert_url_contains" ? "值 / URL" : "值"}
                              </Typography.Text>
                              <Input
                                style={{ marginTop: 8 }}
                                value={typeof step.value === "string" ? step.value : ""}
                                onChange={(event) =>
                                  updateStructuredStep(index, (currentStep) => ({
                                    ...currentStep,
                                    value: event.target.value,
                                  }))
                                }
                              />
                            </div>
                          ) : null}
                          {actionNeedsTimeout(action) ? (
                            <div>
                              <Typography.Text type="secondary">超时 (ms)</Typography.Text>
                              <InputNumber
                                min={1}
                                max={60000}
                                style={{ width: "100%", marginTop: 8 }}
                                value={typeof step.timeout_ms === "number" ? step.timeout_ms : 5000}
                                onChange={(value) =>
                                  updateStructuredStep(index, (currentStep) => ({
                                    ...currentStep,
                                    timeout_ms: typeof value === "number" ? value : 5000,
                                  }))
                                }
                              />
                            </div>
                          ) : null}
                        </div>
                        <Space wrap>
                          <Button onClick={() => moveStep(index, -1)} disabled={index === 0}>
                            上移
                          </Button>
                          <Button
                            onClick={() => moveStep(index, 1)}
                            disabled={index === structuredSteps.length - 1}
                          >
                            下移
                          </Button>
                          <Button
                            danger
                            onClick={() => {
                              if (structuredSteps.length === 1) {
                                syncStructuredSteps([createDefaultStep()]);
                                return;
                              }
                              syncStructuredSteps(structuredSteps.filter((_, stepIndex) => stepIndex !== index));
                            }}
                          >
                            删除
                          </Button>
                        </Space>
                      </Space>
                    </Card>
                  );
                })}
              </Space>
            ) : (
              <Input.TextArea
                value={stepsJson}
                rows={18}
                onChange={(event) => setStepsJson(event.target.value)}
                spellCheck={false}
                style={{ fontFamily: "Consolas, 'Courier New', monospace" }}
              />
            )}
          </Space>
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
