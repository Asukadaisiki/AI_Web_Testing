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
	if identity, ok := BridgeIdentityFromContext(r.Context()); !ok || identity.ProjectID != r.PathValue("id") {
		writeBridgeForbidden(w)
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
	identity, trusted := BridgeIdentityFromContext(r.Context())
	if !trusted || identity.ProjectID != r.PathValue("id") || identity.Role == "viewer" {
		writeBridgeForbidden(w)
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
		CreatedBy: identity.ActorID,
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
	if !s.requireConversationOwner(w, r, r.PathValue("id")) {
		return
	}
	conversation, messages, runs, err := s.repository.GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if messages == nil {
		messages = []domain.Message{}
	}
	publicRuns := make([]publicRun, 0, len(runs))
	for _, run := range runs {
		publicRuns = append(publicRuns, toPublicRun(run))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation": conversation,
		"messages":     messages,
		"runs":         publicRuns,
	})
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	if !s.requireConversationOwner(w, r, r.PathValue("id")) {
		return
	}
	if identity, trusted := BridgeIdentityFromContext(r.Context()); trusted {
		if identity.Role == "viewer" {
			writeBridgeForbidden(w)
			return
		}
		repository, ok := s.repository.(ConversationManagementRepository)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "management_unavailable", "conversation management is unavailable")
			return
		}
		if err := repository.DeleteConversationAs(r.Context(), r.PathValue("id"), identity.ActorID); err != nil {
			writeRepositoryError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"conversation_id": r.PathValue("id"), "state": "deleting"})
		return
	}
	writeBridgeForbidden(w)
}

func (s *Server) requireConversationOwner(w http.ResponseWriter, r *http.Request, id string) bool {
	identity, trusted := BridgeIdentityFromContext(r.Context())
	if !trusted {
		writeBridgeForbidden(w)
		return false
	}
	if !s.requireRepository(w) {
		return false
	}
	conversation, _, _, err := s.repository.GetConversation(r.Context(), id)
	if err != nil {
		writeRepositoryError(w, err)
		return false
	}
	if conversation.ID != id || conversation.ProjectID != identity.ProjectID {
		writeBridgeForbidden(w)
		return false
	}
	return true
}

func (s *Server) handleArchiveConversation(w http.ResponseWriter, r *http.Request) {
	identity, trusted := BridgeIdentityFromContext(r.Context())
	if !trusted || identity.Role == "viewer" {
		writeBridgeForbidden(w)
		return
	}
	if !s.requireConversationOwner(w, r, r.PathValue("id")) {
		return
	}
	repository, ok := s.repository.(ConversationManagementRepository)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "management_unavailable", "conversation management is unavailable")
		return
	}
	var input struct {
		State string `json:"state"`
	}
	if !decodeRequest(w, r, &input) {
		return
	}
	if input.State != "archived" {
		writeError(w, http.StatusBadRequest, "invalid_request", "only archived is supported")
		return
	}
	conversation, err := repository.ArchiveConversation(r.Context(), r.PathValue("id"), identity.ActorID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}
