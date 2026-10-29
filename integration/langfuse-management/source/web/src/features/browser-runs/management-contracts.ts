import { z } from "zod/v4";

const id = z.string().min(1).max(200);
const ConversationStatusSchema = z.enum([
  "active",
  "archived",
  "deleting",
  "deleted",
]);
export const ConversationSchema = z.union([
  z
    .object({
      id,
      project_id: id,
      title: z.string().optional(),
      state: ConversationStatusSchema,
    })
    .transform(({ title, state, ...conversation }) => ({
      ...conversation,
      name: title || "Untitled conversation",
      status: state,
    })),
  z.object({
    id,
    project_id: id,
    name: z.string(),
    status: ConversationStatusSchema,
  }),
]);
export type Conversation = z.infer<typeof ConversationSchema>;
export const EnvironmentSchema = z.object({
  id,
  project_id: id,
  conversation_id: id,
  state: z.enum([
    "provisioning",
    "ready",
    "leased",
    "resetting",
    "needs_review",
    "deleting",
    "broken",
  ]),
  profile_generation: z.number().int().nonnegative(),
  current_run_id: id.nullable().optional(),
  last_used_at: z.string().nullable().optional(),
  lease: z
    .object({
      attempt_id: id,
      expires_at: z.string(),
    })
    .nullable()
    .optional(),
});
export type BrowserEnvironment = z.infer<typeof EnvironmentSchema>;
export const SkillVersionSchema = z.object({
  id,
  skill_id: id,
  name: z.string(),
  project_id: id.optional(),
  version: z.number().int().positive(),
  content_digest: z.string(),
  status: z.enum(["draft", "published", "revoked"]),
  files: z.array(z.object({ path: z.string(), content: z.string() })),
});
export const SkillSchema = z.object({
  id,
  project_id: id,
  name: z.string(),
  versions: z.array(SkillVersionSchema),
});
const relativePath = z
  .string()
  .min(1)
  .max(500)
  .refine(
    (path) =>
      !/[\x00-\x1f\\:]/.test(path) &&
      path.split("/").every((part) => part && part !== "." && part !== ".."),
    "Use package-relative paths",
  );
const skillNameHelp =
  "Use 1-128 characters: start with a letter or digit, then use only letters, digits, dots, underscores or hyphens. No spaces.";
export const SharedSkillNameSchema = z
  .string()
  .min(1, skillNameHelp)
  .max(128, skillNameHelp)
  .regex(/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/, skillNameHelp)
  // JavaScript's $ also matches before a final newline; Go's anchor does not.
  .refine((name) => !/\s/.test(name), skillNameHelp)
  .describe(skillNameHelp);
export const SkillUploadSchema = z
  .strictObject({
    name: SharedSkillNameSchema,
    files: z
      .array(
        z.strictObject({
          path: relativePath,
          content: z.string().max(1024 * 1024),
        }),
      )
      .max(100),
    archive_base64: z
      .string()
      .max(3 * 1024 * 1024)
      .regex(/^[A-Za-z0-9+/]+={0,2}$/)
      .optional(),
  })
  .refine(
    ({ files, archive_base64 }) =>
      archive_base64
        ? files.length === 0
        : files.some((file) => file.path === "SKILL.md") &&
          new Set(files.map((file) => file.path.toLowerCase())).size ===
            files.length &&
          files.reduce(
            (bytes, file) =>
              bytes + new TextEncoder().encode(file.content).length,
            0,
          ) <=
            2 * 1024 * 1024,
    "Provide root SKILL.md, unique paths and at most 2 MiB",
  );
const safeKey = z
  .string()
  .regex(/^[A-Za-z][A-Za-z0-9_.-]*$/)
  .refine(
    (key) =>
      ![
        "command",
        "args",
        "env",
        "executable_ref",
        "__proto__",
        "constructor",
        "prototype",
      ].includes(key),
  );
