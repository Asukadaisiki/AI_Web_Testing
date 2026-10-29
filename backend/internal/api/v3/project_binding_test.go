package v3

import (
	"net/http"
	"strings"
	"testing"
)

func TestManagementBindingEndpointUsesAuthenticatedScope(t *testing.T) {
	repository := ownedManagementRepository()
	handler := New(ServerConfig{Repository: repository, BridgeToken: testBridgeToken}).Handler()
	got := managementRequest(handler, "GET", "/api/v1/projects/project_1/binding", "", "member")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"agenttrace_project_id":"project_1"`) ||
		!strings.Contains(got.Body.String(), `"langfuse_project_id":"lf_1"`) {
		t.Fatalf("binding preflight: status=%d body=%s", got.Code, got.Body)
	}
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		if got := managementRequest(handler, method, "/api/v1/projects/project_1/binding", "{}", "system_admin"); got.Code != http.StatusForbidden {
			t.Fatalf("binding mutation %s status=%d", method, got.Code)
		}
	}
	if got := managementRequest(handler, "GET", "/api/v1/projects/foreign/binding", "", "member"); got.Code != http.StatusForbidden {
		t.Fatalf("foreign binding status=%d", got.Code)
	}
}
