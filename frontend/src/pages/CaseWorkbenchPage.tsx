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
  generateDslCase,
  getAISettings,
  getCaseDetail,
  recordDslGenerationFeedback,
  updateCase,
  validateDslCase,
} from "../services/api";
import type {
  AISettings,
  CaseMutationPayload,
  DSLCasePayload,
  DSLCaseInputContract,
  DSLCaseOutputContract,
  DSLStep,
  DSLValidationResult,
  DSLVariableSource,
  DSLVariableType,
  GenerateDslImportMode,
  GenerateDslMode,
  GenerateDslResponse,
  StoredCaseDetail,
} from "../types/api";

type WorkbenchFormValues = {
  name: string;
  description?: string;
  project_id: number;
  base_url?: string;
};

type WorkbenchDraft = {
  name: string;
  description?: string;
  project_id: number;
  base_url?: string;
  inputContracts: DSLCaseInputContract[];
  outputContracts: DSLCaseOutputContract[];
  editorMode: EditorMode;
  structuredSteps: DSLStep[];
  stepsJson: string;
};

type EditorMode = "structured" | "json";
type StepAction = "goto" | "click" | "input" | "wait_for" | "assert_text" | "assert_url_contains";
type StepTemplate = {
  label: string;
  value: string;
  baseUrl: string;
  steps: DSLStep[];
};

type RecordedGenerationFeedback =
  | { status: "accepted"; importMode: GenerateDslImportMode }
  | { status: "rejected" };

const ACTION_OPTIONS: { label: string; value: StepAction }[] = [
  { label: "goto", value: "goto" },
  { label: "click", value: "click" },
  { label: "input", value: "input" },
  { label: "wait_for", value: "wait_for" },
  { label: "assert_text", value: "assert_text" },
  { label: "assert_url_contains", value: "assert_url_contains" },
];

const VARIABLE_TYPE_OPTIONS: { label: string; value: DSLVariableType }[] = [
  { label: "string", value: "string" },
  { label: "number", value: "number" },
  { label: "boolean", value: "boolean" },
  { label: "object", value: "object" },
  { label: "array", value: "array" },
];

const OUTPUT_SOURCE_OPTIONS: { label: string; value: DSLVariableSource }[] = [
  { label: "latest_url", value: "latest_url" },
  { label: "error_message", value: "error_message" },
  { label: "status", value: "status" },
  { label: "last_step_url", value: "last_step_url" },
  { label: "last_step_page_title", value: "last_step_page_title" },
  { label: "last_step_target", value: "last_step_target" },
  { label: "last_step_value", value: "last_step_value" },
  { label: "last_step_error_message", value: "last_step_error_message" },
];

const STEP_TEMPLATES: StepTemplate[] = [
  {
    label: "公共冒烟模板",
    value: "public-smoke",
    baseUrl: "https://example.com",
    steps: [
      { action: "goto", value: "/" },
      { action: "assert_url_contains", value: "example.com" },
    ],
  },
  {
    label: "基础跳转模板",
    value: "basic-navigation",
    baseUrl: "https://example.com",
    steps: [
      { action: "goto", value: "/" },
      { action: "wait_for", target: "Example Domain", timeout_ms: 5000 },
      { action: "assert_url_contains", value: "example.com" },
    ],
  },
];

const GENERATION_MODE_OPTIONS: { label: string; value: GenerateDslMode }[] = [
  { label: "完整草案", value: "draft" },
  { label: "仅重写步骤", value: "strict_steps_only" },
];

const GENERATION_CONTEXT_OPTIONS = [
  { label: "基于空白需求", value: "blank" },
  { label: "基于当前 DSL 重写", value: "current_case" },
  { label: "基于当前步骤补全", value: "current_steps" },
];

