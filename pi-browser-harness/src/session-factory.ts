import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import type { Writable } from "node:stream";
import type { HarnessDefinition } from "./harness-definition.js";
import type { PiSessionDeps } from "./pi-session.js";
import type { McpResolutionOptions } from "./mcp-capability-resolver.js";
import type { RunRequest } from "./run-protocol.js";
import { SessionCleanupError, type ExecutionSessionFactory } from "./server.js";

/** Only a trusted startup environment value can load executable harness code. */
export async function loadHarnessDefinition(
  modulePath = process.env["AGENTTRACE_HARNESS_MODULE"],
): Promise<HarnessDefinition> {
  if (!modulePath?.trim()) {
    return (await import("./browser-harness.js")).createBrowserHarnessDefinition();
  }
  const specifier = modulePath.startsWith("file:")
    ? modulePath : pathToFileURL(resolve(modulePath)).href;
  const definition: unknown = (await import(specifier)).default;
  if (!definition || typeof definition !== "object" ||
    typeof (definition as HarnessDefinition).systemPrompt !== "string" ||
    !Array.isArray((definition as HarnessDefinition).skillPaths) ||
    !Array.isArray((definition as HarnessDefinition).toolAllowlist) ||
    !(Array.isArray((definition as HarnessDefinition).mcpServers) ||
      typeof (definition as HarnessDefinition).mcpServers === "function") ||
    typeof (definition as HarnessDefinition).completionPolicy !== "function") {
    throw new Error("AGENTTRACE_HARNESS_MODULE must default export a HarnessDefinition");
  }
  return definition as HarnessDefinition;
}

export async function createExecutionSessionFactory(options: {
  definition?: HarnessDefinition;
  stderr?: Pick<Writable, "write">;
  createAgentSession?: PiSessionDeps["createAgentSession"];
  createTransport?: PiSessionDeps["createTransport"];
  transportCloseTimeoutMs?: number;
  mcpResolution?: McpResolutionOptions;
} = {}): Promise<ExecutionSessionFactory> {
  const prepareResources = async (request: RunRequest) =>
    (await import("./harness-resources.js")).prepareHarnessResources(request, options.mcpResolution);
  let legacyDefinition: Promise<HarnessDefinition> | undefined;
  return async (request, { eventWriter, signal }) => {
    const runtime = request.execution?.runtime_id ?? "pi";
    if (runtime === "claude-code") {
      const { createClaudeSession } = await import("./claude-session.js");
      return createClaudeSession(request, { eventWriter, signal,
        ...(options.mcpResolution ? { prepareHarnessResources: prepareResources } : {}) });
    }
    if (runtime === "codex") {
      const { createCodexSession } = await import("./codex-session.js");
      return createCodexSession(request, { eventWriter, signal,
        ...(options.mcpResolution ? { prepareHarnessResources: prepareResources } : {}) });
    }
    if (runtime !== "pi") throw new Error(`unsupported runtime: ${runtime}`);
    const { ModelRuntime, createAgentSession } = await import("@earendil-works/pi-coding-agent");
    const { createPiSession } = await import("./pi-session.js");
    const resources = request.execution
      ? await prepareResources(request)
      : undefined;
    try {
      const definition = resources?.definition ?? await (legacyDefinition ??=
        options.definition ? Promise.resolve(options.definition) : loadHarnessDefinition());
      const session = await createPiSession(request, {
        createAgentSession: options.createAgentSession ?? createAgentSession,
        // Providers are mutable registrations: never share across Attempts.
        modelRuntime: await ModelRuntime.create({ refreshOnCreate: false }),
        eventWriter, signal,
        ...(resources ? { cwd: resources.cwd } : {}),
        ...(options.stderr ? { stderr: options.stderr } : {}),
        ...(options.createTransport ? { createTransport: options.createTransport } : {}),
        ...(options.transportCloseTimeoutMs !== undefined
          ? { transportCloseTimeoutMs: options.transportCloseTimeoutMs } : {}),
      }, definition);
      let closing: Promise<void> | undefined;
      return {
        prompt: session.prompt,
        abort: session.abort,
        close: () => closing ??= (async () => {
          try { await session.close(); } catch (error) {
            if (!(error instanceof SessionCleanupError)) await resources?.dispose();
            throw error;
          }
          await resources?.dispose();
        })(),
      };
    } catch (error) {
      if (!(error instanceof SessionCleanupError)) await resources?.dispose();
      throw error;
    }
  };
}
