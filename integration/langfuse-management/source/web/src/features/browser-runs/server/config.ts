import { env } from "@/src/env.mjs";
import type { BridgeConfig } from "@/src/features/browser-runs/contracts";

export function readBridgeConfig(
  projectId: string,
): Omit<BridgeConfig, "actorId" | "role"> | null {
  let bindings: Record<string, string> = {};
  try {
    bindings = JSON.parse(env.AGENTTRACE_PROJECT_BINDINGS?.trim() || "{}");
    if (!bindings || typeof bindings !== "object" || Array.isArray(bindings))
      return null;
  } catch {
    return null;
  }
  const mapped = Object.hasOwn(bindings, projectId)
    ? bindings[projectId]
    : projectId === env.AGENTTRACE_LANGFUSE_PROJECT_ID?.trim()
      ? env.AGENTTRACE_PROJECT_ID?.trim()
      : undefined;
  const values = {
    apiUrl: env.AGENTTRACE_API_URL?.trim(),
    bridgeToken: env.AGENTTRACE_BRIDGE_TOKEN?.trim(),
    langfuseProjectId: projectId,
    agenttraceProjectId: typeof mapped === "string" ? mapped.trim() : undefined,
  };
  if (Object.values(values).some((value) => !value)) {
    return null;
  }
  return values as Omit<BridgeConfig, "actorId" | "role">;
}
