package langfuse

import (
	"bytes"
	"context"
	"encoding"
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
	contentLength, err := countIngestionRequest(payload, c.maxRequestBytes)
	if err != nil {
		return fmt.Errorf("encode Langfuse ingestion batch: %w", err)
	}
	bodyReader, bodyWriter := io.Pipe()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bodyReader)
	if err != nil {
		return errors.New("build Langfuse ingestion request")
	}
	request.ContentLength = int64(contentLength)
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(c.publicKey, c.secretKey)
	writeResult := make(chan error, 1)
	go func() {
		err := writeIngestionRequest(&streamingJSONWriter{writer: bodyWriter}, payload)
		_ = bodyWriter.CloseWithError(err)
		writeResult <- err
	}()

	response, err := c.httpClient.Do(request)
	if err != nil {
		_ = bodyReader.Close()
		<-writeResult
		return fmt.Errorf("request Langfuse ingestion: %w", err)
	}
	defer response.Body.Close()
	if err := <-writeResult; err != nil {
		return fmt.Errorf("stream Langfuse ingestion batch: %w", err)
	}
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

type ingestionJSONWriter interface {
	writeString(string)
	writeByte(byte)
	fail(error)
	errValue() error
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

func (w *boundedJSONCounter) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

func (w *boundedJSONCounter) errValue() error {
	return w.err
}

type streamingJSONWriter struct {
	writer io.Writer
	err    error
}

func (w *streamingJSONWriter) writeString(value string) {
	if w.err != nil {
		return
	}
	written, err := io.WriteString(w.writer, value)
	if err != nil {
		w.err = err
		return
	}
	if written != len(value) {
		w.err = io.ErrShortWrite
	}
}

func (w *streamingJSONWriter) writeByte(value byte) {
	if w.err != nil {
		return
	}
	written, err := w.writer.Write([]byte{value})
	if err != nil {
		w.err = err
		return
	}
	if written != 1 {
		w.err = io.ErrShortWrite
	}
}

func (w *streamingJSONWriter) fail(err error) {
	if w.err == nil {
		w.err = err
	}
}

func (w *streamingJSONWriter) errValue() error {
	return w.err
}

func countIngestionRequest(payload ingestionRequest, limit int) (int, error) {
	counter := &boundedJSONCounter{limit: limit}
	writeIngestionRequest(counter, payload)
	if err := counter.errValue(); err != nil {
		if errors.Is(err, ErrRequestTooLarge) {
			return 0, fmt.Errorf("%w: limit is %d bytes", ErrRequestTooLarge, limit)
		}
		return 0, err
	}
	return counter.length, nil
}

func writeIngestionRequest(writer ingestionJSONWriter, payload ingestionRequest) error {
	writer.writeString(`{"batch":[`)
	for index, item := range payload.Batch {
		if writer.errValue() != nil {
			return writer.errValue()
		}
		if index > 0 {
			writer.writeByte(',')
		}
		writeIngestionEvent(writer, item)
	}
	writer.writeString(`]}`)
	return writer.errValue()
}

func writeIngestionEvent(writer ingestionJSONWriter, item ingestionEvent) {
	if writer.errValue() != nil {
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
		writer.fail(fmt.Errorf("unsupported Langfuse ingestion body %T", item.Body))
	}
	writer.writeByte('}')
}

func writeTraceBody(writer ingestionJSONWriter, body traceBody) {
	if writer.errValue() != nil {
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

func writeObservationBody(writer ingestionJSONWriter, body observationBody) {
	if writer.errValue() != nil {
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

func writeJSONField(writer ingestionJSONWriter, first *bool, name string, value any) {
	if writer.errValue() != nil {
		return
	}
	writeFieldName(writer, first, name)
	writeJSONValue(writer, value)
}

func writeTimeField(writer ingestionJSONWriter, first *bool, name string, value time.Time) {
	if writer.errValue() != nil {
		return
	}
	writeFieldName(writer, first, name)
	raw, err := value.MarshalJSON()
	if err != nil {
		writer.fail(fmt.Errorf("encode Langfuse timestamp: %w", err))
		return
	}
	writer.writeString(string(raw))
}

func writeFieldName(writer ingestionJSONWriter, first *bool, name string) {
	if writer.errValue() != nil {
		return
	}
	if !*first {
		writer.writeByte(',')
	}
	*first = false
	writeJSONString(writer, name)
	writer.writeByte(':')
}

func writeJSONValue(writer ingestionJSONWriter, value any) {
	if writer.errValue() != nil {
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
		writeJSONNumber(writer, typed)
	case json.RawMessage:
		writeRawJSONMessage(writer, typed)
	case []byte:
		writer.fail(errors.New("unsupported Langfuse JSON []byte value"))
	case json.Marshaler:
		writer.fail(fmt.Errorf("unsupported Langfuse json.Marshaler value %T", value))
	case encoding.TextMarshaler:
		writer.fail(fmt.Errorf("unsupported Langfuse encoding.TextMarshaler value %T", value))
	case encoding.BinaryMarshaler:
		writer.fail(fmt.Errorf("unsupported Langfuse encoding.BinaryMarshaler value %T", value))
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
			if writer.errValue() != nil {
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
			if writer.errValue() != nil {
				return
			}
			if index > 0 {
				writer.writeByte(',')
			}
			writeJSONMap(writer, item)
		}
		writer.writeByte(']')
	default:
		writeUnsupportedJSONValue(writer, value)
	}
}

func writeUnsupportedJSONValue(writer ingestionJSONWriter, value any) {
	valueType := reflect.TypeOf(value)
	switch {
	case valueType.Kind() == reflect.Pointer:
		writer.fail(fmt.Errorf("unsupported Langfuse JSON pointer value %T", value))
	case valueType.PkgPath() != "":
		writer.fail(fmt.Errorf("unsupported Langfuse JSON custom type %T", value))
	default:
		writer.fail(fmt.Errorf("unsupported Langfuse JSON value %T", value))
	}
}

func writeJSONNumber(writer ingestionJSONWriter, value json.Number) {
	raw := value.String()
	if !isJSONNumber(raw) {
		writer.fail(fmt.Errorf("invalid JSON number %q", raw))
		return
	}
	writer.writeString(raw)
}

func isJSONNumber(value string) bool {
	index := 0
	if index < len(value) && value[index] == '-' {
		index++
	}
	if index == len(value) {
		return false
	}
	if value[index] == '0' {
		index++
	} else if value[index] >= '1' && value[index] <= '9' {
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
	} else {
		return false
	}
	if index < len(value) && value[index] == '.' {
		index++
		fractionStart := index
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
		if index == fractionStart {
			return false
		}
	}
	if index < len(value) && (value[index] == 'e' || value[index] == 'E') {
		index++
		if index < len(value) && (value[index] == '+' || value[index] == '-') {
			index++
		}
		exponentStart := index
		for index < len(value) && value[index] >= '0' && value[index] <= '9' {
			index++
		}
		if index == exponentStart {
			return false
		}
	}
	return index == len(value)
}

func writeRawJSONMessage(writer ingestionJSONWriter, raw json.RawMessage) {
	if raw == nil {
		writer.writeString("null")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	writeDecodedJSONValue(writer, decoder)
	if writer.errValue() != nil {
		return
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			writer.fail(errors.New("invalid Langfuse json.RawMessage: multiple JSON values"))
			return
		}
		writer.fail(fmt.Errorf("invalid Langfuse json.RawMessage: %w", err))
	}
}

func writeDecodedJSONValue(writer ingestionJSONWriter, decoder *json.Decoder) {
	token, err := decoder.Token()
	if err != nil {
		writer.fail(fmt.Errorf("invalid Langfuse json.RawMessage: %w", err))
		return
	}
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			writer.writeByte('{')
			first := true
			for decoder.More() {
				if writer.errValue() != nil {
					return
				}
				key, err := decoder.Token()
				if err != nil {
					writer.fail(fmt.Errorf("invalid Langfuse json.RawMessage: %w", err))
					return
				}
				name, ok := key.(string)
				if !ok {
					writer.fail(errors.New("invalid Langfuse json.RawMessage object key"))
					return
				}
				if !first {
					writer.writeByte(',')
				}
				first = false
				writeJSONString(writer, name)
				writer.writeByte(':')
				writeDecodedJSONValue(writer, decoder)
			}
			if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
				writer.fail(errors.New("invalid Langfuse json.RawMessage object"))
				return
			}
			writer.writeByte('}')
		case '[':
			writer.writeByte('[')
			first := true
			for decoder.More() {
				if writer.errValue() != nil {
					return
				}
				if !first {
					writer.writeByte(',')
				}
				first = false
				writeDecodedJSONValue(writer, decoder)
			}
			if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
				writer.fail(errors.New("invalid Langfuse json.RawMessage array"))
				return
			}
			writer.writeByte(']')
		default:
			writer.fail(errors.New("invalid Langfuse json.RawMessage delimiter"))
		}
	case nil:
		writer.writeString("null")
	case bool:
		writer.writeString(strconv.FormatBool(typed))
	case string:
		writeJSONString(writer, typed)
	case json.Number:
		writeJSONNumber(writer, typed)
	default:
		writer.fail(fmt.Errorf("invalid Langfuse json.RawMessage token %T", token))
	}
}

func writeJSONFloat(writer ingestionJSONWriter, value float64, bits int) {
	if writer.errValue() != nil {
		return
	}
	if math.IsInf(value, 0) || math.IsNaN(value) {
		writer.fail(fmt.Errorf("unsupported Langfuse JSON number %v", value))
		return
	}
	writer.writeString(strconv.FormatFloat(value, 'g', -1, bits))
}

func writeJSONMap(writer ingestionJSONWriter, value map[string]any) {
	if writer.errValue() != nil {
		return
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	writer.writeByte('{')
	for index, key := range keys {
		if writer.errValue() != nil {
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

func writeJSONString(writer ingestionJSONWriter, value string) {
	if writer.errValue() != nil {
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
