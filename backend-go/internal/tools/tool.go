package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var ErrToolNotFound = errors.New("tool not found")

type Definition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type Call struct {
	RunID      string          `json:"run_id"`
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
}

type Result struct {
	Content  json.RawMessage `json:"content,omitempty"`
	Artifact *Artifact       `json:"artifact,omitempty"`
}

type Artifact struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type Handler interface {
	Definition() Definition
	Execute(ctx context.Context, call Call) (Result, error)
}

type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

func NewRegistry(handlers ...Handler) (*Registry, error) {
	registry := &Registry{handlers: make(map[string]Handler, len(handlers))}
	for _, handler := range handlers {
		if err := registry.Register(handler); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(handler Handler) error {
	if handler == nil {
		return errors.New("tool handler is required")
	}
	definition := handler.Definition()
	if definition.Name == "" {
		return errors.New("tool name is required")
	}
	if len(definition.InputSchema) == 0 || !json.Valid(definition.InputSchema) {
		return fmt.Errorf("tool %q has invalid input schema", definition.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[definition.Name]; exists {
		return fmt.Errorf("tool %q is already registered", definition.Name)
	}
	r.handlers[definition.Name] = handler
	return nil
}

func (r *Registry) Get(name string) (Handler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, ok := r.handlers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return handler, nil
}

func (r *Registry) Definitions() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]Definition, 0, len(r.handlers))
	for _, handler := range r.handlers {
		definitions = append(definitions, handler.Definition())
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})
	return definitions
}
