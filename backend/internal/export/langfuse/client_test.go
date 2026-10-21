package langfuse

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoundedEncoderMatchesCanonicalJSONEncoding(t *testing.T) {
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

	got, err := encodeIngestionRequest(payload, len(want))
	if err != nil {
		t.Fatalf("encode bounded request: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("bounded JSON differs:\ngot:  %s\nwant: %s", got, want)
	}
	if _, err := encodeIngestionRequest(payload, len(want)-1); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("boundary error = %v, want ErrRequestTooLarge", err)
	}
}

func TestBoundedEncoderCountsStandardJSONValuesExactly(t *testing.T) {
	start := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	payload := ingestionRequest{Batch: []ingestionEvent{{
		ID: "event-trace", Type: "trace-create", Timestamp: start,
		Body: traceBody{
			ID: "trace-1", SessionID: "session-1", Name: "trace",
			Input:  []int{1, 2, 3},
			Output: []any(nil),
			Metadata: map[string]any{
				"escaped": "<>&\u2028\u2029",
				"nested":  map[string]int{"b": 2, "a": 1},
				"nil_map": map[string]any(nil),
			},
			Timestamp: start,
		},
	}}}
	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}

	got, err := encodeIngestionRequest(payload, len(want))
	if err != nil {
		t.Fatalf("encode bounded request: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("bounded JSON differs:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestBoundedEncoderStopsBeforeHostileTailMarshal(t *testing.T) {
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

	_, err := encodeIngestionRequest(payload, 128)
	if !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("encode error = %v, want ErrRequestTooLarge", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("hostile tail MarshalJSON calls = %d, want 0", calls.Load())
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
