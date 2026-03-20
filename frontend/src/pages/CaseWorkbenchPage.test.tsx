import { Route } from "react-router-dom";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { CaseWorkbenchPage } from "./CaseWorkbenchPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    createCase: vi.fn(),
    executeCase: vi.fn(),
    generateDslCase: vi.fn(),
    getAISettings: vi.fn(),
    getCaseDetail: vi.fn(),
    recordDslGenerationFeedback: vi.fn(),
    updateCase: vi.fn(),
    validateDslCase: vi.fn(),
  };
});

beforeEach(() => {
  window.localStorage.clear();
  vi.resetAllMocks();
  vi.mocked(api.getAISettings).mockResolvedValue({
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
  });
  vi.mocked(api.recordDslGenerationFeedback).mockResolvedValue({
    id: 999,
    created_at: "2026-03-17T00:00:00",
    success: true,
    model_name: "gpt-4o-mini",
    generation_mode: "draft",
    import_mode: "replace",
    prompt_variant: "baseline_draft",
    project_id: 1,
    case_id: null,
    prompt_version: "2026-03-18.single-pass-variant-v1",
    error_type: null,
    error_message: null,
    repaired_invalid_actions: 0,
    removed_invalid_steps: 0,
    removed_invalid_contracts: 0,
    warnings_count: 0,
    normalization_notes_count: 0,
    prompt_preview: "seed",
    risk_flags: [],
    feedback_status: "accepted",
    feedback_import_mode: "replace",
    rejection_reason_code: null,
    feedback_recorded_at: "2026-03-17T00:01:00",
  });
});

test("放弃草案后可按拒绝原因重试生成，并携带 retry context", async () => {
  vi.mocked(api.generateDslCase)
    .mockResolvedValueOnce({
      generation_id: 18,
      case: {
        name: "首次草案",
        description: null,
        base_url: null,
        input_contract: [],
        output_contract: [],
        steps: [{ action: "goto", value: "/first-pass" }],
      },
      supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
      warnings: [],
      normalization_notes: [],
      generation_meta: {
        model: "gpt-4o-mini",
        generation_mode: "draft",
        import_mode: "replace",
        prompt_variant: "baseline_draft",
        context_profile: "blank_request",
        risk_flags: [],
        base_url_source: "none",
        base_url_backfilled: false,
        repaired_invalid_actions: 0,
        removed_invalid_steps: 0,
        removed_invalid_contracts: 0,
        preserve_contracts_applied: false,
        used_current_case_context: false,
        used_current_steps_context: false,
      },
    })
    .mockResolvedValueOnce({
      generation_id: 19,
      case: {
        name: "重试草案",
        description: null,
        base_url: null,
        input_contract: [],
        output_contract: [],
        steps: [{ action: "goto", value: "/retry-pass" }],
      },
      supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
      warnings: [],
      normalization_notes: [],
      generation_meta: {
        model: "gpt-4o-mini",
        generation_mode: "draft",
        import_mode: "replace",
        prompt_variant: "baseline_draft",
        context_profile: "blank_request",
        risk_flags: [],
        base_url_source: "none",
        base_url_backfilled: false,
        repaired_invalid_actions: 0,
        removed_invalid_steps: 0,
        removed_invalid_contracts: 0,
        preserve_contracts_applied: false,
        used_current_case_context: false,
        used_current_steps_context: false,
      },
    });
  vi.mocked(api.recordDslGenerationFeedback).mockResolvedValueOnce({
    id: 18,
    created_at: "2026-03-17T00:00:00",
    success: true,
    model_name: "gpt-4o-mini",
    generation_mode: "draft",
    import_mode: "replace",
    prompt_variant: "baseline_draft",
    project_id: 1,
    case_id: null,
    prompt_version: "2026-03-19.retry-strategy-v1+retry.bad_contracts",
    error_type: null,
    error_message: null,
    repaired_invalid_actions: 0,
    removed_invalid_steps: 0,
    removed_invalid_contracts: 0,
    warnings_count: 0,
    normalization_notes_count: 0,
    prompt_preview: "首次草案",
    risk_flags: [],
    feedback_status: "rejected",
    feedback_import_mode: null,
    rejection_reason_code: "bad_contracts",
    feedback_recorded_at: "2026-03-17T00:01:00",
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.type(screen.getByLabelText("自然语言需求"), "生成一个待重试草案");
  await userEvent.click(screen.getByRole("button", { name: "生成 DSL" }));
  expect(await screen.findByText("生成预览")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("combobox", { name: "放弃原因" }));
  await userEvent.click(await screen.findByText("bad_contracts", { selector: ".ant-select-item-option-content" }));
  await userEvent.type(screen.getByLabelText("放弃备注"), "合同命名需要更稳定");
  await userEvent.click(screen.getByRole("button", { name: "放弃草案" }));

  await waitFor(() => {
    expect(api.recordDslGenerationFeedback).toHaveBeenCalledWith(18, {
      actor_user_id: 1,
      feedback_status: "rejected",
      feedback_import_mode: null,
      rejection_reason_code: "bad_contracts",
      feedback_note: "合同命名需要更稳定",
    });
  });

  expect(await screen.findByRole("button", { name: "按拒绝原因重试生成" })).toBeInTheDocument();
  expect(screen.getByLabelText("放弃备注")).toHaveValue("合同命名需要更稳定");

  await userEvent.clear(screen.getByLabelText("自然语言需求"));
  await userEvent.type(screen.getByLabelText("自然语言需求"), "按拒绝原因重新生成");
  await userEvent.click(screen.getByRole("button", { name: "按拒绝原因重试生成" }));

  await waitFor(() => {
    expect(api.generateDslCase).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        prompt: "按拒绝原因重新生成",
        retry_from_generation_id: 18,
        retry_reason_code: "bad_contracts",
        retry_note: "合同命名需要更稳定",
      }),
    );
  });
}, 15000);

