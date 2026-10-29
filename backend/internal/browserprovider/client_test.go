package browserprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

func TestLifecycleUsesAuthenticatedIdentityAndOnlyManagedProvider(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer internal" {
			t.Fatal("missing internal identity")
		}
		if r.URL.Path != "/environments/env_1/provision" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"environment_id":"env_1","profile_generation":1,"fencing_token":1,"state":"ready"}`))
	}))
	defer server.Close()
	client, err := New(server.URL, "internal")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ApplyEnvironment(context.Background(), postgres.EnvironmentCommand{
		ProjectID: "project_1", EnvironmentID: "env_1", Kind: "provision", ProfileGeneration: 1, FencingToken: 1,
		Policy: json.RawMessage(`{"allowed_domains":["example.com"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if received["fencing_token"] != float64(1) || received["profile_generation"] != float64(1) || received["project_id"] != "project_1" {
		t.Fatal(received)
	}
	for _, address := range []string{"file:///tmp/profile", "http://user:pass@worker", "http://worker/?token=secret"} {
		if _, err := New(address, "internal"); err == nil {
			t.Fatalf("unsafe provider %s", address)
		}
	}
	if _, err := New(server.URL, ""); err == nil {
		t.Fatal("missing authentication accepted")
	}
}
