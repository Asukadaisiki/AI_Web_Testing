import { useEffect, useMemo, useState } from "react";
import { Alert, Button, Checkbox, Input, Progress, Select, Tag, Typography, message } from "antd";
import { SendOutlined } from "@ant-design/icons";

import {
  createPlanningSession,
  generatePlanningDrafts,
  getPlanningSession,
  listPlanningSessions,
  sendPlanningMessage,
  updatePlanningDraftStatus,
} from "../services/api";
import type {
  AIPlanningDraft,
  AIPlanningMessage,
  AIPlanningPlan,
  AIPlanningRequirements,
  AIPlanningSessionSummary,
  AIPlanningToolCall,
  AISettings,
  DSLCaseInputContract,
  DSLCaseOutputContract,
  DSLCasePayload,
  DSLStep,
} from "../types/api";
import { NotebookLMLayout } from "../layouts/NotebookLMLayout";

type AITestPlanningPanelProps = {
  aiSettings?: AISettings | null;
  projectId?: number;
  caseId?: number;
  currentCase?: DSLCasePayload | null;
  currentSteps?: DSLStep[] | null;
  currentInputContract?: DSLCaseInputContract[] | null;
  currentOutputContract?: DSLCaseOutputContract[] | null;
  onImportDraft: (draft: AIPlanningDraft) => void | Promise<void>;
  draftImportLabel?: string;
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
  draftImportLabel,
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
  const [sessionList, setSessionList] = useState<AIPlanningSessionSummary[]>([]);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);

  const planningEnabled = Boolean(aiSettings?.enable_ai_planning);
  const isDisabled = !planningEnabled || !projectId;

  async function loadSessionList() {
    if (!projectId) return;
    setIsLoadingHistory(true);
    try {
      const list = await listPlanningSessions(projectId);
      setSessionList(list);
    } catch {
      // silently fail — session list is non-critical
    } finally {
      setIsLoadingHistory(false);
    }
  }

  useEffect(() => {
    if (!projectId) {
      return;
    }

    let cancelled = false;

    async function init() {
      if (!projectId) return;
      setIsBootstrapping(true);
      try {
        // Try restore last session
        const lastId = localStorage.getItem("ai_planning_last_session");
        if (lastId) {
          try {
            const detail = await getPlanningSession(Number(lastId));
            if (!cancelled) {
              setSessionId(detail.session.id);
              setRequirements(detail.session.requirements);
              setMissingSlots(detail.session.missing_slots);
              setSuggestedQuestions([]);
              setPlan(detail.session.plan ?? null);
              setTranscript(detail.messages);
              setDrafts(detail.drafts);
            }
          } catch {
            // Session not found or expired — fall through to create new
          }
        }

        // If no session restored, create new
        if (!cancelled && !localStorage.getItem("ai_planning_last_session")) {
          const resp = await createPlanningSession({
            project_id: projectId,
            case_id: caseId ?? null,
          });
          if (!cancelled) {
            setSessionId(resp.session.id);
            setRequirements(resp.session.requirements);
            setMissingSlots(resp.session.missing_slots);
            setSuggestedQuestions([]);
            setPlan(resp.session.plan ?? null);
            setTranscript(resp.messages);
            setDrafts(resp.drafts);
            localStorage.setItem("ai_planning_last_session", String(resp.session.id));
          }
        }

        // Always load session list
        if (!cancelled) await loadSessionList();
      } catch (err: unknown) {
        if (!cancelled) {
          void messageApi.error(err instanceof Error ? err.message : String(err));
        }
      } finally {
        if (!cancelled) setIsBootstrapping(false);
      }
    }

    void init();
    return () => {
      cancelled = true;
    };
  }, [projectId, caseId]);

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
      localStorage.setItem("ai_planning_last_session", String(sessionId));
      await loadSessionList();
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

  function renderLeftPanel() {
    return (
      <div style={{ display: "flex", flexDirection: "column", gap: 12, flex: 1, overflow: "hidden" }}>
        <div style={{ fontWeight: 700, fontSize: 14 }}>Requirements</div>
        <Progress percent={progressPercent} size="small" />
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          已收集 {progressCount} / {REQUIREMENT_FIELDS.length} 项
        </Typography.Text>
        <div style={{ flex: 1, overflowY: "auto" }} className="panel-scroll">
          {collectedEntries.length ? (
            collectedEntries.map((entry) => (
              <div key={entry.label} className="step-item">
                <Typography.Text strong style={{ fontSize: 13 }}>
                  {entry.label}
                </Typography.Text>
                <div style={{ fontSize: 13, color: "#555", marginTop: 2 }}>{entry.value}</div>
              </div>
            ))
          ) : (
            <Typography.Text type="secondary" style={{ fontSize: 13 }}>
              当前还没有收集到明确的规划信息。
            </Typography.Text>
          )}
        </div>
        {missingSlots.length ? (
          <Alert
            type="info"
            showIcon
            message="待补充信息"
            description={missingSlots.join("、")}
            style={{ fontSize: 12 }}
          />
        ) : null}
      </div>
    );
  }

  function renderCenterPanel() {
    return (
      <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
        {contextHolder}
        {/* Top area with title and status */}
        <div style={{ padding: "20px 40px 0" }}>
          <Typography.Title level={4} style={{ margin: 0 }}>
            AI Planning
          </Typography.Title>
          {aiSettings && planningEnabled ? (
            <Alert
              type="info"
              showIcon
              style={{ marginTop: 12, fontSize: 13 }}
              message={`模型：${aiSettings.ai_planning_model ?? "未配置"}，最多 ${aiSettings.ai_planning_max_react_rounds ?? 5} 轮`}
            />
          ) : null}
          {!projectId ? (
            <Alert type="warning" showIcon message="请先选择项目，再开启 AI 测试规划。" style={{ marginTop: 12 }} />
          ) : null}
        </div>

        {/* Session switcher */}
        <div style={{ display: "flex", gap: 8, padding: "8px 40px 0", alignItems: "center" }}>
          <Select
            style={{ flex: 1 }}
            size="small"
            placeholder="选择会话"
            value={sessionId ?? undefined}
            loading={isLoadingHistory}
            onChange={async (id: number) => {
              setIsBootstrapping(true);
              try {
                const detail = await getPlanningSession(id);
                setSessionId(detail.session.id);
                setRequirements(detail.session.requirements);
                setMissingSlots(detail.session.missing_slots);
                setSuggestedQuestions([]);
                setPlan(detail.session.plan ?? null);
                setTranscript(detail.messages);
                setDrafts(detail.drafts);
                localStorage.setItem("ai_planning_last_session", String(id));
              } catch (err: unknown) {
                void messageApi.error("加载会话失败: " + (err instanceof Error ? err.message : String(err)));
              } finally {
                setIsBootstrapping(false);
              }
            }}
            options={sessionList.map((s) => ({
              value: s.id,
              label: s.title || `会话 #${s.id} (${new Date(s.created_at).toLocaleString()})`,
            }))}
          />
          <Button
            type="primary"
            size="small"
            onClick={async () => {
              if (!projectId) return;
              setIsBootstrapping(true);
              try {
                const resp = await createPlanningSession({ project_id: projectId });
                setSessionId(resp.session.id);
                setRequirements(resp.session.requirements);
                setMissingSlots(resp.session.missing_slots);
                setSuggestedQuestions([]);
                setPlan(resp.session.plan ?? null);
                setTranscript(resp.messages);
                setDrafts(resp.drafts);
                localStorage.setItem("ai_planning_last_session", String(resp.session.id));
                await loadSessionList();
              } catch (err: unknown) {
                void messageApi.error("创建会话失败: " + (err instanceof Error ? err.message : String(err)));
              } finally {
                setIsBootstrapping(false);
              }
            }}
          >
            新建会话
          </Button>
        </div>

        {/* Scrollable message area */}
        <div
          style={{
            flex: 1,
            overflowY: "auto",
            padding: "24px 40px",
          }}
          className="panel-scroll"
        >
          {transcript.map((item) => (
            <div
              key={`${item.id}-${item.turn_type}`}
              style={{
                display: "flex",
                justifyContent: item.role === "user" ? "flex-end" : "flex-start",
                marginBottom: 12,
              }}
            >
              <div className={item.role === "user" ? "chat-bubble-user" : "chat-bubble-ai"}>
                {item.role === "assistant" && item.turn_type === "tool_call" ? (
                  <>
                    <span style={{ fontWeight: 600 }}>🔧 工具调用</span>
                    <div style={{ marginTop: 4 }}>{item.content}</div>
                  </>
                ) : (
                  item.content
                )}
              </div>
            </div>
          ))}
        </div>

        {/* Suggested questions */}
        {suggestedQuestions.length ? (
          <div style={{ padding: "0 40px 8px", display: "flex", flexWrap: "wrap", gap: 8 }}>
            {suggestedQuestions.map((question) => (
              <Tag
                key={question}
                className="action-grid-item"
                style={{ cursor: "pointer" }}
                onClick={() => {
                  setInputValue(question);
                }}
              >
                {question}
              </Tag>
            ))}
          </div>
        ) : null}

        {/* Bottom input bar */}
        <div style={{ padding: "16px 32px 20px", borderTop: "1px solid #f5f5f5" }}>
          <div
            style={{
              display: "flex",
              alignItems: "flex-end",
              gap: 8,
              background: "#F0F4F8",
              borderRadius: 24,
              padding: "8px 8px 8px 16px",
            }}
          >
            <Input.TextArea
              aria-label="测试规划对话输入"
              autoSize={{ minRows: 1, maxRows: 4 }}
              bordered={false}
              value={inputValue}
              onChange={(event) => setInputValue(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey) {
                  event.preventDefault();
                  void handleSendMessage();
                }
              }}
              disabled={isDisabled || isBootstrapping}
              placeholder="描述业务目标、入口页面、核心流程、断言和测试数据…"
              style={{ background: "transparent", resize: "none", flex: 1 }}
            />
            <Button
              type="primary"
              shape="circle"
              icon={<SendOutlined />}
              onClick={() => void handleSendMessage()}
              loading={isSending}
              disabled={isDisabled || isBootstrapping || !sessionId || !inputValue.trim()}
              style={{
                background: "#1a1a2e",
                borderColor: "#1a1a2e",
                width: 40,
                height: 40,
                minWidth: 40,
                flexShrink: 0,
              }}
            />
          </div>
        </div>
      </div>
    );
  }

  function renderRightCards() {
    const cards: React.ReactNode[] = [];

    // Card 1: 规划进度
    cards.push(
      <div key="plan-progress">
        <Typography.Text strong style={{ fontSize: 14 }}>
          规划进度
        </Typography.Text>
        <div style={{ marginTop: 12, display: "flex", flexDirection: "column", gap: 8 }}>
          {plan ? (
            <>
              <Alert type="success" showIcon message={plan.summary} style={{ fontSize: 13 }} />
              {plan.scenarios.map((scenario) => (
                <div key={scenario.scenario_key} style={{ padding: "8px 0" }}>
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
                    {scenario.title}
                  </Checkbox>
                  <Typography.Text style={{ display: "block", fontSize: 12, color: "#555", marginTop: 2, paddingLeft: 24 }}>
                    {scenario.goal}
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ display: "block", fontSize: 12, paddingLeft: 24 }}>
                    数据需求：{scenario.test_data_requirements.length ? scenario.test_data_requirements.map((item) => item.label).join("、") : "无"}
                  </Typography.Text>
                  <Typography.Text type="secondary" style={{ display: "block", fontSize: 12, paddingLeft: 24 }}>
                    关键断言：{scenario.assertions.length ? scenario.assertions.join("、") : "无"}
                  </Typography.Text>
                </div>
              ))}
              <Button
                onClick={() => void handleGenerateDrafts()}
                loading={isGenerating}
                disabled={!selectedScenarioKeys.length}
                type="primary"
                block
              >
                生成选中草案
              </Button>
            </>
          ) : (
            <Typography.Text type="secondary" style={{ fontSize: 13 }}>
              尚未生成规划方案，请在对话中描述测试需求。
            </Typography.Text>
          )}
        </div>
      </div>,
    );

    // Card 2: DSL 草案列表
    if (drafts.length > 0) {
      cards.push(
        <div key="drafts-list">
          <Typography.Text strong style={{ fontSize: 14 }}>
            DSL 草案列表
          </Typography.Text>
          <div style={{ marginTop: 12, display: "flex", flexDirection: "column", gap: 8 }}>
            {drafts.map((draft) => (
              <div key={draft.id} style={{ padding: "8px 0", borderBottom: "1px solid #f5f5f5" }}>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                  <Typography.Text strong style={{ fontSize: 13 }}>
                    {draft.title}
                  </Typography.Text>
                  <Tag>{draft.status}</Tag>
                </div>
                {draft.error_message ? (
                  <Alert type="error" showIcon message={draft.error_message} style={{ marginTop: 4, fontSize: 12 }} />
                ) : null}
                {draft.dsl_case ? (
                  <Button
                    type="primary"
                    size="small"
                    onClick={() => void handleImportDraft(draft)}
                    disabled={draft.status !== "generated"}
                    style={{ marginTop: 6 }}
                  >
                    {draftImportLabel ?? "导入到当前编辑器"}
                  </Button>
                ) : null}
              </div>
            ))}
          </div>
        </div>,
      );
    }

    // Card 3 (optional): AI settings status
    if (aiSettings) {
      cards.push(
        <div key="ai-settings-info">
          <Typography.Text strong style={{ fontSize: 14 }}>
            AI 设置
          </Typography.Text>
          <div style={{ marginTop: 8, fontSize: 12, color: "#888" }}>
            <div>状态：{planningEnabled ? "已启用" : "未启用"}</div>
            {aiSettings.ai_planning_model ? <div>模型：{aiSettings.ai_planning_model}</div> : null}
            <div>最大轮数：{aiSettings.ai_planning_max_react_rounds ?? 5}</div>
          </div>
        </div>,
      );
    }

    return cards;
  }

  return (
    <NotebookLMLayout
      leftPanel={renderLeftPanel()}
      centerPanel={renderCenterPanel()}
      rightCards={renderRightCards()}
    />
  );
}
