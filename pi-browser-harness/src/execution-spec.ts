import { parseSecretReference } from "./secret-resolver.js";

export type JsonValue = string | number | boolean | null | JsonValue[] | { [key: string]: JsonValue };

export interface SnapshotIdentity {
  version_id?: string;
  content_digest?: string;
}

export interface McpCapabilitySnapshot extends SnapshotIdentity {
  capability: string;
  config: Record<string, JsonValue>;
  secret_refs: Record<string, string>;
}

/** Immutable wire snapshot. Keep this SDK-free and aligned with internal/execution. */
export interface ExecutionSpec {
  runtime_id: "pi" | "claude-code" | "codex";
  harness: HarnessSnapshot;
}

export interface HarnessSnapshot extends SnapshotIdentity {
  id: string;
  name: string;
  revision: number;
  system_prompt: string;
  project_id?: string;
  runtime_ids?: ExecutionSpec["runtime_id"][];
  skills: (SnapshotIdentity & {
    name: string;
    content: string;
    files?: { path: string; content: string }[];
  })[];
  mcp: Record<string, McpCapabilitySnapshot>;
}

/** Validates untrusted JSON without resolving secret references or reporting their values. */
export function parseExecutionSpec(value: unknown): ExecutionSpec {
  const spec = record(value, ["runtime_id", "harness"]);
  if (spec.runtime_id !== "pi" && spec.runtime_id !== "claude-code" && spec.runtime_id !== "codex") {
    throw new Error("invalid execution specification");
  }
  const harness = record(spec.harness, ["id", "name", "revision", "system_prompt", "skills", "mcp"],
    ["project_id", "version_id", "content_digest", "runtime_ids"]);
  if (typeof harness.id !== "string" || !/^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(harness.id) ||
      typeof harness.name !== "string" || !harness.name.trim() ||
      typeof harness.revision !== "number" || !Number.isSafeInteger(harness.revision) ||
      harness.revision < 1 || typeof harness.system_prompt !== "string" ||
      !Array.isArray(harness.skills)) throw new Error("invalid harness snapshot");
  const identity = parseIdentity(harness);
  let project_id: string | undefined;
  if (Object.hasOwn(harness, "project_id")) {
    scopedIdentity(harness.project_id);
    project_id = harness.project_id as string;
  }
  let runtime_ids: ExecutionSpec["runtime_id"][] | undefined;
  if (Object.hasOwn(harness, "runtime_ids")) {
    const ids = harness.runtime_ids;
    if (!Array.isArray(ids) || !ids.length || new Set(ids).size !== ids.length ||
      ids.some((id) => !["pi", "claude-code", "codex"].includes(id)) || !ids.includes(spec.runtime_id)) {
      throw new Error("invalid or incompatible Harness runtime_ids");
    }
    runtime_ids = [...ids] as ExecutionSpec["runtime_id"][];
  }
  const skillNames = new Set<string>();
  const skills = harness.skills.map((raw): HarnessSnapshot["skills"][number] => {
    const skill = record(raw, ["name", "content"], ["files", "version_id", "content_digest"]);
    if (typeof skill.name !== "string" || typeof skill.content !== "string") {
      throw new Error("invalid harness skill");
    }
    safePath(skill.name, true);
    unique(skillNames, skill.name);
    const paths = new Set(["skill.md"]);
    let files: { path: string; content: string }[] | undefined;
    if (Object.hasOwn(skill, "files")) {
      if (!Array.isArray(skill.files)) throw new Error("invalid harness skill files");
      files = skill.files.map((rawFile) => {
        const file = record(rawFile, ["path", "content"]);
        if (typeof file.path !== "string" || typeof file.content !== "string") {
          throw new Error("invalid harness skill file");
        }
        safePath(file.path);
        unique(paths, file.path);
        return { path: file.path, content: file.content };
      });
    }
    for (const path of paths) {
      const segments = path.split("/");
      while (segments.length > 1) {
        segments.pop();
        if (paths.has(segments.join("/"))) throw new Error("duplicate skill file path");
      }
    }
    return { name: skill.name, content: skill.content, ...(files ? { files } : {}), ...parseIdentity(skill) };
  });
  const serverNames = new Set<string>();
  const mcp = Object.fromEntries(Object.entries(record(harness.mcp)).map(([name, raw]) => {
    unique(serverNames, normalizeMcpName(name));
    return [name, parseMcpCapability(raw)];
  }));
  return {
    runtime_id: spec.runtime_id,
    harness: {
      id: harness.id, name: harness.name, revision: harness.revision,
      system_prompt: harness.system_prompt, skills, mcp,
      ...identity, ...(project_id ? { project_id } : {}), ...(runtime_ids ? { runtime_ids } : {}),
    },
  };
}

