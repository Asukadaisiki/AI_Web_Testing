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
    getDslGenerationRunDetail: vi.fn(),
    getDslGenerationRuns: vi.fn(),
    updateAISettings: vi.fn(),
  };
});

beforeEach(() => {
  vi.resetAllMocks();
});

test("渲染 AI 治理概览、支持筛选并查看详情", async () => {
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
      accepted_count: 2,
      rejected_count: 1,
      pending_count: 1,
      decision_coverage_rate: 0.75,
      last_model: "gpt-4o-mini",
      last_error_type: "DslGenerationError",
      last_error_message: "bad json",
      last_24h_requests: 3,
      last_24h_success_count: 2,
      last_24h_failure_count: 1,
      last_24h_auto_repair_rate: 0.5,
      top_error_types: [
        {
          error_type: "DslGenerationError",
          count: 1,
        },
      ],
      accepted_import_mode_breakdown: [
        {
          import_mode: "replace",
          count: 1,
        },
        {
          import_mode: "steps_only",
          count: 1,
        },
      ],
      top_rejection_reasons: [{ rejection_reason_code: "bad_contracts", count: 1 }],
      prompt_variant_breakdown: [
        {
          prompt_variant: "baseline_draft",
          total_requests: 4,
          success_count: 3,
          accepted_count: 2,
          rejected_count: 1,
        },
      ],
      context_profile_breakdown: [
        {
          context_profile: "blank_request",
          total_requests: 4,
          success_count: 3,
          accepted_count: 2,
          rejected_count: 1,
        },
      ],
      rejection_reason_by_variant: [
        {
          prompt_variant: "baseline_draft",
          rejection_reason_code: "bad_contracts",
          count: 1,
        },
      ],
      model_outcome_breakdown: [
        {
          model_name: "gpt-4o-mini",
          total_requests: 4,
          success_count: 3,
          accepted_count: 2,
          rejected_count: 1,
        },
      ],
      generation_mode_breakdown: [
        {
          generation_mode: "draft",
          total_requests: 4,
          success_count: 3,
          accepted_count: 2,
          rejected_count: 1,
        },
      ],
    },
  });
  vi.mocked(api.getDslGenerationRuns).mockResolvedValue([
    {
      id: 7,
      created_at: "2026-03-16T10:00:00",
      success: false,
      model_name: "gpt-4o-mini",
      generation_mode: "draft",
      import_mode: "replace",
      prompt_variant: "baseline_draft",
      project_id: 1,
      case_id: 8,
      prompt_version: "2026-03-18.single-pass-variant-v1",
      error_type: "DslGenerationError",
      error_message: "bad json",
      repaired_invalid_actions: 1,
      removed_invalid_steps: 2,
      removed_invalid_contracts: 0,
      warnings_count: 1,
      normalization_notes_count: 2,
      prompt_preview: "打开 example.com 并验证 URL",
      risk_flags: ["invalid_steps_removed"],
      feedback_status: "rejected",
      feedback_import_mode: null,
      rejection_reason_code: "bad_contracts",
      feedback_recorded_at: "2026-03-16T10:02:00",
    },
  ]);
  vi.mocked(api.getDslGenerationRunDetail).mockResolvedValue({
    id: 7,
    created_at: "2026-03-16T10:00:00",
    success: false,
    model_name: "gpt-4o-mini",
    generation_mode: "draft",
    import_mode: "replace",
    prompt_variant: "baseline_draft",
    project_id: 1,
    case_id: 8,
    prompt_version: "2026-03-18.single-pass-variant-v1",
    error_type: "DslGenerationError",
    error_message: "bad json",
    repaired_invalid_actions: 1,
    removed_invalid_steps: 2,
    removed_invalid_contracts: 0,
    warnings_count: 1,
    normalization_notes_count: 2,
    prompt_preview: "打开 example.com 并验证 URL",
    risk_flags: ["invalid_steps_removed"],
    feedback_status: "rejected",
    feedback_import_mode: null,
    rejection_reason_code: "bad_contracts",
    feedback_recorded_at: "2026-03-16T10:02:00",
    request_base_url: "https://example.com",
    generated_case_json: {
      name: "治理详情草案",
      description: null,
      base_url: null,
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/" }],
    },
    warnings_json: ["bad json"],
    normalization_notes_json: ["步骤 #1 已自动修正"],
    feedback_note: "契约不符合预期",
    context_profile: "blank_request",
    used_current_case_context: true,
    used_current_steps_context: false,
    preserve_contracts_requested: true,
    preserve_contracts_applied: true,
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
  expect(screen.getByText("2 / 1 / 1")).toBeInTheDocument();
  expect(screen.getByText("2 / 1")).toBeInTheDocument();
  expect(screen.getByText("50%")).toBeInTheDocument();
  expect(screen.getByText("75%")).toBeInTheDocument();
  expect(screen.getByText("DslGenerationError (1)")).toBeInTheDocument();
  expect(screen.getByText("replace (1)、steps_only (1)")).toBeInTheDocument();
  expect(screen.getByText("bad_contracts (1)")).toBeInTheDocument();
  expect(screen.getByText("baseline_draft: 4 / 2 / 1")).toBeInTheDocument();
  expect(screen.getByText("blank_request: 4 / 2 / 1")).toBeInTheDocument();
  expect(screen.getByText("baseline_draft / bad_contracts (1)")).toBeInTheDocument();
  expect(screen.getByText("gpt-4o-mini: 4 / 2 / 1")).toBeInTheDocument();
  expect(screen.getByText("draft: 4 / 2 / 1")).toBeInTheDocument();
  expect(screen.getByText("治理记录")).toBeInTheDocument();
  expect(screen.getByText("已放弃 (bad_contracts)")).toBeInTheDocument();
  expect(screen.getByText("baseline_draft")).toBeInTheDocument();
  expect(screen.getByText("invalid_steps_removed")).toBeInTheDocument();
  expect(screen.getByText("详情")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("combobox", { name: "Prompt Variant" }));
  await userEvent.click(await screen.findByText("baseline_draft", { selector: ".ant-select-item-option-content" }));
  await userEvent.click(screen.getByRole("combobox", { name: "拒绝原因" }));
  await userEvent.click(await screen.findByText("bad_contracts", { selector: ".ant-select-item-option-content" }));
  await userEvent.click(screen.getByRole("combobox", { name: "风险标签" }));
  await userEvent.click(await screen.findByText("仅高风险", { selector: ".ant-select-item-option-content" }));
  await userEvent.type(screen.getByLabelText("模型名"), "gpt-4o-mini");
  await userEvent.clear(screen.getByLabelText("项目 ID"));
  await userEvent.type(screen.getByLabelText("项目 ID"), "1");
  await userEvent.click(screen.getByRole("button", { name: "应用筛选" }));

  await waitFor(() => {
    expect(api.getDslGenerationRuns).toHaveBeenLastCalledWith({
      prompt_variant: "baseline_draft",
      rejection_reason_code: "bad_contracts",
      has_risk_flags: true,
      model_name: "gpt-4o-mini",
      project_id: 1,
      limit: 10,
      offset: 0,
    });
  });

  await userEvent.click(screen.getByRole("button", { name: "详情" }));
  expect(await screen.findByText("治理详情 #7")).toBeInTheDocument();
  expect(await screen.findByText("契约不符合预期")).toBeInTheDocument();
  expect(screen.getByText("current_case / preserve_contracts (applied)")).toBeInTheDocument();
  expect(screen.getByText("blank_request")).toBeInTheDocument();
  expect(screen.getAllByText("baseline_draft").length).toBeGreaterThan(0);
  expect(screen.getAllByText("invalid_steps_removed").length).toBeGreaterThan(0);
  expect(screen.getByDisplayValue(/治理详情草案/)).toBeInTheDocument();
}, 15000);

