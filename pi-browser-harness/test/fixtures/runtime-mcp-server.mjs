import { appendFile, access } from "node:fs/promises";
import { join } from "node:path";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { CallToolRequestSchema, ListToolsRequestSchema } from "@modelcontextprotocol/sdk/types.js";

const logPath = join(process.cwd(), "lifecycle.log");
const log = (message) => appendFile(logPath, `${message}\n`);
const waitFor = async (name) => {
  for (;;) {
    try { await access(join(process.cwd(), name)); return; }
    catch { await new Promise((resolve) => setTimeout(resolve, 10)); }
  }
};
const server = new Server({ name: "real-stdio-fixture", version: "1.0.0" }, { capabilities: { tools: {} } });
server.oninitialized = () => { void log("initialized"); };
const names = ["echo-tool", "password_input", "fail", "throw", "open_page", "finish_case", "close_session", "release"];
server.setRequestHandler(ListToolsRequestSchema, () => ({
  tools: names.map((name) => ({ name, description: `Fixture ${name}`,
    inputSchema: { type: "object", properties: {}, additionalProperties: true } })),
}));

const browserCase = {
  case_version: "loop.case.v1", name: "Open example", goal: "Verify example",
  base_url: "https://example.test",
  steps: [{ index: 0, action: "goto", intent: "Open example", value: "https://example.test",
    submit: false, preconditions: [],
    postconditions: [{ type: "url_contains", value: "example.test", timeout_ms: 1000 }],
    timeout_ms: 1000 }],
};

server.setRequestHandler(CallToolRequestSchema, async ({ params }) => {
  await log(`call:${params.name}`);
  const args = params.arguments ?? {};
  if (params.name === "password_input") return {
    content: [], structuredContent: {
      accepted: args.value === process.env.PASSWORD_CANARY, redacted_args: ["value"],
    },
  };
  if (params.name === "echo-tool" && args.wait === true) await waitFor("allow-tool");
  if (params.name === "throw") throw new Error("fixture protocol failure");
  if (params.name === "fail") return {
    content: [{ type: "text", text: "fixture tool failed" }], isError: true,
    structuredContent: { ok: false },
  };
  if (params.name === "close_session" || params.name === "release") {
    if (process.env.WAIT_FOR_CLEANUP === "yes") {
      await waitFor("allow-cleanup");
    }
    if (process.env.CLEANUP_FAIL === "yes") return {
      content: [{ type: "text", text: "cleanup uncertain" }], isError: true,
    };
    await log("acknowledged");
    return { content: [], structuredContent: {
      closed: process.env.CLEANUP_MISSING !== "yes", instance: process.env.INSTANCE,
    } };
  }
  if (params.name === "finish_case") {
    const status = args.status === "failed" ? "failed" : "passed";
    const dryRun = {
      execution_id: "execution-1", status,
      started_at: "2026-10-28T12:00:00.000Z", finished_at: "2026-10-28T12:00:01.000Z",
      final_url: "https://example.test", error: null,
      steps: [{ index: 0, action: "goto", status,
        started_at: "2026-10-28T12:00:00.000Z", duration_ms: 10,
        url_before: "about:blank", url_after: "https://example.test", conditions: [],
        evidence: { screenshot_path: null, console: [], network: [] }, error: null }],
    };
    if (args.empty === true) return { content: [], structuredContent: {
      ok: true, case: { ...browserCase, steps: [] }, dry_run: { ...dryRun, steps: [] },
    } };
    if (args.inconsistent === true) dryRun.steps = [];
    return { content: [], structuredContent: { ok: status === "passed", case: browserCase, dry_run: dryRun } };
  }
  if (params.name === "open_page") return { content: [], structuredContent: {
    ok: true, page: { url: "https://example.test", title: "Example", elements: [] },
  } };
  return {
    content: [{ type: "text", text: JSON.stringify(args) }],
    structuredContent: { args, pid: process.pid, cwd: process.cwd(), instance: process.env.INSTANCE },
  };
});
process.stdin.once("end", () => {
  void log("disconnected").finally(() => process.exit(0));
});
await server.connect(new StdioServerTransport());
