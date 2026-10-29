import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import type { HarnessDefinition, JsonObject, ScriptedResponse } from "./harness-definition.js";
import { createTraceMapperState, mapPiEvent } from "./trace-events.js";
import { deriveTerminalVerdict, validateBrowserEventPayload } from "./browser-protocol.js";

export function createBrowserHarnessDefinition(mcpChildPath?: string): HarnessDefinition {
  return {
    systemPrompt: readFileSync(new URL("../resources/system-prompt.md", import.meta.url), "utf8"),
    skillPaths: [fileURLToPath(new URL("../resources/skills", import.meta.url))],
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
      return [{
      name: "browser-harness", command: process.execPath,
      args: [mcpChildPath ?? fileURLToPath(new URL("./mcp/server.js", import.meta.url))],
      env: {
        AGENTTRACE_HARNESS_WORKER_URL: request.worker_url,
        AGENTTRACE_HARNESS_ARTIFACT_SCOPE: request.artifact_scope,
        AGENTTRACE_HARNESS_GOAL: request.goal,
        ...(request.environment_id ? {
          AGENTTRACE_HARNESS_PROJECT_ID: request.project_id!,
          AGENTTRACE_HARNESS_ENVIRONMENT_ID: request.environment_id,
          AGENTTRACE_HARNESS_PROFILE_GENERATION: String(request.profile_generation),
          AGENTTRACE_HARNESS_FENCING_TOKEN: String(request.fencing_token),
        } : {}),
        ...(process.env.AGENTTRACE_BROWSER_PROVIDER_TOKEN ? {
          AGENTTRACE_BROWSER_PROVIDER_TOKEN: process.env.AGENTTRACE_BROWSER_PROVIDER_TOKEN,
        } : {}),
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
    formatPrompt: (goal) => `/skill:browser-case-authoring ${goal}`,
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
    scriptedResponse,
  };
}

function scriptedResponse(goal: string, priorToolResults: number): ScriptedResponse {
  if (goal.includes("DoesNotExist")) {
    if (priorToolResults === 0) return {
      tool: { name: "mcp__browser_harness__open_page", arguments: {
        url: "http://fixture-site/index.html", intent: "Open the local catalog",
      } },
    };
    if (priorToolResults === 1) return {
      tool: { name: "mcp__browser_harness__assert_text", arguments: {
        text: "DoesNotExist", intent: "Verify the missing product is visible",
      } },
    };
    return { text: "Final: DoesNotExist was not found." };
  }
  const tools: { name: string; arguments: JsonObject }[] = [
    { name: "open_page", arguments: {
      url: "http://fixture-site/index.html", intent: "Open the local catalog",
    } },
    { name: "input", arguments: {
      target: { object: { text: "Search items" } }, value: "alpha", submit: true,
      intent: "Filter the catalog to Alpha", expect_value: "alpha", expect_gone: "Beta",
    } },
    { name: "click", arguments: {
      target: { object: { text: "Alpha" } }, intent: "Open the Alpha item",
      expect_url: "detail.html", expect_gone: "Loading details",
    } },
    { name: "input", arguments: {
      target: { object: { text: "Quantity" } }, value: "2",
      intent: "Set quantity to two", expect_value: "2",
    } },
    { name: "click", arguments: {
      target: { object: { text: "Add to cart" } }, intent: "Add Alpha to the cart", expect_text: "Added!",
    } },
    { name: "click", arguments: {
      target: { object: { text: "View Cart" } }, intent: "Open the cart", expect_url: "cart.html",
    } },
    { name: "assert_text", arguments: {
      text: "Alpha x 2", intent: "Verify the final cart quantity",
    } },
    { name: "finish_case", arguments: { name: "canonical" } },
  ];
  const tool = tools[priorToolResults];
  return tool
    ? { tool: { name: `mcp__browser_harness__${tool.name}`, arguments: tool.arguments } }
    : { text: "Final: Alpha x 2 is in the cart." };
}
