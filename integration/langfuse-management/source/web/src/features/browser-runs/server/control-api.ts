import { TRPCError } from "@trpc/server";
import { z } from "zod/v4";
import type { BridgeConfig } from "../contracts";

export const bridgeHeaders = (config: BridgeConfig) => ({
  Authorization: `Bearer ${config.bridgeToken}`,
  "X-Agenttrace-Project-Id": config.agenttraceProjectId,
  "X-Langfuse-Project-Id": config.langfuseProjectId,
  "X-Agenttrace-Role": config.role,
  "X-Agenttrace-Actor-Id": config.actorId,
});

export class ControlApi {
  constructor(
    protected readonly config: BridgeConfig,
    protected readonly fetchImplementation: typeof fetch = globalThis.fetch,
  ) {}

  get headers() {
    return bridgeHeaders(this.config);
  }
  get apiUrl() {
    return this.config.apiUrl;
  }
  protected projectPath(resource: string) {
    return `/api/v1/projects/${encodeURIComponent(this.config.agenttraceProjectId)}/${resource}`;
  }
  protected assertProject(resource: { project_id: string }) {
    if (resource.project_id !== this.config.agenttraceProjectId)
      throw new TRPCError({ code: "NOT_FOUND" });
  }
  async verifyBinding() {
    const binding = await this.request(
      this.projectPath("binding"),
      z.union([
        z
          .object({
            agenttrace_project_id: z.string(),
            langfuse_project_id: z.string(),
            status: z.string(),
          })
          .transform(({ agenttrace_project_id, ...rest }) => ({
            ...rest,
            project_id: agenttrace_project_id,
          })),
        z.object({
          project_id: z.string(),
          langfuse_project_id: z.string(),
          status: z.string(),
        }),
      ]),
    );
    this.assertProject(binding);
    if (
      binding.langfuse_project_id !== this.config.langfuseProjectId ||
      binding.status !== "active"
    )
      throw new TRPCError({ code: "NOT_FOUND" });
  }

  protected async request<T>(
    pathname: string,
    schema: z.ZodType<T>,
    init: RequestInit = {},
    allowNoContent = false,
  ): Promise<T> {
    let response: Response;
    try {
      response = await this.fetchImplementation(
        new URL(pathname, this.config.apiUrl),
        {
          ...init,
          redirect: "error",
          signal: init.signal ?? AbortSignal.timeout(15000),
          headers: {
            Accept: "application/json",
            ...bridgeHeaders(this.config),
            ...(init.body ? { "Content-Type": "application/json" } : {}),
          },
        },
      );
    } catch {
      throw new TRPCError({ code: "INTERNAL_SERVER_ERROR" });
    }
    if (!response.ok) {
      const code =
        response.status === 404
          ? "NOT_FOUND"
          : response.status === 409
            ? "CONFLICT"
            : response.status === 400 || response.status === 422
              ? "BAD_REQUEST"
              : response.status === 401 || response.status === 403
                ? "PRECONDITION_FAILED"
                : "INTERNAL_SERVER_ERROR";
      throw new TRPCError({ code });
    }
    if (allowNoContent && response.status === 204) return undefined as T;
    try {
      return schema.parse(await response.json());
    } catch {
      throw new TRPCError({ code: "INTERNAL_SERVER_ERROR" });
    }
  }
}
