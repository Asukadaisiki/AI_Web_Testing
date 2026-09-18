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
Build a candidate_ref from the current-run explore summary (source.event_seq, probe_id,
observation_id, and candidate_id), then submit it in an explicit grounding.query.v2
explore_flow action. The Go control plane hydrates the reference before the Browser
Worker receives it. A direct locator
action must use a structured semantic LocatorSpec using role, accessible name, label,
placeholder, text, test ID, or a semantic scoped locator. Never submit resolved_candidate,
CSS, or XPath; resolved_candidate is Worker-only trusted state.
Every explore_page or explore_flow call must include plan_step_ids for the next contiguous pending steps.
For explore_flow, set action.plan_step_id on every action intended to ground a pending PlanStep. A click or input that replays an already grounded prerequisite must keep that original plan_step_id and is treated as a supporting action. Supporting wait_for observations may omit plan_step_id. Never rely on action order or target text to infer ownership.
Never execute external_state or unknown side effects during exploration. Such steps may only be grounded by observing their controls or expected facts without triggering them.
Use ask_user_question only when required information or explicit approval is missing.
Tool results shown to you use agent.model_tool_summary.v1. For exploration, first read observation.page_states, observation.selectable_candidates, observation.element_groups, observation.candidate_coverage, observation.action_options, and observation.verification_facts to understand the page; use pages[].a11y_nodes as the exact evidence submitted in generate_dsl.a11y_nodes_by_state. source.event_seq and hashes reference the complete persisted tool.result event.
	Each observation.selectable_candidates entry carries a complete candidate_ref object of schema_version "grounding.candidate-ref.v1" with source_event_seq, probe_id, observation_id, and candidate_id. Copy the whole candidate_ref into the candidate_ref field of a grounding.query.v2 explore_flow action; never rebuild or edit its fields.
	For icon buttons or any control whose accessible name is empty, glyph-only, or not reliably readable, choose the semantic-name candidate with count 1 from observation.selectable_candidates and drive it through candidate_ref. Do not author a role/name semantic locator to guess at such a control: a guess can match zero controls or several controls and fail grounding.
	After the first explore_page, reuse its returned selectable_candidates instead of exploring again to recover them; never call set_task_plan merely to reset exploration budget; and do not re-explore a page state that already holds a count=1 candidate_ref for the required control.
	When a flow action fails with count=0 or count>1, read the failure.candidate_locators and the current observation.selectable_candidates before retrying; do not guess a new locator blindly. A retry must change the locator substance (exact, role, placeholder, or a candidate_ref), never just the description or timeout — the gate rejects identical signatures. Failed calls still count against the exploration budget, so make each retry count.
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
Each step must include plan_step_id; click, input, assert_text, and capture_text steps must include
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

