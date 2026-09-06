package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/cases"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/execution"
)

type BrowserValidator interface {
	ExecuteBrowserCapability(
		ctx context.Context,
		capability string,
		actorUserID int64,
		projectID int64,
		conversationID string,
		arguments json.RawMessage,
	) (json.RawMessage, error)
}

type ControlPlaneCapabilities struct {
	dsl        *dsl.Store
	cases      *cases.PostgresStore
	executions *execution.Store
	browser    BrowserValidator
}

func NewControlPlaneCapabilities(
	dslStore *dsl.Store,
	caseStore *cases.PostgresStore,
	executionStore *execution.Store,
	browser BrowserValidator,
) *ControlPlaneCapabilities {
	return &ControlPlaneCapabilities{
		dsl: dslStore, cases: caseStore, executions: executionStore, browser: browser,
	}
}

func (c *ControlPlaneCapabilities) GenerateDSL(
	ctx context.Context,
	actorUserID, projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	var request struct {
		Case             json.RawMessage              `json:"case"`
		A11yNodesByState map[string][]json.RawMessage `json:"a11y_nodes_by_state"`
	}
	if err := json.Unmarshal(arguments, &request); err != nil || len(request.Case) == 0 {
		return nil, errors.New("generate_dsl requires a case object")
	}
	normalizedCase, _, err := dsl.ValidateCase(request.Case)
	if err != nil {
		return nil, err
	}
	validationArguments, err := json.Marshal(map[string]any{
		"dsl_case":            normalizedCase,
		"a11y_nodes_by_state": request.A11yNodesByState,
	})
	if err != nil {
		return nil, err
	}
	validatedRaw, err := c.browser.ExecuteBrowserCapability(
		ctx, "validate_page_elements", actorUserID, projectID, conversationID, validationArguments,
	)
	if err != nil {
		return nil, err
	}
	var validated struct {
		Case           json.RawMessage `json:"dsl_case"`
		Valid          bool            `json:"valid"`
		ValidationMode string          `json:"validation_mode"`
		CaseDigest     string          `json:"case_digest"`
		EvidenceDigest string          `json:"evidence_digest"`
		Warnings       []string        `json:"warnings"`
	}
	if err := json.Unmarshal(validatedRaw, &validated); err != nil {
		return nil, fmt.Errorf("decode DSL preflight result: %w", err)
	}
	if !validated.Valid || len(validated.Case) == 0 {
		if len(validated.Warnings) > 0 {
			return nil, fmt.Errorf(
				"DSL locator preflight failed: %s",
				strings.Join(validated.Warnings, "; "),
			)
		}
		return nil, errors.New("DSL locator preflight failed without details")
	}
	expectedCaseDigest, err := canonicalJSONDigest(normalizedCase)
	if err != nil {
		return nil, fmt.Errorf("digest normalized DSL case: %w", err)
	}
	expectedEvidenceDigest, err := canonicalJSONDigest(request.A11yNodesByState)
	if err != nil {
		return nil, fmt.Errorf("digest DSL evidence: %w", err)
	}
	if validated.ValidationMode != "dsl_case" ||
		validated.CaseDigest != expectedCaseDigest ||
		validated.EvidenceDigest != expectedEvidenceDigest {
		return nil, errors.New("DSL locator preflight result is not bound to the submitted case and evidence")
	}
	generation, err := c.dsl.CreateGeneration(
		ctx, actorUserID, projectID, validated.Case, validated.Warnings,
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"generation_id":              generation.ID,
		"case":                       generation.Case,
		"dsl_sha256":                 generation.DSLHash,
		"dsl_canonical_version":      generation.CanonicalVersion,
		"validation_case_digest":     validated.CaseDigest,
		"validation_evidence_digest": validated.EvidenceDigest,
		"supported_actions":          []string{"goto", "click", "input", "wait_for", "assert_text", "assert_url_contains", "capture_text"},
		"warnings":                   validated.Warnings,
		"normalization_notes":        []string{},
	})
}

func canonicalJSONDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return "", err
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(decoded); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")))
	return hex.EncodeToString(sum[:]), nil
}

