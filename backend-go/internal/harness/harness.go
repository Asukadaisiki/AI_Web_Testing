package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/taskplan"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/tools"
)

const taskPlanningPrompt = `PHASE 1 - TASK PLANNING
Understand the user's goal and call set_task_plan before browser exploration.
Create a complete ordered business workflow using only goto, click, input, wait_for,
assert_text, assert_url_contains, and capture_text. Each PlanStep must describe an
observable semantic target or page fact, expected value or transition, occurrence count,
idempotency, and side-effect boundary.
TaskPlan describes what must happen and what evidence proves completion. It must not
invent CSS, XPath, DOM node IDs, candidate IDs, or accessibility facts that have not
been observed. The persisted TaskPlan owns action semantics, order, occurrence counts,
forbidden actions, idempotency, and side-effect boundaries.
Never change task semantics during exploration. To revise semantics, call set_task_plan
again to create a new plan version.`

const groundingPrompt = `PHASE 2 - GROUNDING
Use explore_page to obtain the first BrowserObservation, then use explore_flow for later
page states. Analyze the returned accessibility facts, DOM facts, runtime state,
relations, candidate coverage, and action evidence before choosing the next action.
Every explore_flow action must use a structured semantic LocatorSpec using role,
accessible name, label, placeholder, text, test ID, or a semantic scoped locator. The
Browser Worker compiles that LocatorSpec with Playwright and requires one actionable
runtime match. Do not author CSS or XPath on the new grounding path.
Every explore_page or explore_flow call must include plan_step_ids for the next contiguous pending steps.
For explore_flow, set action.plan_step_id on every action intended to ground a pending PlanStep. A click or input that replays an already grounded prerequisite must keep that original plan_step_id and is treated as a supporting action. Supporting wait_for observations may omit plan_step_id. Never rely on action order or target text to infer ownership.
Never execute external_state or unknown side effects during exploration. Such steps may only be grounded by observing their controls or expected facts without triggering them.
Use ask_user_question only when required information or explicit approval is missing.
Tool results shown to you use agent.model_tool_summary.v1. For exploration, first read observation.page_states, observation.element_groups, observation.candidate_coverage, observation.action_options, and observation.verification_facts to understand the page; use pages[].a11y_nodes as the exact evidence submitted in generate_dsl.a11y_nodes_by_state. source.event_seq and hashes reference the complete persisted tool.result event.
After every tool result, reason from the returned facts before selecting another tool. Do not
repeat or revise the TaskPlan to work around a locator failure. Keep the business plan stable,
refine only the next GroundingQuery, and generate DSL only after the persisted plan reports
ready_for_generation.
Never invent omitted nodes or selectors. Re-explore when the retained evidence is insufficient.
Each explore_flow call runs in an isolated disposable probe context; its state is not reused by later probes or official execution. Express intended multiplicity such as quantity 2 inside one probe and in the final DSL, never by relying on state accumulated across calls. Prefer one self-contained flow that captures all downstream evidence.
	Exploration is budgeted per TaskPlan version and by a separate run-wide hard limit. Read task_plan.exploration_budget after every probe, including remaining counters and the current signature allowance, before choosing another exploration call.
	Completed duplicate explore_page and explore_flow signatures are rejected within the same plan version. Do not evade this protection by changing descriptions or timeouts; change the page, actions, or plan_step_ids only when the evidence need is genuinely different.
	For wait_for checks on a control value, use a semantic locator plus condition {"type":"value_equals","expected":"..."}; do not treat the control's visible label as its value.
	Persisted PlanStep status is the only authority for grounding completeness.
You may call validate_page_elements with required_elements to find exploration gaps, but that advisory result does not authorize generation.
`

