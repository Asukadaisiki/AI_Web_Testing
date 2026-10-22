package v3

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type responseRequest struct {
	Kind           string `json:"kind"`
	Content        string `json:"content"`
	IdempotencyKey string `json:"idempotency_key"`
}

type publicRun struct {
	ID                string           `json:"id"`
	ProjectID         string           `json:"project_id"`
	ConversationID    string           `json:"conversation_id"`
	Status            domain.RunStatus `json:"status"`
	CheckpointVersion int64            `json:"checkpoint_version"`
	CancelRequestedAt *time.Time       `json:"cancel_requested_at,omitempty"`
	StartedAt         *time.Time       `json:"started_at,omitempty"`
	FinishedAt        *time.Time       `json:"finished_at,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

type publicTraceAttempt struct {
	ID            string          `json:"id"`
	SpanID        string          `json:"span_id"`
	AttemptNumber int             `json:"attempt_number"`
	Provider      string          `json:"provider,omitempty"`
	OperationID   string          `json:"operation_id,omitempty"`
	Status        string          `json:"status"`
	Error         json.RawMessage `json:"error,omitempty"`
	StartedAt     time.Time       `json:"started_at"`
	EndedAt       *time.Time      `json:"ended_at,omitempty"`
}

type publicTraceUsage struct {
	ID            string          `json:"id"`
	SpanID        string          `json:"span_id,omitempty"`
	SpanAttemptID string          `json:"span_attempt_id,omitempty"`
	Provider      string          `json:"provider,omitempty"`
	Model         string          `json:"model,omitempty"`
	InputTokens   int64           `json:"input_tokens"`
	OutputTokens  int64           `json:"output_tokens"`
	TotalTokens   int64           `json:"total_tokens"`
	CostMicros    int64           `json:"cost_micros"`
	Details       json.RawMessage `json:"details"`
	CreatedAt     time.Time       `json:"created_at"`
}

type publicOperationEvidence struct {
	ID            string          `json:"id"`
	SpanID        string          `json:"span_id,omitempty"`
	SpanAttemptID string          `json:"span_attempt_id,omitempty"`
	OperationID   string          `json:"operation_id"`
	Status        string          `json:"status"`
	Result        json.RawMessage `json:"result,omitempty"`
	Artifacts     json.RawMessage `json:"artifacts"`
	Error         json.RawMessage `json:"error,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// publicTraceSpan contains the trace node fields the workspace renders. JSON
// payloads are copied through redactTraceJSON before leaving the API boundary.
type publicTraceSpan struct {
	ID           string          `json:"id"`
	RunID        string          `json:"run_id"`
	TraceID      string          `json:"trace_id"`
	SpanID       string          `json:"span_id"`
	ParentSpanID string          `json:"parent_span_id,omitempty"`
	Kind         domain.SpanKind `json:"kind"`
	Name         string          `json:"name"`
	Status       string          `json:"status"`
	Input        json.RawMessage `json:"input,omitempty"`
	Output       json.RawMessage `json:"output,omitempty"`
	StartedAt    time.Time       `json:"started_at"`
	EndedAt      *time.Time      `json:"ended_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

var traceSecretFields = map[string]struct{}{
	"authorization": {},
	"cookie":        {},
	"password":      {},
	"token":         {},
	"api_key":       {},
	"secret":        {},
}

// redactTraceJSON keeps the public trace useful while omitting secret-bearing
// map entries at every nesting level. It always creates a new value, leaving
// the canonical PostgreSQL payload untouched.
func redactTraceJSON(raw json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil
	}
	encoded, err := json.Marshal(redactTraceValue(value))
	if err != nil {
		return nil
	}
	return encoded
}

func redactTraceValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if _, secret := traceSecretFields[strings.ToLower(strings.TrimSpace(key))]; secret {
				continue
			}
			result[key] = redactTraceValue(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = redactTraceValue(item)
		}
		return result
	default:
		return value
	}
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	run, err := s.repository.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicRun{
		ID:                run.ID,
		ProjectID:         run.ProjectID,
		ConversationID:    run.ConversationID,
		Status:            run.Status,
		CheckpointVersion: run.CheckpointVersion,
		CancelRequestedAt: run.CancelRequestedAt,
		StartedAt:         run.StartedAt,
		FinishedAt:        run.FinishedAt,
		CreatedAt:         run.CreatedAt,
		UpdatedAt:         run.UpdatedAt,
	})
}

func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	trace, err := s.repository.GetTraceDetails(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	spans := make([]publicTraceSpan, 0, len(trace.Spans))
	for _, span := range trace.Spans {
		spans = append(spans, publicTraceSpan{
			ID:           span.ID,
			RunID:        span.RunID,
			TraceID:      span.TraceID,
			SpanID:       span.SpanID,
			ParentSpanID: span.ParentSpanID,
			Kind:         span.Kind,
			Name:         span.Name,
			Status:       span.Status,
			Input:        redactTraceJSON(span.Input),
			Output:       redactTraceJSON(span.Output),
			StartedAt:    span.StartedAt,
			EndedAt:      span.EndedAt,
			CreatedAt:    span.CreatedAt,
			UpdatedAt:    span.UpdatedAt,
		})
	}
	attempts := make([]publicTraceAttempt, 0, len(trace.Attempts))
	for _, attempt := range trace.Attempts {
		attempts = append(attempts, publicTraceAttempt{
			ID:            attempt.ID,
			SpanID:        attempt.SpanID,
			AttemptNumber: attempt.AttemptNumber,
			Provider:      attempt.Provider,
			OperationID:   attempt.OperationID,
			Status:        attempt.Status,
			Error:         redactTraceJSON(attempt.Error),
			StartedAt:     attempt.StartedAt,
			EndedAt:       attempt.EndedAt,
		})
	}
	usage := make([]publicTraceUsage, 0, len(trace.Usage))
	for _, record := range trace.Usage {
		usage = append(usage, publicTraceUsage{
			ID:            record.ID,
			SpanID:        record.SpanID,
			SpanAttemptID: record.SpanAttemptID,
			Provider:      record.Provider,
			Model:         record.Model,
			InputTokens:   record.InputTokens,
			OutputTokens:  record.OutputTokens,
			TotalTokens:   record.TotalTokens,
			CostMicros:    record.CostMicros,
			Details:       redactTraceJSON(record.Details),
			CreatedAt:     record.CreatedAt,
		})
	}
	operations := make([]publicOperationEvidence, 0, len(trace.Operations))
	for _, operation := range trace.Operations {
		operations = append(operations, publicOperationEvidence{
			ID:            operation.ID,
			SpanID:        operation.SpanID,
			SpanAttemptID: operation.SpanAttemptID,
			OperationID:   operation.OperationID,
			Status:        operation.Status,
			Result:        redactTraceJSON(operation.Result),
			Artifacts:     redactTraceJSON(operation.Artifacts),
			Error:         redactTraceJSON(operation.Error),
			CreatedAt:     operation.CreatedAt,
			UpdatedAt:     operation.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"spans":              spans,
		"attempts":           attempts,
		"usage":              usage,
		"operation_evidence": operations,
	})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	var input struct{}
	if !decodeRequest(w, r, &input) {
		return
	}
	runID := r.PathValue("id")
	if err := s.repository.RequestCancel(r.Context(), runID); err != nil {
		writeRepositoryError(w, err)
		return
	}
	if s.live != nil {
		_ = s.live.RequestCancel(r.Context(), runID, 0)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"run_id": runID, "status": "cancelling"})
}

func (s *Server) handleResponse(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	var input responseRequest
	if !decodeRequest(w, r, &input) {
		return
	}
	for field, value := range map[string]string{
		"kind": input.Kind, "content": input.Content, "idempotency_key": input.IdempotencyKey,
	} {
		if err := requiredText(value, field); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	if input.Kind != "question" && input.Kind != "approval" {
		writeError(w, http.StatusBadRequest, "invalid_request", "kind must be question or approval")
		return
	}
	runID := r.PathValue("id")
	if err := s.repository.SubmitResponse(r.Context(), postgres.SubmitResponseParams{
		RunID:          runID,
		Kind:           input.Kind,
		Content:        input.Content,
		IdempotencyKey: input.IdempotencyKey,
		Continuation:   true,
	}); err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"run_id": runID, "status": "queued"})
}

func (s *Server) handleAgents(w http.ResponseWriter, _ *http.Request) {
	type publicModel struct {
		Provider        string `json:"provider"`
		Name            string `json:"name"`
		ReasoningEffort string `json:"reasoning_effort"`
	}
	type publicAgent struct {
		ID          string      `json:"id"`
		ProductName string      `json:"product_name"`
		Model       publicModel `json:"model"`
		Safety      any         `json:"safety"`
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": []publicAgent{{
		ID:          s.profile.ID,
		ProductName: s.profile.ProductName,
		Model: publicModel{
			Provider: s.model.Provider, Name: s.model.Name,
			ReasoningEffort: s.profile.Model.ReasoningEffort,
		},
		Safety: s.profile.Safety,
	}}})
}
