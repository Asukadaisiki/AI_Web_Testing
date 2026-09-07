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
)

type Client struct {
	baseURL         string
	httpClient      *http.Client
	contextResolver ContextResolver
}

type ContextResolver func(
	ctx context.Context,
	actorUserID int64,
	projectID int64,
	conversationID string,
) (map[string]any, error)

type capabilityRequest struct {
	ActorUserID    int64           `json:"actor_user_id"`
	ProjectID      int64           `json:"project_id"`
	ConversationID string          `json:"conversation_id"`
	Context        map[string]any  `json:"context,omitempty"`
	Arguments      json.RawMessage `json:"arguments"`
}

type capabilityResponse struct {
	Result json.RawMessage `json:"result"`
}

type BrowserExecutionRequest struct {
	ExecutionID int64             `json:"execution_id"`
	DSLCase     json.RawMessage   `json:"dsl_case"`
	BaseURL     string            `json:"base_url,omitempty"`
	InputValues map[string]string `json:"input_values,omitempty"`
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

func (c *Client) SetContextResolver(resolver ContextResolver) {
	c.contextResolver = resolver
}

func (c *Client) ExecuteBrowserCase(
	ctx context.Context,
	request BrowserExecutionRequest,
) (json.RawMessage, error) {
	if request.ExecutionID < 1 {
		return nil, errors.New("execution_id is required for Browser Worker execution")
	}
	if len(request.DSLCase) == 0 || !json.Valid(request.DSLCase) {
		return nil, errors.New("Browser Worker execution requires a valid DSL case")
	}
	if request.InputValues == nil {
		request.InputValues = map[string]string{}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode Browser Worker execution request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/internal/browser-executions",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("create Browser Worker execution request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("call Browser Worker execution: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("read Browser Worker execution response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf(
			"Browser Worker execution returned HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}
	if len(responseBody) == 0 || !json.Valid(responseBody) {
		return nil, errors.New("Browser Worker execution returned invalid JSON")
	}
	return responseBody, nil
}

func (c *Client) ExecuteBrowserCapability(
	ctx context.Context,
	capability string,
	actorUserID int64,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	return c.executeCapability(
		ctx,
		"/internal/browser-capabilities/"+capability,
		capability,
		actorUserID,
		projectID,
		conversationID,
		arguments,
	)
}

func (c *Client) executeCapability(
	ctx context.Context,
	path string,
	capability string,
	actorUserID int64,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	if actorUserID < 1 || projectID < 1 {
		return nil, errors.New("actor_user_id and project_id are required for Browser Worker capabilities")
	}
	if !json.Valid(arguments) {
		return nil, errors.New("Browser Worker capability arguments must be valid JSON")
	}
	var browserContext map[string]any
	if c.contextResolver != nil {
		resolved, err := c.contextResolver(ctx, actorUserID, projectID, conversationID)
		if err != nil {
			return nil, fmt.Errorf("resolve Browser Worker context: %w", err)
		}
		browserContext = resolved
	}
	body, err := json.Marshal(capabilityRequest{
		ActorUserID:    actorUserID,
		ProjectID:      projectID,
		ConversationID: conversationID,
		Context:        browserContext,
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