const dslAuthoringPrompt = `PHASE 3 - DSL AUTHORING
As soon as every PlanStep is grounded, call generate_dsl with the exact plan binding and
step binding IDs returned in the latest tool summary.
Author a research-v2 Draft DSL from the TaskPlan and verified BrowserObservation facts.
Each step must include plan_step_id; click, input, and capture_text steps must include
target_binding_id. Do not copy A11y nodes or author selectors/candidates.
	The control plane compiles the Draft DSL into an immutable Executable DSL and preserves every TaskPlan step's intent, action, value, idempotency, side_effect, order, and expected occurrence count exactly.
	DSL steps may only use goto, click, input, wait_for, assert_text, assert_url_contains, and capture_text. Use wait_for or postconditions for visibility checks; assert_visible is not supported.
	goto and assert_url_contains store their URL in value. assert_text requires both target and expected value. input requires target and value. capture_text requires target and context_key.
	Only make wait_for, assert_text, or capture_text standalone DSL steps when their target exists in pages[].a11y_nodes for the same page_state. If a text was observed only as a successful explore_flow wait_for action or must be checked at runtime, express it as a text_visible postcondition on the preceding grounded step instead of as a locator-bearing step target.
	Every click on an anchor that should move to another page must declare a url_contains postcondition whose value identifies the concrete destination URL or path. url_changes alone is insufficient because hash or interstitial navigation is not the intended destination.
	Never use custom locator strings such as role="name" or inside "scope" in research-v2. LocatorSpec and candidates are compiler-owned.
	When the user asks to search through page controls, the final DSL must contain the real input step followed by the real search-control click. Never replace those actions with goto to a constructed search-result URL.
	Input trigger is optional and only accepts Enter or Tab. Omit trigger for ordinary semantic input, and represent a search-button action as a separate click step.
	Do not include candidates, match_count, or locator_confidence in generate_dsl.case; locator preflight derives those fields from a11y_nodes_by_state.
After generate_dsl, use ask_user_question with a required confirm question whose id is approve_dsl.
Never call execute_dsl until that approval tool result is true for the latest generation.
`

const executionRepairPrompt = `PHASE 4 - EXECUTION AND REPAIR
When execute_dsl returns a batch_id, use get_report to read its current result.
The get_report tool waits for a terminal result by default; call it once instead of polling repeatedly.
For a failed batch, inspect report.failure_signals and then call fix_and_retry first. Follow repair.strategy: re_explore means gather fresh evidence, regenerate_dsl means revise the case, wait_execution means wait/read later, manual_reconcile means stop for human review. Never replay an action when repair.original_action_replay_allowed is false. Never skip DSL validation or approval during repair.
Never claim that a tool ran unless its result is present.
Never invent page elements, execution results, or report data.
When the task is complete, answer concisely in the user's language.`

const defaultSystemPrompt = taskPlanningPrompt + "\n\n" +
	groundingPrompt + "\n\n" +
	dslAuthoringPrompt + "\n\n" +
	executionRepairPrompt

type Harness struct {
	runs   *agentservice.Service
	loop   *agent.Loop
	tools  *tools.Registry
	policy ToolPolicy
	plans  *taskplan.Service

	activeMu   sync.Mutex
	activeRuns map[string]*activeRun
}

type activeRun struct {
	cancel context.CancelFunc
}

func New(runs *agentservice.Service, model agent.Model, registry *tools.Registry, maxSteps int) *Harness {
	return newHarness(runs, model, registry, nil, maxSteps)
}

func NewWithTaskPlans(
	runs *agentservice.Service,
	model agent.Model,
	registry *tools.Registry,
	plans *taskplan.Service,
	maxSteps int,
) *Harness {
	return newHarness(runs, model, registry, plans, maxSteps)
}

func newHarness(
	runs *agentservice.Service,
	model agent.Model,
	registry *tools.Registry,
	plans *taskplan.Service,
	maxSteps int,
) *Harness {
	return &Harness{
		runs:       runs,
		loop:       agent.NewLoop(model, toolDefinitions(registry), defaultSystemPrompt, maxSteps),
		tools:      registry,
		policy:     DefaultToolPolicy{},
		plans:      plans,
		activeRuns: make(map[string]*activeRun),
	}
}

