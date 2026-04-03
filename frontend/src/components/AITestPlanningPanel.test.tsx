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
  enable_ai_planning: true,
  ai_planning_model: "gpt-4.1-mini",
  ai_planning_base_url: "https://api.openai.com/v1",
  ai_planning_timeout_ms: 30000,
  ai_planning_max_react_rounds: 5,
  has_ai_planning_api_key: true,
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
});

test("展示动态进度、工具调用并支持直接生成方案", async () => {
  vi.mocked(api.sendPlanningMessage).mockResolvedValue({
    assistant_message: "信息已经足够，我先给出结构化测试方案。",
    session_status: "plan_ready",
    requirements: {
      app_under_test: "商城后台",
      business_goal: "验证管理员登录",
      entry_url_or_page: "https://shop.example.com/login",
      core_user_flow: "输入账号密码并点击登录",
      main_assertions: ["跳转到 dashboard"],
      test_data_or_account: null,
      scope_limits: null,
    },
    missing_slots: [],
    suggested_questions: [],
    plan: {
      summary: "商城后台登录测试方案",
      assumptions: ["入口页面为 /login"],
      risks: ["未覆盖忘记密码"],
      scenarios: [
        {
          scenario_key: "login_success",
          title: "登录成功",
          goal: "验证管理员可以登录后台",
          preconditions: ["准备管理员账号"],
          priority: "high",
          test_data_requirements: [
            { key: "username", label: "管理员账号", value_type: "string", required: true, source_hint: "seed" },
          ],
          assertions: ["跳转到 dashboard"],
          draft_prompt: "为登录成功场景生成 DSL",
        },
      ],
    },
    drafts: [],
    next_action: "select_scenarios",
    tool_calls: [
      {
        tool: "list_test_cases",
        params: { search: "登录" },
        result: { cases: [{ id: 1, name: "后台登录成功" }] },
      },
    ],
  });

  renderWithProviders(
    <AITestPlanningPanel aiSettings={aiSettings} projectId={1} caseId={undefined} onImportDraft={vi.fn()} />,
  );

  expect(await screen.findByText("AI 测试规划助手")).toBeInTheDocument();
  expect(screen.getByText("已收集 0 / 7 项")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "直接生成方案" }));

  expect(await screen.findByText("调用工具：list_test_cases")).toBeInTheDocument();
  expect(screen.getByText("已收集 5 / 7 项")).toBeInTheDocument();
  expect(screen.getByText("商城后台登录测试方案")).toBeInTheDocument();
  expect(screen.getByRole("checkbox", { name: "选择场景 登录成功" })).toBeInTheDocument();
});

test("可以生成草案并导入到当前编辑器", async () => {
  const onImportDraft = vi.fn();

  vi.mocked(api.sendPlanningMessage).mockResolvedValue({
    assistant_message: "信息已经足够，我先给出结构化测试方案。",
    session_status: "plan_ready",
    requirements: {
      app_under_test: "商城后台",
      business_goal: "验证管理员登录",
      entry_url_or_page: "https://shop.example.com/login",
      core_user_flow: "输入账号密码并点击登录",
      main_assertions: ["跳转到 dashboard"],
      test_data_or_account: "admin@example.com",
      scope_limits: "不覆盖忘记密码",
    },
    missing_slots: [],
    suggested_questions: [],
    plan: {
      summary: "商城后台登录测试方案",
      assumptions: ["入口页面为 /login"],
      risks: ["未覆盖忘记密码"],
      scenarios: [
        {
          scenario_key: "login_success",
          title: "登录成功",
          goal: "验证管理员可以登录后台",
          preconditions: ["准备管理员账号"],
          priority: "high",
          test_data_requirements: [],
          assertions: ["跳转到 dashboard"],
          draft_prompt: "为登录成功场景生成 DSL",
        },
      ],
    },
    drafts: [],
    next_action: "select_scenarios",
    tool_calls: [],
  });

  vi.mocked(api.generatePlanningDrafts).mockResolvedValue({
    assistant_message: "已根据所选场景生成 DSL 草案。",
    session_status: "drafts_ready",
    requirements: {
      app_under_test: "商城后台",
      business_goal: "验证管理员登录",
      entry_url_or_page: "https://shop.example.com/login",
      core_user_flow: "输入账号密码并点击登录",
      main_assertions: ["跳转到 dashboard"],
      test_data_or_account: "admin@example.com",
      scope_limits: "不覆盖忘记密码",
    },
    missing_slots: [],
    suggested_questions: [],
    plan: {
      summary: "商城后台登录测试方案",
      assumptions: ["入口页面为 /login"],
      risks: ["未覆盖忘记密码"],
      scenarios: [
        {
          scenario_key: "login_success",
          title: "登录成功",
          goal: "验证管理员可以登录后台",
          preconditions: ["准备管理员账号"],
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
    tool_calls: [],
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

  expect(await screen.findByText("AI 测试规划助手")).toBeInTheDocument();
  await userEvent.type(screen.getByLabelText("测试规划对话输入"), "请先整理后台登录测试方案");
  await userEvent.click(screen.getByRole("button", { name: "发送消息" }));

  await userEvent.click(await screen.findByRole("checkbox", { name: "选择场景 登录成功" }));
  await userEvent.click(screen.getByRole("button", { name: "生成选中草案" }));
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
