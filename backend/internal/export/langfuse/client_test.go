package langfuse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIngestionSerializerMatchesCanonicalJSONEncoding(t *testing.T) {
	start := time.Date(2026, 10, 21, 12, 0, 0, 123, time.UTC)
	end := start.Add(time.Second)
	payload := ingestionRequest{Batch: []ingestionEvent{
		{
			ID: "event-trace", Type: "trace-create", Timestamp: start,
			Body: traceBody{
				ID: "trace-1", SessionID: "session-1", Name: "<trace>\u2028",
				Input: []map[string]any{{
					"content": "line\n\"quoted\"",
					"ordinal": int64(1),
				}},
				Output: map[string]any{"ok": true},
				Metadata: map[string]any{
					"nested": map[string]any{"b": 2.5, "a": nil},
					"items":  []any{"one", false},
				},
				Timestamp: start,
			},
		},
		{
			ID: "event-span", Type: "span-create", Timestamp: start,
			Body: observationBody{
				ID: "span-1", TraceID: "trace-1", ParentObservationID: "parent-1",
				Name: "operation & result", Input: map[string]any{"count": float64(3)},
				Output: map[string]any{"done": true}, Metadata: map[string]any{"kind": "operation"},
				StartTime: start, EndTime: &end, Level: "DEFAULT", StatusMessage: "succeeded",
			},
		},
	}}
	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}

	got := serializedIngestionRequest(t, payload)
	if string(got) != string(want) {
		t.Fatalf("serialized JSON differs:\ngot:  %s\nwant: %s", got, want)
	}
	if got, err := countIngestionRequest(payload, len(want)); err != nil || got != len(want) {
		t.Fatalf("count = %d, %v; want %d, nil", got, err, len(want))
	}
	if _, err := countIngestionRequest(payload, len(want)-1); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("boundary error = %v, want ErrRequestTooLarge", err)
	}
}

func TestIngestionSerializerCountsSupportedJSONValuesExactly(t *testing.T) {
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	payload := ingestionRequest{Batch: []ingestionEvent{{
		ID: "event-trace", Type: "trace-create", Timestamp: start,
		Body: traceBody{
			ID: "trace-1", SessionID: "session-1", Name: "trace",
			Input:  []any{1, 2, 3},
			Output: []any(nil),
			Metadata: map[string]any{
				"escaped": "<>&\u2028\u2029",
				"nested":  map[string]any{"b": 2, "a": 1},
				"nil_map": map[string]any(nil),
			},
			Timestamp: start,
		},
	}}}
	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}

	got := serializedIngestionRequest(t, payload)
	if string(got) != string(want) {
		t.Fatalf("serialized JSON differs:\ngot:  %s\nwant: %s", got, want)
	}
	if gotCount, err := countIngestionRequest(payload, len(want)); err != nil || gotCount != len(want) {
		t.Fatalf("count = %d, %v; want %d, nil", gotCount, err, len(want))
	}
}

func TestIngestionSerializerAcceptsRawMessageWithUseNumberAndCanonicalBytes(t *testing.T) {
	raw := json.RawMessage(` { "z": 9007199254740993123456789, "a": [true, null, "<>&"] } `)
	payload := ingestionRequest{Batch: []ingestionEvent{{
		ID: "event-trace", Type: "trace-create",
		Body: traceBody{
			ID: "trace-1", SessionID: "session-1", Name: "trace", Input: raw,
		},
	}}}
	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}

	got := serializedIngestionRequest(t, payload)
	if string(got) != string(want) {
		t.Fatalf("raw message encoding differs:\ngot:  %s\nwant: %s", got, want)
	}
	if !strings.Contains(string(got), `9007199254740993123456789`) {
		t.Fatalf("raw number was not preserved: %s", got)
	}
}

func TestIngestionSerializerCompactsHugeWhitespacePaddedRawMessage(t *testing.T) {
	raw := json.RawMessage(
		strings.Repeat(" ", 1<<20) +
			"{\n" + strings.Repeat("\t", 1<<20) +
			`"value"` + strings.Repeat("\r\n", 1<<19) +
			":" + strings.Repeat(" ", 1<<20) +
			"null" + strings.Repeat("\n", 1<<20) +
			"}" + strings.Repeat(" ", 1<<20),
	)
	payload := rawMessagePayload(raw)
	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}

	gotCount, err := countIngestionRequest(payload, len(want))
	if err != nil {
		t.Fatalf("count whitespace-padded raw message: %v", err)
	}
	if gotCount != len(want) {
		t.Fatalf("count = %d, want %d", gotCount, len(want))
	}
	got := serializedIngestionRequest(t, payload)
	if string(got) != string(want) {
		t.Fatalf("whitespace-padded raw message differs:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestIngestionSerializerRawMessageMatchesStandardJSONEscaping(t *testing.T) {
	raw := json.RawMessage(` { "literal": "<>&` + "\u2028\u2029" + `", "escaped": "\u003c\u003e\u0026" } `)
	payload := rawMessagePayload(raw)
	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}

	got := serializedIngestionRequest(t, payload)
	if string(got) != string(want) {
		t.Fatalf("raw message escaping differs:\ngot:  %s\nwant: %s", got, want)
	}
	if gotCount, err := countIngestionRequest(payload, len(want)); err != nil || gotCount != len(want) {
		t.Fatalf("count = %d, %v; want %d, nil", gotCount, err, len(want))
	}
}

