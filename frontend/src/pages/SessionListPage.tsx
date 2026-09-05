import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, Tag, Empty, Spin, Typography, message } from "antd";
import { PlusOutlined } from "@ant-design/icons";
import {
  createPlanningSession,
  listPlanningSessions,
} from "../features/planning/api";
import { WorkspacePageLayout } from "../layouts/WorkspacePageLayout";

const STATUS_LABELS: Record<string, { label: string; color: string }> = {
  collecting: { label: "收集中", color: "processing" },
  plan_ready: { label: "计划就绪", color: "warning" },
  drafts_ready: { label: "草案就绪", color: "success" },
  reviewing: { label: "审查中", color: "processing" },
  saving: { label: "保存中", color: "processing" },
  executing: { label: "执行中", color: "processing" },
  completed: { label: "已完成", color: "default" },
  closed: { label: "已关闭", color: "default" },
  error: { label: "错误", color: "error" },
};

export function SessionListPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [creating, setCreating] = useState(false);

  const sessionsQuery = useQuery({
    queryKey: ["planning-sessions"],
    queryFn: listPlanningSessions,
  });

  async function handleCreate() {
    setCreating(true);
    try {
      const detail = await createPlanningSession({});
      queryClient.invalidateQueries({ queryKey: ["planning-sessions"] });
      navigate(`/planning/sessions/${detail.session.id}`);
    } catch (err) {
      void message.error(err instanceof Error ? err.message : "创建会话失败");
    } finally {
      setCreating(false);
    }
  }

  const sessions = sessionsQuery.data ?? [];

  return (
    <WorkspacePageLayout
      title="AI 测试规划"
      description="管理规划会话，并进入 AgentCore 工作台生成和执行结构化用例。"
      actions={
        <Button type="primary" icon={<PlusOutlined />} loading={creating} onClick={handleCreate}>
          新建会话
        </Button>
      }
    >
      {sessionsQuery.isLoading ? (
        <Spin />
      ) : sessions.length === 0 ? (
        <Empty description="暂无规划会话，点击「新建会话」开始" />
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          {sessions.map((s) => {
            const statusInfo = STATUS_LABELS[s.status] ?? { label: s.status, color: "default" };
            return (
              <Card
                key={s.id}
                hoverable
                size="small"
                onClick={() => navigate(`/planning/sessions/${s.id}`)}
                style={{ cursor: "pointer" }}
              >
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                  <div>
                    <Typography.Text strong>
                      {s.title || `会话 #${s.id}`}
                    </Typography.Text>
                    <div style={{ marginTop: 4, display: "flex", gap: 4, flexWrap: "wrap" }}>
                      {s.projects.map((p) => (
                        <Tag key={p.id} color="blue">{p.name}</Tag>
                      ))}
                      {s.projects.length === 0 && (
                        <Tag color="default">未关联项目</Tag>
                      )}
                    </div>
                  </div>
                  <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                    <Tag color={statusInfo.color}>{statusInfo.label}</Tag>
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      {new Date(s.updated_at).toLocaleString()}
                    </Typography.Text>
                  </div>
                </div>
              </Card>
            );
          })}
        </div>
      )}
    </WorkspacePageLayout>
  );
}
