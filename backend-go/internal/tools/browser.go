package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/browsercontract"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
)

type BrowserCapabilityClient interface {
	ExecuteBrowserCapability(
		ctx context.Context,
		capability string,
		actorUserID int64,
		projectID int64,
		conversationID string,
		arguments json.RawMessage,
	) (json.RawMessage, error)
}

type CandidateResolver interface {
	ResolveCandidate(
		context.Context,
		string,
		string,
		browsercontract.CandidateRef,
	) (browsercontract.TrustedResolvedCandidate, error)
}

type BrowserTool struct {
	name        string
	description string
	inputSchema json.RawMessage
	client      BrowserCapabilityClient
	candidates  CandidateResolver
	taskPlans   *taskplan.Service
}

func NewBrowserTools(
	client BrowserCapabilityClient,
	candidates CandidateResolver,
	taskPlans *taskplan.Service,
) []Handler {
	return []Handler{
		BrowserTool{
			name: "explore_page",
			description: "Open one known URL and return its accessibility elements and candidate links. " +
				"Use this first when the user has only provided one entry URL. " +
				"Bind the probe to the next pending task plan steps with plan_step_ids. " +
				"Each result reports per-plan and run-wide exploration_budget counters. " +
				"Do not repeat the same URL and plan_step_ids signature.",
			inputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"url":{"type":"string","description":"Absolute page URL"},
					"core_user_flow_text":{"type":"string","description":"User flow used to prioritize relevant elements"},
						"observation_schema_version":{"type":"string","enum":["v1","v2"],"default":"v2"},
					"plan_step_ids":{"type":"array","minItems":1,"items":{"type":"string"}}
				},
				"required":["url","plan_step_ids"]
			}`),
			client: client,
		},
		BrowserTool{
			name: "explore_flow",
			description: "Explore multiple page states in one browser session. " +
				"Use discovered links and a bounded sequence of click, input, and wait_for actions. " +
				"Use grounding.query.v2 candidate_ref from the current-run explore summary; the control plane resolves it from current-run evidence before the Worker call. " +
				"For search workflows, explore the actual input and search-control click; do not jump directly to a constructed search URL. " +
				"Each call runs in an isolated disposable probe context, so its browser and business state are never reused by another call or by official execution. " +
				"Express intended multiplicity such as quantity 2 in the flow actions and final DSL, not by relying on state accumulated across calls. " +
				"Each result reports per-plan and run-wide exploration_budget counters. " +
				"Do not repeat a completed flow signature; change its page, actions, or bound plan steps. " +
				"Every click and input must reference its TaskPlan step with action.plan_step_id; a supporting wait_for observation may omit it. " +
				"Prefer a structured semantic locator for click, input, and wait_for so the Browser Worker can compile it with Playwright, require one runtime match, and return resolved target evidence. " +
				"Use a scoped semantic locator to disambiguate repeated controls; do not submit CSS or XPath on this path. " +
				"For a control value check, use wait_for with a semantic locator and condition value_equals instead of treating its label as text.",
			inputSchema: json.RawMessage(`{
				"type":"object",
				"additionalProperties":false,
				"properties":{
					"schema_version":{"type":"string","enum":["grounding.query.v1","grounding.query.v2"],"default":"grounding.query.v2"},
					"base_url":{"type":"string"},
					"flow_description":{"type":"string"},
					"observation_schema_version":{"type":"string","enum":["v1","v2"],"default":"v2"},
					"plan_step_ids":{"type":"array","minItems":1,"items":{"type":"string"}},
					"steps":{
						"type":"array",
						"minItems":1,
						"items":{}
					}
				},
				"required":["steps","plan_step_ids"],
				"allOf":[{
					"if":{
						"properties":{"schema_version":{"const":"grounding.query.v2"}},
						"required":["schema_version"]
					},
					"then":{"properties":{"steps":{"items":{"$ref":"#/$defs/step_v2"}}}},
					"else":{"properties":{"steps":{"items":{"$ref":"#/$defs/step_v1"}}}}
				}],
				"$defs":{
					"semantic_leaf":{
						"oneOf":[
							{"type":"object","properties":{"kind":{"const":"role"},"role":{"type":"string","minLength":1},"name":{"type":["string","null"]},"exact":{"type":"boolean","default":true}},"required":["kind","role"],"additionalProperties":false},
							{"type":"object","properties":{"kind":{"enum":["label","placeholder","text","test_id"]},"value":{"type":"string","minLength":1},"exact":{"type":"boolean","default":true}},"required":["kind","value"],"additionalProperties":false}
						]
					},
					"semantic_locator":{
						"oneOf":[
							{"$ref":"#/$defs/semantic_leaf"},
							{"type":"object","properties":{"kind":{"const":"scoped"},"scope":{"$ref":"#/$defs/semantic_leaf"},"target":{"$ref":"#/$defs/semantic_leaf"}},"required":["kind","scope","target"],"additionalProperties":false}
						]
					},
					"candidate_ref":{
						"type":"object",
						"properties":{
							"schema_version":{"const":"grounding.candidate-ref.v1"},
							"source_event_seq":{"type":"integer","minimum":1},
							"probe_id":{"type":"string","minLength":1},
							"observation_id":{"type":"string","minLength":1},
							"candidate_id":{"type":"string","minLength":1}
						},
						"required":["schema_version","source_event_seq","probe_id","observation_id","candidate_id"],
						"additionalProperties":false
					},
					"condition":{
						"type":"object",
						"properties":{
							"type":{"type":"string","enum":["visible","value_equals"]},
							"expected":{"type":"string"}
						},
						"required":["type"],
						"allOf":[{"if":{"properties":{"type":{"const":"value_equals"}},"required":["type"]},"then":{"required":["expected"]}}],
						"additionalProperties":false
					},
					"action":{
						"type":"object",
						"properties":{
							"action":{"type":"string","enum":["click","input","wait_for"]},
							"plan_step_id":{"type":"string","minLength":1,"maxLength":64},
							"locator":{"$ref":"#/$defs/semantic_locator"},
							"candidate_ref":{"$ref":"#/$defs/candidate_ref"},
							"condition":{"$ref":"#/$defs/condition"},
							"value":{"type":"string"},
							"timeout_ms":{"type":"integer","minimum":1,"maximum":60000}
						},
						"required":["action"],
						"allOf":[
							{"if":{"properties":{"action":{"enum":["click","input"]}},"required":["action"]},"then":{"required":["plan_step_id"]}}
						],
						"additionalProperties":false
					},
					"action_v1":{
						"allOf":[
							{"$ref":"#/$defs/action"},
							{"required":["locator"],"not":{"required":["candidate_ref"]}}
						]
					},
					"action_v2":{
						"allOf":[
							{"$ref":"#/$defs/action"},
							{"oneOf":[
								{"required":["locator"],"not":{"required":["candidate_ref"]}},
								{"required":["candidate_ref","plan_step_id"],"not":{"required":["locator"]}}
							]}
						]
					},
					"step_v1":{
						"type":"object",
						"properties":{
							"url":{"type":"string"},
							"description":{"type":"string"},
							"actions":{"type":"array","minItems":0,"items":{"$ref":"#/$defs/action_v1"}}
						},
						"additionalProperties":false
					},
					"step_v2":{
						"type":"object",
						"properties":{
							"url":{"type":"string"},
							"description":{"type":"string"},
							"actions":{"type":"array","minItems":0,"items":{"$ref":"#/$defs/action_v2"}}
						},
						"additionalProperties":false
					}
				}
			}`),
			client: client, candidates: candidates,
			taskPlans: taskPlans,
		},
		BrowserTool{
			name: "validate_page_elements",
			description: "Advisory check for whether explored accessibility elements cover required user actions and assertions. " +
				"This does not authorize generation; generate_dsl performs bound case validation internally.",
			inputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"required_elements":{
						"type":"array",
						"items":{
							"type":"object",
							"properties":{
								"id":{"type":"string"},
								"description":{"type":"string"},
								"keywords":{"type":"array","items":{"type":"string"}},
								"roles":{"type":"array","items":{"type":"string"}}
							},
							"required":["id","description","keywords"]
						}
					},
					"a11y_nodes":{"type":"array","items":{"type":"object"}}
				},
				"required":["required_elements","a11y_nodes"]
			}`),
			client: client,
		},
	}
}

