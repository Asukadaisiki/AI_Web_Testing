import { useEffect, useMemo, useState } from "react";
import { Alert, Button, Card, Checkbox, Input, Progress, Space, Tag, Typography, message } from "antd";

import {
  createPlanningSession,
  generatePlanningDrafts,
  sendPlanningMessage,
  updatePlanningDraftStatus,
} from "../services/api";
import type {
  AIPlanningDraft,
  AIPlanningMessage,
  AIPlanningPlan,
  AIPlanningRequirements,
  AIPlanningToolCall,
  AISettings,
  DSLCaseInputContract,
  DSLCaseOutputContract,
  DSLCasePayload,
  DSLStep,
} from "../types/api";

type AITestPlanningPanelProps = {
  aiSettings?: AISettings | null;
  projectId?: number;
  caseId?: number;
  currentCase?: DSLCasePayload | null;
  currentSteps?: DSLStep[] | null;
  currentInputContract?: DSLCaseInputContract[] | null;
  currentOutputContract?: DSLCaseOutputContract[] | null;
  onImportDraft: (draft: AIPlanningDraft) => void | Promise<void>;
};

type RequirementFieldMeta = {
  key: keyof AIPlanningRequirements;
  label: string;
};

const REQUIREMENT_FIELDS: RequirementFieldMeta[] = [
  { key: "app_under_test", label: "被测系统" },
  { key: "business_goal", label: "业务目标" },
  { key: "entry_url_or_page", label: "入口页面或 URL" },
  { key: "core_user_flow", label: "核心流程" },
  { key: "main_assertions", label: "关键断言" },
  { key: "test_data_or_account", label: "测试数据或账号" },
  { key: "scope_limits", label: "范围限制" },
];

const DEFAULT_REQUIREMENTS: AIPlanningRequirements = {
  app_under_test: null,
  business_goal: null,
  entry_url_or_page: null,
  core_user_flow: null,
  main_assertions: [],
  test_data_or_account: null,
  scope_limits: null,
};

function formatRequirementValue(value: AIPlanningRequirements[keyof AIPlanningRequirements]) {
  if (Array.isArray(value)) {
    return value.length ? value.join("、") : null;
  }
  return value?.trim() ? value : null;
}

function createOptimisticMessage(
  sessionId: number,
  role: AIPlanningMessage["role"],
  turnType: AIPlanningMessage["turn_type"],
  content: string,
  structuredPayload?: Record<string, unknown> | null,
): AIPlanningMessage {
  return {
    id: -Date.now() - Math.floor(Math.random() * 1000),
    session_id: sessionId,
    role,
    turn_type: turnType,
    content,
    structured_payload: structuredPayload ?? null,
    created_at: new Date().toISOString(),
  };
}

function buildToolMessages(sessionId: number, toolCalls: AIPlanningToolCall[]) {
  return toolCalls.map((toolCall) =>
    createOptimisticMessage(
      sessionId,
      "assistant",
      "tool_call",
      `调用工具：${toolCall.tool}`,
      { type: "tool_call", ...toolCall },
    ),
  );
}

