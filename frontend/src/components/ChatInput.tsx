import type { CSSProperties } from "react";

import { SendOutlined } from "@ant-design/icons";
import { Button, Input } from "antd";

interface ChatInputProps {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  placeholder?: string;
  loading?: boolean;
  ariaLabel?: string;
  sendLabel?: string;
  containerStyle?: CSSProperties;
}

export function ChatInput({
  value,
  onChange,
  onSend,
  placeholder = "描述你想要的操作或修改...",
  loading = false,
  ariaLabel = "自然语言需求",
  sendLabel = "发送",
  containerStyle,
}: ChatInputProps) {
  const canSend = !loading && value.trim().length > 0;

  return (
    <div style={{ padding: "16px 24px 20px", borderTop: "1px solid #f5f5f5", ...containerStyle }}>
      <div
        style={{
          background: "#f0f4f8",
          borderRadius: 24,
          padding: "12px 16px",
          display: "flex",
          alignItems: "flex-end",
          gap: 12,
        }}
      >
        <Input.TextArea
          aria-label={ariaLabel}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder={placeholder}
          autoSize={{ minRows: 1, maxRows: 4 }}
          variant="borderless"
          style={{
            background: "transparent",
            resize: "none",
            fontSize: 14,
            lineHeight: 1.5,
          }}
          onPressEnter={(event) => {
            if (!event.shiftKey) {
              event.preventDefault();
              if (canSend) {
                onSend();
              }
            }
          }}
        />
        <Button
          type="primary"
          shape="circle"
          icon={<SendOutlined />}
          aria-label={sendLabel}
          disabled={!canSend}
          loading={loading}
          onClick={onSend}
          style={{
            width: 40,
            height: 40,
            flexShrink: 0,
            background: canSend ? "#1a1a2e" : undefined,
            borderColor: canSend ? "#1a1a2e" : undefined,
          }}
        />
      </div>
    </div>
  );
}
