package v3

import (
	"context"
	"net/http"
	"time"
)

type healthDependency struct {
	Status        string `json:"status"`
	Required      bool   `json:"required"`
	Mode          string `json:"mode,omitempty"`
	PendingOutbox int    `json:"pending_outbox,omitempty"`
}

type healthResponse struct {
	Status       string                      `json:"status"`
	Dependencies map[string]healthDependency `json:"dependencies"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	dependencies := map[string]healthDependency{
		"postgres": checkDependency(r.Context(), s.health.Postgres, true),
		"kafka":    checkDependency(r.Context(), s.health.Kafka, false),
		"redis":    checkDependency(r.Context(), s.health.Redis, false),
		"worker":   checkDependency(r.Context(), s.health.Worker, false),
	}
	if dependencies["redis"].Status != "ok" {
		redis := dependencies["redis"]
		redis.Mode = "postgres_poll"
		dependencies["redis"] = redis
	}
	if s.repository != nil {
		if pending, err := s.repository.PendingOutbox(r.Context()); err == nil {
			kafka := dependencies["kafka"]
			kafka.PendingOutbox = pending
			dependencies["kafka"] = kafka
		}
	}
	response := healthResponse{Dependencies: dependencies}
	if dependencies["postgres"].Status != "ok" {
		response.Status = "down"
		writeJSON(w, http.StatusServiceUnavailable, response)
		return
	}
	response.Status = "ok"
	for name, dependency := range dependencies {
		if name != "postgres" && dependency.Status != "ok" {
			response.Status = "degraded"
			break
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func checkDependency(ctx context.Context, check func(context.Context) error, required bool) healthDependency {
	if check == nil {
		return healthDependency{Status: "disabled", Required: required}
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := check(checkCtx); err != nil {
		return healthDependency{Status: "down", Required: required}
	}
	return healthDependency{Status: "ok", Required: required}
}
