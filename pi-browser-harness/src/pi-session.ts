import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import type { Writable } from "node:stream";

import {
  DefaultResourceLoader,
  SessionManager,
  SettingsManager,
  loadSkillsFromDir,
  type CreateAgentSessionOptions,
  type CreateAgentSessionResult,
  type InlineExtension,
  type ModelRuntime,
  type ResourceLoader,
} from "@earendil-works/pi-coding-agent";
import {
  createAssistantMessageEventStream,
  type AssistantMessage,
  type Message,
  type ToolCall,
} from "@earendil-works/pi-ai";
import type { ThinkingLevel } from "@earendil-works/pi-agent-core";

import {
  createBrowserHarnessExtension,
  createSequentialBrowserMcpToolsExtension,
} from "./extension.js";
import {
  createTraceMapperState,
  mapPiEvent,
  type TraceMapperState,
} from "./trace-events.js";
import {
  HarnessEventWriter,
  deriveTerminalVerdict,
  type RunRequest,
} from "./run-protocol.js";

const SYSTEM_PROMPT_PATH = fileURLToPath(
  new URL("../resources/system-prompt.md", import.meta.url),
);
const SKILLS_DIR = fileURLToPath(
  new URL("../resources/skills", import.meta.url),
);

/** Dependencies injected into {@link createBrowserSession}. */
export interface BrowserSessionDeps {
  /** Pi session factory. Injected so tests can substitute a fake session. */
  createAgentSession: (
    options: CreateAgentSessionOptions,
  ) => Promise<CreateAgentSessionResult>;
  /** Model runtime with providers registered by the harness. */
  modelRuntime: ModelRuntime;
  /** Writes harness JSONL events to stdout. */
  eventWriter: HarnessEventWriter;
  /** Working directory for the Pi session. Defaults to process.cwd(). */
  cwd?: string;
  /** Sink for diagnostic logs. Defaults to process.stderr. */
  stderr?: Pick<Writable, "write">;
  /** Path to the compiled MCP child. Defaults to dist/mcp/server.js. */
  mcpChildPath?: string;
  /** Override the resource loader (tests). */
  resourceLoader?: ResourceLoader;
  /** Override extension factories (tests). */
  extensionFactories?: InlineExtension[];
}

/** A running Pi browser authoring session. */
export interface BrowserSession {
  /** Sends the forced browser-case-authoring skill prompt with the run goal. */
  prompt(goal: string): Promise<void>;
  /** Aborts the current agent run and waits for idle. */
  abort(): Promise<void>;
  /** Disconnects listeners and releases the session. */
  dispose(): void;
  /** Names of the tools currently declared to the model. */
  getActiveToolNames(): string[];
  /** Terminal verdict derived from settlement and browser case evidence. */
  getVerdict(): "succeeded" | "failed";
}

/**
 * Creates a Pi agent session configured for browser case authoring: forced
 * system prompt, forced browser skill, in-memory session/settings, only the
 * browser MCP tool namespace active, and the browser-harness extension that
 * owns the MCP child.
 */
export async function createBrowserSession(
  request: RunRequest,
  deps: BrowserSessionDeps,
): Promise<BrowserSession> {
  const cwd = deps.cwd ?? process.cwd();
  const stderr = deps.stderr ?? process.stderr;

  const model = resolveModel(deps.modelRuntime, request);

  const resourceLoader =
    deps.resourceLoader ??
    buildResourceLoader(
      cwd,
      deps.extensionFactories,
      request,
      deps.mcpChildPath,
    );
  await resourceLoader.reload();

  const settingsManager = SettingsManager.inMemory({
    retry: {
      enabled: true,
      maxRetries: 2,
      baseDelayMs: 100,
      maxAgentDelayMs: 1000,
    },
  });

  const { session } = await deps.createAgentSession({
    cwd,
    model,
    thinkingLevel: request.model.reasoning_effort as ThinkingLevel,
    modelRuntime: deps.modelRuntime,
    resourceLoader,
    tools: ["mcp__browser_harness__*"],
    sessionManager: SessionManager.inMemory(cwd),
    settingsManager,
  });

  await session.bindExtensions({});

  const state: TraceMapperState = createTraceMapperState();
  let assistantSettled = false;

  const unsubscribe = session.subscribe((event) => {
    if (event.type === "agent_settled") {
      assistantSettled = true;
    }
    const mapped = mapPiEvent(event, state);
    for (const item of mapped) {
      deps.eventWriter.write(item.type, item.payload as never);
    }
  });

  const self: BrowserSession = {
    async prompt(goal: string): Promise<void> {
      await session.prompt(`/skill:browser-case-authoring ${goal}`);
    },
    async abort(): Promise<void> {
      await session.abort();
    },
    dispose(): void {
      unsubscribe();
      session.dispose();
    },
    getActiveToolNames(): string[] {
      return session.getActiveToolNames();
    },
    getVerdict(): "succeeded" | "failed" {
      return deriveTerminalVerdict({
        assistant_settled: assistantSettled,
        browser_case_verdicts: state.browser_case_verdicts,
      });
    },
  };

  return self;
}

