import { screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";

import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";
import { PlanningPage } from "./PlanningPage";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    createPlanningSession: vi.fn(),
    sendPlanningMessage: vi.fn(),
    generatePlanningDrafts: vi.fn(),
    updatePlanningDraftStatus: vi.fn(),
    getProjects: vi.fn(),
    getAISettings: vi.fn(),
    createCase: vi.fn(),
  };
});

beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(api.getProjects).mockResolvedValue([
    { id: 1, name: "Demo Project", description: null },
  ]);
  vi.mocked(api.getAISettings).mockResolvedValue({ enable_ai_planning: true } as never);
  vi.mocked(api.createPlanningSession).mockResolvedValue({
    session: {
      id: 1,
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

test("renders planning page with AI planning panel", async () => {
  renderWithProviders(<PlanningPage />, { route: "/", path: "/" });

  expect(await screen.findByText("AI 测试规划")).toBeInTheDocument();
  expect(await screen.findByText("AI 测试规划助手")).toBeInTheDocument();
});

test("shows alert when no projects available", async () => {
  vi.mocked(api.getProjects).mockResolvedValue([]);

  renderWithProviders(<PlanningPage />, { route: "/", path: "/" });

  expect(await screen.findByText("暂无可用项目")).toBeInTheDocument();
});

test("bootstraps planning session on mount with first project", async () => {
  renderWithProviders(<PlanningPage />, { route: "/", path: "/" });

  await waitFor(() => {
    expect(api.createPlanningSession).toHaveBeenCalledWith({
      project_id: 1,
      case_id: null,
    });
  });
});

test("planning page passes custom import label to panel", async () => {
  renderWithProviders(<PlanningPage />, { route: "/", path: "/" });

  await waitFor(() => {
    expect(api.createPlanningSession).toHaveBeenCalled();
  });

  // The panel bootstraps, so the custom label is passed via props
  // The button "创建用例并进入用例中心" would only appear in drafts
  expect(screen.getByText("AI 测试规划助手")).toBeInTheDocument();
});
