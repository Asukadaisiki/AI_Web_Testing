import { homedir } from "node:os";
import { resolve } from "node:path";
import type { Writable } from "node:stream";
import {
  DefaultResourceLoader,
  SessionManager,
  SettingsManager,
  loadSkillsFromDir,
  type CreateAgentSessionOptions,
  type CreateAgentSessionResult,
  type InlineExtension,
  type McpTransportFactory,
  type ModelRuntime,
  type ResourceLoader,
} from "@earendil-works/pi-coding-agent";
import { StdioTransport, StreamableHttpTransport } from "@earendil-works/pi-mcp";
import type { ThinkingLevel } from "@earendil-works/pi-agent-core";
import type { AssistantMessage } from "@earendil-works/pi-ai";
import type { HarnessCleanupContext, HarnessDefinition, ObservedEvent } from "./harness-definition.js";
import { SessionCleanupError, type ExecutionSession } from "./server.js";
import { createHarnessMcpToolsExtension, createHarnessExtension } from "./extension.js";
import { createStandardEventMapper, mapToolCompleted } from "./pi-events.js";
import { resolveModel } from "./model-provider.js";
import { HarnessEventWriter, type HarnessEventPayloads, type RunRequest } from "./run-protocol.js";

export interface PiSessionDeps {
  createAgentSession(options: CreateAgentSessionOptions): Promise<CreateAgentSessionResult>;
  modelRuntime: ModelRuntime;
  eventWriter: HarnessEventWriter;
  cwd?: string;
  stderr?: Pick<Writable, "write">;
  resourceLoader?: ResourceLoader;
  extensionFactories?: InlineExtension[];
  /** Supported Pi MCP transport factory; transports must report settlement via onClose. */
  createTransport?: McpTransportFactory;
  /** Maximum time to await a transport close acknowledgement. Default: 5000ms. */
  transportCloseTimeoutMs?: number;
  signal?: AbortSignal;
}

export interface BrowserSessionDeps extends PiSessionDeps {
  mcpChildPath?: string;
}

export interface PiSession extends ExecutionSession {
  getActiveToolNames(): string[];
  getVerdict(): "succeeded" | "failed";
  /** Legacy synchronous SDK disposal; owners must await close() for resource release. */
  dispose(): void;
}

export type BrowserSession = PiSession;

export async function createBrowserSession(
  request: RunRequest, deps: BrowserSessionDeps,
): Promise<BrowserSession> {
  const { createBrowserHarnessDefinition } = await import("./browser-harness.js");
  return createPiSession(request, deps, createBrowserHarnessDefinition(deps.mcpChildPath));
}

