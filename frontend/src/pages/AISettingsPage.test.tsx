import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { vi } from "vitest";

import { AISettingsPage } from "./AISettingsPage";
import * as api from "../services/api";
import { renderWithProviders } from "../test/test-utils";

vi.mock("../services/api", async () => {
  const actual = await vi.importActual<typeof import("../services/api")>("../services/api");
  return {
    ...actual,
    getAISettings: vi.fn(),
    getAISettingsOverview: vi.fn(),
    updateAISettings: vi.fn(),
  };
});

beforeEach(() => {
  vi.resetAllMocks();
});

test("渲染 AI 配置并允许保存修改", async () => {
  vi.mocked(api.getAISettings).mockResolvedValue({
    enable_ai_dsl_generate: false,
    ai_dsl_timeout_ms: 15000,
    ai_dsl_base_url: "https://api.openai.com/v1",
    ai_dsl_model: "gpt-4o-mini",
    ai_dsl_strict_mode: false,
    ai_dsl_allow_auto_repair: true,
    has_ai_dsl_api_key: true,
    enable_ai_visual_locate: false,
    ai_visual_timeout_ms: 10000,
    ai_visual_failure_threshold: 3,
    ai_visual_cooldown_seconds: 60,
    ai_visual_rate_limit_per_minute: 10,
    vlm_base_url: "https://api.openai.com/v1",
    vlm_model: "gpt-4o",
    vlm_model_family: "gpt-4o",
    has_vlm_api_key: false,
  });
  vi.mocked(api.getAISettingsOverview).mockResolvedValue({
    ai_dsl_enabled: false,
    ai_dsl_model: "gpt-4o-mini",
    ai_dsl_strict_mode: false,
    ai_dsl_allow_auto_repair: true,
    generation_stats: {
      total_requests: 4,
      success_count: 3,
      failure_count: 1,
      last_model: "gpt-4o-mini",
      last_error_type: "DslGenerationError",
      last_error_message: "bad json",
    },
  });
  vi.mocked(api.updateAISettings).mockResolvedValue({
    enable_ai_dsl_generate: true,
    ai_dsl_timeout_ms: 18000,
    ai_dsl_base_url: "https://llm.example.com/v1",
    ai_dsl_model: "gpt-dsl",
    ai_dsl_strict_mode: true,
    ai_dsl_allow_auto_repair: false,
    has_ai_dsl_api_key: true,
    enable_ai_visual_locate: true,
    ai_visual_timeout_ms: 12000,
    ai_visual_failure_threshold: 4,
    ai_visual_cooldown_seconds: 90,
    ai_visual_rate_limit_per_minute: 12,
    vlm_base_url: "https://vlm.example.com/v1",
    vlm_model: "gpt-4o",
    vlm_model_family: "gpt-4o",
    has_vlm_api_key: false,
  });

  renderWithProviders(<AISettingsPage />, {
    route: "/settings/ai",
    path: "/settings/ai",
  });

  expect(await screen.findByLabelText("AI DSL Base URL")).toHaveValue("https://api.openai.com/v1");
  expect(screen.getByText("已配置")).toBeInTheDocument();
  expect(screen.getByText("未配置")).toBeInTheDocument();
  expect(screen.getByText("4")).toBeInTheDocument();
  expect(screen.getByText("3 / 1")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("switch", { name: "启用 DSL 生成" }));
  await userEvent.click(screen.getByRole("switch", { name: "严格生成模式" }));
  await userEvent.click(screen.getByRole("switch", { name: "允许自动修正" }));
  await userEvent.clear(screen.getByLabelText("AI DSL Base URL"));
  await userEvent.type(screen.getByLabelText("AI DSL Base URL"), "https://llm.example.com/v1");
  await userEvent.clear(screen.getByLabelText("AI DSL Timeout (ms)"));
  await userEvent.type(screen.getByLabelText("AI DSL Timeout (ms)"), "18000");
  await userEvent.clear(screen.getByLabelText("AI DSL Model"));
  await userEvent.type(screen.getByLabelText("AI DSL Model"), "gpt-dsl");
  const passwordInputs = screen.getAllByPlaceholderText("留空则保持原值");
  await userEvent.type(passwordInputs[0], "new-dsl-secret");
  await userEvent.type(passwordInputs[1], "new-vlm-secret");

  await userEvent.click(screen.getByRole("switch", { name: "启用视觉定位" }));

  await userEvent.click(screen.getByRole("button", { name: "保存配置" }));

  await waitFor(() => {
    expect(api.updateAISettings).toHaveBeenCalledWith({
      enable_ai_dsl_generate: true,
      ai_dsl_timeout_ms: 18000,
      ai_dsl_base_url: "https://llm.example.com/v1",
      ai_dsl_model: "gpt-dsl",
      ai_dsl_strict_mode: true,
      ai_dsl_allow_auto_repair: false,
      ai_dsl_api_key: "new-dsl-secret",
      clear_ai_dsl_api_key: false,
      enable_ai_visual_locate: true,
      ai_visual_timeout_ms: 10000,
      ai_visual_failure_threshold: 3,
      ai_visual_cooldown_seconds: 60,
      ai_visual_rate_limit_per_minute: 10,
      vlm_base_url: "https://api.openai.com/v1",
      vlm_model: "gpt-4o",
      vlm_model_family: "gpt-4o",
      vlm_api_key: "new-vlm-secret",
      clear_vlm_api_key: false,
    });
  });
}, 10000);

test("加载失败时展示错误块", async () => {
  vi.mocked(api.getAISettings).mockRejectedValue(new Error("settings failed"));
  vi.mocked(api.getAISettingsOverview).mockResolvedValue({
    ai_dsl_enabled: false,
    ai_dsl_model: null,
    ai_dsl_strict_mode: false,
    ai_dsl_allow_auto_repair: true,
    generation_stats: {
      total_requests: 0,
      success_count: 0,
      failure_count: 0,
      last_model: null,
      last_error_type: null,
      last_error_message: null,
    },
  });

  renderWithProviders(<AISettingsPage />, {
    route: "/settings/ai",
    path: "/settings/ai",
  });

  expect(await screen.findByText("settings failed")).toBeInTheDocument();
});
