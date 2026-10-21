package langfuse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const DefaultMaxRequestBytes = 1 << 20

// ErrRequestTooLarge is returned before HTTP when the encoded ingestion batch
// exceeds the configured request boundary.
var ErrRequestTooLarge = errors.New("Langfuse ingestion request exceeds maximum encoded size")

// HTTPError is a provider response outside the successful 2xx range.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("Langfuse ingestion returned HTTP %d", e.StatusCode)
}

// IsTransient reports whether another delivery may succeed without changing
// the canonical batch.
func IsTransient(err error) bool {
	var statusErr *HTTPError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == http.StatusRequestTimeout ||
			statusErr.StatusCode == http.StatusTooManyRequests ||
			statusErr.StatusCode >= http.StatusInternalServerError
	}
	var permanent interface{ Permanent() bool }
	if errors.As(err, &permanent) && permanent.Permanent() {
		return false
	}
	return !errors.Is(err, ErrRequestTooLarge)
}

// HTTPConfig configures Langfuse's HTTP ingestion boundary.
type HTTPConfig struct {
	BaseURL         string
	PublicKey       string
	SecretKey       string
	HTTPClient      *http.Client
	MaxRequestBytes int
}

// HTTPClient sends deterministic ingestion batches to Langfuse.
type HTTPClient struct {
	endpoint        string
	publicKey       string
	secretKey       string
	httpClient      *http.Client
	maxRequestBytes int
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
	maxRequestBytes := config.MaxRequestBytes
	if maxRequestBytes == 0 {
		maxRequestBytes = DefaultMaxRequestBytes
	}
	if maxRequestBytes < 0 {
		return nil, errors.New("Langfuse maximum request bytes must be positive")
	}
	return &HTTPClient{
		endpoint:        strings.TrimRight(baseURL, "/") + "/api/public/ingestion",
		publicKey:       config.PublicKey,
		secretKey:       config.SecretKey,
		httpClient:      client,
		maxRequestBytes: maxRequestBytes,
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
	raw, err := encodeIngestionRequest(payload, c.maxRequestBytes)
	if err != nil {
		return fmt.Errorf("encode Langfuse ingestion batch: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return errors.New("build Langfuse ingestion request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(c.publicKey, c.secretKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Langfuse ingestion: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &HTTPError{StatusCode: response.StatusCode}
	}
	return nil
}

type boundedJSONCounter struct {
	limit  int
	length int
	err    error
}

func (w *boundedJSONCounter) writeString(value string) {
	if w.err != nil {
		return
	}
	if len(value) > w.limit-w.length {
		w.err = ErrRequestTooLarge
		return
	}
	w.length += len(value)
}

func (w *boundedJSONCounter) writeByte(value byte) {
	if w.err != nil {
		return
	}
	if w.length == w.limit {
		w.err = ErrRequestTooLarge
		return
	}
	w.length++
}

func encodeIngestionRequest(payload ingestionRequest, limit int) ([]byte, error) {
	counter := &boundedJSONCounter{limit: limit}
	counter.writeString(`{"batch":[`)
	for index, item := range payload.Batch {
		if index > 0 {
			counter.writeByte(',')
		}
		writeIngestionEvent(counter, item)
	}
	counter.writeString(`]}`)
	if counter.err != nil {
		if errors.Is(counter.err, ErrRequestTooLarge) {
			return nil, fmt.Errorf("%w: limit is %d bytes", ErrRequestTooLarge, limit)
		}
		return nil, counter.err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal Langfuse ingestion batch: %w", err)
	}
	if len(raw) != counter.length {
		return nil, errors.New("Langfuse ingestion JSON length counter mismatch")
	}
	return raw, nil
}

func writeIngestionEvent(writer *boundedJSONCounter, item ingestionEvent) {
	if writer.err != nil {
		return
	}
	writer.writeByte('{')
	first := true
	writeJSONField(writer, &first, "id", item.ID)
	writeJSONField(writer, &first, "type", item.Type)
	writeTimeField(writer, &first, "timestamp", item.Timestamp)
	writeFieldName(writer, &first, "body")
	switch body := item.Body.(type) {
	case traceBody:
		writeTraceBody(writer, body)
	case observationBody:
		writeObservationBody(writer, body)
	default:
		writer.err = fmt.Errorf("unsupported Langfuse ingestion body %T", item.Body)
	}
	writer.writeByte('}')
}

func writeTraceBody(writer *boundedJSONCounter, body traceBody) {
	if writer.err != nil {
		return
	}
	writer.writeByte('{')
	first := true
	writeJSONField(writer, &first, "id", body.ID)
	writeJSONField(writer, &first, "sessionId", body.SessionID)
	writeJSONField(writer, &first, "name", body.Name)
	if body.Input != nil {
		writeJSONField(writer, &first, "input", body.Input)
	}
	if body.Output != nil {
		writeJSONField(writer, &first, "output", body.Output)
	}
	if len(body.Metadata) > 0 {
		writeJSONField(writer, &first, "metadata", body.Metadata)
	}
	writeTimeField(writer, &first, "timestamp", body.Timestamp)
	writer.writeByte('}')
}

func writeObservationBody(writer *boundedJSONCounter, body observationBody) {
	if writer.err != nil {
		return
	}
	writer.writeByte('{')
	first := true
	writeJSONField(writer, &first, "id", body.ID)
	writeJSONField(writer, &first, "traceId", body.TraceID)
	if body.ParentObservationID != "" {
		writeJSONField(writer, &first, "parentObservationId", body.ParentObservationID)
	}
	writeJSONField(writer, &first, "name", body.Name)
	if body.Input != nil {
		writeJSONField(writer, &first, "input", body.Input)
	}
	if body.Output != nil {
		writeJSONField(writer, &first, "output", body.Output)
	}
	if len(body.Metadata) > 0 {
		writeJSONField(writer, &first, "metadata", body.Metadata)
	}
	writeTimeField(writer, &first, "startTime", body.StartTime)
	if body.EndTime != nil {
		writeTimeField(writer, &first, "endTime", *body.EndTime)
	}
	if body.Level != "" {
		writeJSONField(writer, &first, "level", body.Level)
	}
	if body.StatusMessage != "" {
		writeJSONField(writer, &first, "statusMessage", body.StatusMessage)
	}
	writer.writeByte('}')
}

func writeJSONField(writer *boundedJSONCounter, first *bool, name string, value any) {
	if writer.err != nil {
		return
	}
	writeFieldName(writer, first, name)
	writeJSONValue(writer, value)
}

func writeTimeField(writer *boundedJSONCounter, first *bool, name string, value time.Time) {
	if writer.err != nil {
		return
	}
	writeFieldName(writer, first, name)
	raw, err := value.MarshalJSON()
	if err != nil {
		writer.err = fmt.Errorf("encode Langfuse timestamp: %w", err)
		return
	}
	writer.writeString(string(raw))
}

func writeFieldName(writer *boundedJSONCounter, first *bool, name string) {
	if writer.err != nil {
		return
	}
	if !*first {
		writer.writeByte(',')
	}
	*first = false
	writeJSONString(writer, name)
	writer.writeByte(':')
}

func writeJSONValue(writer *boundedJSONCounter, value any) {
	if writer.err != nil {
		return
	}
	switch typed := value.(type) {
	case nil:
		writer.writeString("null")
	case bool:
		writer.writeString(strconv.FormatBool(typed))
	case string:
		writeJSONString(writer, typed)
	case float64:
		writeJSONFloat(writer, typed, 64)
	case float32:
		writeJSONFloat(writer, float64(typed), 32)
	case int:
		writer.writeString(strconv.Itoa(typed))
	case int8:
		writer.writeString(strconv.FormatInt(int64(typed), 10))
	case int16:
		writer.writeString(strconv.FormatInt(int64(typed), 10))
	case int64:
		writer.writeString(strconv.FormatInt(typed, 10))
	case int32:
		writer.writeString(strconv.FormatInt(int64(typed), 10))
	case uint:
		writer.writeString(strconv.FormatUint(uint64(typed), 10))
	case uint8:
		writer.writeString(strconv.FormatUint(uint64(typed), 10))
	case uint16:
		writer.writeString(strconv.FormatUint(uint64(typed), 10))
	case uint64:
		writer.writeString(strconv.FormatUint(typed, 10))
	case uint32:
		writer.writeString(strconv.FormatUint(uint64(typed), 10))
	case json.Number:
		raw := typed.String()
		if !json.Valid([]byte(raw)) {
			writer.err = fmt.Errorf("invalid JSON number %q", raw)
			return
		}
		writer.writeString(raw)
	case map[string]any:
		if typed == nil {
			writer.writeString("null")
			return
		}
		writeJSONMap(writer, typed)
	case []any:
		if typed == nil {
			writer.writeString("null")
			return
		}
		writer.writeByte('[')
		for index, item := range typed {
			if writer.err != nil {
				return
			}
			if index > 0 {
				writer.writeByte(',')
			}
			writeJSONValue(writer, item)
		}
		writer.writeByte(']')
	case []map[string]any:
		if typed == nil {
			writer.writeString("null")
			return
		}
		writer.writeByte('[')
		for index, item := range typed {
			if writer.err != nil {
				return
			}
			if index > 0 {
				writer.writeByte(',')
			}
			writeJSONMap(writer, item)
		}
		writer.writeByte(']')
	default:
		writeReflectedJSONValue(writer, reflect.ValueOf(value))
	}
}

func writeReflectedJSONValue(writer *boundedJSONCounter, value reflect.Value) {
	if writer.err != nil {
		return
	}
	if !value.IsValid() {
		writer.writeString("null")
		return
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			writer.writeString("null")
			return
		}
		writeReflectedJSONValue(writer, value.Elem())
	case reflect.Bool:
		writer.writeString(strconv.FormatBool(value.Bool()))
	case reflect.String:
		writeJSONString(writer, value.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		writer.writeString(strconv.FormatInt(value.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		writer.writeString(strconv.FormatUint(value.Uint(), 10))
	case reflect.Float32:
		writeJSONFloat(writer, value.Float(), 32)
	case reflect.Float64:
		writeJSONFloat(writer, value.Float(), 64)
	case reflect.Array, reflect.Slice:
		if value.Kind() == reflect.Slice && value.IsNil() {
			writer.writeString("null")
			return
		}
		writer.writeByte('[')
		for index := 0; index < value.Len(); index++ {
			if writer.err != nil {
				return
			}
			if index > 0 {
				writer.writeByte(',')
			}
			writeReflectedJSONValue(writer, value.Index(index))
		}
		writer.writeByte(']')
	case reflect.Map:
		if value.IsNil() {
			writer.writeString("null")
			return
		}
		if value.Type().Key().Kind() != reflect.String {
			writer.err = fmt.Errorf("unsupported Langfuse JSON map key %s", value.Type().Key())
			return
		}
		keys := value.MapKeys()
		sort.Slice(keys, func(i, j int) bool {
			return keys[i].String() < keys[j].String()
		})
		writer.writeByte('{')
		for index, key := range keys {
			if writer.err != nil {
				return
			}
			if index > 0 {
				writer.writeByte(',')
			}
			writeJSONString(writer, key.String())
			writer.writeByte(':')
			writeReflectedJSONValue(writer, value.MapIndex(key))
		}
		writer.writeByte('}')
	default:
		writer.err = fmt.Errorf("unsupported Langfuse JSON value %s", value.Type())
	}
}

func writeJSONFloat(writer *boundedJSONCounter, value float64, bits int) {
	if writer.err != nil {
		return
	}
	if math.IsInf(value, 0) || math.IsNaN(value) {
		writer.err = fmt.Errorf("unsupported Langfuse JSON number %v", value)
		return
	}
	writer.writeString(strconv.FormatFloat(value, 'g', -1, bits))
}

func writeJSONMap(writer *boundedJSONCounter, value map[string]any) {
	if writer.err != nil {
		return
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	writer.writeByte('{')
	for index, key := range keys {
		if writer.err != nil {
			return
		}
		if index > 0 {
			writer.writeByte(',')
		}
		writeJSONString(writer, key)
		writer.writeByte(':')
		writeJSONValue(writer, value[key])
	}
	writer.writeByte('}')
}

func writeJSONString(writer *boundedJSONCounter, value string) {
	if writer.err != nil {
		return
	}
	const hex = "0123456789abcdef"

	writer.writeByte('"')
	start := 0
	for index := 0; index < len(value); {
		current := value[index]
		if current < utf8.RuneSelf {
			if current >= 0x20 && current != '\\' && current != '"' &&
				current != '<' && current != '>' && current != '&' {
				index++
				continue
			}
			writer.writeString(value[start:index])
			switch current {
			case '\\', '"':
				writer.writeByte('\\')
				writer.writeByte(current)
			case '\b':
				writer.writeString(`\b`)
			case '\f':
				writer.writeString(`\f`)
			case '\n':
				writer.writeString(`\n`)
			case '\r':
				writer.writeString(`\r`)
			case '\t':
				writer.writeString(`\t`)
			default:
				writer.writeString(`\u00`)
				writer.writeByte(hex[current>>4])
				writer.writeByte(hex[current&0xF])
			}
			index++
			start = index
			continue
		}

		runeValue, size := utf8.DecodeRuneInString(value[index:])
		if runeValue == utf8.RuneError && size == 1 {
			writer.writeString(value[start:index])
			writer.writeString(`\ufffd`)
			index++
			start = index
			continue
		}
		if runeValue == '\u2028' || runeValue == '\u2029' {
			writer.writeString(value[start:index])
			writer.writeString(`\u202`)
			writer.writeByte(hex[runeValue&0xF])
			index += size
			start = index
			continue
		}
		index += size
	}
	writer.writeString(value[start:])
	writer.writeByte('"')
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