const GENERATION_IMPORT_MODE_OPTIONS: { label: string; value: GenerateDslImportMode }[] = [
  { label: "整单替换", value: "replace" },
  { label: "仅导入步骤", value: "steps_only" },
  { label: "仅合并契约", value: "contracts_only" },
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

function createDefaultInputContract(): DSLCaseInputContract {
  return {
    name: "contextVar",
    context_key: "context_var",
    value_type: "string",
    required: true,
    description: null,
  };
}

function createDefaultOutputContract(): DSLCaseOutputContract {
  return {
    name: "resultVar",
    context_key: "result_var",
    value_type: "string",
    source: "latest_url",
    description: null,
  };
}

function buildDslPayload(
  values: WorkbenchFormValues,
  stepsJson: string,
  inputContracts: DSLCaseInputContract[],
  outputContracts: DSLCaseOutputContract[],
): DSLCasePayload {
  return {
    name: values.name,
    description: values.description || null,
    base_url: values.base_url?.trim() || null,
    input_contract: inputContracts,
    output_contract: outputContracts,
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

function buildDraftKey(caseId?: string) {
  return caseId ? `case-draft:${caseId}` : "case-draft:new";
}

function createDefaultDraft(): WorkbenchDraft {
  const steps = [createDefaultStep()];
  return {
    name: "",
    description: "",
    project_id: 1,
    base_url: "",
    inputContracts: [],
    outputContracts: [],
    editorMode: "structured",
    structuredSteps: steps,
    stepsJson: formatStepsJson(steps),
  };
}

const DEFAULT_FORM_VALUES: WorkbenchFormValues = {
  name: "",
  description: "",
  project_id: 1,
  base_url: "",
};

function readDraft(key: string): WorkbenchDraft | null {
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) {
      return null;
    }
    return JSON.parse(raw) as WorkbenchDraft;
  } catch {
    return null;
  }
}

function writeDraft(key: string, draft: WorkbenchDraft) {
  try {
    window.localStorage.setItem(key, JSON.stringify(draft));
  } catch {
    // Ignore storage failures in the workbench.
  }
}

function removeDraft(key: string) {
  try {
    window.localStorage.removeItem(key);
  } catch {
    // Ignore storage failures in the workbench.
  }
}

function isAbsoluteUrl(value: string) {
  return value.startsWith("http://") || value.startsWith("https://");
}

function hasRelativeGotoStep(steps: DSLStep[]) {
  return steps.some((step) => step.action === "goto" && typeof step.value === "string" && !isAbsoluteUrl(step.value));
}

function buildDraftSignature(draft: WorkbenchDraft) {
  return JSON.stringify(draft);
}

function mergeContractsByContextKey<T extends { context_key: string }>(currentContracts: T[], nextContracts: T[]) {
  if (!nextContracts.length) {
    return currentContracts;
  }

  const nextContractMap = new Map(nextContracts.map((contract) => [contract.context_key, contract]));
  const mergedContracts = currentContracts.map((contract) => nextContractMap.get(contract.context_key) ?? contract);
  const existingContextKeys = new Set(currentContracts.map((contract) => contract.context_key));

  for (const contract of nextContracts) {
    if (!existingContextKeys.has(contract.context_key)) {
      mergedContracts.push(contract);
    }
  }

  return mergedContracts;
}

function formatGeneratedCase(caseDraft: DSLCasePayload) {
  return JSON.stringify(caseDraft, null, 2);
}

function formatDslGenerationStatus(settings: AISettings) {
  const model = settings.ai_dsl_model?.trim() || "未配置";
  const enabled = settings.enable_ai_dsl_generate ? "已启用" : "未启用";
  const strictMode = settings.ai_dsl_strict_mode ? "严格模式" : "宽松模式";
  const autoRepair = settings.ai_dsl_allow_auto_repair ? "自动修正开启" : "自动修正关闭";
  return `当前生成配置：${enabled}，模型 ${model}，${strictMode}，${autoRepair}`;
}

function formatImportModeLabel(mode: GenerateDslImportMode) {
  if (mode === "replace") {
    return "替换当前 DSL";
  }
  if (mode === "steps_only") {
    return "仅导入步骤";
  }
  return "仅合并契约";
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
  const [inputContracts, setInputContracts] = useState<DSLCaseInputContract[]>([]);
  const [outputContracts, setOutputContracts] = useState<DSLCaseOutputContract[]>([]);
  const [structuredSteps, setStructuredSteps] = useState<DSLStep[]>([createDefaultStep()]);
  const [stepsJson, setStepsJson] = useState<string>(formatStepsJson([createDefaultStep()]));
  const [validationResult, setValidationResult] = useState<DSLValidationResult | null>(null);
  const [generationPrompt, setGenerationPrompt] = useState("");
  const [generationMode, setGenerationMode] = useState<GenerateDslMode>("draft");
  const [generationModeDirty, setGenerationModeDirty] = useState(false);
  const [generationContextSource, setGenerationContextSource] = useState<"blank" | "current_case" | "current_steps">(
    "blank",
  );
  const [generationImportMode, setGenerationImportMode] = useState<GenerateDslImportMode>("replace");
  const [preserveContracts, setPreserveContracts] = useState(true);
  const [generatedDraft, setGeneratedDraft] = useState<GenerateDslResponse | null>(null);
  const [generationFeedbackError, setGenerationFeedbackError] = useState<string | null>(null);
  const [recordedGenerationFeedback, setRecordedGenerationFeedback] = useState<RecordedGenerationFeedback | null>(null);
  const [pendingDraft, setPendingDraft] = useState<WorkbenchDraft | null>(null);
  const [isHydrated, setIsHydrated] = useState(false);
  const [baselineSignature, setBaselineSignature] = useState<string>(buildDraftSignature(createDefaultDraft()));
  const draftKey = useMemo(() => buildDraftKey(caseId), [caseId]);
  const watchedName = Form.useWatch("name", form);
  const watchedDescription = Form.useWatch("description", form);
  const watchedProjectId = Form.useWatch("project_id", form);
  const watchedBaseUrl = Form.useWatch("base_url", form);

  const caseQuery = useQuery({
    queryKey: ["case-detail", caseId],
    queryFn: () => getCaseDetail(Number(caseId)),
    enabled: isEditMode,
  });
  const aiSettingsQuery = useQuery({
    queryKey: ["ai-settings"],
    queryFn: getAISettings,
  });

  useEffect(() => {
    if (!aiSettingsQuery.data || generationModeDirty) {
      return;
    }
    setGenerationMode(aiSettingsQuery.data.ai_dsl_strict_mode ? "strict_steps_only" : "draft");
  }, [aiSettingsQuery.data, generationModeDirty]);

  const applyStoredCase = (storedCase: StoredCaseDetail) => {
    form.setFieldsValue({
      name: storedCase.name,
      description: storedCase.description ?? "",
      project_id: storedCase.project_id,
      base_url: storedCase.base_url ?? "",
    });
    setInputContracts(storedCase.input_contract);
    setOutputContracts(storedCase.output_contract);
    setStructuredSteps(storedCase.steps);
    setStepsJson(formatStepsJson(storedCase.steps));
    setEditorMode("structured");
    setValidationResult(null);
  };

  const toDraftFromStoredCase = (storedCase: StoredCaseDetail): WorkbenchDraft => ({
    name: storedCase.name,
    description: storedCase.description ?? "",
    project_id: storedCase.project_id,
    base_url: storedCase.base_url ?? "",
    inputContracts: storedCase.input_contract,
    outputContracts: storedCase.output_contract,
    editorMode: "structured",
    structuredSteps: storedCase.steps,
    stepsJson: formatStepsJson(storedCase.steps),
  });

  const applyDraft = (draft: WorkbenchDraft) => {
    form.setFieldsValue({
      name: draft.name,
      description: draft.description ?? "",
      project_id: draft.project_id,
      base_url: draft.base_url ?? "",
    });
    setInputContracts(draft.inputContracts ?? []);
    setOutputContracts(draft.outputContracts ?? []);
    setStructuredSteps(draft.structuredSteps);
    setStepsJson(draft.stepsJson);
    setEditorMode(draft.editorMode);
    setValidationResult(null);
  };

  useEffect(() => {
    setIsHydrated(false);
    setPendingDraft(null);

    if (isEditMode) {
      if (!caseQuery.data) {
        return;
      }
      applyStoredCase(caseQuery.data);
      setBaselineSignature(buildDraftSignature(toDraftFromStoredCase(caseQuery.data)));
      setPendingDraft(readDraft(draftKey));
      setIsHydrated(true);
      return;
    }

    const savedDraft = readDraft(draftKey);
    if (savedDraft) {
      applyDraft(savedDraft);
    } else {
      const defaultDraft = createDefaultDraft();
      applyDraft(defaultDraft);
    }
    setBaselineSignature(buildDraftSignature(createDefaultDraft()));
    setIsHydrated(true);
  }, [caseQuery.data, draftKey, form, isEditMode]);

  useEffect(() => {
    if (!isHydrated || pendingDraft !== null || watchedProjectId === undefined) {
      return;
    }

    const draft = {
      name: watchedName ?? "",
      description: watchedDescription ?? "",
      project_id: watchedProjectId,
      base_url: watchedBaseUrl ?? "",
      inputContracts,
      outputContracts,
      editorMode,
      structuredSteps,
      stepsJson,
    };

    if (buildDraftSignature(draft) === baselineSignature) {
      removeDraft(draftKey);
      return;
    }

    writeDraft(draftKey, draft);
  }, [
    baselineSignature,
    draftKey,
    editorMode,
    inputContracts,
    isHydrated,
    outputContracts,
    pendingDraft,
    stepsJson,
    structuredSteps,
    watchedBaseUrl,
    watchedDescription,
    watchedName,
    watchedProjectId,
  ]);

  const syncStructuredSteps = (nextSteps: DSLStep[]) => {
    setStructuredSteps(nextSteps);
    setStepsJson(formatStepsJson(nextSteps));
    setValidationResult(null);
  };

  const syncInputContracts = (nextContracts: DSLCaseInputContract[]) => {
    setInputContracts(nextContracts);
    setValidationResult(null);
  };

  const syncOutputContracts = (nextContracts: DSLCaseOutputContract[]) => {
    setOutputContracts(nextContracts);
    setValidationResult(null);
  };

  const buildStepsJsonForSubmit = () => (editorMode === "json" ? stepsJson : formatStepsJson(structuredSteps));
  const buildCurrentDslCase = (): DSLCasePayload => {
    const values = form.getFieldsValue();
    return buildDslPayload(values, buildStepsJsonForSubmit(), inputContracts, outputContracts);
  };

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
    setValidationResult(null);
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

  const currentStepsForWarning = useMemo(() => {
    if (editorMode === "json") {
      try {
        return parseStepsJson(stepsJson);
      } catch {
        return null;
      }
    }
    return structuredSteps;
  }, [editorMode, stepsJson, structuredSteps]);

  const shouldWarnMissingBaseUrl =
    !String(watchedBaseUrl ?? "").trim() &&
    currentStepsForWarning !== null &&
    hasRelativeGotoStep(currentStepsForWarning);

  const saveMutation = useMutation({
    mutationFn: async ({ executeAfterSave }: { executeAfterSave: boolean }) => {
      const values = await form.validateFields();
      const dslPayload = buildDslPayload(values, buildStepsJsonForSubmit(), inputContracts, outputContracts);
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
      removeDraft(draftKey);
      setPendingDraft(null);
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
      return validateDslCase(buildDslPayload(values, buildStepsJsonForSubmit(), inputContracts, outputContracts));
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

  const generateMutation = useMutation({
    mutationFn: async () => {
      const values = form.getFieldsValue();
      const shouldIncludeCurrentCase = generationContextSource === "current_case";
      const shouldIncludeCurrentSteps = generationContextSource === "current_steps";
      const currentCase = shouldIncludeCurrentCase || shouldIncludeCurrentSteps ? buildCurrentDslCase() : null;
      return generateDslCase({
        prompt: generationPrompt,
        base_url: String(values.base_url ?? "").trim() || null,
        actor_user_id: 1,
        generation_mode: generationMode,
        import_mode: generationImportMode,
        current_case: shouldIncludeCurrentCase ? currentCase : null,
        current_steps: shouldIncludeCurrentSteps && currentCase ? currentCase.steps : null,
        current_input_contract: preserveContracts ? inputContracts : null,
        current_output_contract: preserveContracts ? outputContracts : null,
        preserve_contracts: preserveContracts,
      });
    },
    onSuccess: (result) => {
      setGeneratedDraft(result);
      setGenerationFeedbackError(null);
      setRecordedGenerationFeedback(null);
      void messageApi.success("AI DSL 草案已生成。");
    },
    onError: (error: Error) => {
      setGeneratedDraft(null);
      setGenerationFeedbackError(null);
      setRecordedGenerationFeedback(null);
      void messageApi.error(error.message);
    },
  });

  const feedbackMutation = useMutation({
    mutationFn: async ({
      generationId,
      status,
      importMode,
    }: {
      generationId: number;
      status: "accepted" | "rejected";
      importMode?: GenerateDslImportMode;
    }) =>
      recordDslGenerationFeedback(generationId, {
        actor_user_id: 1,
        feedback_status: status,
        feedback_import_mode: importMode ?? null,
      }),
  });

  const applyGeneratedDraft = async (mode: GenerateDslImportMode) => {
    if (!generatedDraft) {
      return;
    }
    const generationId = generatedDraft.generation_id;
    if (mode === "replace") {
      const nextCase = generatedDraft.case;
      form.setFieldsValue({
        name: nextCase.name,
        description: nextCase.description ?? "",
        project_id: form.getFieldValue("project_id") ?? 1,
        base_url: nextCase.base_url ?? "",
      });
      syncInputContracts(nextCase.input_contract);
      syncOutputContracts(nextCase.output_contract);
      syncStructuredSteps(nextCase.steps);
      setStepsJson(formatStepsJson(nextCase.steps));
      setEditorMode("structured");
      setValidationResult(null);
    } else if (mode === "contracts_only") {
      syncInputContracts(mergeContractsByContextKey(inputContracts, generatedDraft.case.input_contract));
      syncOutputContracts(mergeContractsByContextKey(outputContracts, generatedDraft.case.output_contract));
      setValidationResult(null);
    } else {
      syncStructuredSteps(generatedDraft.case.steps);
      setStepsJson(formatStepsJson(generatedDraft.case.steps));
      setEditorMode("structured");
      setValidationResult(null);
    }
    setGenerationFeedbackError(null);
    try {
      await feedbackMutation.mutateAsync({
        generationId,
        status: "accepted",
        importMode: mode,
      });
      setRecordedGenerationFeedback({ status: "accepted", importMode: mode });
      void messageApi.success(`已记录草案采纳反馈：${formatImportModeLabel(mode)}。`);
    } catch (error) {
      setGenerationFeedbackError(`反馈未记录，可重试：${(error as Error).message}`);
      void messageApi.error(`反馈未记录：${(error as Error).message}`);
    }
  };

  const rejectGeneratedDraft = async () => {
    if (!generatedDraft) {
      return;
    }
    const generationId = generatedDraft.generation_id;
    setGenerationFeedbackError(null);
    try {
      await feedbackMutation.mutateAsync({
        generationId,
        status: "rejected",
      });
      setRecordedGenerationFeedback({ status: "rejected" });
      setGeneratedDraft(null);
      void messageApi.success("已记录草案放弃反馈。");
    } catch (error) {
      setGenerationFeedbackError(`反馈未记录，可重试：${(error as Error).message}`);
      void messageApi.error(`反馈未记录：${(error as Error).message}`);
    }
  };

  const feedbackLocked = feedbackMutation.isPending || recordedGenerationFeedback?.status === "accepted";

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
        <Space align="start" style={{ justifyContent: "space-between", width: "100%" }} wrap>
          <div>
            <h1 className="page-title">{isEditMode ? "用例工作台" : "新建用例"}</h1>
            <p className="page-subtitle">默认使用结构化步骤编辑；必要时可切到原始 JSON 模式。</p>
          </div>
          <Space wrap>
            <Button onClick={() => navigate("/cases")}>返回用例列表</Button>
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
      </div>
      <Space direction="vertical" size="large" style={{ width: "100%" }}>
        {pendingDraft ? (
          <Alert
            type="warning"
            showIcon
            message="检测到本地草稿"
            description={
              <Space wrap>
                <Typography.Text>当前编辑页存在未保存的本地草稿，可恢复或丢弃。</Typography.Text>
                <Button
                  size="small"
                  type="primary"
                  onClick={() => {
                    applyDraft(pendingDraft);
                    setPendingDraft(null);
                  }}
                >
                  恢复草稿
                </Button>
                <Button
                  size="small"
                  onClick={() => {
                    removeDraft(draftKey);
                    setPendingDraft(null);
                  }}
                >
                  丢弃草稿
                </Button>
              </Space>
            }
          />
        ) : null}

        {shouldWarnMissingBaseUrl ? (
          <Alert
            type="warning"
            showIcon
            message="当前用例包含相对路径 goto"
            description="请先填写用例 Base URL，否则执行时会直接失败。"
          />
        ) : null}

        <Card>
          <Form form={form} layout="vertical" initialValues={DEFAULT_FORM_VALUES}>
            <div className="workbench-grid">
              <Form.Item label="用例名称" name="name" rules={[{ required: true, message: "请输入用例名称" }]}>
                <Input placeholder="例如：公共冒烟" />
              </Form.Item>
              <Form.Item
                label="项目 ID"
                name="project_id"
                rules={[{ required: true, message: "请输入项目 ID" }]}
              >
                <InputNumber min={1} style={{ width: "100%" }} />
              </Form.Item>
            </div>
            <Form.Item label="用例 Base URL" name="base_url">
              <Input placeholder="例如：https://example.com" />
            </Form.Item>
            <Form.Item label="描述" name="description">
              <Input.TextArea rows={3} placeholder="说明该用例验证的业务链路" />
            </Form.Item>
          </Form>
        </Card>

        <Card title="自然语言生成">
          <Space direction="vertical" size="middle" style={{ width: "100%" }}>
            <Typography.Text type="secondary">
              输入一句自然语言需求，生成可编辑 DSL 草案；生成结果不会直接保存或执行。
            </Typography.Text>
            {aiSettingsQuery.data ? (
              <Alert
                type={aiSettingsQuery.data.enable_ai_dsl_generate ? "info" : "warning"}
                showIcon
                message="当前生成配置"
                description={formatDslGenerationStatus(aiSettingsQuery.data)}
              />
            ) : null}
            <div className="structured-step-grid">
              <div>
                <Typography.Text type="secondary">生成模式</Typography.Text>
                <Select
                  style={{ marginTop: 8, width: "100%" }}
                  value={generationMode}
                  options={GENERATION_MODE_OPTIONS}
                  onChange={(value) => {
                    setGenerationModeDirty(true);
                    setGenerationMode(value);
                  }}
                />
              </div>
              <div>
                <Typography.Text type="secondary">上下文来源</Typography.Text>
                <Select
                  style={{ marginTop: 8, width: "100%" }}
                  value={generationContextSource}
                  options={GENERATION_CONTEXT_OPTIONS}
                  onChange={(value) => setGenerationContextSource(value)}
                />
              </div>
              <div>
                <Typography.Text type="secondary">预期导入方式</Typography.Text>
                <Select
                  style={{ marginTop: 8, width: "100%" }}
                  value={generationImportMode}
                  options={GENERATION_IMPORT_MODE_OPTIONS}
                  onChange={(value) => setGenerationImportMode(value)}
                />
              </div>
            </div>
            <div className="workbench-grid">
              <div>
                <Typography.Text type="secondary">保留当前契约</Typography.Text>
                <Select
                  aria-label="保留当前契约"
                  style={{ marginTop: 8, width: "100%" }}
                  value={preserveContracts ? "yes" : "no"}
                  options={[
                    { label: "保留", value: "yes" },
                    { label: "不保留", value: "no" },
                  ]}
                  onChange={(value) => setPreserveContracts(value === "yes")}
                />
              </div>
            </div>
            <Input.TextArea
              aria-label="自然语言需求"
              rows={4}
              placeholder="例如：打开 example.com，验证 URL 包含 example.com"
              value={generationPrompt}
              onChange={(event) => setGenerationPrompt(event.target.value)}
            />
            <Space wrap>
              <Button
                type="primary"
                loading={generateMutation.isPending}
                disabled={!generationPrompt.trim() || feedbackMutation.isPending}
                onClick={() => generateMutation.mutate()}
              >
                生成 DSL
              </Button>
              {generatedDraft ? (
                <>
                  <Button disabled={feedbackLocked} onClick={() => void applyGeneratedDraft("replace")}>
                    替换当前 DSL
                  </Button>
                  <Button disabled={feedbackLocked} onClick={() => void applyGeneratedDraft("steps_only")}>
                    仅导入步骤
                  </Button>
                  <Button disabled={feedbackLocked} onClick={() => void applyGeneratedDraft("contracts_only")}>
                    仅合并契约
                  </Button>
                  <Button danger disabled={feedbackLocked} onClick={() => void rejectGeneratedDraft()}>
                    放弃草案
                  </Button>
                </>
              ) : null}
            </Space>
            {generateMutation.isError ? (
              <Alert
                type="error"
                showIcon
                message="不可导入错误"
                description={generateMutation.error.message}
              />
            ) : null}
            {generationFeedbackError ? (
              <Alert
                type="warning"
                showIcon
                message="反馈记录失败"
                description={generationFeedbackError}
              />
            ) : null}
            {recordedGenerationFeedback?.status === "accepted" ? (
              <Alert
                type="info"
                showIcon
                message="草案反馈已记录"
                description={`已采纳当前草案，导入方式：${formatImportModeLabel(recordedGenerationFeedback.importMode)}。`}
              />
            ) : null}
            {generatedDraft?.normalization_notes.length ? (
              <Alert
                type="success"
                showIcon
                message="自动修正项"
                description={
                  <Space direction="vertical" size="small">
                    {generatedDraft.normalization_notes.map((note) => (
                      <Typography.Text key={note}>{note}</Typography.Text>
                    ))}
                  </Space>
                }
              />
            ) : null}
            {generatedDraft?.warnings.length ? (
              <Alert
                type="warning"
                showIcon
                message="生成提示"
                description={
                  <Space direction="vertical" size="small">
                    {generatedDraft.warnings.map((warning) => (
                      <Typography.Text key={warning}>{warning}</Typography.Text>
                    ))}
                  </Space>
                }
              />
            ) : null}
            {generatedDraft ? (
              <Card size="small" title="生成预览">
                <Space direction="vertical" size="middle" style={{ width: "100%" }}>
                  <Space wrap>
                    <Tag color="blue">{generatedDraft.case.name}</Tag>
                    <Tag color="geekblue">{generatedDraft.generation_meta.generation_mode}</Tag>
                    <Tag color="purple">{generatedDraft.generation_meta.import_mode}</Tag>
                    <Tag color="gold">{generatedDraft.generation_meta.model ?? "未配置模型"}</Tag>
                    {generatedDraft.supported_actions.map((action) => (
                      <Tag key={action}>{action}</Tag>
                    ))}
                  </Space>
                  <Typography.Text type="secondary">
                    Base URL 来源：{generatedDraft.generation_meta.base_url_source}；修正 action {generatedDraft.generation_meta.repaired_invalid_actions} 个；
                    删除非法步骤 {generatedDraft.generation_meta.removed_invalid_steps} 个；删除非法契约 {generatedDraft.generation_meta.removed_invalid_contracts} 个。
                  </Typography.Text>
                  <Input.TextArea
                    aria-label="生成 DSL 预览"
                    readOnly
                    rows={12}
                    value={formatGeneratedCase(generatedDraft.case)}
                    spellCheck={false}
                    style={{ fontFamily: "Consolas, 'Courier New', monospace" }}
                  />
                </Space>
              </Card>
            ) : null}
          </Space>
        </Card>

        <Card title="上下文契约">
          <Space direction="vertical" size="large" style={{ width: "100%" }}>
            <Space direction="vertical" size="middle" style={{ width: "100%" }}>
              <Space align="center" style={{ justifyContent: "space-between", width: "100%" }} wrap>
                <Typography.Title level={5} style={{ margin: 0 }}>
                  输入契约
                </Typography.Title>
                <Button onClick={() => syncInputContracts([...inputContracts, createDefaultInputContract()])}>
                  新增输入契约
                </Button>
              </Space>
              {inputContracts.length ? (
                inputContracts.map((contract, index) => (
                  <Card key={`input-contract-${index}`} size="small" title={`输入 ${index + 1}`}>
                    <Space direction="vertical" size="middle" style={{ width: "100%" }}>
                      <div className="structured-step-grid">
                        <div>
                          <Typography.Text type="secondary">名称</Typography.Text>
                          <Input
                            style={{ marginTop: 8 }}
                            value={contract.name}
                            onChange={(event) =>
                              syncInputContracts(
                                inputContracts.map((item, itemIndex) =>
                                  itemIndex === index ? { ...item, name: event.target.value } : item,
                                ),
                              )
                            }
                          />
                        </div>
                        <div>
                          <Typography.Text type="secondary">上下文键</Typography.Text>
                          <Input
                            style={{ marginTop: 8 }}
                            value={contract.context_key}
                            onChange={(event) =>
                              syncInputContracts(
                                inputContracts.map((item, itemIndex) =>
                                  itemIndex === index ? { ...item, context_key: event.target.value } : item,
                                ),
                              )
                            }
                          />
                        </div>
                        <div>
                          <Typography.Text type="secondary">类型</Typography.Text>
                          <Select
                            style={{ width: "100%", marginTop: 8 }}
                            value={contract.value_type}
                            options={VARIABLE_TYPE_OPTIONS}
                            onChange={(value) =>
                              syncInputContracts(
                                inputContracts.map((item, itemIndex) =>
                                  itemIndex === index ? { ...item, value_type: value } : item,
                                ),
                              )
                            }
                          />
                        </div>
                        <div>
                          <Typography.Text type="secondary">必填</Typography.Text>
                          <Select
                            style={{ width: "100%", marginTop: 8 }}
                            value={contract.required}
                            options={[
                              { label: "是", value: true },
                              { label: "否", value: false },
                            ]}
                            onChange={(value) =>
                              syncInputContracts(
                                inputContracts.map((item, itemIndex) =>
                                  itemIndex === index ? { ...item, required: value } : item,
                                ),
                              )
                            }
                          />
                        </div>
                      </div>
                      <Input.TextArea
                        rows={2}
                        placeholder="可选：说明这个变量在 Suite 中的来源或用途"
                        value={contract.description ?? ""}
                        onChange={(event) =>
                          syncInputContracts(
                            inputContracts.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, description: event.target.value || null } : item,
                            ),
                          )
                        }
                      />
                      <Space wrap>
                        <Button
                          danger
                          onClick={() =>
                            syncInputContracts(inputContracts.filter((_, itemIndex) => itemIndex !== index))
                          }
                        >
                          删除输入契约
                        </Button>
                      </Space>
                    </Space>
                  </Card>
                ))
              ) : (
                <Alert type="info" showIcon message="当前没有输入契约，Case 将不会从 Suite Context 读取变量。" />
              )}
            </Space>

            <Space direction="vertical" size="middle" style={{ width: "100%" }}>
              <Space align="center" style={{ justifyContent: "space-between", width: "100%" }} wrap>
                <Typography.Title level={5} style={{ margin: 0 }}>
                  输出契约
                </Typography.Title>
                <Button onClick={() => syncOutputContracts([...outputContracts, createDefaultOutputContract()])}>
                  新增输出契约
                </Button>
              </Space>
              {outputContracts.length ? (
                outputContracts.map((contract, index) => (
                  <Card key={`output-contract-${index}`} size="small" title={`输出 ${index + 1}`}>
                    <Space direction="vertical" size="middle" style={{ width: "100%" }}>
                      <div className="structured-step-grid">
                        <div>
                          <Typography.Text type="secondary">名称</Typography.Text>
                          <Input
                            style={{ marginTop: 8 }}
                            value={contract.name}
                            onChange={(event) =>
                              syncOutputContracts(
                                outputContracts.map((item, itemIndex) =>
                                  itemIndex === index ? { ...item, name: event.target.value } : item,
                                ),
                              )
                            }
                          />
                        </div>
                        <div>
                          <Typography.Text type="secondary">上下文键</Typography.Text>
                          <Input
                            style={{ marginTop: 8 }}
                            value={contract.context_key}
                            onChange={(event) =>
                              syncOutputContracts(
                                outputContracts.map((item, itemIndex) =>
                                  itemIndex === index ? { ...item, context_key: event.target.value } : item,
                                ),
                              )
                            }
                          />
                        </div>
                        <div>
                          <Typography.Text type="secondary">类型</Typography.Text>
                          <Select
                            style={{ width: "100%", marginTop: 8 }}
                            value={contract.value_type}
                            options={VARIABLE_TYPE_OPTIONS}
                            onChange={(value) =>
                              syncOutputContracts(
                                outputContracts.map((item, itemIndex) =>
                                  itemIndex === index ? { ...item, value_type: value } : item,
                                ),
                              )
                            }
                          />
                        </div>
                        <div>
                          <Typography.Text type="secondary">提取来源</Typography.Text>
                          <Select
                            style={{ width: "100%", marginTop: 8 }}
                            value={contract.source ?? undefined}
                            placeholder="请选择"
                            options={OUTPUT_SOURCE_OPTIONS}
                            onChange={(value) =>
                              syncOutputContracts(
                                outputContracts.map((item, itemIndex) =>
                                  itemIndex === index ? { ...item, source: value } : item,
                                ),
                              )
                            }
                          />
                        </div>
                      </div>
                      <Input.TextArea
                        rows={2}
                        placeholder="可选：说明这个变量会回写到哪个上下文键"
                        value={contract.description ?? ""}
                        onChange={(event) =>
                          syncOutputContracts(
                            outputContracts.map((item, itemIndex) =>
                              itemIndex === index ? { ...item, description: event.target.value || null } : item,
                            ),
                          )
                        }
                      />
                      <Space wrap>
                        <Button
                          danger
                          onClick={() =>
                            syncOutputContracts(outputContracts.filter((_, itemIndex) => itemIndex !== index))
                          }
                        >
                          删除输出契约
                        </Button>
                      </Space>
                    </Space>
                  </Card>
                ))
              ) : (
                <Alert type="info" showIcon message="当前没有输出契约，Case 执行后不会向 Suite Context 回写变量。" />
              )}
            </Space>
          </Space>
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
                  form.setFieldValue("base_url", selectedTemplate.baseUrl);
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
                onChange={(event) => {
                  setStepsJson(event.target.value);
                  setValidationResult(null);
                }}
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
      </Space>
    </>
  );
}
