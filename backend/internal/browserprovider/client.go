// Package browserprovider connects the control plane to a deployed browser
// provider. Provider addresses and credentials never come from a user snapshot.
package browserprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/postgres"
)

type Client struct {
	base  string
	token string
	http  *http.Client
}

func New(address, token string) (*Client, error) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || token == "" {
		return nil, errors.New("browser provider requires a configured HTTP address and internal identity")
	}
	return &Client{base: strings.TrimRight(u.String(), "/"), token: token,
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) WorkerID() string {
	if c == nil {
		return ""
	}
	return postgres.BrowserWorkerID(c.base)
}

func (c *Client) request(ctx context.Context, method, path string, input any, generation, fence int64, project, environment string) ([]byte, error) {
	var raw []byte
	var err error
	if input != nil {
		raw, err = json.Marshal(input)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if project != "" {
		req.Header.Set("X-Project-Id", project)
	}
	if environment != "" {
		req.Header.Set("X-Environment-Id", environment)
	}
	if generation > 0 {
		req.Header.Set("X-Profile-Generation", strconv.FormatInt(generation, 10))
		req.Header.Set("X-Fencing-Token", strconv.FormatInt(fence, 10))
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("browser provider request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(body) > 8<<20 {
		return nil, errors.New("browser provider response exceeds limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("browser provider returned HTTP %d", response.StatusCode)
	}
	return body, nil
}

func (c *Client) ApplyEnvironment(ctx context.Context, command postgres.EnvironmentCommand) (postgres.EnvironmentProviderResult, error) {
	body := map[string]any{"profile_generation": command.ProfileGeneration, "fencing_token": command.FencingToken}
	method, path := "POST", "/environments/"+url.PathEscape(command.EnvironmentID)
	switch command.Kind {
	case "provision":
		path += "/provision"
		body["policy"] = command.Policy
		body["project_id"] = command.ProjectID
	case "reset":
		path += "/reset"
	case "delete":
		method = "DELETE"
	default:
		return postgres.EnvironmentProviderResult{}, errors.New("unknown browser lifecycle command")
	}
	raw, err := c.request(ctx, method, path, body, 0, 0, command.ProjectID, command.EnvironmentID)
	if err != nil {
		return postgres.EnvironmentProviderResult{}, err
	}
	var result struct {
		EnvironmentID     string `json:"environment_id"`
		ProfileGeneration int64  `json:"profile_generation"`
		FencingToken      int64  `json:"fencing_token"`
		State             string `json:"state"`
	}
	if json.Unmarshal(raw, &result) != nil || result.EnvironmentID != command.EnvironmentID ||
		result.ProfileGeneration != command.ProfileGeneration || result.FencingToken != command.FencingToken ||
		(command.Kind == "delete" && result.State != "deleted") || (command.Kind != "delete" && result.State != "ready") {
		return postgres.EnvironmentProviderResult{}, errors.New("browser provider identity mismatch")
	}
	return postgres.EnvironmentProviderResult{ProfileRef: "managed:" + command.EnvironmentID}, nil
}

func (c *Client) Open(ctx context.Context, environment postgres.Environment, scope string) (string, error) {
	raw, err := c.request(ctx, "POST", "/sessions", map[string]any{
		"session_id": scope, "project_id": environment.ProjectID, "environment_id": environment.ID,
		"profile_generation": environment.ProfileGeneration, "fencing_token": environment.FencingToken,
	}, 0, 0, environment.ProjectID, environment.ID)
	if err != nil {
		return "", err
	}
	var response struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(raw, &response) != nil || !strings.HasPrefix(response.SessionID, "bsess_") {
		return "", errors.New("invalid browser session handle")
	}
	return response.SessionID, nil
}

func (c *Client) Close(ctx context.Context, session string, generation, fence int64, project, environment string) error {
	_, err := c.request(ctx, "DELETE", "/sessions/"+url.PathEscape(session), nil, generation, fence, project, environment)
	return err
}
func (c *Client) Screenshot(ctx context.Context, environment, session string, generation, fence int64, project string) ([]byte, error) {
	return c.request(ctx, "GET", "/environments/"+url.PathEscape(environment)+"/sessions/"+url.PathEscape(session)+"/screenshot", nil, generation, fence, project, environment)
}
func (c *Client) Input(ctx context.Context, environment, session string, generation, fence int64, input map[string]any, project string) error {
	// Caller cannot override the leased identity.
	input["profile_generation"], input["fencing_token"] = generation, fence
	_, err := c.request(ctx, "POST", "/environments/"+url.PathEscape(environment)+"/sessions/"+url.PathEscape(session)+"/input", input, 0, 0, project, environment)
	return err
}
