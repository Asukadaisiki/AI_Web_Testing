package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Asukadaisiki/AI_Web_Testing/v2/backend/internal/usage"
)

// ScriptedStep 是离线脚本的一步。
//
// 三种形态：
//   - 只给 Tool + Arguments：返回一次工具调用；
//   - 只给 Final：返回一段纯文本（用于测试"只说话不调工具"的分支）；
//   - Ask 是 Tool=ask_user 的简写。
type ScriptedStep struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Final     string          `json:"final"`
	Ask       string          `json:"ask"`
}

// ScriptedLLM 回放一份固定脚本，用于离线、确定性地跑通整条闭环。
type ScriptedLLM struct {
	mu     sync.Mutex
	index  int
	steps  []ScriptedStep
	seen   []Message
	repeat string
}

// NewScriptedLLM 创建脚本模型。
func NewScriptedLLM(steps []ScriptedStep) *ScriptedLLM {
	return &ScriptedLLM{steps: steps}
}

// ScriptedFromJSON 从 JSON 解析脚本，形如 {"steps":[{...}]}。
func ScriptedFromJSON(raw []byte) (*ScriptedLLM, error) {
	var parsed struct {
		Steps []ScriptedStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse scripted llm: %w", err)
	}
	if len(parsed.Steps) == 0 {
		return nil, fmt.Errorf("scripted llm needs at least one step")
	}
	return NewScriptedLLM(parsed.Steps), nil
}

// Label 实现 LLM。
func (s *ScriptedLLM) Label() string { return "scripted" }

// Restart 让脚本从头开始。脚本模型一次只服务一个 run，控制面在每个 run 开始时调用它，
// 这样"离线跑第二遍"不会撞上已经回放完的脚本。
func (s *ScriptedLLM) Restart() {
	s.mu.Lock()
	s.index = 0
	s.seen = nil
	s.mu.Unlock()
}

// Calls 返回已经回放了多少步。
func (s *ScriptedLLM) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index
}

// Seen 返回收到的全部消息，便于测试断言提示词与工具结果。
func (s *ScriptedLLM) Seen() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Message, len(s.seen))
	copy(out, s.seen)
	return out
}

// Next 实现 LLM：按顺序回放；脚本用尽后重复最后一条（默认要求收尾）。
//
// 离线脚本不花钱，所以用量一律返回零值——它不是"0 token"，而是"没有真实调用"。
// 成本熔断因此对脚本模型天然不生效，这也正是离线验证可以反复跑的原因。
func (s *ScriptedLLM) Next(_ context.Context, messages []Message) (Message, usage.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, messages...)
	if s.index >= len(s.steps) {
		if s.repeat != "" {
			return Message{Role: RoleAssistant, Content: s.repeat}, usage.Usage{}, nil
		}
		return Message{Role: RoleAssistant, Content: "script exhausted"}, usage.Usage{}, nil
	}
	step := s.steps[s.index]
	s.index++
	if step.Final != "" {
		return Message{Role: RoleAssistant, Content: step.Final}, usage.Usage{}, nil
	}
	name := step.Tool
	arguments := step.Arguments
	if step.Ask != "" {
		name = "ask_user"
		encoded, err := json.Marshal(map[string]string{"question": step.Ask})
		if err != nil {
			return Message{}, usage.Usage{}, err
		}
		arguments = encoded
	}
	if name == "" {
		return Message{}, usage.Usage{}, fmt.Errorf("scripted step %d has neither tool nor final", s.index-1)
	}
	if len(arguments) == 0 {
		arguments = json.RawMessage("{}")
	}
	return Message{
		Role: RoleAssistant,
		ToolCalls: []ToolCall{{
			ID:        fmt.Sprintf("call_%d", s.index),
			Name:      name,
			Arguments: arguments,
		}},
	}, usage.Usage{}, nil
}

// scriptedSummary 只用于错误信息里提示脚本进度。
func (s *ScriptedLLM) scriptedSummary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.steps))
	for _, step := range s.steps {
		if step.Ask != "" {
			names = append(names, "ask_user")
			continue
		}
		if step.Tool != "" {
			names = append(names, step.Tool)
			continue
		}
		names = append(names, "final")
	}
	return strings.Join(names, " → ")
}
