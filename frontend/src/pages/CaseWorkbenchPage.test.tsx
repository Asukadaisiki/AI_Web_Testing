import { Route } from "react-router-dom";
import { screen, waitFor } from "@testing-library/react";
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
    getCaseDetail: vi.fn(),
    updateCase: vi.fn(),
    validateDslCase: vi.fn(),
  };
});

beforeEach(() => {
  window.localStorage.clear();
  vi.resetAllMocks();
});

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
}, 10000);

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
}, 10000);