func (e *Harness) Start(ctx context.Context, conversationID string, input string) (agentservice.AgentRun, error) {
	run, err := e.runs.StartProjectRun(ctx, conversationID, 0, input)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	return e.Continue(ctx, run.ID)
}

func (e *Harness) StartAsync(conversationID string, input string) (agentservice.AgentRun, error) {
	return e.StartProjectAsync(conversationID, 0, input)
}

func (e *Harness) StartProjectAsync(
	conversationID string,
	projectID int64,
	input string,
) (agentservice.AgentRun, error) {
	return e.StartOwnedProjectAsync(context.Background(), 0, conversationID, projectID, input)
}

func (e *Harness) StartOwnedProjectAsync(
	ctx context.Context,
	actorUserID int64,
	conversationID string,
	projectID int64,
	input string,
) (agentservice.AgentRun, error) {
	runContext := context.WithoutCancel(ctx)
	run, err := e.runs.StartOwnedProjectRun(
		runContext,
		actorUserID,
		conversationID,
		projectID,
		input,
	)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	go func() {
		_, _ = e.Continue(runContext, run.ID)
	}()
	return run, nil
}

func (e *Harness) Continue(ctx context.Context, runID string) (agentservice.AgentRun, error) {
	runContext, cancel := context.WithCancel(ctx)
	active := &activeRun{cancel: cancel}
	e.activeMu.Lock()
	previous := e.activeRuns[runID]
	e.activeRuns[runID] = active
	e.activeMu.Unlock()
	if previous != nil {
		previous.cancel()
	}
	defer func() {
		e.activeMu.Lock()
		if e.activeRuns[runID] == active {
			delete(e.activeRuns, runID)
		}
		e.activeMu.Unlock()
		cancel()
	}()

	return e.continueRun(runContext, runID)
}