func TestIngestionSerializerAcceptsOnlyValidJSONNumbers(t *testing.T) {
	for _, test := range []struct {
		name  string
		value json.Number
		valid bool
	}{
		{name: "integer", value: json.Number("9007199254740993123456789"), valid: true},
		{name: "decimal exponent", value: json.Number("-1.25e+8"), valid: true},
		{name: "leading zero", value: json.Number("01"), valid: false},
		{name: "literal", value: json.Number("true"), valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := ingestionRequest{Batch: []ingestionEvent{{
				ID: "event-trace", Type: "trace-create",
				Body: traceBody{
					ID: "trace-1", SessionID: "session-1", Name: "trace", Input: test.value,
				},
			}}}
			_, err := countIngestionRequest(payload, DefaultMaxRequestBytes)
			if test.valid && err != nil {
				t.Fatalf("valid json.Number rejected: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("invalid json.Number accepted")
			}
		})
	}
}

func TestIngestionSerializerRejectsMalformedOrTrailingRawMessage(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"unterminated":`),
		json.RawMessage(`{"valid":true} false`),
		json.RawMessage(`["bad escape: \x"]`),
	} {
		if _, err := countIngestionRequest(rawMessagePayload(raw), DefaultMaxRequestBytes); err == nil {
			t.Fatalf("accepted invalid RawMessage %q", raw)
		}
	}
}

func TestIngestionSerializerStopsBeforeHostileTailMarshal(t *testing.T) {
	var calls atomic.Int64
	payload := ingestionRequest{Batch: []ingestionEvent{{
		ID: "event-trace", Type: "trace-create",
		Body: traceBody{
			ID: "trace-1", SessionID: "session-1", Name: "trace",
			Input: []any{
				strings.Repeat("x", 1024),
				marshalTail{calls: &calls},
			},
		},
	}}}

	_, err := countIngestionRequest(payload, 128)
	if !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("count error = %v, want ErrRequestTooLarge", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("hostile tail MarshalJSON calls = %d, want 0", calls.Load())
	}
}

func TestIngestionSerializerRejectsHugeMapBeforeSortingKeys(t *testing.T) {
	value := make(map[string]any, 4096)
	for index := 0; index < 4096; index++ {
		value[strconv.Itoa(index)] = true
	}
	payload := ingestionRequest{Batch: []ingestionEvent{{
		ID: "event-trace", Type: "trace-create",
		Body: traceBody{
			ID: "trace-1", SessionID: "session-1", Name: "trace", Input: value,
		},
	}}}

	var sorts atomic.Int64
	originalSort := sortJSONMapKeys
	sortJSONMapKeys = func(keys []string) {
		sorts.Add(1)
		originalSort(keys)
	}
	t.Cleanup(func() {
		sortJSONMapKeys = originalSort
	})

	_, err := countIngestionRequest(payload, 256)
	if !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("count error = %v, want ErrRequestTooLarge", err)
	}
	if sorts.Load() != 0 {
		t.Fatalf("map key sorts = %d, want 0", sorts.Load())
	}
}

func TestIngestionSerializerRejectsHugeCanonicalRawString(t *testing.T) {
	raw := json.RawMessage(`"` + strings.Repeat(`\u0000`, 1024) + `"`)
	_, err := countIngestionRequest(rawMessagePayload(raw), 256)
	if !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("count error = %v, want ErrRequestTooLarge", err)
	}
}

func TestIngestionSerializerBoundsRawMessageNesting(t *testing.T) {
	for _, test := range []struct {
		name  string
		depth int
		want  error
	}{
		{name: "at limit", depth: maxIngestionJSONDepth},
		{name: "above limit", depth: maxIngestionJSONDepth + 1, want: ErrRequestTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(strings.Repeat("[", test.depth) + "null" + strings.Repeat("]", test.depth))
			_, err := countIngestionRequest(rawMessagePayload(raw), DefaultMaxRequestBytes)
			if !errors.Is(err, test.want) {
				t.Fatalf("count error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestIngestionSerializerStopsRawMessageAtCanonicalOverflow(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`"` + strings.Repeat("x", 1024) + `" malformed hostile tail`),
		json.RawMessage(`"` + strings.Repeat("x", 1024)),
		json.RawMessage("1" + strings.Repeat("0", 1024) + "e"),
	} {
		_, err := countIngestionRequest(rawMessagePayload(raw), 256)
		if !errors.Is(err, ErrRequestTooLarge) {
			t.Fatalf("count error = %v, want ErrRequestTooLarge", err)
		}
	}
}

