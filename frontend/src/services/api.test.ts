import { afterEach, beforeEach, expect, test, vi } from "vitest";

import {
  generateDslCase,
  getAISettings,
  getAISettingsOverview,
  getCorrectionEvents,
  getCorrections,
  getDslGenerationRunDetail,
  getDslGenerationRuns,
  getExecutions,
  recordDslGenerationFeedback,
  updateAISettings,
} from "./api";

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockResolvedValue({
    ok: true,
    json: async () => [],
  });
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
});

test("getCorrections includes offset=0 in query string", async () => {
  await getCorrections({
    target_description: "登录按钮",
    offset: 0,
    limit: 10,
  });

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/corrections?target_description=%E7%99%BB%E5%BD%95%E6%8C%89%E9%92%AE&limit=10&offset=0",
    expect.any(Object),
  );
});

test("getExecutions includes offset=0 in query string", async () => {
  await getExecutions({
    project_id: 1,
    offset: 0,
    limit: 10,
  });

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/executions?project_id=1&limit=10&offset=0",
    expect.any(Object),
  );
});

test("getCorrectionEvents includes offset=0 in query string", async () => {
  await getCorrectionEvents(12, {
    offset: 0,
    limit: 20,
  });

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/corrections/12/events?limit=20&offset=0",
    expect.any(Object),
  );
});

test("generateDslCase posts prompt payload to DSL generate endpoint", async () => {
  await generateDslCase({
    prompt: "打开 example.com 并验证 URL",
    base_url: "https://example.com",
    actor_user_id: 1,
    project_id: 1,
    case_id: 9,
    generation_mode: "strict_steps_only",
    import_mode: "steps_only",
    current_case: {
      name: "当前用例",
      description: "当前描述",
      base_url: "https://example.com",
      input_contract: [],
      output_contract: [],
      steps: [{ action: "goto", value: "/" }],
    },
    current_steps: [{ action: "goto", value: "/" }],
    current_input_contract: [
      {
        name: "token",
        context_key: "token",
        value_type: "string",
        required: true,
      },
    ],
    current_output_contract: [
      {
        name: "latestPage",
        context_key: "latest_page",
        value_type: "string",
        source: "latest_url",
      },
    ],
    preserve_contracts: true,
  });

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/dsl/generate",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify({
        prompt: "打开 example.com 并验证 URL",
        base_url: "https://example.com",
        actor_user_id: 1,
        project_id: 1,
        case_id: 9,
        generation_mode: "strict_steps_only",
        import_mode: "steps_only",
        current_case: {
          name: "当前用例",
          description: "当前描述",
          base_url: "https://example.com",
          input_contract: [],
          output_contract: [],
          steps: [{ action: "goto", value: "/" }],
        },
        current_steps: [{ action: "goto", value: "/" }],
        current_input_contract: [
          {
            name: "token",
            context_key: "token",
            value_type: "string",
            required: true,
          },
        ],
        current_output_contract: [
          {
            name: "latestPage",
            context_key: "latest_page",
            value_type: "string",
            source: "latest_url",
          },
        ],
        preserve_contracts: true,
      }),
    }),
  );
});

test("getAISettings requests the runtime AI settings endpoint", async () => {
  await getAISettings();

  expect(fetchMock).toHaveBeenCalledWith("/api/v1/settings/ai", expect.any(Object));
});

test("getAISettingsOverview requests the AI settings overview endpoint", async () => {
  await getAISettingsOverview();

  expect(fetchMock).toHaveBeenCalledWith("/api/v1/settings/ai/overview", expect.any(Object));
});

test("getDslGenerationRuns includes governance filters in query string", async () => {
  await getDslGenerationRuns({
    status: "failed",
    feedback_status: "rejected",
    generation_mode: "strict_steps_only",
    import_mode: "steps_only",
    prompt_variant: "repair_steps",
    rejection_reason_code: "context_mismatch",
    has_risk_flags: true,
    model_name: "gpt-4o-mini",
    project_id: 1,
    case_id: 9,
    created_from: "2026-03-18T00:00:00",
    created_to: "2026-03-18T23:59:59",
    limit: 10,
    offset: 0,
  });

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/dsl/generations?status=failed&feedback_status=rejected&generation_mode=strict_steps_only&import_mode=steps_only&prompt_variant=repair_steps&rejection_reason_code=context_mismatch&has_risk_flags=true&model_name=gpt-4o-mini&project_id=1&case_id=9&created_from=2026-03-18T00%3A00%3A00&created_to=2026-03-18T23%3A59%3A59&limit=10&offset=0",
    expect.any(Object),
  );
});

test("getDslGenerationRunDetail requests the detail endpoint", async () => {
  await getDslGenerationRunDetail(23);

  expect(fetchMock).toHaveBeenCalledWith("/api/v1/dsl/generations/23", expect.any(Object));
});

test("recordDslGenerationFeedback posts feedback payload to endpoint", async () => {
  await recordDslGenerationFeedback(23, {
    actor_user_id: 1,
    feedback_status: "rejected",
    feedback_import_mode: null,
    rejection_reason_code: "bad_contracts",
    feedback_note: "契约不符合预期",
  });

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/dsl/generations/23/feedback",
    expect.objectContaining({
      method: "PATCH",
      body: JSON.stringify({
        actor_user_id: 1,
        feedback_status: "rejected",
        feedback_import_mode: null,
        rejection_reason_code: "bad_contracts",
        feedback_note: "契约不符合预期",
      }),
    }),
  );
});

test("updateAISettings sends the runtime AI settings payload", async () => {
  await updateAISettings({
    enable_ai_dsl_generate: true,
    ai_dsl_timeout_ms: 15000,
    ai_dsl_base_url: "https://api.openai.com/v1",
    ai_dsl_model: "gpt-4o-mini",
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
    vlm_api_key: null,
    clear_vlm_api_key: true,
  });

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/v1/settings/ai",
    expect.objectContaining({
      method: "PUT",
      body: JSON.stringify({
        enable_ai_dsl_generate: true,
        ai_dsl_timeout_ms: 15000,
        ai_dsl_base_url: "https://api.openai.com/v1",
        ai_dsl_model: "gpt-4o-mini",
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
        vlm_api_key: null,
        clear_vlm_api_key: true,
      }),
    }),
  );
});
