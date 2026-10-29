package v3

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type ControlSessionRepository interface {
	CreateControlSession(context.Context, postgres.CreateControlSessionParams) (postgres.ControlSession, string, error)
	GetControlSession(context.Context, string, string, string) (postgres.ControlSession, error)
	ConsumeControlSession(context.Context, string, string, string, string) (postgres.ControlSession, postgres.Environment, error)
	ActivateControlSession(context.Context, string, string, string, string) (postgres.ControlSession, error)
	ClaimControlSessionOperation(context.Context, string, string, string) (postgres.ControlSession, postgres.Environment, error)
	CompleteControlSessionOperation(context.Context, postgres.ControlSession, bool) error
	BeginCloseControlSession(context.Context, string, string, string) (postgres.ControlSession, error)
	CompleteCloseControlSession(context.Context, postgres.ControlSession) (postgres.ControlSession, error)
	FailControlSession(context.Context, string, string, string) error
	RecoverExpiredControlSessions(context.Context) (int, error)
	EnvironmentWorkerID(context.Context, string) (string, error)
}

func (s *Server) registerControlSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/environments/{id}/control-session", s.handleCreateControlSession)
	mux.HandleFunc("GET /api/v1/control-sessions/{id}", s.handleGetControlSession)
	mux.HandleFunc("POST /api/v1/control-sessions/{id}/consume", s.handleConsumeControlSession)
	mux.HandleFunc("GET /api/v1/control-sessions/{id}/screenshot", s.handleControlScreenshot)
	mux.HandleFunc("POST /api/v1/control-sessions/{id}/input", s.handleControlInput)
	mux.HandleFunc("DELETE /api/v1/control-sessions/{id}", s.handleCloseControlSession)
}

func (s *Server) requireControlSessions(w http.ResponseWriter, r *http.Request) (ControlSessionRepository, BridgeIdentity, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	identity, ok := BridgeIdentityFromContext(r.Context())
	if !ok {
		writeBridgeForbidden(w)
		return nil, BridgeIdentity{}, false
	}
	repository, ok := s.repository.(ControlSessionRepository)
	if !ok || s.browserProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "control_unavailable", "browser control is unavailable")
		return nil, identity, false
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/control-sessions/") {
		owners, ok := s.repository.(ControlSessionOwnershipRepository)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "control_unavailable", "browser control is unavailable")
			return nil, identity, false
		}
		_, environmentID, err := owners.ControlSessionOwner(r.Context(), r.PathValue("id"))
		if err != nil {
			writeRepositoryError(w, err)
			return nil, identity, false
		}
		workerID, err := repository.EnvironmentWorkerID(r.Context(), environmentID)
		if err != nil {
			writeRepositoryError(w, err)
			return nil, identity, false
		}
		if workerID != s.browserProvider.WorkerID() {
			writeError(w, http.StatusServiceUnavailable, "provider_unavailable", "environment provider is unavailable")
			return nil, identity, false
		}
	}
	// Opportunistic bounded recovery is safe even if no background sweeper is
	// running: an expired reservation can never become runnable on its own.
	if _, err := repository.RecoverExpiredControlSessions(r.Context()); err != nil {
		writeRepositoryError(w, err)
		return nil, identity, false
	}
	return repository, identity, true
}

func (s *Server) handleCreateControlSession(w http.ResponseWriter, r *http.Request) {
	repository, identity, ok := s.requireControlSessions(w, r)
	if !ok {
		return
	}
	environment, ok := s.requireEnvironmentOwner(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	workerID, err := repository.EnvironmentWorkerID(r.Context(), environment.ID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if workerID != s.browserProvider.WorkerID() {
		writeError(w, http.StatusServiceUnavailable, "provider_unavailable", "environment provider is unavailable")
		return
	}
	if identity.Role == "viewer" {
		writeBridgeForbidden(w)
		return
	}
	var input struct {
		TTLSeconds int `json:"ttl_seconds,omitempty"`
	}
	if !decodeRequest(w, r, &input) {
		return
	}
	if input.TTLSeconds < 0 || input.TTLSeconds > int(postgres.MaxControlSessionTTL/time.Second) {
		writeError(w, 400, "invalid_request", "control session TTL is outside the allowed range")
		return
	}
	c, token, err := repository.CreateControlSession(r.Context(), postgres.CreateControlSessionParams{
		EnvironmentID: environment.ID, ProjectID: identity.ProjectID, ActorID: identity.ActorID,
		TTL: time.Duration(input.TTLSeconds) * time.Second,
	})
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		postgres.ControlSession
		Token string `json:"token"`
	}{c, token})
}