func (e *Harness) continueRun(ctx context.Context, runID string) (agentservice.AgentRun, error) {
	run, err := e.runs.GetRun(ctx, runID)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	if run.Status != agentservice.RunStatusRunning {
		return run, nil
	}

	loopErr := e.loop.RunWithModelContext(
		ctx,
		&run.Transcript,
		func(callContext context.Context) context.Context {
			logicalCallID := e.runs.NewID("llm")
			stepID := e.runs.NewID("step")
			state, _, _ := e.currentPipelineState(callContext, run)
			return agent.WithTelemetryRecorder(
				callContext,
				func(recordContext context.Context, record agent.TelemetryRecord) error {
					return e.runs.RecordModelTelemetry(recordContext, run, record)
				},
				logicalCallID,
				stepID,
				state,
			)
		},
		func(ctx context.Context, response agent.ModelResponse) (bool, error) {
			if strings.TrimSpace(response.Content) != "" {
				if err := e.recordMessage(ctx, run, response.Content); err != nil {
					return false, err
				}
			}
			if len(response.ToolCalls) == 0 {
				if e.plans != nil {
					plan, planErr := e.plans.GetCurrent(ctx, run.ID)
					if planErr != nil {
						return false, fmt.Errorf(
							"agent cannot complete without a task plan: %w",
							planErr,
						)
					}
					if plan.Status != taskplan.StatusCompleted {
						return false, fmt.Errorf(
							"agent cannot complete while task plan status is %q",
							plan.Status,
						)
					}
				}
				if err := e.runs.SaveRun(ctx, run); err != nil {
					return false, err
				}
				run, err = e.runs.CompleteRun(ctx, run)
				return false, err
			}

			for _, call := range response.ToolCalls {
				stepID := e.runs.NewID("step")
				_, tracePlan, traceErr := e.currentPipelineState(ctx, run)
				if traceErr != nil {
					return false, traceErr
				}
				traceIdentity, traceErr := e.newToolTraceIdentity(
					ctx,
					run,
					call,
					tracePlan,
				)
				if traceErr != nil {
					return false, traceErr
				}
				if traceErr := e.recordPipelineToolTrace(
					ctx,
					run,
					stepID,
					call,
					traceIdentity,
					"proposed",
					"",
				); traceErr != nil {
					return false, traceErr
				}
				if err := e.recordToolStart(ctx, run, stepID, call); err != nil {
					return false, err
				}
				var policyPlan *taskplan.Plan
				if e.plans != nil {
					if err := e.plans.Authorize(
						ctx,
						run.ID,
						call.Name,
						json.RawMessage(call.Arguments),
					); err != nil {
						if traceErr := e.recordPipelineToolTrace(
							ctx,
							run,
							stepID,
							call,
							traceIdentity,
							"rejected",
							"task_plan_authorization_failed",
						); traceErr != nil {
							return false, traceErr
						}
						if recordErr := e.recordRecoverableToolFailure(
							ctx,
							&run,
							stepID,
							call,
							err,
						); recordErr != nil {
							return false, recordErr
						}
						continue
					}
					if agent.IsExplorationTool(call.Name) {
						currentPlan, planErr := e.plans.GetCurrent(ctx, run.ID)
						if planErr != nil {
							return false, planErr
						}
						policyPlan = &currentPlan
					}
				}
				if err := e.policy.BeforeToolCall(run, call, policyPlan); err != nil {
					if traceErr := e.recordPipelineToolTrace(
						ctx,
						run,
						stepID,
						call,
						traceIdentity,
						"rejected",
						pipelineReasonCode(err, "tool_policy_rejected"),
					); traceErr != nil {
						return false, traceErr
					}
					if recordErr := e.recordRecoverableToolFailure(ctx, &run, stepID, call, err); recordErr != nil {
						return false, recordErr
					}
					continue
				}
				if traceErr := e.recordPipelineToolTrace(
					ctx,
					run,
					stepID,
					call,
					traceIdentity,
					"authorized",
					"",
				); traceErr != nil {
					return false, traceErr
				}
				if traceErr := e.recordPipelineToolTrace(
					ctx,
					run,
					stepID,
					call,
					traceIdentity,
					"running",
					"",
				); traceErr != nil {
					return false, traceErr
				}
				result, executeErr := e.tools.Execute(ctx, tools.Call{
					RunID:                run.ID,
					RunInput:             run.Input,
					ActorUserID:          run.ActorUserID,
					ConversationID:       run.ConversationID,
					ProjectID:            run.ProjectID,
					LatestGenerationID:   run.LatestGenerationID,
					ApprovedGenerationID: run.ApprovedGenerationID,
					ToolCallID:           call.ID,
					Name:                 call.Name,
					Arguments:            json.RawMessage(call.Arguments),
				})
				if executeErr != nil {
					if errors.Is(executeErr, context.Canceled) {
						return false, executeErr
					}
					if traceErr := e.recordPipelineToolTrace(
						ctx,
						run,
						stepID,
						call,
						traceIdentity,
						"failed",
						"tool_execution_failed",
					); traceErr != nil {
						return false, traceErr
					}
					if e.plans != nil {
						if planErr := e.plans.RecordToolFailure(
							ctx,
							run.ID,
							call.Name,
							executeErr.Error(),
						); planErr != nil {
							return false, fmt.Errorf(
								"record task plan failure after %s: %w",
								call.Name,
								planErr,
							)
						}
						if planErr := e.recordTaskPlanState(
							ctx,
							run,
							stepID,
							call.ID,
						); planErr != nil {
							return false, planErr
						}
					}
					if recordErr := e.recordRecoverableToolFailure(ctx, &run, stepID, call, executeErr); recordErr != nil {
						return false, recordErr
					}
					continue
				}
				if result.Pending != nil {
					if traceErr := e.recordPipelineToolTrace(
						ctx,
						run,
						stepID,
						call,
						traceIdentity,
						"pending",
						"user_input_required",
					); traceErr != nil {
						return false, traceErr
					}
					var request agentservice.AskUserRequest
					if err := json.Unmarshal(result.Pending.Payload, &request); err != nil {
						return false, err
					}
					if err := e.runs.SaveRun(ctx, run); err != nil {
						return false, err
					}
					run, _, err = e.runs.RequestUserInputForCall(
						ctx,
						run.ID,
						call.ID,
						stepID,
						request,
					)
					return false, err
				}
				if result.Artifact != nil && result.Artifact.Type == "dsl_generation" {
					generationID, parseErr := strconv.ParseInt(result.Artifact.ID, 10, 64)
					if parseErr != nil {
						_ = e.recordToolFailure(ctx, run, stepID, call, parseErr)
						return false, parseErr
					}
					run.LatestGenerationID = &generationID
					run.ApprovedGenerationID = nil
				}
				if result.Artifact != nil &&
					result.Artifact.Type == "task_plan" {
					run.LatestGenerationID = nil
					run.ApprovedGenerationID = nil
				}
				sourceEventSeq, err := e.recordToolResult(ctx, run, stepID, call, result)
				if err != nil {
					return false, err
				}
				if e.plans != nil {
					if err := e.plans.RecordToolResult(
						ctx,
						run.ID,
						call.Name,
						json.RawMessage(call.Arguments),
						result.Content,
						sourceEventSeq,
					); err != nil {
						return false, fmt.Errorf(
							"advance task plan after %s: %w",
							call.Name,
							err,
						)
					}
					if err := e.recordTaskPlanState(
						ctx,
						run,
						stepID,
						call.ID,
					); err != nil {
						return false, err
					}
				}
				var taskPlanSummary *agent.ToolResultTaskPlanSummary
				if e.plans != nil &&
					(agent.IsExplorationTool(call.Name) ||
						call.Name == "set_task_plan") {
					currentPlan, planErr := e.plans.GetCurrent(ctx, run.ID)
					if planErr != nil {
						return false, planErr
					}
					taskPlanSummary = modelTaskPlanSummary(currentPlan)
					if provider, ok := e.policy.(explorationBudgetProvider); ok {
						var completedCall *agent.ModelTool
						if agent.IsExplorationTool(call.Name) {
							completedCall = &call
						}
						taskPlanSummary.ExplorationBudget =
							provider.ExplorationBudget(
								run,
								&currentPlan,
								completedCall,
							)
					}
				}
				modelContent, err := agent.BuildModelToolSummary(
					call.Name,
					result.Content,
					sourceEventSeq,
					taskPlanSummary,
				)
				if err != nil {
					return false, fmt.Errorf("summarize tool result: %w", err)
				}
				run.Transcript = append(run.Transcript, agent.Message{
					Role:       "tool",
					Content:    modelContent,
					ToolCallID: call.ID,
				})
				run.Transcript = agent.CompactExplorationTranscript(run.Transcript)
				if err := e.runs.SaveRun(ctx, run); err != nil {
					return false, err
				}
				traceIdentity.lineage, traceErr = e.pipelineLineage(
					ctx,
					run,
					call.Name,
					result.Content,
					traceIdentity.planStepIDs,
				)
				if traceErr != nil {
					return false, traceErr
				}
				if traceErr := e.recordPipelineToolTrace(
					ctx,
					run,
					stepID,
					call,
					traceIdentity,
					"succeeded",
					"",
				); traceErr != nil {
					return false, traceErr
				}
			}
			return true, nil
		},
	)
	if loopErr != nil {
		current, getErr := e.runs.GetRun(context.WithoutCancel(ctx), run.ID)
		if getErr == nil && current.Status == agentservice.RunStatusCancelled {
			return current, nil
		}
		if errors.Is(loopErr, context.Canceled) ||
			errors.Is(loopErr, agentservice.ErrRunCancelled) {
			if getErr == nil {
				return current, loopErr
			}
			return run, loopErr
		}
		if e.plans != nil {
			_ = e.plans.MarkFailed(context.WithoutCancel(ctx), run.ID)
			_ = e.recordTaskPlanState(
				context.WithoutCancel(ctx),
				run,
				"",
				"",
			)
		}
		failedRun, _ := e.runs.FailRun(ctx, run, loopErr)
		return failedRun, loopErr
	}
	return run, nil
}

