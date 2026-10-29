import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { createInterface } from "node:readline";

const input = createInterface({ input: process.stdin });
const keepAlive = setInterval(() => {}, 1000);
let terminating = false;
input.on("line", (line) => {
  const message = JSON.parse(line);
  if (message.type === "control_request") {
    process.stdout.write(`${JSON.stringify({
      type: "control_response",
      response: {
        subtype: "success", request_id: message.request_id,
        response: { commands: [], models: [], account: {} },
      },
    })}\n`);
  } else if (message.type === "user") {
    writeFileSync(join(process.cwd(), "child-ready"), "ready");
  }
});
input.on("close", () => { process.stdout.end(); });
process.on("SIGTERM", () => {
  if (terminating) return;
  terminating = true;
  setTimeout(() => {
    writeFileSync(join(process.cwd(), "child-terminated"), "terminated");
    clearInterval(keepAlive);
    process.exit(0);
  }, 3000);
});
