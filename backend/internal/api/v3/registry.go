package v3

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/registry"
)

type RegistryRepository interface {
	UploadSkill(context.Context, string, string, string, []registry.File) (registry.SkillVersion, error)
	ListSkills(context.Context, string) ([]registry.SkillVersion, error)
	PublishSkill(context.Context, string, string, string) (registry.SkillVersion, error)
	RevokeSkill(context.Context, string, string, string) error
	PublishMCP(context.Context, registry.MCPVersion, string) (registry.MCPVersion, error)
	ListMCPVersions(context.Context) ([]registry.MCPVersion, error)
	RevokeMCP(context.Context, string, string) error
	SaveMCPBinding(context.Context, string, string, string, registry.Binding) (registry.Binding, error)
	ListMCPBindings(context.Context, string) ([]registry.Binding, error)
	PublishProjectHarness(context.Context, string, string, registry.HarnessVersion, string) (registry.HarnessVersion, error)
	ListProjectHarnesses(context.Context, string) ([]registry.HarnessVersion, error)
	ResolveProjectHarness(context.Context, string, string, string) (execution.HarnessSnapshot, error)
	RevokeProjectHarness(context.Context, string, string, string) error
}

func (s *Server) registerRegistryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/projects/{id}/skills", s.handleSkills)
	mux.HandleFunc("POST /api/v1/projects/{id}/skills/uploads", s.handleSkillUpload)
	mux.HandleFunc("POST /api/v1/projects/{id}/skills/{skillId}/versions", s.handleSkillPublish)
	mux.HandleFunc("POST /api/v1/projects/{id}/skill-versions/{versionId}/publish", s.handleSkillPublish)
	mux.HandleFunc("POST /api/v1/projects/{id}/skill-versions/{versionId}/revoke", s.handleSkillRevoke)
	mux.HandleFunc("GET /api/v1/mcp-definitions", s.handleMCPVersions)
	mux.HandleFunc("POST /api/v1/mcp-definitions", s.handleMCPPublish)
	mux.HandleFunc("POST /api/v1/mcp-versions/{versionId}/revoke", s.handleMCPRevoke)
	mux.HandleFunc("GET /api/v1/projects/{id}/mcp-bindings", s.handleMCPBindings)
	mux.HandleFunc("POST /api/v1/projects/{id}/mcp-bindings", s.handleMCPSaveBinding)
	mux.HandleFunc("PATCH /api/v1/projects/{id}/mcp-bindings/{bindingId}", s.handleMCPSaveBinding)
	mux.HandleFunc("GET /api/v1/projects/{id}/harnesses", s.handleProjectHarnesses)
	mux.HandleFunc("POST /api/v1/projects/{id}/harnesses", s.handleProjectHarnessPublish)
	mux.HandleFunc("POST /api/v1/projects/{id}/harnesses/{harnessId}/versions", s.handleProjectHarnessPublish)
	mux.HandleFunc("POST /api/v1/projects/{id}/harness-versions/{versionId}/revoke", s.handleProjectHarnessRevoke)
}

// Headers are accepted only after service authentication and project binding in bridgeGuard.
func registryAdmin(w http.ResponseWriter, r *http.Request, system bool) bool {
	role := r.Header.Get("X-Agenttrace-Role")
	if strings.TrimSpace(r.Header.Get("X-Agenttrace-Actor-Id")) == "" || (role != "system_admin" && (system || (role != "admin" && role != "project_admin"))) {
		writeError(w, http.StatusForbidden, "admin_required", "administrator permission is required")
		return false
	}
	return true
}

func (s *Server) registryRepository(w http.ResponseWriter) RegistryRepository {
	repo, ok := s.repository.(RegistryRepository)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "registry_unavailable", "Registry is unavailable")
		return nil
	}
	return repo
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	versions, err := repo.ListSkills(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": versions})
}

func (s *Server) handleSkillUpload(w http.ResponseWriter, r *http.Request) {
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	if r.Header.Get("X-Agenttrace-Actor-Id") == "" {
		writeBridgeForbidden(w)
		return
	}
	var input struct {
		Name          string          `json:"name"`
		Files         []registry.File `json:"files"`
		ArchiveBase64 string          `json:"archive_base64,omitempty"`
	}
	if !decodeRequest(w, r, &input) {
		return
	}
	if input.ArchiveBase64 != "" {
		if len(input.Files) > 0 {
			writeError(w, 400, "invalid_skill", "choose files or archive")
			return
		}
		archive, err := base64.StdEncoding.DecodeString(input.ArchiveBase64)
		if err != nil {
			writeError(w, 400, "invalid_skill", "invalid ZIP encoding")
			return
		}
		preview, err := registry.ParseSkillArchive(input.Name, archive)
		if err != nil {
			writeError(w, 400, "invalid_skill", err.Error())
			return
		}
		input.Files = preview.Files
	}
	if _, err := registry.ValidateSkill(input.Name, input.Files); err != nil {
		writeError(w, 400, "invalid_skill", err.Error())
		return
	}
	version, err := repo.UploadSkill(r.Context(), r.PathValue("id"), r.Header.Get("X-Agenttrace-Actor-Id"), input.Name, input.Files)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, version)
}

