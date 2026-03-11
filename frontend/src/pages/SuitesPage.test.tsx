import { Route } from "react-router-dom";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { SuitesPage } from "./SuitesPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getSuites: vi.fn(),
    executeSuite: vi.fn(),
  };
});

test("渲染 suite 列表并展示执行摘要链接", async () => {
  vi.mocked(api.getSuites).mockResolvedValue([
    {
      id: 1,
      project_id: 1,
      name: "登录回归套件",
      description: "包含登录与退出",
      case_count: 2,
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-11T20:00:00",
      updated_at: "2026-03-11T20:00:00",
    },
  ]);
  vi.mocked(api.executeSuite).mockResolvedValue({
    suite_id: 1,
    suite_name: "登录回归套件",
    started_at: "2026-03-11T20:01:00",
    finished_at: "2026-03-11T20:01:03",
    total_cases: 2,
    passed_cases: 1,
    failed_cases: 1,
    status: "failed",
    executions: [
      { execution_id: 11, case_id: 1, case_name: "登录用例", status: "failed" },
      { execution_id: 12, case_id: 2, case_name: "退出用例", status: "passed" },
    ],
  });

  renderWithProviders(<SuitesPage />, {
    route: "/suites",
    path: "/suites",
    extraRoutes: [
      <Route key="execution-detail" path="/executions/:executionId" element={<div>execution-detail</div>} />,
    ],
  });

  expect(await screen.findByText("登录回归套件")).toBeInTheDocument();
  const suiteRow = screen.getByText("登录回归套件").closest("tr");
  expect(suiteRow).not.toBeNull();

  await userEvent.click(within(suiteRow as HTMLElement).getByRole("button", { name: /执\s*行/ }));

  await waitFor(() => {
    expect(api.executeSuite).toHaveBeenCalledWith(1, { actor_user_id: 1 });
  });
  expect(await screen.findByText("Suite 执行完成：登录回归套件")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "登录用例 (failed)" })).toHaveAttribute("href", "/executions/11");
});