test("结构化步骤编辑支持应用模板、切换 JSON 并保存执行", async () => {
  const templateSteps = [
    { action: "goto", value: "/" },
    { action: "assert_url_contains", value: "example.com" },
  ];

  vi.mocked(api.validateDslCase).mockResolvedValue({
    valid: true,
    case: {
      name: "公共冒烟",
      description: "验证公共站点可访问",
      base_url: "https://example.com",
      input_contract: [],
      output_contract: [],
      steps: templateSteps,
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
  });
  vi.mocked(api.createCase).mockResolvedValue({
    id: 3,
    project_id: 1,
    name: "公共冒烟",
    description: "验证公共站点可访问",
    base_url: "https://example.com",
    input_contract: [],
    output_contract: [],
    steps: templateSteps,
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-09T10:00:00",
    updated_at: "2026-03-09T10:00:00",
  });
  vi.mocked(api.executeCase).mockResolvedValue({
    id: 55,
    case_id: 3,
    case_name: "公共冒烟",
    project_id: 1,
    triggered_by: 1,
    status: "passed",
    error_message: null,
    started_at: "2026-03-09T10:00:00",
    finished_at: "2026-03-09T10:00:02",
    duration_ms: 2000,
    total_steps: 2,
    failed_step_index: null,
    failure_category: null,
    failure_step_action: null,
    latest_url: null,
    latest_screenshot_url: "/artifacts/executions/55/step-02.png",
    report: {
      status: "passed",
      steps: [],
    },
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
    extraRoutes: [
      <Route key="detail" path="/executions/:executionId" element={<div>execution-view</div>} />,
      <Route key="cases" path="/cases" element={<div>cases-view</div>} />,
    ],
  });

  expect(screen.getByRole("button", { name: "返回用例列表" })).toBeInTheDocument();

  await userEvent.clear(screen.getByLabelText("用例名称"));
  await userEvent.type(screen.getByLabelText("用例名称"), "公共冒烟");
  await userEvent.clear(screen.getByLabelText("描述"));
  await userEvent.type(screen.getByLabelText("描述"), "验证公共站点可访问");

  await userEvent.click(screen.getByRole("button", { name: "应用模板" }));
  expect(screen.getByDisplayValue("https://example.com")).toBeInTheDocument();
  expect(screen.getByText("Step 2")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "原始 JSON" }));
  await waitFor(() => {
    const textboxes = screen.getAllByRole("textbox");
    const jsonEditor = textboxes[textboxes.length - 1] as HTMLTextAreaElement;
    expect(jsonEditor.value).toContain('"action": "goto"');
    expect(jsonEditor.value).toContain('"assert_url_contains"');
    expect(jsonEditor.value).toContain('"example.com"');
  });

  await userEvent.click(screen.getByRole("button", { name: "结构化编辑" }));
  expect(await screen.findByText("Step 2")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "校验 DSL" }));
  expect(await screen.findByText("DSL 校验通过")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "保存并执行" }));

  await waitFor(() => {
    expect(api.validateDslCase).toHaveBeenCalled();
    expect(api.createCase).toHaveBeenCalledWith({
      project_id: 1,
      actor_user_id: 1,
      name: "公共冒烟",
      description: "验证公共站点可访问",
      base_url: "https://example.com",
      input_contract: [],
      output_contract: [],
      steps: templateSteps,
    });
    expect(api.executeCase).toHaveBeenCalledWith(3, { actor_user_id: 1 });
  });

  expect(await screen.findByText("execution-view")).toBeInTheDocument();
}, 20000);

