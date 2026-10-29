import { z } from "zod/v4";

export type BridgeConfig = {
  apiUrl: string;
  bridgeToken: string;
  langfuseProjectId: string;
  agenttraceProjectId: string;
  actorId: string;
  role: "admin" | "member";
};

export type CreateRunInput = {
  projectId: string;
  conversationId: string;
  url?: string;
  goal: string;
  runtimeId: RuntimeId;
  harnessId: string;
  idempotencyKey: string;
};

export const RuntimeIdSchema = z.enum(["pi", "claude-code", "codex"]);
export type RuntimeId = z.infer<typeof RuntimeIdSchema>;
export const RuntimeSchema = z.object({
  id: RuntimeIdSchema,
  name: z.string(),
  model: z.object({
    provider: z.string(),
    name: z.string(),
    reasoning_effort: z.string().optional(),
  }),
  available: z.boolean(),
  unavailable_reason: z.string().optional(),
});
export type Runtime = z.infer<typeof RuntimeSchema>;

const ResourceNameSchema = z.string().regex(/^[A-Za-z0-9][A-Za-z0-9._-]*$/);
export const HarnessDefinitionSchema = z.strictObject({
  name: z.string().trim().min(1).max(200),
  system_prompt: z.string().max(100000),
  skill_version_ids: z.array(ResourceNameSchema),
  mcp_binding_ids: z.array(ResourceNameSchema),
  runtime_ids: z.array(RuntimeIdSchema).min(1),
});
export const HarnessSnapshotSchema = HarnessDefinitionSchema.extend({
  id: ResourceNameSchema,
  version: z.number().int().positive(),
  harness_id: z.string(),
  project_id: z.string(),
  content_digest: z.string(),
  revoked_at: z.string().nullable().optional(),
}).strip();
export type HarnessDefinition = z.infer<typeof HarnessDefinitionSchema>;
export type HarnessSnapshot = z.infer<typeof HarnessSnapshotSchema>;

export const CreateRunSchema = z.object({
  projectId: z.string().min(1),
  conversationId: z.string().min(1),
  goal: z.string().trim().min(1),
  url: z
    .string()
    .trim()
    .transform((value) => value || undefined)
    .optional()
    .refine((value) => {
      if (!value) return true;
      try {
        return ["http:", "https:"].includes(new URL(value).protocol);
      } catch {
        return false;
      }
    }, "URL must use HTTP or HTTPS"),
  runtimeId: RuntimeIdSchema,
  harnessId: ResourceNameSchema,
  idempotencyKey: z.string().trim().min(1),
});

const JsonObjectSchema = z.record(z.string(), z.unknown());
const DateTimeSchema = z.iso.datetime({ offset: true });

const ConditionSchema = z.object({
  type: z.string(),
  value: z.string(),
  timeout_ms: z.number().int().nonnegative(),
});

const LocatorSchema = z.object({
  kind: z.string(),
  role: z.string().optional(),
  name: z.string().optional(),
  exact: z.boolean().optional(),
  text: z.string().optional(),
  css: z.string().optional(),
  match_count: z.number().int(),
});

const GroundingSchema = z.object({
  observation_id: z.string(),
  page_state_id: z.string(),
  candidate_id: z.string(),
  page_url: z.string(),
});

const TargetSpecSchema = z.object({
  object: z.object({
    role: z.string().optional(),
    text: z.string().optional(),
    name: z.string().optional(),
    aliases: z.array(z.string()).optional(),
  }),
  scope: z
    .object({
      kind: z.string().optional(),
      contains_text: z.string().optional(),
      ref: z.string().optional(),
    })
    .optional(),
  relation: z.string().optional(),
  role: z.string().optional(),
  text: z.string().optional(),
  name: z.string().optional(),
  aliases: z.array(z.string()).optional(),
});

const TargetSchema = z.object({
  hint: z.string(),
  locator: LocatorSchema,
  grounding: GroundingSchema,
  spec: TargetSpecSchema.optional(),
});