func (c *ControlPlaneCapabilities) ExecuteDSL(
	ctx context.Context,
	actorUserID int64,
	runID string,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	var request struct {
		GenerationID int64             `json:"generation_id"`
		InputValues  map[string]string `json:"input_values"`
	}
	if err := json.Unmarshal(arguments, &request); err != nil || request.GenerationID < 1 {
		return nil, errors.New("generation_id must be a positive integer")
	}
	idempotencyKey := fmt.Sprintf("agent:%s:generation:%d", runID, request.GenerationID)
	if batch, found, err := c.executions.BatchByIdempotency(ctx, actorUserID, idempotencyKey); err != nil {
		return nil, err
	} else if found {
		return executionResult(batch)
	}
	generation, err := c.dsl.GetGeneration(ctx, actorUserID, projectID, request.GenerationID)
	if err != nil {
		return nil, err
	}
	if !generation.Success {
		return nil, errors.New("DSL generation has no executable case")
	}
	mutation, err := caseMutation(projectID, generation.Case)
	if err != nil {
		return nil, err
	}
	testCase, err := c.cases.Create(ctx, actorUserID, mutation)
	if err != nil {
		return nil, err
	}
	var planningSessionID *int64
	if value, parseErr := strconv.ParseInt(conversationID, 10, 64); parseErr == nil && value > 0 {
		planningSessionID = &value
	}
	batch, err := c.executions.CreateBatch(ctx, actorUserID, execution.BatchCreateRequest{
		ProjectID:       projectID,
		CaseIDs:         []int64{testCase.ID},
		PlanningSession: planningSessionID,
		IdempotencyKey:  &idempotencyKey,
		Concurrency:     1,
		InputValues:     request.InputValues,
		DSLBindings: map[int64]execution.CanonicalDSLBinding{
			testCase.ID: {
				CanonicalJSON: generation.Case,
				SHA256:        generation.DSLHash,
				Version:       generation.CanonicalVersion,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return executionResult(batch)
}

func (c *ControlPlaneCapabilities) GetReport(
	ctx context.Context,
	actorUserID, projectID int64,
	_ string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	batchID, err := batchIDArgument(arguments)
	if err != nil {
		return nil, err
	}
	report, err := c.executions.BatchReport(ctx, actorUserID, batchID)
	if err != nil {
		return nil, err
	}
	if report["project_id"] != projectID {
		return nil, execution.ErrNotFound
	}
	return json.Marshal(report)
}

func (c *ControlPlaneCapabilities) PrepareFixAndRetry(
	ctx context.Context,
	actorUserID, projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	raw, err := c.GetReport(ctx, actorUserID, projectID, conversationID, arguments)
	if err != nil {
		return nil, err
	}
	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, err
	}
	status, _ := report["status"].(string)
	if status == "passed" {
		return repairResponse(report, "not_required", "none", "The execution already passed.")
	}
	if status == "pending" || status == "running" {
		return repairResponse(report, "wait_execution", "wait", "The source execution has not reached a terminal state.")
	}

	signals, sourceExecutionID, sourceDSL := reportFailures(report)
	categories := make(map[string]bool)
	for _, signal := range signals {
		if category, ok := signal["category"].(string); ok {
			categories[category] = true
		}
	}
	strategy := "manual"
	reason := "Configuration, network, runner, or unknown failures require manual review."
	if categories["locator"] || categories["navigation"] {
		strategy = "re_explore"
		reason = "Page structure or navigation evidence is stale or incomplete."
	} else if categories["assertion"] {
		strategy = "regenerate_dsl"
		reason = "The expected result or assertion logic must be revised."
	}
	repairStatus := "repair_ready"
	if strategy == "manual" {
		repairStatus = "manual_required"
	}
	return json.Marshal(map[string]any{
		"status": repairStatus, "source_batch_id": report["id"],
		"source_execution_id": sourceExecutionID, "strategy": strategy,
		"reason": reason, "failure_signals": signals, "source_dsl": sourceDSL,
		"report": report,
	})
}

func caseMutation(projectID int64, raw json.RawMessage) (cases.Mutation, error) {
	var candidate struct {
		Name           string          `json:"name"`
		Description    *string         `json:"description"`
		BaseURL        *string         `json:"base_url"`
		InputContract  json.RawMessage `json:"input_contract"`
		OutputContract json.RawMessage `json:"output_contract"`
		Steps          json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(raw, &candidate); err != nil {
		return cases.Mutation{}, err
	}
	return cases.Mutation{
		ProjectID: projectID, Name: candidate.Name, Description: candidate.Description,
		BaseURL: candidate.BaseURL, InputContract: candidate.InputContract,
		OutputContract: candidate.OutputContract, Steps: candidate.Steps,
	}, nil
}

func executionResult(batch map[string]any) (json.RawMessage, error) {
	var caseID any
	if jobs, ok := batch["jobs"].([]map[string]any); ok && len(jobs) > 0 {
		caseID = jobs[0]["case_id"]
	}
	return json.Marshal(map[string]any{
		"batch_id": batch["id"], "case_id": caseID, "status": batch["status"],
		"report_api_url": fmt.Sprintf("/api/v2/execution-batches/%v/report", batch["id"]),
	})
}

func batchIDArgument(arguments json.RawMessage) (int64, error) {
	var request struct {
		BatchID int64 `json:"batch_id"`
	}
	if err := json.Unmarshal(arguments, &request); err != nil || request.BatchID < 1 {
		return 0, errors.New("batch_id must be a positive integer")
	}
	return request.BatchID, nil
}

func repairResponse(report map[string]any, status, strategy, reason string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"status": status, "source_batch_id": report["id"],
		"strategy": strategy, "reason": reason, "report": report,
	})
}

func reportFailures(report map[string]any) ([]map[string]any, any, any) {
	signals := make([]map[string]any, 0)
	if analysis, ok := report["analysis"].(map[string]any); ok {
		if values, ok := analysis["failure_signals"].([]any); ok {
			for _, value := range values {
				if signal, ok := value.(map[string]any); ok {
					signals = append(signals, signal)
				}
			}
		}
	}
	var sourceExecutionID, sourceDSL any
	useJobSignals := len(signals) == 0
	if jobs, ok := report["jobs"].([]any); ok {
		for _, rawJob := range jobs {
			job, _ := rawJob.(map[string]any)
			latest, _ := job["latest_execution"].(map[string]any)
			if latest == nil {
				continue
			}
			if signal, ok := latest["failure_signal"].(map[string]any); ok && useJobSignals {
				signals = append(signals, signal)
			}
			if sourceExecutionID == nil && latest["status"] != "passed" {
				sourceExecutionID = latest["id"]
				sourceDSL = latest["dsl_snapshot"]
			}
		}
	}
	return signals, sourceExecutionID, sourceDSL
}
