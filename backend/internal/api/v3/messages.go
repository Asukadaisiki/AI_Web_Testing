package v3

import (
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type createMessageRequest struct {
	Content        string         `json:"content"`
	Model          event.ModelRef `json:"model"`
	IdempotencyKey string         `json:"idempotency_key"`
	RuntimeID      string         `json:"runtime_id"`
	HarnessID      string         `json:"harness_id"`
}

func (s *Server) handleCreateMessage(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	var input createMessageRequest
	if !decodeRequest(w, r, &input) {
		return
	}
	for _, field := range []struct{ value, name string }{
		{input.Content, "content"}, {input.IdempotencyKey, "idempotency_key"},
		{input.RuntimeID, "runtime_id"}, {input.HarnessID, "harness_id"},
	} {
		if err := requiredText(field.value, field.name); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	var selected *execution.Runtime
	for _, candidate := range s.runtimes {
		if candidate.ID == input.RuntimeID {
			selected = &candidate
			break
		}
	}
	if selected == nil {
		writeError(w, http.StatusBadRequest, "unknown_runtime", "runtime_id is not available")
		return
	}
	if !selected.Available {
		writeError(w, http.StatusBadRequest, "runtime_unavailable", "runtime is not configured or its executable is unavailable")
		return
	}
	model := event.ModelRef{
		Provider: selected.Model.Provider, Name: selected.Model.Name, ReasoningEffort: selected.Model.ReasoningEffort,
	}
	if input.Model != (event.ModelRef{}) && input.Model != model {
		writeError(w, http.StatusBadRequest, "model_not_available", "model is not available for selected runtime")
		return
	}
	message, run, err := s.repository.CreateMessageAndRun(r.Context(), postgres.CreateMessageParams{
		ConversationID: r.PathValue("id"), Content: input.Content, Model: model,
		IdempotencyKey: input.IdempotencyKey, RuntimeID: input.RuntimeID, HarnessVersionID: input.HarnessID,
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"message_id": message.ID, "run_id": run.ID, "status": string(run.Status),
	})
}
