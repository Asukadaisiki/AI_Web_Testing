import { CONDITION_TYPES } from "./contract.js";

const CONDITION_NAMES = new Set<string>(CONDITION_TYPES);
const PROTOCOL_IDENTIFIERS = new Set([
  "action", "type", "phase", "status", "verdict", "kind", "case_version", "role", "confidence",
  "toolName", "tool_name", "toolCallId", "tool_call_id", "id", "ref", "target_ref",
  "candidate_id", "observation_id", "page_state_id", "browser_session_id", "execution_id",
  "provider", "model", "api", "stopReason",
]);

/** Attempt-local password values. Only grounded password targets may register values. */
export class PasswordSecrets {
  readonly #values = new Set<string>();

  add(value: unknown): void {
    if (typeof value === "string" && value !== "") this.#values.add(value);
  }

  redact<T>(value: T): T {
    const variants = [...this.#values].flatMap((secret) => [
      secret, JSON.stringify(secret).slice(1, -1), encodeURIComponent(secret),
    ]).sort((left, right) => right.length - left.length);
    const text = (raw: string) => {
      for (const secret of variants) raw = raw.replaceAll(secret, "[redacted]");
      return raw;
    };
    const visit = (item: unknown, key = "", parent?: Record<string, unknown>): unknown => {
      if (typeof item === "string") {
        // Stable protocol identifiers are not credential-bearing evidence.
        if (PROTOCOL_IDENTIFIERS.has(key) || (key === "name" && item.startsWith("mcp__"))) return item;
        if (key === "preconditions" || key === "postconditions") {
          const separator = item.indexOf(":");
          const condition = item.slice(0, separator);
          if (separator >= 0 && CONDITION_NAMES.has(condition)) {
            return `${condition}:${text(item.slice(separator + 1))}`;
          }
        }
        const redacted = text(item);
        if (key === "value" && parent?.action === "goto") {
          try {
            const url = new URL(redacted);
            if (!url.host) throw new Error("missing host");
          } catch {
            return "http://redacted.invalid/";
          }
        }
        return redacted;
      }
      if (Array.isArray(item)) {
        return key === "redacted_args" ? [...item] : item.map((value) => visit(value, key));
      }
      if (typeof item === "object" && item !== null) {
        return Object.fromEntries(Object.entries(item).map(([key, value]) =>
          [key, visit(value, key, item as Record<string, unknown>)]));
      }
      return item;
    };
    return visit(value) as T;
  }
}
