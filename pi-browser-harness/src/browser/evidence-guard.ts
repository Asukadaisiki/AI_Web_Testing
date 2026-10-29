import { PasswordSecrets } from "./password-secrets.js";
import type { ObservedEvent } from "../harness-definition.js";
import type { HarnessEventWriter } from "../run-protocol.js";

/** Receives only value-free password field markers from the browser MCP builder. */
export class BrowserEvidenceGuard extends PasswordSecrets {
  readonly #calls = new Map<string, Record<string, unknown>>();
  readonly #pending: ObservedEvent[] = [];
  readonly #maxPendingEvents: number;
  readonly #maxPendingBytes: number;
  #pendingBytes = 0;
  #omitModelContent = false;

  constructor(limits: { maxPendingEvents?: number; maxPendingBytes?: number } = {}) {
    super();
    this.#maxPendingEvents = limits.maxPendingEvents ?? 256;
    this.#maxPendingBytes = limits.maxPendingBytes ?? 4 * 1024 * 1024;
    if (![this.#maxPendingEvents, this.#maxPendingBytes].every((value) =>
      Number.isSafeInteger(value) && value > 0)) {
      throw new Error("Browser telemetry limits must be positive safe integers");
    }
  }

  publicWriter(writer: Pick<HarnessEventWriter, "write" | "hasTerminalEvent">):
    Pick<HarnessEventWriter, "write" | "hasTerminalEvent"> {
    return {
      get hasTerminalEvent() { return writer.hasTerminalEvent; },
      write: (type, payload) => {
        if (writer.hasTerminalEvent) return undefined;
        if (type === "pi.tool.started") {
          const start = payload as Extract<ObservedEvent, { type: "pi.tool.started" }>["payload"];
          return writer.write(type, {
            tool_call_id: start.tool_call_id, tool_name: start.tool_name,
          } as typeof payload);
        }
        if (type !== "pi.message.completed" && type !== "pi.usage" &&
          type !== "harness.settled" && type !== "harness.failed") {
          // Grounded tool/browser evidence and value-free progress must remain live.
          return writer.write(type, this.redact(payload));
        }
        const event = { type, payload } as ObservedEvent;
        if (type === "harness.settled" || type === "harness.failed") {
          const unclassified = this.#omitModelContent || type === "harness.failed" || this.#calls.size > 0;
          this.#flush(writer, unclassified);
          const publicEvent = this.#publicEvent(event, unclassified);
          return writer.write(type, publicEvent.payload as typeof payload);
        }
        // Later tool turns can classify different passwords in earlier messages.
        // Overflow releases metadata/usage only, never unclassified model content.
        if (!this.#omitModelContent) {
          const bytes = Buffer.byteLength(JSON.stringify(payload));
          if (this.#pending.length < this.#maxPendingEvents &&
            this.#pendingBytes + bytes <= this.#maxPendingBytes) {
            this.#pending.push({ type, payload: this.redact(payload) } as ObservedEvent);
            this.#pendingBytes += bytes;
            return undefined;
          }
          this.#omitModelContent = true;
          this.#flush(writer, true);
        }
        const publicEvent = this.#publicEvent(event, true);
        return writer.write(type, publicEvent.payload as typeof payload);
      },
    };
  }

  #flush(writer: Pick<HarnessEventWriter, "write">, omit: boolean): void {
    const pending = this.#pending.splice(0);
    this.#pendingBytes = 0;
    for (const event of pending) {
      const publicEvent = this.#publicEvent(event, omit);
      writer.write(publicEvent.type, publicEvent.payload);
    }
  }

  #publicEvent(event: ObservedEvent, omit: boolean): ObservedEvent {
    if (omit && event.type === "pi.message.completed") {
      // Whitelist span metadata so arbitrary extension fields cannot carry prose.
      return { type: event.type, payload: { text: "", ...metadata(event.payload, [
        "provider", "model", "started_at", "ended_at", "granularity",
        "provider_source", "model_source", "timing_source",
      ]) } };
    }
    if (omit && event.type === "pi.usage") {
      return { type: event.type, payload: {
        ...metadata(event.payload, ["provider", "model", "granularity", "usage_scope",
          "provider_source", "model_source", "timing_scope"]),
        ...Object.fromEntries(Object.entries(event.payload).filter(([, value]) => typeof value === "number")),
        input_tokens: event.payload.input_tokens, output_tokens: event.payload.output_tokens,
      } };
    }
    if (event.type === "harness.failed") {
      return { type: event.type, payload: { error: "Browser model execution failed" } };
    }
    if (omit && event.type === "harness.settled") {
      return { type: event.type, payload: { assistant_text: "", verdict: event.payload.verdict } };
    }
    return { type: event.type, payload: this.redact(event.payload) } as ObservedEvent;
  }

  discardPending(): void {
    this.#pending.length = 0;
    this.#pendingBytes = 0;
  }

  observe(raw: unknown): void {
    if (!isRecord(raw) || typeof raw.toolName !== "string" ||
      !raw.toolName.startsWith("mcp__browser_harness__") || typeof raw.toolCallId !== "string") return;
    if (raw.type === "tool_execution_start" && isRecord(raw.args)) {
      this.#calls.set(raw.toolCallId, structuredClone(raw.args));
    }
    if (raw.type !== "tool_execution_end") return;
    const args = this.#calls.get(raw.toolCallId) ?? (isRecord(raw.args) ? raw.args : undefined);
    this.#calls.delete(raw.toolCallId);
    if (!args || !isRecord(raw.result)) {
      this.#omitModelContent = true;
      return;
    }
    let structured = raw.result.structuredContent;
    if (isRecord(structured) && Array.isArray(structured.content)) {
      structured = structured.structuredContent;
    }
    if (!isRecord(structured) || !Array.isArray(structured.redacted_args)) {
      if (raw.isError === true || raw.result.isError === true) this.#omitModelContent = true;
      return;
    }
    for (const field of structured.redacted_args) {
      if (field === "value" || field === "expect_value" || field === "expect_attribute") {
        this.add(args[field]);
        if (field === "expect_attribute" && typeof args[field] === "string") {
          const separator = args[field].indexOf("=");
          if (separator >= 0) this.add(args[field].slice(separator + 1).trim());
        }
      }
    }
  }

  modelContext<T extends { messages: unknown[] }>(context: T): T {
    // The user's canonical goal is user-authored, not browser evidence.
    const clone = JSON.parse(JSON.stringify(context)) as T;
    return { ...this.redact({ ...clone, messages: [] }), messages: clone.messages.map((message) =>
      isRecord(message) && message.role === "user" ? message : this.redact(message)) };
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function metadata(payload: Record<string, unknown>, keys: string[]): Record<string, unknown> {
  return Object.fromEntries(keys.filter((key) => Object.hasOwn(payload, key))
    .map((key) => [key, payload[key]]));
}
