package v3

import (
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type createMessageRequest struct {
	Content        string         `json:"content"`
	AgentID        string         `json:"agent_id"`
	Model          event.ModelRef `json:"model"`
	IdempotencyKey string         `json:"idempotency_key"`
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
	if input.AgentID != s.profile.ID {
		writeError(w, http.StatusBadRequest, "unknown_agent", "agent_id is not available")
		return
	}
	if err := requiredText(input.Model.Provider, "model.provider"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := requiredText(input.Model.Name, "model.name"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := requiredText(input.IdempotencyKey, "idempotency_key"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	message, run, err := s.repository.CreateMessageAndRun(r.Context(), postgres.CreateMessageParams{
		ConversationID: r.PathValue("id"),
		Content:        input.Content,
		AgentID:        s.profile.ID,
		Model:          input.Model,
		Operations:     s.profile.RunOperations(),
		IdempotencyKey: input.IdempotencyKey,
	})
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
