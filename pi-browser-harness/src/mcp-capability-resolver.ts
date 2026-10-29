import { normalizeMcpName, parseMcpCapability, type JsonValue, type McpCapabilitySnapshot } from "./execution-spec.js";
import type { HarnessMcpServer, HarnessDefinition } from "./harness-definition.js";
import type { RunRequest } from "./run-protocol.js";
import { parseSecretReference, resolveSecret, type SecretResolution } from "./secret-resolver.js";

/** Private, reviewed deployment configuration. Never serialized into a Run snapshot. */
export interface ReviewedMcpCapability {
  command: string;
  args?: readonly string[];
  env?: Readonly<Record<string, string>>;
  config: Readonly<Record<string, {
    type: "string" | "number" | "boolean" | "json";
    env: string;
    required?: boolean;
  }>>;
  /** Logical secret slot to a fixed deployment env name. All declared slots are required. */
  secrets: Readonly<Record<string, string>>;
  hostOnlyTools?: readonly string[];
  version_id?: string;
  content_digest?: string;
}

export interface McpResolutionOptions {
  capabilities?: ReadonlyMap<string, ReviewedMcpCapability>;
  secrets?: SecretResolution;
}

export async function resolveMcpCapabilities(
  snapshots: Record<string, McpCapabilitySnapshot>, request: RunRequest,
  options: McpResolutionOptions, browser?: HarnessDefinition,
): Promise<HarnessMcpServer[]> {
  const names = new Set<string>();
  const servers: HarnessMcpServer[] = [];
  for (const [rawName, rawSnapshot] of Object.entries(snapshots)) {
    const snapshot = parseMcpCapability(rawSnapshot);
    if (snapshot.capability === "browser-harness") {
      if (!browser) throw new Error("browser capability is unavailable");
      const builtins = typeof browser.mcpServers === "function" ? browser.mcpServers(request) : browser.mcpServers;
      for (const server of builtins) {
        reserveName(server.name, names);
        servers.push(server);
      }
      continue;
    }
    const name = normalizeMcpName(rawName);
    if (name.toLowerCase() === "browser_harness") throw new Error("reserved browser capability name");
    reserveName(name, names);
    const deployed = options.capabilities?.get(snapshot.capability);
    if (!deployed) throw new Error("MCP capability is not deployed");
    for (const key of ["version_id", "content_digest"] as const) {
      if (deployed[key] !== undefined && snapshot[key] !== deployed[key]) {
        throw new Error("MCP capability identity does not match deployment");
      }
    }
    const env: Record<string, string> = { ...deployed.env };
    const mapped = new Set(Object.keys(env));
    const mapValue = (key: string, value: string): void => {
      if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key) || ["__proto__", "constructor", "prototype"].includes(key) ||
        mapped.has(key) || value.includes("\0")) throw new Error("invalid deployed MCP environment mapping");
      mapped.add(key);
      env[key] = value;
    };
    for (const [key, value] of Object.entries(snapshot.config)) {
      if (!Object.hasOwn(deployed.config, key)) throw new Error("MCP config is not whitelisted");
      const field = deployed.config[key]!;
      if (field.type !== "json" && typeof value !== field.type) throw new Error("invalid MCP config type");
      mapValue(field.env, serializeConfig(value));
    }
    for (const [key, field] of Object.entries(deployed.config)) {
      if (field.required && !Object.hasOwn(snapshot.config, key)) throw new Error("required MCP config is missing");
    }
    const slots = Object.keys(deployed.secrets);
    if (Object.keys(snapshot.secret_refs).length !== slots.length ||
      slots.some((slot) => !Object.hasOwn(snapshot.secret_refs, slot))) {
      throw new Error("MCP secret slots are not whitelisted");
    }
    // Check every reference before fetching any secret, including cross-scope requests.
    for (const ref of Object.values(snapshot.secret_refs)) {
      if (!options.secrets || parseSecretReference(ref).scope !== options.secrets.scope) {
        throw new Error("MCP secret resolver is unavailable or out of scope");
      }
    }
    for (const [slot, ref] of Object.entries(snapshot.secret_refs)) {
      mapValue(deployed.secrets[slot]!, await resolveSecret(ref, options.secrets));
    }
    const hostOnlyTools = [...(deployed.hostOnlyTools ?? [])];
    const tools = new Set<string>();
    for (const tool of hostOnlyTools) reserveName(tool, tools);
    servers.push({
      name, command: deployed.command, ...(deployed.args ? { args: [...deployed.args] } : {}),
      env, hostOnlyTools, exposure: "direct", executionMode: "parallel",
    });
  }
  return servers;
}

function reserveName(name: string, names: Set<string>): void {
  const normalized = normalizeMcpName(name).toLowerCase();
  if (names.has(normalized)) throw new Error("duplicate resolved MCP name");
  names.add(normalized);
}

function serializeConfig(value: JsonValue): string {
  return typeof value === "string" ? value : JSON.stringify(value);
}
