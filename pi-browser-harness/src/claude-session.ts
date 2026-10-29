import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { readFile, readdir, stat } from "node:fs/promises";
import { basename, join, relative } from "node:path";
import type { Options, Query, SDKMessage, SDKResultMessage, SpawnOptions, query as officialQuery } from "@anthropic-ai/claude-agent-sdk";
import { prepareHarnessResources, type HarnessResources } from "./harness-resources.js";
import type { ObservedEvent } from "./harness-definition.js";
import type { HarnessEventWriter, RunRequest } from "./run-protocol.js";
import { SessionCleanupError, type ExecutionSession } from "./server.js";
import { BrowserEvidenceGuard } from "./browser/evidence-guard.js";

type ClaudeQuery = Pick<Query, "next" | "return" | "throw" | typeof Symbol.asyncIterator | "close">;
type RuntimeMcp = {
  mcpServers: Record<string, { type: "http"; url: string }>;
  events: ObservedEvent[];
  close(): Promise<void>;
};

export interface ClaudeSessionDeps {
  eventWriter: HarnessEventWriter;
  signal?: AbortSignal;
  query?: (parameters: Parameters<typeof officialQuery>[0]) => ClaudeQuery;
  spawnClaudeCodeProcess?: (options: SpawnOptions) => ChildProcessWithoutNullStreams;
  prepareHarnessResources?: (request: RunRequest) => Promise<HarnessResources>;
  openRuntimeMcp?: (resources: HarnessResources, writer: Pick<HarnessEventWriter, "write">,
    evidenceGuard?: BrowserEvidenceGuard) => Promise<RuntimeMcp>;
}

const errorText = (error: unknown): string => error instanceof Error ? error.message : String(error);

