package planner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/worker"
)

func TestToolsDoNotExposeDropLastStep(t *testing.T) {
	for _, tool := range Tools() {
		if tool.Name == "drop_last_step" {
			t.Fatal("committed steps must not be removable by a model tool")
		}
	}
}

func TestDryRunFailureKeepsPassedPrefixAndReplaysBrowser(t *testing.T) {
	const (
		domainSession = "sess_replay"
		oldBrowser    = "bsess_old"
		newBrowser    = "bsess_new"
		listURL       = "https://shop.test/products"
		detailURL     = "https://shop.test/item/1"
	)
	searchLocator := contract.Locator{
		Kind: "role", Role: "textbox", Name: "Search", Exact: true, MatchCount: 1,
	}
	widgetLocator := contract.Locator{Kind: "text", Text: "Widget", Exact: true, MatchCount: 1}
	cartLocator := contract.Locator{
		Kind: "role", Role: "button", Name: "Add to cart", Exact: true, MatchCount: 1,
	}
	listObservation := replayObservation(listURL,
		replayElement("search", "input", "textbox", "Search", "", searchLocator),
		replayElement("widget", "a", "link", "Widget", "Widget", widgetLocator),
	)
	detailObservation := replayObservation(detailURL,
		replayElement("cart", "button", "button", "Add to cart", "Add to cart", cartLocator),
	)

	type recordedAct struct {
		sessionID string
		request   contract.ActRequest
	}
	var (
		openedDomains []string
		closed        []string
		navigated     []string
		acts          []recordedAct
	)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, r *http.Request) {
		var request contract.OpenSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		openedDomains = append(openedDomains, request.SessionID)
		writeReplayJSON(w, contract.Session{SessionID: newBrowser})
	})
	mux.HandleFunc("DELETE /sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		closed = append(closed, r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /sessions/{id}/navigate", func(w http.ResponseWriter, r *http.Request) {
		var request contract.NavigateRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		navigated = append(navigated, r.PathValue("id")+" "+request.URL)
		writeReplayJSON(w, listObservation)
	})
	mux.HandleFunc("POST /sessions/{id}/act", func(w http.ResponseWriter, r *http.Request) {
		var request contract.ActRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		acts = append(acts, recordedAct{sessionID: r.PathValue("id"), request: request})
		observation := listObservation
		if request.Action == contract.ActionClick || request.Action == contract.ActionAssertElement {
			observation = detailObservation
		}
		writeReplayJSON(w, contract.ActResponse{Status: "passed", Observation: observation})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	gotoStep, err := contract.DeriveGotoStep(0, "open products", listURL)
	if err != nil {
		t.Fatalf("derive goto: %v", err)
	}
	alpha := "alpha"
	inputStep := replayActionStep(
		t, 1, contract.ActionInput, "enter search", listURL, searchLocator, &alpha,
		contract.Expects{Value: &alpha},
	)
	textStep, err := contract.DeriveAssertTextStep(2, "products are visible", "Widget", listURL)
	if err != nil {
		t.Fatalf("derive text assertion: %v", err)
	}
	expectDetail := "/item/1"
	clickStep := replayActionStep(
		t, 3, contract.ActionClick, "open widget", listURL, widgetLocator, nil,
		contract.Expects{URL: &expectDetail},
	)
	visible := "visible"
	elementStep := replayActionStep(
		t, 4, contract.ActionAssertElement, "cart action is visible", detailURL, cartLocator, nil,
		contract.Expects{Element: &visible},
	)
	failedStep, err := contract.DeriveAssertTextStep(5, "impossible tail", "Never appears", detailURL)
	if err != nil {
		t.Fatalf("derive failed assertion: %v", err)
	}
	original := []contract.Step{gotoStep, inputStep, textStep, clickStep, elementStep, failedStep}
	planner := &Planner{
		client:           worker.New(server.URL),
		sessionID:        domainSession,
		browserSessionID: oldBrowser,
		baseURL:          "https://shop.test",
		observation:      detailObservation,
		hasPage:          true,
		steps:            append([]contract.Step(nil), original...),
	}

	result, err := planner.PrepareDryRunRepair(context.Background(), contract.ExecutionResult{
		ExecutionID: "exec_failed",
		Status:      contract.ExecutionFailed,
		FinalURL:    detailURL,
		Steps: []contract.StepResult{
			{Index: 0, Action: contract.ActionGoto, Status: "passed"},
			{Index: 1, Action: contract.ActionInput, Status: "passed"},
			{Index: 2, Action: contract.ActionAssertText, Status: "passed"},
			{Index: 3, Action: contract.ActionClick, Status: "passed"},
			{Index: 4, Action: contract.ActionAssertElement, Status: "passed"},
			{
				Index: 5, Action: contract.ActionAssertText, Status: "failed", URLAfter: detailURL,
				Error: &contract.StepError{
					Kind: contract.SignalConditionUnmet, Message: "Never appears is not visible",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("prepare repair: %v", err)
	}
	if result.OK || result.Error != "dry_run_failed" {
		t.Fatalf("repair result = %+v", result)
	}
	if result.Page == nil || result.Page.URL != detailURL {
		t.Fatalf("restored page = %+v, want %s", result.Page, detailURL)
	}
	wantPrefix := original[:5]
	if got := planner.Steps(); !reflect.DeepEqual(got, wantPrefix) {
		t.Fatalf("committed steps = %#v, want prefix %#v", got, wantPrefix)
	}
	if planner.browserSessionID != newBrowser {
		t.Fatalf("browser session = %q, want %q", planner.browserSessionID, newBrowser)
	}
	if !reflect.DeepEqual(openedDomains, []string{domainSession}) {
		t.Fatalf("opened domain sessions = %v", openedDomains)
	}
	if !reflect.DeepEqual(closed, []string{oldBrowser}) {
		t.Fatalf("closed browser sessions = %v", closed)
	}
	if !reflect.DeepEqual(navigated, []string{newBrowser + " " + listURL}) {
		t.Fatalf("navigate replay = %v", navigated)
	}
	wantActs := []recordedAct{
		{
			sessionID: newBrowser,
			request: contract.ActRequest{
				Action: contract.ActionInput, Locator: searchLocator, Value: alpha,
				Postconditions: inputStep.Postconditions,
			},
		},
		{
			sessionID: newBrowser,
			request: contract.ActRequest{
				Action: contract.ActionClick, Locator: widgetLocator,
				Postconditions: clickStep.Postconditions,
			},
		},
		{
			sessionID: newBrowser,
			request: contract.ActRequest{
				Action: contract.ActionAssertElement, Locator: cartLocator,
				Postconditions: elementStep.Postconditions,
			},
		},
	}
	if !reflect.DeepEqual(acts, wantActs) {
		t.Fatalf("act replay = %#v, want %#v", acts, wantActs)
	}
}

func TestDryRunFailureRejectsAnUnknownFailedStep(t *testing.T) {
	gotoStep, err := contract.DeriveGotoStep(0, "open products", "https://shop.test/products")
	if err != nil {
		t.Fatalf("derive goto: %v", err)
	}
	planner := &Planner{
		client:    worker.New("http://127.0.0.1:1"),
		sessionID: "sess_invalid_failure",
		steps:     []contract.Step{gotoStep},
	}

	_, err = planner.PrepareDryRunRepair(context.Background(), contract.ExecutionResult{
		Status: contract.ExecutionFailed,
		Steps: []contract.StepResult{{
			Index: 1, Action: contract.ActionClick, Status: "failed",
		}},
	})
	if err == nil || !IsFatalError(err) ||
		!strings.Contains(err.Error(), "does not identify a committed step") {
		t.Fatalf("repair error = %v, want fatal invalid-index error", err)
	}
}

func replayActionStep(
	t *testing.T,
	index int,
	action contract.Action,
	intent string,
	pageURL string,
	locator contract.Locator,
	value *string,
	expects contract.Expects,
) contract.Step {
	t.Helper()
	step, err := contract.DeriveActionStep(
		index,
		action,
		intent,
		contract.Target{
			Hint:    intent,
			Locator: locator,
			Grounding: contract.Grounding{
				ObservationID: "obs_" + pageURL,
				PageStateID:   "state_" + pageURL,
				CandidateID:   "candidate",
				PageURL:       pageURL,
			},
		},
		value,
		false,
		pageURL,
		expects,
	)
	if err != nil {
		t.Fatalf("derive %s step: %v", action, err)
	}
	return step
}

func replayObservation(url string, elements ...contract.Element) contract.Observation {
	return contract.Observation{
		ObservationID: "obs_" + url,
		PageStateID:   "state_" + url,
		URL:           url,
		Title:         url,
		Elements:      elements,
	}
}

func replayElement(
	ref, tag, role, name, text string, locator contract.Locator,
) contract.Element {
	return contract.Element{
		Ref: ref, Tag: tag, Role: role, Name: name, Text: text,
		Visible: true, Enabled: true, Locators: []contract.Locator{locator},
	}
}

func writeReplayJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
