package operation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type httpOperation struct {
	definition Definition
	schema     schema
}

// HTTPDispatcher validates calls against server-owned definitions before
// invoking their approved HTTP callbacks.
type HTTPDispatcher struct {
	client     *http.Client
	operations map[string]httpOperation
}

// NewHTTPDispatcher constructs a dispatcher whose callbacks must all appear in
// the server profile's explicit allowlist.
func NewHTTPDispatcher(config HTTPConfig) (*HTTPDispatcher, error) {
	approved := make(map[string]struct{}, len(config.ApprovedCallbackURLs))
	for _, callbackURL := range config.ApprovedCallbackURLs {
		normalized, err := validateCallbackURL(callbackURL)
		if err != nil {
			return nil, fmt.Errorf("approved callback URL: %w", err)
		}
		approved[normalized] = struct{}{}
	}
	if len(approved) == 0 {
		return nil, errors.New("at least one approved callback URL is required")
	}

	operations := make(map[string]httpOperation, len(config.Operations))
	for _, definition := range config.Operations {
		if strings.TrimSpace(definition.Name) == "" {
			return nil, errors.New("operation name is required")
		}
		if _, exists := operations[definition.Name]; exists {
			return nil, fmt.Errorf("duplicate operation %q", definition.Name)
		}
		callbackURL, err := validateCallbackURL(definition.CallbackURL)
		if err != nil {
			return nil, fmt.Errorf("operation %q callback: %w", definition.Name, err)
		}
		if _, exists := approved[callbackURL]; !exists {
			return nil, fmt.Errorf(
				"operation %q callback URL %q is not approved by the server profile",
				definition.Name,
				callbackURL,
			)
		}
		if definition.Timeout <= 0 {
			return nil, fmt.Errorf("operation %q timeout must be positive", definition.Name)
		}
		if definition.MaxAttempts < 1 {
			return nil, fmt.Errorf("operation %q max attempts must be at least 1", definition.Name)
		}
		if definition.RetryBackoff < 0 {
			return nil, fmt.Errorf("operation %q retry backoff cannot be negative", definition.Name)
		}
		if definition.MaxResultBytes < 1 {
			return nil, fmt.Errorf("operation %q max result bytes must be positive", definition.Name)
		}
		compiled, err := compileSchema(definition.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("operation %q: %w", definition.Name, err)
		}
		definition.CallbackURL = callbackURL
		operations[definition.Name] = httpOperation{definition: definition, schema: compiled}
	}
	if len(operations) == 0 {
		return nil, errors.New("at least one operation is required")
	}

	client := config.Client
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &HTTPDispatcher{client: &clientCopy, operations: operations}, nil
}

// Dispatch validates the typed arguments locally, then retries transient HTTP
// failures while preserving the operation's stable idempotency identity.
func (d *HTTPDispatcher) Dispatch(ctx context.Context, call Call) (Result, error) {
	if d == nil {
		return Result{}, errors.New("HTTP operation dispatcher is not configured")
	}
	operation, exists := d.operations[call.Name]
	if !exists {
		return Result{}, fmt.Errorf("operation %q is not in the server profile", call.Name)
	}
	if err := validateCallIdentity(call); err != nil {
		return Result{}, err
	}
	if err := operation.schema.validateRaw(call.Arguments); err != nil {
		return Result{}, fmt.Errorf("operation %q: %w", call.Name, err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, operation.definition.Timeout)
	defer cancel()

	var (
		attempts int
		lastErr  error
	)
	for attempt := 1; attempt <= operation.definition.MaxAttempts; attempt++ {
		attempts = attempt
		result, retry, err := d.dispatchAttempt(timeoutCtx, operation.definition, call)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retry || attempt == operation.definition.MaxAttempts {
			break
		}
		if err := waitForRetry(timeoutCtx, operation.definition.RetryBackoff); err != nil {
			return Result{}, fmt.Errorf("operation %q retry: %w", call.Name, err)
		}
	}
	return Result{}, fmt.Errorf(
		"operation %q failed after %d attempt(s): %w",
		call.Name,
		attempts,
		lastErr,
	)
}

func (d *HTTPDispatcher) dispatchAttempt(
	ctx context.Context,
	definition Definition,
	call Call,
) (Result, bool, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		definition.CallbackURL,
		bytes.NewReader(call.Arguments),
	)
	if err != nil {
		return Result{}, false, fmt.Errorf("create callback request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", call.OperationID)
	request.Header.Set("X-AgentTrace-Run-ID", call.RunID)
	request.Header.Set("X-AgentTrace-Span-ID", call.SpanID)

	response, err := d.client.Do(request)
	if err != nil {
		return Result{}, true, fmt.Errorf("send callback request: %w", err)
	}
	defer response.Body.Close()

	body, err := readBounded(response.Body, definition.MaxResultBytes)
	if err != nil {
		return Result{}, false, fmt.Errorf("read callback response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, retryableStatus(response.StatusCode), fmt.Errorf(
			"callback returned HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	var result Result
	if err := decodeOneJSON(body, &result, true); err != nil {
		return Result{}, false, fmt.Errorf("decode callback result: %w", err)
	}
	if result.Status != "succeeded" && result.Status != "failed" {
		return Result{}, false, fmt.Errorf("callback result has invalid status %q", result.Status)
	}
	return result, false, nil
}

func validateCallIdentity(call Call) error {
	for name, value := range map[string]string{
		"run ID":       call.RunID,
		"span ID":      call.SpanID,
		"operation ID": call.OperationID,
		"tool call ID": call.ToolCallID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

func validateCallbackURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("%q must be an absolute HTTP URL", value)
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return "", fmt.Errorf("%q must not contain user information or a fragment", value)
	}
	return parsed.String(), nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, nil
}

func retryableStatus(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

func waitForRetry(ctx context.Context, backoff time.Duration) error {
	if backoff == 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var _ Dispatcher = (*HTTPDispatcher)(nil)
