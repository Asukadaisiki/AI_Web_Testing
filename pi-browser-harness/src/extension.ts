import type {
  ExtensionAPI,
  ExtensionFactory,
  McpServerConfig,
} from "@earendil-works/pi-coding-agent";

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