export function normalizeMcpName(name: string): string {
  const normalized = name.replace(/[.-]/g, "_");
  if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(name) || normalized.includes("__") ||
      ["__proto__", "constructor", "prototype"].includes(name)) {
    throw new Error("invalid MCP resource name");
  }
  return normalized;
}

function record(value: unknown, required?: string[], optional: string[] = []): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("invalid execution specification shape");
  }
  const result = value as Record<string, unknown>;
  if (required && (!required.every((key) => Object.hasOwn(result, key)) ||
      Object.keys(result).some((key) => !required.includes(key) && !optional.includes(key)))) {
    throw new Error("invalid execution specification fields");
  }
  return result;
}

function safePath(path: string, singleSegment = false): void {
  if (!path || /[\\:\x00-\x1f]/.test(path) ||
      path.split("/").some((segment) => !segment || segment === "." || segment === "..") ||
      (singleSegment && !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(path))) {
    throw new Error("unsafe harness skill path");
  }
}

function unique(values: Set<string>, value: string): void {
  const key = value.toLowerCase();
  if (values.has(key)) throw new Error("duplicate harness resource path or name");
  values.add(key);
}

function scopedIdentity(value: unknown): asserts value is string {
  if (typeof value !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(value) ||
    ["__proto__", "constructor", "prototype"].includes(value)) {
    throw new Error("invalid scoped snapshot identity");
  }
}

function parseIdentity(value: Record<string, unknown>): SnapshotIdentity {
  const identity: SnapshotIdentity = {};
  if (Object.hasOwn(value, "version_id")) {
    scopedIdentity(value.version_id);
    identity.version_id = value.version_id;
  }
  if (Object.hasOwn(value, "content_digest")) {
    if (typeof value.content_digest !== "string" || !/^(sha256:)?[a-f0-9]{64}$/.test(value.content_digest)) {
      throw new Error("invalid snapshot content digest");
    }
    identity.content_digest = value.content_digest;
  }
  return identity;
}

export function parseMcpCapability(value: unknown): McpCapabilitySnapshot {
  const server = record(value, ["capability", "config", "secret_refs"], ["version_id", "content_digest"]);
  scopedIdentity(server.capability);
  const config = jsonConfig(record(server.config), 0) as Record<string, JsonValue>;
  const secret_refs = Object.fromEntries(Object.entries(record(server.secret_refs)).map(([name, ref]) => {
    scopedIdentity(name);
    parseSecretReference(ref);
    return [name, ref as string];
  }));
  if (server.capability === "browser-harness" && (Object.keys(config).length || Object.keys(secret_refs).length)) {
    throw new Error("browser capability does not accept public config or secrets");
  }
  return { capability: server.capability, config, secret_refs, ...parseIdentity(server) };
}

function jsonConfig(value: unknown, depth: number): JsonValue {
  if (depth > 64) throw new Error("MCP config exceeds nesting limit");
  if (value === null || typeof value === "boolean") return value;
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && !value.includes("\0") && !value.toLowerCase().includes("env:")) return value;
  if (Array.isArray(value)) return value.map((item) => jsonConfig(item, depth + 1));
  if (value !== null && typeof value === "object") {
    const fields = record(value);
    if (Object.getPrototypeOf(fields) !== Object.prototype && Object.getPrototypeOf(fields) !== null) {
      throw new Error("MCP config must contain JSON values");
    }
    return Object.fromEntries(Object.entries(fields).map(([name, item]) => {
      scopedIdentity(name);
      if (/(?:TOKEN|KEY|SECRET|PASSWORD|AUTHORIZATION|COOKIE|CREDENTIAL)/i.test(name) ||
        ["command", "args", "env", "host_only_tools"].includes(name.toLowerCase())) {
        throw new Error("MCP config must contain non-sensitive JSON values");
      }
      return [name, jsonConfig(item, depth + 1)];
    }));
  }
  throw new Error("MCP config must contain non-sensitive JSON values");
}
