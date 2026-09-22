// Package worker 是 Python 执行器的 HTTP 客户端。
//
// 它只做传输，不含任何业务判断：case 的合法性与语义由 contract 决定。
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

// Client 是执行器客户端。
type Client struct {
	baseURL string
	http    *http.Client
}

// New 创建客户端。执行整个 case 可能较慢，因此超时较宽松。
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8100"
	}
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// BaseURL 返回执行器地址，供 /api/health 展示。
func (c *Client) BaseURL() string { return c.baseURL }

// Error 是执行器返回的结构化错误。
type Error struct {
	StatusCode int
	Code       string
	Detail     string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("worker error %d: %s", e.StatusCode, e.Code)
	}
	return fmt.Sprintf("worker error %d: %s: %s", e.StatusCode, e.Code, e.Detail)
}

// Health 是执行器的探活结果。
//
// ArtifactsDir 用来和本进程的证据目录对账：截图由控制面的 `/artifacts/` 提供，
// 两个目录不一致时图片会静默 404。
type Health struct {
	Status       string `json:"status"`
	ArtifactsDir string `json:"artifacts_dir"`
}

// Health 探活。
func (c *Client) Health(ctx context.Context) (Health, error) {
	var payload Health
	if err := c.do(ctx, http.MethodGet, "/health", nil, &payload); err != nil {
		return Health{}, err
	}
	return payload, nil
}

// OpenSession 开一个作者态浏览器会话。
//
// sessionID 是领域会话（CONTRACT §9）：执行器据此把观测截图落进该会话的产物目录。
func (c *Client) OpenSession(ctx context.Context, sessionID string) (string, error) {
	var session contract.Session
	body := contract.OpenSessionRequest{SessionID: sessionID}
	if err := c.do(ctx, http.MethodPost, "/sessions", body, &session); err != nil {
		return "", err
	}
	if session.SessionID == "" {
		return "", fmt.Errorf("worker returned an empty session id")
	}
	return session.SessionID, nil
}

// CloseSession 关闭会话，失败不影响主流程。
func (c *Client) CloseSession(ctx context.Context, sessionID string) error {
	return c.do(ctx, http.MethodDelete, "/sessions/"+sessionID, nil, nil)
}

// Navigate 在会话里导航并返回观测。
func (c *Client) Navigate(ctx context.Context, sessionID, url string) (contract.Observation, error) {
	var observation contract.Observation
	body := contract.NavigateRequest{URL: url}
	if err := c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/navigate", body, &observation); err != nil {
		return contract.Observation{}, err
	}
	return observation, nil
}

// Act 在会话里执行一个动作（用于让模型走到下一页）并返回新观测。
func (c *Client) Act(
	ctx context.Context, sessionID string, request contract.ActRequest,
) (contract.Observation, error) {
	var observation contract.Observation
	if err := c.do(ctx, http.MethodPost, "/sessions/"+sessionID+"/act", request, &observation); err != nil {
		return contract.Observation{}, err
	}
	return observation, nil
}

// Execute 在全新上下文里执行整个 case。
//
// sessionID 是领域会话：每步截图的路径形如 `<session_id>/exec_..._0.png`（CONTRACT §9.2）。
func (c *Client) Execute(
	ctx context.Context, sessionID string, artifact contract.Case,
) (contract.ExecutionResult, error) {
	var result contract.ExecutionResult
	body := contract.ExecuteRequest{SessionID: sessionID, Case: artifact}
	if err := c.do(ctx, http.MethodPost, "/execute", body, &result); err != nil {
		return contract.ExecutionResult{}, err
	}
	return result, nil
}

func (c *Client) do(
	ctx context.Context, method, path string, request any, response any,
) error {
	var reader io.Reader
	if request != nil {
		raw, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if request != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := c.http.Do(httpRequest)
	if err != nil {
		return &Error{StatusCode: 0, Code: string(contract.SignalWorkerError), Detail: err.Error()}
	}
	defer httpResponse.Body.Close()
	raw, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return &Error{StatusCode: httpResponse.StatusCode, Code: string(contract.SignalWorkerError), Detail: err.Error()}
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		var workerError contract.WorkerError
		if err := json.Unmarshal(raw, &workerError); err == nil && workerError.Error != "" {
			return &Error{
				StatusCode: httpResponse.StatusCode,
				Code:       workerError.Error,
				Detail:     workerError.Detail,
			}
		}
		return &Error{
			StatusCode: httpResponse.StatusCode,
			Code:       string(contract.SignalWorkerError),
			Detail:     string(raw),
		}
	}
	if response == nil {
		return nil
	}
	if err := json.Unmarshal(raw, response); err != nil {
		return &Error{
			StatusCode: httpResponse.StatusCode,
			Code:       string(contract.SignalWorkerError),
			Detail:     fmt.Sprintf("decode worker response: %v", err),
		}
	}
	return nil
}