test("新建页可自动恢复本地草稿并在保存后清除", async () => {
  window.localStorage.setItem(
    "case-draft:new",
    JSON.stringify({
      name: "草稿用例",
      description: "草稿描述",
      project_id: 1,
      base_url: "https://example.com",
      inputContracts: [],
      outputContracts: [],
      editorMode: "structured",
      structuredSteps: [{ action: "goto", value: "/" }],
      stepsJson: JSON.stringify([{ action: "goto", value: "/" }], null, 2),
    }),
  );

  vi.mocked(api.validateDslCase).mockResolvedValue({
    valid: true,
    case: {
      name: "草稿用例",
      description: "草稿描述",
      base_url: "https://example.com",
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/" }],
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
  });
  vi.mocked(api.createCase).mockResolvedValue({
    id: 7,
    project_id: 1,
    name: "草稿用例",
    description: "草稿描述",
    base_url: "https://example.com",
    input_contract: [],
    output_contract: [],
    steps: [{ action: "goto", value: "/" }],
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-10T10:00:00",
    updated_at: "2026-03-10T10:00:00",
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
    extraRoutes: [<Route key="edit" path="/cases/:caseId/edit" element={<div>edit-view</div>} />],
  });

  expect(await screen.findByDisplayValue("草稿用例")).toBeInTheDocument();
  expect(screen.getByDisplayValue("https://example.com")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: /^保\s*存$/ }));

  await waitFor(() => {
    expect(window.localStorage.getItem("case-draft:new")).toBeNull();
  });
  expect(await screen.findByText("edit-view")).toBeInTheDocument();
});

test("编辑页检测到本地草稿时可恢复", async () => {
  window.localStorage.setItem(
    "case-draft:9",
    JSON.stringify({
      name: "本地草稿",
      description: "草稿版本",
      project_id: 1,
      base_url: "https://draft.example.com",
      inputContracts: [],
      outputContracts: [],
      editorMode: "structured",
      structuredSteps: [{ action: "goto", value: "/draft" }],
      stepsJson: JSON.stringify([{ action: "goto", value: "/draft" }], null, 2),
    }),
  );

  vi.mocked(api.getCaseDetail).mockResolvedValue({
    id: 9,
    project_id: 1,
    name: "服务端版本",
    description: "服务器描述",
    base_url: "https://server.example.com",
    input_contract: [],
    output_contract: [],
    steps: [{ action: "goto", value: "/server" }],
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-10T10:00:00",
    updated_at: "2026-03-10T10:00:00",
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/9/edit",
    path: "/cases/:caseId/edit",
  });

  expect(await screen.findByText("检测到本地草稿")).toBeInTheDocument();
  expect(screen.getByDisplayValue("服务端版本")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "恢复草稿" }));
  expect(await screen.findByDisplayValue("本地草稿")).toBeInTheDocument();
});