/** Official SDK execution; the native MCP helper is the only tool-event authority. */
export async function createClaudeSession(
  request: RunRequest, deps: ClaudeSessionDeps,
): Promise<ExecutionSession> {
  deps.signal?.throwIfAborted();
  const resources = await (deps.prepareHarnessResources ?? prepareHarnessResources)(request);
  const evidenceGuard = resources.servers.some((server) => server.name === "browser-harness")
    ? new BrowserEvidenceGuard() : undefined;
  const eventWriter = evidenceGuard?.publicWriter(deps.eventWriter) ?? deps.eventWriter;
  let mcp: RuntimeMcp;
  let systemPrompt: string;
  try {
    deps.signal?.throwIfAborted();
    systemPrompt = await loadSystemPrompt(resources);
    deps.signal?.throwIfAborted();
    const openMcp = deps.openRuntimeMcp ?? (await import("./runtime-mcp.js")).openRuntimeMcp;
    mcp = await openMcp(resources, eventWriter, evidenceGuard);
  } catch (error) {
    if (error instanceof SessionCleanupError) throw error;
    try { await resources.dispose(); } catch (cleanupError) {
      throw new SessionCleanupError(errorText(cleanupError), { cause: error });
    }
    throw error;
  }

  const controller = new AbortController();
  let sdk: ClaudeQuery | undefined;
  let sdkClosed = false;
  let sdkCloseError: unknown;
  let running: Promise<void> | undefined;
  let closing: Promise<void> | undefined;
  let aborting: Promise<void> | undefined;
  let prompted = false;
  const children: ReturnType<typeof superviseChild>[] = [];
  const spawnClaudeCodeProcess = (options: SpawnOptions): ChildProcessWithoutNullStreams => {
    controller.signal.throwIfAborted();
    const child = deps.spawnClaudeCodeProcess
      ? deps.spawnClaudeCodeProcess(options)
      : spawn(options.command, options.args, {
        cwd: options.cwd, env: options.env, signal: options.signal, stdio: "pipe", windowsHide: true,
      });
    const supervised = superviseChild(child);
    children.push(supervised);
    // Custom SDK spawners own stderr; drain it without exposing private diagnostics.
    child.stderr.resume();
    if (controller.signal.aborted || sdkClosed) supervised.stop();
    return child;
  };

  const closeSdk = (): void => {
    if (sdk && !sdkClosed) {
      sdkClosed = true;
      try { sdk.close(); } catch (error) { sdkCloseError = error; }
    }
    // Also owns a child spawned before query construction throws or close fails.
    for (const child of children) child.stop();
  };
  const abort = (): Promise<void> => {
    controller.abort();
    closeSdk();
    aborting ??= (async () => {
      await joinShutdown((async () => {
        // A rejected model turn is not a cleanup failure, but must still be joined.
        await running?.catch(() => {});
        await Promise.all(children.map((child) => child.closed));
      })());
      if (sdkCloseError !== undefined) {
        throw new SessionCleanupError(errorText(sdkCloseError), { cause: sdkCloseError });
      }
    })();
    return aborting;
  };
  const onAbort = (): void => { void abort().catch(() => {}); };
  const close = (): Promise<void> => {
    closing ??= (async () => {
      deps.signal?.removeEventListener("abort", onAbort);
      // Never relinquish MCP or remove its directory without actual child close.
      await abort();
      evidenceGuard?.discardPending();
      try {
        await mcp.close();
      } catch (error) {
        // A failed helper join retains the directory used by its still-owned child.
        throw error instanceof SessionCleanupError ? error :
          new SessionCleanupError(errorText(error), { cause: error });
      }
      try { await resources.dispose(); } catch (error) {
        throw new SessionCleanupError(errorText(error), { cause: error });
      }
    })();
    return closing;
  };

  const execute = async (goal: string): Promise<void> => {
    let result: SDKResultMessage | undefined;
    try {
      controller.signal.throwIfAborted();
      const query = deps.query ?? (await import("@anthropic-ai/claude-agent-sdk")).query;
      controller.signal.throwIfAborted();
      sdk = query({
        prompt: goal,
        options: {
          cwd: resources.cwd, model: request.model.name, systemPrompt,
          abortController: controller, env: sdkEnvironment(resources.cwd),
          spawnClaudeCodeProcess,
          tools: [], skills: [], plugins: [], settingSources: [],
          settings: { disableAllHooks: true },
          strictMcpConfig: true, mcpServers: mcp.mcpServers,
          allowedTools: Object.keys(mcp.mcpServers).map((name) => `mcp__${name}__*`),
          permissionMode: "dontAsk", persistSession: false, includePartialMessages: false,
          ...effortOption(request.model.reasoning_effort),
        },
      });
      controller.signal.throwIfAborted();
      for await (const message of sdk) {
        controller.signal.throwIfAborted();
        if (message.type === "assistant") {
          if (message.error) throw new Error(`Claude SDK assistant error: ${message.error}`);
          if (message.aborted) throw new Error("Claude SDK assistant was aborted");
          writeAssistant(message, request, eventWriter);
        } else if (message.type === "result") {
          result = message;
          if (message.subtype !== "success" || message.is_error) {
            writeUsage(message, request, eventWriter);
            throw new Error(message.subtype === "success"
              ? `Claude SDK error: ${message.result}`
              : `Claude SDK ${message.subtype}: ${message.errors.join("; ")}`);
          }
        }
      }
      controller.signal.throwIfAborted();
      if (!result || result.subtype !== "success" || result.is_error) {
        throw new Error("Claude SDK stream ended without a successful result");
      }
      writeUsage(result, request, eventWriter);
      const completion = resources.definition.completionPolicy({
        assistant_settled: true, aborted: false, assistant_text: result.result, events: mcp.events,
      });
      eventWriter.write("harness.settled", { assistant_text: result.result, ...completion });
    } catch (error) {
      eventWriter.write("harness.failed", { error: errorText(error) });
      throw error;
    } finally {
      closeSdk();
    }
  };

  deps.signal?.addEventListener("abort", onAbort, { once: true });
  if (deps.signal?.aborted) {
    try { await close(); } catch (cleanupError) {
      throw new SessionCleanupError(errorText(cleanupError), { cause: deps.signal.reason });
    }
    deps.signal.throwIfAborted();
  }
  return {
    prompt(goal) {
      if (prompted || closing || controller.signal.aborted) {
        return Promise.reject(new Error("Claude session is closed or already prompted"));
      }
      prompted = true;
      // Defer execution so synchronous SDK dependencies cannot race the join handle.
      running = Promise.resolve().then(() => execute(goal));
      return running;
    },
    abort, close,
  };
}

function superviseChild(child: ChildProcessWithoutNullStreams): { closed: Promise<void>; stop(): void } {
  let didClose = false;
  let stopping = false;
  let term: ReturnType<typeof setTimeout> | undefined;
  let kill: ReturnType<typeof setTimeout> | undefined;
  const closed = new Promise<void>((resolve) => {
    // 'error' alone is not exit acknowledgement, including asynchronous spawn failures.
    child.on("error", () => {});
    child.stdin.on("error", () => {});
    child.stderr.on("error", () => {});
    child.once("close", () => {
      didClose = true;
      clearTimeout(term);
      clearTimeout(kill);
      resolve();
    });
  });
  const signal = (value: NodeJS.Signals): void => {
    if (didClose) return;
    try { child.kill(value); } catch { /* Only close can establish shutdown. */ }
  };
  return {
    closed,
    stop() {
      if (stopping || didClose) return;
      stopping = true;
      try { child.stdin.end(); } catch { /* Escalate if graceful EOF fails. */ }
      term = setTimeout(() => {
        signal("SIGTERM");
        if (!didClose) kill = setTimeout(() => signal("SIGKILL"), 5000);
        kill?.unref();
      }, 2000);
      term.unref();
    },
  };
}

