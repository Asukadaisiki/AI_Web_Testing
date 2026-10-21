package agentruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/model"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/planner"
)

func TestRuntimeModelAdapterPreservesPlannerTools(t *testing.T) {
	provider := &recordingModel{}
	runtime := New(Config{Model: provider})

	if _, _, err := runtime.llm.Next(context.Background(), []Message{{Role: RoleUser, Content: "inspect"}}); err != nil {
		t.Fatalf("complete through adapter: %v", err)
	}
	tools := planner.Tools()
	if len(provider.request.Tools) != len(tools) {
		t.Fatalf("adapter tools = %d, want %d", len(provider.request.Tools), len(tools))
	}
	if got, want := provider.request.Tools[0].Name, tools[0].Name; got != want {
		t.Fatalf("first adapter tool = %q, want %q", got, want)
	}
	if got, want := provider.request.Tools[0].Description, tools[0].Description; got != want {
		t.Fatalf("first adapter tool description = %q, want %q", got, want)
	}
	if provider.request.ToolChoice != "auto" {
		t.Fatalf("adapter tool choice = %q, want auto", provider.request.ToolChoice)
	}
}

func TestRuntimeModelAdapterEnforcesRequestSize(t *testing.T) {
	provider := &recordingModel{requestSize: 2}
	runtime := New(Config{Model: provider, MaxRequestBytes: 1})

	err := runtime.enforceRequestSize([]Message{{Role: RoleUser, Content: "inspect"}})
	if err == nil {
		t.Fatal("request over the configured limit must fail before completion")
	}
	if !strings.Contains(err.Error(), "模型请求超过大小上限") {
		t.Fatalf("request-size error = %v", err)
	}
	if len(provider.sized.Tools) == 0 {
		t.Fatal("sized request omitted planner tools")
	}
	if provider.sized.ToolChoice != "auto" {
		t.Fatalf("sized tool choice = %q, want auto", provider.sized.ToolChoice)
	}
}

type recordingModel struct {
	request     model.Request
	sized       model.Request
	requestSize int
}

func (*recordingModel) Label() string { return "recording-model" }

func (m *recordingModel) Complete(_ context.Context, request model.Request) (model.Response, error) {
	m.request = request
	return model.Response{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}, nil
}

func (m *recordingModel) RequestSize(request model.Request) (int, error) {
	m.sized = request
	return m.requestSize, nil
}
