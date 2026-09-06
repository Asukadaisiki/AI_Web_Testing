package agentservice

import (
	"context"
	"encoding/json"
	"maps"
	"sync"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
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

	current, ok := r.runs[run.ID]
	if !ok {
		return ErrRunNotFound
	}
	if current.Status == RunStatusCancelled {
		return ErrRunCancelled
	}
	r.runs[run.ID] = cloneRun(run)
	return nil
}

func (r *MemoryRepository) CancelRun(
	_ context.Context,
	runID string,
	updatedAt time.Time,
	event Event,
) (AgentRun, Event, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	run, ok := r.runs[runID]
	if !ok {
		return AgentRun{}, Event{}, false, ErrRunNotFound
	}
	if run.Status != RunStatusRunning && run.Status != RunStatusWaitingUser {
		return cloneRun(run), Event{}, false, nil
	}
	run.Status = RunStatusCancelled
	run.PendingToolCallID = nil
	run.PendingStepID = nil
	run.UpdatedAt = updatedAt
	r.runs[runID] = cloneRun(run)
	event.RunID = run.ID
	event.ConversationID = run.ConversationID
	event.Seq = int64(len(r.events[runID]) + 1)
	event = normalizeEvent(event)
	r.events[runID] = append(r.events[runID], event)
	return cloneRun(run), event, true, nil
}

func (r *MemoryRepository) AppendEvent(_ context.Context, event Event) (Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	run, ok := r.runs[event.RunID]
	if !ok {
		return Event{}, ErrRunNotFound
	}
	if run.Status == RunStatusCancelled {
		return Event{}, ErrRunCancelled
	}
	event.Seq = int64(len(r.events[event.RunID]) + 1)
	event = normalizeEvent(event)
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
	if run.PendingStepID != nil {
		pendingStepID := *run.PendingStepID
		run.PendingStepID = &pendingStepID
	}
	if run.LatestGenerationID != nil {
		latestGenerationID := *run.LatestGenerationID
		run.LatestGenerationID = &latestGenerationID
	}
	if run.ApprovedGenerationID != nil {
		approvedGenerationID := *run.ApprovedGenerationID
		run.ApprovedGenerationID = &approvedGenerationID
	}
	run.Transcript = append([]agent.Message(nil), run.Transcript...)
	for index := range run.Transcript {
		run.Transcript[index].ToolCalls = append([]agent.ModelTool(nil), run.Transcript[index].ToolCalls...)
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

func normalizeEvent(event Event) Event {
	encoded, err := json.Marshal(event.Payload)
	if err != nil {
		return event
	}
	var payload map[string]any
	if json.Unmarshal(encoded, &payload) == nil {
		event.Payload = payload
	}
	event.Timestamp = event.Timestamp.UTC()
	return event
}
