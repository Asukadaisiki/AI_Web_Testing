import { useMemo, useState } from "react";
import {
  Alert,
  Button,
  Checkbox,
  Input,
  Radio,
  Select,
  Spin,
  Tag,
  Typography,
  message,
} from "antd";
import {
  CheckCircleFilled,
  ClockCircleOutlined,
  LoadingOutlined,
  ReloadOutlined,
  SendOutlined,
  ToolOutlined,
} from "@ant-design/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";

import {
  readArtifacts,
  readAssistantMessages,
  readToolActivities,
} from "../features/agent/events";
import type {
  AgentQuestion,
  AgentRunStatus,
  AgentToolActivity,
} from "../features/agent/types";
import { useAgentRun } from "../features/agent/useAgentRun";
import {
  getPlanningSession,
  listPlanningSessions,
} from "../features/planning/api";
import type { AISettings } from "../types/api";
import { NotebookLMLayout } from "../layouts/NotebookLMLayout";
import { SessionProjectPanel } from "./SessionProjectPanel";

interface AgentWorkbenchProps {
  aiSettings?: AISettings | null;
  sessionId: number;
}

const RUN_STATUS: Record<AgentRunStatus, { label: string; color: string }> = {
  running: { label: "运行中", color: "processing" },
  waiting_user: { label: "等待确认", color: "warning" },
  completed: { label: "已完成", color: "success" },
  failed: { label: "失败", color: "error" },
  cancelled: { label: "已取消", color: "default" },
};

const TOOL_LABELS: Record<string, string> = {
  ask_user_question: "请求确认",
  explore_page: "探索页面",
  explore_flow: "探索流程",
  validate_page_elements: "验证页面元素",
  generate_dsl: "生成 DSL",
  execute_dsl: "执行 DSL",
  get_report: "读取报告",
  fix_and_retry: "分析并准备修复",
};

function toolStatusIcon(status: AgentToolActivity["status"]) {
  if (status === "completed") {
    return <CheckCircleFilled style={{ color: "#2f855a" }} />;
  }
  if (status === "failed") {
    return <span style={{ color: "#c53030" }}>!</span>;
  }
  if (status === "waiting_user") {
    return <ClockCircleOutlined style={{ color: "#b7791f" }} />;
  }
  return <LoadingOutlined style={{ color: "#2563eb" }} />;
}