func modelTaskPlanSummary(plan taskplan.Plan) *agent.ToolResultTaskPlanSummary {
	summary := &agent.ToolResultTaskPlanSummary{
		PlanID: plan.ID, Version: plan.Version,
		PlanSHA256: plan.PlanSHA256, Status: string(plan.Status),
		StepBindings: make(map[string]string),
		StepLineage:  make(map[string]agent.ToolResultLineageSummary),
	}
	for _, step := range plan.Steps {
		summary.StepIDs = append(summary.StepIDs, step.ID)
		if step.Status == taskplan.StepGrounded {
			summary.GroundedStepIDs = append(summary.GroundedStepIDs, step.ID)
		} else {
			summary.PendingStepIDs = append(summary.PendingStepIDs, step.ID)
		}
		if step.TargetBinding != nil {
			summary.StepBindings[step.ID] = step.TargetBinding.BindingID
			summary.StepLineage[step.ID] = agent.ToolResultLineageSummary{
				ProbeID:             step.TargetBinding.ProbeID,
				ObservationID:       step.TargetBinding.ObservationID,
				ObservationSHA256:   step.TargetBinding.ObservationSHA256,
				PageStateID:         step.TargetBinding.PageStateID,
				TargetBindingID:     step.TargetBinding.BindingID,
				SelectedCandidateID: step.TargetBinding.SelectedCandidateID,
				ElementRefs: append(
					[]string(nil),
					step.TargetBinding.ElementRefs...,
				),
			}
		}
	}
	if len(summary.StepBindings) == 0 {
		summary.StepBindings = nil
		summary.StepLineage = nil
	}
	return summary
}