export function AITestPlanningPanel({
  aiSettings,
  projectId,
  caseId,
  currentCase,
  currentSteps,
  currentInputContract,
  currentOutputContract,
  onImportDraft,
}: AITestPlanningPanelProps) {
  const [messageApi, contextHolder] = message.useMessage();
  const [sessionId, setSessionId] = useState<number | null>(null);
  const [transcript, setTranscript] = useState<AIPlanningMessage[]>([]);
  const [requirements, setRequirements] = useState<AIPlanningRequirements>(DEFAULT_REQUIREMENTS);
  const [missingSlots, setMissingSlots] = useState<string[]>([]);
  const [suggestedQuestions, setSuggestedQuestions] = useState<string[]>([]);
  const [plan, setPlan] = useState<AIPlanningPlan | null>(null);
  const [drafts, setDrafts] = useState<AIPlanningDraft[]>([]);
  const [selectedScenarioKeys, setSelectedScenarioKeys] = useState<string[]>([]);
  const [inputValue, setInputValue] = useState("");
  const [isBootstrapping, setIsBootstrapping] = useState(false);
  const [isSending, setIsSending] = useState(false);
  const [isGenerating, setIsGenerating] = useState(false);

  const planningEnabled = Boolean(aiSettings?.enable_ai_planning);
  const isDisabled = !planningEnabled || !projectId;

  useEffect(() => {
    if (!projectId) {
      return;
    }

    let cancelled = false;
    setIsBootstrapping(true);
    void createPlanningSession({ project_id: projectId, case_id: caseId ?? null })
      .then((detail) => {
        if (cancelled) {
          return;
        }
        setSessionId(detail.session.id);
        setTranscript(detail.messages);
        setRequirements(detail.session.requirements);
        setMissingSlots(detail.session.missing_slots);
        setSuggestedQuestions([]);
        setPlan(detail.session.plan ?? null);
        setDrafts(detail.drafts);
      })
      .catch((error: Error) => {
        if (!cancelled) {
          void messageApi.error(error.message);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setIsBootstrapping(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [caseId, messageApi, projectId]);

  const collectedEntries = useMemo(
    () =>
      REQUIREMENT_FIELDS.flatMap((field) => {
        const value = formatRequirementValue(requirements[field.key]);
        return value ? [{ label: field.label, value }] : [];
      }),
    [requirements],
  );

  const progressCount = collectedEntries.length;
  const progressPercent = Math.round((progressCount / REQUIREMENT_FIELDS.length) * 100);

  async function handleSendMessage() {
    if (!sessionId) {
      return;
    }
    const trimmed = inputValue.trim();
    if (!trimmed) {
      return;
    }

    setIsSending(true);
    try {
      const response = await sendPlanningMessage(sessionId, { content: trimmed });
      setTranscript((current) => [
        ...current,
        createOptimisticMessage(sessionId, "user", "user", trimmed),
        ...buildToolMessages(sessionId, response.tool_calls ?? []),
        createOptimisticMessage(
          sessionId,
          "assistant",
          response.plan ? "plan" : response.session_status === "error" ? "system_error" : "followup",
          response.assistant_message,
        ),
      ]);
      setRequirements(response.requirements);
      setMissingSlots(response.missing_slots);
      setSuggestedQuestions(response.suggested_questions);
      setPlan(response.plan ?? null);
      setDrafts(response.drafts);
      setInputValue("");
    } catch (error) {
      void messageApi.error((error as Error).message);
    } finally {
      setIsSending(false);
    }
  }

  async function handleGenerateDrafts() {
    if (!sessionId || !selectedScenarioKeys.length) {
      return;
    }
    setIsGenerating(true);
    try {
      const response = await generatePlanningDrafts(sessionId, {
        scenario_keys: selectedScenarioKeys,
        current_case: currentCase ?? null,
        current_steps: currentSteps ?? null,
        current_input_contract: currentInputContract ?? null,
        current_output_contract: currentOutputContract ?? null,
        preserve_contracts: true,
      });
      setDrafts(response.drafts);
      setPlan(response.plan ?? null);
      setRequirements(response.requirements);
      setMissingSlots(response.missing_slots);
      setTranscript((current) => [
        ...current,
        createOptimisticMessage(sessionId, "assistant", "plan", response.assistant_message),
      ]);
    } catch (error) {
      void messageApi.error((error as Error).message);
    } finally {
      setIsGenerating(false);
    }
  }

  async function handleImportDraft(draft: AIPlanningDraft) {
    await onImportDraft(draft);
    const updatedDraft = await updatePlanningDraftStatus(draft.id, { status: "imported" });
    setDrafts((current) => current.map((item) => (item.id === draft.id ? updatedDraft : item)));
  }

  return (
    <Card title="AI 测试规划助手">
      {contextHolder}
      <Space direction="vertical" size="middle" style={{ width: "100%" }}>
        {aiSettings ? (
          <Alert
            type={planningEnabled ? "info" : "warning"}
            showIcon
            message="AI 规划状态"
            description={
              planningEnabled
                ? `已启用，模型：${aiSettings.ai_planning_model ?? "未配置"}，最多 ${aiSettings.ai_planning_max_react_rounds ?? 5} 轮`
                : "当前未启用 AI planning，规划对话暂不可用。"
            }
          />
        ) : null}
        {!projectId ? <Alert type="warning" showIcon message="请先选择项目，再开启 AI 测试规划。" /> : null}
        <div className="workbench-grid">
          <Card size="small" title="规划对话">
            <Space direction="vertical" size="middle" style={{ width: "100%" }}>
              {transcript.map((item) => (
                <Alert
                  key={`${item.id}-${item.turn_type}`}
                  type={item.role === "assistant" ? "info" : "success"}
                  showIcon
                  message={item.turn_type === "tool_call" ? "工具调用" : item.role === "assistant" ? "AI" : "用户"}
                  description={item.content}
                />
              ))}
              {suggestedQuestions.length ? (
                <Space wrap>
                  {suggestedQuestions.map((question) => (
                    <Tag key={question}>{question}</Tag>
                  ))}
                </Space>
              ) : null}
              <Input.TextArea
                aria-label="测试规划对话输入"
                rows={4}
                value={inputValue}
                onChange={(event) => setInputValue(event.target.value)}
                disabled={isDisabled || isBootstrapping}
                placeholder="描述业务目标、入口页面、核心流程、断言和测试数据。"
              />
              <Space wrap>
                <Button
                  type="primary"
                  onClick={() => void handleSendMessage()}
                  loading={isSending}
                  disabled={isDisabled || isBootstrapping || !sessionId || !inputValue.trim()}
                >
                  发送消息
                </Button>
              </Space>
            </Space>
          </Card>

          <Card size="small" title="规划进度">
            <Space direction="vertical" size="middle" style={{ width: "100%" }}>
              <div>
                <Typography.Text strong>信息收集进度</Typography.Text>
                <Progress percent={progressPercent} size="small" />
                <Typography.Text type="secondary">
                  已收集 {progressCount} / {REQUIREMENT_FIELDS.length} 项
                </Typography.Text>
              </div>

              {collectedEntries.length ? (
                collectedEntries.map((entry) => (
                  <div key={entry.label}>
                    <Typography.Text strong>{entry.label}：</Typography.Text>
                    <Typography.Text>{entry.value}</Typography.Text>
                  </div>
                ))
              ) : (
                <Typography.Text type="secondary">当前还没有收集到明确的规划信息。</Typography.Text>
              )}

              {missingSlots.length ? (
                <Alert
                  type="info"
                  showIcon
                  message="待补充信息"
                  description={missingSlots.join("、")}
                />
              ) : null}

              {plan ? (
                <>
                  <Alert type="success" showIcon message={plan.summary} />
                  {plan.scenarios.map((scenario) => (
                    <Card key={scenario.scenario_key} size="small" title={scenario.title}>
                      <Space direction="vertical" size="small" style={{ width: "100%" }}>
                        <Checkbox
                          aria-label={`选择场景 ${scenario.title}`}
                          checked={selectedScenarioKeys.includes(scenario.scenario_key)}
                          onChange={(event) =>
                            setSelectedScenarioKeys((current) =>
                              event.target.checked
                                ? [...current, scenario.scenario_key]
                                : current.filter((item) => item !== scenario.scenario_key),
                            )
                          }
                        >
                          选择场景 {scenario.title}
                        </Checkbox>
                        <Typography.Text>{scenario.goal}</Typography.Text>
                        <Typography.Text type="secondary">
                          数据需求：
                          {scenario.test_data_requirements.length
                            ? scenario.test_data_requirements.map((item) => item.label).join("、")
                            : "无"}
                        </Typography.Text>
                        <Typography.Text type="secondary">
                          关键断言：{scenario.assertions.length ? scenario.assertions.join("、") : "无"}
                        </Typography.Text>
                      </Space>
                    </Card>
                  ))}
                  <Button onClick={() => void handleGenerateDrafts()} loading={isGenerating} disabled={!selectedScenarioKeys.length}>
                    生成选中草案
                  </Button>
                </>
              ) : null}

              {drafts.length ? (
                <Card size="small" title="DSL 草案列表">
                  <Space direction="vertical" size="middle" style={{ width: "100%" }}>
                    {drafts.map((draft) => (
                      <Card key={draft.id} size="small" title={draft.title}>
                        <Space direction="vertical" size="small" style={{ width: "100%" }}>
                          <Tag>{draft.status}</Tag>
                          {draft.error_message ? <Alert type="error" showIcon message={draft.error_message} /> : null}
                          {draft.dsl_case ? (
                            <>
                              <Input.TextArea readOnly rows={6} value={JSON.stringify(draft.dsl_case, null, 2)} />
                              <Button
                                type="primary"
                                onClick={() => void handleImportDraft(draft)}
                                disabled={draft.status !== "generated"}
                              >
                                导入到当前编辑器
                              </Button>
                            </>
                          ) : null}
                        </Space>
                      </Card>
                    ))}
                  </Space>
                </Card>
              ) : null}
            </Space>
          </Card>
        </div>
      </Space>
    </Card>
  );
}
