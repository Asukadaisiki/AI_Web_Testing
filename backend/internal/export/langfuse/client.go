package langfuse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPConfig configures Langfuse's HTTP ingestion boundary.
type HTTPConfig struct {
	BaseURL    string
	PublicKey  string
	SecretKey  string
	HTTPClient *http.Client
}

// HTTPClient sends deterministic ingestion batches to Langfuse.
type HTTPClient struct {
	endpoint   string
	publicKey  string
	secretKey  string
	httpClient *http.Client
}

// NewHTTPClient validates provider configuration without making a network request.
func NewHTTPClient(config HTTPConfig) (*HTTPClient, error) {
	baseURL := strings.TrimSpace(config.BaseURL)
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("Langfuse base URL must be an absolute HTTP URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("Langfuse base URL must use HTTP or HTTPS")
	}
	if parsed.User != nil {
		return nil, errors.New("Langfuse base URL must not contain credentials")
	}
	if strings.TrimSpace(config.PublicKey) == "" || strings.TrimSpace(config.SecretKey) == "" {
		return nil, errors.New("Langfuse public and secret keys are required")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPClient{
		endpoint:   strings.TrimRight(baseURL, "/") + "/api/public/ingestion",
		publicKey:  config.PublicKey,
		secretKey:  config.SecretKey,
		httpClient: client,
	}, nil
}

// Ingest converts the transport-independent batch to Langfuse ingestion events.
func (c *HTTPClient) Ingest(ctx context.Context, batch Batch) error {
	if c == nil || c.httpClient == nil {
		return errors.New("Langfuse HTTP client is not configured")
	}
	payload := ingestionRequest{Batch: make([]ingestionEvent, 0, 1+len(batch.Observations))}
	payload.Batch = append(payload.Batch, traceEvent(batch.Trace))
	for _, observation := range batch.Observations {
		payload.Batch = append(payload.Batch, observationEvent(observation))
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal Langfuse ingestion batch: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return errors.New("build Langfuse ingestion request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(c.publicKey, c.secretKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return errors.New("request Langfuse ingestion")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Langfuse ingestion returned HTTP %d", response.StatusCode)
	}
	return nil
}

type ingestionRequest struct {
	Batch []ingestionEvent `json:"batch"`
}

type ingestionEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Body      any       `json:"body"`
}

type traceBody struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId"`
	Name      string         `json:"name"`
	Input     any            `json:"input,omitempty"`
	Output    any            `json:"output,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

type observationBody struct {
	ID                  string         `json:"id"`
	TraceID             string         `json:"traceId"`
	ParentObservationID string         `json:"parentObservationId,omitempty"`
	Name                string         `json:"name"`
	Input               any            `json:"input,omitempty"`
	Output              any            `json:"output,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
	StartTime           time.Time      `json:"startTime"`
	EndTime             *time.Time     `json:"endTime,omitempty"`
	Level               string         `json:"level,omitempty"`
	StatusMessage       string         `json:"statusMessage,omitempty"`
}

func traceEvent(trace Trace) ingestionEvent {
	metadata := copyMetadata(trace.Metadata)
	metadata["agenttrace_status"] = trace.Status
	if trace.EndTime != nil {
		metadata["agenttrace_end_time"] = trace.EndTime.UTC().Format(time.RFC3339Nano)
	}
	return ingestionEvent{
		ID:        remoteID("ingestion-trace", trace.ID),
		Type:      "trace-create",
		Timestamp: trace.StartTime,
		Body: traceBody{
			ID:        trace.ID,
			SessionID: trace.SessionID,
			Name:      trace.Name,
			Input:     trace.Input,
			Output:    trace.Output,
			Metadata:  metadata,
			Timestamp: trace.StartTime,
		},
	}
}

func observationEvent(observation Observation) ingestionEvent {
	eventType := "span-create"
	if observation.Type == "generation" {
		eventType = "generation-create"
	}
	return ingestionEvent{
		ID:        remoteID("ingestion-observation", observation.ID),
		Type:      eventType,
		Timestamp: observation.StartTime,
		Body: observationBody{
			ID:                  observation.ID,
			TraceID:             observation.TraceID,
			ParentObservationID: observation.ParentObservationID,
			Name:                observation.Name,
			Input:               observation.Input,
			Output:              observation.Output,
			Metadata:            observation.Metadata,
			StartTime:           observation.StartTime,
			EndTime:             observation.EndTime,
			Level:               statusLevel(observation.Status),
			StatusMessage:       observation.Status,
		},
	}
}

func copyMetadata(metadata map[string]any) map[string]any {
	result := make(map[string]any, len(metadata)+2)
	for key, value := range metadata {
		result[key] = value
	}
	return result
}

func statusLevel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "error":
		return "ERROR"
	case "cancelled", "warning":
		return "WARNING"
	default:
		return "DEFAULT"
	}
}