const CaseStepSchema = z.object({
  index: z.number().int().nonnegative(),
  action: z.string(),
  intent: z.string(),
  value: z.string().optional(),
  submit: z.boolean().optional(),
  target: TargetSchema.optional(),
  preconditions: z.array(ConditionSchema),
  postconditions: z.array(ConditionSchema),
  timeout_ms: z.number().int().nonnegative(),
});

export const CaseSchema = z.object({
  case_version: z.string(),
  name: z.string(),
  goal: z.string(),
  base_url: z.string(),
  steps: z.array(CaseStepSchema),
});

const StepErrorSchema = z.object({
  kind: z.string(),
  message: z.string(),
});

const ConditionResultSchema = z.object({
  phase: z.enum(["pre", "post"]),
  type: z.string(),
  value: z.string(),
  satisfied: z.boolean(),
  detail: z.string().nullable(),
});

const EvidenceSchema = z.object({
  screenshot_path: z.string(),
  console: z.array(z.object({ level: z.string(), text: z.string() })),
  network: z.array(
    z.object({
      method: z.string(),
      url: z.string(),
      status: z.number().int(),
    }),
  ),
});

const BlockerSchema = z.object({
  kind: z.string(),
  ref: z.string().optional(),
  confidence: z.string(),
  covers_target_ref: z.string().optional(),
  dismiss_candidates: z.array(LocatorSchema).optional(),
  reason: z.string(),
});

const HitTestSchema = z.object({
  target_ref: z.string().optional(),
  x: z.number(),
  y: z.number(),
  hit_ref: z.string().optional(),
  hit_tag: z.string().optional(),
  hit_role: z.string().optional(),
  hit_text: z.string().optional(),
  covered: z.boolean(),
  blocker_kind: z.string().optional(),
});

const RecoveryAttemptSchema = z.object({
  blocker: BlockerSchema,
  action: z.string(),
  succeeded: z.boolean(),
  reason: z.string().optional(),
  before_screenshot_path: z.string().optional(),
  after_screenshot_path: z.string().optional(),
  url_before: z.string().optional(),
  url_after: z.string().optional(),
  retried_original_action: z.boolean(),
});

const ExecutionStepSchema = z.object({
  index: z.number().int().nonnegative(),
  action: z.string(),
  status: z.string(),
  started_at: DateTimeSchema,
  duration_ms: z.number().int().nonnegative(),
  url_before: z.string(),
  url_after: z.string(),
  conditions: z.array(ConditionResultSchema),
  evidence: EvidenceSchema,
  error: StepErrorSchema.nullable(),
  blocker: BlockerSchema.nullable().optional(),
  hit_test: HitTestSchema.nullable().optional(),
  recovery: z.array(RecoveryAttemptSchema).optional(),
});

export const ExecutionResultSchema = z.object({
  execution_id: z.string(),
  status: z.enum(["passed", "failed", "error"]),
  started_at: DateTimeSchema,
  finished_at: DateTimeSchema,
  final_url: z.string(),
  steps: z.array(ExecutionStepSchema),
  error: StepErrorSchema.nullable(),
});