func (t BrowserTool) Definition() Definition {
	return Definition{
		Name:        t.name,
		Description: t.description,
		InputSchema: t.inputSchema,
	}
}

func (t BrowserTool) Execute(ctx context.Context, call Call) (Result, error) {
	arguments := call.Arguments
	observationVersion := ""
	if t.name == "explore_page" || t.name == "explore_flow" {
		var payload map[string]any
		if err := json.Unmarshal(call.Arguments, &payload); err != nil {
			return Result{}, err
		}
		delete(payload, "plan_step_ids")
		payload["probe_id"] = browserProbeID(call)
		if t.name == "explore_flow" {
			if _, exists := payload["schema_version"]; !exists {
				payload["schema_version"] = browsercontract.GroundingQueryV1
			}
			if payload["schema_version"] == browsercontract.GroundingQueryV2 {
				if err := t.hydrateCandidateReferences(
					ctx,
					call.RunID,
					payload,
				); err != nil {
					return Result{}, err
				}
			}
		}
		if _, exists := payload["observation_schema_version"]; !exists {
			payload["observation_schema_version"] = "v2"
		}
		observationVersion, _ = payload["observation_schema_version"].(string)
		var err error
		arguments, err = json.Marshal(payload)
		if err != nil {
			return Result{}, err
		}
	}
	content, err := t.client.ExecuteBrowserCapability(
		ctx,
		t.name,
		call.ActorUserID,
		call.ProjectID,
		call.ConversationID,
		arguments,
	)
	if err != nil {
		return Result{}, err
	}
	if observationVersion == "v2" {
		content, err = compactV2ExplorationResult(content)
		if err != nil {
			return Result{}, err
		}
	}
	return Result{Content: content}, nil
}

