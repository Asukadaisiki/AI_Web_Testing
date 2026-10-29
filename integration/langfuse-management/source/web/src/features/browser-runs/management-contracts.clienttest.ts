import { ServicesSchema, SkillUploadSchema } from "./management-contracts";

const files = [{ path: "SKILL.md", content: "# Test\nInstructions" }];

describe("Skill upload name contract", () => {
  it.each(["A", "0", "Login.v2_test-1", "a".repeat(128)])(
    "preserves valid name %s",
    (name) => {
      expect(SkillUploadSchema.parse({ name, files }).name).toBe(name);
    },
  );
  it.each([
    "",
    " ",
    "bad name",
    " leading",
    "trailing ",
    "name\n",
    "name\t",
    ".hidden",
    "-leading",
    "_leading",
    "bad/name",
    "caf\u00e9",
    "a".repeat(129),
  ])("rejects invalid name without normalization: %j", (name) => {
    const result = SkillUploadSchema.safeParse({ name, files });
    expect(result.success).toBe(false);
    if (!result.success) {
      expect(
        result.error.issues.some((issue) => issue.path[0] === "name"),
      ).toBe(true);
    }
  });
});

const servicesWire = {
  workers: [],
  runs: { queued: 3 },
  environments: { ready: 1 },
  active_leases: 1,
  expired_leases: 0,
  lost_attempts: 2,
  pending_outbox: 4,
  oldest_outbox_seconds: null,
  dlq_count: 2,
  dlq_scope: "project_retained_outbox_handoffs",
  dependencies: {
    api: { status: "ok" },
    redis: {
      status: "degraded",
      mode: "postgres_poll",
      reason: "Redis probe failed; durable polling remains available.",
    },
  },
  observability: {
    kafka_consumer_lag: {
      status: "unmeasured",
      scope: "deployment",
      reason: "Consumer offset measurement is not configured.",
      value: null,
    },
    outbox_relay_health: {
      status: "degraded",
      scope: "project_queue",
      reason: "Queue age is measured, not relay process liveness.",
      value: 95,
    },
  },
  recent_registry_audit: [
    {
      id: 42,
      project_id: "p",
      actor_id: "actor",
      action: "skill.publish",
      resource_id: "sv-1",
      occurred_at: "2026-10-28T12:00:00Z",
      private_payload: "must-not-leak",
    },
  ],
};

describe("Services measured-data projection", () => {
  it("retains measured DLQ, nullable metrics, scopes, health reasons and audit", () => {
    const view = ServicesSchema.parse(servicesWire);
    expect(view).toMatchObject({
      metrics: {
        "runs.queued": 3,
        pending_outbox: 4,
        dlq_count: 2,
        oldest_outbox_seconds: null,
      },
      dlq_scope: "project_retained_outbox_handoffs",
      components: [
        { name: "api", status: "ok" },
        {
          name: "redis",
          status: "degraded (postgres_poll)",
          reason: "Redis probe failed; durable polling remains available.",
        },
      ],
      observability: {
        kafka_consumer_lag: { value: null, status: "unmeasured" },
        outbox_relay_health: { value: 95, scope: "project_queue" },
      },
      recent_registry_audit: [
        { id: 42, action: "skill.publish", resource_id: "sv-1" },
      ],
    });
    expect(JSON.stringify(view)).not.toContain("must-not-leak");
  });
  it("preserves a real zero DLQ and an unavailable DLQ as distinct values", () => {
    expect(
      ServicesSchema.parse({ ...servicesWire, dlq_count: 0 }),
    ).toMatchObject({ metrics: { dlq_count: 0 } });
    expect(
      ServicesSchema.parse({ ...servicesWire, dlq_count: null }),
    ).toMatchObject({ metrics: { dlq_count: null } });
  });
  it("preserves additive heartbeat and lease measurements without requiring raw telemetry", () => {
    const observations = {
      exporter_health: {
        status: "healthy",
        scope: "deployment_process",
        reason: "Fresh heartbeat; process liveness only.",
        value: 1,
        observed_at: "2026-10-28T13:00:00Z",
      },
      outbox_relay_process_health: {
        status: "stale",
        scope: "deployment_process",
        reason: "Heartbeat expired.",
        value: 0,
      },
      lease_contention: {
        status: "measured",
        scope: "project",
        reason: "Durable conflict count.",
        value: 3,
      },
      stale_fencing_rejections: {
        status: "unmeasured",
        scope: "project",
        reason: "No durable measurement is available yet.",
        value: null,
      },
    };
    const view = ServicesSchema.parse({
      ...servicesWire,
      observability: observations,
      service_health: [{ role: "exporter", private_endpoint: "internal-only" }],
      lease_contention: 3,
      stale_fencing_rejections: null,
    });
    expect(view.observability).toEqual(observations);
    expect(view).not.toHaveProperty("service_health");
    expect(JSON.stringify(view)).not.toContain("internal-only");
  });
  it("keeps normalized legacy responses compatible without inventing metrics", () => {
    expect(
      ServicesSchema.parse({ workers: [], components: [], metrics: {} }),
    ).toEqual({ workers: [], components: [], metrics: {} });
  });
  it("accepts measured Kafka lag with omitted reason and retains its observation metadata", () => {
    const view = ServicesSchema.parse({
      ...servicesWire,
      observability: {
        kafka_consumer_lag: {
          status: "measured",
          scope: "deployment_consumer_group",
          value: 0,
          consumer_group: "configured-runtime",
          topic: "configured-commands",
          observed_at: "2026-10-28T12:00:00Z",
        },
      },
    });
    expect(view).toMatchObject({
      observability: {
        kafka_consumer_lag: {
          value: 0,
          consumer_group: "configured-runtime",
          topic: "configured-commands",
          observed_at: "2026-10-28T12:00:00Z",
        },
      },
    });
  });
});
