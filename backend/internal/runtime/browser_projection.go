package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/piharness"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

const (
	browserHarnessToolPrefix = "mcp__browser_harness__"
	finishCaseToolName       = browserHarnessToolPrefix + "finish_case"
)

// browserProjection is the fixed browser evidence adapter. Generic Pi
// persistence does not interpret tool names, case verdicts or screenshots.
type browserProjection struct {
	Store *postgres.Store
}

func (p browserProjection) project(ctx context.Context, ref HarnessAttemptRef, ev piharness.Event, record *postgres.HarnessEventRecord) error {
	switch ev.Type {
	case piharness.EventToolCompleted:
		var payload piharness.ToolCompletedPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return err
		}
		if isBrowserHarnessTool(payload.ToolName) && payload.ToolName != finishCaseToolName {
			record.Operation = browserToolOperationResult(ref, ev, record.Attempt.OperationID, payload)
		}
	case piharness.EventObservation:
		var payload piharness.ObservationPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return err
		}
		toolAttempt, err := p.Store.ToolAttemptByCallID(ctx, ref.RunID, ref.ID, payload.ToolCallID, ev.Sequence)
		if err != nil {
			return err
		}
		if toolAttempt.SpanID == "" || toolAttempt.SpanAttemptID == "" {
			return errors.New("browser.observation has no matching tool attempt")
		}
		spanID := stableID("span", ref.RunID, ref.ID, "observation", payload.ToolCallID, fmt.Sprintf("%d", ev.Sequence))
		now := record.Event.OccurredAt
		record.Span = &domain.Span{
			ID: spanID, RunID: ref.RunID, TraceID: traceID(ref.RunID), SpanID: spanID,
			ParentSpanID: toolAttempt.SpanID, Kind: domain.SpanOperation,
			Name: "observation", Status: "succeeded", StartedAt: now, EndedAt: &now,
			Output: append(json.RawMessage(nil), ev.Payload...),
		}
	case piharness.EventCaseCompleted:
		finishAttempt, err := p.Store.LatestCompletedToolAttempt(ctx, ref.RunID, ref.ID, finishCaseToolName, ev.Sequence)
		if err != nil {
			return err
		}
		if finishAttempt.SpanID == "" || finishAttempt.SpanAttemptID == "" {
			return errors.New("browser.case.completed has no completed finish_case attempt")
		}
		var payload piharness.CaseCompletedPayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			return err
		}
		verdict := payload.Verdict
		if verdict == "" {
			verdict = "unknown"
		}
		operationID := finishAttempt.OperationID
		if operationID == "" {
			operationID = stableID("operation", ref.RunID, ref.ID, "case")
		}
		record.Operation = &postgres.TraceOperationResult{
			ID:          stableID("result", ref.ID, fmt.Sprintf("%d", ev.Sequence)),
			OperationID: operationID, Status: verdict,
			Result: append(json.RawMessage(nil), payload.DryRun...), Artifacts: deriveBrowserStepArtifacts(payload.DryRun),
		}
		record.Attempt = &postgres.TraceAttempt{ID: finishAttempt.SpanAttemptID, AttemptNumber: 1}
	}
	return nil
}

func deriveBrowserStepArtifacts(dryRun json.RawMessage) json.RawMessage {
	var decoded struct {
		Steps []struct {
			Evidence struct {
				ScreenshotPath string `json:"screenshot_path"`
			} `json:"evidence"`
		} `json:"steps"`
	}
	_ = json.Unmarshal(dryRun, &decoded)
	paths := make([]string, 0, len(decoded.Steps))
	seen := make(map[string]struct{}, len(decoded.Steps))
	for _, step := range decoded.Steps {
		path := step.Evidence.ScreenshotPath
		if path == "" {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	encoded, _ := json.Marshal(map[string][]string{"screenshot_paths": paths})
	return encoded
}

func isBrowserHarnessTool(toolName string) bool {
	return strings.HasPrefix(toolName, browserHarnessToolPrefix)
}

func browserToolOperationResult(ref HarnessAttemptRef, ev piharness.Event, operationID string, payload piharness.ToolCompletedPayload) *postgres.TraceOperationResult {
	result := append(json.RawMessage(nil), payload.StructuredResult...)
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	var structured struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(result, &structured)
	status := "passed"
	if payload.IsError || !structured.OK {
		status = "failed"
	}
	var errorEvidence json.RawMessage
	if status == "failed" {
		errorCode := strings.TrimSpace(structured.Error)
		if errorCode == "" {
			if payload.IsError {
				errorCode = "tool_transport_error"
			} else {
				errorCode = "tool_failed"
			}
		}
		detail := strings.TrimSpace(structured.Detail)
		if detail == "" {
			if payload.IsError {
				detail = "tool completion transport marked error"
			} else {
				detail = "tool completion has no successful structured result"
			}
		}
		errorEvidence, _ = json.Marshal(map[string]string{"error": errorCode, "detail": detail})
	}
	return &postgres.TraceOperationResult{
		ID:          stableID("result", ref.ID, fmt.Sprintf("%d", ev.Sequence)),
		OperationID: operationID, Status: status, Result: result,
		Artifacts: deriveBrowserToolArtifacts(result), Error: errorEvidence,
	}
}

func deriveBrowserToolArtifacts(result json.RawMessage) json.RawMessage {
	var decoded struct {
		Page struct {
			ScreenshotPath string `json:"screenshot_path"`
		} `json:"page"`
	}
	_ = json.Unmarshal(result, &decoded)
	paths := make([]string, 0, 1)
	if path := strings.TrimSpace(decoded.Page.ScreenshotPath); path != "" {
		paths = append(paths, path)
	}
	encoded, _ := json.Marshal(map[string][]string{"screenshot_paths": paths})
	return encoded
}
