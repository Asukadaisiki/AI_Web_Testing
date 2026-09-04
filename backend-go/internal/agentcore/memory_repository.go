package agentcore

import (
	"context"
	"maps"
	"sync"
)

type MemoryRepository struct {
	mu     sync.RWMutex
	runs   map[string]AgentRun
	events map[string][]Event
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		runs:   make(map[string]AgentRun),
		events: make(map[string][]Event),
	}
}

func (r *MemoryRepository) CreateRun(_ context.Context, run AgentRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.runs[run.ID] = cloneRun(run)
	return nil
}

func (r *MemoryRepository) GetRun(_ context.Context, runID string) (AgentRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	run, ok := r.runs[runID]
	if !ok {
		return AgentRun{}, ErrRunNotFound
	}
	return cloneRun(run), nil
}

func (r *MemoryRepository) SaveRun(_ context.Context, run AgentRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.runs[run.ID]; !ok {
		return ErrRunNotFound
	}
	r.runs[run.ID] = cloneRun(run)
	return nil
}

func (r *MemoryRepository) AppendEvent(_ context.Context, event Event) (Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.runs[event.RunID]; !ok {
		return Event{}, ErrRunNotFound
	}
	event.Seq = int64(len(r.events[event.RunID]) + 1)
	event.Payload = cloneMap(event.Payload)
	r.events[event.RunID] = append(r.events[event.RunID], event)
	return event, nil
}

func (r *MemoryRepository) ListEvents(_ context.Context, runID string, afterSeq int64) ([]Event, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, ok := r.runs[runID]; !ok {
		return nil, ErrRunNotFound
	}
	source := r.events[runID]
	result := make([]Event, 0, len(source))
	for _, event := range source {
		if event.Seq <= afterSeq {
			continue
		}
		event.Payload = cloneMap(event.Payload)
		result = append(result, event)
	}
	return result, nil
}

func cloneRun(run AgentRun) AgentRun {
	if run.PendingToolCallID != nil {
		pendingID := *run.PendingToolCallID
		run.PendingToolCallID = &pendingID
	}
	return run
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(source))
	maps.Copy(result, source)
	return result
}