func (e *Harness) Resume(
	ctx context.Context,
	runID string,
	toolCallID string,
	request agentservice.ResumeToolCallRequest,
) (agentservice.AgentRun, error) {
	pendingRun, err := e.runs.GetRun(ctx, runID)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	pendingCall, pendingTrace, hasPendingTrace, err :=
		e.pendingToolTraceIdentity(ctx, pendingRun, toolCallID)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	run, err := e.runs.ResumeToolCall(ctx, runID, toolCallID, request)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	result, err := json.Marshal(request)
	if err != nil {
		return agentservice.AgentRun{}, fmt.Errorf("encode tool resume result: %w", err)
	}
	run.Transcript = append(run.Transcript, agent.Message{
		Role:       "tool",
		Content:    string(result),
		ToolCallID: toolCallID,
	})
	if approved, ok := request.Answers["approve_dsl"].(bool); ok && approved {
		if e.plans != nil {
			if run.LatestGenerationID == nil {
				return agentservice.AgentRun{}, errors.New(
					"cannot approve without a DSL generation",
				)
			}
			if err := e.plans.Approve(
				ctx,
				run.ID,
				*run.LatestGenerationID,
			); err != nil {
				return agentservice.AgentRun{}, err
			}
			if err := e.recordTaskPlanState(
				ctx,
				run,
				"",
				toolCallID,
			); err != nil {
				return agentservice.AgentRun{}, err
			}
		}
		run.ApprovedGenerationID = run.LatestGenerationID
	}
	if err := e.runs.SaveRun(ctx, run); err != nil {
		return agentservice.AgentRun{}, err
	}
	if hasPendingTrace {
		if err := e.recordPipelineToolTrace(
			ctx,
			run,
			"",
			pendingCall,
			pendingTrace,
			"succeeded",
			"user_input_received",
		); err != nil {
			return agentservice.AgentRun{}, err
		}
	}
	return e.Continue(ctx, run.ID)
}

