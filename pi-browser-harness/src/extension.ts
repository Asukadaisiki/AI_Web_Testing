import {
  createMcpExtension,
  type ExtensionAPI,
  type ExtensionFactory,
  type McpExtensionOptions,
} from "@earendil-works/pi-coding-agent";
import type { HarnessMcpServer } from "./harness-definition.js";

function namespace(name: string): string {
  return `mcp__${name.replaceAll("-", "_")}__`;
}

export function createHarnessMcpToolsExtension(
  servers: readonly HarnessMcpServer[],
  options: McpExtensionOptions = {},
): ExtensionFactory {
  return (pi) => {
    const registerTool = pi.registerTool.bind(pi);
    return createMcpExtension(options)(new Proxy(pi, {
      get(target, key, receiver) {
        if (key !== "registerTool") return Reflect.get(target, key, receiver);
        return ((tool) => {
          const server = servers.find((server) => tool.name.startsWith(namespace(server.name)));
          const hostOnly = server?.hostOnlyTools?.some(
            (name) => tool.name === `${namespace(server.name)}${name}`,
          );
          registerTool({
            ...tool,
            ...(server?.executionMode ? { executionMode: server.executionMode } : {}),
            ...(hostOnly ? { exposure: "hidden" } : {}),
          });
        }) satisfies ExtensionAPI["registerTool"];
      },
    }));
  };
}

export function createHarnessExtension(
  servers: readonly HarnessMcpServer[], allowlist: readonly string[],
): ExtensionFactory {
  return (pi) => {
    for (const { name, executionMode: _mode, hostOnlyTools, ...config } of servers) {
      pi.registerMcpServer(name, {
        type: "stdio", ...config,
        // MCP-config hidden tools reject even host calls. Defer at the connection,
        // then hide at registerTool so only the host can invoke their definitions.
        ...(hostOnlyTools?.length ? {
          toolExposure: Object.fromEntries(hostOnlyTools.map((tool) => [tool, "deferred" as const])),
        } : {}),
      });
    }
    pi.on("tool_call", (event) => {
      const allowed = allowlist.some((pattern) => pattern.endsWith("*")
        ? event.toolName.startsWith(pattern.slice(0, -1)) : event.toolName === pattern);
      if (!allowed) return { block: true, reason: "tool is not allowed by the selected harness" };
      return undefined;
    });
  };
}
