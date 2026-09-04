package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeTool struct {
	definition Definition
}

func (f fakeTool) Definition() Definition {
	return f.definition
}

func (f fakeTool) Execute(_ context.Context, _ Call) (Result, error) {
	return Result{Content: json.RawMessage(`{"ok":true}`)}, nil
}

func TestRegistryReturnsSortedDefinitions(t *testing.T) {
	registry, err := NewRegistry(
		fakeTool{definition: Definition{Name: "zeta", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		fakeTool{definition: Definition{Name: "alpha", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	definitions := registry.Definitions()
	if len(definitions) != 2 {
		t.Fatalf("len(definitions) = %d, want 2", len(definitions))
	}
	if definitions[0].Name != "alpha" || definitions[1].Name != "zeta" {
		t.Fatalf("definitions are not sorted: %#v", definitions)
	}
}

func TestRegistryRejectsDuplicateTools(t *testing.T) {
	tool := fakeTool{definition: Definition{Name: "duplicate", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	_, err := NewRegistry(tool, tool)
	if err == nil {
		t.Fatal("NewRegistry() error = nil, want duplicate error")
	}
}

func TestRegistryReturnsTypedNotFoundError(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	_, err = registry.Get("missing")
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("Get() error = %v, want ErrToolNotFound", err)
	}
}
