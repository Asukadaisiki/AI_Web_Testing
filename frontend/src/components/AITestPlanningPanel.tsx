import { useState } from "react";
import { Alert, Button, Checkbox, Input, Select, Tag, Typography, message } from "antd";
import { DeleteOutlined, SendOutlined, CheckCircleFilled, LoadingOutlined, ClockCircleOutlined } from "@ant-design/icons";
import { useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";

import {
  cancelExecution,
  deletePlanningDraft,
  getPlanningSession,
  saveAndExecuteDrafts,
} from "../features/planning/api";
import { usePlanningSessionState } from "../features/planning/usePlanningSessionState";
import { usePlanningSse } from "../features/planning/usePlanningSse";
import {
  createOptimisticMessage,
  readContentBlocks,
} from "../features/planning/planningStreamEvents";
import { PlanningRequirementsPanel } from "../features/planning/PlanningRequirementsPanel";
import type {
  AIPlanningMessage,
  AISettings,
  DSLCaseInputContract,
  DSLCaseOutputContract,
  DSLCasePayload,
  DSLStep,
  ExecutionStreamEvent,
  ExecutionAnalysis,
  ExecutionSummaryResult,
} from "../types/api";
import { NotebookLMLayout } from "../layouts/NotebookLMLayout";
import { ExecutionAnalysisPanel } from "./ExecutionAnalysisPanel";
import { SessionProjectPanel } from "./SessionProjectPanel";

type AITestPlanningPanelProps = {
  aiSettings?: AISettings | null;
  sessionId: number;
  currentCase?: DSLCasePayload | null;
  currentSteps?: DSLStep[] | null;
  currentInputContract?: DSLCaseInputContract[] | null;
  currentOutputContract?: DSLCaseOutputContract[] | null;
};

function AssistantMessageBody({
  message,
  streaming,
}: {
  message: AIPlanningMessage;
  streaming: boolean;
}) {
  const blocks = readContentBlocks(message.structured_payload);
  if (blocks.length === 0) {
    return <>{message.content}</>;
  }
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      {blocks.map((block, idx) => {
        if (block.type === "thinking") {
          const collapsed = block.content.length > 500;
          return (
            <details
              key={idx}
              style={{ fontSize: 12, color: "#666", background: "#fafafa",
                       borderRadius: 6, padding: "4px 8px" }}
              open={streaming}
            >
              <summary style={{ cursor: "pointer", fontWeight: 500 }}>
                {collapsed ? `思考过程（${block.content.length} 字，已折叠）` : "思考过程"}
              </summary>
              <div style={{
                whiteSpace: "pre-wrap",
                marginTop: 4,
                maxHeight: 200,
                overflowY: "auto",
                opacity: streaming ? 1 : 0.7,
              }}>
                {collapsed ? block.content.slice(0, 500) + "..." : block.content}
              </div>
            </details>
          );
        }
        return (
          <div key={idx} style={{ whiteSpace: "pre-wrap" }}>
            {block.content}
            {streaming && idx === blocks.length - 1 ? (
              <span className="typing-cursor">▊</span>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}

const phaseColorMap: Record<string, string> = {
  thinking: "processing",
  generating: "warning",
  tool_calling: "warning",
  draft_generating: "warning",
  executing: "success",
};

export function AITestPlanningPanel({
  aiSettings,
  sessionId: sessionIdProp,
  currentCase,
  currentSteps,
  currentInputContract,
  currentOutputContract,
}: AITestPlanningPanelProps) {
  const [messageApi, contextHolder] = message.useMessage();
  const queryClient = useQueryClient();
  const {
    activeStreamKind,
    createAndSelectSession,
    deleteAndSelectSession,
    drafts,
    isBootstrapping,
    isLoadingHistory,
    loadSessionDetail,
    loadSessionList,
    missingSlots,
    plan,
    requirements,
    sessionId,
    sessionList,
    setDrafts,
    setIsBootstrapping,
    setTranscript,
    suggestedQuestions,
    transcript,
  } = usePlanningSessionState({
    initialSessionId: sessionIdProp,
    onError: (errorMessage) => {
      void messageApi.error(errorMessage);
    },
  });
  const [selectedScenarioKeys, setSelectedScenarioKeys] = useState<string[]>([]);
  const [inputValue, setInputValue] = useState("");
  const [isSaving, setIsSaving] = useState(false);
  const { abort: abortStream, run: runStream } = usePlanningSse();

  const planningEnabled = Boolean(aiSettings?.enable_ai_planning);
  const isDisabled = !planningEnabled;
  const isSending = activeStreamKind === "chat";
  const isGenerating = activeStreamKind === "drafts";
  const isExecuting = activeStreamKind === "execute";

  function clearStreamingOnMessage(targetId: number | null) {
    if (targetId == null) return;
    setTranscript((current) =>
      current.map((msg) => {
        if (msg.id !== targetId) return msg;
        const payload = msg.structured_payload as Record<string, unknown> | null;
        if (!payload?._streaming) return msg;
        return { ...msg, structured_payload: { ...payload, _streaming: false } };
      }),
    );
  }

  function clearAllStreaming() {
    setTranscript((current) =>
      current.map((msg) => {
        const payload = msg.structured_payload as Record<string, unknown> | null;
        if (!payload?._streaming) return msg;
        return { ...msg, structured_payload: { ...payload, _streaming: false } };
      }),
    );
  }

  function handleStreamSideEffect(event: ExecutionStreamEvent) {
    if (event.type === "error") {
      void messageApi.error("执行错误: " + event.message);
      return;
    }

    if (event.type === "done" || event.type === "cancelled") {
      void queryClient.invalidateQueries({ queryKey: ["cases"] });
      void queryClient.invalidateQueries({ queryKey: ["executions"] });
      if (event.type === "cancelled") {
        void messageApi.info("执行已取消");
      }
    }
  }

  async function finalizeStream(sessionIdValue: number | null) {
    if (!sessionIdValue) return;
    await loadSessionDetail(sessionIdValue).catch(() => {});
    await loadSessionList().catch(() => {});
  }

  async function handleSendMessage() {
    if (!sessionId) {
      return;
    }
    const trimmed = inputValue.trim();
    if (!trimmed) {
      return;
    }

    // Validate session exists before sending
    try {
      await getPlanningSession(sessionId);
    } catch (err: unknown) {
      void messageApi.error("会话不存在，请刷新页面");
      await loadSessionList();
      return;
    }

    clearAllStreaming();
    const optimisticUser = createOptimisticMessage(sessionId, "user", "user", trimmed);
    const optimisticAssistant = createOptimisticMessage(sessionId, "assistant", "followup", "", {
      _phase: "thinking",
      _phaseMessage: "正在分析需求...",
      _streaming: true,
    });
    const assistantId = optimisticAssistant.id;
    setTranscript((current) => [...current, optimisticUser, optimisticAssistant]);
    setInputValue("");

    try {
      await runStream(sessionId, "chat", assistantId, {
        url: `/api/v1/ai-planning/sessions/${sessionId}/chat`,
        body: { content: trimmed },
        onEvent: handleStreamSideEffect,
      });
    } catch (error) {
      clearStreamingOnMessage(assistantId);
      if ((error as Error).name !== "AbortError") {
        void messageApi.error((error as Error).message);
        await loadSessionList();
      }
    } finally {
      await finalizeStream(sessionId);
    }
  }

  async function handleGenerateDrafts() {
    if (!sessionId || !selectedScenarioKeys.length) {
      return;
    }

    // Validate session exists before generating
    try {
      await getPlanningSession(sessionId);
    } catch (err: unknown) {
      void messageApi.error("会话不存在，请刷新页面");
      await loadSessionList();
      return;
    }

    clearAllStreaming();

    const optimisticAssistant = createOptimisticMessage(sessionId, "assistant", "plan", "", {
      _phase: "generating",
      _phaseMessage: "正在生成 DSL...",
      _streaming: true,
    });
    const assistantId = optimisticAssistant.id;
    setTranscript((current) => [...current, optimisticAssistant]);

    try {
      await runStream(sessionId, "drafts", assistantId, {
        url: `/api/v1/ai-planning/sessions/${sessionId}/drafts`,
        body: {
          scenario_keys: selectedScenarioKeys,
          current_case: currentCase ?? null,
          current_steps: currentSteps ?? null,
          current_input_contract: currentInputContract ?? null,
          current_output_contract: currentOutputContract ?? null,
          preserve_contracts: true,
        },
        onEvent: handleStreamSideEffect,
      });
    } catch (error) {
      clearStreamingOnMessage(assistantId);
      if ((error as Error).name !== "AbortError") {
        void messageApi.error((error as Error).message);
        await loadSessionList();
      }
    } finally {
      await finalizeStream(sessionId);
    }
  }

  function renderLeftPanel() {
    return (
      <PlanningRequirementsPanel
        requirements={requirements}
        missingSlots={missingSlots}
      />
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
          <div style={{ marginTop: 8 }}>
            <SessionProjectPanel sessionId={sessionId ?? 0} onProjectsChange={() => {
              queryClient.invalidateQueries({ queryKey: ["planning-sessions"] });
            }} />
          </div>
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
                await loadSessionDetail(id);
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
          {sessionId ? (
            <Button
              size="small"
              danger
              icon={<DeleteOutlined />}
              aria-label={`删除会话 ${sessionList.find((item) => item.id === sessionId)?.title ?? `#${sessionId}`}`}
              onClick={async () => {
                // Abort any ongoing SSE stream before deleting
                abortStream();

                const currentSession = sessionList.find((item) => item.id === sessionId);
                const label = currentSession?.title ?? `会话 #${sessionId}`;
                if (!window.confirm(`确认删除"${label}"吗？此操作不可恢复。`)) {
                  return;
                }

                setIsBootstrapping(true);
                try {
                  await deleteAndSelectSession(sessionId);
                  void messageApi.success("会话已删除");
                } catch (err: unknown) {
                  void messageApi.error("删除会话失败: " + (err instanceof Error ? err.message : String(err)));
                } finally {
                  setIsBootstrapping(false);
                }
              }}
            />
          ) : null}
          <Button
            type="primary"
            size="small"
            onClick={async () => {
              setIsBootstrapping(true);
              try {
                await createAndSelectSession();
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
                    {item.structured_payload?.result_summary ? (
                      <details style={{ fontSize: 12, color: "#666", background: "#fafafa",
                                        borderRadius: 6, padding: "4px 8px", marginTop: 4 }}>
                        <summary style={{ cursor: "pointer", fontWeight: 500 }}>
                          查看摘要
                          {item.structured_payload.result_summary &&
                            typeof item.structured_payload.result_summary === "object" &&
                            "page_title" in (item.structured_payload.result_summary as Record<string, unknown>)
                            ? ` — ${(item.structured_payload.result_summary as Record<string, unknown>).page_title}`
                            : ""}
                        </summary>
                        <pre style={{ whiteSpace: "pre-wrap", marginTop: 4, maxHeight: 200,
                                      overflowY: "auto", fontSize: 11 }}>
                          {JSON.stringify(item.structured_payload.result_summary, null, 2)}
                        </pre>
                      </details>
                    ) : null}
                  </>
                ) : item.role === "assistant" &&
                  item.structured_payload?.type === "execution_progress" ? (
                  <div>
                    <span style={{ fontWeight: 600 }}>⚡ 执行进度</span>
                    <div style={{ marginTop: 4, whiteSpace: "pre-wrap" }}>{item.content}</div>
                    {isExecuting && (
                      <div style={{ marginTop: 4 }}>
                        <Button
                          size="small"
                          danger
                          onClick={() => {
                            abortStream();
                            if (sessionId) void cancelExecution(sessionId);
                          }}
                        >
                          取消执行
                        </Button>
                      </div>
                    )}
                  </div>
                ) : item.role === "assistant" &&
                  item.structured_payload?.type === "execution_summary" &&
                  Array.isArray(item.structured_payload.execution_summaries) ? (
                  <div>
                    <div style={{ whiteSpace: "pre-wrap" }}>{item.content}</div>
                    {(item.structured_payload.execution_summaries as ExecutionSummaryResult[]).map((ex) => (
                      <div
                        key={ex.execution_id}
                        style={{ marginTop: 8, display: "flex", alignItems: "center", gap: 8 }}
                      >
                        {ex.status === "passed" ? "✅" : "❌"}
                        <span>{ex.case_name}</span>
                        <span style={{ color: "#888" }}>
                          {ex.passed_steps}/{ex.total_steps}步
                        </span>
                        {ex.duration_ms ? (
                          <span style={{ color: "#888" }}>{(ex.duration_ms / 1000).toFixed(1)}s</span>
                        ) : null}
                        <Link
                          to={ex.report_url}
                          state={{ fromExecutions: `/planning/sessions/${item.session_id}` }}
                        >
                          查看完整报告
                        </Link>
                      </div>
                    ))}
                    {item.structured_payload.analysis ? (
                      <div style={{ marginTop: 12 }}>
                        <ExecutionAnalysisPanel
                          analysis={item.structured_payload.analysis as ExecutionAnalysis}
                          compact
                        />
                      </div>
                    ) : null}
                  </div>
                ) : item.role === "assistant" &&
                  Array.isArray(item.structured_payload?.todo_list) &&
                  (item.structured_payload?.todo_list as Array<{ item: string; status: string }>).length > 0 ? (
                  <div>
                    <div style={{ whiteSpace: "pre-wrap", marginBottom: 8 }}>{item.content}</div>
                    <div style={{
                      background: "rgba(0,0,0,0.03)",
                      borderRadius: 8,
                      padding: "8px 12px",
                    }}>
                      {(item.structured_payload!.todo_list as Array<{ item: string; status: string }>).map((todo, idx) => (
                        <div key={idx} style={{ display: "flex", alignItems: "center", gap: 8, padding: "3px 0" }}>
                          {todo.status === "done" ? (
                            <CheckCircleFilled style={{ color: "#52c41a", fontSize: 14 }} />
                          ) : todo.status === "in_progress" ? (
                            <LoadingOutlined style={{ color: "#1677ff", fontSize: 14 }} />
                          ) : (
                            <ClockCircleOutlined style={{ color: "#d9d9d9", fontSize: 14 }} />
                          )}
                          <span style={{
                            textDecoration: todo.status === "done" ? "line-through" : "none",
                            color: todo.status === "pending" ? "#aaa" : todo.status === "done" ? "#888" : "#333",
                          }}>
                            {todo.item}
                          </span>
                        </div>
                      ))}
                    </div>
                  </div>
                ) : item.role === "assistant" && (item.structured_payload as Record<string, unknown>)?._phase ? (
                  <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                    <Tag color={phaseColorMap[((item.structured_payload as Record<string, unknown>)._phase as string) ?? ""] ?? "processing"}>
                      {String((item.structured_payload as Record<string, unknown>)._phaseMessage ?? "处理中...")}
                    </Tag>
                    <AssistantMessageBody
                      message={item}
                      streaming={Boolean((item.structured_payload as Record<string, unknown>)?._streaming)}
                    />
                    {(item.structured_payload as Record<string, unknown>)?._interrupted ? (
                      <span style={{ color: "#faad14", fontSize: 12 }}>⏸ 回复中断</span>
                    ) : (item.structured_payload as Record<string, unknown>)?._recovered ? (
                      <span style={{ color: "#52c41a", fontSize: 12 }}>✓ 已恢复</span>
                    ) : null}
                  </div>
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
              rows={2}
              variant="borderless"
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
            {(isSending || isGenerating || isExecuting) ? (
              <Button
                danger
                shape="circle"
                onClick={() => {
                  abortStream();
                  if (isExecuting && sessionId) void cancelExecution(sessionId);
                }}
                style={{
                  width: 40,
                  height: 40,
                  minWidth: 40,
                  flexShrink: 0,
                }}
              >
                ■
              </Button>
            ) : (
              <Button
                type="primary"
                shape="circle"
                icon={<SendOutlined />}
                onClick={() => void handleSendMessage()}
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
            )}
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
                disabled={!selectedScenarioKeys.length || isGenerating}
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
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
            <Typography.Text strong style={{ fontSize: 14 }}>
              测试用例草案
            </Typography.Text>
            <div style={{ display: "flex", gap: 8 }}>
              <Button
                size="small"
                disabled={selectedScenarioKeys.length === 0 || isSaving}
                onClick={async () => {
                  if (!sessionId || selectedScenarioKeys.length === 0) return;
                  setIsSaving(true);
                  try {
                    const resp = await saveAndExecuteDrafts(
                      sessionId,
                      drafts.filter((d) => selectedScenarioKeys.includes(d.scenario_key)).map((d) => d.id),
                      false,
                    );
                    await queryClient.invalidateQueries({ queryKey: ["cases"] });
                    await loadSessionDetail(sessionId);
                    void messageApi.success(`已保存 ${resp.saved_cases?.length ?? 0} 个用例`);
                    await loadSessionList();
                  } catch (err: unknown) {
                    void messageApi.error("保存失败: " + (err instanceof Error ? err.message : String(err)));
                  } finally {
                    setIsSaving(false);
                  }
                }}
              >
                仅保存
              </Button>
              <Button
                type="primary"
                size="small"
                loading={isExecuting}
                disabled={selectedScenarioKeys.length === 0 || isExecuting}
                onClick={async () => {
                  if (!sessionId || selectedScenarioKeys.length === 0) return;
                  const draftIds = drafts
                    .filter((d) => selectedScenarioKeys.includes(d.scenario_key))
                    .map((d) => d.id);

                  const progressMessage = createOptimisticMessage(
                    sessionId,
                    "assistant",
                    "followup",
                    "正在保存并执行已选草案…",
                    { type: "execution_progress", saved_count: 0, total: 0, cases: [] },
                  );
                  const progressId = progressMessage.id;
                  setTranscript((current) => [...current, progressMessage]);

                  try {
                    await runStream(sessionId, "execute", progressId, {
                      url: `/api/v1/ai-planning/sessions/${sessionId}/execute`,
                      body: { draft_ids: draftIds },
                      onEvent: handleStreamSideEffect,
                    });
                  } catch (error) {
                    clearStreamingOnMessage(progressId);
                    if ((error as Error).name !== "AbortError") {
                      void messageApi.error("执行失败: " + (error instanceof Error ? error.message : String(error)));
                    }
                  } finally {
                    await finalizeStream(sessionId);
                  }
                }}
              >
                保存并执行
              </Button>
            </div>
          </div>
          <div style={{ marginTop: 12, display: "flex", flexDirection: "column", gap: 8 }}>
            {drafts.map((draft) => (
              <div key={draft.id} style={{ padding: "8px 0", borderBottom: "1px solid #f5f5f5" }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <Checkbox
                    checked={selectedScenarioKeys.includes(draft.scenario_key)}
                    onChange={(e) => {
                      setSelectedScenarioKeys((prev) =>
                        e.target.checked
                          ? [...prev, draft.scenario_key]
                          : prev.filter((k) => k !== draft.scenario_key),
                      );
                    }}
                    disabled={draft.status !== "generated" || !draft.dsl_case}
                  />
                  <Typography.Text strong style={{ fontSize: 13, flex: 1 }}>
                    {draft.title}
                  </Typography.Text>
                  <Tag>{draft.status}</Tag>
                  <DeleteOutlined
                    style={{ fontSize: 12, color: "#999", cursor: "pointer" }}
                    title="删除草案"
                    onClick={async () => {
                      try {
                        await deletePlanningDraft(draft.id);
                        setDrafts((prev) => prev.filter((d) => d.id !== draft.id));
                        setSelectedScenarioKeys((prev) => prev.filter((k) => k !== draft.scenario_key));
                        void messageApi.success("草案已删除");
                      } catch (err) {
                        void messageApi.error("删除失败: " + (err instanceof Error ? err.message : String(err)));
                      }
                    }}
                  />
                </div>
                {draft.error_message ? (
                  <Alert type="error" showIcon message={draft.error_message} style={{ marginTop: 4, fontSize: 12 }} />
                ) : null}
                {draft.dsl_case ? (
                  <div style={{ marginLeft: 30, color: "#888", fontSize: 12, marginTop: 4 }}>
                    {draft.dsl_case.steps.map((s) => s.action).join(" → ")}
                  </div>
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