test("编辑页支持丢弃本地草稿", async () => {
  window.localStorage.setItem(
    "case-draft:9",
    JSON.stringify({
      name: "另一个草稿",
      description: "待丢弃",
      project_id: 1,
      base_url: "https://discard.example.com",
      inputContracts: [],
      outputContracts: [],
      editorMode: "structured",
      structuredSteps: [{ action: "goto", value: "/discard" }],
      stepsJson: JSON.stringify([{ action: "goto", value: "/discard" }], null, 2),
    }),
  );

  vi.mocked(api.getCaseDetail).mockResolvedValue({
    id: 9,
    project_id: 1,
    name: "服务端版本",
    description: "服务器描述",
    base_url: "https://server.example.com",
    input_contract: [],
    output_contract: [],
    steps: [{ action: "goto", value: "/server" }],
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-10T10:00:00",
    updated_at: "2026-03-10T10:00:00",
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/9/edit",
    path: "/cases/:caseId/edit",
  });

  expect(await screen.findByText("检测到本地草稿")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "丢弃草稿" }));

  await waitFor(() => {
    expect(window.localStorage.getItem("case-draft:9")).toBeNull();
  });
});

test("结构化步骤编辑支持新增、移动和删除", async () => {
  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.click(screen.getByRole("button", { name: "新增步骤" }));
  expect(screen.getByText("Step 2")).toBeInTheDocument();

  await userEvent.click(screen.getAllByRole("button", { name: /下\s*移/ })[0]);
  const stepTitles = screen.getAllByText(/Step \d/).map((node) => node.textContent);
  expect(stepTitles).toContain("Step 1");
  expect(stepTitles).toContain("Step 2");

  await userEvent.click(screen.getAllByRole("button", { name: /删\s*除/ })[1]);
  expect(screen.queryByText("Step 2")).not.toBeInTheDocument();
});

test("工作台可编辑输入输出契约并随保存一起提交", async () => {
  vi.mocked(api.validateDslCase).mockImplementation(async (payload) => ({
    valid: true,
    case: payload,
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
  }));
  vi.mocked(api.createCase).mockResolvedValue({
    id: 15,
    project_id: 1,
    name: "上下文用例",
    description: null,
    base_url: "https://example.com",
    input_contract: [
      {
        name: "sessionToken",
        context_key: "session_token",
        value_type: "string",
        required: true,
        description: null,
      },
    ],
    output_contract: [
      {
        name: "latestPage",
        context_key: "latest_page",
        value_type: "string",
        source: "latest_url",
        description: null,
      },
    ],
    steps: [{ action: "goto", value: "/" }],
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-14T10:00:00",
    updated_at: "2026-03-14T10:00:00",
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
    extraRoutes: [<Route key="edit" path="/cases/:caseId/edit" element={<div>edit-view</div>} />],
  });

  await userEvent.type(screen.getByLabelText("用例名称"), "上下文用例");
  await userEvent.clear(screen.getByLabelText("用例 Base URL"));
  await userEvent.type(screen.getByLabelText("用例 Base URL"), "https://example.com");

  await userEvent.click(screen.getByRole("button", { name: "新增输入契约" }));
  const sessionTokenFields = screen.getAllByDisplayValue("contextVar");
  await userEvent.clear(sessionTokenFields[0]);
  await userEvent.type(sessionTokenFields[0], "sessionToken");
  const sessionKeyFields = screen.getAllByDisplayValue("context_var");
  await userEvent.clear(sessionKeyFields[0]);
  await userEvent.type(sessionKeyFields[0], "session_token");

  await userEvent.click(screen.getByRole("button", { name: "新增输出契约" }));
  const resultNameFields = screen.getAllByDisplayValue("resultVar");
  await userEvent.clear(resultNameFields[0]);
  await userEvent.type(resultNameFields[0], "latestPage");
  const resultKeyFields = screen.getAllByDisplayValue("result_var");
  await userEvent.clear(resultKeyFields[0]);
  await userEvent.type(resultKeyFields[0], "latest_page");

  await userEvent.click(screen.getByRole("button", { name: /^保\s*存$/ }));

  await waitFor(() => {
    expect(api.createCase).toHaveBeenCalledWith({
      project_id: 1,
      actor_user_id: 1,
      name: "上下文用例",
      description: null,
      base_url: "https://example.com",
      input_contract: [
        {
          name: "sessionToken",
          context_key: "session_token",
          value_type: "string",
          required: true,
          description: null,
        },
      ],
      output_contract: [
        {
          name: "latestPage",
          context_key: "latest_page",
          value_type: "string",
          source: "latest_url",
          description: null,
        },
      ],
      steps: [{ action: "goto", value: "/" }],
    });
  });
}, 20000);

