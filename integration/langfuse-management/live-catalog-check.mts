import { createRequire } from "node:module";
import { parseArgs } from "node:util";
import { pathToFileURL } from "node:url";
import { writeFileSync } from "node:fs";

const { values } = parseArgs({ options: {
  source: { type: "string", default: "/Users/bytedance/project/langfuse-agenttrace" },
  "env-file": { type: "string" },
  output: { type: "string", default: "/Users/bytedance/project/AI_Web_Testing/integration/langfuse-management/live-catalog-evidence.json" },
  diagnose: { type: "boolean", default: false },
} });
const source = values.source!;
const dotenv = createRequire(`${source}/web/package.json`)("dotenv");
dotenv.config({ path: values["env-file"], override: true, quiet: true });
const env = process.env;
const results: { check: string; passed: boolean; count?: number; code?: string }[] = [];
const requests: { path: string; status: number }[] = [];
try {
  const { AgentTraceClient } = await import(pathToFileURL(`${source}/web/src/features/browser-runs/server/agenttrace-client.ts`).href);
  const client = new AgentTraceClient({
    apiUrl: env.AGENTTRACE_API_URL,
    bridgeToken: env.AGENTTRACE_BRIDGE_TOKEN,
    langfuseProjectId: env.AGENTTRACE_LANGFUSE_PROJECT_ID,
    agenttraceProjectId: env.AGENTTRACE_PROJECT_ID,
    actorId: "langfuse-management-acceptance", role: "admin",
  }, async (input: URL | RequestInfo, init: RequestInit) => {
    const response = await fetch(input, init);
    requests.push({ path: new URL(String(input)).pathname, status: response.status });
    if (values.diagnose && response.ok && /mcp-bindings|harnesses/.test(new URL(String(input)).pathname)) {
      const json = await response.clone().json() as Record<string, Record<string, unknown>[]>;
      for (const [wrapper, items] of Object.entries(json)) {
        console.log(JSON.stringify({ wrapper, rows: items.map((item) => Object.fromEntries(Object.entries(item).map(([key, value]) => [key, value === null ? "null" : Array.isArray(value) ? `array:${value.length}` : typeof value]))) }));
      }
    }
    return response;
  });
  await client.verifyBinding();
  for (const check of ["listRuntimes", "listSkills", "listMcpDefinitions", "listMcpBindings", "listHarnesses", "services"]) {
    try {
      const data = await client[check]();
      results.push({ check, passed: true, ...(Array.isArray(data) ? { count: data.length } : {}) });
    } catch (error) {
      results.push({ check, passed: false, code: error && typeof error === "object" && "code" in error ? String(error.code) : "unavailable" });
    }
  }
} catch {
  results.push({ check: "binding", passed: false });
}
const passed = results.length === 6 && results.every((result) => result.passed);
writeFileSync(values.output!, `${JSON.stringify({ checked_at: new Date().toISOString(), passed, results, requests }, null, 2)}\n`);
console.log(JSON.stringify({ passed, results, output: values.output }));
if (!passed) process.exitCode = 1;