/**
 * Resolves the model for the run. For the `scripted` provider a deterministic
 * provider is registered so tests never hit the network. For any other
 * provider an OpenAI-compatible provider is registered from server-owned
 * environment values.
 */
function resolveModel(
  runtime: ModelRuntime,
  request: RunRequest,
) {
  const provider = request.model.provider;
  if (provider === "scripted") {
    registerScriptedProvider(runtime, request.model.name);
  } else {
    registerOpenAiCompatibleProvider(runtime, provider, request.model.name);
  }
  const model = runtime.getModel(provider, request.model.name);
  if (model === undefined) {
    throw new Error(
      `model ${provider}/${request.model.name} is not available`,
    );
  }
  return model;
}

function registerOpenAiCompatibleProvider(
  runtime: ModelRuntime,
  provider: string,
  modelName: string,
): void {
  const baseUrl = requiredEnv("AGENTTRACE_MODEL_BASE_URL");
  const apiKey = requiredEnv("AGENTTRACE_MODEL_API_KEY");
  const api = resolveProviderApi();
  runtime.registerProvider(provider, {
    baseUrl,
    apiKey,
    api,
    models: [registeredModel(modelName, true)],
  });
}

/**
 * Resolves the OpenAI-compatible provider API surface from
 * `AGENTTRACE_PI_PROVIDER_API`. Defaults to `"openai-completions"`; supported
 * values include `"openai-completions"` and `"openai-responses"`.
 */
function resolveProviderApi(): "openai-completions" | "openai-responses" {
  const value = process.env["AGENTTRACE_PI_PROVIDER_API"];
  if (value === undefined || value.trim() === "") {
    return "openai-completions";
  }
  const trimmed = value.trim();
  if (trimmed === "openai-responses") {
    return "openai-responses";
  }
  if (trimmed === "openai-completions") {
    return "openai-completions";
  }
  throw new Error(
    `AGENTTRACE_PI_PROVIDER_API must be "openai-completions" or "openai-responses", got "${trimmed}"`,
  );
}

function registerScriptedProvider(runtime: ModelRuntime, modelName: string): void {
  runtime.registerProvider("scripted", {
    api: "pi-messages",
    baseUrl: "http://scripted.local",
    apiKey: "scripted",
    models: [registeredModel(modelName, false)],
    streamSimple: (model, context) => {
      const stream = createAssistantMessageEventStream();
      const goal = scriptedUserGoal(context.messages);
      const priorToolResults = context.messages.filter(
        (message) => message.role === "toolResult",
      ).length;
      const message = makeScriptedAssistantMessage(
        model.id,
        scriptedResponse(goal, priorToolResults),
        priorToolResults + 1,
      );
      stream.end(message);
      return stream;
    },
  });
}

function registeredModel(modelName: string, reasoning: boolean) {
  return {
    id: modelName,
    name: modelName,
    reasoning,
    input: ["text"] as ("text")[],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    contextWindow: 128_000,
    maxTokens: 4096,
  };
}

interface ScriptedTool {
  name: string;
  arguments: ToolCall["arguments"];
}

type ScriptedResponse =
  | { tool: ScriptedTool }
  | { text: string };