test("输出契约 source 为空时显示占位符，选择后再随保存提交", async () => {
  window.localStorage.setItem(
    "case-draft:new",
    JSON.stringify({
      name: "空来源用例",
      description: null,
      project_id: 1,
      base_url: "https://example.com",
      inputContracts: [],
      outputContracts: [
        {
          name: "latestPage",
          context_key: "latest_page",
          value_type: "string",
          source: null,
          description: null,
        },
      ],
      editorMode: "structured",
      structuredSteps: [{ action: "goto", value: "/" }],
      stepsJson: JSON.stringify([{ action: "goto", value: "/" }], null, 2),
    }),
  );

  vi.mocked(api.validateDslCase).mockImplementation(async (payload) => ({
    valid: true,
    case: payload,
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
  }));
  vi.mocked(api.createCase).mockResolvedValue({
    id: 16,
    project_id: 1,
    name: "空来源用例",
    description: null,
    base_url: "https://example.com",
    input_contract: [],
    output_contract: [
      {
        name: "latestPage",
        context_key: "latest_page",
        value_type: "string",
        source: "latest_url",
        description: null,
      },
    ],
    steps: [{ action: "goto", value: "/" }],
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-14T10:00:00",
    updated_at: "2026-03-14T10:00:00",
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
    extraRoutes: [<Route key="edit" path="/cases/:caseId/edit" element={<div>edit-view</div>} />],
  });

  expect(await screen.findByDisplayValue("空来源用例")).toBeInTheDocument();
  const outputCard = screen.getByText("输出 1").closest(".ant-card");
  expect(outputCard).not.toBeNull();
  expect(within(outputCard as HTMLElement).getByText("请选择")).toBeInTheDocument();

  const outputSelects = (outputCard as HTMLElement).querySelectorAll(".ant-select");
  expect(outputSelects).toHaveLength(2);
  const sourceSelect = outputSelects[1]?.querySelector(".ant-select-selector");
  expect(sourceSelect).not.toBeNull();
  fireEvent.mouseDown(sourceSelect as HTMLElement);
  await userEvent.click(await screen.findByText("latest_url", { selector: ".ant-select-item-option-content" }));
  await waitFor(() => {
    expect(window.localStorage.getItem("case-draft:new")).toContain('"source":"latest_url"');
  });

  await userEvent.click(screen.getByRole("button", { name: /^保\s*存$/ }));

  await waitFor(() => {
    expect(api.createCase).toHaveBeenCalledWith({
      project_id: 1,
      actor_user_id: 1,
      name: "空来源用例",
      description: null,
      base_url: "https://example.com",
      input_contract: [],
      output_contract: [
        {
          name: "latestPage",
          context_key: "latest_page",
          value_type: "string",
          source: "latest_url",
          description: null,
        },
      ],
      steps: [{ action: "goto", value: "/" }],
    });
  });
}, 10000);