type candidateHydration struct {
	planStepID string
	actionName string
	ref        browsercontract.CandidateRef
	resolved   browsercontract.TrustedResolvedCandidate
	action     map[string]any
}

func (t BrowserTool) hydrateCandidateReferences(
	ctx context.Context,
	runID string,
	payload map[string]any,
) error {
	steps, ok := payload["steps"].([]any)
	if !ok {
		return errors.New("grounding.query.v2 steps are invalid")
	}
	hydrations := make([]candidateHydration, 0)
	for stepIndex, rawStep := range steps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			return fmt.Errorf(
				"grounding.query.v2 step %d is invalid",
				stepIndex,
			)
		}
		rawActions, exists := step["actions"]
		if !exists {
			continue
		}
		actions, ok := rawActions.([]any)
		if !ok {
			return fmt.Errorf(
				"grounding.query.v2 step %d actions are invalid",
				stepIndex,
			)
		}
		for actionIndex, rawAction := range actions {
			action, ok := rawAction.(map[string]any)
			if !ok {
				return fmt.Errorf(
					"grounding.query.v2 action %d:%d is invalid",
					stepIndex,
					actionIndex,
				)
			}
			if _, exists := action["resolved_candidate"]; exists {
				return errors.New(
					"model-supplied resolved_candidate is forbidden",
				)
			}
			locator, hasLocator := action["locator"]
			candidate, hasCandidate := action["candidate_ref"]
			hasLocator = hasLocator && locator != nil
			hasCandidate = hasCandidate && candidate != nil
			if hasLocator == hasCandidate {
				return fmt.Errorf(
					"grounding.query.v2 action %d:%d requires exactly one of locator or candidate_ref",
					stepIndex,
					actionIndex,
				)
			}
			if !hasCandidate {
				continue
			}
			if t.candidates == nil ||
				t.taskPlans == nil {
				return errors.New(
					"candidate hydration dependencies are unavailable",
				)
			}
			planStepID, _ := action["plan_step_id"].(string)
			actionName, _ := action["action"].(string)
			if planStepID == "" || actionName == "" {
				return errors.New(
					"candidate_ref action requires action and plan_step_id",
				)
			}
			ref, err := decodeCandidateRef(candidate)
			if err != nil {
				return fmt.Errorf(
					"decode candidate_ref for plan step %q: %w",
					planStepID,
					err,
				)
			}
			resolved, err := t.candidates.ResolveCandidate(
				ctx,
				runID,
				actionName,
				ref,
			)
			if err != nil {
				return fmt.Errorf(
					"resolve candidate for plan step %q: %w",
					planStepID,
					err,
				)
			}
			if resolved.Source != ref {
				err = errors.New(
					"resolved candidate source does not match candidate_ref",
				)
			} else {
				err = resolved.Validate()
			}
			if err != nil {
				return fmt.Errorf(
					"validate candidate for plan step %q: %w",
					planStepID,
					err,
				)
			}
			hydrations = append(hydrations, candidateHydration{
				planStepID: planStepID,
				actionName: actionName,
				ref:        ref,
				resolved:   resolved,
				action:     action,
			})
		}
	}
	if len(hydrations) == 0 {
		return nil
	}

	authorizations := make(
		[]taskplan.ResolvedCandidateAuthorization,
		len(hydrations),
	)
	seenSteps := make(map[string]browsercontract.CandidateRef)
	for index, hydration := range hydrations {
		authorizations[index] = taskplan.ResolvedCandidateAuthorization{
			PlanStepID: hydration.planStepID,
			Action:     hydration.actionName,
			Candidate:  hydration.resolved,
		}
		if selected, exists := seenSteps[hydration.planStepID]; exists && selected != hydration.ref {
			return fmt.Errorf(
				"plan step %q references multiple candidates",
				hydration.planStepID,
			)
		}
		seenSteps[hydration.planStepID] = hydration.ref
	}
	if err := t.taskPlans.AuthorizeResolvedCandidates(
		ctx,
		runID,
		authorizations,
	); err != nil {
		return err
	}
	for _, hydration := range hydrations {
		delete(hydration.action, "candidate_ref")
		hydration.action["resolved_candidate"] = hydration.resolved
	}
	return nil
}

func decodeCandidateRef(value any) (browsercontract.CandidateRef, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return browsercontract.CandidateRef{}, err
	}
	var ref browsercontract.CandidateRef
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ref); err != nil {
		return browsercontract.CandidateRef{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return browsercontract.CandidateRef{}, errors.New(
				"candidate_ref contains multiple JSON values",
			)
		}
		return browsercontract.CandidateRef{}, err
	}
	if err := ref.Validate(); err != nil {
		return browsercontract.CandidateRef{}, err
	}
	return ref, nil
}

func browserProbeID(call Call) string {
	sum := sha256.Sum256([]byte(
		call.RunID + "\x00" + call.ToolCallID + "\x00" + call.Name,
	))
	return "probe_" + hex.EncodeToString(sum[:12])
}

func compactV2ExplorationResult(raw json.RawMessage) (json.RawMessage, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if _, exists := result["observation_v2"]; exists {
		delete(result, "a11y_nodes")
	}
	if pages, ok := result["pages"].([]any); ok {
		for _, rawPage := range pages {
			page, _ := rawPage.(map[string]any)
			if page == nil {
				continue
			}
			if _, exists := page["observation_v2"]; exists {
				delete(page, "a11y_nodes")
			}
		}
	}
	return json.Marshal(result)
}
