export default {
  systemPrompt: "Respond directly without tools.",
  skillPaths: [],
  toolAllowlist: [],
  mcpServers: [],
  completionPolicy: ({ assistant_settled, aborted }) => ({
    verdict: assistant_settled && !aborted ? "succeeded" : "failed",
  }),
};
