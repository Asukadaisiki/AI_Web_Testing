// Package v3 exposes the AgentTrace v3 workspace HTTP API.
package v3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/browserprovider"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/execution"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/serviceprobe"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/stream"
)

const (
	maxRequestBodyBytes = 1 << 20
	defaultHeartbeat    = 15 * time.Second
	defaultSSEWriteWait = 5 * time.Second
)

// Repository is the canonical PostgreSQL boundary used by HTTP handlers.
type Repository interface {
	ListConversations(context.Context, string) ([]postgres.Conversation, error)
	CreateConversation(context.Context, postgres.CreateConversationParams) (postgres.Conversation, error)
	GetConversation(context.Context, string) (postgres.Conversation, []domain.Message, []domain.Run, error)
	CreateMessageAndRun(context.Context, postgres.CreateMessageParams) (domain.Message, domain.Run, error)
	GetRun(context.Context, string) (domain.Run, error)
	CreateRerun(context.Context, postgres.CreateRerunParams) (domain.Run, error)
	GetTraceDetails(context.Context, string) (postgres.TraceDetails, error)
	RequestCancel(context.Context, string) error
	PendingOutbox(context.Context) (int, error)
}

// HealthDependencies provides independent liveness probes for the API health endpoint.
type HealthDependencies struct {
	Postgres func(context.Context) error
	Kafka    func(context.Context) error
	Redis    func(context.Context) error
	Worker   func(context.Context) error
	Langfuse func(context.Context) error
	KafkaLag func(context.Context) serviceprobe.Measurement
}

type healthDependencies = HealthDependencies

// ServerConfig supplies the server-owned dependencies for the API process.
type ServerConfig struct {
	Repository        Repository
	Runtimes          []execution.Runtime
	Stream            *stream.Service
	Health            HealthDependencies
	ArtifactDir       string
	BridgeToken       string
	BridgeProjectID   string
	BrowserProvider   *browserprovider.Client
	HeartbeatInterval time.Duration
	SSEWriteTimeout   time.Duration
}

// Server is an HTTP handler for the one-workspace AgentTrace API.
type Server struct {
	repository      Repository
	runtimes        []execution.Runtime
	browserProvider *browserprovider.Client
	stream          *stream.Service
	health          HealthDependencies
	heartbeat       time.Duration
	writeWait       time.Duration
	handler         http.Handler
}

// New constructs the API routes. Dependency availability is reported through
// health rather than replaced with an in-memory canonical fallback.
func New(config ServerConfig) *Server {
	heartbeat := config.HeartbeatInterval
	if heartbeat <= 0 {
		heartbeat = defaultHeartbeat
	}
	writeWait := config.SSEWriteTimeout
	if writeWait <= 0 {
		writeWait = defaultSSEWriteWait
	}
	server := &Server{
		repository:      config.Repository,
		runtimes:        append([]execution.Runtime(nil), config.Runtimes...),
		stream:          config.Stream,
		health:          config.Health,
		heartbeat:       heartbeat,
		writeWait:       writeWait,
		browserProvider: config.BrowserProvider,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects/{id}/conversations", server.handleListConversations)
	mux.HandleFunc("POST /api/v1/projects/{id}/conversations", server.handleCreateConversation)
	mux.HandleFunc("GET /api/v1/conversations/{id}", server.handleGetConversation)
	mux.HandleFunc("DELETE /api/v1/conversations/{id}", server.handleDeleteConversation)
	mux.HandleFunc("POST /api/v1/conversations/{id}/messages", server.handleCreateMessage)
	mux.HandleFunc("GET /api/v1/runs/{id}", server.handleGetRun)
	mux.HandleFunc("POST /api/v1/runs/{id}/reruns", server.handleRerun)
	mux.HandleFunc("GET /api/v1/runs/{id}/trace", server.handleGetTrace)
	mux.HandleFunc("GET /api/v1/runs/{id}/events", server.handleEvents)
	mux.HandleFunc("POST /api/v1/runs/{id}/cancel", server.handleCancel)
	mux.HandleFunc("GET /api/v1/runtimes", server.handleRuntimes)
	mux.HandleFunc("GET /api/v1/health", server.handleHealth)
	server.registerRegistryRoutes(mux)
	server.registerEnvironmentRoutes(mux)
	server.registerControlSessionRoutes(mux)
	server.registerWorkerRoutes(mux)
	server.handler = server.bridgeGuard(mux, config)
	return server
}

// Handler returns the fully registered HTTP handler.
func (s *Server) Handler() http.Handler {
	if s == nil || s.handler == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusServiceUnavailable, "server_unavailable", "API server is not configured")
		})
	}
	return s.handler
}

func (s *Server) requireRepository(w http.ResponseWriter) bool {
	if s.repository != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "postgres_unavailable", "PostgreSQL is unavailable")
	return false
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	defer body.Close()
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeDecodeError(w, err)
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "request body must contain one JSON value")
		} else {
			writeDecodeError(w, err)
		}
		return false
	}
	return true
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds the limit")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writeRepositoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "requested record was not found")
	case errors.Is(err, postgres.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "request conflicts with durable state")
	default:
		writeError(w, http.StatusServiceUnavailable, "postgres_unavailable", "PostgreSQL operation failed")
	}
}

func requiredText(value, field string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}
