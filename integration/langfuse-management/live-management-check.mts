import { createRequire } from "node:module";
import { parseArgs } from "node:util";
import { pathToFileURL } from "node:url";
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import assert from "node:assert/strict";

const { values } = parseArgs({ options: {
  source: { type: "string", default: "/Users/bytedance/project/langfuse-agenttrace" },
  "env-file": { type: "string", multiple: true },
  output: { type: "string", default: "/Users/bytedance/project/AI_Web_Testing/integration/langfuse-management/live-evidence.json" },
  "actor-id": { type: "string", default: "langfuse-management-acceptance" },
  "wait-seconds": { type: "string", default: "0" },
} });
const source = resolve(values.source!);
const require = createRequire(`${source}/web/package.json`);
const dotenv = require("dotenv") as { config: (options: object) => unknown };
for (const path of values["env-file"] ?? []) dotenv.config({ path, override: true, quiet: true });
const get = (name: string) => {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`Missing ${name}`);
  return value;
};
const config = {
  apiUrl: get("AGENTTRACE_API_URL"),
  bridgeToken: get("AGENTTRACE_BRIDGE_TOKEN"),
  langfuseProjectId: get("AGENTTRACE_LANGFUSE_PROJECT_ID"),
  agenttraceProjectId: get("AGENTTRACE_PROJECT_ID"),
  actorId: values["actor-id"]!,
  role: "admin" as const,
};
const requests: { method: string; path: string; status: number; input?: unknown }[] = [];
const upstream: typeof fetch = async (input, init) => {
  const response = await fetch(input, init);
  const url = new URL(String(input));
  requests.push({
    method: init?.method ?? "GET", path: url.pathname, status: response.status,
    // Only known non-secret conversation request fields are persisted.
    ...(init?.body && url.pathname.includes("conversations") ? { input: JSON.parse(String(init.body)) } : {}),
  });
  return response;
};
try {
  const waitSeconds = Math.min(600, Math.max(0, Number(values["wait-seconds"]) || 0));
  const deadline = Date.now() + waitSeconds * 1000;
  while (waitSeconds > 0) {
    try {
      const health = await fetch(new URL("/api/v1/health", config.apiUrl), { signal: AbortSignal.timeout(2000) });
      if (health.ok) break;
    } catch { /* Parent starts the management API independently. */ }
    if (Date.now() >= deadline) throw new Error("API health timeout");
    await new Promise((done) => setTimeout(done, 3000));
  }
  const { AgentTraceClient } = await import(pathToFileURL(`${source}/web/src/features/browser-runs/server/agenttrace-client.ts`).href);
  const client = new AgentTraceClient(config, upstream);
  await client.verifyBinding();
  const before = await client.listConversations();
  const title = `Langfuse BFF acceptance ${new Date().toISOString()}`;
  const created = await client.createConversation(title);
  assert.equal(created.name, title);
  assert.equal(created.status, "active");
  assert.equal(created.project_id, config.agenttraceProjectId);
  const listed = await client.listConversations();
  assert.ok(listed.some((item: { id: string }) => item.id === created.id));
  const archived = await client.updateConversation(created.id, "archived");
  assert.equal(archived.status, "archived");
  const final = await client.listConversations();
  assert.ok(final.some((item: { id: string; status: string }) => item.id === created.id && item.status === "archived"));
  const evidence = { checked_at: new Date().toISOString(), passed: true, api_origin: new URL(config.apiUrl).origin, project_id: config.agenttraceProjectId, langfuse_project_id: config.langfuseProjectId, conversation_id: created.id, initial_count: before.length, final_count: final.length, requests };
  writeFileSync(values.output!, `${JSON.stringify(evidence, null, 2)}\n`);
  console.log(JSON.stringify({ passed: true, output: values.output, conversation_id: created.id, statuses: requests.map((request) => request.status) }));
} catch (error) {
  writeFileSync(values.output!, `${JSON.stringify({ checked_at: new Date().toISOString(), passed: false, requests }, null, 2)}\n`);
  const code = error && typeof error === "object" && "code" in error ? String(error.code) : "unavailable";
  console.error(`Real management BFF check failed (${code}); inspect sanitized request statuses and ensure the management API is running.`);
  process.exitCode = 1;
}
