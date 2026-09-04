package tools

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeCapabilityClient struct {
	capability     string
	projectID      int64
	conversationID string
	arguments      json.RawMessage
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