func (e *Harness) recordTaskPlanState(
	ctx context.Context,
	run agentservice.AgentRun,
	stepID string,
	toolCallID string,
) error {
	plan, err := e.plans.GetCurrent(ctx, run.ID)
	if err != nil {
		if errors.Is(err, taskplan.ErrNotFound) {
			return nil
		}
		return err
	}
	steps := make([]map[string]any, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		snapshot := map[string]any{
			"id":                 step.ID,
			"position":           step.Position,
			"status":             step.Status,
			"grounding_attempts": step.GroundingAttempts,
			"evidence_count":     len(step.Evidence),
		}
		if step.TargetBinding != nil {
			snapshot["target_binding_id"] = step.TargetBinding.BindingID
			snapshot["probe_id"] = step.TargetBinding.ProbeID
			snapshot["observation_id"] = step.TargetBinding.ObservationID
			snapshot["observation_sha256"] =
				step.TargetBinding.ObservationSHA256
			snapshot["page_state_id"] = step.TargetBinding.PageStateID
			snapshot["selected_candidate_id"] =
				step.TargetBinding.SelectedCandidateID
			snapshot["element_refs"] =
				append([]string(nil), step.TargetBinding.ElementRefs...)
		}
		steps = append(steps, snapshot)
	}
	_, err = e.runs.RecordEvent(ctx, run, agentservice.Event{
		Type:       agentservice.EventTaskPlanUpdated,
		StepID:     stepID,
		ToolCallID: toolCallID,
		Payload: map[string]any{
			"schema_version":      plan.SchemaVersion,
			"plan_id":             plan.ID,
			"version":             plan.Version,
			"plan_sha256":         plan.PlanSHA256,
			"status":              plan.Status,
			"bound_generation_id": plan.BoundGenerationID,
			"steps":               steps,
		},
	})
	return err
}

func (e *Harness) ResumeOwned(
	ctx context.Context,
	actorUserID int64,
	runID string,
	toolCallID string,
	request agentservice.ResumeToolCallRequest,
) (agentservice.AgentRun, error) {
	if _, err := e.runs.GetOwnedRun(ctx, runID, actorUserID); err != nil {
		return agentservice.AgentRun{}, err
	}
	return e.Resume(ctx, runID, toolCallID, request)
}

func (e *Harness) GetRun(ctx context.Context, runID string) (agentservice.AgentRun, error) {
	return e.runs.GetRun(ctx, runID)
}

func (e *Harness) GetOwnedRun(
	ctx context.Context,
	runID string,
	actorUserID int64,
) (agentservice.AgentRun, error) {
	return e.runs.GetOwnedRun(ctx, runID, actorUserID)
}

func (e *Harness) CancelOwned(
	ctx context.Context,
	actorUserID int64,
	runID string,
	reason string,
) (agentservice.AgentRun, error) {
	run, err := e.runs.CancelOwnedRun(ctx, runID, actorUserID, reason)
	if err != nil {
		return agentservice.AgentRun{}, err
	}
	if run.Status == agentservice.RunStatusCancelled {
		if e.plans != nil {
			_ = e.plans.MarkBlocked(context.WithoutCancel(ctx), run.ID)
		}
		e.activeMu.Lock()
		active := e.activeRuns[runID]
		e.activeMu.Unlock()
		if active != nil {
			active.cancel()
		}
	}
	return run, nil
}

func (e *Harness) ListEvents(ctx context.Context, runID string, afterSeq int64) ([]agentservice.Event, error) {
	return e.runs.ListEvents(ctx, runID, afterSeq)
}

func (e *Harness) ListOwnedEvents(
	ctx context.Context,
	runID string,
	actorUserID int64,
	afterSeq int64,
) ([]agentservice.Event, error) {
	if _, err := e.runs.GetOwnedRun(ctx, runID, actorUserID); err != nil {
		return nil, err
	}
	return e.runs.ListEvents(ctx, runID, afterSeq)
}

func (e *Harness) Subscribe(runID string) agentservice.Subscription {
	return e.runs.Subscribe(runID)
}

func toolDefinitions(registry *tools.Registry) []agent.ToolDefinition {
	definitions := registry.Definitions()
	result := make([]agent.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, agent.ToolDefinition{
			Name:        definition.Name,
			Description: definition.Description,
			InputSchema: definition.InputSchema,
		})
	}
	return result
}