const webPlatformKnowledgePrompt = `PHASE 2.5 - WEB PLATFORM KNOWLEDGE
You are operating a real browser through accessibility and DOM evidence. Apply this
general web knowledge when reading observations and authoring flow actions. It is
platform knowledge, not task-specific data.

PAGE STRUCTURE
- A page usually has one header/banner, one main content region, and one footer.
  Navigation links live inside the header/banner node. A "breadcrumb" is a small
  path trail (for example Home > Shopping Cart) rendered as a list, NOT a heading.
- A page title or section heading is usually role=heading (h1-h6). Do NOT assume a
  heading exists for a page just because the page has a title: verify with the
  observation's a11y_nodes before authoring wait_for role=heading.
- "Added!", "Success", or confirmation dialogs are modal dialogs: their heading and
  buttons appear in a new overlay on top of the current page. After dismissing or
  clicking through them, the underlying page (not the modal) is the new state.
- A modal's "View Cart" link navigates to the cart page; the cart page title is
  usually a breadcrumb or a table header, not a heading.

ACCESSIBLE NAMES AND ICON GLYPHS
- Icon-only buttons (search, cart, hamburger) often have an accessible name that is a
  FontAwesome glyph (for example \uf002) or empty. Their label may look like garbage
  or blank in the observation. Prefer the count=1 candidate from
  observation.selectable_candidates and drive it through candidate_ref. Never author
  a role/name locator from a glyph you cannot read.
- A link or button whose accessible name contains the page name plus extra text (for
  example a product card "Blue Top" with nested links) may not match an exact
  role=name locator. Use exact=false or a scoped locator only when the observation
  proves the target, otherwise reuse the candidate_ref.

LOCATOR STRATEGY
- You must NOT author CSS, XPath, or raw DOM ids in flow actions; the gate rejects
  them. When the observation exposes a candidate_ref (selectable_candidates), copy it
  verbatim into the action — it is the ground truth the control plane hydrated.
- placeholder=... matches an input's placeholder attribute; role=... matches by
  accessibility role and accessible name. If a locator returns count=0 or count>1,
  do not guess again with the same shape; switch to a different evidence-backed
  locator (exact=false, another role, or the candidate_ref from the same page state).
- wait_for with condition visible checks that the target resolves exactly once and
  is visible. Use it for cross-page transition confirmation, but bind it to a
  heading/link/text that the observation actually shows on the destination page.

FLOW ACTIONS
- One explore_flow call can walk several page states: input, click, wait_for chain.
  Keep the chain within what a single disposable browser session can do. If a step
  in the middle fails, the flow reports the failure and the evidence collected so
  far; the next call continues from the last confirmed page state.
- Search workflows must execute the real input and the real search-control click;
  never synthesize a search-result URL.

DSL FIDELITY
- The final DSL must preserve every TaskPlan step's action, intent, and value
  verbatim. A step planned as click (for example "click Products in the top
  navigation") must stay a click in the DSL; never rewrite it as goto to the
  destination URL just because you know the URL. goto is only valid for steps
  the TaskPlan itself declares as goto. The compiler rejects any rewritten
  action or intent, so author the DSL directly from the persisted plan fields.
- Navigating by real UI (clicking the nav link) and navigating by goto URL are
  semantically different: the first verifies the link is reachable and usable,
  the second only proves the URL loads. Preserve the planned action to keep that
  verification.

AD OVERLAYS AND INTERSTITIALS
- Commercial sites may inject ad overlays, popups, or interstitial redirects
  (for example a URL fragment like #google_vignette) that intercept a click or
  navigation. This is an ad, not the page's real navigation: the click did not
  reach the intended destination and no business step completed.
- When a click's expected navigation lands on an ad URL or the page does not
  change to the expected destination, treat the action as failed, retry it once
  or twice (the ad is intermittent), and prefer a fresh probe context each time
  so ad state is not carried over. Do not change the plan, do not invent a
  different URL, and do not treat the ad page as a valid page state.
- A wait_for that would run on the ad page will fail; re-verify after the ad is
  gone by re-clicking or re-navigating to the intended page.
`

const defaultSystemPrompt = taskPlanningPrompt + "\n\n" +
	groundingPrompt + "\n\n" +
	webPlatformKnowledgePrompt + "\n\n" +
	dslAuthoringPrompt + "\n\n" +
	executionRepairPrompt

type Harness struct {
	runs   *agentservice.Service
	loop   *agent.Loop
	tools  *tools.Registry
	policy ToolPolicy
	plans  *taskplan.Service

	// BUG-155: run-level hard cost fuses. Zero fields mean "unlimited".
	costLimits RunCostLimits

	// perTurnTranscriptCompaction keeps the legacy behaviour of rewriting
	// historical tool summaries inside a turn. It bounds the request size but
	// invalidates the provider's prefix cache from the rewritten message
	// onward, so it is opt-in; see SetPerTurnTranscriptCompaction.
	perTurnTranscriptCompaction bool

	activeMu   sync.Mutex
	activeRuns map[string]*activeRun
}

// RunCostLimits caps cumulative model cost per run. A zero value disables the
// corresponding fuse; all values below must be >= 0.
type RunCostLimits struct {
	MaxModelCalls      int   // cumulative model calls (logical Complete calls)
	MaxTotalTokens     int64 // cumulative total tokens across calls
	MaxTranscriptBytes int   // serialized transcript size in bytes
}

func (l RunCostLimits) withDefaults() RunCostLimits {
	if l.MaxModelCalls <= 0 {
		l.MaxModelCalls = 40
	}
	if l.MaxTotalTokens <= 0 {
		l.MaxTotalTokens = 3_000_000
	}
	if l.MaxTranscriptBytes <= 0 {
		l.MaxTranscriptBytes = 2_000_000
	}
	return l
}

