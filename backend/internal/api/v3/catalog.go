package v3

import (
	"context"
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

// HarnessRepository is optional for legacy repository implementations.
type HarnessRepository interface {
	ListHarnesses(context.Context) ([]execution.HarnessSnapshot, error)
	GetHarness(context.Context, string) (execution.HarnessSnapshot, error)
	CreateHarness(context.Context, execution.HarnessSnapshot) (execution.HarnessSnapshot, error)
	UpdateHarness(context.Context, string, execution.HarnessSnapshot) (execution.HarnessSnapshot, error)
	DeleteHarness(context.Context, string) (execution.HarnessSnapshot, error)
}

func (s *Server) handleRuntimes(w http.ResponseWriter, _ *http.Request) {
	runtimes := s.runtimes
	if runtimes == nil {
		runtimes = []execution.Runtime{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtimes": runtimes})
}

func (s *Server) requireHarnesses(w http.ResponseWriter) bool {
	if s.harnesses == nil {
		writeError(w, http.StatusServiceUnavailable, "postgres_unavailable", "Harness catalog is unavailable")
		return false
	}
	return true
}

func (s *Server) handleListHarnesses(w http.ResponseWriter, r *http.Request) {
	if !s.requireHarnesses(w) {
		return
	}
	harnesses, err := s.harnesses.ListHarnesses(r.Context())
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if harnesses == nil {
		harnesses = []execution.HarnessSnapshot{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"harnesses": harnesses})
}

func (s *Server) handleGetHarness(w http.ResponseWriter, r *http.Request) {
	if !s.requireHarnesses(w) {
		return
	}
	h, err := s.harnesses.GetHarness(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) handleCreateHarness(w http.ResponseWriter, r *http.Request) {
	s.writeHarness(w, r, false)
}

func (s *Server) handleUpdateHarness(w http.ResponseWriter, r *http.Request) {
	s.writeHarness(w, r, true)
}

func (s *Server) writeHarness(w http.ResponseWriter, r *http.Request, update bool) {
	if !s.requireHarnesses(w) {
		return
	}
	var h execution.HarnessSnapshot
	if !decodeRequest(w, r, &h) {
		return
	}
	if update && h.ID != "" && h.ID != r.PathValue("id") {
		writeError(w, http.StatusBadRequest, "invalid_request", "Harness id does not match URL")
		return
	}
	if err := h.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var err error
	status := http.StatusCreated
	if update {
		h, err = s.harnesses.UpdateHarness(r.Context(), r.PathValue("id"), h)
		status = http.StatusOK
	} else {
		h, err = s.harnesses.CreateHarness(r.Context(), h)
	}
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, status, h)
}

func (s *Server) handleDeleteHarness(w http.ResponseWriter, r *http.Request) {
	if !s.requireHarnesses(w) {
		return
	}
	h, err := s.harnesses.DeleteHarness(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}
