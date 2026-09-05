package browserworker

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

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/authn"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type capabilityRequest struct {
	ProjectID      int64           `json:"project_id"`
	ConversationID string          `json:"conversation_id"`
	Arguments      json.RawMessage `json:"arguments"`
}

type capabilityResponse struct {
	Result json.RawMessage `json:"result"`
}

func NewClient(baseURL string, timeout time.Duration) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("Browser Worker URL must be an absolute HTTP URL")
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (c *Client) ExecuteBrowserCapability(
	ctx context.Context,
	capability string,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	return c.executeCapability(
		ctx,
		"/internal/browser-capabilities/"+capability,
		capability,
		projectID,
		conversationID,
		arguments,
	)
}

func (c *Client) executeCapability(
	ctx context.Context,
	path string,
	capability string,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	if projectID < 1 {
		return nil, errors.New("project_id is required for Python capabilities")
	}
	if !json.Valid(arguments) {
		return nil, errors.New("Browser Worker capability arguments must be valid JSON")
	}
	body, err := json.Marshal(capabilityRequest{
		ProjectID:      projectID,
		ConversationID: conversationID,
		Arguments:      arguments,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Browser Worker capability request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create Browser Worker capability request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if identity, ok := authn.FromContext(ctx); ok && identity.Cookie != "" {
		request.Header.Set("Cookie", identity.Cookie)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call Browser Worker capability %q: %w", capability, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("read Browser Worker capability response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf(
			"Browser Worker capability %q returned HTTP %d: %s",
			capability,
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}
	var decoded capabilityResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode Browser Worker capability response: %w", err)
	}
	if len(decoded.Result) == 0 || !json.Valid(decoded.Result) {
		return nil, fmt.Errorf("Browser Worker capability %q returned invalid result", capability)
	}
	return decoded.Result, nil
}