function scriptedResponse(
  goal: string,
  priorToolResults: number,
): ScriptedResponse {
  if (goal.includes("DoesNotExist")) {
    if (priorToolResults === 0) {
      return {
        tool: {
          name: "mcp__browser_harness__open_page",
          arguments: {
            url: "http://fixture-site/index.html",
            intent: "Open the local catalog",
          },
        },
      };
    }
    if (priorToolResults === 1) {
      return {
        tool: {
          name: "mcp__browser_harness__assert_text",
          arguments: {
            text: "DoesNotExist",
            intent: "Verify the missing product is visible",
          },
        },
      };
    }
    return { text: "Final: DoesNotExist was not found." };
  }

  const canonicalTools: ScriptedTool[] = [
    {
      name: "mcp__browser_harness__open_page",
      arguments: {
        url: "http://fixture-site/index.html",
        intent: "Open the local catalog",
      },
    },
    {
      name: "mcp__browser_harness__input",
      arguments: {
        target: { object: { text: "Search items" } },
        value: "alpha",
        submit: true,
        intent: "Filter the catalog to Alpha",
        expect_value: "alpha",
        expect_gone: "Beta",
      },
    },
    {
      name: "mcp__browser_harness__click",
      arguments: {
        target: { object: { text: "Alpha" } },
        intent: "Open the Alpha item",
        expect_url: "detail.html",
        expect_gone: "Loading details",
      },
    },
    {
      name: "mcp__browser_harness__input",
      arguments: {
        target: { object: { text: "Quantity" } },
        value: "2",
        intent: "Set quantity to two",
        expect_value: "2",
      },
    },
    {
      name: "mcp__browser_harness__click",
      arguments: {
        target: { object: { text: "Add to cart" } },
        intent: "Add Alpha to the cart",
        expect_text: "Added!",
      },
    },
    {
      name: "mcp__browser_harness__click",
      arguments: {
        target: { object: { text: "View Cart" } },
        intent: "Open the cart",
        expect_url: "cart.html",
      },
    },
    {
      name: "mcp__browser_harness__assert_text",
      arguments: {
        text: "Alpha x 2",
        intent: "Verify the final cart quantity",
      },
    },
    {
      name: "mcp__browser_harness__finish_case",
      arguments: { name: "canonical" },
    },
  ];

  const tool = canonicalTools[priorToolResults];
  return tool === undefined
    ? { text: "Final: Alpha x 2 is in the cart." }
    : { tool };
}

function scriptedUserGoal(messages: Message[]): string {
  const userMessage = messages.find((message) => message.role === "user");
  if (userMessage === undefined) {
    return "";
  }
  if (typeof userMessage.content === "string") {
    return userMessage.content;
  }
  return userMessage.content
    .filter((content) => content.type === "text")
    .map((content) => content.text)
    .join("\n");
}

function makeScriptedAssistantMessage(
  model: string,
  response: ScriptedResponse,
  step: number,
): AssistantMessage {
  const ts = Date.now();
  return {
    role: "assistant" as const,
    content: "tool" in response
      ? [
          {
            type: "toolCall" as const,
            id: `scripted-tool-${step}`,
            name: response.tool.name,
            arguments: response.tool.arguments,
          },
        ]
      : [{ type: "text" as const, text: response.text }],
    api: "pi-messages",
    provider: "scripted",
    model,
    usage: {
      input: 10,
      output: 5,
      cacheRead: 0,
      cacheWrite: 0,
      totalTokens: 15,
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
    },
    stopReason: "tool" in response ? "toolUse" : "stop",
    timestamp: ts,
  };
}

/**
 * Builds the resource loader with the checked-in system prompt, the single
 * browser-case-authoring skill, and the MCP + browser-harness extensions.
 */
function buildResourceLoader(
  cwd: string,
  extensionFactoriesOverride: InlineExtension[] | undefined,
  request: RunRequest,
  mcpChildPath: string | undefined,
): ResourceLoader {
  const systemPrompt = readFileSync(SYSTEM_PROMPT_PATH, "utf8");
  const { skills } = loadSkillsFromDir({
    dir: SKILLS_DIR,
    source: "browser-harness",
  });

  const extensionFactories: InlineExtension[] =
    extensionFactoriesOverride ?? [
      createSequentialBrowserMcpToolsExtension(),
      createBrowserHarnessExtension({
        mcpChildPath: mcpChildPath ?? defaultMcpChildPath(),
        workerUrl: request.worker_url,
        artifactScope: request.artifact_scope,
        goal: request.goal,
      }),
    ];

  return new DefaultResourceLoader({
    cwd,
    agentDir: cwd,
    extensionFactories,
    noSkills: true,
    noPromptTemplates: true,
    noThemes: true,
    noContextFiles: true,
    systemPromptOverride: () => systemPrompt,
    appendSystemPromptOverride: () => [],
    skillsOverride: () => ({ skills, diagnostics: [] }),
  });
}

function defaultMcpChildPath(): string {
  return fileURLToPath(new URL("./mcp/server.js", import.meta.url));
}

function requiredEnv(name: string): string {
  const value = process.env[name];
  if (value === undefined || value.trim() === "") {
    throw new Error(`environment variable ${name} is required`);
  }
  return value;
}