test("保存 AI 配置会提交最新表单值", async () => {
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
      total_requests: 0,
      success_count: 0,
      failure_count: 0,
      accepted_count: 0,
      rejected_count: 0,
      pending_count: 0,
      decision_coverage_rate: 0,
      last_model: null,
      last_error_type: null,
      last_error_message: null,
      last_24h_requests: 0,
      last_24h_success_count: 0,
      last_24h_failure_count: 0,
      last_24h_auto_repair_rate: 0,
      top_error_types: [],
      accepted_import_mode_breakdown: [],
      top_rejection_reasons: [],
      prompt_variant_breakdown: [],
      context_profile_breakdown: [],
      rejection_reason_by_variant: [],
      model_outcome_breakdown: [],
      generation_mode_breakdown: [],
    },
  });
  vi.mocked(api.getDslGenerationRuns).mockResolvedValue([]);
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
}, 15000);

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
      accepted_count: 0,
      rejected_count: 0,
      pending_count: 0,
      decision_coverage_rate: 0,
      last_model: null,
      last_error_type: null,
      last_error_message: null,
      last_24h_requests: 0,
      last_24h_success_count: 0,
      last_24h_failure_count: 0,
      last_24h_auto_repair_rate: 0,
      top_error_types: [],
      accepted_import_mode_breakdown: [],
      top_rejection_reasons: [],
      prompt_variant_breakdown: [],
      context_profile_breakdown: [],
      rejection_reason_by_variant: [],
      model_outcome_breakdown: [],
      generation_mode_breakdown: [],
    },
  });
  vi.mocked(api.getDslGenerationRuns).mockResolvedValue([]);
  vi.mocked(api.getDslGenerationRunDetail).mockResolvedValue({
    id: 1,
    created_at: "2026-03-16T10:00:00",
    success: false,
    model_name: null,
    generation_mode: "draft",
    import_mode: "replace",
    prompt_variant: "baseline_draft",
    project_id: null,
    case_id: null,
    prompt_version: "2026-03-18.single-pass-variant-v1",
    error_type: null,
    error_message: null,
    repaired_invalid_actions: 0,
    removed_invalid_steps: 0,
    removed_invalid_contracts: 0,
    warnings_count: 0,
    normalization_notes_count: 0,
    prompt_preview: "preview",
    risk_flags: [],
    feedback_status: "pending",
    feedback_import_mode: null,
    rejection_reason_code: null,
    feedback_recorded_at: null,
    request_base_url: null,
    generated_case_json: null,
    warnings_json: [],
    normalization_notes_json: [],
    feedback_note: null,
    context_profile: "blank_request",
    used_current_case_context: false,
    used_current_steps_context: false,
    preserve_contracts_requested: false,
    preserve_contracts_applied: false,
  });

  renderWithProviders(<AISettingsPage />, {
    route: "/settings/ai",
    path: "/settings/ai",
  });

  expect(await screen.findByText("settings failed")).toBeInTheDocument();
});
