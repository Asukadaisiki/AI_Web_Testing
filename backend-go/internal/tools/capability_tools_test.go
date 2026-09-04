package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type fakeCapabilityClient struct {
	capability      string
	projectID       int64
	conversationID  string
	arguments       json.RawMessage
	reportResponses []json.RawMessage
	reportCalls     int
}

func (c *fakeCapabilityClient) ExecuteBrowserCapability(
	_ context.Context,
	capability string,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = capability
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	return json.RawMessage(`{"ok":true}`), nil
}

func (c *fakeCapabilityClient) GenerateDSL(
	_ context.Context,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = "generate_dsl"
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	return json.RawMessage(`{"generation_id":1}`), nil
}

func (c *fakeCapabilityClient) ExecuteDSL(
	_ context.Context,
	runID string,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = "execute_dsl"
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	return json.RawMessage(`{"batch_id":3,"status":"pending"}`), nil
}

func (c *fakeCapabilityClient) GetReport(
	_ context.Context,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = "get_report"
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	c.reportCalls++
	if len(c.reportResponses) > 0 {
		response := c.reportResponses[0]
		c.reportResponses = c.reportResponses[1:]
		return response, nil
	}
	return json.RawMessage(`{"id":3,"status":"passed"}`), nil
}

func TestGetReportToolWaitsForTerminalStatus(t *testing.T) {
	client := &fakeCapabilityClient{
		reportResponses: []json.RawMessage{
			json.RawMessage(`{"id":3,"status":"running"}`),
			json.RawMessage(`{"id":3,"status":"passed"}`),
		},
	}
	handler := NewGetReportTool(client)
	handler.pollInterval = time.Millisecond
	handler.maxWait = time.Second
	result, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "get_report",
		Arguments:      json.RawMessage(`{"batch_id":3}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if client.reportCalls != 2 {
		t.Fatalf("report calls = %d, want 2", client.reportCalls)
	}
	if string(result.Content) != `{"id":3,"status":"passed"}` {
		t.Fatalf("result = %s", result.Content)
	}
}

func TestBrowserToolForwardsRunContext(t *testing.T) {
	client := &fakeCapabilityClient{}
	handler := NewBrowserTools(client)[0]
	_, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "explore_page",
		Arguments:      json.RawMessage(`{"url":"https://example.com"}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if client.capability != "explore_page" ||
		client.projectID != 7 ||
		client.conversationID != "11" {
		t.Fatalf("forwarded context = %#v", client)
	}
}

func TestGenerateDSLToolForwardsRunContext(t *testing.T) {
	client := &fakeCapabilityClient{}
	handler := NewGenerateDSLTool(client)
	result, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "generate_dsl",
		Arguments:      json.RawMessage(`{"prompt":"test"}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if client.capability != "generate_dsl" || string(result.Content) != `{"generation_id":1}` {
		t.Fatalf("result = %s, client = %#v", result.Content, client)
	}
}

func TestExecuteDSLToolRequiresMatchingApproval(t *testing.T) {
	client := &fakeCapabilityClient{}
	handler := NewExecuteDSLTool(client)
	generationID := int64(8)
	call := Call{
		RunID:                "run-1",
		ProjectID:            7,
		ConversationID:       "11",
		Name:                 "execute_dsl",
		Arguments:            json.RawMessage(`{"generation_id":8}`),
		LatestGenerationID:   &generationID,
		ApprovedGenerationID: nil,
	}
	if _, err := handler.Execute(context.Background(), call); err == nil {
		t.Fatal("Execute() error = nil, want approval error")
	}

	call.ApprovedGenerationID = &generationID
	result, err := handler.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Artifact == nil || result.Artifact.Type != "execution_batch" || result.Artifact.ID != "3" {
		t.Fatalf("artifact = %#v", result.Artifact)
	}
}
