package v3

import (
	"errors"
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type createMessageRequest struct {
	Content        string         `json:"content"`
	AgentID        string         `json:"agent_id"`
	Model          event.ModelRef `json:"model"`
	IdempotencyKey string         `json:"idempotency_key"`
	RuntimeID      string         `json:"runtime_id,omitempty"`
	HarnessID      string         `json:"harness_id,omitempty"`
}

func (s *Server) handleCreateMessage(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	var input createMessageRequest
	if !decodeRequest(w, r, &input) {
		return
	}
	if err := requiredText(input.Content, "content"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := requiredText(input.IdempotencyKey, "idempotency_key"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	agentID, model := s.profile.ID, s.model
	if s.management && (input.RuntimeID == "" || input.HarnessID == "") {
		writeError(w, http.StatusBadRequest, "invalid_request", "runtime_id and harness_id are required")
		return
	}
	var selected *execution.Spec
	if input.RuntimeID != "" || input.HarnessID != "" {
		if input.RuntimeID == "" || input.HarnessID == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "runtime_id and harness_id are required together")
			return
		}
		var runtime *execution.Runtime
		for _, candidate := range s.runtimes {
			if candidate.ID == input.RuntimeID {
				copy := candidate
				runtime = &copy
				break
			}
		}
		if runtime == nil {
			writeError(w, http.StatusBadRequest, "unknown_runtime", "runtime_id is not available")
			return
		}
		if !runtime.Available {
			writeError(w, http.StatusBadRequest, "runtime_unavailable", "runtime is not configured or its executable is unavailable")
			return
		}
		model = event.ModelRef{
			Provider: runtime.Model.Provider, Name: runtime.Model.Name, ReasoningEffort: runtime.Model.ReasoningEffort,
		}
		if input.Model != (event.ModelRef{}) && input.Model != model {
			writeError(w, http.StatusBadRequest, "model_not_available", "model is not available for selected runtime")
			return
		}
		if !s.management {
			if !s.requireHarnesses(w) {
				return
			}
			h, err := s.harnesses.GetHarness(r.Context(), input.HarnessID)
			if errors.Is(err, postgres.ErrNotFound) {
				writeError(w, http.StatusBadRequest, "unknown_harness", "harness_id is not available")
				return
			}
			if err != nil {
				writeRepositoryError(w, err)
				return
			}
			selected = (&execution.Spec{RuntimeID: runtime.ID, Harness: h}).Clone()
			if err := selected.Validate(); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_harness", "selected Harness is invalid")
				return
			}
		}
		agentID = runtime.ID
	} else {
		if input.AgentID != s.profile.ID {
			writeError(w, http.StatusBadRequest, "unknown_agent", "agent_id is not available")
			return
		}
		if input.Model != s.model {
			writeError(w, http.StatusBadRequest, "model_not_available", "model is not available")
			return
		}
	}
	params := postgres.CreateMessageParams{
		ConversationID: r.PathValue("id"),
		Content:        input.Content,
		AgentID:        agentID,
		Model:          model,
		Execution:      selected,
		IdempotencyKey: input.IdempotencyKey,
	}
	if s.management {
		params.RuntimeID, params.HarnessVersionID = input.RuntimeID, input.HarnessID
	}
	message, run, err := s.repository.CreateMessageAndRun(r.Context(), params)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"message_id": message.ID,
		"run_id":     run.ID,
		"status":     string(run.Status),
	})
}
