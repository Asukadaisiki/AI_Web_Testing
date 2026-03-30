import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { AITestPlanningPanel } from "./AITestPlanningPanel";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";
import type { AISettings } from "../types/api";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    createPlanningSession: vi.fn(),
    generatePlanningDrafts: vi.fn(),
    getPlanningSession: vi.fn(),
    sendPlanningMessage: vi.fn(),
    updatePlanningDraftStatus: vi.fn(),
  };
});

const aiSettings: AISettings = {
  enable_ai_dsl_generate: true,
  ai_dsl_timeout_ms: 15000,
  ai_dsl_base_url: "https://api.openai.com/v1",
  ai_dsl_model: "gpt-4o-mini",
  ai_dsl_strict_mode: false,
  ai_dsl_allow_auto_repair: true,
  has_ai_dsl_api_key: true,
  enable_ai_visual_locate: false,
  ai_visual_timeout_ms: 10000,
  ai_visual_failure_threshold: 3,
  ai_visual_cooldown_seconds: 60,
  ai_visual_rate_limit_per_minute: 10,
  vlm_base_url: "https://api.openai.com/v1",
  vlm_model: "gpt-4o",
  vlm_model_family: "gpt-4o",
  has_vlm_api_key: false,
};

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(api.createPlanningSession).mockResolvedValue({
    session: {
      id: 5,
      actor_user_id: 1,
      project_id: 1,
      case_id: null,
      title: null,
      status: "collecting",
      requirements: {
        app_under_test: null,
        business_goal: null,
        entry_url_or_page: null,
        core_user_flow: null,
        main_assertions: [],
        test_data_or_account: null,
        scope_limits: null,
      },
      plan: null,
      missing_slots: ["app_under_test", "business_goal"],
      last_error_message: null,
      created_at: "2026-03-30T10:00:00",
      updated_at: "2026-03-30T10:00:00",
    },
    messages: [],
    drafts: [],
  });
  vi.mocked(api.getPlanningSession).mockResolvedValue({
    session: {
      id: 5,
      actor_user_id: 1,
      project_id: 1,
      case_id: null,
      title: null,
      status: "collecting",
      requirements: {
        app_under_test: null,
        business_goal: null,
        entry_url_or_page: null,
        core_user_flow: null,
        main_assertions: [],
        test_data_or_account: null,
        scope_limits: null,
      },
      plan: null,
      missing_slots: ["app_under_test", "business_goal"],
      last_error_message: null,
      created_at: "2026-03-30T10:00:00",
      updated_at: "2026-03-30T10:00:00",
    },
    messages: [],
    drafts: [],
  });
});

test("发送消息后展示追问与方案，并生成草案", async () => {
  vi.mocked(api.sendPlanningMessage).mockResolvedValueOnce({
    assistant_message: "为了完善测试方案，我还需要补充几项关键信息。",
    session_status: "collecting",
    requirements: {
      app_under_test: "电商后台",
      business_goal: null,
      entry_url_or_page: null,
      core_user_flow: null,
      main_assertions: [],
      test_data_or_account: null,
      scope_limits: null,
    },
    missing_slots: ["business_goal", "entry_url_or_page"],
    suggested_questions: ["请明确这次测试想验证的核心业务目标。", "请提供入口页面 URL 或页面名称。"],
    plan: null,
    drafts: [],
    next_action: "ask_followup",
  });
  vi.mocked(api.sendPlanningMessage).mockResolvedValueOnce({
    assistant_message: "信息已经足够，我先给出结构化测试方案，请选择要生成草案的场景。",
    session_status: "plan_ready",
    requirements: {
      app_under_test: "电商后台",
      business_goal: "验证管理员登录",
      entry_url_or_page: "https://shop.example.com/login",
      core_user_flow: "输入账号密码并点击登录",
      main_assertions: ["跳转到 dashboard", "显示欢迎文案"],
      test_data_or_account: "管理员账号",
      scope_limits: "不覆盖注册和忘记密码",
    },
    missing_slots: [],
    suggested_questions: [],
    plan: {
      summary: "电商后台 - 验证管理员登录",
      assumptions: ["入口页使用 https://shop.example.com/login"],
      risks: ["不覆盖注册和忘记密码"],
      scenarios: [
        {
          scenario_key: "login_success",
          title: "登录成功",
          goal: "验证管理员登录",
          preconditions: ["准备管理员账号"],
          priority: "high",
          test_data_requirements: [
            { key: "username", label: "登录账号", value_type: "string", required: true, source_hint: "管理员账号" },
          ],
          assertions: ["跳转到 dashboard"],
          draft_prompt: "为登录成功场景生成 DSL",
        },
      ],
    },
    drafts: [],
    next_action: "select_scenarios",
  });
  vi.mocked(api.generatePlanningDrafts).mockResolvedValue({
    assistant_message: "已根据所选场景生成 DSL 草案。",
    session_status: "drafts_ready",
    requirements: {
      app_under_test: "电商后台",
      business_goal: "验证管理员登录",
      entry_url_or_page: "https://shop.example.com/login",
      core_user_flow: "输入账号密码并点击登录",
      main_assertions: ["跳转到 dashboard", "显示欢迎文案"],
      test_data_or_account: "管理员账号",
      scope_limits: "不覆盖注册和忘记密码",
    },
    missing_slots: [],
    suggested_questions: [],
    plan: {
      summary: "电商后台 - 验证管理员登录",
      assumptions: [],
      risks: [],
      scenarios: [
        {
          scenario_key: "login_success",
          title: "登录成功",
          goal: "验证管理员登录",
          preconditions: [],
          priority: "high",
          test_data_requirements: [],
          assertions: ["跳转到 dashboard"],
          draft_prompt: "为登录成功场景生成 DSL",
        },
      ],
    },
    drafts: [
      {
        id: 11,
        session_id: 5,
        scenario_key: "login_success",
        title: "登录成功",
        status: "generated",
        dsl_generation_id: 33,
        dsl_case: {
          name: "登录成功",
          description: "草案",
          base_url: "https://shop.example.com",
          input_contract: [],
          output_contract: [],
          steps: [{ action: "goto", value: "/login" }],
        },
        warnings: [],
        normalization_notes: [],
        error_message: null,
        created_at: "2026-03-30T10:00:00",
        updated_at: "2026-03-30T10:00:00",
      },
    ],
    next_action: "drafts_generated",
  });

  renderWithProviders(
    <AITestPlanningPanel aiSettings={aiSettings} projectId={1} caseId={undefined} onImportDraft={vi.fn()} />,
  );

  expect(await screen.findByText("AI 测试助手")).toBeInTheDocument();
  await userEvent.type(screen.getByLabelText("测试规划对话输入"), "帮我设计登录测试");
  await waitFor(() => {
    expect(screen.getByRole("button", { name: "发送消息" })).toBeEnabled();
  });
  await userEvent.click(screen.getByRole("button", { name: "发送消息" }));

  expect(await screen.findByText("为了完善测试方案，我还需要补充几项关键信息。")).toBeInTheDocument();
  expect(screen.getByText("请明确这次测试想验证的核心业务目标。")).toBeInTheDocument();

  await userEvent.clear(screen.getByLabelText("测试规划对话输入"));
  await userEvent.type(screen.getByLabelText("测试规划对话输入"), "补充完整登录方案");
  await userEvent.click(screen.getByRole("button", { name: "发送消息" }));

  expect(await screen.findByText("登录成功")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("checkbox", { name: "选择场景 登录成功" }));
  await userEvent.click(screen.getByRole("button", { name: "生成选中草案" }));

  expect(await screen.findByText("已根据所选场景生成 DSL 草案。")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "导入到当前编辑器" })).toBeInTheDocument();
});

