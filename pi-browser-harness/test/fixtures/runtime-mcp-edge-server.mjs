import { spawn } from "node:child_process";
import { appendFile, access } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema, InitializeRequestSchema, ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

const instance = process.env.INSTANCE ?? "edge";
const path = (name) => join(process.cwd(), `${instance}-${name}`);
const log = (message) => appendFile(path("lifecycle.log"), `${message}\n`);
const waitFor = async (name) => {
  for (;;) {
    try { await access(path(name)); return; }
    catch { await new Promise((resolve) => setTimeout(resolve, 10)); }
  }
};

if (process.argv[2] === "hold-stdout") {
  await log(`holder:${process.pid}`);
  // An inherited pipe keeps the parent's real child.close pending after SIGKILL.
  await waitFor("allow-close");
  await log("holder-exited");
  process.exit(0);
}

await log(`server:${process.pid}`);
if (process.env.HOLD_STDOUT === "yes") {
  const holder = spawn(process.execPath, [fileURLToPath(import.meta.url), "hold-stdout"], {
    stdio: ["ignore", "inherit", "ignore"], env: process.env,
  });
  await new Promise((resolve, reject) => {
    holder.once("spawn", resolve);
    holder.once("error", reject);
  });
}
if (process.env.SHUTDOWN_MODE) {
  setInterval(() => {}, 1000);
  process.on("SIGTERM", () => {
    void log("sigterm");
    if (process.env.SHUTDOWN_MODE === "delayed") {
      void waitFor("allow-exit").then(async () => {
        await log("server-exiting");
        process.exit(0);
      });
    }
  });
}
process.stdin.once("end", () => {
  void log("eof").then(() => {
    if (!process.env.SHUTDOWN_MODE) process.exit(0);
  });
});

const server = new Server({ name: "edge-fixture", version: "1.0.0" }, { capabilities: { tools: {} } });
if (process.env.FAIL_INITIALIZE === "yes") {
  server.setRequestHandler(InitializeRequestSchema, () => {
    throw new Error("fixture handshake rejected");
  });
}
const names = ["inspect", "text_success", "text_error", "text_false", "close_session"];
server.setRequestHandler(ListToolsRequestSchema, () => ({
  tools: names.map((name) => ({ name, inputSchema: { type: "object", properties: {} } })),
}));
server.setRequestHandler(CallToolRequestSchema, async ({ params }) => {
  if (params.name === "close_session") {
    await log("acknowledged");
    return { content: [], structuredContent: { closed: true } };
  }
  if (params.name === "text_success") return {
    content: [{ type: "text", text: "actual text observation" }],
  };
  if (params.name === "text_error") return {
    content: [{ type: "text", text: "actual text failure details" }], isError: true,
  };
  if (params.name === "text_false") return {
    content: [{ type: "text", text: "explicit successful observation" }], isError: false,
  };
  return { content: [], structuredContent: { pid: process.pid, instance } };
});
await server.connect(new StdioServerTransport());
