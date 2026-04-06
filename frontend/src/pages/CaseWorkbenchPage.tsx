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
import {
  SendOutlined,
  SearchOutlined,
  PlusOutlined,
  CheckCircleOutlined,
  RobotOutlined,
  EditOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import { useNavigate, useParams } from "react-router-dom";

import { ErrorBlock, LoadingBlock } from "../components/PageFeedback";
import { AITestPlanningPanel } from "../components/AITestPlanningPanel";
import { NotebookLMLayout } from "../layouts/NotebookLMLayout";
import { StepList } from "../components/StepList";
import { ChatInput } from "../components/ChatInput";
import {
  createCase,
  executeCase,
  generateDslCase,
  getAISettings,
  getCaseDetail,
  getProjects,
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
  DslGenerationRejectionReasonCode,
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

const REJECTION_REASON_OPTIONS: { label: string; value: DslGenerationRejectionReasonCode }[] = [
  { label: "wrong_actions", value: "wrong_actions" },
  { label: "invalid_structure", value: "invalid_structure" },
  { label: "context_mismatch", value: "context_mismatch" },
  { label: "bad_contracts", value: "bad_contracts" },
  { label: "other", value: "other" },
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

function formatRiskFlags(flags: string[]) {
  return flags.length ? flags.join("、") : "无";
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
  const [rejectionReasonCode, setRejectionReasonCode] = useState<DslGenerationRejectionReasonCode | null>(null);
  const [feedbackNote, setFeedbackNote] = useState("");
  const [retryFromGenerationId, setRetryFromGenerationId] = useState<number | null>(null);
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
  const projectsQuery = useQuery({
    queryKey: ["projects"],
    queryFn: getProjects,
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
        void navigate(`/run/${executionId}`);
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
        project_id: Number(values.project_id ?? 0) || null,
        case_id: caseId ? Number(caseId) : null,
        generation_mode: generationMode,
        import_mode: generationImportMode,
        current_case: shouldIncludeCurrentCase ? currentCase : null,
        current_steps: shouldIncludeCurrentSteps && currentCase ? currentCase.steps : null,
        current_input_contract: preserveContracts ? inputContracts : null,
        current_output_contract: preserveContracts ? outputContracts : null,
        retry_from_generation_id: retryFromGenerationId,
        retry_reason_code: retryFromGenerationId ? rejectionReasonCode : null,
        retry_note: retryFromGenerationId ? feedbackNote.trim() || null : null,
        preserve_contracts: preserveContracts,
      });
    },
    onSuccess: (result) => {
      setGeneratedDraft(result);
      setGenerationFeedbackError(null);
      setRecordedGenerationFeedback(null);
      setRetryFromGenerationId(null);
      setRejectionReasonCode(null);
      setFeedbackNote("");
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
        rejection_reason_code: status === "rejected" ? rejectionReasonCode : null,
        feedback_note: status === "rejected" ? feedbackNote.trim() || null : null,
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
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["ai-settings-overview"] }),
        queryClient.invalidateQueries({ queryKey: ["dsl-generation-runs"] }),
      ]);
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
    if (!rejectionReasonCode) {
      setGenerationFeedbackError("请先选择放弃原因。");
      void messageApi.error("请先选择放弃原因。");
      return;
    }
    const generationId = generatedDraft.generation_id;
    setGenerationFeedbackError(null);
    try {
      await feedbackMutation.mutateAsync({
        generationId,
        status: "rejected",
      });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["ai-settings-overview"] }),
        queryClient.invalidateQueries({ queryKey: ["dsl-generation-runs"] }),
      ]);
      setRecordedGenerationFeedback({ status: "rejected" });
      setRetryFromGenerationId(generationId);
      setGeneratedDraft(null);
      void messageApi.success("已记录草案放弃反馈。");
    } catch (error) {
      setGenerationFeedbackError(`反馈未记录，可重试：${(error as Error).message}`);
      void messageApi.error(`反馈未记录：${(error as Error).message}`);
    }
  };

  const importPlanningDraft = async (draft: {
    dsl_case?: DSLCasePayload | null;
    dsl_generation_id?: number | null;
  }) => {
    if (!draft.dsl_case) {
      throw new Error("当前草案没有可导入的 DSL 内容。");
    }

    form.setFieldsValue({
      name: draft.dsl_case.name,
      description: draft.dsl_case.description ?? "",
      project_id: form.getFieldValue("project_id") ?? 1,
      base_url: draft.dsl_case.base_url ?? "",
    });
    syncInputContracts(draft.dsl_case.input_contract);
    syncOutputContracts(draft.dsl_case.output_contract);
    syncStructuredSteps(draft.dsl_case.steps);
    setStepsJson(formatStepsJson(draft.dsl_case.steps));
    setEditorMode("structured");
    setValidationResult(null);

    if (draft.dsl_generation_id) {
      await recordDslGenerationFeedback(draft.dsl_generation_id, {
        actor_user_id: 1,
        feedback_status: "accepted",
        feedback_import_mode: "replace",
        rejection_reason_code: null,
        feedback_note: "Imported from AI planning draft",
      });
    }
  };

  const feedbackLocked = feedbackMutation.isPending || recordedGenerationFeedback?.status === "accepted";
  const [activeStepIndex, setActiveStepIndex] = useState(0);
  const [showContracts, setShowContracts] = useState(false);
  const [stepSearch, setStepSearch] = useState("");

  const activeStep = structuredSteps[activeStepIndex];
  const activeAction = (activeStep?.action ?? "goto") as StepAction;

  if (caseQuery.isLoading) {
    return <LoadingBlock />;
  }

  if (caseQuery.isError) {
    return <ErrorBlock message={caseQuery.error.message} />;
  }

  return (
    <NotebookLMLayout
      leftPanel={
        <StepList
          steps={structuredSteps}
          activeIndex={activeStepIndex}
          onSelect={setActiveStepIndex}
          onAdd={() => { syncStructuredSteps([...structuredSteps, createDefaultStep()]); setEditorMode("structured"); }}
          searchValue={stepSearch}
          onSearchChange={setStepSearch}
        />
      }
      centerPanel={
        <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
          {contextHolder}
          {/* Top bar */}
          <div style={{ padding: "12px 24px", borderBottom: "1px solid #f0f0f0", display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <div>
              <Typography.Text strong style={{ fontSize: 16 }}>{isEditMode ? "用例工作台" : "新建用例"}</Typography.Text>
              <Typography.Text type="secondary" style={{ fontSize: 12, marginLeft: 8 }}>{watchedBaseUrl || "未配置 Base URL"}</Typography.Text>
            </div>
            <Space size="small">
              <Button size="small" onClick={() => navigate("/cases")}>返回</Button>
              <Button size="small" loading={validateMutation.isPending} onClick={() => validateMutation.mutate()}>校验</Button>
              <Button size="small" type="primary" loading={saveMutation.isPending} onClick={() => saveMutation.mutate({ executeAfterSave: false })}>保存</Button>
              <Button size="small" type="primary" ghost loading={saveMutation.isPending} onClick={() => saveMutation.mutate({ executeAfterSave: true })}>保存并执行</Button>
            </Space>
          </div>
          {/* Alerts */}
          <div style={{ padding: "8px 24px 0" }}>
            {pendingDraft ? (
              <Alert type="warning" showIcon message="检测到本地草稿" style={{ marginBottom: 8 }} description={
                <Space wrap>
                  <Typography.Text>存在未保存的本地草稿。</Typography.Text>
                  <Button size="small" type="primary" onClick={() => { applyDraft(pendingDraft); setPendingDraft(null); }}>恢复</Button>
                  <Button size="small" onClick={() => { removeDraft(draftKey); setPendingDraft(null); }}>丢弃</Button>
                </Space>
              } />
            ) : null}
            {shouldWarnMissingBaseUrl ? (
              <Alert type="warning" showIcon message="缺少 Base URL" description="包含相对路径 goto，请先填写。" style={{ marginBottom: 8 }} />
            ) : null}
          </div>
          {/* Compact form */}
          <div style={{ padding: "8px 24px", borderBottom: "1px solid #f0f0f0" }}>
            <Form form={form} layout="inline" initialValues={DEFAULT_FORM_VALUES}>
              <Form.Item name="name" rules={[{ required: true, message: "名称" }]} style={{ marginBottom: 0 }}>
                <Input placeholder="用例名称" style={{ width: 180 }} />
              </Form.Item>
              <Form.Item name="project_id" rules={[{ required: true, message: "项目" }]} style={{ marginBottom: 0 }}>
                <Select loading={projectsQuery.isLoading} style={{ width: 160 }} placeholder="项目"
                  options={(projectsQuery.data ?? []).map((p) => ({ label: `${p.name} (#${p.id})`, value: p.id }))} />
              </Form.Item>
              <Form.Item name="base_url" style={{ marginBottom: 0 }}>
                <Input placeholder="Base URL" style={{ width: 200 }} />
              </Form.Item>
            </Form>
          </div>
          {/* Scrollable step editor area */}
          <div style={{ flex: 1, overflowY: "auto", padding: "16px 24px" }} className="panel-scroll">
            {/* Template selector + mode toggle */}
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12 }}>
              <Space>
                <Select value={templateValue} options={templateOptions} style={{ width: 200 }} onChange={setTemplateValue} />
                <Button onClick={() => {
                  const t = STEP_TEMPLATES.find((item) => item.value === templateValue);
                  if (t) { form.setFieldValue("base_url", t.baseUrl); syncStructuredSteps(t.steps); setEditorMode("structured"); }
                }}>应用模板</Button>
              </Space>
              <Space.Compact>
                <Button type={editorMode === "structured" ? "primary" : "default"} onClick={() => changeEditorMode("structured")}>结构化</Button>
                <Button type={editorMode === "json" ? "primary" : "default"} onClick={() => changeEditorMode("json")}>JSON</Button>
              </Space.Compact>
            </div>

            {/* Active step editor (structured mode) */}
            {editorMode === "structured" && activeStep ? (
              <div style={{ background: "#f5f5f5", borderRadius: 12, padding: 16, marginBottom: 12 }}>
                <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))", gap: 12 }}>
                  <div>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>动作</Typography.Text>
                    <Select value={activeAction} options={ACTION_OPTIONS} style={{ width: "100%", marginTop: 4 }}
                      onChange={(next) => updateStructuredStep(activeStepIndex, (s) => normalizeStepForAction(s, next))} />
                  </div>
                  {actionNeedsTarget(activeAction) ? (
                    <div>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>目标</Typography.Text>
                      <Input style={{ marginTop: 4 }} value={typeof activeStep.target === "string" ? String(activeStep.target) : ""}
                        onChange={(e) => updateStructuredStep(activeStepIndex, (s) => ({ ...s, target: e.target.value }))} />
                    </div>
                  ) : null}
                  {actionNeedsValue(activeAction) ? (
                    <div>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>{activeAction === "goto" || activeAction === "assert_url_contains" ? "值 / URL" : "值"}</Typography.Text>
                      <Input style={{ marginTop: 4 }} value={typeof activeStep.value === "string" ? String(activeStep.value) : ""}
                        onChange={(e) => updateStructuredStep(activeStepIndex, (s) => ({ ...s, value: e.target.value }))} />
                    </div>
                  ) : null}
                  {actionNeedsTimeout(activeAction) ? (
                    <div>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>超时 (ms)</Typography.Text>
                      <InputNumber min={1} max={60000} style={{ width: "100%", marginTop: 4 }}
                        value={typeof activeStep.timeout_ms === "number" ? activeStep.timeout_ms : 5000}
                        onChange={(v) => updateStructuredStep(activeStepIndex, (s) => ({ ...s, timeout_ms: typeof v === "number" ? v : 5000 }))} />
                    </div>
                  ) : null}
                </div>
                <Space style={{ marginTop: 8 }}>
                  <Button size="small" onClick={() => moveStep(activeStepIndex, -1)} disabled={activeStepIndex === 0}>上移</Button>
                  <Button size="small" onClick={() => moveStep(activeStepIndex, 1)} disabled={activeStepIndex === structuredSteps.length - 1}>下移</Button>
                  <Button size="small" danger onClick={() => {
                    if (structuredSteps.length === 1) { syncStructuredSteps([createDefaultStep()]); return; }
                    syncStructuredSteps(structuredSteps.filter((_, i) => i !== activeStepIndex));
                    setActiveStepIndex(Math.max(0, activeStepIndex - 1));
                  }}>删除</Button>
                </Space>
              </div>
            ) : editorMode === "json" ? (
              <Input.TextArea value={stepsJson} rows={18}
                onChange={(e) => { setStepsJson(e.target.value); setValidationResult(null); }}
                spellCheck={false} style={{ fontFamily: "Consolas, 'Courier New', monospace", fontSize: 13 }} />
            ) : null}

            {validationResult ? (
              <Alert type="success" showIcon message="DSL 校验通过" style={{ marginTop: 8 }}
                description={<Space wrap><Typography.Text>支持动作：</Typography.Text>{validationResult.supported_actions.map((a) => <Tag key={a}>{a}</Tag>)}</Space>} />
            ) : null}

            {/* AI generation feedback alerts */}
            {aiSettingsQuery.data ? (
              <Alert type={aiSettingsQuery.data.enable_ai_dsl_generate ? "info" : "warning"} showIcon style={{ marginTop: 8 }}
                message="当前生成配置" description={formatDslGenerationStatus(aiSettingsQuery.data)} />
            ) : null}
            {generateMutation.isError ? (
              <Alert type="error" showIcon message="生成失败" style={{ marginTop: 8 }} description={generateMutation.error.message} />
            ) : null}
            {generationFeedbackError ? (
              <Alert type="warning" showIcon message="反馈记录失败" style={{ marginTop: 8 }} description={generationFeedbackError} />
            ) : null}
            {recordedGenerationFeedback?.status === "accepted" ? (
              <Alert type="info" showIcon message="草案反馈已记录" style={{ marginTop: 8 }}
                description={"已采纳，导入方式：" + formatImportModeLabel(recordedGenerationFeedback.importMode)} />
            ) : null}
            {generatedDraft?.normalization_notes.length ? (
              <Alert type="success" showIcon message="自动修正项" style={{ marginTop: 8 }}
                description={generatedDraft.normalization_notes.join("；")} />
            ) : null}
            {generatedDraft?.warnings.length ? (
              <Alert type="warning" showIcon message="生成提示" style={{ marginTop: 8 }}
                description={generatedDraft.warnings.join("；")} />
            ) : null}
          </div>
          {/* ChatInput at bottom */}
          <ChatInput
            value={generationPrompt}
            onChange={setGenerationPrompt}
            onSend={() => generateMutation.mutate()}
            placeholder="描述测试需求，AI 帮你生成 DSL..."
            loading={generateMutation.isPending}
          />
        </div>
      }

      rightCards={[
        /* Card 1: Step info */
        <div key="step-info">
          <Typography.Text strong style={{ fontSize: 13, display: "block", marginBottom: 8 }}>当前步骤</Typography.Text>
          {activeStep ? (
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 6, fontSize: 12 }}>
              <div><span style={{ color: "#999" }}>动作</span> <strong>{activeStep.action}</strong></div>
              {activeStep.target ? <div><span style={{ color: "#999" }}>目标</span> {String(activeStep.target)}</div> : null}
              {activeStep.value ? <div><span style={{ color: "#999" }}>值</span> {String(activeStep.value)}</div> : null}
              {typeof activeStep.timeout_ms === "number" ? <div><span style={{ color: "#999" }}>超时</span> {activeStep.timeout_ms}ms</div> : null}
            </div>
          ) : <Typography.Text type="secondary" style={{ fontSize: 12 }}>选择步骤查看</Typography.Text>}
        </div>,
        /* Card 2: Actions */
        <div key="actions">
          <Typography.Text strong style={{ fontSize: 13, display: "block", marginBottom: 8 }}>操作</Typography.Text>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
            <div className="action-grid-item" onClick={() => generateMutation.mutate()}><RobotOutlined style={{ fontSize: 16 }} /><br /><span style={{ fontSize: 11 }}>AI 生成</span></div>
            <div className="action-grid-item" onClick={() => validateMutation.mutate()}><CheckCircleOutlined style={{ fontSize: 16 }} /><br /><span style={{ fontSize: 11 }}>校验 DSL</span></div>
            <div className="action-grid-item" onClick={() => setShowContracts(!showContracts)}><EditOutlined style={{ fontSize: 16 }} /><br /><span style={{ fontSize: 11 }}>契约编辑</span></div>
            <div className="action-grid-item" onClick={() => navigate("/cases")}><ThunderboltOutlined style={{ fontSize: 16 }} /><br /><span style={{ fontSize: 11 }}>用例列表</span></div>
          </div>
        </div>,
        /* Card 3: Generation settings */
        <div key="gen-settings">
          <Typography.Text strong style={{ fontSize: 13, display: "block", marginBottom: 8 }}>生成设置</Typography.Text>
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
            <Select value={generationMode} options={GENERATION_MODE_OPTIONS} size="small" onChange={(v) => { setGenerationModeDirty(true); setGenerationMode(v); }} />
            <Select value={generationContextSource} options={GENERATION_CONTEXT_OPTIONS} size="small" onChange={(v) => setGenerationContextSource(v)} />
            <Select value={generationImportMode} options={GENERATION_IMPORT_MODE_OPTIONS} size="small" onChange={(v) => setGenerationImportMode(v)} />
          </div>
        </div>,
        /* Card 4: Draft (conditional) */
        ...(generatedDraft ? [
          <div key="draft-preview">
            <Typography.Text strong style={{ fontSize: 13, display: "block", marginBottom: 8 }}>生成预览</Typography.Text>
            <Space wrap style={{ marginBottom: 8 }}>
              <Tag color="blue">{generatedDraft.case.name}</Tag>
              <Tag color="purple">{generatedDraft.generation_meta.import_mode}</Tag>
            </Space>
            <Space wrap>
              <Button size="small" disabled={feedbackLocked} onClick={() => void applyGeneratedDraft("replace")}>替换</Button>
              <Button size="small" disabled={feedbackLocked} onClick={() => void applyGeneratedDraft("steps_only")}>仅步骤</Button>
              <Button size="small" danger disabled={feedbackLocked} onClick={() => void rejectGeneratedDraft()}>放弃</Button>
            </Space>
          </div>
        ] : []),
      ]}
    />
  );
}
