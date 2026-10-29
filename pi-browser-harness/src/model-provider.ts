import type { ModelRuntime } from "@earendil-works/pi-coding-agent";
import {
  createAssistantMessageEventStream,
  type AssistantMessage,
  type Message,
} from "@earendil-works/pi-ai";
import type { HarnessDefinition, ScriptedResponse } from "./harness-definition.js";
import type { RunRequest } from "./run-protocol.js";

export function resolveModel(
  runtime: ModelRuntime,
  request: RunRequest,
  definition: HarnessDefinition,
) {
  const { provider, name } = request.model;
  const model = {
    id: name, name, reasoning: provider !== "scripted",
    input: ["text"] as ("text")[],
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
    contextWindow: 128_000, maxTokens: 4096,
  };
  if (provider === "scripted") {
    runtime.registerProvider(provider, {
      api: "pi-messages", baseUrl: "http://scripted.local", apiKey: "scripted", models: [model],
      streamSimple: (selected, context) => {
        const stream = createAssistantMessageEventStream();
        const prior = context.messages.filter((message) => message.role === "toolResult").length;
        const response = definition.scriptedResponse?.(userGoal(context.messages), prior) ??
          { text: "Final: request completed." };
        stream.end(assistantMessage(selected.id, response, prior + 1));
        return stream;
      },
    });
  } else {
    const api = process.env["AGENTTRACE_PI_PROVIDER_API"]?.trim() || "openai-completions";
    if (api !== "openai-completions" && api !== "openai-responses") {
      throw new Error(`AGENTTRACE_PI_PROVIDER_API must be "openai-completions" or "openai-responses", got "${api}"`);
    }
    runtime.registerProvider(provider, {
      baseUrl: requiredEnv("AGENTTRACE_MODEL_BASE_URL"),
      apiKey: requiredEnv("AGENTTRACE_MODEL_API_KEY"),
      api, models: [model],
    });
  }
  const selected = runtime.getModel(provider, name);
  if (!selected) throw new Error(`model ${provider}/${name} is not available`);
  return selected;
}

function requiredEnv(name: string): string {
  const value = process.env[name];
  if (!value?.trim()) throw new Error(`environment variable ${name} is required`);
  return value;
}

function userGoal(messages: Message[]): string {
  const message = messages.find((item) => item.role === "user");
  if (!message) return "";
  if (typeof message.content === "string") return message.content;
  return message.content.filter((item) => item.type === "text").map((item) => item.text).join("\n");
}

function assistantMessage(model: string, response: ScriptedResponse, step: number): AssistantMessage {
  return {
    role: "assistant",
    content: "tool" in response
      ? [{ type: "toolCall", id: `scripted-tool-${step}`, ...response.tool }]
      : [{ type: "text", text: response.text }],
    api: "pi-messages", provider: "scripted", model,
    usage: {
      input: 10, output: 5, cacheRead: 0, cacheWrite: 0, totalTokens: 15,
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
    },
    stopReason: "tool" in response ? "toolUse" : "stop",
    timestamp: Date.now(),
  };
}
