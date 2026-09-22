// Package agentruntime 驱动"输入 → 规划"这一段：模型只输出工具调用，Go 负责构建 case。
package agentruntime

import (
	"context"
	"encoding/json"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/usage"
)

// Role 是消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall 是模型请求调用的一次工具。
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Message 是对话里的一条消息。
type Message struct {
	Role             Role       `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

// LLM 是模型接口。两个实现：OpenAI 兼容的真实模型，以及离线脚本回放。
type LLM interface {
	// Label 用于日志与事件，说明当前用的是哪个模型。
	Label() string
	// Next 返回模型的下一步输出：要么带 ToolCalls，要么只带 Content，
	// 以及这次调用花掉的 token（脚本模型返回零值）。
	//
	// 用量与输出一起返回，是为了让 Runtime 能在**同一轮**里决定是否已经超预算，
	// 而不是等整条链路跑完才发现钱花超了。
	Next(ctx context.Context, messages []Message) (Message, usage.Usage, error)
}
