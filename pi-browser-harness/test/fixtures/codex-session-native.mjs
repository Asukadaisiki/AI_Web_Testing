#!/usr/bin/env node
import { writeFileSync } from "node:fs";
import { join } from "node:path";

const home = process.env.CODEX_HOME;
let input = "";
process.stdin.setEncoding("utf8");
process.stdin.on("data", (chunk) => { input += chunk; });
process.stdin.on("end", () => {
  if (input === "fixture:inspect") {
    writeFileSync(join(home, "fixture-argv.json"), JSON.stringify({
      argv: process.argv.slice(2), environmentKeys: Object.keys(process.env),
    }));
    process.stdout.write([
      { type: "thread.started", thread_id: "fixture-inspect" },
      { type: "turn.started" },
      { type: "item.completed", item: { type: "agent_message", id: "answer", text: "fixture SDK result" } },
      { type: "turn.completed", usage: { input_tokens: 7, cached_input_tokens: 0, output_tokens: 2 } },
    ].map((event) => JSON.stringify(event)).join("\n") + "\n");
    clearInterval(timer);
    return;
  }
  if (input !== "fixture:cancel") throw new Error("unexpected fixture input");
  writeFileSync(join(home, "fixture-child.ready"), String(process.pid));
  process.stdout.end('{"type":"thread.started","thread_id":"fixture"}\n');
});
const timer = setInterval(() => {}, 1000);
process.on("SIGTERM", () => {
  setTimeout(() => {
    writeFileSync(join(home, "fixture-child.closed"), "closed");
    clearInterval(timer);
    process.exit(0);
  }, 200);
});
