import { TRPCError } from "@trpc/server";
import { z } from "zod/v4";
import {
  CreateRunSchema,
  RuntimeSchema,
  TraceSchema,
  RunSchema,
  type AgentTraceRun,
  type CreateRunInput,
} from "@/src/features/browser-runs/contracts";
import { ManagementClient } from "./management-client";

const RuntimesResponseSchema = z.object({ runtimes: z.array(RuntimeSchema) });

const ConversationResponseSchema = z.object({
  conversation: z.object({
    id: z.string(),
    project_id: z.string(),
  }),
  messages: z.array(z.unknown()),
  runs: z.array(RunSchema),
});

const AcceptedRunSchema = z.object({
  run_id: z.string(),
  status: z.string(),
  message_id: z.string().optional(),
  retry_of_run_id: z.string().optional(),
});

const AuthorizedEventsSchema = z.object({ runId: z.string() });

const fail = (code: TRPCError["code"]): never => {
  throw new TRPCError({ code });
};

export function assertRunProject(
  run: {
    project_id: string;
    harness_snapshot?: { project_id?: string };
  },
  expectedProjectId: string,
): void {
  if (
    run.project_id !== expectedProjectId ||
    (run.harness_snapshot?.project_id !== undefined &&
      run.harness_snapshot.project_id !== expectedProjectId)
  ) {
    fail("NOT_FOUND");
  }
}

export class AgentTraceClient extends ManagementClient {
  async listRuns(conversationId: string): Promise<AgentTraceRun[]> {
    const response = await this.request(
      `/api/v1/conversations/${encodeURIComponent(conversationId)}`,
      ConversationResponseSchema,
    );
    if (
      response.conversation.project_id !== this.config.agenttraceProjectId ||
      response.conversation.id !== conversationId
    ) {
      fail("NOT_FOUND");
    }
    for (const run of response.runs) {
      this.assertRunScope(run);
      if (run.conversation_id !== conversationId) fail("NOT_FOUND");
    }
    return response.runs;
  }

  async createRun(input: CreateRunInput) {
    this.assertLangfuseProject(input.projectId);
    const parsed = CreateRunSchema.safeParse(input);
    if (!parsed.success) return fail("BAD_REQUEST");
    const { conversationId, url, goal, runtimeId, harnessId, idempotencyKey } =
      parsed.data;
    const [runtimes, harnesses] = await Promise.all([
      this.listRuntimes(),
      this.listHarnesses(),
      this.listRuns(conversationId),
    ]);
    if (
      !runtimes.some(
        (runtime) => runtime.id === runtimeId && runtime.available,
      ) ||
      !harnesses.some(
        (harness) =>
          harness.id === harnessId &&
          !harness.revoked_at &&
          harness.runtime_ids.includes(runtimeId),
      )
    )
      fail("BAD_REQUEST");
    return this.request(
      `/api/v1/conversations/${encodeURIComponent(conversationId)}/messages`,
      AcceptedRunSchema,
      {
        method: "POST",
        body: JSON.stringify({
          content: url ? `Open ${url}. ${goal}` : goal,
          runtime_id: runtimeId,
          harness_id: harnessId,
          idempotency_key: idempotencyKey,
        }),
      },
    );
  }

  async getRun(runId: string): Promise<AgentTraceRun> {
    const run = await this.request(
      `/api/v1/runs/${encodeURIComponent(runId)}`,
      RunSchema,
    );
    this.assertRunScope(run);
    if (run.id !== runId) fail("NOT_FOUND");
    return run;
  }

  async getTrace(runId: string) {
    await this.assertOwnedRun(runId);
    const trace = await this.request(
      `/api/v1/runs/${encodeURIComponent(runId)}/trace`,
      TraceSchema,
    );
    if (trace.spans.some((span) => span.run_id !== runId)) fail("NOT_FOUND");
    return trace;
  }

  async listRuntimes() {
    return (await this.request("/api/v1/runtimes", RuntimesResponseSchema))
      .runtimes;
  }

  async cancelRun(runId: string) {
    await this.assertOwnedRun(runId);
    return this.request(
      `/api/v1/runs/${encodeURIComponent(runId)}/cancel`,
      AcceptedRunSchema,
      { method: "POST", body: "{}" },
    );
  }

  async rerun(runId: string, idempotencyKey: string) {
    await this.assertOwnedRun(runId);
    if (!idempotencyKey.trim()) {
      fail("BAD_REQUEST");
    }
    return this.request(
      `/api/v1/runs/${encodeURIComponent(runId)}/reruns`,
      AcceptedRunSchema,
      {
        method: "POST",
        body: JSON.stringify({ idempotency_key: idempotencyKey }),
      },
    );
  }

  async authorizeEvents(runId: string) {
    await this.assertOwnedRun(runId);
    return AuthorizedEventsSchema.parse({ runId });
  }

  private async assertOwnedRun(runId: string): Promise<void> {
    await this.getRun(runId);
  }

  private assertRunScope(run: AgentTraceRun): void {
    assertRunProject(run, this.config.agenttraceProjectId);
  }

  private assertLangfuseProject(projectId: string): void {
    if (projectId !== this.config.langfuseProjectId) {
      fail("NOT_FOUND");
    }
  }
}

export const createAgentTraceClient = (
  config: import("../contracts").BridgeConfig,
) => new AgentTraceClient(config);
