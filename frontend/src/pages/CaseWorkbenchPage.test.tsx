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

test("结构化步骤编辑支持应用模板、切换 JSON 并保存执行", async () => {
  const templateSteps = [
    { action: "goto", value: "/login" },
    { action: "input", target: "用户名输入框", value: "admin" },
    { action: "input", target: "密码输入框", value: "123456" },
    { action: "click", target: "登录按钮" },
    { action: "assert_url_contains", value: "/dashboard" },
  ];

  vi.mocked(api.validateDslCase).mockResolvedValue({
    valid: true,
    case: {
      name: "登录冒烟",
      description: "检查登录链路",
      steps: templateSteps,
    },
    supported_actions: ["goto", "click", "input", "wait_for", "assert_text", "assert_url_contains"],
  });
  vi.mocked(api.createCase).mockResolvedValue({
    id: 3,
    project_id: 1,
    name: "登录冒烟",
    description: "检查登录链路",
    steps: templateSteps,
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-09T10:00:00",
    updated_at: "2026-03-09T10:00:00",
  });
  vi.mocked(api.executeCase).mockResolvedValue({
    id: 55,
    case_id: 3,
    case_name: "登录冒烟",
    project_id: 1,
    triggered_by: 1,
    status: "passed",
    error_message: null,
    started_at: "2026-03-09T10:00:00",
    finished_at: "2026-03-09T10:00:02",
    duration_ms: 2000,
    total_steps: 5,
    failed_step_index: null,
    latest_screenshot_url: "/artifacts/executions/55/step-05.png",
    report: {
      status: "passed",
      steps: [],
    },
  });

  renderWithProviders(<CaseWorkbenchPage />, {
    route: "/cases/new",
    path: "/cases/new",
    extraRoutes: [<Route key="detail" path="/executions/:executionId" element={<div>execution-view</div>} />],
  });

  await userEvent.clear(screen.getByLabelText("用例名称"));
  await userEvent.type(screen.getByLabelText("用例名称"), "登录冒烟");
  await userEvent.clear(screen.getByLabelText("描述"));
  await userEvent.type(screen.getByLabelText("描述"), "检查登录链路");

  await userEvent.click(screen.getByRole("button", { name: "应用模板" }));
  expect(screen.getByText("Step 5")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "原始 JSON" }));
  await waitFor(() => {
    const textboxes = screen.getAllByRole("textbox");
    const jsonEditor = textboxes[textboxes.length - 1] as HTMLTextAreaElement;
    expect(jsonEditor.value).toContain('"action": "goto"');
    expect(jsonEditor.value).toContain('"assert_url_contains"');
    expect(jsonEditor.value).toContain('"/dashboard"');
  });

  await userEvent.click(screen.getByRole("button", { name: "结构化编辑" }));
  expect(await screen.findByText("Step 5")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "校验 DSL" }));
  expect(await screen.findByText("DSL 校验通过")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "保存并执行" }));

  await waitFor(() => {
    expect(api.validateDslCase).toHaveBeenCalled();
    expect(api.createCase).toHaveBeenCalledWith({
      project_id: 1,
      actor_user_id: 1,
      name: "登录冒烟",
      description: "检查登录链路",
      steps: templateSteps,
    });
    expect(api.executeCase).toHaveBeenCalledWith(3, { actor_user_id: 1 });
  });

  expect(await screen.findByText("execution-view")).toBeInTheDocument();
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
