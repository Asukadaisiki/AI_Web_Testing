package v3

import (
	"context"
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type EnvironmentRepository interface {
	GetEnvironment(context.Context, string) (postgres.Environment, error)
	GetConversationEnvironment(context.Context, string) (postgres.Environment, error)
	ResetEnvironment(context.Context, string, string) (postgres.Environment, error)
	DeleteEnvironment(context.Context, string, string) error
}

type ConversationManagementRepository interface {
	ArchiveConversation(context.Context, string, string) (postgres.Conversation, error)
	DeleteConversationAs(context.Context, string, string) error
}

type EnvironmentReviewRepository interface {
	ReviewEnvironment(context.Context, string, string) (postgres.Environment, error)
}

func (s *Server) registerEnvironmentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects/{id}/binding", s.handleProjectBinding)
	mux.HandleFunc("PATCH /api/v1/conversations/{id}", s.handleArchiveConversation)
	mux.HandleFunc("GET /api/v1/conversations/{id}/runs", s.handleConversationRuns)
	mux.HandleFunc("POST /api/v1/conversations/{id}/runs", s.handleCreateMessage)
	mux.HandleFunc("GET /api/v1/conversations/{id}/environment", s.handleConversationEnvironment)
	mux.HandleFunc("GET /api/v1/environments/{id}", s.handleGetEnvironment)
	mux.HandleFunc("POST /api/v1/environments/{id}/reset", s.handleResetEnvironment)
	mux.HandleFunc("POST /api/v1/environments/{id}/review", s.handleReviewEnvironment)
	mux.HandleFunc("DELETE /api/v1/environments/{id}", s.handleDeleteEnvironment)
}

func (s *Server) handleReviewEnvironment(w http.ResponseWriter, r *http.Request) {
	environment, ok := s.requireEnvironmentOwner(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	identity, _ := BridgeIdentityFromContext(r.Context())
	if identity.Role == "viewer" {
		writeBridgeForbidden(w)
		return
	}
	var input struct{}
	if !decodeRequest(w, r, &input) {
		return
	}
	repository, ok := s.repository.(EnvironmentReviewRepository)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "management_unavailable", "environment review is unavailable")
		return
	}
	updated, err := repository.ReviewEnvironment(r.Context(), environment.ID, identity.ActorID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicEnvironment(updated))
}

func (s *Server) handleProjectBinding(w http.ResponseWriter, r *http.Request) {
	identity, ok := BridgeIdentityFromContext(r.Context())
	if !ok || identity.ProjectID != r.PathValue("id") {
		writeBridgeForbidden(w)
		return
	}
	repository, ok := s.repository.(ProjectBindingRepository)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "management_unavailable", "project bindings are unavailable")
		return
	}
	binding, err := repository.GetProjectBinding(r.Context(), identity.ProjectID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if binding.AgenttraceProjectID != identity.ProjectID || binding.LangfuseProjectID != identity.LangfuseProjectID || binding.Status != "active" {
		writeBridgeForbidden(w)
		return
	}
	writeJSON(w, http.StatusOK, binding)
}

func (s *Server) requireEnvironmentRepository(w http.ResponseWriter) (EnvironmentRepository, bool) {
	repository, ok := s.repository.(EnvironmentRepository)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "management_unavailable", "environment management is unavailable")
	}
	return repository, ok
}

func (s *Server) requireEnvironmentOwner(w http.ResponseWriter, r *http.Request, id string) (postgres.Environment, bool) {
	identity, ok := BridgeIdentityFromContext(r.Context())
	if !ok {
		writeBridgeForbidden(w)
		return postgres.Environment{}, false
	}
	repository, ok := s.requireEnvironmentRepository(w)
	if !ok {
		return postgres.Environment{}, false
	}
	environment, err := repository.GetEnvironment(r.Context(), id)
	if err != nil {
		writeRepositoryError(w, err)
		return postgres.Environment{}, false
	}
	if environment.ID != id || environment.ProjectID != identity.ProjectID || environment.ConversationID == "" {
		writeBridgeForbidden(w)
		return postgres.Environment{}, false
	}
	if !s.requireConversationOwner(w, r, environment.ConversationID) {
		return postgres.Environment{}, false
	}
	return environment, true
}

// Internal managed profile references are not a browser-visible API field.
func publicEnvironment(environment postgres.Environment) postgres.Environment {
	environment.ProfileRef = ""
	return environment
}

func (s *Server) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	environment, ok := s.requireEnvironmentOwner(w, r, r.PathValue("id"))
	if ok {
		writeJSON(w, http.StatusOK, publicEnvironment(environment))
	}
}

func (s *Server) handleConversationEnvironment(w http.ResponseWriter, r *http.Request) {
	identity, trusted := BridgeIdentityFromContext(r.Context())
	if !trusted || !s.requireConversationOwner(w, r, r.PathValue("id")) {
		if !trusted {
			writeBridgeForbidden(w)
		}
		return
	}
	repository, ok := s.requireEnvironmentRepository(w)
	if !ok {
		return
	}
	environment, err := repository.GetConversationEnvironment(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if environment.ConversationID != r.PathValue("id") || environment.ProjectID != identity.ProjectID {
		writeBridgeForbidden(w)
		return
	}
	writeJSON(w, http.StatusOK, publicEnvironment(environment))
}

func (s *Server) handleResetEnvironment(w http.ResponseWriter, r *http.Request) {
	environment, ok := s.requireEnvironmentOwner(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	identity, _ := BridgeIdentityFromContext(r.Context())
	if identity.Role == "viewer" {
		writeBridgeForbidden(w)
		return
	}
	repository, _ := s.requireEnvironmentRepository(w)
	updated, err := repository.ResetEnvironment(r.Context(), environment.ID, identity.ActorID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, publicEnvironment(updated))
}

func (s *Server) handleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	environment, ok := s.requireEnvironmentOwner(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	identity, _ := BridgeIdentityFromContext(r.Context())
	if identity.Role == "viewer" {
		writeBridgeForbidden(w)
		return
	}
	repository, _ := s.requireEnvironmentRepository(w)
	if err := repository.DeleteEnvironment(r.Context(), environment.ID, identity.ActorID); err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"conversation_id": environment.ConversationID, "state": "deleting"})
}
