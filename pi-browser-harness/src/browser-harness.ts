import { fileURLToPath } from "node:url";
import type { HarnessDefinition } from "./harness-definition.js";
import { createTraceMapperState, mapPiEvent } from "./trace-events.js";
import { deriveTerminalVerdict, validateBrowserEventPayload } from "./browser-protocol.js";

/** Browser capability behavior only; selected snapshots own prompts and Skills. */
export function createBrowserHarnessDefinition(mcpChildPath?: string): HarnessDefinition {
  return {
    systemPrompt: "",
    skillPaths: [],
    toolAllowlist: ["mcp__browser_harness__*"],
    mcpServers: (request) => {
      if (!request.worker_url?.trim()) throw new Error("browser harness requires worker_url");
      if (!request.artifact_scope?.trim()) throw new Error("browser harness requires artifact_scope");
      try {
        const url = new URL(request.worker_url);
        if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("invalid protocol");
      } catch {
        throw new Error("browser harness worker_url must be an absolute HTTP URL");
      }
      if (!request.project_id?.trim() || !request.environment_id?.trim() ||
        !Number.isSafeInteger(request.profile_generation) || request.profile_generation! < 1 ||
        !Number.isSafeInteger(request.fencing_token) || request.fencing_token! < 1) {
        throw new Error("browser harness requires complete managed environment lease identity");
      }
      const providerToken = process.env.AGENTTRACE_BROWSER_PROVIDER_TOKEN;
      if (!providerToken?.trim()) throw new Error("browser harness requires host provider token");
      return [{
        name: "browser-harness", command: process.execPath,
        args: [mcpChildPath ?? fileURLToPath(new URL("./mcp/server.js", import.meta.url))],
        env: {
          AGENTTRACE_HARNESS_WORKER_URL: request.worker_url,
          AGENTTRACE_HARNESS_ARTIFACT_SCOPE: request.artifact_scope,
          AGENTTRACE_HARNESS_GOAL: request.goal,
          AGENTTRACE_HARNESS_PROJECT_ID: request.project_id,
          AGENTTRACE_HARNESS_ENVIRONMENT_ID: request.environment_id,
          AGENTTRACE_HARNESS_PROFILE_GENERATION: String(request.profile_generation),
          AGENTTRACE_HARNESS_FENCING_TOKEN: String(request.fencing_token),
          AGENTTRACE_BROWSER_PROVIDER_TOKEN: providerToken,
        },
        exposure: "direct", executionMode: "sequential",
        hostOnlyTools: ["close_session"],
      }];
    },
    cleanup: async ({ callMcpTool }) => {
      const result = await callMcpTool("mcp__browser_harness__close_session", {});
      if ((result as { structuredContent?: { closed?: unknown } } | null)
        ?.structuredContent?.closed !== true) {
        throw new Error("browser cleanup acknowledgement is missing");
      }
    },
    completionEventTypes: ["browser.case.completed"],
    createEventMapper: () => {
      const state = createTraceMapperState();
      return (event) => {
        const mapped = mapPiEvent(event as Parameters<typeof mapPiEvent>[0], state);
        for (const item of mapped) validateBrowserEventPayload(item.type, item.payload);
        return mapped;
      };
    },
    completionPolicy: (context) => ({
      verdict: deriveTerminalVerdict({
        assistant_settled: context.assistant_settled && !context.aborted,
        browser_case_verdicts: context.events
          .filter((event) => event.type === "browser.case.completed")
          .map((event) => (event.payload as { verdict: "passed" | "failed" }).verdict),
      }),
    }),
  };
}