func TestIngestionSerializerRejectsHugeEscapedString(t *testing.T) {
	payload := ingestionRequest{Batch: []ingestionEvent{{
		ID: "event-trace", Type: "trace-create",
		Body: traceBody{
			ID: "trace-1", SessionID: "session-1", Name: "trace",
			Input: strings.Repeat("<>&", 1024),
		},
	}}}

	if _, err := countIngestionRequest(payload, 256); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("count error = %v, want ErrRequestTooLarge", err)
	}
}

func TestHTTPClientReturnsTypedClassifiedStatusErrors(t *testing.T) {
	for _, test := range []struct {
		status    int
		transient bool
	}{
		{status: http.StatusBadRequest, transient: false},
		{status: http.StatusUnauthorized, transient: false},
		{status: http.StatusNotFound, transient: false},
		{status: http.StatusRequestTimeout, transient: true},
		{status: http.StatusTooManyRequests, transient: true},
		{status: http.StatusServiceUnavailable, transient: true},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			client := newHTTPTestClient(t, server.URL, server.Client())

			err := client.Ingest(context.Background(), minimalBatch())
			var statusErr *HTTPError
			if !errors.As(err, &statusErr) {
				t.Fatalf("ingestion error = %T %v, want *HTTPError", err, err)
			}
			if statusErr.StatusCode != test.status {
				t.Fatalf("HTTP status = %d, want %d", statusErr.StatusCode, test.status)
			}
			if got := IsTransient(err); got != test.transient {
				t.Fatalf("IsTransient(%d) = %v, want %v", test.status, got, test.transient)
			}
		})
	}
}

func TestHTTPClientReturnsAfterEarlyResponseWithoutConsumingBody(t *testing.T) {
	for _, status := range []int{http.StatusAccepted, http.StatusBadRequest} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := newHTTPTestClient(t, "https://langfuse.example", &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: status,
						Body:       io.NopCloser(strings.NewReader("response")),
					}, nil
				}),
			})

			result := make(chan error, 1)
			go func() {
				result <- client.Ingest(context.Background(), minimalBatch())
			}()

			select {
			case err := <-result:
				if status >= http.StatusBadRequest {
					var statusErr *HTTPError
					if !errors.As(err, &statusErr) || statusErr.StatusCode != status {
						t.Fatalf("ingestion error = %v, want HTTP %d", err, status)
					}
					return
				}
				if err != nil {
					t.Fatalf("ingest: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("Ingest did not return after transport stopped reading the request body")
			}
		})
	}
}

func TestHTTPClientCancelsWhenEarlyResponseLeavesWriterBlocked(t *testing.T) {
	started := make(chan struct{})
	client := newHTTPTestClient(t, "https://langfuse.example", &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			close(started)
			<-request.Context().Done()
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Body:       io.NopCloser(strings.NewReader("response")),
			}, nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)
	go func() {
		result <- client.Ingest(ctx, minimalBatch())
	}()
	<-started
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ingestion error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Ingest did not return after cancellation unblocked the writer")
	}
}

func TestHTTPClientClassifiesNetworkFailureAsTransient(t *testing.T) {
	client := newHTTPTestClient(t, "https://langfuse.example", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset")
		}),
	})

	err := client.Ingest(context.Background(), minimalBatch())
	if err == nil {
		t.Fatal("network failure returned nil")
	}
	var statusErr *HTTPError
	if errors.As(err, &statusErr) {
		t.Fatalf("network failure returned HTTP error: %v", statusErr)
	}
	if !IsTransient(err) {
		t.Fatalf("network failure classified permanent: %v", err)
	}
}

