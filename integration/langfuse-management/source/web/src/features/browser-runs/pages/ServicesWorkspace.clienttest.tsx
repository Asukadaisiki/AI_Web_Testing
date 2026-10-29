import { fireEvent, render, screen, within } from "@testing-library/react";
import { ServicesSchema } from "../management-contracts";
import { ServicesWorkspace } from "./ServicesWorkspace";

const mocks = vi.hoisted(() => ({
  data: undefined as unknown,
  refetch: vi.fn(),
  drain: vi.fn(),
  isError: false,
}));
vi.mock("@/src/utils/api", () => ({
  api: {
    useUtils: () => ({
      browserRuns: { services: { invalidate: vi.fn() } },
    }),
    browserRuns: {
      services: {
        useQuery: () => ({
          data: mocks.data,
          isError: mocks.isError,
          isPending: false,
          refetch: mocks.refetch,
        }),
      },
      drainWorker: {
        useMutation: () => ({
          mutate: mocks.drain,
          isPending: false,
          isError: false,
        }),
      },
    },
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.isError = false;
  mocks.data = ServicesSchema.parse({
    workers: [],
    runs: {},
    environments: {},
    active_leases: 0,
    expired_leases: 0,
    lost_attempts: 0,
    pending_outbox: 1,
    oldest_outbox_seconds: null,
    dlq_count: 2,
    dlq_scope: "project_retained_outbox_handoffs",
    dependencies: {
      redis: {
        status: "degraded",
        mode: "postgres_poll",
        reason: "Durable polling remains available.",
      },
    },
    observability: {
      kafka_consumer_lag: {
        status: "unmeasured",
        scope: "deployment",
        reason: "Consumer offsets are not configured.",
        value: null,
      },
      outbox_relay_health: {
        status: "degraded",
        scope: "project_queue",
        reason: "Measured queue age, not relay process liveness.",
        value: 95,
      },
    },
    recent_registry_audit: [
      {
        id: 42,
        action: "skill.publish",
        resource_id: "sv-1",
        occurred_at: "2026-10-28T12:00:00Z",
      },
    ],
  });
});

it("displays measured DLQ, nullable Kafka with reasons and recent registry audit", () => {
  render(<ServicesWorkspace projectId="lf" />);
  const dlq = screen.getByText("dlq_count").parentElement!;
  expect(within(dlq).getByText("2")).toBeInTheDocument();
  expect(
    screen.getByText(/project_retained_outbox_handoffs/),
  ).toBeInTheDocument();
  expect(
    screen.getByText("Durable polling remains available."),
  ).toBeInTheDocument();
  const kafka = screen.getByText("kafka_consumer_lag").parentElement!;
  expect(within(kafka).getByText("Unavailable")).toBeInTheDocument();
  expect(within(kafka).queryByText("0")).not.toBeInTheDocument();
  expect(within(kafka).getByText("unmeasured")).toBeInTheDocument();
  expect(
    within(kafka).getByText("Consumer offsets are not configured."),
  ).toBeInTheDocument();
  expect(within(kafka).getByText(/deployment/)).toBeInTheDocument();
  const relay = screen.getByText("outbox_relay_health").parentElement!;
  expect(within(relay).getByText("95")).toBeInTheDocument();
  expect(
    within(relay).getByText("Measured queue age, not relay process liveness."),
  ).toBeInTheDocument();
  expect(screen.getByText("skill.publish")).toBeInTheDocument();
  expect(screen.getByText("sv-1")).toBeInTheDocument();
  expect(screen.getByText("2026-10-28T12:00:00Z")).toBeInTheDocument();
});

it("renders a measured zero without labeling it unavailable", () => {
  mocks.data = ServicesSchema.parse({
    workers: [],
    components: [],
    metrics: { dlq_count: 0, kafka_consumer_lag: null },
  });
  render(<ServicesWorkspace projectId="lf" />);
  expect(
    within(screen.getByText("dlq_count").parentElement!).getByText("0"),
  ).toBeInTheDocument();
  expect(
    within(screen.getByText("kafka_consumer_lag").parentElement!).getByText(
      "Unavailable",
    ),
  ).toBeInTheDocument();
});

it("retains error recovery without fabricating services data", () => {
  mocks.data = undefined;
  mocks.isError = true;
  render(<ServicesWorkspace projectId="lf" />);
  expect(screen.getByRole("alert")).toBeInTheDocument();
  expect(screen.queryByText("dlq_count")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Refresh services" }));
  expect(mocks.refetch).toHaveBeenCalledOnce();
});

it("shows the configured Kafka measurement scope and observation time", () => {
  mocks.data = ServicesSchema.parse({
    workers: [],
    components: [],
    metrics: {},
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
  render(<ServicesWorkspace projectId="lf" />);
  const kafka = screen.getByText("kafka_consumer_lag").parentElement!;
  expect(within(kafka).getByText("0")).toBeInTheDocument();
  expect(within(kafka).getByText(/configured-runtime/)).toBeInTheDocument();
  expect(within(kafka).getByText(/configured-commands/)).toBeInTheDocument();
  expect(within(kafka).getByText(/2026-10-28T12:00:00Z/)).toBeInTheDocument();
});

it("renders additive exporter/relay heartbeats and durable lease counters", () => {
  mocks.data = ServicesSchema.parse({
    workers: [],
    components: [],
    metrics: {},
    observability: {
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
        value: 3,
      },
      stale_fencing_rejections: {
        status: "unmeasured",
        scope: "project",
        reason: "No durable measurement is available yet.",
        value: null,
      },
    },
  });
  render(<ServicesWorkspace projectId="lf" />);
  const exporter = screen.getByText("exporter_health").parentElement!;
  expect(within(exporter).getByText("healthy")).toBeInTheDocument();
  expect(
    within(exporter).getByText("Fresh heartbeat; process liveness only."),
  ).toBeInTheDocument();
  expect(
    within(exporter).getByText(/2026-10-28T13:00:00Z/),
  ).toBeInTheDocument();
  const relay = screen.getByText("outbox_relay_process_health").parentElement!;
  expect(within(relay).getByText("0")).toBeInTheDocument();
  expect(within(relay).getByText("stale")).toBeInTheDocument();
  expect(
    within(screen.getByText("lease_contention").parentElement!).getByText("3"),
  ).toBeInTheDocument();
  const fencing = screen.getByText("stale_fencing_rejections").parentElement!;
  expect(within(fencing).getByText("Unavailable")).toBeInTheDocument();
  expect(within(fencing).queryByText("0")).not.toBeInTheDocument();
});