async function joinShutdown(joined: Promise<void>): Promise<void> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      joined,
      new Promise<never>((_resolve, reject) => {
        timer = setTimeout(() => reject(new SessionCleanupError(
          "Claude SDK work or child shutdown was not acknowledged; MCP and resources retained",
        )), 15_000);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

function writeAssistant(
  message: Extract<SDKMessage, { type: "assistant" }>, request: RunRequest, writer: Pick<HarnessEventWriter, "write">,
): void {
  writer.write("pi.message.completed", {
    text: message.message.content.filter((block) => block.type === "text").map((block) => block.text).join(""),
    provider: request.model.provider, provider_source: "runtime_configuration",
    model: message.message.model, granularity: "sdk_assistant_message",
    sdk_message_id: message.message.id, sdk_message_uuid: message.uuid,
    ...(message.request_id ? { sdk_request_id: message.request_id } : {}),
    ...(message.timestamp ? { sdk_timestamp: message.timestamp } : {}),
    ...(message.parent_tool_use_id ? { sdk_parent_tool_use_id: message.parent_tool_use_id } : {}),
  });
}

function writeUsage(result: SDKResultMessage, request: RunRequest, writer: Pick<HarnessEventWriter, "write">): void {
  // modelUsage includes the query pipeline's sidechains; result.usage is main-loop only.
  for (const [model, usage] of Object.entries(result.modelUsage)) {
    writer.write("pi.usage", {
      provider: usage.provider ?? request.model.provider,
      provider_source: usage.provider ? "sdk_model_usage" : "runtime_configuration",
      model, input_tokens: usage.inputTokens, output_tokens: usage.outputTokens,
      cache_read_tokens: usage.cacheReadInputTokens, cache_write_tokens: usage.cacheCreationInputTokens,
      granularity: "sdk_query_model_cumulative", usage_scope: "sdk_query_pipeline",
      sdk_result_uuid: result.uuid, sdk_num_turns: result.num_turns,
      sdk_duration_ms: result.duration_ms, sdk_duration_api_ms: result.duration_api_ms,
      timing_scope: "sdk_turn", sdk_cost_usd: usage.costUSD,
      ...(usage.canonicalModel ? { sdk_canonical_model: usage.canonicalModel } : {}),
    });
  }
}

async function loadSystemPrompt(resources: HarnessResources): Promise<string> {
  const parts = [resources.systemPrompt];
  const visit = async (root: string, path: string): Promise<void> => {
    const info = await stat(path);
    if (info.isDirectory()) {
      for (const entry of (await readdir(path, { withFileTypes: true })).sort((a, b) => a.name.localeCompare(b.name))) {
        if (!entry.isDirectory() && !entry.isFile()) throw new Error("Unsupported Claude skill resource");
        await visit(root, join(path, entry.name));
      }
    } else if (info.isFile()) {
      parts.push(`\nSelected Harness skill file: ${relative(root, path) || basename(path)}\n${await readFile(path, "utf8")}`);
    } else {
      throw new Error("Unsupported Claude skill resource");
    }
  };
  for (const path of resources.skillPaths) await visit(path, path);
  return parts.join("\n");
}

function sdkEnvironment(cwd: string): NonNullable<Options["env"]> {
  const env: NonNullable<Options["env"]> = {};
  for (const key of [
    "PATH", "HOME", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL",
    "NODE_EXTRA_CA_CERTS", "SSL_CERT_FILE", "SSL_CERT_DIR",
    "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
  ]) {
    if (process.env[key] !== undefined) env[key] = process.env[key];
  }
  env["CLAUDE_CONFIG_DIR"] = join(cwd, ".claude-config");
  env["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1";
  env["ANTHROPIC_API_KEY"] = process.env["AGENTTRACE_CLAUDE_API_KEY"];
  const base = process.env["AGENTTRACE_CLAUDE_BASE_URL"];
  if (base) {
    const url = new URL(base);
    // Anthropic appends /v1/messages, unlike OpenAI-compatible clients' base URLs.
    url.pathname = url.pathname.replace(/\/v1\/?$/, "").replace(/\/$/, "");
    env["ANTHROPIC_BASE_URL"] = url.href.replace(/\/$/, "");
  }
  return env;
}

function effortOption(effort: string): Pick<Options, "effort"> {
  return effort === "low" || effort === "medium" || effort === "high" ||
    effort === "xhigh" || effort === "max" ? { effort } : {};
}