func (l RunCostLimits) Enabled() bool {
	return l.MaxModelCalls > 0 || l.MaxTotalTokens > 0 || l.MaxTranscriptBytes > 0
}

type activeRun struct {
	cancel     context.CancelFunc
	turnBudget int
	// generationSegmentPlanSHA256 records the plan revision for which the run
	// already received its dedicated generation context, so the boundary is
	// opened once per revision instead of on every turn that observes the
	// ready status.
	generationSegmentPlanSHA256 string
	// BUG-155 cumulative cost state.
	modelCalls      int
	totalTokens     int64
	transcriptBytes int
	lastLimit       string
}

func (a *activeRun) updateCost(calls int, totalTokens int64, transcriptBytes int) {
	a.modelCalls += calls
	a.totalTokens += totalTokens
	a.transcriptBytes = transcriptBytes
}

// exceed returns the first violated limit name or "".
func (a *activeRun) exceed(limits RunCostLimits) string {
	if limits.MaxModelCalls > 0 && a.modelCalls > limits.MaxModelCalls {
		return "model_calls"
	}
	if limits.MaxTotalTokens > 0 && a.totalTokens > limits.MaxTotalTokens {
		return "total_tokens"
	}
	if limits.MaxTranscriptBytes > 0 && a.transcriptBytes > limits.MaxTranscriptBytes {
		return "transcript_bytes"
	}
	return ""
}

// turnBudgetReserve reserves extra turns beyond grounding (2 per PlanStep)
// for DSL generation, approval, and repair phases.
const turnBudgetReserve = 8

func (e *Harness) turnBudgetFor(runID string) int {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	if active := e.activeRuns[runID]; active != nil {
		return active.turnBudget
	}
	return 0
}

func (e *Harness) raiseTurnBudget(runID string, needed int) {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	if active := e.activeRuns[runID]; active != nil && needed > active.turnBudget {
		active.turnBudget = needed
	}
}

// RunCostLimitError is a terminal run failure produced by a BUG-155 cost fuse.
type RunCostLimitError struct {
	Limit     string `json:"limit"`
	Current   string `json:"current"`
	Max       string `json:"max"`
	LimitType string `json:"limit_type"`
}

func (e *RunCostLimitError) Error() string {
	return fmt.Sprintf(
		"agent run cost limit exceeded (%s: %s > %s)",
		e.LimitType, e.Current, e.Max,
	)
}

// recordRunCost accumulates model call and token usage into the active run and
// returns a RunCostLimitError when a hard cost fuse trips. Called from the
// telemetry recorder after each model call.
func (e *Harness) recordRunCost(run agentservice.AgentRun, record agent.TelemetryRecord) error {
	if !e.costLimits.Enabled() {
		return nil
	}
	var totalTokens int64
	if usage := record.Telemetry.Usage; usage.TotalTokens != nil {
		totalTokens = *usage.TotalTokens
	}
	e.activeMu.Lock()
	active := e.activeRuns[run.ID]
	if active != nil {
		active.updateCost(1, totalTokens, active.transcriptBytes)
	}
	var exceeded string
	var calls int
	var tokens int64
	var transcriptBytes int
	if active != nil {
		exceeded = active.exceed(e.costLimits)
		calls = active.modelCalls
		tokens = active.totalTokens
		transcriptBytes = active.transcriptBytes
	}
	e.activeMu.Unlock()
	if active == nil {
		return nil
	}
	return e.costLimitError(exceeded, calls, tokens, transcriptBytes)
}

// recordTranscriptCost re-measures the serialized transcript and trips the
// transcript byte fuse when the transcript grows past the configured cap.
func (e *Harness) recordTranscriptCost(run agentservice.AgentRun) error {
	if e.costLimits.MaxTranscriptBytes <= 0 {
		return nil
	}
	bytes := serializedTranscriptBytes(run.Transcript)
	e.activeMu.Lock()
	active := e.activeRuns[run.ID]
	if active != nil {
		active.transcriptBytes = bytes
	}
	var exceeded string
	var calls int
	var tokens int64
	if active != nil {
		exceeded = active.exceed(e.costLimits)
		calls = active.modelCalls
		tokens = active.totalTokens
	}
	e.activeMu.Unlock()
	return e.costLimitError(exceeded, calls, tokens, bytes)
}

