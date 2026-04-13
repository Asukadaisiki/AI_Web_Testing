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
    deletePlanningSession: vi.fn(),
    generatePlanningDrafts: vi.fn(),
    getPlanningSession: vi.fn(),
    listPlanningSessions: vi.fn(),
    saveAndExecuteDrafts: vi.fn(),
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
  localStorage.clear();
  vi.stubGlobal("confirm", vi.fn(() => true));

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
      title: "当前会话",
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
      created_at: "2026-04-12T10:00:00",
      updated_at: "2026-04-12T10:00:00",
    },
    messages: [],
    drafts: [],
  });
  vi.mocked(api.listPlanningSessions).mockResolvedValue([
    {
      id: 5,
      title: "当前会话",
      status: "collecting",
      created_at: "2026-04-12T10:00:00",
      updated_at: "2026-04-12T10:00:00",
    },
    {
      id: 9,
      title: "保留会话",
      status: "plan_ready",
      created_at: "2026-04-12T09:00:00",
      updated_at: "2026-04-12T09:30:00",
    },
  ]);
  vi.mocked(api.deletePlanningSession).mockResolvedValue(undefined);
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

  expect(await screen.findByText("AI Planning")).toBeInTheDocument();
  expect(screen.getByText("已收集 0 / 7 项")).toBeInTheDocument();

  await userEvent.type(screen.getByLabelText("测试规划对话输入"), "请先整理后台登录测试方案{enter}");

  expect(await screen.findByText(/list_test_cases/)).toBeInTheDocument();
  expect(screen.getByText("已收集 5 / 7 项")).toBeInTheDocument();
  expect(screen.getByText("商城后台登录测试方案")).toBeInTheDocument();
  expect(screen.getByRole("checkbox", { name: "选择场景 登录成功" })).toBeInTheDocument();
});

test("可以生成草案并展示审阅操作", async () => {
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
    <AITestPlanningPanel aiSettings={aiSettings} projectId={1} caseId={undefined} onImportDraft={vi.fn()} />,
  );

  expect(await screen.findByText("AI Planning")).toBeInTheDocument();
  await userEvent.type(screen.getByLabelText("测试规划对话输入"), "请先整理后台登录测试方案{enter}");

  await userEvent.click(await screen.findByRole("checkbox", { name: "选择场景 登录成功" }));
  await userEvent.click(screen.getByRole("button", { name: "生成选中草案" }));

  await waitFor(() => {
    expect(api.generatePlanningDrafts).toHaveBeenCalledWith(
      5,
      expect.objectContaining({
        scenario_keys: ["login_success"],
      }),
    );
  });
  expect(await screen.findByText("测试用例草案")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "仅保存" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "保存并执行" })).toBeInTheDocument();
});

test("删除当前会话后会切换到剩余会话并更新 localStorage", async () => {
  vi.mocked(api.listPlanningSessions)
    .mockResolvedValueOnce([
      {
        id: 5,
        title: "当前会话",
        status: "collecting",
        created_at: "2026-04-12T10:00:00",
        updated_at: "2026-04-12T10:00:00",
      },
      {
        id: 9,
        title: "保留会话",
        status: "plan_ready",
        created_at: "2026-04-12T09:00:00",
        updated_at: "2026-04-12T09:30:00",
      },
    ])
    .mockResolvedValueOnce([
      {
        id: 9,
        title: "保留会话",
        status: "plan_ready",
        created_at: "2026-04-12T09:00:00",
        updated_at: "2026-04-12T09:30:00",
      },
    ]);

  vi.mocked(api.getPlanningSession).mockImplementation(async (sessionId: number) => ({
    session: {
      id: sessionId,
      actor_user_id: 1,
      project_id: 1,
      case_id: null,
      title: sessionId === 9 ? "保留会话" : "当前会话",
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
      missing_slots: [],
      last_error_message: null,
      created_at: "2026-04-12T10:00:00",
      updated_at: "2026-04-12T10:00:00",
    },
    messages: [],
    drafts: [],
  }));

  renderWithProviders(
    <AITestPlanningPanel aiSettings={aiSettings} projectId={1} caseId={undefined} onImportDraft={vi.fn()} />,
  );

  await waitFor(() => {
    expect(screen.getByRole("button", { name: "删除会话 当前会话" })).toBeInTheDocument();
  });
  await userEvent.click(screen.getByRole("button", { name: "删除会话 当前会话" }));

  await waitFor(() => {
    expect(api.deletePlanningSession).toHaveBeenCalledWith(5);
    expect(api.getPlanningSession).toHaveBeenLastCalledWith(9);
    expect(localStorage.getItem("ai_planning_last_session")).toBe("9");
  });
});

