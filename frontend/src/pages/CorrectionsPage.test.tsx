import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { CorrectionsPage } from "./CorrectionsPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../components/OverviewChart", () => ({
  OverviewChart: () => <div data-testid="corrections-trend-chart" />,
}));

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    batchUpdateCorrectionState: vi.fn(),
    getCorrectionEvents: vi.fn(),
    getCorrections: vi.fn(),
    getCorrectionsOverview: vi.fn(),
    updateCorrectionState: vi.fn(),
  };
});

const mockCorrection = {
  id: 12,
  page_url_pattern: "https://app.example.com/login?session=*",
  target_description: "登录按钮",
  correction_type: "test_id" as const,
  correction_value: "login-button",
  verified_count: 3,
  consecutive_failures: 0,
  is_active: true,
  source_execution_id: 41,
  created_by: 1,
  created_at: "2026-03-14T20:00:00",
  updated_at: "2026-03-14T20:10:00",
};

const mockOverview = {
  total_count: 5,
  active_count: 3,
  inactive_count: 2,
  hit_count: 8,
  miss_count: 2,
  auto_deactivated_count: 1,
  current_window_start: "2026-03-08",
  current_window_end: "2026-03-14",
  trend_points: [
    { date: "2026-03-13", hit_count: 2, miss_count: 1 },
    { date: "2026-03-14", hit_count: 6, miss_count: 1 },
  ],
};

test("渲染 corrections overview、支持批量停用并查看事件", async () => {
  vi.mocked(api.getCorrections).mockResolvedValue([mockCorrection]);
  vi.mocked(api.getCorrectionsOverview).mockResolvedValue(mockOverview);
  vi.mocked(api.batchUpdateCorrectionState).mockResolvedValue([
    { ...mockCorrection, is_active: false },
  ]);
  vi.mocked(api.getCorrectionEvents).mockResolvedValue([
    {
      id: 101,
      correction_id: 12,
      event_type: "tier0_hit",
      page_url_pattern: mockCorrection.page_url_pattern,
      target_description: mockCorrection.target_description,
      execution_id: 41,
      verified_count_after: 3,
      consecutive_failures_after: 0,
      is_active_after: true,
      created_at: "2026-03-14T20:15:00",
    },
  ]);

  renderWithProviders(<CorrectionsPage />, {
    route: "/corrections?window_days=14",
    path: "/corrections",
  });

  expect(await screen.findByText("登录按钮")).toBeInTheDocument();
  expect(api.getCorrectionsOverview).toHaveBeenCalledWith(14);
  expect(screen.getByText("修正总数")).toBeInTheDocument();
  expect(screen.getByText("5")).toBeInTheDocument();

  const row = screen.getByText("登录按钮").closest("tr");
  expect(row).not.toBeNull();
  await userEvent.click(within(row as HTMLElement).getByRole("checkbox"));
  await userEvent.click(screen.getByRole("button", { name: "批量停用" }));

  await waitFor(() => {
    expect(api.batchUpdateCorrectionState).toHaveBeenCalledWith({
      correction_ids: [12],
      is_active: false,
    });
  });

  await userEvent.click(within(row as HTMLElement).getByRole("button", { name: "事件" }));

  expect(await screen.findByText("登录按钮 的事件时间线")).toBeInTheDocument();
  await waitFor(() => {
    expect(api.getCorrectionEvents).toHaveBeenCalledWith(12, { limit: 20, offset: 0 });
  });
  expect(screen.getByText("Tier 0 命中")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: "执行 #41" })).toHaveAttribute("href", "/executions/41");
});

test("应用筛选时会带上 page_url 和状态参数重新查询", async () => {
  vi.mocked(api.getCorrections).mockResolvedValue([]);
  vi.mocked(api.getCorrectionsOverview).mockResolvedValue(mockOverview);

  renderWithProviders(<CorrectionsPage />, {
    route: "/corrections",
    path: "/corrections",
  });

  await screen.findByText("当前筛选条件下没有修正记录。");
  await userEvent.type(screen.getByPlaceholderText("按目标描述筛选"), "订单按钮");
  await userEvent.type(screen.getByPlaceholderText("按页面 URL 筛选"), "https://app.example.com/orders/123");
  await userEvent.click(screen.getAllByRole("combobox")[0]);
  await userEvent.click(screen.getByText("仅已停用"));
  await userEvent.click(screen.getByRole("button", { name: "应用筛选" }));

  await waitFor(() => {
    expect(api.getCorrections).toHaveBeenLastCalledWith({
      target_description: "订单按钮",
      page_url: "https://app.example.com/orders/123",
      is_active: false,
      limit: 10,
      offset: 0,
    });
  });
});
