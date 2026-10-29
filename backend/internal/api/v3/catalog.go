package v3

import (
	"net/http"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
)

func (s *Server) handleRuntimes(w http.ResponseWriter, _ *http.Request) {
	runtimes := s.runtimes
	if runtimes == nil {
		runtimes = []execution.Runtime{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"runtimes": runtimes})
}
