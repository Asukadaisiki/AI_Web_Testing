package v3

import (
	"encoding/json"
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type createProjectRequest struct {
	Name     string          `json:"name"`
	Metadata json.RawMessage `json:"metadata"`
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	projects, err := s.repository.ListProjects(r.Context())
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if projects == nil {
		projects = []postgres.Project{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireRepository(w) {
		return
	}
	var input createProjectRequest
	if !decodeRequest(w, r, &input) {
		return
	}
	if err := requiredText(input.Name, "name"); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	project, err := s.repository.CreateProject(r.Context(), postgres.CreateProjectParams{
		Name: input.Name, Metadata: input.Metadata,
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}