function QuestionControl({
  question,
  value,
  onChange,
}: {
  question: AgentQuestion;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  if (question.type === "confirm") {
    return (
      <Radio.Group
        value={value}
        onChange={(event) => onChange(event.target.value)}
        options={[
          { label: "批准", value: true },
          { label: "拒绝", value: false },
        ]}
      />
    );
  }
  if (question.type === "single_select") {
    return (
      <Select
        style={{ width: "100%" }}
        value={value}
        onChange={onChange}
        options={(question.options ?? []).map((option) => ({
          label: option.label,
          value: option.value,
        }))}
      />
    );
  }
  if (question.type === "multi_select") {
    return (
      <Checkbox.Group
        value={Array.isArray(value) ? value as string[] : []}
        onChange={onChange}
        options={(question.options ?? []).map((option) => ({
          label: option.label,
          value: option.value,
        }))}
      />
    );
  }
  return (
    <Input.TextArea
      rows={2}
      value={typeof value === "string" ? value : ""}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

export function AgentWorkbench({
  aiSettings,
  sessionId,
}: AgentWorkbenchProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [messageApi, contextHolder] = message.useMessage();
  const [input, setInput] = useState("");
  const [answers, setAnswers] = useState<Record<string, unknown>>({});
  const [submitting, setSubmitting] = useState(false);
  const { error, events, loading, reconnect, resume, run, start } =
    useAgentRun(String(sessionId));

  const sessionQuery = useQuery({
    queryKey: ["planning-session", sessionId],
    queryFn: () => getPlanningSession(sessionId),
  });
  const sessionsQuery = useQuery({
    queryKey: ["planning-sessions"],
    queryFn: listPlanningSessions,
  });
  const messages = useMemo(() => readAssistantMessages(events), [events]);
  const tools = useMemo(() => readToolActivities(events), [events]);
  const artifacts = useMemo(() => readArtifacts(events), [events]);
  const pendingTool = tools.find(
    (tool) =>
      tool.id === run?.pending_tool_call_id &&
      tool.status === "waiting_user",
  );
  const projectId = sessionQuery.data?.session.active_project_id ?? null;
  const busy = run?.status === "running" || submitting;

  async function handleStart() {
    const content = input.trim();
    if (!content || !projectId) return;
    setSubmitting(true);
    try {
      await start(projectId, content);
      setInput("");
      setAnswers({});
    } catch (cause) {
      void messageApi.error(
        cause instanceof Error ? cause.message : String(cause),
      );
    } finally {
      setSubmitting(false);
    }
  }

  async function handleResume() {
    if (!pendingTool?.questions) return;
    const missingRequired = pendingTool.questions.some(
      (question) =>
        question.required &&
        (answers[question.id] === undefined ||
          answers[question.id] === "" ||
          (Array.isArray(answers[question.id]) &&
            (answers[question.id] as unknown[]).length === 0)),
    );
    if (missingRequired) {
      void messageApi.warning("请完成所有必填项");
      return;
    }
    setSubmitting(true);
    try {
      await resume(answers);
      setAnswers({});
    } catch (cause) {
      void messageApi.error(
        cause instanceof Error ? cause.message : String(cause),
      );
    } finally {
      setSubmitting(false);
    }
  }

  const leftPanel = (
    <>
      <Typography.Text strong>会话与项目</Typography.Text>
      <Select
        style={{ width: "100%", marginTop: 12 }}
        value={sessionId}
        loading={sessionsQuery.isLoading}
        onChange={(value: number) => navigate(`/planning/sessions/${value}`)}
        options={(sessionsQuery.data ?? []).map((session) => ({
          value: session.id,
          label: session.title || `会话 #${session.id}`,
        }))}
      />
      <div style={{ marginTop: 16 }}>
        <SessionProjectPanel
          sessionId={sessionId}
          onProjectsChange={() => {
            void queryClient.invalidateQueries({
              queryKey: ["planning-session", sessionId],
            });
            void queryClient.invalidateQueries({
              queryKey: ["planning-sessions"],
            });
          }}
        />
      </div>
      <div style={{ marginTop: 20 }}>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          当前模型
        </Typography.Text>
        <div style={{ marginTop: 4 }}>
          <Tag>{aiSettings?.ai_planning_model ?? "未配置"}</Tag>
        </div>
      </div>
      <div style={{ marginTop: 20 }}>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          当前 Run
        </Typography.Text>
        <div style={{ marginTop: 6 }}>
          {run ? (
            <Tag color={RUN_STATUS[run.status].color}>
              {RUN_STATUS[run.status].label}
            </Tag>
          ) : (
            <Typography.Text type="secondary">尚未运行</Typography.Text>
          )}
        </div>
        {run ? (
          <Typography.Text
            copyable={{ text: run.id }}
            style={{ display: "block", marginTop: 6, fontSize: 11 }}
          >
            {run.id}
          </Typography.Text>
        ) : null}
      </div>
    </>
  );

  const centerPanel = (
    <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
      {contextHolder}
      <div
        style={{
          padding: "18px 28px",
          borderBottom: "1px solid #e5e7eb",
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
        }}
      >
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            AgentCore
          </Typography.Title>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            工具调用、审批与执行事件实时可见
          </Typography.Text>
        </div>
        {run ? (
          <Button
            icon={<ReloadOutlined />}
            onClick={() => void reconnect()}
            disabled={submitting}
          >
            刷新
          </Button>
        ) : null}
      </div>

      <div
        className="panel-scroll"
        style={{ flex: 1, overflowY: "auto", padding: "22px 28px" }}
      >
        {loading ? <Spin /> : null}
        {error ? (
          <Alert type="error" showIcon message="AgentCore 连接失败" description={error} />
        ) : null}
        {!projectId ? (
          <Alert
            type="warning"
            showIcon
            message="请先为会话关联并选择当前项目"
          />
        ) : null}
        {!run && !loading ? (
          <div style={{ padding: "48px 0", textAlign: "center" }}>
            <ToolOutlined style={{ fontSize: 28, color: "#64748b" }} />
            <Typography.Title level={5}>描述一个可执行测试目标</Typography.Title>
            <Typography.Text type="secondary">
              Agent 会按需探索页面、验证元素、生成 DSL，并在执行前请求批准。
            </Typography.Text>
          </div>
        ) : null}
        {run ? (
          <div className="agent-message agent-message-user">{run.input}</div>
        ) : null}
        {messages.map((item) => (
          <div key={item.seq} className="agent-message agent-message-assistant">
            {item.content}
          </div>
        ))}
        {pendingTool?.questions?.length ? (
          <div className="agent-checkpoint">
            <Typography.Text strong>需要你的确认</Typography.Text>
            {pendingTool.questions.map((question) => (
              <div key={question.id} style={{ marginTop: 12 }}>
                <Typography.Text style={{ display: "block", marginBottom: 6 }}>
                  {question.question}
                </Typography.Text>
                <QuestionControl
                  question={question}
                  value={answers[question.id]}
                  onChange={(value) =>
                    setAnswers((current) => ({
                      ...current,
                      [question.id]: value,
                    }))
                  }
                />
              </div>
            ))}
            <Button
              type="primary"
              style={{ marginTop: 14 }}
              loading={submitting}
              onClick={() => void handleResume()}
            >
              提交并继续
            </Button>
          </div>
        ) : null}
      </div>

      <div style={{ padding: "14px 20px", borderTop: "1px solid #e5e7eb" }}>
        <div className="agent-composer">
          <Input.TextArea
            aria-label="AgentCore 对话输入"
            autoSize={{ minRows: 2, maxRows: 5 }}
            variant="borderless"
            value={input}
            onChange={(event) => setInput(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void handleStart();
              }
            }}
            disabled={busy || run?.status === "waiting_user"}
            placeholder={
              run?.status === "waiting_user"
                ? "请先完成上方确认"
                : "输入测试目标、入口 URL、核心流程和断言"
            }
          />
          <Button
            type="primary"
            shape="circle"
            aria-label="发送"
            icon={<SendOutlined />}
            loading={submitting}
            disabled={
              busy ||
              run?.status === "waiting_user" ||
              !projectId ||
              !input.trim()
            }
            onClick={() => void handleStart()}
          />
        </div>
      </div>
    </div>
  );

  const rightCards = [
    <div key="tools">
      <Typography.Text strong>工具轨迹</Typography.Text>
      <div style={{ marginTop: 12 }}>
        {tools.length === 0 ? (
          <Typography.Text type="secondary">暂无工具调用</Typography.Text>
        ) : (
          tools.map((tool) => (
            <details key={tool.id} className="agent-tool-row">
              <summary>
                {toolStatusIcon(tool.status)}
                <span>{TOOL_LABELS[tool.name] ?? tool.name}</span>
              </summary>
              {tool.arguments !== undefined ? (
                <pre>{JSON.stringify(tool.arguments, null, 2)}</pre>
              ) : null}
              {tool.result !== undefined ? (
                <pre>{JSON.stringify(tool.result, null, 2)}</pre>
              ) : null}
              {tool.error ? <Alert type="error" message={tool.error} /> : null}
            </details>
          ))
        )}
      </div>
    </div>,
    <div key="artifacts">
      <Typography.Text strong>产物</Typography.Text>
      <div style={{ marginTop: 12, display: "flex", flexWrap: "wrap", gap: 8 }}>
        {artifacts.length === 0 ? (
          <Typography.Text type="secondary">暂无产物</Typography.Text>
        ) : (
          artifacts.map((artifact) => (
            <Tag key={`${artifact.seq}-${artifact.type}-${artifact.id}`}>
              {artifact.type} #{artifact.id}
            </Tag>
          ))
        )}
      </div>
    </div>,
  ];

  return (
    <NotebookLMLayout
      leftPanel={leftPanel}
      centerPanel={centerPanel}
      rightCards={rightCards}
    />
  );
}
