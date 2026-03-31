import { useEffect, useMemo, useState } from "react";
import { Alert, Button, Card, Checkbox, Input, Space, Tag, Typography, message } from "antd";

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

const DEFAULT_REQUIREMENTS: AIPlanningRequirements = {
  app_under_test: null,
  business_goal: null,
  entry_url_or_page: null,
  core_user_flow: null,
  main_assertions: [],
  test_data_or_account: null,
  scope_limits: null,
};

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

  const isDisabled = !aiSettings?.enable_ai_dsl_generate || !projectId;

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

  const collectedInfo = useMemo(
    () => [
      ["被测系统", requirements.app_under_test],
      ["业务目标", requirements.business_goal],
      ["入口页面", requirements.entry_url_or_page],
      ["核心流程", requirements.core_user_flow],
      ["测试数据", requirements.test_data_or_account],
      ["范围限制", requirements.scope_limits],
    ],
    [requirements],
  );

  async function handleSendMessage() {
    if (!sessionId || !inputValue.trim()) {
      return;
    }
    const content = inputValue.trim();
    setIsSending(true);
    try {
      const response = await sendPlanningMessage(sessionId, { content });
      // Use negative timestamps for optimistic updates to avoid conflicts with server IDs
      const tempId = -Date.now();
      setTranscript((current) => [
        ...current,
        {
          id: tempId,
          session_id: sessionId,
          role: "user",
          turn_type: "user",
          content,
          structured_payload: null,
          created_at: new Date().toISOString(),
        },
        {
          id: tempId + 1,
          session_id: sessionId,
          role: "assistant",
          turn_type: response.plan ? "plan" : "followup",
          content: response.assistant_message,
          structured_payload: null,
          created_at: new Date().toISOString(),
        },
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
      // Use negative timestamp for optimistic update to avoid conflicts with server IDs
      const tempId = -Date.now();
      setTranscript((current) => [
        ...current,
        {
          id: tempId,
          session_id: sessionId,
          role: "assistant",
          turn_type: "plan",
          content: response.assistant_message,
          structured_payload: null,
          created_at: new Date().toISOString(),
        },
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
    setDrafts((current) =>
      current.map((item) => (item.id === draft.id ? updatedDraft : item)),
    );
  }

  return (
    <Card title="AI 测试助手">
      {contextHolder}
      <Space direction="vertical" size="middle" style={{ width: "100%" }}>
        {aiSettings ? (
          <Alert
            type={aiSettings.enable_ai_dsl_generate ? "info" : "warning"}
            showIcon
            message="AI 规划状态"
            description={
              aiSettings.enable_ai_dsl_generate
                ? `已启用，模型：${aiSettings.ai_dsl_model ?? "未配置"}`
                : "当前未启用 AI DSL 生成，规划对话不可用。"
            }
          />
        ) : null}
        {!projectId ? <Alert type="warning" showIcon message="请先选择项目，再开启 AI 测试规划。" /> : null}
        <div className="workbench-grid">
          <Card size="small" title="对话">
            <Space direction="vertical" size="middle" style={{ width: "100%" }}>
              {transcript.map((item, index) => (
                <Alert
                  key={`${item.role}-${index}-${item.content}`}
                  type={item.role === "assistant" ? "info" : "success"}
                  showIcon
                  message={item.role === "assistant" ? "AI" : "用户"}
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
                placeholder="描述业务目标、入口页面、流程、断言和测试数据"
              />
              <Button
                type="primary"
                onClick={() => void handleSendMessage()}
                loading={isSending}
                disabled={isDisabled || isBootstrapping || !sessionId || !inputValue.trim()}
              >
                发送消息
              </Button>
            </Space>
          </Card>
          <Card size="small" title="规划结果">
            <Space direction="vertical" size="middle" style={{ width: "100%" }}>
              {collectedInfo.map(([label, value]) => (
                <div key={label}>
                  <Typography.Text strong>{label}：</Typography.Text>
                  <Typography.Text>{value || "待补充"}</Typography.Text>
                </div>
              ))}
              <div>
                <Typography.Text strong>缺失槽位：</Typography.Text>
                <Typography.Text>{missingSlots.length ? missingSlots.join("、") : "无"}</Typography.Text>
              </div>
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
                          数据需求：{scenario.test_data_requirements.map((item) => item.label).join("、") || "无"}
                        </Typography.Text>
                        <Typography.Text type="secondary">
                          断言：{scenario.assertions.join("、") || "无"}
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
                              <Button type="primary" onClick={() => void handleImportDraft(draft)} disabled={draft.status !== "generated"}>
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
