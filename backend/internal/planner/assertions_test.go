package planner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/worker"
)

func TestObservationHasTextChecksEachVisibleNodeWithoutJoiningNodes(t *testing.T) {
	tests := []struct {
		name        string
		observation contract.Observation
		text        string
		want        bool
	}{
		{
			name: "element name",
			observation: contract.Observation{Elements: []contract.Element{{
				Name: "  Logged   in as AI Web Test ", Visible: true,
			}}},
			text: "logged IN as ai web test",
			want: true,
		},
		{
			name: "element text",
			observation: contract.Observation{Elements: []contract.Element{{
				Text: "  Logged   in as AI Web Test ", Visible: true,
			}}},
			text: "logged IN as ai web test",
			want: true,
		},
		{
			name: "element full text",
			observation: contract.Observation{Elements: []contract.Element{{
				FullText: "  Logged   in as AI Web Test ", Visible: true,
			}}},
			text: "logged IN as ai web test",
			want: true,
		},
		{
			name: "structure full text",
			observation: contract.Observation{Structures: []contract.StructureNode{{
				FullText: "  Logged   in as AI Web Test ", Visible: true,
			}}},
			text: "logged IN as ai web test",
			want: true,
		},
		{
			name: "hidden element",
			observation: contract.Observation{Elements: []contract.Element{{
				Name: "Logged in as AI Web Test", Text: "Logged in as AI Web Test",
				FullText: "Logged in as AI Web Test", Visible: false,
			}}},
			text: "Logged in as AI Web Test",
			want: false,
		},
		{
			name: "hidden structure",
			observation: contract.Observation{Structures: []contract.StructureNode{{
				FullText: "Logged in as AI Web Test", Visible: false,
			}}},
			text: "Logged in as AI Web Test",
			want: false,
		},
		{
			name: "unrelated nodes are not joined",
			observation: contract.Observation{
				Elements: []contract.Element{{Text: "Logged in as", Visible: true}},
				Structures: []contract.StructureNode{{
					FullText: "AI Web Test", Visible: true,
				}},
			},
			text: "Logged in as AI Web Test",
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := observationHasText(test.observation, test.text); got != test.want {
				t.Fatalf("observationHasText() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestUnsatisfiedPageAssertionsDoNotCommit(t *testing.T) {
	gotoStep, err := contract.DeriveGotoStep(
		0, "open products", "https://shop.test/products",
	)
	if err != nil {
		t.Fatalf("derive goto: %v", err)
	}
	original := []contract.Step{gotoStep}

	tests := []struct {
		name string
		call func(*Planner) (Result, error)
	}{
		{
			name: "text",
			call: func(planner *Planner) (Result, error) {
				return planner.assertText("Missing text", "verify missing text")
			},
		},
		{
			name: "url",
			call: func(planner *Planner) (Result, error) {
				return planner.assertURL("/missing", "verify missing url")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planner := &Planner{
				observation: contract.Observation{
					URL: "https://shop.test/products",
					Elements: []contract.Element{{
						Text: "Products", Visible: true,
					}},
				},
				hasPage: true,
				steps:   append([]contract.Step(nil), original...),
			}

			result, err := test.call(planner)
			if err != nil {
				t.Fatalf("assertion: %v", err)
			}
			if result.OK || result.Error != string(contract.SignalConditionUnmet) {
				t.Fatalf("assertion result = %+v, want condition_unmet failure", result)
			}
			if got := planner.Steps(); !reflect.DeepEqual(got, original) {
				t.Fatalf("committed steps = %#v, want unchanged %#v", got, original)
			}
		})
	}
}

func TestSatisfiedPageAssertionsCommit(t *testing.T) {
	planner := &Planner{
		observation: contract.Observation{
			URL: "https://shop.test/products?search=Blue%20Top",
			Structures: []contract.StructureNode{{
				FullText: "Searched Products Blue Top", Visible: true,
			}},
		},
		hasPage: true,
	}

	textResult, err := planner.assertText(" blue   top ", "verify product")
	if err != nil || !textResult.OK {
		t.Fatalf("assert text: result=%+v err=%v", textResult, err)
	}
	urlResult, err := planner.assertURL("search=Blue%20Top", "verify search url")
	if err != nil || !urlResult.OK {
		t.Fatalf("assert url: result=%+v err=%v", urlResult, err)
	}
	if got := planner.Steps(); len(got) != 2 ||
		got[0].Action != contract.ActionAssertText ||
		got[1].Action != contract.ActionAssertURL {
		t.Fatalf("committed steps = %#v, want text and url assertions", got)
	}
}

func TestFailedTargetAssertionsUseWorkerEvaluationBeforeCommit(t *testing.T) {
	tests := []struct {
		name    string
		action  contract.Action
		expects contract.Expects
	}{
		{
			name:    "element state",
			action:  contract.ActionAssertElement,
			expects: contract.Expects{Element: stringPointer("disabled")},
		},
		{
			name:    "attribute",
			action:  contract.ActionAssertAttribute,
			expects: contract.Expects{Attribute: stringPointer("data-state=ready")},
		},
		{
			name:    "count",
			action:  contract.ActionAssertCount,
			expects: contract.Expects{Count: stringPointer("2")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const pageURL = "https://shop.test/products"
			locator := contract.Locator{
				Kind: "role", Role: "button", Name: "Continue", Exact: true, MatchCount: 1,
			}
			observation := contract.Observation{
				ObservationID: "obs-products",
				PageStateID:   "page-products",
				URL:           pageURL,
				Title:         "Products",
				Elements: []contract.Element{{
					Ref: "continue", Tag: "button", Role: "button", Name: "Continue",
					Visible: true, Enabled: true, Locators: []contract.Locator{locator},
				}},
			}
			gotoStep, err := contract.DeriveGotoStep(0, "open products", pageURL)
			if err != nil {
				t.Fatalf("derive goto: %v", err)
			}
			var acts []contract.ActRequest
			mux := http.NewServeMux()
			mux.HandleFunc("POST /sessions/{id}/act", func(w http.ResponseWriter, r *http.Request) {
				var request contract.ActRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				acts = append(acts, request)
				message := "assertion did not hold"
				writeReplayJSON(w, contract.ActResponse{
					Status:      "failed",
					Observation: observation,
					Error: &contract.StepError{
						Kind: contract.SignalConditionUnmet, Message: message,
					},
				})
			})
			mux.HandleFunc("DELETE /sessions/{id}", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, _ *http.Request) {
				writeReplayJSON(w, contract.Session{SessionID: "browser-replayed"})
			})
			mux.HandleFunc("POST /sessions/{id}/navigate", func(w http.ResponseWriter, _ *http.Request) {
				writeReplayJSON(w, observation)
			})
			server := httptest.NewServer(mux)
			t.Cleanup(server.Close)

			planner := &Planner{
				client:           worker.New(server.URL),
				sessionID:        "assertion-test",
				browserSessionID: "browser-original",
				observation:      observation,
				hasPage:          true,
				steps:            []contract.Step{gotoStep},
			}
			result, err := planner.targetAction(
				context.Background(), test.action, "Continue", nil, "", "",
				"verify continue", test.expects,
			)
			if err != nil {
				t.Fatalf("target assertion: %v", err)
			}
			if result.OK || result.Error != string(contract.SignalConditionUnmet) {
				t.Fatalf("target assertion result = %+v, want condition_unmet", result)
			}
			if len(acts) != 1 || acts[0].Action != test.action {
				t.Fatalf("worker assertion requests = %#v, want one %s", acts, test.action)
			}
			if got := planner.Steps(); !reflect.DeepEqual(got, []contract.Step{gotoStep}) {
				t.Fatalf("committed steps = %#v, want only goto", got)
			}
		})
	}
}