func (s *Server) handleSkillPublish(w http.ResponseWriter, r *http.Request) {
	if !registryAdmin(w, r, false) {
		return
	}
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	id := r.PathValue("versionId")
	if id == "" {
		var input struct {
			VersionID string `json:"version_id"`
		}
		if !decodeRequest(w, r, &input) {
			return
		}
		id = input.VersionID
		// Verify the version belongs to the skill named in the URL.
		versions, err := repo.ListSkills(r.Context(), r.PathValue("id"))
		if err != nil {
			writeRepositoryError(w, err)
			return
		}
		matched := false
		for _, v := range versions {
			matched = matched || (v.ID == id && v.SkillID == r.PathValue("skillId"))
		}
		if !matched {
			writeError(w, 404, "not_found", "Skill version was not found")
			return
		}
	}
	v, err := repo.PublishSkill(r.Context(), r.PathValue("id"), id, r.Header.Get("X-Agenttrace-Actor-Id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) handleSkillRevoke(w http.ResponseWriter, r *http.Request) {
	if !registryAdmin(w, r, false) {
		return
	}
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	if err := repo.RevokeSkill(r.Context(), r.PathValue("id"), r.PathValue("versionId"), r.Header.Get("X-Agenttrace-Actor-Id")); err != nil {
		writeRepositoryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMCPVersions(w http.ResponseWriter, r *http.Request) {
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	versions, err := repo.ListMCPVersions(r.Context())
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"mcp_versions": versions})
}
func (s *Server) handleMCPPublish(w http.ResponseWriter, r *http.Request) {
	if !registryAdmin(w, r, true) {
		return
	}
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	var input registry.MCPVersion
	if !decodeRequest(w, r, &input) {
		return
	}
	if err := registry.ValidateMCP(input); err != nil {
		writeError(w, 400, "invalid_mcp", err.Error())
		return
	}
	v, err := repo.PublishMCP(r.Context(), input, r.Header.Get("X-Agenttrace-Actor-Id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 201, v)
}
func (s *Server) handleMCPRevoke(w http.ResponseWriter, r *http.Request) {
	if !registryAdmin(w, r, true) {
		return
	}
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	if err := repo.RevokeMCP(r.Context(), r.PathValue("versionId"), r.Header.Get("X-Agenttrace-Actor-Id")); err != nil {
		writeRepositoryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleMCPBindings(w http.ResponseWriter, r *http.Request) {
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	bindings, err := repo.ListMCPBindings(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"mcp_bindings": bindings})
}
func (s *Server) handleMCPSaveBinding(w http.ResponseWriter, r *http.Request) {
	if !registryAdmin(w, r, false) {
		return
	}
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	var input registry.Binding
	if !decodeRequest(w, r, &input) {
		return
	}
	if input.ProjectID != "" && input.ProjectID != r.PathValue("id") {
		writeBridgeForbidden(w)
		return
	}
	versions, err := repo.ListMCPVersions(r.Context())
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	var selected *registry.MCPVersion
	for _, v := range versions {
		if v.ID == input.MCPVersionID {
			copy := v
			selected = &copy
		}
	}
	if selected == nil {
		writeError(w, 400, "invalid_mcp", "MCP version is unavailable")
		return
	}
	if err := registry.ValidateBinding(r.PathValue("id"), *selected, input.Config, input.SecretRefs); err != nil {
		writeError(w, 400, "invalid_mcp", err.Error())
		return
	}
	output, err := repo.SaveMCPBinding(r.Context(), r.PathValue("id"), r.PathValue("bindingId"), r.Header.Get("X-Agenttrace-Actor-Id"), input)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 200, output)
}
func (s *Server) handleProjectHarnesses(w http.ResponseWriter, r *http.Request) {
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	versions, err := repo.ListProjectHarnesses(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"harnesses": versions})
}
func (s *Server) handleProjectHarnessPublish(w http.ResponseWriter, r *http.Request) {
	if !registryAdmin(w, r, false) {
		return
	}
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	var input registry.HarnessVersion
	if !decodeRequest(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) == "" || len(input.RuntimeIDs) == 0 {
		writeError(w, 400, "invalid_harness", "name and runtime_ids are required")
		return
	}
	if input.ProjectID != "" && input.ProjectID != r.PathValue("id") {
		writeBridgeForbidden(w)
		return
	}
	v, err := repo.PublishProjectHarness(r.Context(), r.PathValue("id"), r.PathValue("harnessId"), input, r.Header.Get("X-Agenttrace-Actor-Id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 201, v)
}
func (s *Server) handleProjectHarnessRevoke(w http.ResponseWriter, r *http.Request) {
	if !registryAdmin(w, r, false) {
		return
	}
	repo := s.registryRepository(w)
	if repo == nil {
		return
	}
	if err := repo.RevokeProjectHarness(r.Context(), r.PathValue("id"), r.PathValue("versionId"), r.Header.Get("X-Agenttrace-Actor-Id")); err != nil {
		writeRepositoryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
