export interface SecretResolution {
  /** Trusted scope, obtained from authenticated execution context, not an MCP descriptor. */
  scope: string;
  resolve(ref: string): string | undefined | Promise<string | undefined>;
}

export function parseSecretReference(value: unknown): { scope: string; name: string } {
  const match = typeof value === "string"
    ? /^secret:\/\/([A-Za-z0-9][A-Za-z0-9._-]*)\/([A-Za-z0-9][A-Za-z0-9._-]*)$/.exec(value) : null;
  if (!match || match.slice(1).some((part) => ["__proto__", "constructor", "prototype"].includes(part))) {
    throw new Error("invalid scoped MCP secret reference");
  }
  return { scope: match[1]!, name: match[2]! };
}

/** Resolve only after scope validation; never consult ambient host environment. */
export async function resolveSecret(ref: string, resolver?: SecretResolution): Promise<string> {
  const parsed = parseSecretReference(ref);
  if (!resolver || parsed.scope !== resolver.scope) {
    throw new Error("MCP secret resolver is unavailable or out of scope");
  }
  let value: unknown;
  try { value = await resolver.resolve(ref); } catch {
    throw new Error("MCP secret resolution failed");
  }
  if (typeof value !== "string" || !value || value.includes("\0")) {
    throw new Error("MCP secret is unavailable");
  }
  return value;
}
