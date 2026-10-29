import {
  createMcpExtension,
  type ExtensionAPI,
  type ExtensionFactory,
  type McpExtensionOptions,
  type McpServerConfig,
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

/**
 * Configuration for the browser-harness extension. The extension registers a
 * single Run-scoped stdio MCP server that owns the authoring browser session,
 * blocks every non-browser tool, and cleans up the MCP child on shutdown.
 */
export interface BrowserHarnessExtensionOptions {
  /** Absolute path to the compiled MCP child entry (dist/mcp/server.js). */
  mcpChildPath: string;
  /** Worker URL passed to the MCP child through its environment. */
  workerUrl: string;
  /** Artifact scope passed to the MCP child through its environment. */
  artifactScope: string;
  /** Goal passed to the MCP child through its environment. */
  goal: string;
}

const BROWSER_TOOL_PREFIX = "mcp__browser_harness__";

/**
 * Ensures the built-in MCP extension registers browser tools with Pi's
 * per-tool sequential execution mode. Pi's MCP server configuration exposes
 * tool visibility but not execution mode, so this narrowly decorates only the
 * browser namespace at its supported registerTool boundary.
 */
export function createSequentialBrowserMcpToolsExtension(): ExtensionFactory {
  return (pi: ExtensionAPI) => {
    const registerTool = pi.registerTool.bind(pi);
    const registerBrowserTool: ExtensionAPI["registerTool"] = (tool) => {
      registerTool(
        tool.name.startsWith(BROWSER_TOOL_PREFIX)
          ? { ...tool, executionMode: "sequential" }
          : tool,
      );
    };
    const proxy = new Proxy(pi, {
      get(target, key, receiver) {
        if (key !== "registerTool") {
          return Reflect.get(target, key, receiver);
        }
        return registerBrowserTool;
      },
    });
    return createMcpExtension()(proxy);
  };
}

/**
 * Creates the browser-harness Pi extension. The extension registers the
 * Run-scoped browser MCP server as a direct-exposure stdio child, blocks every
 * tool outside the `mcp__browser_harness__*` namespace, and idempotently
 * unregisters the server (which closes the child and its browser session) on
 * `session_shutdown`.
 */
export function createBrowserHarnessExtension(
  options: BrowserHarnessExtensionOptions,
): ExtensionFactory {
  const serverConfig: McpServerConfig = {
    type: "stdio",
    command: process.execPath,
    args: [options.mcpChildPath],
    env: {
      AGENTTRACE_HARNESS_WORKER_URL: options.workerUrl,
      AGENTTRACE_HARNESS_ARTIFACT_SCOPE: options.artifactScope,
      AGENTTRACE_HARNESS_GOAL: options.goal,
    },
    exposure: "direct",
  };

  return (pi: ExtensionAPI): void => {
    pi.registerMcpServer("browser-harness", serverConfig);

    // Defense in depth: the session allowlist already restricts active tools to
    // mcp__browser_harness__*, but block any other tool call here too.
    pi.on("tool_call", (event) => {
      const name = (event as { toolName?: string }).toolName ?? "";
      if (!name.startsWith(BROWSER_TOOL_PREFIX)) {
        return { block: true, reason: "only browser harness tools are allowed" };
      }
      return undefined;
    });

    pi.on("session_shutdown", () => {
      // Unregistering closes the MCP child, which owns and closes the
      // authoring browser session. Safe to call repeatedly.
      try {
        pi.unregisterMcpServer("browser-harness");
      } catch {
        // already unregistered or connection gone
      }
    });
  };
}
