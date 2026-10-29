import { mkdir, readFile, readdir, stat, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { delimiter, dirname, join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import type { CodexOptions, Input, ModelReasoningEffort, RunStreamedResult, ThreadOptions, TurnOptions } from "@openai/codex-sdk";
import { normalizeName, prepareHarnessResources, type HarnessResources } from "./harness-resources.js";
import type { ObservedEvent } from "./harness-definition.js";
import type { HarnessEventPayloads, HarnessEventWriter, RunRequest } from "./run-protocol.js";
import { SessionCleanupError, type ExecutionSession } from "./server.js";
import { openCodexProvider, type CodexProvider } from "./codex-provider.js";
import { BrowserEvidenceGuard } from "./browser/evidence-guard.js";

export interface CodexSessionDeps {
  eventWriter: HarnessEventWriter;
  signal?: AbortSignal;
  env?: Record<string, string | undefined>;
  prepareHarnessResources?(request: RunRequest): Promise<HarnessResources>;
  openRuntimeMcp?(resources: HarnessResources, writer: Pick<HarnessEventWriter, "write">,
    evidenceGuard?: BrowserEvidenceGuard): Promise<{
    mcpServers: Record<string, { type: "http"; url: string }>;
    events: ObservedEvent[];
    close(): Promise<void>;
  }>;
  createClient?(options: CodexOptions): {
    startThread(options?: ThreadOptions): {
      runStreamed(input: Input, options?: TurnOptions): Promise<RunStreamedResult>;
    };
  };
}

export async function createCodexSession(
  request: RunRequest, deps: CodexSessionDeps,
): Promise<ExecutionSession> {
  deps.signal?.throwIfAborted();
  const source = deps.env ?? process.env;
  const baseUrl = required(source, "AGENTTRACE_CODEX_BASE_URL");
  const apiKey = required(source, "AGENTTRACE_CODEX_API_KEY");
  const model = request.model.name;
  try {
    const url = new URL(baseUrl);
    if (!["http:", "https:"].includes(url.protocol)) throw new Error();
  } catch {
    throw new Error("AGENTTRACE_CODEX_BASE_URL must be an absolute HTTP URL");
  }
  const headers = privateHeaders(source.AGENTTRACE_CODEX_HTTP_HEADERS);
  const toolFormat = source.AGENTTRACE_CODEX_TOOL_FORMAT?.trim() || "native";
  if (!["native", "flat-functions"].includes(toolFormat)) {
    throw new Error('AGENTTRACE_CODEX_TOOL_FORMAT must be "native" or "flat-functions"');
  }
  const headerSecrets = Object.values(headers).flatMap((value) => {
    const bearer = /^Bearer\s+(.+)$/i.exec(value);
    return bearer ? [value, bearer[1]!] : [value];
  });
  const secrets = [apiKey, baseUrl, ...headerSecrets].filter(Boolean);
  const errorText = (error: unknown): string => {
    let text = error instanceof Error ? error.message : String(error);
    for (const secret of [...secrets].sort((a, b) => b.length - a.length)) {
      text = text.replaceAll(secret, "[redacted]");
    }
    return text;
  };
  const resources = await (deps.prepareHarnessResources ?? prepareHarnessResources)(request);
  const evidenceGuard = resources.servers.some((server) => server.name === "browser-harness")
    ? new BrowserEvidenceGuard() : undefined;
  const eventWriter = evidenceGuard?.publicWriter(deps.eventWriter) ?? deps.eventWriter;
  let mcp: Awaited<ReturnType<NonNullable<CodexSessionDeps["openRuntimeMcp"]>>> | undefined;
  let provider: CodexProvider | undefined;
  const controller = new AbortController();
  const onAbort = () => controller.abort(deps.signal?.reason);
  deps.signal?.addEventListener("abort", onAbort, { once: true });
  if (deps.signal?.aborted) onAbort();
  try {
    const home = join(resources.cwd, ".codex-home");
    const userHome = join(resources.cwd, ".home");
    await mkdir(home, { mode: 0o700 });
    await mkdir(userHome, { mode: 0o700 });
    await writeFile(join(resources.cwd, ".agenttrace-root"), "", { mode: 0o600, flag: "wx" });
    controller.signal.throwIfAborted();
    const openMcp = deps.openRuntimeMcp ?? (await import("./runtime-mcp.js")).openRuntimeMcp;
    mcp = await openMcp(resources, eventWriter, evidenceGuard);
    controller.signal.throwIfAborted();
    if (toolFormat === "flat-functions") {
      provider = await openCodexProvider({
        baseUrl, apiKey, headers, servers: Object.keys(mcp.mcpServers), signal: controller.signal,
      });
      secrets.push(provider.apiKey, provider.baseUrl);
    }
    const instructions = [
      resources.systemPrompt,
      "Use only the selected harness MCP tools. Do not use shell commands, apply_patch, built-in browser tools, apps, or external skills.",
      ...(await skillInstructions(resources.skillPaths)),
    ].join("\n\n");
    const instructionsFile = join(home, "instructions.md");
    await writeFile(instructionsFile, resources.systemPrompt, { mode: 0o600, flag: "wx" });
    await writeFile(join(home, "config.toml"), scopedConfig({
      instructions, instructionsFile, baseUrl: provider?.baseUrl ?? baseUrl,
      headers: provider ? {} : headers, model, servers: mcp.mcpServers,
    }), { mode: 0o600, flag: "wx" });
    const binary = nativeBinary(source.AGENTTRACE_CODEX_PATH);
    const supervisorPath = join(home, "exec-supervisor.cjs");
    const statePath = join(home, "exec-state.json");
    await writeFile(supervisorPath, supervisorSource(binary.path, statePath), { mode: 0o700, flag: "wx" });
    const env: Record<string, string> = {
      PATH: [dirname(process.execPath), ...binary.pathDirs, source.PATH ?? ""].join(delimiter),
      HOME: userHome, CODEX_HOME: home, XDG_CONFIG_HOME: join(userHome, ".config"),
      XDG_DATA_HOME: join(userHome, ".local", "share"), XDG_CACHE_HOME: join(userHome, ".cache"),
      AGENTTRACE_CODEX_API_KEY: provider?.apiKey ?? apiKey,
    };
    for (const key of ["TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "SYSTEMROOT", "WINDIR"]) {
      if (source[key]) env[key] = source[key]!;
    }
    const clientOptions: CodexOptions = { env, codexPathOverride: supervisorPath };
    const client = deps.createClient
      ? deps.createClient(clientOptions)
      : new (await import("@openai/codex-sdk")).Codex(clientOptions);
    const threadOptions: ThreadOptions = {
      model, workingDirectory: resources.cwd, sandboxMode: "read-only",
      approvalPolicy: "never", skipGitRepoCheck: true, webSearchMode: "disabled",
      // MCP HTTP is performed by Codex's host, not by sandboxed shell commands.
      networkAccessEnabled: true,
      ...(reasoningEffort(request.model.reasoning_effort) ? {
        modelReasoningEffort: reasoningEffort(request.model.reasoning_effort)!,
      } : {}),
    };
    const thread = client.startThread(threadOptions);
    controller.signal.throwIfAborted();
    let active: Promise<void> | undefined;
    let closing: Promise<void> | undefined;
    let aborting: Promise<void> | undefined;
    let prompted = false;
    const joinProcess = () => awaitProcessClose(statePath);
    const abort = (): Promise<void> => {
      controller.abort();
      aborting ??= (async () => {
        try { await active; } catch (error) {
          if (error instanceof SessionCleanupError) throw error;
        }
        await joinProcess();
      })();
      return aborting;
    };
    const helper = mcp;
    const close = (): Promise<void> => {
      closing ??= (async () => {
        deps.signal?.removeEventListener("abort", onAbort);
        // A join failure must not falsely release MCP or delete its working resources.
        await abort();
        evidenceGuard?.discardPending();
        await provider?.close();
        const errors: string[] = [];
        try { await helper.close(); } catch (error) {
          throw new SessionCleanupError(errorText(error));
        }
        try { await resources.dispose(); } catch (error) { errors.push(errorText(error)); }
        if (errors.length) throw new SessionCleanupError(errors.join("; "));
      })();
      return closing;
    };
    const run = async (goal: string): Promise<void> => {
      const startedAt = new Date().toISOString();
      let assistantText = "";
      let hasMessage = false;
      let modelEmitted = false;
      let turnUsage: HarnessEventPayloads["pi.usage"] | undefined;
      let completed = false;
      let failure: Error | undefined;
      const emitModelTurn = () => {
        if (modelEmitted) return;
        modelEmitted = true;
        eventWriter.write("pi.message.completed", {
          text: assistantText, provider: "openai-responses", model,
          model_source: "request", granularity: "turn", timing_source: "host",
          started_at: startedAt, ended_at: new Date().toISOString(),
        });
        // Host projection attaches usage to the preceding model span.
        if (turnUsage) eventWriter.write("pi.usage", turnUsage);
      };
      try {
        controller.signal.throwIfAborted();
        const turn = await thread.runStreamed(goal, { signal: controller.signal });
        // Drain even turn.failed events; returning early lets SDK finally kill without joining.
        for await (const event of turn.events) {
          if (event.type === "error" || event.type === "turn.failed") {
            failure ??= new Error(event.type === "error" ? event.message : event.error.message);
          } else if (event.type === "item.completed" && event.item.type === "agent_message") {
            assistantText = event.item.text;
            hasMessage = true;
          } else if ((event.type === "item.started" || event.type === "item.updated" || event.type === "item.completed") &&
            !["agent_message", "reasoning", "error"].includes(event.item.type)) {
            const item = event.item;
            const descriptor = item.type === "mcp_tool_call"
              ? resources.servers.find((server) => normalizeName(server.name) === item.server) : undefined;
            const selected = item.type === "mcp_tool_call" &&
              Object.hasOwn(helper.mcpServers, item.server) && item.tool !== "close_session" &&
              descriptor?.exposure !== "hidden" &&
              !descriptor?.hostOnlyTools?.some((tool) => normalizeName(tool) === item.tool);
            if (!selected) {
              failure ??= new Error(`Codex attempted a tool outside the selected harness: ${item.type}`);
              controller.abort();
            }
          } else if (event.type === "turn.completed") {
            if (completed) {
              failure ??= new Error("Codex emitted multiple completions for one turn");
              continue;
            }
            completed = true;
            const usage = event.usage;
            turnUsage = {
              provider: "openai-responses", model, model_source: "request", granularity: "turn",
              input_tokens: usage.input_tokens, output_tokens: usage.output_tokens,
              cache_read_tokens: usage.cached_input_tokens,
              ...(usage.cache_write_input_tokens !== undefined ? { cache_write_tokens: usage.cache_write_input_tokens } : {}),
              ...(usage.reasoning_output_tokens !== undefined ? { reasoning_output_tokens: usage.reasoning_output_tokens } : {}),
              total_tokens: usage.input_tokens + usage.output_tokens,
            };
          }
          // MCP tool/browser evidence belongs exclusively to the attempt-local helper.
        }
        await joinProcess();
        if (failure) throw failure;
        controller.signal.throwIfAborted();
        if (!completed) throw new Error("Codex stream ended without a completed turn");
        emitModelTurn();
        const completion = resources.definition.completionPolicy({
          assistant_settled: true, aborted: false, assistant_text: assistantText, events: helper.events,
        });
        eventWriter.write("harness.settled", { assistant_text: assistantText, ...completion });
      } catch (error) {
        // In particular, the SDK can throw AbortError after stdout EOF but before child exit.
        await joinProcess();
        if (hasMessage) emitModelTurn();
        const message = errorText(error);
        eventWriter.write("harness.failed", { error: message });
        if (error instanceof SessionCleanupError) throw error;
        throw new Error(message);
      }
    };
    return {
      prompt(goal) {
        if (prompted || closing) return Promise.reject(new Error("Codex session is already used or closed"));
        prompted = true;
        active = Promise.resolve().then(() => run(goal));
        void active.catch(() => {});
        return active;
      },
      abort, close,
    };
  } catch (error) {
    deps.signal?.removeEventListener("abort", onAbort);
    controller.abort();
    const errors: string[] = [];
    let uncertain = error instanceof SessionCleanupError;
    try { await provider?.close(); } catch (cleanupError) {
      uncertain = true;
      errors.push(errorText(cleanupError));
    }
    try { await mcp?.close(); } catch (cleanupError) {
      uncertain = true;
      errors.push(errorText(cleanupError));
    }
    if (uncertain) throw new SessionCleanupError([errorText(error), ...errors].join("; "));
    try { await resources.dispose(); } catch (cleanupError) { errors.push(errorText(cleanupError)); }
    if (errors.length) throw new SessionCleanupError(errors.join("; "), { cause: new Error(errorText(error)) });
    throw new Error(errorText(error));
  }
}

function required(env: Record<string, string | undefined>, name: string): string {
  const value = env[name]?.trim();
  if (!value) throw new Error(`${name} is required for Codex SDK execution`);
  return value;
}

function privateHeaders(raw?: string): Record<string, string> {
  if (!raw?.trim()) return {};
  let value: unknown;
  try { value = JSON.parse(raw); } catch { throw new Error("AGENTTRACE_CODEX_HTTP_HEADERS must be a JSON object of headers"); }
  if (!value || typeof value !== "object" || Array.isArray(value) ||
      Object.entries(value).some(([key, val]) => !/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(key) ||
        typeof val !== "string" || /[\r\n\0]/.test(val))) {
    throw new Error("AGENTTRACE_CODEX_HTTP_HEADERS must be a JSON object of string headers");
  }
  return value as Record<string, string>;
}

function reasoningEffort(value: string): ModelReasoningEffort | undefined {
  if (["off", "none"].includes(value)) return undefined;
  if (!["minimal", "low", "medium", "high", "xhigh", "max", "ultra", "persistent"].includes(value)) {
    throw new Error("unsupported Codex reasoning effort");
  }
  return value as ModelReasoningEffort;
}

async function skillInstructions(paths: readonly string[]): Promise<string[]> {
  const result: string[] = [];
  const visit = async (path: string): Promise<void> => {
    const info = await stat(path);
    if (info.isFile()) {
      result.push(`Selected skill file (${path}):\n${await readFile(path, "utf8")}`);
      return;
    }
    for (const entry of (await readdir(path, { withFileTypes: true })).sort((a, b) => a.name.localeCompare(b.name))) {
      if (entry.isDirectory()) await visit(join(path, entry.name));
      else if (entry.isFile()) await visit(join(path, entry.name));
    }
  };
  for (const path of paths) await visit(path);
  return result;
}

function scopedConfig(options: {
  instructions: string; instructionsFile: string; baseUrl: string; model: string;
  headers: Record<string, string>;
  servers: Record<string, { type: "http"; url: string }>;
}): string {
  // Only JSON-escaped basic strings and fixed TOML scalars are serialized here.
  const quote = JSON.stringify;
  const lines = [
    `model = ${quote(options.model)}`, 'model_provider = "agenttrace"',
    `developer_instructions = ${quote(options.instructions)}`,
    `model_instructions_file = ${quote(options.instructionsFile)}`,
    'sandbox_mode = "read-only"', 'approval_policy = "never"', 'web_search = "disabled"',
    "project_doc_max_bytes = 0", 'project_root_markers = [".agenttrace-root"]',
    "allow_login_shell = false", "include_apps_instructions = false",
    'cli_auth_credentials_store = "file"', 'mcp_oauth_credentials_store = "file"',
    "[features]",
    ...["shell_tool", "unified_exec", "shell_snapshot", "hooks", "plugins", "apps", "multi_agent",
      "memories", "skill_mcp_dependency_install", "skill_search", "browser_use", "browser_use_external",
      "computer_use", "image_generation", "view_image", "code_mode", "sleep_tool", "daemon_auto_start", "goals"]
      .map((key) => `${key} = false`),
    "skip_host_skill_discovery = true",
    "[skills]", "include_instructions = false", "[skills.bundled]", "enabled = false",
    "[tools.experimental_request_user_input]", "enabled = false",
    "[tools.update_plan]", "enabled = false",
    "[model_providers.agenttrace]", 'name = "agenttrace"', `base_url = ${quote(options.baseUrl)}`,
    'env_key = "AGENTTRACE_CODEX_API_KEY"', 'wire_api = "responses"', "requires_openai_auth = false",
    "supports_websockets = false",
    "[model_providers.agenttrace.http_headers]",
    ...Object.entries(options.headers).map(([key, value]) => `${quote(key)} = ${quote(value)}`),
  ];
  for (const [name, server] of Object.entries(options.servers)) {
    // Keep selected tools in the initial request even for search-capable models.
    // They are host-vetted; "never" otherwise denies unannotated MCP calls.
    lines.push(`[mcp_servers.${quote(name)}]`, `url = ${quote(server.url)}`,
      'omit_tools_from = ["deferred", "code_mode"]',
      'default_tools_approval_mode = "approve"',
      "enabled = true", "required = true", "startup_timeout_sec = 30", "tool_timeout_sec = 120");
  }
  return `${lines.join("\n")}\n`;
}

function nativeBinary(override?: string): { path: string; pathDirs: string[] } {
  if (override?.trim()) return { path: resolve(override), pathDirs: [] };
  const triples: Record<string, string> = {
    "darwin-arm64": "aarch64-apple-darwin", "darwin-x64": "x86_64-apple-darwin",
    "linux-arm64": "aarch64-unknown-linux-musl", "linux-x64": "x86_64-unknown-linux-musl",
  };
  const platform = `${process.platform}-${process.arch}`;
  const triple = triples[platform];
  if (!triple) throw new Error(`Codex process supervision is unsupported on ${platform}`);
  const sdkRequire = createRequire(fileURLToPath(import.meta.resolve("@openai/codex-sdk")));
  const codexRequire = createRequire(sdkRequire.resolve("@openai/codex/package.json"));
  const packageRoot = dirname(codexRequire.resolve(`@openai/codex-${platform}/package.json`));
  const vendor = join(packageRoot, "vendor", triple);
  return { path: join(vendor, "bin", "codex"), pathDirs: [join(vendor, "codex-path")] };
}

async function awaitProcessClose(path: string): Promise<void> {
  const deadline = Date.now() + 15000;
  while (true) {
    let state: { supervisor: number; closed: boolean };
    try {
      state = JSON.parse(await readFile(path, "utf8")) as typeof state;
    } catch (error) {
      // No supervisor was started (injected client, pre-spawn abort, or spawn failure).
      if ((error as NodeJS.ErrnoException).code === "ENOENT") return;
      if (Date.now() >= deadline) throw new SessionCleanupError("Cannot read Codex process settlement");
      await delay(10); continue;
    }
    let running = true;
    try { process.kill(state.supervisor, 0); } catch (error) {
      if ((error as NodeJS.ErrnoException).code === "ESRCH") running = false;
      else throw new SessionCleanupError("Cannot verify Codex supervisor exit");
    }
    if (state.closed && !running) return;
    if (Date.now() >= deadline) throw new SessionCleanupError("Codex child shutdown was not acknowledged; resources retained");
    await delay(10);
  }
}

function supervisorSource(binary: string, statePath: string): string {
  // The official SDK's exec finally does not join child.close. This supported
  // executable override forwards CLI I/O unchanged and acknowledges real shutdown.
  return `#!${process.execPath}
const { spawn } = require("node:child_process");
const { writeFileSync, renameSync } = require("node:fs");
const statePath = ${JSON.stringify(statePath)};
const save = (closed) => {
  writeFileSync(statePath + ".tmp", JSON.stringify({ supervisor: process.pid, closed }), { mode: 0o600 });
  renameSync(statePath + ".tmp", statePath);
};
save(false);
const child = spawn(${JSON.stringify(binary)}, process.argv.slice(2), {
  env: process.env, detached: true, stdio: ["pipe", "pipe", "pipe"]
});
let timer;
let stopped = false;
const kill = (signal) => {
  try { if (child.pid) process.kill(-child.pid, signal); } catch (error) {
    if (error.code !== "ESRCH") process.stderr.write("Codex supervisor could not signal child\\n");
  }
};
const stop = () => {
  if (stopped) return;
  stopped = true;
  kill("SIGTERM");
  timer = setTimeout(() => kill("SIGKILL"), 2000);
};
process.on("SIGTERM", stop);
process.on("SIGINT", stop);
process.stdin.pipe(child.stdin);
child.stdin.on("error", (error) => { if (error.code !== "EPIPE") stop(); });
process.stdout.on("error", stop);
child.stdout.pipe(process.stdout, { end: false });
child.stderr.pipe(process.stderr, { end: false });
child.once("error", () => { process.stderr.write("Unable to spawn Codex native CLI\\n"); });
child.once("close", (code, signal) => {
  clearTimeout(timer);
  save(true);
  process.stdin.destroy();
  process.exitCode = stopped || signal ? 1 : (code ?? 1);
});
`;
}
