package v3

import (
	"encoding/json"
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type createConversationRequest struct {
	Title    string          `json:"title"`
	Metadata json.RawMessage `json:"metadata"`
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	conversations, err := s.repository.ListConversations(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if conversations == nil {
		conversations = []postgres.Conversation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": conversations})
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	var input createConversationRequest
	if !decodeRequest(w, r, &input) {
		return
	}
	conversation, err := s.repository.CreateConversation(r.Context(), postgres.CreateConversationParams{
		ProjectID: r.PathValue("id"),
		Title:     input.Title,
		Metadata:  input.Metadata,
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, conversation)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	conversation, messages, err := s.repository.GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if messages == nil {
		messages = []domain.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation": conversation,
		"messages":     messages,
	})
}
