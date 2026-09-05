import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AuthGuard } from "./AuthGuard";
import { getSafeDestination } from "./LoginPage";
import { getCurrentUser } from "./api";

vi.mock("./api", () => ({
  getCurrentUser: vi.fn(),
  login: vi.fn(),
}));

function renderGuard(initialEntry = "/reports") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/login" element={<div>登录页面</div>} />
          <Route element={<AuthGuard />}>
            <Route path="/reports" element={<div>报告中心</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("AuthGuard", () => {
  beforeEach(() => {
    vi.mocked(getCurrentUser).mockReset();
  });

  it("未登录时跳转登录页", async () => {
    vi.mocked(getCurrentUser).mockRejectedValue(new Error("401"));
    renderGuard();
    expect(await screen.findByText("登录页面")).toBeInTheDocument();
  });

  it("已登录时渲染受保护页面", async () => {
    vi.mocked(getCurrentUser).mockResolvedValue({
      id: 1,
      email: "tester@example.com",
      display_name: "Tester",
    });
    renderGuard();
    expect(await screen.findByText("报告中心")).toBeInTheDocument();
  });
});

describe("getSafeDestination", () => {
  it("拒绝外部和反斜杠跳转", () => {
    expect(getSafeDestination({ from: "//evil.example" })).toBe("/planning");
    expect(getSafeDestination({ from: "/\\evil.example" })).toBe("/planning");
  });

  it("保留站内路由", () => {
    expect(getSafeDestination({ from: "/reports/12?tab=evidence" })).toBe(
      "/reports/12?tab=evidence",
    );
  });
});