func (s *Server) handleGetControlSession(w http.ResponseWriter, r *http.Request) {
	repository, identity, ok := s.requireControlSessions(w, r)
	if !ok {
		return
	}
	c, err := repository.GetControlSession(r.Context(), r.PathValue("id"), identity.ProjectID, identity.ActorID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) handleConsumeControlSession(w http.ResponseWriter, r *http.Request) {
	repository, identity, ok := s.requireControlSessions(w, r)
	if !ok {
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !decodeRequest(w, r, &input) {
		return
	}
	if len(input.Token) == 0 || len(input.Token) > 128 {
		writeError(w, 400, "invalid_request", "a one-time control token is required")
		return
	}
	c, environment, err := repository.ConsumeControlSession(r.Context(), r.PathValue("id"), identity.ProjectID, identity.ActorID, input.Token)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	handle, err := s.browserProvider.Open(r.Context(), environment, c.ID)
	if err != nil {
		failControlSession(r.Context(), repository, c)
		writeControlProviderError(w)
		return
	}
	active, err := repository.ActivateControlSession(r.Context(), c.ID, identity.ProjectID, identity.ActorID, handle)
	if err != nil {
		// Even when the request was cancelled, stop an acknowledged Open. An
		// uncertain acknowledgement remains needs_review, never auto-ready.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 35*time.Second)
		_ = s.browserProvider.Close(ctx, handle, c.ProfileGeneration, c.FencingToken, c.ProjectID, c.EnvironmentID)
		cancel()
		failControlSession(r.Context(), repository, c)
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 200, active)
}

func failControlSession(ctx context.Context, repository ControlSessionRepository, c postgres.ControlSession) {
	recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = repository.FailControlSession(recovery, c.ID, c.ProjectID, c.ActorID)
}

func writeControlProviderError(w http.ResponseWriter) {
	writeError(w, http.StatusBadGateway, "browser_provider_failed", "browser operation failed; environment requires review or reset")
}

func (s *Server) handleControlScreenshot(w http.ResponseWriter, r *http.Request) {
	repository, identity, ok := s.requireControlSessions(w, r)
	if !ok {
		return
	}
	c, environment, err := repository.ClaimControlSessionOperation(r.Context(), r.PathValue("id"), identity.ProjectID, identity.ActorID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	image, providerErr := s.browserProvider.Screenshot(r.Context(), environment.ID, c.ProviderSessionID, c.ProfileGeneration, c.FencingToken, c.ProjectID)
	err = repository.CompleteControlSessionOperation(r.Context(), c, providerErr == nil)
	if providerErr != nil || err != nil {
		failControlSession(r.Context(), repository, c)
		if providerErr != nil {
			writeControlProviderError(w)
		} else {
			writeRepositoryError(w, err)
		}
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(200)
	_, _ = w.Write(image)
}

type controlInput struct {
	Action string  `json:"action"`
	X      *int    `json:"x,omitempty"`
	Y      *int    `json:"y,omitempty"`
	Key    *string `json:"key,omitempty"`
	Text   *string `json:"text,omitempty"`
	URL    *string `json:"url,omitempty"`
}

func (input controlInput) validated() (map[string]any, bool) {
	supplied := 0
	for _, set := range []bool{input.X != nil, input.Y != nil, input.Key != nil, input.Text != nil, input.URL != nil} {
		if set {
			supplied++
		}
	}
	body := map[string]any{"action": input.Action}
	switch input.Action {
	case "click":
		if supplied != 2 || input.X == nil || input.Y == nil || *input.X < 0 || *input.Y < 0 || *input.X >= 1280 || *input.Y >= 800 {
			return nil, false
		}
		body["x"], body["y"] = *input.X, *input.Y
	case "key":
		if supplied != 1 || input.Key == nil {
			return nil, false
		}
		switch *input.Key {
		case "Enter", "Tab", "Shift+Tab", "Escape", "Backspace", "Delete", "ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End":
			body["key"] = *input.Key
		default:
			return nil, false
		}
	case "type":
		if supplied != 1 || input.Text == nil || !utf8.ValidString(*input.Text) || utf8.RuneCountInString(*input.Text) > 4096 {
			return nil, false
		}
		for _, char := range *input.Text {
			if unicode.IsControl(char) {
				return nil, false
			}
		}
		body["text"] = *input.Text
	case "navigate":
		if supplied != 1 || input.URL == nil || len(*input.URL) > 4096 || strings.TrimSpace(*input.URL) != *input.URL {
			return nil, false
		}
		u, err := url.Parse(*input.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
			return nil, false
		}
		body["url"] = *input.URL
	default:
		return nil, false
	}
	return body, true
}

func (s *Server) handleControlInput(w http.ResponseWriter, r *http.Request) {
	repository, identity, ok := s.requireControlSessions(w, r)
	if !ok {
		return
	}
	if identity.Role == "viewer" {
		writeBridgeForbidden(w)
		return
	}
	var input controlInput
	if !decodeRequest(w, r, &input) {
		return
	}
	body, valid := input.validated()
	if !valid {
		writeError(w, 400, "invalid_request", "input must be an allowed bounded browser action")
		return
	}
	c, environment, err := repository.ClaimControlSessionOperation(r.Context(), r.PathValue("id"), identity.ProjectID, identity.ActorID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	providerErr := s.browserProvider.Input(r.Context(), environment.ID, c.ProviderSessionID, c.ProfileGeneration, c.FencingToken, body, c.ProjectID)
	err = repository.CompleteControlSessionOperation(r.Context(), c, providerErr == nil)
	if providerErr != nil || err != nil {
		failControlSession(r.Context(), repository, c)
		if providerErr != nil {
			writeControlProviderError(w)
		} else {
			writeRepositoryError(w, err)
		}
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleCloseControlSession(w http.ResponseWriter, r *http.Request) {
	repository, identity, ok := s.requireControlSessions(w, r)
	if !ok {
		return
	}
	c, err := repository.BeginCloseControlSession(r.Context(), r.PathValue("id"), identity.ProjectID, identity.ActorID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	if c.ProviderSessionID != "" {
		if err := s.browserProvider.Close(r.Context(), c.ProviderSessionID, c.ProfileGeneration, c.FencingToken, c.ProjectID, c.EnvironmentID); err != nil {
			failControlSession(r.Context(), repository, c)
			writeControlProviderError(w)
			return
		}
	}
	closed, err := repository.CompleteCloseControlSession(r.Context(), c)
	if err != nil {
		failControlSession(r.Context(), repository, c)
		writeRepositoryError(w, err)
		return
	}
	writeJSON(w, 200, closed)
}