test("导入草案时回调父组件并更新草案状态", async () => {
  const onImportDraft = vi.fn();
  vi.mocked(api.sendPlanningMessage).mockResolvedValue({
    assistant_message: "信息已经足够，我先给出结构化测试方案，请选择要生成草案的场景。",
    session_status: "drafts_ready",
    requirements: {
      app_under_test: "电商后台",
      business_goal: "验证管理员登录",
      entry_url_or_page: "https://shop.example.com/login",
      core_user_flow: "输入账号密码并点击登录",
      main_assertions: ["跳转到 dashboard"],
      test_data_or_account: "管理员账号",
      scope_limits: "不覆盖注册",
    },
    missing_slots: [],
    suggested_questions: [],
    plan: {
      summary: "电商后台 - 验证管理员登录",
      assumptions: [],
      risks: [],
      scenarios: [
        {
          scenario_key: "login_success",
          title: "登录成功",
          goal: "验证管理员登录",
          preconditions: [],
          priority: "high",
          test_data_requirements: [],
          assertions: ["跳转到 dashboard"],
          draft_prompt: "为登录成功场景生成 DSL",
        },
      ],
    },
    drafts: [
      {
        id: 11,
        session_id: 5,
        scenario_key: "login_success",
        title: "登录成功",
        status: "generated",
        dsl_generation_id: 33,
        dsl_case: {
          name: "登录成功",
          description: "草案",
          base_url: "https://shop.example.com",
          input_contract: [],
          output_contract: [],
          steps: [{ action: "goto", value: "/login" }],
        },
        warnings: [],
        normalization_notes: [],
        error_message: null,
        created_at: "2026-03-30T10:00:00",
        updated_at: "2026-03-30T10:00:00",
      },
    ],
    next_action: "drafts_generated",
  });
  vi.mocked(api.updatePlanningDraftStatus).mockResolvedValue({
    id: 11,
    session_id: 5,
    scenario_key: "login_success",
    title: "登录成功",
    status: "imported",
    dsl_generation_id: 33,
    dsl_case: {
      name: "登录成功",
      description: "草案",
      base_url: "https://shop.example.com",
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/login" }],
    },
    warnings: [],
    normalization_notes: [],
    error_message: null,
    created_at: "2026-03-30T10:00:00",
    updated_at: "2026-03-30T10:00:00",
  });

  renderWithProviders(
    <AITestPlanningPanel aiSettings={aiSettings} projectId={1} caseId={undefined} onImportDraft={onImportDraft} />,
  );

  expect(await screen.findByText("AI 测试助手")).toBeInTheDocument();
  await waitFor(() => {
    expect(screen.getByLabelText("测试规划对话输入")).toBeEnabled();
  });
  await userEvent.type(screen.getByLabelText("测试规划对话输入"), "直接给我完整登录方案");
  await waitFor(() => {
    expect(screen.getByRole("button", { name: "发送消息" })).toBeEnabled();
  });
  await userEvent.click(screen.getByRole("button", { name: "发送消息" }));
  await userEvent.click(await screen.findByRole("button", { name: "导入到当前编辑器" }));

  await waitFor(() => {
    expect(onImportDraft).toHaveBeenCalledWith(
      expect.objectContaining({
        scenario_key: "login_success",
      }),
    );
    expect(api.updatePlanningDraftStatus).toHaveBeenCalledWith(11, { status: "imported" });
  });
});
