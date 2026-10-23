package traceprojection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/domain"
)

// Evidence is the API-neutral operation evidence consumed by Project.
type Evidence struct {
	ID, SpanID, Status       string
	Result, Artifacts, Error json.RawMessage
	CreatedAt, UpdatedAt     time.Time
}

// Node is a display-only child projected from persisted operation evidence.
type Node struct {
	ID               string          `json:"id"`
	ParentSpanID     string          `json:"parent_span_id"`
	SourceEvidenceID string          `json:"source_evidence_id"`
	Kind             string          `json:"kind"`
	Name             string          `json:"name"`
	Status           string          `json:"status"`
	StartedAt        time.Time       `json:"started_at"`
	EndedAt          *time.Time      `json:"ended_at"`
	Data             json.RawMessage `json:"data"`
	Evidence         json.RawMessage `json:"evidence"`
}

type projector func(domain.Span, Evidence) ([]Node, error)

var projectors = map[string]projector{
	"browser_execute_case": projectBrowserSteps,
}

// Project converts the first repository-ordered evidence record for each
// canonical operation span into display-only child nodes.
func Project(spans []domain.Span, evidence []Evidence) []Node {
	firstEvidence := make(map[string]Evidence, len(evidence))
	for _, item := range evidence {
		if _, exists := firstEvidence[item.SpanID]; !exists {
			firstEvidence[item.SpanID] = item
		}
	}

	nodes := make([]Node, 0)
	for _, span := range spans {
		if span.Kind != domain.SpanOperation {
			continue
		}
		item, exists := firstEvidence[span.SpanID]
		if !exists {
			continue
		}

		project, exists := projectors[operationName(span)]
		if !exists {
			nodes = append(nodes, fallbackNode(span, item))
			continue
		}
		projected, err := project(span, item)
		if err != nil {
			nodes = append(nodes, fallbackNode(span, item))
			continue
		}
		nodes = append(nodes, projected...)
	}
	return nodes
}

func operationName(span domain.Span) string {
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeOne(span.Input, &input); err == nil && input.Name != "" {
		return input.Name
	}
	return span.Name
}

func projectBrowserSteps(parent domain.Span, source Evidence) ([]Node, error) {
	var result struct {
		Steps []any `json:"steps"`
	}
	if err := decodeOne(source.Result, &result); err != nil {
		return nil, err
	}
	if result.Steps == nil {
		return nil, fmt.Errorf("steps must be an array")
	}

	nodes := make([]Node, 0, len(result.Steps))
	indexes := make(map[int64]struct{}, len(result.Steps))
	for position, rawStep := range result.Steps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("step %d must be an object", position)
		}

		index, err := nonNegativeInteger(step["index"])
		if err != nil || index != int64(position) {
			return nil, fmt.Errorf("step %d has invalid index", position)
		}
		if _, duplicate := indexes[index]; duplicate {
			return nil, fmt.Errorf("step %d has duplicate index", position)
		}
		indexes[index] = struct{}{}

		action, ok := step["action"].(string)
		if !ok {
			return nil, fmt.Errorf("step %d action must be a string", position)
		}
		status, ok := step["status"].(string)
		if !ok {
			return nil, fmt.Errorf("step %d status must be a string", position)
		}
		startedAtText, ok := step["started_at"].(string)
		if !ok {
			return nil, fmt.Errorf("step %d started_at must be a string", position)
		}
		startedAt, err := time.Parse(time.RFC3339, startedAtText)
		if err != nil {
			return nil, fmt.Errorf("step %d started_at: %w", position, err)
		}
		durationMS, err := nonNegativeInteger(step["duration_ms"])
		if err != nil {
			return nil, fmt.Errorf("step %d duration_ms: %w", position, err)
		}
		if durationMS > math.MaxInt64/int64(time.Millisecond) {
			return nil, fmt.Errorf("step %d duration_ms overflows time.Duration", position)
		}
		stepEvidence, ok := step["evidence"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("step %d evidence must be an object", position)
		}

		data := make(map[string]any, len(step)-3)
		for key, value := range step {
			if key != "evidence" && key != "started_at" && key != "status" {
				data[key] = value
			}
		}
		dataJSON, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("marshal step %d data: %w", position, err)
		}
		evidenceJSON, err := json.Marshal(stepEvidence)
		if err != nil {
			return nil, fmt.Errorf("marshal step %d evidence: %w", position, err)
		}

		endedAt := startedAt.Add(time.Duration(durationMS) * time.Millisecond)
		nodes = append(nodes, Node{
			ID:               fmt.Sprintf("derived:%s:step:%d", parent.SpanID, index),
			ParentSpanID:     parent.SpanID,
			SourceEvidenceID: source.ID,
			Kind:             "step",
			Name:             fmt.Sprintf("Step %d: %s", index+1, action),
			Status:           status,
			StartedAt:        startedAt,
			EndedAt:          &endedAt,
			Data:             dataJSON,
			Evidence:         evidenceJSON,
		})
	}
	return nodes, nil
}

func fallbackNode(parent domain.Span, source Evidence) Node {
	startedAt := parent.StartedAt
	if startedAt.IsZero() {
		startedAt = source.CreatedAt
	}
	endedAt := parent.EndedAt
	if endedAt == nil && !source.UpdatedAt.IsZero() {
		value := source.UpdatedAt
		endedAt = &value
	}

	data, _ := json.Marshal(map[string]json.RawMessage{
		"result": rawOrNull(source.Result),
	})
	projectedEvidence, _ := json.Marshal(map[string]json.RawMessage{
		"artifacts": rawOrNull(source.Artifacts),
		"error":     rawOrNull(source.Error),
	})
	return Node{
		ID:               fmt.Sprintf("derived:%s:unknown_tool", parent.SpanID),
		ParentSpanID:     parent.SpanID,
		SourceEvidenceID: source.ID,
		Kind:             "unknown_tool",
		Name:             "unknown_tool",
		Status:           source.Status,
		StartedAt:        startedAt,
		EndedAt:          endedAt,
		Data:             data,
		Evidence:         projectedEvidence,
	}
}

func decodeOne(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON documents")
		}
		return err
	}
	return nil
}

func nonNegativeInteger(value any) (int64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("must be a number")
	}
	integer, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || integer < 0 {
		return 0, fmt.Errorf("must be a non-negative integer")
	}
	return integer, nil
}

func rawOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}
