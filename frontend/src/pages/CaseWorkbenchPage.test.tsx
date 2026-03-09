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

test("支持 DSL 校验、保存并执行后跳转", async () => {
  vi.mocked(api.validateDslCase).mockResolvedValue({
    valid: true,
    case: {
      name: "登录冒烟",
      description: "检查登录链路",
      steps: [{ action: "goto", value: "/login" }],
    },
    supported_actions: ["goto", "click", "input"],
  });
  vi.mocked(api.createCase).mockResolvedValue({
    id: 3,
    project_id: 1,
    name: "登录冒烟",
    description: "检查登录链路",
    steps: [{ action: "goto", value: "/login" }],
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
      steps: [{ action: "goto", value: "/login" }],
    });
    expect(api.executeCase).toHaveBeenCalledWith(3, { actor_user_id: 1 });
  });

  expect(await screen.findByText("execution-view")).toBeInTheDocument();
});
