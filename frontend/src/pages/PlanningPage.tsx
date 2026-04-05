import { Alert, Space, Typography } from "antd";

import { AITestPlanningPanel } from "../components/AITestPlanningPanel";
import { useQuery } from "@tanstack/react-query";
import { createCase, getAISettings, getProjects } from "../services/api";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import type { AIPlanningDraft } from "../types/api";

export function PlanningPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const projectsQuery = useQuery({ queryKey: ["projects"], queryFn: getProjects });
  const aiSettingsQuery = useQuery({ queryKey: ["ai-settings"], queryFn: getAISettings });

  const firstProject = projectsQuery.data?.[0];

  async function handleImportDraft(draft: AIPlanningDraft) {
    if (!draft.dsl_case) {
      throw new Error("规划草案没有可创建的 DSL 内容。");
    }
    if (!firstProject) {
      throw new Error("当前没有可用项目，无法创建用例。");
    }
    const createdCase = await createCase({
      project_id: firstProject.id,
      actor_user_id: 1,
      ...draft.dsl_case,
    });
    await queryClient.invalidateQueries({ queryKey: ["cases"] });
    navigate(`/cases?created=${createdCase.id}`);
  }

  if (projectsQuery.data?.length === 0) {
    return (
      <div style={{ padding: 24 }}>
        <Alert
          type="warning"
          showIcon
          message="暂无可用项目"
          description="请先在数据库中创建至少一个项目，再使用 AI 规划。"
        />
      </div>
    );
  }

  return (
    <div style={{ padding: 24 }}>
      <Space direction="vertical" size="middle" style={{ width: "100%" }}>
        <Typography.Title level={3}>AI 测试规划</Typography.Title>
        <Typography.Paragraph type="secondary">
          描述您的测试需求，AI 将帮助您生成测试方案和测试用例。这是演示流程的第一步。
        </Typography.Paragraph>
        <AITestPlanningPanel
          aiSettings={aiSettingsQuery.data ?? null}
          projectId={firstProject?.id}
          onImportDraft={handleImportDraft}
          draftImportLabel="创建用例并进入用例中心"
        />
      </Space>
    </div>
  );
}
