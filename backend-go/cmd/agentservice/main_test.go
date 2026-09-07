package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/planning"
)

type fakePlanningStore struct {
	detail planning.SessionDetail
	calls  int
}

func (s *fakePlanningStore) CreateSession(context.Context, int64, planning.CreateSessionRequest) (planning.SessionDetail, error) {
	panic("unused")
}

func (s *fakePlanningStore) ListSessions(context.Context, int64) ([]planning.SessionSummary, error) {
	panic("unused")
}

func (s *fakePlanningStore) GetSession(context.Context, int64, int64) (planning.SessionDetail, error) {
	s.calls++
	return s.detail, nil
}

func (s *fakePlanningStore) UpdateSession(context.Context, int64, int64, planning.UpdateSessionRequest) (planning.SessionDetail, error) {
	panic("unused")
}

func (s *fakePlanningStore) DeleteSession(context.Context, int64, int64) error {
	panic("unused")
}

func (s *fakePlanningStore) ListProjects(context.Context, int64, int64) ([]planning.ProjectSummary, error) {
	panic("unused")
}

func (s *fakePlanningStore) LinkProject(context.Context, int64, int64, int64) (planning.ProjectSummary, error) {
	panic("unused")
}

func (s *fakePlanningStore) UnlinkProject(context.Context, int64, int64, int64) error {
	panic("unused")
}

func (s *fakePlanningStore) CreateProject(context.Context, int64, int64, planning.CreateProjectRequest) (planning.ProjectSummary, error) {
	panic("unused")
}

func (s *fakePlanningStore) ResolveRunContext(context.Context, int64, int64) (string, int64, error) {
	panic("unused")
}

func TestBrowserCapabilityContextResolver(t *testing.T) {
	requirements, _ := json.Marshal(map[string]any{
		"clean_context":     true,
		"entry_url_or_page": "https://example.com/start",
	})
	store := &fakePlanningStore{
		detail: planning.SessionDetail{
			Session: planning.Session{
				Requirements: json.RawMessage(requirements),
				Projects: []planning.ProjectSummary{
					{ID: 7, Name: "owned"},
				},
			},
		},
	}
	resolver := browserCapabilityContextResolver(store)

	result, err := resolver(context.Background(), 5, 7, "11")
	if err != nil {
		t.Fatalf("resolver() error = %v", err)
	}
	if result["clean_context"] != true {
		t.Fatalf("clean_context = %#v", result["clean_context"])
	}
	if result["entry_url_or_page"] != "https://example.com/start" {
		t.Fatalf("entry_url_or_page = %#v", result["entry_url_or_page"])
	}
}

func TestBrowserCapabilityContextResolverSkipsNonPlanningConversation(t *testing.T) {
	store := &fakePlanningStore{}
	resolver := browserCapabilityContextResolver(store)

	result, err := resolver(context.Background(), 5, 7, "run_abc")
	if err != nil {
		t.Fatalf("resolver() error = %v", err)
	}
	if result != nil {
		t.Fatalf("context = %#v, want nil", result)
	}
	if store.calls != 0 {
		t.Fatalf("GetSession calls = %d, want 0", store.calls)
	}
}

func TestBrowserCapabilityContextResolverRejectsUnlinkedProject(t *testing.T) {
	store := &fakePlanningStore{
		detail: planning.SessionDetail{
			Session: planning.Session{
				Requirements: json.RawMessage(`{"clean_context":false}`),
				Projects: []planning.ProjectSummary{
					{ID: 8, Name: "other"},
				},
			},
		},
	}
	resolver := browserCapabilityContextResolver(store)

	if _, err := resolver(context.Background(), 5, 7, "11"); err == nil {
		t.Fatal("resolver() error = nil, want project link error")
	}
}