export const McpBindingInputSchema = z.strictObject({
  mcp_version_id: id,
  config: z.record(
    safeKey,
    z.union([z.string().max(2000), z.number(), z.boolean()]),
  ),
  secret_refs: z.record(
    safeKey,
    z.string().regex(/^secret:\/\/[A-Za-z0-9][A-Za-z0-9/_.-]*$/),
  ),
  enabled: z.boolean(),
});
export const McpBindingSchema = McpBindingInputSchema.extend({
  id,
  project_id: id,
}).strip();
export const McpDefinitionSchema = z.object({
  id,
  name: z.string(),
  version: z.number().int().positive(),
  config_schema: z.object({
    properties: z
      .record(
        z.string(),
        z.object({
          type: z.string().optional(),
          description: z.string().optional(),
        }),
      )
      .default({}),
    required: z.array(z.string()).default([]),
  }),
  secret_schema: z.object({
    properties: z
      .record(
        z.string(),
        z.object({
          type: z.string().optional(),
          description: z.string().optional(),
        }),
      )
      .default({}),
    required: z.array(z.string()).default([]),
  }),
  revoked_at: z.string().nullable().optional(),
});
const normalizedWorker = z.object({
  id,
  role: z.enum(["runtime", "browser"]),
  version: z.string(),
  capacity: z.number().nonnegative(),
  active: z.number().nonnegative(),
  drain: z.boolean(),
  last_seen_at: z.string(),
  alive: z.boolean().optional(),
  project_id: z.string().optional(),
});
export const WorkerSchema = z.union([
  z
    .object({
      id,
      role: z.enum(["runtime", "browser"]),
      version: z.string(),
      capacity: z.number().nonnegative(),
      in_use: z.number().nonnegative(),
      draining: z.boolean(),
      last_seen_at: z.string(),
      alive: z.boolean(),
      project_id: z.string().optional(),
    })
    .transform(({ in_use, draining, ...worker }) => ({
      ...worker,
      active: in_use,
      drain: draining,
    })),
  normalizedWorker,
]);
const serviceObservability = z.record(
  z.string(),
  z.object({
    status: z.string(),
    scope: z.string(),
    reason: z.string().optional(),
    value: z.number().nullable(),
    consumer_group: z.string().optional(),
    topic: z.string().optional(),
    observed_at: z.string().optional(),
  }),
);
const registryAudit = z.array(
  z.object({
    id: z.number().int(),
    action: z.string(),
    resource_id: z.string(),
    occurred_at: z.string(),
  }),
);
const serviceDetails = {
  observability: serviceObservability.optional(),
  recent_registry_audit: registryAudit.optional(),
  dlq_scope: z.string().optional(),
};
const normalizedServices = z.object({
  workers: z.array(WorkerSchema),
  components: z.array(
    z.object({
      name: z.string(),
      status: z.string(),
      reason: z.string().optional(),
    }),
  ),
  metrics: z.record(z.string(), z.number().nullable()),
  ...serviceDetails,
});
export const ServicesSchema = z.union([
  z
    .object({
      workers: z.array(WorkerSchema),
      runs: z.record(z.string(), z.number()),
      environments: z.record(z.string(), z.number()),
      active_leases: z.number(),
      expired_leases: z.number(),
      lost_attempts: z.number(),
      pending_outbox: z.number(),
      oldest_outbox_seconds: z.number().nullable(),
      dlq_count: z.number().nullable().optional(),
      ...serviceDetails,
      dependencies: z.record(
        z.string(),
        z.object({
          status: z.string(),
          mode: z.string().optional(),
          reason: z.string().optional(),
        }),
      ),
    })
    .transform((view) => ({
      workers: view.workers,
      components: Object.entries(view.dependencies).map(
        ([name, dependency]) => ({
          name,
          status: `${dependency.status}${dependency.mode ? ` (${dependency.mode})` : ""}`,
          ...(dependency.reason ? { reason: dependency.reason } : {}),
        }),
      ),
      metrics: {
        ...Object.fromEntries(
          Object.entries(view.runs).map(([name, count]) => [
            `runs.${name}`,
            count,
          ]),
        ),
        ...Object.fromEntries(
          Object.entries(view.environments).map(([name, count]) => [
            `environments.${name}`,
            count,
          ]),
        ),
        active_leases: view.active_leases,
        expired_leases: view.expired_leases,
        lost_attempts: view.lost_attempts,
        pending_outbox: view.pending_outbox,
        oldest_outbox_seconds: view.oldest_outbox_seconds,
        ...(view.dlq_count === undefined ? {} : { dlq_count: view.dlq_count }),
      },
      ...(view.observability === undefined
        ? {}
        : { observability: view.observability }),
      ...(view.recent_registry_audit === undefined
        ? {}
        : { recent_registry_audit: view.recent_registry_audit }),
      ...(view.dlq_scope === undefined ? {} : { dlq_scope: view.dlq_scope }),
    })),
  normalizedServices,
]);
export const ControlSessionSchema = z.object({
  id,
  project_id: id.optional(),
  environment_id: id,
  expires_at: z.string(),
  state: z.string(),
  control_url: z.string().optional(),
  token: z.string().max(128).optional(),
});
