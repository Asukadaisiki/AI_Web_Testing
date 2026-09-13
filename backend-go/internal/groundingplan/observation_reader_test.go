package groundingplan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
)

func TestObservationReaderResolvesCandidateOnlyFromCurrentRun(t *testing.T) {
	ctx := context.Background()
	runs := agentservice.NewService(agentservice.NewMemoryRepository())
	current := startObservationRun(t, runs, "current")
	other := startObservationRun(t, runs, "other")
	source := recordObservationEvent(
		t,
		runs,
		current,
		agentservice.EventToolResult,
		observationToolResultFixture,
	)
	otherSource := recordObservationEvent(
		t,
		runs,
		other,
		agentservice.EventToolResult,
		strings.ReplaceAll(
			observationToolResultFixture,
			"candidate-submit",
			"candidate-other",
		),
	)
	reader := NewObservationReader(runs)

	resolved, err := reader.ResolveCandidate(
		ctx,
		current.ID,
		"click",
		browsercontract.CandidateRef{
			SchemaVersion:  browsercontract.CandidateRefVersion,
			SourceEventSeq: source.Seq,
			ProbeID:        "probe-current",
			ObservationID:  "obs-current",
			CandidateID:    "candidate-submit",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source.CandidateID != "candidate-submit" ||
		resolved.PageStateID != "form" ||
		resolved.PageStateSHA256 != strings.Repeat("a", 64) ||
		resolved.ElementRef != "form:7" ||
		resolved.Locator.Kind != "css" ||
		resolved.Locator.Value != "#submit_search" ||
		resolved.Provenance != "a11y_backend_dom_node" ||
		len(resolved.ContextPath.Frames) != 0 ||
		len(resolved.ContextPath.ShadowHosts) != 0 {
		t.Fatalf("resolved candidate = %#v", resolved)
	}

	_, err = reader.ResolveCandidate(
		ctx,
		current.ID,
		"click",
		browsercontract.CandidateRef{
			SchemaVersion:  browsercontract.CandidateRefVersion,
			SourceEventSeq: otherSource.Seq,
			ProbeID:        "probe-current",
			ObservationID:  "obs-current",
			CandidateID:    "candidate-other",
		},
	)
	if err == nil {
		t.Fatal("candidate from another run was accepted")
	}
}

func TestObservationReaderRejectsUntrustedObservationSources(t *testing.T) {
	tests := []struct {
		name      string
		eventType agentservice.EventType
		payload   string
	}{
		{
			name:      "non tool result event",
			eventType: agentservice.EventToolFinished,
			payload:   observationToolResultFixture,
		},
		{
			name:      "non exploration tool",
			eventType: agentservice.EventToolResult,
			payload: strings.Replace(
				observationToolResultFixture,
				`"tool":"explore_page"`,
				`"tool":"get_report"`,
				1,
			),
		},
		{
			name:      "probe mismatch",
			eventType: agentservice.EventToolResult,
			payload: strings.Replace(
				observationToolResultFixture,
				`"probe_id":"probe-current"`,
				`"probe_id":"probe-other"`,
				1,
			),
		},
		{
			name:      "duplicate candidate id",
			eventType: agentservice.EventToolResult,
			payload: strings.Replace(
				observationToolResultFixture,
				`"candidate_id":"candidate-secondary"`,
				`"candidate_id":"candidate-submit"`,
				1,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			runs := agentservice.NewService(agentservice.NewMemoryRepository())
			run := startObservationRun(t, runs, test.name)
			event := recordObservationEvent(
				t,
				runs,
				run,
				test.eventType,
				test.payload,
			)
			_, err := NewObservationReader(runs).Query(
				ctx,
				run.ID,
				ObservationQuery{
					SchemaVersion:  ObservationQueryVersion,
					PlanStepID:     "submit",
					SourceEventSeq: event.Seq,
					Action:         "click",
				},
			)
			if err == nil {
				t.Fatal("untrusted observation source was accepted")
			}
		})
	}
}

func TestObservationReaderRejectsMalformedPersistedObservations(t *testing.T) {
	tests := []struct {
		name    string
		old     string
		invalid string
	}{
		{
			name:    "missing context path",
			old:     `"context_path":{"frames":[],"shadow_hosts":[]},`,
			invalid: "",
		},
		{
			name:    "non hex page state sha256",
			old:     strings.Repeat("a", 64),
			invalid: strings.Repeat("z", 64),
		},
		{
			name:    "empty element ref",
			old:     `"element_ref":"form:7"`,
			invalid: `"element_ref":""`,
		},
		{
			name:    "empty provenance",
			old:     `"provenance":"a11y_backend_dom_node"`,
			invalid: `"provenance":""`,
		},
		{
			name:    "missing runtime",
			old:     `"runtime":{"connected":true,"visible":true,"enabled":true,"editable":false},`,
			invalid: "",
		},
		{
			name:    "missing runtime connected",
			old:     `"connected":true,`,
			invalid: "",
		},
		{
			name:    "missing runtime visible",
			old:     `"visible":true,`,
			invalid: "",
		},
		{
			name:    "missing runtime enabled",
			old:     `"enabled":true,`,
			invalid: "",
		},
		{
			name:    "missing runtime editable",
			old:     `,"editable":false`,
			invalid: "",
		},
		{
			name:    "invalid page revision",
			old:     `"revision":1`,
			invalid: `"revision":0`,
		},
		{
			name:    "missing a11y",
			old:     `"a11y":{"role":"button","name":"","states":{"focusable":true,"disabled":false},"relations":{}},`,
			invalid: "",
		},
		{
			name:    "missing dom",
			old:     `"dom":{"backend_node_id":7,"tag":"button","attrs":{"id":"submit_search","type":"button"},"text":""},`,
			invalid: "",
		},
		{
			name:    "negative observed count",
			old:     `"observed_count":1`,
			invalid: `"observed_count":-1`,
		},
		{
			name:    "invalid relation",
			old:     `"relations":[]`,
			invalid: `"relations":[{"kind":"","source":"form:7","target":"form:8"}]`,
		},
		{
			name:    "locator missing exact",
			old:     `,"exact":true`,
			invalid: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := strings.Replace(
				observationToolResultFixture,
				test.old,
				test.invalid,
				1,
			)
			if payload == observationToolResultFixture {
				t.Fatalf("fixture does not contain %q", test.old)
			}
			ctx := context.Background()
			runs := agentservice.NewService(agentservice.NewMemoryRepository())
			run := startObservationRun(t, runs, test.name)
			event := recordObservationEvent(
				t,
				runs,
				run,
				agentservice.EventToolResult,
				payload,
			)

			_, err := NewObservationReader(runs).Query(
				ctx,
				run.ID,
				ObservationQuery{
					SchemaVersion:  ObservationQueryVersion,
					PlanStepID:     "submit",
					SourceEventSeq: event.Seq,
					Action:         "click",
				},
			)
			if err == nil {
				t.Fatal("malformed persisted observation was accepted")
			}
		})
	}
}

func TestObservationReaderFiltersCandidatesByActionabilityAndDOMFields(t *testing.T) {
	ctx := context.Background()
	runs := agentservice.NewService(agentservice.NewMemoryRepository())
	run := startObservationRun(t, runs, "filter")
	event := recordObservationEvent(
		t,
		runs,
		run,
		agentservice.EventToolResult,
		observationToolResultFixture,
	)
	reader := NewObservationReader(runs)

	clickResult, err := reader.Query(ctx, run.ID, ObservationQuery{
		SchemaVersion:  ObservationQueryVersion,
		PlanStepID:     "submit",
		SourceEventSeq: event.Seq,
		ObservationID:  "obs-current",
		PageStateID:    "form",
		Action:         "click",
		Query:          "submit_search",
		Role:           "button",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(clickResult.Matches) != 1 ||
		clickResult.Matches[0].CandidateRef.CandidateID != "candidate-submit" ||
		clickResult.Matches[0].Name != "" ||
		clickResult.Matches[0].DOM.Attrs["id"] != "submit_search" {
		t.Fatalf("click query result = %#v", clickResult)
	}

	inputResult, err := reader.Query(ctx, run.ID, ObservationQuery{
		SchemaVersion:  ObservationQueryVersion,
		PlanStepID:     "enter_email",
		SourceEventSeq: event.Seq,
		Action:         "input",
		Role:           "textbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputResult.Matches) != 1 ||
		inputResult.Matches[0].CandidateRef.CandidateID != "candidate-input" {
		t.Fatalf("input query result = %#v", inputResult)
	}
	scopedResult, err := reader.Query(ctx, run.ID, ObservationQuery{
		SchemaVersion:  ObservationQueryVersion,
		PlanStepID:     "open_details",
		SourceEventSeq: event.Seq,
		Action:         "click",
		Query:          "deep-control",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(scopedResult.Matches) != 1 ||
		scopedResult.Matches[0].CandidateRef.CandidateID != "candidate-scoped" {
		t.Fatalf("scoped locator query result = %#v", scopedResult)
	}
	for _, match := range append(clickResult.Matches, inputResult.Matches...) {
		if match.ObservedCount != 1 ||
			match.CandidateRef.CandidateID == "candidate-count-two" ||
			match.CandidateRef.CandidateID == "candidate-disabled" ||
			match.CandidateRef.CandidateID == "candidate-hidden" ||
			match.CandidateRef.CandidateID == "candidate-readonly" {
			t.Fatalf("non-actionable candidate returned: %#v", match)
		}
	}
}

func TestObservationReaderAppliesLimitAndReportsOmittedMatches(t *testing.T) {
	ctx := context.Background()
	runs := agentservice.NewService(agentservice.NewMemoryRepository())
	run := startObservationRun(t, runs, "limit")
	event := recordObservationEvent(
		t,
		runs,
		run,
		agentservice.EventToolResult,
		observationToolResultFixture,
	)

	result, err := NewObservationReader(runs).Query(
		ctx,
		run.ID,
		ObservationQuery{
			SchemaVersion:  ObservationQueryVersion,
			PlanStepID:     "submit",
			SourceEventSeq: event.Seq,
			Action:         "click",
			Role:           "button",
			Limit:          1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 ||
		result.Matches[0].CandidateRef.CandidateID != "candidate-submit" ||
		result.OmittedCount != 1 {
		t.Fatalf("limited result = %#v", result)
	}
}

func startObservationRun(
	t *testing.T,
	service *agentservice.Service,
	suffix string,
) agentservice.AgentRun {
	t.Helper()
	run, err := service.StartRun(
		context.Background(),
		"conversation-"+suffix,
		"query observation",
	)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func recordObservationEvent(
	t *testing.T,
	service *agentservice.Service,
	run agentservice.AgentRun,
	eventType agentservice.EventType,
	raw string,
) agentservice.Event {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	event, err := service.RecordEvent(context.Background(), run, agentservice.Event{
		Type:    eventType,
		Payload: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

const observationToolResultFixture = `{
	"schema_version":"agent.tool_result.v1",
	"tool":"explore_page",
	"content":{
		"probe_id":"probe-current",
		"url":"https://example.test/form",
		"element_count":7,
		"observation_v2":{
			"schema_version":"browser.observation.v2",
			"probe_id":"probe-current",
			"observation_id":"obs-current",
			"page_state":{
				"state_id":"form",
				"revision":1,
				"url":"https://example.test/form",
				"title":"Form",
				"state_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"previous_state_sha256":null
			},
			"elements":[
				{
					"element_ref":"form:7",
					"context_path":{"frames":[],"shadow_hosts":[]},
					"a11y":{"role":"button","name":"","states":{"focusable":true,"disabled":false},"relations":{}},
					"dom":{"backend_node_id":7,"tag":"button","attrs":{"id":"submit_search","type":"button"},"text":""},
					"runtime":{"connected":true,"visible":true,"enabled":true,"editable":false},
					"locators":[{
						"candidate_id":"candidate-submit",
						"locator":{"kind":"css","value":"#submit_search","exact":true},
						"provenance":"a11y_backend_dom_node",
						"observed_count":1
					}]
				},
				{
					"element_ref":"form:8",
					"context_path":{"frames":[],"shadow_hosts":[]},
					"a11y":{"role":"button","name":"Secondary","states":{"focusable":true,"disabled":false},"relations":{}},
					"dom":{"backend_node_id":8,"tag":"button","attrs":{"id":"secondary"},"text":"Secondary"},
					"runtime":{"connected":true,"visible":true,"enabled":true,"editable":false},
					"locators":[{
						"candidate_id":"candidate-secondary",
						"locator":{"kind":"role","role":"button","name":"Secondary","exact":true},
						"provenance":"a11y_exact",
						"observed_count":1
					}]
				},
				{
					"element_ref":"form:9",
					"context_path":{"frames":[],"shadow_hosts":[]},
					"a11y":{"role":"button","name":"Repeated","states":{"focusable":true,"disabled":false},"relations":{}},
					"dom":{"backend_node_id":9,"tag":"button","attrs":{"id":"repeated"},"text":"Repeated"},
					"runtime":{"connected":true,"visible":true,"enabled":true,"editable":false},
					"locators":[{
						"candidate_id":"candidate-count-two",
						"locator":{"kind":"css","value":"#repeated","exact":true},
						"provenance":"a11y_backend_dom_node",
						"observed_count":2
					}]
				},
				{
					"element_ref":"form:10",
					"context_path":{"frames":[],"shadow_hosts":[]},
					"a11y":{"role":"button","name":"Disabled","states":{"focusable":true,"disabled":true},"relations":{}},
					"dom":{"backend_node_id":10,"tag":"button","attrs":{"id":"disabled"},"text":"Disabled"},
					"runtime":{"connected":true,"visible":true,"enabled":false,"editable":false},
					"locators":[{
						"candidate_id":"candidate-disabled",
						"locator":{"kind":"css","value":"#disabled","exact":true},
						"provenance":"a11y_backend_dom_node",
						"observed_count":1
					}]
				},
				{
					"element_ref":"form:11",
					"context_path":{"frames":[],"shadow_hosts":[]},
					"a11y":{"role":"button","name":"Hidden","states":{"focusable":true,"disabled":false},"relations":{}},
					"dom":{"backend_node_id":11,"tag":"button","attrs":{"id":"hidden"},"text":"Hidden"},
					"runtime":{"connected":true,"visible":false,"enabled":true,"editable":false},
					"locators":[{
						"candidate_id":"candidate-hidden",
						"locator":{"kind":"css","value":"#hidden","exact":true},
						"provenance":"a11y_backend_dom_node",
						"observed_count":1
					}]
				},
				{
					"element_ref":"form:12",
					"context_path":{"frames":[],"shadow_hosts":[]},
					"a11y":{"role":"textbox","name":"Readonly","states":{"focusable":true,"disabled":false},"relations":{}},
					"dom":{"backend_node_id":12,"tag":"input","attrs":{"id":"readonly"},"text":""},
					"runtime":{"connected":true,"visible":true,"enabled":true,"editable":false},
					"locators":[{
						"candidate_id":"candidate-readonly",
						"locator":{"kind":"css","value":"#readonly","exact":true},
						"provenance":"a11y_backend_dom_node",
						"observed_count":1
					}]
				},
				{
					"element_ref":"form:13",
					"context_path":{"frames":[],"shadow_hosts":[]},
					"a11y":{"role":"textbox","name":"Email","states":{"focusable":true,"disabled":false},"relations":{}},
					"dom":{"backend_node_id":13,"tag":"input","attrs":{"id":"email"},"text":""},
					"runtime":{"connected":true,"visible":true,"enabled":true,"editable":true},
					"locators":[{
						"candidate_id":"candidate-input",
						"locator":{"kind":"css","value":"#email","exact":true},
						"provenance":"a11y_backend_dom_node",
						"observed_count":1
					}]
				},
				{
					"element_ref":"form:14",
					"context_path":{"frames":[],"shadow_hosts":[]},
					"a11y":{"role":"link","name":"","states":{"focusable":true,"disabled":false},"relations":{}},
					"dom":{"backend_node_id":14,"tag":"a","attrs":{"id":"details"},"text":""},
					"runtime":{"connected":true,"visible":true,"enabled":true,"editable":false},
					"locators":[{
						"candidate_id":"candidate-scoped",
						"locator":{
							"kind":"scoped",
							"scope":{"kind":"role","role":"region","name":"Products","exact":true},
							"target":{"kind":"text","value":"deep-control","exact":true}
						},
						"provenance":"a11y_scoped",
						"observed_count":1
					}]
				}
			],
			"relations":[]
		}
	},
	"content_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	"content_bytes":4096
}`