test("缓存的最后会话不存在时会自动创建新会话", async () => {
  localStorage.setItem("ai_planning_last_session", "77");
  vi.mocked(api.getPlanningSession).mockRejectedValueOnce(new Error("AI planning session 77 not found."));

  renderWithProviders(
    <AITestPlanningPanel aiSettings={aiSettings} projectId={1} caseId={undefined} onImportDraft={vi.fn()} />,
  );

  await waitFor(() => {
    expect(api.getPlanningSession).toHaveBeenCalledWith(77);
    expect(api.createPlanningSession).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem("ai_planning_last_session")).toBe("5");
  });
});

test("保存并执行后会重新加载会话详情并展示持久化的执行摘要", async () => {
  vi.mocked(api.createPlanningSession).mockResolvedValue({
    session: {
      id: 5,
      actor_user_id: 1,
      project_id: 1,
      case_id: null,
      title: "当前会话",
      status: "drafts_ready",
      requirements: {
        app_under_test: "商城后台",
        business_goal: "验证管理员登录",
        entry_url_or_page: "https://shop.example.com/login",
        core_user_flow: "输入账号密码并点击登录",
        main_assertions: ["跳转到 dashboard"],
        test_data_or_account: "admin@example.com",
        scope_limits: "不覆盖忘记密码",
      },
      plan: {
        summary: "商城后台登录测试方案",
        assumptions: [],
        risks: [],
        scenarios: [
          {
            scenario_key: "login_success",
            title: "登录成功",
            goal: "验证管理员可以登录后台",
            preconditions: [],
            priority: "high",
            test_data_requirements: [],
            assertions: ["跳转到 dashboard"],
            draft_prompt: "为登录成功场景生成 DSL",
          },
        ],
      },
      missing_slots: [],
      last_error_message: null,
      created_at: "2026-04-13T10:00:00",
      updated_at: "2026-04-13T10:00:00",
    },
    messages: [],
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
        created_at: "2026-04-13T10:00:00",
        updated_at: "2026-04-13T10:00:00",
      },
    ],
  });
  vi.mocked(api.saveAndExecuteDrafts).mockResolvedValue({
    assistant_message: "测试执行完成",
    session_status: "completed",
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
    plan: null,
    drafts: [],
    next_action: "ask_followup",
    saved_cases: [{ case_id: 101, case_name: "登录成功", status: "saved" }],
    execution_summaries: [
      {
        execution_id: 88,
        case_id: 101,
        case_name: "登录成功",
        status: "passed",
        total_steps: 1,
        passed_steps: 1,
        failed_steps: 0,
        duration_ms: 1234,
        screenshot_url: "/artifacts/executions/88/final.png",
        report_url: "/run/88",
      },
    ],
  });
  vi.mocked(api.getPlanningSession).mockResolvedValue({
    session: {
      id: 5,
      actor_user_id: 1,
      project_id: 1,
      case_id: null,
      title: "当前会话",
      status: "completed",
      requirements: {
        app_under_test: "商城后台",
        business_goal: "验证管理员登录",
        entry_url_or_page: "https://shop.example.com/login",
        core_user_flow: "输入账号密码并点击登录",
        main_assertions: ["跳转到 dashboard"],
        test_data_or_account: "admin@example.com",
        scope_limits: "不覆盖忘记密码",
      },
      plan: null,
      missing_slots: [],
      last_error_message: null,
      created_at: "2026-04-13T10:00:00",
      updated_at: "2026-04-13T10:05:00",
    },
    messages: [
      {
        id: 99,
        session_id: 5,
        role: "assistant",
        turn_type: "plan",
        content: "测试执行完成",
        structured_payload: {
          type: "execution_summary",
          execution_summaries: [
            {
              execution_id: 88,
              case_id: 101,
              case_name: "登录成功",
              status: "passed",
              total_steps: 1,
              passed_steps: 1,
              failed_steps: 0,
              duration_ms: 1234,
              screenshot_url: "/artifacts/executions/88/final.png",
              report_url: "/run/88",
            },
          ],
        },
        created_at: "2026-04-13T10:05:00",
      },
    ],
    drafts: [
      {
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
        created_at: "2026-04-13T10:00:00",
        updated_at: "2026-04-13T10:05:00",
      },
    ],
  });

  renderWithProviders(
    <AITestPlanningPanel aiSettings={aiSettings} projectId={1} caseId={undefined} onImportDraft={vi.fn()} />,
  );

  expect(await screen.findByText("AI Planning")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("checkbox", { name: "选择场景 登录成功" }));
  await userEvent.click(screen.getByRole("button", { name: "保存并执行" }));

  await waitFor(() => {
    expect(api.saveAndExecuteDrafts).toHaveBeenCalledWith(5, [11], true);
    expect(api.getPlanningSession).toHaveBeenCalledWith(5);
  });
});
