package v3

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"path"
	"strings"
	"syscall"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type ProjectBindingRepository interface {
	GetProjectBinding(context.Context, string) (postgres.ProjectBinding, error)
}

// ControlSessionOwnershipRepository is optional until takeover is installed.
// The owner must be read from PostgreSQL, never supplied by the caller.
type ControlSessionOwnershipRepository interface {
	ControlSessionOwner(context.Context, string) (projectID, environmentID string, err error)
}

type BridgeIdentity struct {
	ProjectID         string
	LangfuseProjectID string
	Role              string
	ActorID           string
}

type bridgeIdentityKey struct{}

func BridgeIdentityFromContext(ctx context.Context) (BridgeIdentity, bool) {
	identity, ok := ctx.Value(bridgeIdentityKey{}).(BridgeIdentity)
	return identity, ok
}

// bridgeGuard is the sole entry point in bridge mode, including unmatched
// routes and artifacts. It never dispatches the unrestricted mux before scope
// and ownership checks.
// Parent integration passes ServerConfig.ManagementEnabled as the optional
// flag. Omitting it retains the fixed-Conversation first-loop allowlist.
func (s *Server) bridgeGuard(next http.Handler, config ServerConfig, managementEnabled ...bool) http.Handler {
	token := strings.TrimSpace(config.BridgeToken)
	projectID := strings.TrimSpace(config.BridgeProjectID)
	conversationID := strings.TrimSpace(config.BridgeConversationID)
	dynamic := len(managementEnabled) > 0 && managementEnabled[0]
	complete := token != "" && projectID != "" && conversationID != ""
	if dynamic {
		complete = token != ""
	}
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/health" && r.URL.RawPath == "" {
			s.handleHealth(w, r)
			return
		}
		if !complete {
			writeError(w, http.StatusServiceUnavailable, "bridge_unavailable", "bridge configuration is incomplete")
			return
		}
		headers := r.Header.Values("Authorization")
		authorization := r.Header.Get("Authorization")
		supplied := sha256.Sum256([]byte(strings.TrimPrefix(authorization, "Bearer ")))
		matched := subtle.ConstantTimeCompare(supplied[:], expected[:])
		if len(headers) != 1 || !strings.HasPrefix(authorization, "Bearer ") || matched != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "service authentication is required")
			return
		}
		kind, id, allowed := bridgeRoute(r, dynamic)
		if !allowed {
			writeBridgeForbidden(w)
			return
		}
		requestProjectID := projectID
		if dynamic {
			identity, ok := s.requireDynamicBridgeIdentity(w, r, projectID)
			if !ok {
				return
			}
			if !managementRoleAllows(r, identity.Role) {
				writeBridgeForbidden(w)
				return
			}
			requestProjectID = identity.ProjectID
			r = r.WithContext(context.WithValue(r.Context(), bridgeIdentityKey{}, identity))
		}
		switch kind {
		case "project":
			if id != requestProjectID {
				writeBridgeForbidden(w)
				return
			}
		case "conversation":
			if !dynamic && id != conversationID {
				writeBridgeForbidden(w)
				return
			}
			if !s.requireRepository(w) {
				return
			}
			conversation, _, _, err := s.repository.GetConversation(r.Context(), id)
			if err != nil {
				writeRepositoryError(w, err)
				return
			}
			if conversation.ID != id || conversation.ProjectID != requestProjectID {
				writeBridgeForbidden(w)
				return
			}
		case "run", "artifact":
			scopedConversationID := conversationID
			if dynamic {
				scopedConversationID = ""
			}
			if !s.bridgeOwnsRun(w, r, id, requestProjectID, scopedConversationID) {
				return
			}
			if kind == "artifact" {
				s.serveBridgeArtifact(w, r, config.ArtifactDir, id)
				return
			}
		case "environment":
			if _, ok := s.requireEnvironmentOwner(w, r, id); !ok {
				return
			}
		case "control-session":
			repository, ok := s.repository.(ControlSessionOwnershipRepository)
			if !ok {
				writeError(w, http.StatusServiceUnavailable, "management_unavailable", "control sessions are unavailable")
				return
			}
			owner, environmentID, err := repository.ControlSessionOwner(r.Context(), id)
			if err != nil {
				writeRepositoryError(w, err)
				return
			}
			if owner != requestProjectID || environmentID == "" {
				writeBridgeForbidden(w)
				return
			}
			if _, ok := s.requireEnvironmentOwner(w, r, environmentID); !ok {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Explicit method/path matching also rejects mux cleaning redirects, so no
// alternate path or future native route can bypass the first-loop allowlist.
func bridgeRoute(r *http.Request, dynamic ...bool) (kind, id string, allowed bool) {
	if r.URL.Path != path.Clean(r.URL.Path) || strings.Contains(r.URL.Path, `\`) ||
		strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%2f") {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(dynamic) > 0 && dynamic[0] {
		return managementBridgeRoute(r.Method, parts)
	}
	method := r.Method
	if len(parts) >= 3 && parts[0] == "artifacts" && (method == http.MethodGet || method == http.MethodHead) {
		return "artifact", parts[1], true
	}
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "v1" {
		return "", "", false
	}
	switch parts[2] {
	case "health", "runtimes":
		return "", "", len(parts) == 3 && method == http.MethodGet
	case "harnesses":
		if len(parts) == 3 {
			return "", "", method == http.MethodGet || method == http.MethodPost
		}
		return "", "", len(parts) == 4 && (method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete)
	case "conversations":
		if len(parts) == 4 && method == http.MethodGet {
			return "conversation", parts[3], true
		}
		if len(parts) == 5 && parts[4] == "messages" && method == http.MethodPost {
			return "conversation", parts[3], true
		}
	case "runs":
		if len(parts) == 4 && method == http.MethodGet {
			return "run", parts[3], true
		}
		if len(parts) == 5 {
			switch parts[4] {
			case "trace", "events":
				return "run", parts[3], method == http.MethodGet
			case "cancel", "reruns":
				return "run", parts[3], method == http.MethodPost
			}
		}
	}
	return "", "", false
}

func (s *Server) requireDynamicBridgeIdentity(w http.ResponseWriter, r *http.Request, configuredProject string) (BridgeIdentity, bool) {
	project, projectOK := trustedBridgeHeader(r, "X-Agenttrace-Project-Id")
	langfuse, langfuseOK := trustedBridgeHeader(r, "X-Langfuse-Project-Id")
	role, roleOK := trustedBridgeHeader(r, "X-Agenttrace-Role")
	actor, actorOK := trustedBridgeHeader(r, "X-Agenttrace-Actor-Id")
	if !projectOK || !langfuseOK || !roleOK || !actorOK || !knownBridgeRole(role) ||
		(configuredProject != "" && project != configuredProject) {
		writeBridgeForbidden(w)
		return BridgeIdentity{}, false
	}
	repository, ok := s.repository.(ProjectBindingRepository)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "management_unavailable", "project bindings are unavailable")
		return BridgeIdentity{}, false
	}
	binding, err := repository.GetProjectBinding(r.Context(), project)
	if err != nil {
		writeRepositoryError(w, err)
		return BridgeIdentity{}, false
	}
	if binding.AgenttraceProjectID != project || binding.LangfuseProjectID != langfuse || binding.Status != "active" {
		writeBridgeForbidden(w)
		return BridgeIdentity{}, false
	}
	return BridgeIdentity{ProjectID: project, LangfuseProjectID: langfuse, Role: role, ActorID: actor}, true
}

func trustedBridgeHeader(r *http.Request, name string) (string, bool) {
	values := r.Header.Values(name)
	value := r.Header.Get(name)
	return value, len(values) == 1 && value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, ",\r\n")
}

func knownBridgeRole(role string) bool {
	return role == "viewer" || role == "member" || role == "admin" || role == "project_admin" || role == "system_admin"
}

func managementBridgeRoute(method string, parts []string) (kind, id string, allowed bool) {
	for _, part := range parts {
		if part == "" {
			return "", "", false
		}
	}
	if len(parts) >= 3 && parts[0] == "artifacts" && (method == "GET" || method == "HEAD") {
		return "artifact", parts[1], true
	}
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "v1" {
		return "", "", false
	}
	switch parts[2] {
	case "health", "runtimes":
		return "", "", len(parts) == 3 && method == "GET"
	case "projects":
		if len(parts) < 5 {
			break
		}
		projectID, resource := parts[3], parts[4]
		if len(parts) == 5 {
			switch resource {
			case "conversations", "skills", "mcp-bindings", "harnesses":
				return "project", projectID, method == "GET" || method == "POST"
			case "services", "workers", "binding":
				return "project", projectID, method == "GET"
			}
		}
		if len(parts) == 6 {
			switch resource {
			case "skills":
				if parts[5] == "uploads" {
					return "project", projectID, method == "POST"
				}
				return "project", projectID, method == "GET"
			case "mcp-bindings":
				return "project", projectID, method == "GET" || method == "PATCH"
			case "harnesses", "skill-versions", "harness-versions":
				return "project", projectID, method == "GET"
			case "workers":
				return "project", projectID, method == "GET" || method == "PATCH"
			}
		}
		if len(parts) == 7 {
			switch resource {
			case "skills", "harnesses":
				return "project", projectID, parts[6] == "versions" && (method == "GET" || method == "POST")
			case "skill-versions":
				return "project", projectID, (parts[6] == "publish" || parts[6] == "revoke") && method == "POST"
			case "harness-versions":
				return "project", projectID, parts[6] == "revoke" && method == "POST"
			case "workers":
				return "project", projectID, (parts[6] == "drain" || parts[6] == "enable") && method == "POST"
			}
		}
	case "conversations":
		if len(parts) == 4 {
			return "conversation", parts[3], method == "GET" || method == "PATCH" || method == "DELETE"
		}
		if len(parts) == 5 {
			switch parts[4] {
			case "environment":
				return "conversation", parts[3], method == "GET"
			case "runs":
				return "conversation", parts[3], method == "GET" || method == "POST"
			case "messages":
				return "conversation", parts[3], method == "POST"
			}
		}
	case "runs":
		if len(parts) == 4 {
			return "run", parts[3], method == "GET"
		}
		if len(parts) == 5 {
			switch parts[4] {
			case "trace", "events":
				return "run", parts[3], method == "GET"
			case "cancel", "reruns", "responses":
				return "run", parts[3], method == "POST"
			}
		}
	case "environments":
		if len(parts) == 4 {
			return "environment", parts[3], method == "GET" || method == "DELETE"
		}
		if len(parts) == 5 {
			return "environment", parts[3], (parts[4] == "reset" || parts[4] == "review" || parts[4] == "control-session" || parts[4] == "control-sessions") && method == "POST"
		}
	case "control-sessions":
		if len(parts) == 4 {
			return "control-session", parts[3], method == "GET" || method == "DELETE"
		}
		if len(parts) == 5 {
			if parts[4] == "screenshot" {
				return "control-session", parts[3], method == "GET"
			}
			return "control-session", parts[3], (parts[4] == "stop" || parts[4] == "consume" || parts[4] == "input") && method == "POST"
		}
	case "mcp-definitions":
		if len(parts) == 3 {
			return "system", "", method == "GET" || method == "POST"
		}
		if len(parts) == 4 {
			return "system", "", method == "GET"
		}
		if len(parts) == 5 {
			return "system", "", parts[4] == "versions" && (method == "GET" || method == "POST")
		}
	case "mcp-versions":
		if len(parts) == 4 {
			return "system", "", method == "GET"
		}
		if len(parts) == 5 {
			return "system", "", parts[4] == "revoke" && method == "POST"
		}
	}
	return "", "", false
}

func managementRoleAllows(r *http.Request, role string) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	admin := role == "admin" || role == "project_admin" || role == "system_admin"
	if len(parts) >= 3 && (parts[2] == "mcp-definitions" || parts[2] == "mcp-versions") {
		return role == "system_admin" || r.Method == "GET" && admin
	}
	if len(parts) >= 5 && parts[2] == "projects" {
		switch parts[4] {
		case "services", "workers":
			return admin
		case "skills":
			if r.Method == "POST" && len(parts) == 6 && parts[5] == "uploads" {
				return role != "viewer"
			}
			return r.Method == "GET" || admin
		case "skill-versions", "mcp-bindings", "harnesses", "harness-versions":
			return r.Method == "GET" || admin
		}
	}
	return r.Method == "GET" || r.Method == "HEAD" || role != "viewer"
}

func writeBridgeForbidden(w http.ResponseWriter) {
	writeError(w, http.StatusForbidden, "bridge_forbidden", "request is outside the configured bridge scope")
}

func (s *Server) bridgeOwnsRun(w http.ResponseWriter, r *http.Request, id, projectID, conversationID string) bool {
	if !s.requireRepository(w) {
		return false
	}
	run, err := s.repository.GetRun(r.Context(), id)
	if err != nil {
		writeRepositoryError(w, err)
		return false
	}
	if run.ID != id || run.ProjectID != projectID || run.ConversationID == "" ||
		(conversationID != "" && run.ConversationID != conversationID) {
		writeBridgeForbidden(w)
		return false
	}
	if conversationID == "" {
		conversation, _, _, err := s.repository.GetConversation(r.Context(), run.ConversationID)
		if err != nil {
			writeRepositoryError(w, err)
			return false
		}
		if conversation.ID != run.ConversationID || conversation.ProjectID != projectID {
			writeBridgeForbidden(w)
			return false
		}
	}
	return true
}

func (s *Server) serveBridgeArtifact(w http.ResponseWriter, r *http.Request, directory, runID string) {
	reference := strings.TrimPrefix(r.URL.Path, "/artifacts/")
	trace, err := s.repository.GetTraceDetails(r.Context(), runID)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	listed := false
	for _, operation := range trace.Operations {
		var artifacts struct {
			ScreenshotPaths []string `json:"screenshot_paths"`
		}
		if json.Unmarshal(operation.Artifacts, &artifacts) != nil {
			continue
		}
		for _, screenshot := range artifacts.ScreenshotPaths {
			if screenshot == reference {
				listed = true
			}
		}
	}
	if !listed || directory == "" {
		writeBridgeForbidden(w)
		return
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		writeBridgeArtifactUnavailable(w)
		return
	}
	defer root.Close()
	namespace, err := root.Lstat(runID)
	if err != nil || !namespace.IsDir() || namespace.Mode()&os.ModeSymlink != 0 {
		writeBridgeArtifactUnavailable(w)
		return
	}
	runRoot, err := root.OpenRoot(runID)
	if err != nil {
		writeBridgeArtifactUnavailable(w)
		return
	}
	defer runRoot.Close()
	openedNamespace, err := runRoot.Stat(".")
	if err != nil || !os.SameFile(namespace, openedNamespace) {
		writeBridgeArtifactUnavailable(w)
		return
	}
	// The Run-root handle enforces symlink containment even if paths change
	// during the request. Never hand an authorized path back to FileServer.
	relative := strings.TrimPrefix(reference, runID+"/")
	info, err := runRoot.Stat(relative)
	if err != nil || !info.Mode().IsRegular() {
		writeBridgeArtifactUnavailable(w)
		return
	}
	file, err := runRoot.OpenFile(relative, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		writeBridgeArtifactUnavailable(w)
		return
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeBridgeArtifactUnavailable(w)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, path.Base(reference), info.ModTime(), file)
}

func writeBridgeArtifactUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "artifact_unavailable", "screenshot is unavailable")
}