func TestHTTPClientRejectsUnsafeJSONValuesBeforeRequest(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	var marshalCalls atomic.Int64
	for _, test := range []struct {
		name      string
		value     any
		wantError string
	}{
		{name: "bytes", value: []byte("secret"), wantError: "[]byte"},
		{name: "json marshaler", value: marshalTail{calls: &marshalCalls}, wantError: "json.Marshaler"},
		{name: "text marshaler", value: textTail{}, wantError: "encoding.TextMarshaler"},
		{name: "binary marshaler", value: binaryTail{}, wantError: "encoding.BinaryMarshaler"},
		{name: "pointer", value: &plainJSONValue{Value: "value"}, wantError: "pointer"},
		{name: "custom string", value: customJSONText("value"), wantError: "custom"},
		{name: "custom slice", value: customJSONList{"value"}, wantError: "custom"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newHTTPTestClient(t, server.URL, server.Client())
			err := client.Ingest(context.Background(), Batch{Trace: Trace{
				ID: "trace-1", SessionID: "session-1", Name: "trace",
				Input: test.value, StartTime: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
			}})
			if err == nil {
				t.Fatal("unsafe JSON value was accepted")
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ingestion error = %v, want %q rejection", err, test.wantError)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("HTTP requests = %d, want 0", requests.Load())
	}
	if marshalCalls.Load() != 0 {
		t.Fatalf("MarshalJSON calls = %d, want 0", marshalCalls.Load())
	}
}

func TestHTTPClientStreamsCountedBytesWithContentLength(t *testing.T) {
	batch := Batch{Trace: Trace{
		ID: "trace-1", SessionID: "session-1", Name: "trace",
		Input: map[string]any{
			"raw": json.RawMessage(`{"large":9007199254740993123456789,"escaped":"<>&"}`),
		},
		StartTime: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
	}}
	payload := ingestionRequest{Batch: []ingestionEvent{traceEvent(batch.Trace)}}
	want := serializedIngestionRequest(t, payload)
	wantCount, err := countIngestionRequest(payload, DefaultMaxRequestBytes)
	if err != nil {
		t.Fatalf("count expected request: %v", err)
	}

	var gotBody []byte
	var gotLength int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var err error
		gotBody, err = io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotLength = request.ContentLength
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := newHTTPTestClient(t, server.URL, server.Client())
	if err := client.Ingest(context.Background(), batch); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if gotLength != int64(wantCount) {
		t.Fatalf("Content-Length = %d, want %d", gotLength, wantCount)
	}
	if string(gotBody) != string(want) {
		t.Fatalf("pipe bytes differ:\ngot:  %s\nwant: %s", gotBody, want)
	}
}

func TestHTTPClientRejectsOversizeEncodedBatchBeforeRequest(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client, err := NewHTTPClient(HTTPConfig{
		BaseURL:         server.URL,
		PublicKey:       "public-test",
		SecretKey:       "secret-test",
		HTTPClient:      server.Client(),
		MaxRequestBytes: 256,
	})
	if err != nil {
		t.Fatalf("create HTTP client: %v", err)
	}
	err = client.Ingest(context.Background(), Batch{
		Trace: Trace{
			ID:        "trace-1",
			SessionID: "session-1",
			Name:      strings.Repeat("x", 1024),
			StartTime: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
		},
	})
	if !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("oversize ingestion error = %v, want ErrRequestTooLarge", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("HTTP requests = %d, want 0", requests.Load())
	}
}

func newHTTPTestClient(t *testing.T, baseURL string, httpClient *http.Client) *HTTPClient {
	t.Helper()
	client, err := NewHTTPClient(HTTPConfig{
		BaseURL:    baseURL,
		PublicKey:  "public-test",
		SecretKey:  "secret-test",
		HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatalf("create HTTP client: %v", err)
	}
	return client
}

func minimalBatch() Batch {
	return Batch{Trace: Trace{
		ID:        "trace-1",
		SessionID: "session-1",
		Name:      "trace",
		StartTime: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
	}}
}

func rawMessagePayload(raw json.RawMessage) ingestionRequest {
	return ingestionRequest{Batch: []ingestionEvent{{
		ID: "event-trace", Type: "trace-create",
		Body: traceBody{
			ID: "trace-1", SessionID: "session-1", Name: "trace", Input: raw,
		},
	}}}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type marshalTail struct {
	calls *atomic.Int64
}

func (tail marshalTail) MarshalJSON() ([]byte, error) {
	tail.calls.Add(1)
	return []byte(`"tail"`), nil
}

type textTail struct{}

func (textTail) MarshalText() ([]byte, error) {
	return []byte("tail"), nil
}

type binaryTail struct{}

func (binaryTail) MarshalBinary() ([]byte, error) {
	return []byte("tail"), nil
}

type plainJSONValue struct {
	Value string
}

type customJSONText string

type customJSONList []string

func serializedIngestionRequest(t *testing.T, payload ingestionRequest) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := &streamingJSONWriter{writer: &buffer}
	if err := writeIngestionRequest(writer, payload); err != nil {
		t.Fatalf("serialize ingestion request: %v", err)
	}
	return buffer.Bytes()
}