func (e *Harness) recordMessage(ctx context.Context, run agentservice.AgentRun, content string) error {
	stepID := e.runs.NewID("step")
	events := []agentservice.Event{
		{Type: agentservice.EventMessageStarted, StepID: stepID},
		{Type: agentservice.EventMessageDelta, StepID: stepID, Payload: map[string]any{"delta": content}},
		{Type: agentservice.EventMessageFinished, StepID: stepID, Payload: map[string]any{"content": content}},
	}
	for _, event := range events {
		if _, err := e.runs.RecordEvent(ctx, run, event); err != nil {
			return err
		}
	}
	return nil
}

func (e *Harness) recordToolResult(
	ctx context.Context,
	run agentservice.AgentRun,
	stepID string,
	call agent.ModelTool,
	result tools.Result,
) (int64, error) {
	payload, err := agent.NewToolResultEventPayload(call.Name, result.Content)
	if err != nil {
		return 0, err
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("encode tool result event: %w", err)
	}
	var eventPayload map[string]any
	if err := json.Unmarshal(encodedPayload, &eventPayload); err != nil {
		return 0, fmt.Errorf("normalize tool result event: %w", err)
	}
	persisted, err := e.runs.RecordEvent(ctx, run, agentservice.Event{
		Type:       agentservice.EventToolResult,
		StepID:     stepID,
		ToolCallID: call.ID,
		Payload:    eventPayload,
	})
	if err != nil {
		return 0, err
	}
	events := []agentservice.Event{
		{
			Type:       agentservice.EventToolFinished,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload:    map[string]any{"tool": call.Name},
		},
	}
	if result.Artifact != nil {
		events = append(events, agentservice.Event{
			Type:       agentservice.EventArtifact,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload: map[string]any{
				"type": result.Artifact.Type,
				"id":   result.Artifact.ID,
			},
		})
	}
	for _, event := range events {
		if _, err := e.runs.RecordEvent(ctx, run, event); err != nil {
			return 0, err
		}
	}
	return persisted.Seq, nil
}

func (e *Harness) recordToolStart(
	ctx context.Context,
	run agentservice.AgentRun,
	stepID string,
	call agent.ModelTool,
) error {
	events := []agentservice.Event{
		{
			Type:       agentservice.EventToolStarted,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload:    map[string]any{"tool": call.Name},
		},
		{
			Type:       agentservice.EventToolArgsDelta,
			StepID:     stepID,
			ToolCallID: call.ID,
			Payload:    map[string]any{"arguments": call.Arguments},
		},
	}
	for _, event := range events {
		if _, err := e.runs.RecordEvent(ctx, run, event); err != nil {
			return err
		}
	}
	return nil
}

func (e *Harness) recordToolFailure(
	ctx context.Context,
	run agentservice.AgentRun,
	stepID string,
	call agent.ModelTool,
	cause error,
) error {
	_, err := e.runs.RecordEvent(ctx, run, agentservice.Event{
		Type:       agentservice.EventToolFailed,
		StepID:     stepID,
		ToolCallID: call.ID,
		Payload: map[string]any{
			"tool":    call.Name,
			"message": cause.Error(),
		},
	})
	return err
}

func (e *Harness) recordRecoverableToolFailure(
	ctx context.Context,
	run *agentservice.AgentRun,
	stepID string,
	call agent.ModelTool,
	cause error,
) error {
	if err := e.recordToolFailure(ctx, *run, stepID, call, cause); err != nil {
		return err
	}
	payload := map[string]any{
		"status":  "error",
		"tool":    call.Name,
		"message": cause.Error(),
	}
	var gateError *ExplorationGateError
	if errors.As(cause, &gateError) {
		payload["code"] = gateError.Code
		payload["exploration_budget"] = gateError.Budget
		payload["next_action"] = gateError.NextAction
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode tool failure result: %w", err)
	}
	run.Transcript = append(run.Transcript, agent.Message{
		Role:       "tool",
		Content:    string(content),
		ToolCallID: call.ID,
	})
	if err := e.runs.SaveRun(ctx, *run); err != nil {
		return fmt.Errorf("save recoverable tool failure: %w", err)
	}
	return nil
}
