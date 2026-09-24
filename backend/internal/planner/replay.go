package planner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

const CodeCommittedPrefixReplayFailed = "committed_prefix_replay_failed"

// FatalError marks planner state failures that must terminate the run instead
// of being returned to the model as another repairable tool result.
type FatalError struct {
	Code string
	Err  error
}

func (e *FatalError) Error() string {
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *FatalError) Unwrap() error {
	return e.Err
}

func IsFatalError(err error) bool {
	var fatal *FatalError
	return errors.As(err, &fatal)
}

// ReplayCommitted recreates the authoring browser and restores an exact
// committed prefix. The requested prefix becomes the planner's complete step
// list before any worker calls, so a failed replay cannot expose the rejected
// tail again.
func (p *Planner) ReplayCommitted(ctx context.Context, prefixLength int) error {
	if prefixLength < 0 || prefixLength > len(p.steps) {
		return fmt.Errorf("invalid committed prefix length %d for %d steps", prefixLength, len(p.steps))
	}
	p.steps = append([]contract.Step(nil), p.steps[:prefixLength]...)
	p.observation = contract.Observation{}
	p.hasPage = false
	p.baseURL = ""

	previousSessionID := p.browserSessionID
	p.browserSessionID = ""
	if previousSessionID != "" {
		_ = p.client.CloseSession(ctx, previousSessionID)
	}
	browserSessionID, err := p.client.OpenSession(ctx, p.sessionID)
	if err != nil {
		return fmt.Errorf("open replay session: %w", err)
	}
	p.browserSessionID = browserSessionID

	for _, step := range p.steps {
		if err := p.replayStep(ctx, step); err != nil {
			return fmt.Errorf("replay committed step %d (%s): %w", step.Index, step.Action, err)
		}
	}
	return nil
}

func (p *Planner) replayStep(ctx context.Context, step contract.Step) error {
	if step.Action == contract.ActionGoto {
		if step.Value == nil {
			return errors.New("goto step has no url")
		}
		observation, err := p.client.Navigate(ctx, p.browserSessionID, *step.Value)
		if err != nil {
			return err
		}
		if err := evaluatePageConditions(observation, step.Postconditions); err != nil {
			return err
		}
		p.observation = observation
		p.hasPage = true
		if p.baseURL == "" {
			p.baseURL = origin(*step.Value)
		}
		return nil
	}
	if !p.hasPage {
		return errors.New("step has no replayed page")
	}
	if step.Target == nil {
		if !isPageAssertion(step.Action) {
			return errors.New("non-assertion step has no target")
		}
		return evaluatePageConditions(p.observation, step.Postconditions)
	}

	request := contract.ActRequest{
		Action:         step.Action,
		Locator:        step.Target.Locator,
		Submit:         step.Submit,
		Postconditions: step.Postconditions,
	}
	if step.Value != nil {
		request.Value = *step.Value
	}
	response, err := p.client.Act(ctx, p.browserSessionID, request)
	if err != nil {
		return err
	}
	p.observation = response.Observation
	p.hasPage = true
	if response.Status != "passed" {
		if response.Error != nil {
			return fmt.Errorf("%s: %s", response.Error.Kind, response.Error.Message)
		}
		return fmt.Errorf("worker returned status %q", response.Status)
	}
	return nil
}

func isPageAssertion(action contract.Action) bool {
	return action == contract.ActionAssertText || action == contract.ActionAssertURL
}

func evaluatePageConditions(
	observation contract.Observation, conditions []contract.Condition,
) error {
	for _, condition := range conditions {
		var satisfied bool
		switch condition.Type {
		case contract.CondURLContains:
			satisfied = strings.Contains(observation.URL, condition.Value)
		case contract.CondTextVisible:
			satisfied = observationHasText(observation, condition.Value)
		case contract.CondTextGone:
			satisfied = !observationHasText(observation, condition.Value)
		default:
			return fmt.Errorf("cannot evaluate page condition %q during replay", condition.Type)
		}
		if !satisfied {
			return fmt.Errorf("replay condition %s(%q) was not satisfied", condition.Type, condition.Value)
		}
	}
	return nil
}

// PrepareDryRunRepair removes the failed step and its tail based only on the
// fresh execution result, then restores the remaining committed prefix.
func (p *Planner) PrepareDryRunRepair(
	ctx context.Context, execution contract.ExecutionResult,
) (Result, error) {
	result := Result{
		OK:      false,
		Error:   "dry_run_failed",
		Detail:  "the authored case did not pass a full dry run; the failed tail was removed",
		Failure: dryRunFailure(execution),
		Steps:   stepViews(p.steps),
	}
	failedIndex := -1
	for _, step := range execution.Steps {
		if step.Status != "passed" {
			failedIndex = step.Index
			break
		}
	}
	if failedIndex < 0 {
		if p.hasPage {
			result.Page = pageView(p.observation)
		}
		return result, nil
	}
	if failedIndex >= len(p.steps) {
		return Result{}, &FatalError{
			Code: CodeCommittedPrefixReplayFailed,
			Err: fmt.Errorf(
				"dry-run failed step index %d does not identify a committed step among %d steps",
				failedIndex,
				len(p.steps),
			),
		}
	}
	if err := p.ReplayCommitted(ctx, failedIndex); err != nil {
		return Result{}, &FatalError{Code: CodeCommittedPrefixReplayFailed, Err: err}
	}
	result.Steps = stepViews(p.steps)
	if p.hasPage {
		result.Page = pageView(p.observation)
	}
	return result, nil
}
