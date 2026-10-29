import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { CallToolRequestSchema, ListToolsRequestSchema } from "@modelcontextprotocol/sdk/types.js";

const server = new Server({ name: "codex-adapter-fixture", version: "1.0.0" }, { capabilities: { tools: {} } });
server.setRequestHandler(ListToolsRequestSchema, () => ({
  tools: [
    { name: "emit_receipt", description: "Return a receipt for the supplied label.",
      inputSchema: { type: "object", properties: { label: { type: "string" } }, required: ["label"], additionalProperties: false } },
    { name: "close_session", description: "Host-only release.",
      inputSchema: { type: "object", properties: {}, additionalProperties: false } },
  ],
}));
server.setRequestHandler(CallToolRequestSchema, ({ params }) => {
  if (params.name === "close_session") return { content: [], structuredContent: { closed: true } };
  if (params.name !== "emit_receipt" || typeof params.arguments?.label !== "string") {
    throw new Error("Invalid receipt request");
  }
  const receipt = `receipt:${params.arguments.label}`;
  return { content: [{ type: "text", text: receipt }], structuredContent: { receipt } };
});
await server.connect(new StdioServerTransport());