test("自然语言生成支持预览、仅导入步骤和替换当前 DSL", async () => {
  vi.mocked(api.generateDslCase).mockResolvedValue({
    generation_id: 11,
    case: {
      name: "AI 生成冒烟",
      description: "验证首页可访问",
      base_url: "https://example.com",
      input_contract: [],
      output_contract: [],
      steps: [
        { action: "goto", value: "/" },
        { action: "assert_url_contains", value: "example.com" },
      ],
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    warnings: ["AI 草案未提供 base_url，已回填请求中的 Base URL。"],
    normalization_notes: ["步骤 #1 的 action 已从 open 自动修正为 goto。"],
    generation_meta: {
      model: "gpt-4o-mini",
      generation_mode: "draft",
      import_mode: "replace",
      prompt_variant: "baseline_draft",
      context_profile: "blank_request",
      risk_flags: ["base_url_backfilled", "invalid_actions_repaired"],
      base_url_source: "request",
      base_url_backfilled: true,
      repaired_invalid_actions: 1,
      removed_invalid_steps: 0,
      removed_invalid_contracts: 0,
      preserve_contracts_applied: false,
      used_current_case_context: false,
      used_current_steps_context: false,
    },
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.type(screen.getByLabelText("用例名称"), "现有名称");
  await userEvent.type(screen.getByLabelText("自然语言需求"), "打开 example.com 并验证 URL 包含 example.com");

  await userEvent.click(screen.getByRole("button", { name: "生成 DSL" }));

  expect(await screen.findByText(/当前生成配置：已启用，模型 gpt-4o-mini/)).toBeInTheDocument();
  expect(await screen.findByText("生成预览")).toBeInTheDocument();
  expect(screen.getByDisplayValue(/"name": "AI 生成冒烟"/)).toBeInTheDocument();
  expect(screen.getByText("AI 草案未提供 base_url，已回填请求中的 Base URL。")).toBeInTheDocument();
  expect(screen.getByText("步骤 #1 的 action 已从 open 自动修正为 goto。")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "仅导入步骤" }));
  expect(await screen.findByText("Step 2")).toBeInTheDocument();
  expect(screen.getByDisplayValue("现有名称")).toBeInTheDocument();
  await waitFor(() => {
    expect(api.recordDslGenerationFeedback).toHaveBeenCalledWith(11, {
      actor_user_id: 1,
      feedback_status: "accepted",
      feedback_import_mode: "steps_only",
      rejection_reason_code: null,
      feedback_note: null,
    });
  });

  expect(screen.getByText("草案反馈已记录")).toBeInTheDocument();
}, 15000);

test("自然语言生成会按上下文来源与策略带上请求 payload", async () => {
  vi.mocked(api.generateDslCase).mockResolvedValue({
    generation_id: 12,
    case: {
      name: "AI 重写结果",
      description: "基于当前 DSL 重写",
      base_url: "https://example.com",
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/dashboard" }],
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    warnings: [],
    normalization_notes: [],
    generation_meta: {
      model: "gpt-4o-mini",
      generation_mode: "strict_steps_only",
      import_mode: "steps_only",
      prompt_variant: "repair_steps",
      context_profile: "repair_steps",
      risk_flags: [],
      base_url_source: "ai_output",
      base_url_backfilled: false,
      repaired_invalid_actions: 0,
      removed_invalid_steps: 0,
      removed_invalid_contracts: 0,
      preserve_contracts_applied: false,
      used_current_case_context: true,
      used_current_steps_context: true,
    },
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.type(screen.getByLabelText("用例名称"), "当前用例");
  await userEvent.type(screen.getByLabelText("描述"), "当前描述");
  await userEvent.clear(screen.getByLabelText("用例 Base URL"));
  await userEvent.type(screen.getByLabelText("用例 Base URL"), "https://example.com");
  await userEvent.click(screen.getByRole("button", { name: "新增输入契约" }));
  await userEvent.type(screen.getByLabelText("自然语言需求"), "把当前用例改写成 dashboard 冒烟");

  await userEvent.click(screen.getByRole("button", { name: "生成 DSL" }));

  await waitFor(() => {
    expect(api.generateDslCase).toHaveBeenCalledWith({
      prompt: "把当前用例改写成 dashboard 冒烟",
      base_url: "https://example.com",
      actor_user_id: 1,
      project_id: 1,
      case_id: null,
      generation_mode: "draft",
      import_mode: "replace",
      current_case: null,
      current_steps: null,
      current_input_contract: [
        {
          name: "contextVar",
          context_key: "context_var",
          value_type: "string",
          required: true,
          description: null,
        },
      ],
      current_output_contract: [],
      retry_from_generation_id: null,
      retry_reason_code: null,
      retry_note: null,
      preserve_contracts: true,
    });
  });
}, 15000);

test("自然语言生成默认模式会跟随 AI strict setting", async () => {
  vi.mocked(api.getAISettings).mockResolvedValueOnce({
    enable_ai_dsl_generate: true,
    ai_dsl_timeout_ms: 15000,
    ai_dsl_base_url: "https://api.openai.com/v1",
    ai_dsl_model: "gpt-4o-mini",
    ai_dsl_strict_mode: true,
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
  });
  vi.mocked(api.generateDslCase).mockResolvedValue({
    generation_id: 13,
    case: {
      name: "严格模式结果",
      description: null,
      base_url: null,
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/strict" }],
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    warnings: [],
    normalization_notes: [],
    generation_meta: {
      model: "gpt-4o-mini",
      generation_mode: "strict_steps_only",
      import_mode: "replace",
      prompt_variant: "baseline_draft",
      context_profile: "blank_request",
      risk_flags: [],
      base_url_source: "none",
      base_url_backfilled: false,
      repaired_invalid_actions: 0,
      removed_invalid_steps: 0,
      removed_invalid_contracts: 0,
      preserve_contracts_applied: false,
      used_current_case_context: false,
      used_current_steps_context: false,
    },
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.type(screen.getByLabelText("自然语言需求"), "生成严格模式 DSL");
  await userEvent.click(screen.getByRole("button", { name: "生成 DSL" }));

  await waitFor(() => {
    expect(api.generateDslCase).toHaveBeenCalledWith(
      expect.objectContaining({
        generation_mode: "strict_steps_only",
      }),
    );
  });
});

test("仅合并契约不会覆盖现有契约", async () => {
  vi.mocked(api.generateDslCase).mockResolvedValue({
    generation_id: 14,
    case: {
      name: "契约合并结果",
      description: null,
      base_url: null,
      input_contract: [],
      output_contract: [
        {
          name: "latestPage",
          context_key: "latest_page",
          value_type: "string",
          source: "latest_url",
          description: null,
        },
      ],
      steps: [{ action: "goto", value: "/merge" }],
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    warnings: [],
    normalization_notes: [],
    generation_meta: {
      model: "gpt-4o-mini",
      generation_mode: "draft",
      import_mode: "contracts_only",
      prompt_variant: "contracts_focus",
      context_profile: "contracts_focus",
      risk_flags: [],
      base_url_source: "none",
      base_url_backfilled: false,
      repaired_invalid_actions: 0,
      removed_invalid_steps: 0,
      removed_invalid_contracts: 0,
      preserve_contracts_applied: false,
      used_current_case_context: false,
      used_current_steps_context: false,
    },
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.click(screen.getByRole("button", { name: "新增输入契约" }));
  await userEvent.type(screen.getByLabelText("自然语言需求"), "补充输出契约");
  await userEvent.click(screen.getByRole("button", { name: "生成 DSL" }));
  await userEvent.click(await screen.findByRole("button", { name: "仅合并契约" }));

  expect(screen.getByDisplayValue("contextVar")).toBeInTheDocument();
  expect(screen.getByDisplayValue("latestPage")).toBeInTheDocument();
});

test("放弃草案会记录 rejected 反馈并清空预览", async () => {
  vi.mocked(api.generateDslCase).mockResolvedValue({
    generation_id: 15,
    case: {
      name: "待放弃草案",
      description: null,
      base_url: null,
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/discard" }],
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    warnings: [],
    normalization_notes: [],
    generation_meta: {
      model: "gpt-4o-mini",
      generation_mode: "draft",
      import_mode: "replace",
      prompt_variant: "baseline_draft",
      context_profile: "blank_request",
      risk_flags: [],
      base_url_source: "none",
      base_url_backfilled: false,
      repaired_invalid_actions: 0,
      removed_invalid_steps: 0,
      removed_invalid_contracts: 0,
      preserve_contracts_applied: false,
      used_current_case_context: false,
      used_current_steps_context: false,
    },
  });
  vi.mocked(api.recordDslGenerationFeedback).mockResolvedValueOnce({
    id: 15,
    created_at: "2026-03-17T00:00:00",
    success: true,
    model_name: "gpt-4o-mini",
    generation_mode: "draft",
    import_mode: "replace",
    prompt_variant: "baseline_draft",
    project_id: 1,
    case_id: null,
    prompt_version: "2026-03-18.single-pass-variant-v1",
    error_type: null,
    error_message: null,
    repaired_invalid_actions: 0,
    removed_invalid_steps: 0,
    removed_invalid_contracts: 0,
    warnings_count: 0,
    normalization_notes_count: 0,
    prompt_preview: "待放弃草案",
    risk_flags: [],
    feedback_status: "rejected",
    feedback_import_mode: null,
    rejection_reason_code: "bad_contracts",
    feedback_recorded_at: "2026-03-17T00:01:00",
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.type(screen.getByLabelText("自然语言需求"), "生成一个待放弃草案");
  await userEvent.click(screen.getByRole("button", { name: "生成 DSL" }));
  expect(await screen.findByText("生成预览")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("combobox", { name: "放弃原因" }));
  await userEvent.click(await screen.findByText("bad_contracts", { selector: ".ant-select-item-option-content" }));
  await userEvent.type(screen.getByLabelText("放弃备注"), "契约不符合预期");

  await userEvent.click(screen.getByRole("button", { name: "放弃草案" }));

  await waitFor(() => {
    expect(api.recordDslGenerationFeedback).toHaveBeenCalledWith(15, {
      actor_user_id: 1,
      feedback_status: "rejected",
      feedback_import_mode: null,
      rejection_reason_code: "bad_contracts",
      feedback_note: "契约不符合预期",
    });
  });
  await waitFor(() => {
    expect(screen.queryByText("生成预览")).not.toBeInTheDocument();
  });
}, 15000);

test("放弃草案前必须先选择结构化原因", async () => {
  vi.mocked(api.generateDslCase).mockResolvedValue({
    generation_id: 17,
    case: {
      name: "待选择原因草案",
      description: null,
      base_url: null,
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/reject" }],
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    warnings: [],
    normalization_notes: [],
    generation_meta: {
      model: "gpt-4o-mini",
      generation_mode: "draft",
      import_mode: "replace",
      prompt_variant: "baseline_draft",
      context_profile: "blank_request",
      risk_flags: [],
      base_url_source: "none",
      base_url_backfilled: false,
      repaired_invalid_actions: 0,
      removed_invalid_steps: 0,
      removed_invalid_contracts: 0,
      preserve_contracts_applied: false,
      used_current_case_context: false,
      used_current_steps_context: false,
    },
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.type(screen.getByLabelText("自然语言需求"), "生成一个待放弃草案");
  await userEvent.click(screen.getByRole("button", { name: "生成 DSL" }));
  expect(await screen.findByText("生成预览")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "放弃草案" }));

  expect((await screen.findAllByText("请先选择放弃原因。")).length).toBeGreaterThan(0);
  expect(api.recordDslGenerationFeedback).not.toHaveBeenCalled();
}, 15000);

test("采纳反馈失败时保留草案预览并提示可重试", async () => {
  vi.mocked(api.generateDslCase).mockResolvedValue({
    generation_id: 16,
    case: {
      name: "反馈失败草案",
      description: null,
      base_url: null,
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/retry" }],
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
    warnings: [],
    normalization_notes: [],
    generation_meta: {
      model: "gpt-4o-mini",
      generation_mode: "draft",
      import_mode: "replace",
      prompt_variant: "baseline_draft",
      context_profile: "blank_request",
      risk_flags: [],
      base_url_source: "none",
      base_url_backfilled: false,
      repaired_invalid_actions: 0,
      removed_invalid_steps: 0,
      removed_invalid_contracts: 0,
      preserve_contracts_applied: false,
      used_current_case_context: false,
      used_current_steps_context: false,
    },
  });
  vi.mocked(api.recordDslGenerationFeedback).mockRejectedValueOnce(new Error("409 Conflict"));

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.type(screen.getByLabelText("用例名称"), "保留编辑态");
  await userEvent.type(screen.getByLabelText("自然语言需求"), "生成一个反馈失败草案");
  await userEvent.click(screen.getByRole("button", { name: "生成 DSL" }));
  expect(await screen.findByText("生成预览")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "仅导入步骤" }));

  expect(await screen.findByText("反馈记录失败")).toBeInTheDocument();
  expect(screen.getByText("反馈未记录，可重试：409 Conflict")).toBeInTheDocument();
  expect(screen.getByText("Step 1")).toBeInTheDocument();
  expect(screen.getByDisplayValue("保留编辑态")).toBeInTheDocument();
  expect(screen.getByText("生成预览")).toBeInTheDocument();
}, 15000);

test("自然语言生成失败时不污染当前编辑内容", async () => {
  vi.mocked(api.generateDslCase).mockRejectedValue(new Error("AI DSL 生成配置不完整。"));

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
  });

  await userEvent.type(screen.getByLabelText("用例名称"), "保留原始名称");
  await userEvent.type(screen.getByLabelText("自然语言需求"), "生成一个登录冒烟测试");

  await userEvent.click(screen.getByRole("button", { name: "生成 DSL" }));

  expect(await screen.findByText("不可导入错误")).toBeInTheDocument();
  expect(screen.getAllByText("AI DSL 生成配置不完整。").length).toBeGreaterThan(0);
  expect(screen.getByDisplayValue("保留原始名称")).toBeInTheDocument();
  expect(screen.queryByText("生成预览")).not.toBeInTheDocument();
});