export const RunSchema = z.object({
  id: z.string(),
  project_id: z.string(),
  conversation_id: z.string(),
  source_message_id: z.string().optional(),
  retry_of_run_id: z.string().optional(),
  runtime_id: RuntimeIdSchema.optional(),
  harness_id: z.string().optional(),
  harness_revision: z.number().int().positive().optional(),
  environment_id: z.string().optional(),
  profile_generation: z.number().int().nonnegative().optional(),
  model: z.object({
    provider: z.string(),
    name: z.string(),
    reasoning_effort: z.string().optional(),
  }).optional(),
  harness_snapshot: z
    .object({
      harness_id: z.string(),
      harness_version_id: z.string(),
      revision: z.number().int().nonnegative().optional(),
      name: z.string().optional(),
      project_id: z.string().optional(),
      content_digest: z.string(),
      runtime_ids: z.array(RuntimeIdSchema).optional(),
      skill_version_ids: z.array(z.string()),
      mcp_binding_ids: z.array(z.string()),
      runtime_id: RuntimeIdSchema,
      environment_id: z.string(),
      skills: z.array(z.object({
        name: z.string(),
        version_id: z.string().optional(),
        content_digest: z.string().optional(),
      })).optional(),
      mcp: z.record(z.string(), z.object({
        capability: z.string(),
        version_id: z.string().optional(),
        content_digest: z.string().optional(),
      })).optional(),
    })
    .optional(),
  status: z.enum([
    "queued",
    "running",
    "waiting_input",
    "waiting_approval",
    "cancelling",
    "succeeded",
    "failed",
    "cancelled",
  ]),
  checkpoint_version: z.number().int().nonnegative(),
  cancel_requested_at: DateTimeSchema.optional(),
  started_at: DateTimeSchema.optional(),
  finished_at: DateTimeSchema.optional(),
  created_at: DateTimeSchema,
  updated_at: DateTimeSchema,
});

export const TraceSchema = z.object({
  spans: z.array(
    z.object({
      id: z.string(),
      run_id: z.string(),
      trace_id: z.string(),
      span_id: z.string(),
      parent_span_id: z.string().optional(),
      kind: z.string(),
      name: z.string(),
      status: z.string(),
      input: z.unknown().optional(),
      output: z.unknown().optional(),
      decision_context: JsonObjectSchema.optional(),
      started_at: DateTimeSchema,
      ended_at: DateTimeSchema.nullable().optional(),
      created_at: DateTimeSchema,
      updated_at: DateTimeSchema,
    }),
  ),
  attempts: z.array(
    z.object({
      id: z.string(),
      span_id: z.string(),
      attempt_number: z.number().int().nonnegative(),
      provider: z.string().optional(),
      operation_id: z.string().optional(),
      status: z.string(),
      error: z.unknown().optional(),
      started_at: DateTimeSchema,
      ended_at: DateTimeSchema.nullable().optional(),
    }),
  ),
  usage: z.array(
    z.object({
      id: z.string(),
      span_id: z.string().optional(),
      span_attempt_id: z.string().optional(),
      provider: z.string().optional(),
      model: z.string().optional(),
      input_tokens: z.number().int(),
      output_tokens: z.number().int(),
      total_tokens: z.number().int(),
      cost_micros: z.number().int(),
      details: z.unknown(),
      created_at: DateTimeSchema,
    }),
  ),
  operation_evidence: z.array(
    z.object({
      id: z.string(),
      span_id: z.string().optional(),
      span_attempt_id: z.string().optional(),
      operation_id: z.string(),
      status: z.string(),
      result: z.unknown().optional(),
      artifacts: z
        .object({ screenshot_paths: z.array(z.string()).optional() })
        .nullable(),
      error: z.unknown().optional(),
      created_at: DateTimeSchema,
      updated_at: DateTimeSchema,
    }),
  ),
  derived_nodes: z.array(
    z.object({
      id: z.string(),
      parent_span_id: z.string(),
      source_evidence_id: z.string(),
      kind: z.string(),
      name: z.string(),
      status: z.string(),
      started_at: DateTimeSchema,
      ended_at: DateTimeSchema.nullable(),
      data: z.unknown(),
      evidence: z.unknown(),
    }),
  ),
});

export type AgentTraceRun = z.infer<typeof RunSchema>;
export type AgentTrace = z.infer<typeof TraceSchema>;

export function readAgentTraceRunId(metadata: unknown): string | null {
  try {
    const value =
      typeof metadata === "string" ? JSON.parse(metadata) : metadata;
    if (!value || typeof value !== "object" || Array.isArray(value))
      return null;
    const id = (value as Record<string, unknown>).agenttrace_run_id;
    return typeof id === "string" && id.startsWith("run_") ? id : null;
  } catch {
    return null;
  }
}

export const shouldShowAgentTracePanel = (
  runId: string | null,
  hasProjectAccess: boolean,
) => Boolean(runId && hasProjectAccess);
