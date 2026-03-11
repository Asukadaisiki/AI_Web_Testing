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

test("渲染 suite 列表并展示最近批次入口", async () => {
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
      latest_run: {
        id: 7,
        suite_id: 1,
        suite_name: "登录回归套件",
        triggered_by: 1,
        source: "manual",
        source_suite_run_id: null,
        status: "failed",
        total_cases: 2,
        passed_cases: 1,
        failed_cases: 1,
        base_url_override: null,
        started_at: "2026-03-11T20:01:00",
        finished_at: "2026-03-11T20:01:03",
      },
    },
  ]);

  renderWithProviders(<SuitesPage />, {
    route: "/suites",
    path: "/suites",
  });

  expect(await screen.findByText("登录回归套件")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "查看历史" })).toHaveAttribute("href", "/suites/1/runs/7");
  expect(screen.getByRole("link", { name: "历史" })).toHaveAttribute("href", "/suites/1/runs/7");
});

test("执行 suite 后跳转到批次详情页", async () => {
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
      latest_run: null,
    },
  ]);
  vi.mocked(api.executeSuite).mockResolvedValue({
    id: 11,
    suite_id: 1,
    suite_name: "登录回归套件",
    triggered_by: 1,
    source: "manual",
    source_suite_run_id: null,
    started_at: "2026-03-11T20:01:00",
    finished_at: "2026-03-11T20:01:03",
    total_cases: 2,
    passed_cases: 1,
    failed_cases: 1,
    base_url_override: null,
    status: "failed",
    items: [
      { id: 21, execution_id: 11, case_id: 1, case_name_snapshot: "登录用例", order_index: 1, status: "failed" },
      { id: 22, execution_id: 12, case_id: 2, case_name_snapshot: "退出用例", order_index: 2, status: "passed" },
    ],
    executions: [
      { execution_id: 11, case_id: 1, case_name: "登录用例", status: "failed" },
      { execution_id: 12, case_id: 2, case_name: "退出用例", status: "passed" },
    ],
  });

  renderWithProviders(<SuitesPage />, {
    route: "/suites",
    path: "/suites",
    extraRoutes: [
      <Route key="suite-run-detail" path="/suites/:suiteId/runs/:runId" element={<div>suite-run-detail</div>} />,
    ],
  });

  const row = (await screen.findByText("登录回归套件")).closest("tr");
  expect(row).not.toBeNull();

  await userEvent.click(within(row as HTMLElement).getByRole("button", { name: /执\s*行/ }));

  await waitFor(() => {
    expect(api.executeSuite).toHaveBeenCalledWith(1, { actor_user_id: 1 });
  });
  expect(await screen.findByText("suite-run-detail")).toBeInTheDocument();
});
