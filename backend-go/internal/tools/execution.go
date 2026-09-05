package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

type ExecutionCapabilityClient interface {
	ExecuteDSL(
		ctx context.Context,
		actorUserID int64,
		runID string,
		projectID int64,
		conversationID string,
		arguments json.RawMessage,
	) (json.RawMessage, error)
	GetReport(
		ctx context.Context,
		actorUserID int64,
		projectID int64,
		conversationID string,
		arguments json.RawMessage,
	) (json.RawMessage, error)
	PrepareFixAndRetry(
		ctx context.Context,
		actorUserID int64,
		projectID int64,
		conversationID string,
		arguments json.RawMessage,
	) (json.RawMessage, error)
}

type ExecuteDSLTool struct {
	client ExecutionCapabilityClient
}

func NewExecuteDSLTool(client ExecutionCapabilityClient) ExecuteDSLTool {
	return ExecuteDSLTool{client: client}
}

func (t ExecuteDSLTool) Definition() Definition {
	return Definition{
		Name: "execute_dsl",
		Description: "Persist the latest approved DSL as a test case and enqueue it for official execution. " +
			"The generation_id must match the version approved by the user.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"generation_id":{"type":"integer","minimum":1},
				"input_values":{"type":"object","additionalProperties":{"type":"string"}}
			},
			"required":["generation_id"]
		}`),
	}
}

func (t ExecuteDSLTool) Execute(ctx context.Context, call Call) (Result, error) {
	var arguments struct {
		GenerationID int64 `json:"generation_id"`
	}
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return Result{}, fmt.Errorf("decode execute_dsl arguments: %w", err)
	}
	if arguments.GenerationID < 1 {
		return Result{}, errors.New("generation_id must be a positive integer")
	}
	if call.ApprovedGenerationID == nil || *call.ApprovedGenerationID != arguments.GenerationID {
		return Result{}, errors.New("the latest DSL generation has not been approved by the user")
	}
	content, err := t.client.ExecuteDSL(
		ctx,
		call.ActorUserID,
		call.RunID,
		call.ProjectID,
		call.ConversationID,
		call.Arguments,
	)
	if err != nil {
		return Result{}, err
	}
	var response struct {
		BatchID int64 `json:"batch_id"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return Result{}, err
	}
	result := Result{Content: content}
	if response.BatchID > 0 {
		result.Artifact = &Artifact{
			Type: "execution_batch",
			ID:   strconv.FormatInt(response.BatchID, 10),
		}
	}
	return result, nil
}

type GetReportTool struct {
	client       ExecutionCapabilityClient
	pollInterval time.Duration
	maxWait      time.Duration
}

func NewGetReportTool(client ExecutionCapabilityClient) GetReportTool {
	return GetReportTool{
		client:       client,
		pollInterval: time.Second,
		maxWait:      10 * time.Minute,
	}
}

func (t GetReportTool) Definition() Definition {
	return Definition{
		Name:        "get_report",
		Description: "Read the structured report for an execution batch. Use the returned status and failure signals as facts.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"batch_id":{"type":"integer","minimum":1},
				"wait_for_terminal":{"type":"boolean","description":"Wait for the queued execution to finish; defaults to true"}
			},
			"required":["batch_id"]
		}`),
	}
}

func (t GetReportTool) Execute(ctx context.Context, call Call) (Result, error) {
	var arguments struct {
		BatchID         int64 `json:"batch_id"`
		WaitForTerminal *bool `json:"wait_for_terminal"`
	}
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return Result{}, fmt.Errorf("decode get_report arguments: %w", err)
	}
	if arguments.BatchID < 1 {
		return Result{}, errors.New("batch_id must be a positive integer")
	}
	waitForTerminal := arguments.WaitForTerminal == nil || *arguments.WaitForTerminal
	deadline := time.Now().Add(t.maxWait)

	var content json.RawMessage
	for {
		var err error
		content, err = t.client.GetReport(
			ctx,
			call.ActorUserID,
			call.ProjectID,
			call.ConversationID,
			call.Arguments,
		)
		if err != nil {
			return Result{}, err
		}
		status, err := reportStatus(content)
		if err != nil {
			return Result{}, err
		}
		if !waitForTerminal || isTerminalExecutionStatus(status) || time.Now().After(deadline) {
			break
		}
		timer := time.NewTimer(t.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Result{}, ctx.Err()
		case <-timer.C:
		}
	}

	result := Result{Content: content}
	if arguments.BatchID > 0 {
		result.Artifact = &Artifact{
			Type: "execution_report",
			ID:   strconv.FormatInt(arguments.BatchID, 10),
		}
	}
	return result, nil
}

func reportStatus(content json.RawMessage) (string, error) {
	var response struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return "", fmt.Errorf("decode execution report: %w", err)
	}
	if response.Status == "" {
		return "", errors.New("execution report has no status")
	}
	return response.Status, nil
}

func isTerminalExecutionStatus(status string) bool {
	switch status {
	case "passed", "failed", "needs_intervention", "cancelled":
		return true
	default:
		return false
	}
}

type FixAndRetryTool struct {
	client ExecutionCapabilityClient
}

func NewFixAndRetryTool(client ExecutionCapabilityClient) FixAndRetryTool {
	return FixAndRetryTool{client: client}
}

func (t FixAndRetryTool) Definition() Definition {
	return Definition{
		Name: "fix_and_retry",
		Description: "Start a transparent repair workflow for a failed execution batch. " +
			"Returns the failure facts, source DSL, and required strategy. " +
			"Follow the returned strategy with exploration when required, validate elements, generate a new DSL, request approval, and execute it.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"batch_id":{"type":"integer","minimum":1}
			},
			"required":["batch_id"]
		}`),
	}
}

func (t FixAndRetryTool) Execute(ctx context.Context, call Call) (Result, error) {
	content, err := t.client.PrepareFixAndRetry(
		ctx,
		call.ActorUserID,
		call.ProjectID,
		call.ConversationID,
		call.Arguments,
	)
	if err != nil {
		return Result{}, err
	}
	var response struct {
		SourceBatchID int64  `json:"source_batch_id"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return Result{}, err
	}
	result := Result{Content: content}
	if response.SourceBatchID > 0 &&
		(response.Status == "repair_ready" || response.Status == "manual_required") {
		result.Artifact = &Artifact{
			Type: "repair_plan",
			ID:   strconv.FormatInt(response.SourceBatchID, 10),
		}
	}
	return result, nil
}