/** Adapts a selected harness to Pi. All behavior specific to tools lives in its definition. */
export async function createPiSession(
  request: RunRequest, deps: PiSessionDeps, definition: HarnessDefinition,
): Promise<PiSession> {
  deps.signal?.throwIfAborted();
  const cwd = deps.cwd ?? process.cwd();
  const model = resolveModel(deps.modelRuntime, request, definition);
  const settingsManager = SettingsManager.inMemory({
    retry: { enabled: true, maxRetries: 2, baseDelayMs: 100, maxAgentDelayMs: 1000 },
  });
  const servers = typeof definition.mcpServers === "function"
    ? definition.mcpServers(request) : definition.mcpServers;
  const evidenceGuard = servers.some((server) => server.name === "browser-harness")
    ? new (await import("./browser/evidence-guard.js")).BrowserEvidenceGuard() : undefined;
  const redact = <T>(value: T): T => evidenceGuard?.redact(value) ?? value;
  const eventWriter = evidenceGuard?.publicWriter(deps.eventWriter) ?? deps.eventWriter;
  const transportCloseTimeoutMs = deps.transportCloseTimeoutMs ?? 5_000;
  if (!Number.isFinite(transportCloseTimeoutMs) || transportCloseTimeoutMs <= 0) {
    throw new Error("transportCloseTimeoutMs must be positive and finite");
  }
  const transports: { close(): Promise<void> }[] = [];
  const expandHome = (path: string) => path === "~" ? homedir()
    : path.startsWith("~/") || (process.platform === "win32" && path.startsWith("~\\"))
      ? resolve(homedir(), path.slice(2)) : path;
  const createTransport: McpTransportFactory = (entry, directory, authProvider) => {
    const config = entry.config;
    // Use Pi's public native transports, leaving discovery and tool execution to Pi.
    const transport = deps.createTransport?.(entry, directory, authProvider) ??
      ("url" in config ? new StreamableHttpTransport({
        url: config.url, ...(config.headers ? { headers: { ...config.headers } } : {}),
        ...(authProvider ? { authProvider } : {}),
      }) : new StdioTransport({
        command: expandHome(config.command),
        ...(config.args ? { args: config.args.map(expandHome) } : {}),
        cwd: resolve(directory, expandHome(config.cwd ?? ".")),
        // Capability resolution already produced literal values, not Pi config syntax.
        ...(config.env ? { env: { ...config.env } } : {}), stderr: "pipe",
      }));
    const nativeClose = transport.close.bind(transport);
    const acknowledged = new Promise<void>((resolve) => { transport.onClose(resolve); });
    let transportClosing: Promise<void> | undefined;
    // The observer predates connect and outlives the client's own listener disposal.
    transport.close = () => {
      transportClosing ??= (async () => {
        let timer: ReturnType<typeof setTimeout> | undefined;
        try {
          await Promise.race([
            Promise.all([Promise.resolve().then(nativeClose), acknowledged]),
            new Promise<never>((_resolve, reject) => {
              timer = setTimeout(() => reject(new SessionCleanupError(
                `MCP transport close acknowledgement is missing: ${entry.name}`,
              )), transportCloseTimeoutMs);
            }),
          ]);
        } finally {
          if (timer) clearTimeout(timer);
        }
      })();
      // Pi suppresses client-close rejections; retain them for the host's close below.
      void transportClosing.catch(() => {});
      return transportClosing;
    };
    transports.push(transport);
    return transport;
  };
  const closeTransports = async () => {
    const outcomes = await Promise.allSettled(transports.map((transport) => transport.close()));
    const failures = outcomes.filter((outcome) => outcome.status === "rejected");
    if (failures.length) {
      throw new SessionCleanupError(failures.map((failure) =>
        failure.reason instanceof Error ? failure.reason.message : String(failure.reason)).join("; "));
    }
  };
  const resourceLoader = deps.resourceLoader ?? new DefaultResourceLoader({
    cwd, agentDir: cwd, settingsManager,
    noExtensions: true,
    disabledBuiltinExtensions: ["mcp"],
    extensionFactories: deps.extensionFactories ?? [
      ...(servers.length ? [createHarnessMcpToolsExtension(servers, { createTransport })] : []),
      createHarnessExtension(servers, definition.toolAllowlist),
    ],
    noSkills: true, noPromptTemplates: true, noThemes: true, noContextFiles: true,
    systemPromptOverride: () => definition.systemPrompt,
    appendSystemPromptOverride: () => [],
    skillsOverride: () => ({
      skills: definition.skillPaths.flatMap((dir) => loadSkillsFromDir({ dir, source: "harness" }).skills),
      diagnostics: [],
    }),
  });
  await resourceLoader.reload();
  deps.signal?.throwIfAborted();
  const { session } = await deps.createAgentSession({
    cwd, model, thinkingLevel: request.model.reasoning_effort as ThinkingLevel,
    modelRuntime: deps.modelRuntime, resourceLoader,
    tools: [...definition.toolAllowlist],
    sessionManager: SessionManager.inMemory(cwd, { id: request.attempt_id }),
    settingsManager,
  }).catch(async (error: unknown) => {
    try { await closeTransports(); } catch (cleanupError) {
      throw new SessionCleanupError(
        cleanupError instanceof Error ? cleanupError.message : String(cleanupError),
        { cause: error },
      );
    }
    throw error;
  });
  let modelInput: HarnessEventPayloads["pi.message.completed"]["input"];
  const originalStreamSimple = deps.modelRuntime.streamSimple;
  deps.modelRuntime.streamSimple = function(model, context, options) {
    // Capture the transformed model context only, never request options/auth headers.
    const privateContext = evidenceGuard?.modelContext(context) ?? context;
    modelInput = JSON.parse(JSON.stringify(redact(privateContext))) as typeof modelInput;
    return originalStreamSimple.call(this, model, privateContext, options);
  };

  let closing: Promise<void> | undefined;
  let disposed = false;
  let unsubscribe = () => {};
  let aborting: Promise<void> | undefined;
  let running: Promise<void> | undefined;
  const abort = (): Promise<void> => {
    aborting ??= Promise.resolve().then(() => session.abort());
    return aborting;
  };
  const onAbort = () => { void abort().catch(() => {}); };
  const dispose = () => {
    if (disposed) return;
    disposed = true;
    deps.modelRuntime.streamSimple = originalStreamSimple;
    unsubscribe();
    evidenceGuard?.discardPending();
    session.dispose();
  };
  const close = (): Promise<void> => {
    closing ??= (async () => {
      unsubscribe();
      deps.signal?.removeEventListener("abort", onAbort);
      const errors: string[] = [];
      const offErrors = session.extensionRunner?.onError?.((error) => { errors.push(error.error); });
      const hostCalls = new Map<string, Promise<unknown>>();
      const namespace = (name: string) => `mcp__${name.replace(/[^A-Za-z0-9_]/g, "_")}__`;
      const callMcpTool: HarnessCleanupContext["callMcpTool"] = (name, arguments_) => {
        const prior = hostCalls.get(name);
        if (prior) return prior;
        const calling = (async () => {
          const tool = session.extensionRunner.getToolDefinition(name);
          if (!name.startsWith("mcp__") || !tool) {
            throw new Error(`MCP cleanup tool is unavailable: ${name}`);
          }
          const id = `cleanup:${name}`;
          const result = await tool.execute(
            id, arguments_, undefined, undefined,
            session.extensionRunner.createToolContext(id, undefined),
          );
          if (result.isError) {
            throw new Error(result.content
              .filter((block) => block.type === "text")
              .map((block) => block.text).join("\n") || `MCP cleanup failed: ${name}`);
          }
          // Preserve the SDK envelope and injected legacy definitions' result shape.
          return result.structuredContent;
        })();
        hostCalls.set(name, calling);
        return calling;
      };
      const recordError = (error: unknown) => {
        errors.push(error instanceof Error ? error.message : String(error));
      };
      try {
        try {
          try { await abort(); } catch (error) { recordError(error); }
          // Join the exposed prompt too; a rejected model turn is not a cleanup failure.
          await running?.catch(() => {});
          if (definition.cleanup || servers.length) {
            // The supported MCP command awaits pending discovery, even if no model
            // prompt ran. Hidden lifecycle tools are available through the SDK.
            try { await session.prompt("/mcp"); } catch (error) { recordError(error); }
          }
          if (definition.cleanup) {
            try { await definition.cleanup({ callMcpTool }); } catch (error) { recordError(error); }
          }
          for (const server of servers) {
            try {
              const prefix = namespace(server.name);
              const wasCleaned = (server.hostOnlyTools ?? []).some((tool) =>
                hostCalls.has(`${prefix}${tool.replace(/[^A-Za-z0-9_]/g, "_")}`));
              const name = `${prefix}close_session`;
              const tool = session.extensionRunner?.getToolDefinition?.(name);
              if (wasCleaned || hostCalls.has(name) || !tool ||
                (!server.hostOnlyTools?.includes("close_session") &&
                  server.exposure !== "hidden" && tool.exposure !== "hidden")) continue;
              const result = await callMcpTool(name, {}) as {
                structuredContent?: { closed?: unknown }; closed?: unknown;
              } | undefined;
              if ((result?.structuredContent?.closed ?? result?.closed) !== true) {
                throw new Error(`MCP cleanup acknowledgement is missing: ${name}`);
              }
            } catch (error) { recordError(error); }
          }
        } finally {
          // SDK dispose() does not emit shutdown. MCP awaits transports and pending connects.
          try {
            await session.extensionRunner?.emit({ type: "session_shutdown", reason: "quit" });
          } catch (error) { recordError(error); }
          try { await closeTransports(); } catch (error) { recordError(error); }
        }
        if (errors.length) throw new SessionCleanupError(errors.join("; "));
      } finally {
        offErrors?.();
        dispose();
      }
    })();
    return closing;
  };
  deps.signal?.addEventListener("abort", onAbort, { once: true });
  try {
    deps.signal?.throwIfAborted();
    await session.bindExtensions({});
    deps.signal?.throwIfAborted();

    const map = definition.createEventMapper?.() ?? createStandardEventMapper();
    const events = new Map<ObservedEvent["type"], ObservedEvent>();
    let finalAssistant: AssistantMessage | undefined;
    let text = "";
    let verdict: "succeeded" | "failed" = "failed";
    unsubscribe = session.subscribe((event) => {
      if (eventWriter.hasTerminalEvent) return;
      try {
        evidenceGuard?.observe(event);
        if (event.type === "message_end" && event.message.role === "assistant") {
          // Retry attempts also emit message_end; only the latest outcome owns settlement.
          finalAssistant = event.message;
        }
        for (const item of map(event)) {
          // The selected completion policy, not the mapper, owns settlement.
          if (item.type === "harness.settled") continue;
          if (item.type === "pi.tool.completed" && event.type === "tool_execution_end" &&
            item.payload.tool_call_id === event.toolCallId && item.payload.tool_name === event.toolName) {
            const canonical = mapToolCompleted(event).payload;
            item.payload = {
              ...canonical, ...item.payload,
              ...(canonical.result !== undefined ? { result: canonical.result } : {}),
            };
          }
          if (item.type === "pi.message.completed") {
            text = item.payload.text;
            if (modelInput !== undefined) item.payload.input = modelInput;
          }
          item.payload = redact(item.payload);
          if (definition.completionEventTypes?.includes(item.type)) events.set(item.type, item);
          eventWriter.write(item.type, item.payload);
        }
        if (event.type === "agent_settled") {
          if (event.aborted || finalAssistant?.stopReason === "error" ||
            finalAssistant?.stopReason === "aborted" || finalAssistant?.errorMessage) {
            verdict = "failed";
            eventWriter.write("harness.failed", {
              error: redact(finalAssistant?.errorMessage ||
                (event.aborted || finalAssistant?.stopReason === "aborted"
                  ? "Pi assistant was aborted" : "Pi assistant failed")),
            });
            return;
          }
          const completion = definition.completionPolicy({
            assistant_settled: true, aborted: Boolean(event.aborted),
            assistant_text: text, events: [...events.values()],
          });
          verdict = completion.verdict;
          eventWriter.write("harness.settled", { assistant_text: text, ...completion });
        }
      } catch (error) {
        verdict = "failed";
        eventWriter.write("harness.failed", {
          error: redact(error instanceof Error ? error.message : String(error)),
        });
      }
    });
    return {
      prompt: (goal) => running = session.prompt(definition.formatPrompt?.(goal) ?? goal),
      abort,
      close, dispose,
      getActiveToolNames: () => session.getActiveToolNames(),
      getVerdict: () => verdict,
    };
  } catch (error) {
    try {
      await close();
    } catch (cleanupError) {
      throw new SessionCleanupError(
        cleanupError instanceof Error ? cleanupError.message : String(cleanupError),
        { cause: error },
      );
    }
    throw error;
  }
}