func (e *Harness) costLimitError(limit string, calls int, tokens int64, transcriptBytes int) error {
	if limit == "" {
		return nil
	}
	limits := e.costLimits
	max, current := "", ""
	switch limit {
	case "model_calls":
		max, current = intString(limits.MaxModelCalls), intString(calls)
	case "total_tokens":
		max, current = int64String(limits.MaxTotalTokens), int64String(tokens)
	case "transcript_bytes":
		max, current = intString(limits.MaxTranscriptBytes), intString(transcriptBytes)
	}
	return &RunCostLimitError{
		Limit: limit, LimitType: limit,
		Current: current, Max: max,
	}
}

func serializedTranscriptBytes(messages []agent.Message) int {
	encoded, err := json.Marshal(messages)
	if err != nil {
		return 0
	}
	return len(encoded)
}

func intString(value int) string     { return strconv.Itoa(value) }
func int64String(value int64) string { return strconv.FormatInt(value, 10) }

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

func NewWithTaskPlansAndExploration(
	runs *agentservice.Service,
	model agent.Model,
	registry *tools.Registry,
	plans *taskplan.Service,
	maxSteps int,
	exploration ExplorationGateConfig,
) *Harness {
	engine := newHarness(runs, model, registry, plans, maxSteps)
	engine.policy = DefaultToolPolicy{Exploration: exploration}
	return engine
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
		costLimits: defaultRunCostLimits(),
		activeRuns: make(map[string]*activeRun),
	}
}

func defaultRunCostLimits() RunCostLimits {
	return RunCostLimits{
		MaxModelCalls:      40,
		MaxTotalTokens:     3_000_000,
		MaxTranscriptBytes: 1_000_000,
	}
}

// SetRunCostLimits overrides the default hard cost fuses. Zero fields disable
// the corresponding fuse (a nil pointer also disables all fuses).
func (e *Harness) SetRunCostLimits(limits RunCostLimits) {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	e.costLimits = limits
}

