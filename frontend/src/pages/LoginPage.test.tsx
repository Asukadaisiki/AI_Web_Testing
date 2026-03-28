import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { renderWithProviders } from "../test/test-utils";
import { LoginPage } from "./LoginPage";

const loginMock = vi.fn();

vi.mock("../auth/AuthContext", () => ({
  useAuth: () => ({
    currentUser: null,
    isAuthResolved: true,
    isAuthenticated: false,
    login: loginMock,
    logout: vi.fn(),
  }),
}));

beforeEach(() => {
  loginMock.mockResolvedValue({
    id: 1,
    email: "seed-owner@example.com",
    display_name: "Seed Owner",
  });
});

afterEach(() => {
  vi.clearAllMocks();
});

test("登录成功后跳转到 dashboard", async () => {
  renderWithProviders(
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/dashboard" element={<div>Dashboard Mock</div>} />
    </Routes>,
    {
      route: "/login",
      path: "*",
    },
  );

  await userEvent.type(screen.getByLabelText("邮箱"), "seed-owner@example.com");
  await userEvent.type(screen.getByLabelText("密码"), "password123");
  await userEvent.click(screen.getByRole("button", { name: /登\s*录/ }));

  await waitFor(() => {
    expect(screen.getByText("Dashboard Mock")).toBeInTheDocument();
  });
  expect(loginMock).toHaveBeenCalledWith({
    email: "seed-owner@example.com",
    password: "password123",
  });
}, 10000);
