import { Route } from "react-router-dom";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { CasesPage } from "./CasesPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getCases: vi.fn(),
    executeCase: vi.fn(),
  };
});

test("渲染用例列表并支持执行后跳转", async () => {
  vi.mocked(api.getCases).mockResolvedValue([
    {
      id: 1,
      project_id: 1,
      name: "登录冒烟",
      description: "检查登录链路",
      steps: [{ action: "goto", value: "/login" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-09T10:00:00",
      updated_at: "2026-03-09T10:00:00",
    },
  ]);
  vi.mocked(api.executeCase).mockResolvedValue({
    id: 88,
    case_id: 1,
    case_name: "登录冒烟",
    project_id: 1,
    triggered_by: 1,
    status: "passed",
    error_message: null,
    started_at: "2026-03-09T10:00:00",
    finished_at: "2026-03-09T10:00:01",
    report: {
      status: "passed",
      steps: [],
    },
  });

  renderWithProviders(<CasesPage />, {
    route: "/cases",
    path: "/cases",
    extraRoutes: [<Route key="detail" path="/executions/:executionId" element={<div>detail-view</div>} />],
  });

  expect(await screen.findByText("登录冒烟")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: /执\s*行/ }));

  await waitFor(() => {
    expect(api.executeCase).toHaveBeenCalledWith(1, { actor_user_id: 1 });
  });
  expect(await screen.findByText("detail-view")).toBeInTheDocument();
});
