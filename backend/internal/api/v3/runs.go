package v3

import (
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type responseRequest struct {
	Kind           string `json:"kind"`
	Content        string `json:"content"`
	IdempotencyKey string `json:"idempotency_key"`
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
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	spans, err := s.repository.ListTrace(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spans": spans})
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
			Provider: s.profile.Model.Provider, Name: s.profile.Model.Name,
			ReasoningEffort: s.profile.Model.ReasoningEffort,
		},
		Safety: s.profile.Safety,
	}}})
}
