import type { ReactNode } from "react";
import { Typography } from "antd";

interface ChatMessageProps {
  role: "user" | "assistant";
  content: string;
  structuredData?: ReactNode;
}

export function ChatMessage({ role, content, structuredData }: ChatMessageProps) {
  if (role === "user") {
    return (
      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <div className="chat-bubble-user">{content}</div>
      </div>
    );
  }

  return (
    <div style={{ display: "flex", justifyContent: "flex-start" }}>
      <div className="chat-bubble-ai">
        <Typography.Text strong style={{ fontSize: 13 }}>
          AI 助手
        </Typography.Text>
        <div style={{ marginTop: 4 }}>{content}</div>
        {structuredData ? <div style={{ marginTop: 8 }}>{structuredData}</div> : null}
      </div>
    </div>
  );
}