// SetPerTurnTranscriptCompaction opts into rewriting historical tool summaries
// after every tool result. Rewriting an earlier message changes the request
// prefix, which discards the provider's prompt cache from that point onward,
// so the default is off: the transcript stays append-only and phase boundaries
// bound its growth instead. Enable it only when request size must be capped
// and cache locality does not matter.
func (e *Harness) SetPerTurnTranscriptCompaction(enabled bool) {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	e.perTurnTranscriptCompaction = enabled
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

	loopErr := e.loop.RunWithTurnBudget(
		ctx,
		&run.Transcript,
		func(callContext context.Context) context.Context {
			logicalCallID := e.runs.NewID("llm")
			stepID := e.runs.NewID("step")
			state, _, _ := e.currentPipelineState(callContext, run)
			return agent.WithTelemetryRecorder(
				callContext,
				func(recordContext context.Context, record agent.TelemetryRecord) error {
					if err := e.runs.RecordModelTelemetry(recordContext, run, record); err != nil {
						return err
					}
					// BUG-155: accumulate run-level cost; trip the hard fuse
					// before the loop schedules the next model call.
					return e.recordRunCost(run, record)
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
				var readyPlan *taskplan.Plan
				if e.plans != nil &&
					(agent.IsExplorationTool(call.Name) ||
						call.Name == "set_task_plan") {
					currentPlan, planErr := e.plans.GetCurrent(ctx, run.ID)
					if planErr != nil {
						return false, planErr
					}
					e.raiseTurnBudget(
						run.ID,
						len(currentPlan.Steps)*2+turnBudgetReserve,
					)
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
					if currentPlan.Status == taskplan.StatusReadyForGeneration {
						plan := currentPlan
						readyPlan = &plan
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
				if e.perTurnTranscriptCompaction {
					run.Transcript = agent.CompactExplorationTranscript(
						run.Transcript,
					)
				}
				// Grounding finished, so close the exploration context. The
				// handoff is appended after the tool result to keep the
				// assistant/tool message pairing valid, and it starts a new
				// segment so the model stops replaying every grounding turn.
				if readyPlan != nil &&
					e.markGenerationSegment(run.ID, readyPlan.PlanSHA256) {
					run.Transcript = append(
						run.Transcript,
						generationHandoffMessage(*readyPlan),
					)
				}
				if err := e.runs.SaveRun(ctx, run); err != nil {
					return false, err
				}
				if err := e.recordTranscriptCost(run); err != nil {
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
		func(used int) int {
			return e.turnBudgetFor(run.ID)
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

// markGenerationSegment records that the run has been handed its dedicated
// generation context for this plan revision. It returns false when the run was
// already segmented for the same revision, so a later turn that merely
// observes the ready status again does not reset the context repeatedly.
func (e *Harness) markGenerationSegment(runID, planSHA256 string) bool {
	e.activeMu.Lock()
	defer e.activeMu.Unlock()
	active, ok := e.activeRuns[runID]
	if !ok {
		return false
	}
	if active.generationSegmentPlanSHA256 == planSHA256 {
		return false
	}
	active.generationSegmentPlanSHA256 = planSHA256
	return true
}

// generationHandoffMessage opens the generation segment. Grounding results are
// carried by the persisted TaskPlan, so the handoff restates the grounded plan
// in full and the model no longer has to re-read the exploration turns (and
// their replayed reasoning) to author the DSL.
func generationHandoffMessage(plan taskplan.Plan) agent.Message {
	type handoffStep struct {
		ID                   string   `json:"id"`
		Position             int      `json:"position"`
		Action               string   `json:"action"`
		Intent               string   `json:"intent"`
		Target               string   `json:"target,omitempty"`
		Value                string   `json:"value,omitempty"`
		Trigger              string   `json:"trigger,omitempty"`
		ContextKey           string   `json:"context_key,omitempty"`
		TimeoutMS            int      `json:"timeout_ms,omitempty"`
		Idempotency          string   `json:"idempotency"`
		SideEffect           string   `json:"side_effect"`
		Preconditions        []string `json:"preconditions"`
		CompletionConditions []string `json:"completion_conditions"`
		TargetBindingID      string   `json:"target_binding_id,omitempty"`
		SelectedCandidateID  string   `json:"selected_candidate_id,omitempty"`
	}
	payload := struct {
		SchemaVersion string        `json:"schema_version"`
		Kind          string        `json:"kind"`
		Goal          string        `json:"goal"`
		PlanID        string        `json:"plan_id"`
		PlanVersion   int           `json:"plan_version"`
		PlanSHA256    string        `json:"plan_sha256"`
		Status        string        `json:"status"`
		Steps         []handoffStep `json:"steps"`
		Instruction   string        `json:"instruction"`
	}{
		SchemaVersion: "agent.grounding_handoff.v1",
		Kind:          "grounding_complete",
		Goal:          plan.Goal,
		PlanID:        plan.ID,
		PlanVersion:   plan.Version,
		PlanSHA256:    plan.PlanSHA256,
		Status:        string(plan.Status),
		Instruction: "Every plan step is grounded. The exploration context was closed to " +
			"bound the request size, so treat this handoff and the persisted TaskPlan as " +
			"the authoritative plan. Author the research-v2 DSL with generate_dsl: bind " +
			"each step to its plan_step_id and target_binding_id, and reproduce every " +
			"plan-owned field (action, value, trigger, context_key, timeout_ms, " +
			"idempotency, side_effect, preconditions, completion conditions) exactly.",
	}
	for _, step := range plan.Steps {
		entry := handoffStep{
			ID: step.ID, Position: step.Position, Action: step.Action,
			Intent: step.Intent, Target: step.Target, Value: step.Value,
			Trigger: step.Trigger, ContextKey: step.ContextKey,
			TimeoutMS: step.TimeoutMS, Idempotency: step.Idempotency,
			SideEffect:           string(step.SideEffect),
			Preconditions:        append([]string(nil), step.Preconditions...),
			CompletionConditions: append([]string(nil), step.CompletionConditions...),
		}
		if step.TargetBinding != nil {
			entry.TargetBindingID = step.TargetBinding.BindingID
			entry.SelectedCandidateID = step.TargetBinding.SelectedCandidateID
		}
		payload.Steps = append(payload.Steps, entry)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		// Every field is a plain value, so encoding cannot realistically fail;
		// fall back to the goal rather than dropping the boundary message.
		encoded = []byte(plan.Goal)
	}
	return agent.Message{
		Role:            "user",
		Content:         string(encoded),
		SegmentBoundary: true,
	}
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
