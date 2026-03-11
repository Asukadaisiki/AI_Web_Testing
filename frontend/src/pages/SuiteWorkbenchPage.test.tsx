import { Route } from "react-router-dom";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { SuiteWorkbenchPage } from "./SuiteWorkbenchPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getCases: vi.fn(),
    getSuiteDetail: vi.fn(),
    createSuite: vi.fn(),
    updateSuite: vi.fn(),
    executeSuite: vi.fn(),
  };
});

test("suite 工作台支持创建、排序并保存", async () => {
  vi.mocked(api.getCases).mockResolvedValue([
    {
      id: 1,
      project_id: 1,
      name: "登录用例",
      description: "检查登录",
      steps: [{ action: "goto", value: "/login" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-11T20:00:00",
      updated_at: "2026-03-11T20:00:00",
    },
    {
      id: 2,
      project_id: 1,
      name: "退出用例",
      description: "检查退出",
      steps: [{ action: "click", target: "退出按钮" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-11T20:00:00",
      updated_at: "2026-03-11T20:00:00",
    },
  ]);
  vi.mocked(api.createSuite).mockResolvedValue({
    id: 9,
    project_id: 1,
    name: "新建回归套件",
    description: "组合验证",
    case_count: 2,
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-11T20:00:00",
    updated_at: "2026-03-11T20:00:00",
    cases: [
      { case_id: 2, case_name: "退出用例", order_index: 1 },
      { case_id: 1, case_name: "登录用例", order_index: 2 },
    ],
  });

  renderWithProviders(<SuiteWorkbenchPage />, {
    route: "/suites/new",
    path: "/suites/new",
    extraRoutes: [<Route key="edit" path="/suites/:suiteId/edit" element={<div>suite-edit-view</div>} />],
  });

  expect(await screen.findByText("登录用例")).toBeInTheDocument();
  await userEvent.type(screen.getByLabelText("Suite 名称"), "新建回归套件");
  await userEvent.click(screen.getAllByRole("button", { name: "加入 Suite" })[0]);
  await userEvent.click(screen.getAllByRole("button", { name: "加入 Suite" })[0]);

  const selectedCards = screen.getAllByText(/Case \d/);
  expect(selectedCards).toHaveLength(2);
  await userEvent.click(screen.getAllByRole("button", { name: /上\s*移/ })[1]);
  await userEvent.click(screen.getByRole("button", { name: /保\s*存/ }));

  await waitFor(() => {
    expect(api.createSuite).toHaveBeenCalledWith({
      project_id: 1,
      actor_user_id: 1,
      name: "新建回归套件",
      description: null,
      cases: [{ case_id: 2 }, { case_id: 1 }],
    });
  });
  expect(await screen.findByText("suite-edit-view")).toBeInTheDocument();
});

test("suite 工作台支持编辑并执行 suite", async () => {
  vi.mocked(api.getCases).mockResolvedValue([
    {
      id: 1,
      project_id: 1,
      name: "登录用例",
      description: "检查登录",
      steps: [{ action: "goto", value: "/login" }],
      created_by: 1,
      updated_by: 1,
      created_at: "2026-03-11T20:00:00",
      updated_at: "2026-03-11T20:00:00",
    },
  ]);
  vi.mocked(api.getSuiteDetail).mockResolvedValue({
    id: 3,
    project_id: 1,
    name: "已有 Suite",
    description: "已有描述",
    case_count: 1,
    created_by: 1,
    updated_by: 1,
    created_at: "2026-03-11T20:00:00",
    updated_at: "2026-03-11T20:00:00",
    cases: [{ case_id: 1, case_name: "登录用例", order_index: 1 }],
  });
  vi.mocked(api.executeSuite).mockResolvedValue({
    suite_id: 3,
    suite_name: "已有 Suite",
    started_at: "2026-03-11T20:05:00",
    finished_at: "2026-03-11T20:05:01",
    total_cases: 1,
    passed_cases: 1,
    failed_cases: 0,
    status: "passed",
    executions: [{ execution_id: 31, case_id: 1, case_name: "登录用例", status: "passed" }],
  });

  renderWithProviders(<SuiteWorkbenchPage />, {
    route: "/suites/3/edit",
    path: "/suites/:suiteId/edit",
    extraRoutes: [
      <Route key="execution-detail" path="/executions/:executionId" element={<div>execution-detail</div>} />,
    ],
  });

  expect(await screen.findByDisplayValue("已有 Suite")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: /执\s*行 Suite/ }));

  await waitFor(() => {
    expect(api.executeSuite).toHaveBeenCalledWith(3, { actor_user_id: 1 });
  });
  expect(await screen.findByText("最近一次执行：已有 Suite")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "登录用例 (passed)" })).toHaveAttribute("href", "/executions/31");
});
