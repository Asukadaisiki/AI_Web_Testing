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
	repairResponse  json.RawMessage
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
	_ int64,
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
	_ int64,
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
	_ int64,
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

func (c *fakeCapabilityClient) PrepareFixAndRetry(
	_ context.Context,
	_ int64,
	projectID int64,
	conversationID string,
	arguments json.RawMessage,
) (json.RawMessage, error) {
	c.capability = "fix_and_retry"
	c.projectID = projectID
	c.conversationID = conversationID
	c.arguments = arguments
	if len(c.repairResponse) > 0 {
		return c.repairResponse, nil
	}
	return json.RawMessage(`{"source_batch_id":3,"status":"repair_ready","strategy":"re_explore"}`), nil
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

func TestFixAndRetryPublishesRepairPlan(t *testing.T) {
	client := &fakeCapabilityClient{}
	handler := NewFixAndRetryTool(client)
	result, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "fix_and_retry",
		Arguments:      json.RawMessage(`{"batch_id":3}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Artifact == nil || result.Artifact.Type != "repair_plan" || result.Artifact.ID != "3" {
		t.Fatalf("artifact = %#v", result.Artifact)
	}
}

func TestFixAndRetryDoesNotPublishPlanWhenRepairIsNotRequired(t *testing.T) {
	client := &fakeCapabilityClient{
		repairResponse: json.RawMessage(
			`{"source_batch_id":3,"status":"not_required","strategy":"none"}`,
		),
	}
	handler := NewFixAndRetryTool(client)
	result, err := handler.Execute(context.Background(), Call{
		ProjectID:      7,
		ConversationID: "11",
		Name:           "fix_and_retry",
		Arguments:      json.RawMessage(`{"batch_id":3}`),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Artifact != nil {
		t.Fatalf("artifact = %#v, want nil", result.Artifact)
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
