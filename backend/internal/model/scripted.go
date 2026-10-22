package model

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/usage"
)

// ScriptedStep is one deterministic model response.
type ScriptedStep struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Report    string          `json:"report"`
	Stage     Stage           `json:"stage"`
	Final     string          `json:"final"`
	Ask       string          `json:"ask"`
}

// Scripted replays fixed responses for deterministic tests and offline runs.
type Scripted struct {
	mu    sync.Mutex
	index int
	steps []ScriptedStep
	seen  []Message
	calls [][]Message
}

// CheckpointedScripted replays fixture steps from the durable model-call count.
// It is intended for long-lived runtimes that execute more than one run.
type CheckpointedScripted struct {
	*Scripted
}

// NewScripted creates a deterministic scripted model.
func NewScripted(steps []ScriptedStep) *Scripted {
	return &Scripted{steps: append([]ScriptedStep(nil), steps...)}
}

// NewCheckpointedScripted creates a deterministic model safe for run replay.
func NewCheckpointedScripted(steps []ScriptedStep) *CheckpointedScripted {
	return &CheckpointedScripted{Scripted: NewScripted(steps)}
}

// ScriptedFromJSON parses {"steps":[...]}.
func ScriptedFromJSON(raw []byte) (*CheckpointedScripted, error) {
	var decoded struct {
		Steps []ScriptedStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("parse scripted model: %w", err)
	}
	if len(decoded.Steps) == 0 {
		return nil, fmt.Errorf("scripted model needs at least one step")
	}
	return NewCheckpointedScripted(decoded.Steps), nil
}

// Label identifies this deterministic provider.
func (*Scripted) Label() string { return "scripted" }

// Restart replays the fixture from its beginning.
func (s *Scripted) Restart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index = 0
	s.seen = nil
	s.calls = nil
}

// Calls returns completed scripted turns.
func (s *Scripted) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index
}

// Seen returns all messages accepted by the fixture.
func (s *Scripted) Seen() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMessages(s.seen)
}

// CallsSeen returns request messages for every completion call.
func (s *Scripted) CallsSeen() [][]Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]Message, len(s.calls))
	for i := range s.calls {
		out[i] = cloneMessages(s.calls[i])
	}
	return out
}

// Complete returns the next scripted response. Scripted calls consume no billed usage.
func (s *Scripted) Complete(_ context.Context, request Request) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	captured := cloneMessages(request.Messages)
	s.seen = append(s.seen, captured...)
	s.calls = append(s.calls, captured)
	if s.index >= len(s.steps) {
		return Response{Message: Message{Role: RoleAssistant, Content: "script exhausted"}}, nil
	}
	index := s.index
	s.index++
	return scriptedResponse(s.steps[index], index)
}

// CompleteForRun selects a scripted response from the durable model-call count.
// This keeps the fixture rerunnable across independent runs and process restarts.
func (s *CheckpointedScripted) CompleteForRun(
	_ context.Context,
	_ string,
	modelCalls int,
	request Request,
) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if modelCalls < 0 {
		return Response{}, fmt.Errorf("scripted model call count must not be negative")
	}
	captured := cloneMessages(request.Messages)
	s.seen = append(s.seen, captured...)
	s.calls = append(s.calls, captured)
	if modelCalls >= len(s.steps) {
		return Response{Message: Message{Role: RoleAssistant, Content: "script exhausted"}}, nil
	}
	return scriptedResponse(s.steps[modelCalls], modelCalls)
}

func scriptedResponse(step ScriptedStep, index int) (Response, error) {
	if step.Report != "" {
		return Response{
			Message: Message{Role: RoleAssistant, Content: step.Report},
			Report:  step.Report,
			Stage:   step.Stage,
		}, nil
	}
	if step.Final != "" {
		return Response{Message: Message{Role: RoleAssistant, Content: step.Final}, Stage: step.Stage}, nil
	}
	name, arguments := step.Tool, step.Arguments
	if step.Ask != "" {
		name = "ask_user"
		encoded, err := json.Marshal(map[string]string{"question": step.Ask})
		if err != nil {
			return Response{}, err
		}
		arguments = encoded
	}
	if name == "" {
		return Response{}, fmt.Errorf("scripted step %d has neither tool nor final", index)
	}
	if len(arguments) == 0 {
		arguments = json.RawMessage("{}")
	}
	return Response{Message: Message{
		Role: RoleAssistant,
		ToolCalls: []ToolCall{{
			ID:        fmt.Sprintf("call_%d", index+1),
			Name:      name,
			Arguments: append(json.RawMessage(nil), arguments...),
		}},
	}, Stage: step.Stage, Usage: usage.Usage{}}, nil
}

func cloneMessages(messages []Message) []Message {
	out := make([]Message, len(messages))
	for i, message := range messages {
		out[i] = message
		out[i].ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
		for j := range out[i].ToolCalls {
			out[i].ToolCalls[j].Arguments = append(json.RawMessage(nil), message.ToolCalls[j].Arguments...)
		}
	}
	return out
}
