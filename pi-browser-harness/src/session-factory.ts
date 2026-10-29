import type { Writable } from "node:stream";
import { parseExecutionSpec } from "./execution-spec.js";
import type { PiSessionDeps } from "./pi-session.js";
import type { McpResolutionOptions } from "./mcp-capability-resolver.js";
import type { RunRequest } from "./run-protocol.js";
import { SessionCleanupError, type ExecutionSessionFactory } from "./server.js";

export async function createExecutionSessionFactory(options: {
  stderr?: Pick<Writable, "write">;
  createAgentSession?: PiSessionDeps["createAgentSession"];
  createTransport?: PiSessionDeps["createTransport"];
  transportCloseTimeoutMs?: number;
  mcpResolution?: McpResolutionOptions;
} = {}): Promise<ExecutionSessionFactory> {
  const prepareResources = async (request: RunRequest) =>
    (await import("./harness-resources.js")).prepareHarnessResources(request, options.mcpResolution);
  return async (request, { eventWriter, signal }) => {
    request = { ...request, execution: parseExecutionSpec(request.execution) };
    const runtime = request.execution.runtime_id;
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
    const { ModelRuntime, createAgentSession } = await import("@earendil-works/pi-coding-agent");
    const { createPiSession } = await import("./pi-session.js");
    const resources = await prepareResources(request);
    try {
      const session = await createPiSession(request, {
        createAgentSession: options.createAgentSession ?? createAgentSession,
        // Providers are mutable registrations: never share across Attempts.
        modelRuntime: await ModelRuntime.create({ refreshOnCreate: false }),
        eventWriter, signal,
        cwd: resources.cwd,
        ...(options.stderr ? { stderr: options.stderr } : {}),
        ...(options.createTransport ? { createTransport: options.createTransport } : {}),
        ...(options.transportCloseTimeoutMs !== undefined
          ? { transportCloseTimeoutMs: options.transportCloseTimeoutMs } : {}),
      }, resources.definition);
      let closing: Promise<void> | undefined;
      return {
        prompt: session.prompt,
        abort: session.abort,
        close: () => closing ??= (async () => {
          try { await session.close(); } catch (error) {
            if (!(error instanceof SessionCleanupError)) await resources.dispose();
            throw error;
          }
          await resources.dispose();
        })(),
      };
    } catch (error) {
      if (!(error instanceof SessionCleanupError)) await resources.dispose();
      throw error;
    }
  };
}
