package research_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agent"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/agentservice"
	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/research"
)

func TestPostgresSourceReaderProjectsRealAgentEvents(t *testing.T) {
	fixture := newPostgresFixture(t, false)
	runService := agentservice.NewService(
		agentservice.NewPostgresRepository(fixture.db),
	)
	agentRun, err := runService.StartOwnedProjectRun(
		fixture.ctx,
		fixture.userID,
		"conversation-"+fixture.suffix,
		fixture.projectID,
		"inspect example.com",
	)
	if err != nil {
		t.Fatal(err)
	}

	inputTokens, outputTokens, totalTokens := int64(10), int64(4), int64(14)
	if recordErr := runService.RecordModelTelemetry(
		fixture.ctx,
		agentRun,
		agent.TelemetryRecord{
			LogicalCallID: "logical-real",
			StepID:        "step-real",
			ToolCallIDs:   []string{"tool-real"},
			Telemetry: agent.ModelTelemetry{
				Provider:       "provider",
				RequestedModel: "model",
				ResolvedModel:  "model-v1",
				FinishReason:   "tool_calls",
				Prompt: agent.PromptSpec{
					Version:       "prompt.v1",
					RequestSHA256: "request-hash",
					PromptSHA256:  "prompt-hash",
				},
				Usage: agent.ModelUsage{
					Status:       agent.UsageAvailable,
					InputTokens:  &inputTokens,
					OutputTokens: &outputTokens,
					TotalTokens:  &totalTokens,
				},
				Attempts: []agent.ModelAttempt{
					{
						Attempt:   1,
						Status:    "failed",
						StartedAt: time.Unix(1, 0).UTC(),
						LatencyMS: 7,
						Error: agent.NewModelError(
							"http", "http_429", "private provider detail", true, nil,
						),
					},
					{
						Attempt:   2,
						Status:    "succeeded",
						StartedAt: time.Unix(2, 0).UTC(),
						LatencyMS: 11,
					},
				},
				TotalLatencyMS: 18,
			},
		},
	); recordErr != nil {
		t.Fatal(recordErr)
	}
	for _, event := range []agentservice.Event{
		{
			Type: agentservice.EventToolStarted, StepID: "step-real",
			ToolCallID: "tool-real", Payload: map[string]any{"tool": "explore_page"},
		},
		{
			Type: agentservice.EventToolArgsDelta, StepID: "step-real",
			ToolCallID: "tool-real",
			Payload: map[string]any{
				"tool":      "explore_page",
				"arguments": `{"url":"https://example.com"}`,
			},
		},
		{
			Type: agentservice.EventToolResult, StepID: "step-real",
			ToolCallID: "tool-real",
			Payload: toolResultEventMap(
				t,
				"explore_page",
				json.RawMessage(`{"status":"ok","url":"https://example.com"}`),
			),
		},
		{
			Type: agentservice.EventToolFinished, StepID: "step-real",
			ToolCallID: "tool-real", Payload: map[string]any{"tool": "explore_page"},
		},
		{
			Type: agentservice.EventType("future.event"), StepID: "step-future",
			ToolCallID: "tool-future", Payload: map[string]any{"future": true},
		},
	} {
		if _, recordErr := runService.RecordEvent(fixture.ctx, agentRun, event); recordErr != nil {
			t.Fatal(recordErr)
		}
	}
	agentRun, err = runService.CompleteRun(fixture.ctx, agentRun)
	if err != nil {
		t.Fatal(err)
	}

	repository := research.NewPostgresRepository(fixture.db)
	experiment := fixture.createExperiment(t, repository, "source-real-events", 1)
	researchRun := fixture.newRun(experiment.ID, "source-real-events", 0)
	if _, createErr := repository.CreateRun(fixture.ctx, researchRun); createErr != nil {
		t.Fatal(createErr)
	}
	if _, linkErr := repository.UpdateRunLinks(
		fixture.ctx,
		researchRun.ID,
		research.RunLinks{AgentRunID: &agentRun.ID},
		time.Time{},
	); linkErr != nil {
		t.Fatal(linkErr)
	}

	reader := research.NewPostgresSourceReader(fixture.db)
	snapshot, err := reader.Read(fixture.ctx, researchRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgentRunStatus != "completed" || len(snapshot.Events) != 9 {
		t.Fatalf("source snapshot status/events = %s/%d", snapshot.AgentRunStatus, len(snapshot.Events))
	}
	if snapshot.Cursor.AgentRunID != agentRun.ID ||
		snapshot.Cursor.AgentEventSeq != int64(len(snapshot.Events)) ||
		len(snapshot.Cursor.ApprovedGenerationIDs) != 0 ||
		len(snapshot.Cursor.BatchIDs) != 0 ||
		len(snapshot.Cursor.ExecutionIDs) != 0 {
		t.Fatalf("source cursor = %#v", snapshot.Cursor)
	}
	for index, event := range snapshot.Events {
		if event.Seq != int64(index+1) ||
			event.Ref.Sequence.Value == nil ||
			*event.Ref.Sequence.Value != event.Seq ||
			event.Ref.ContentSHA256 == "" {
			t.Fatalf("source event[%d] = %#v", index, event)
		}
	}
	copyForHash := snapshot
	copyForHash.SourceSHA256 = ""
	hash, err := research.CanonicalSHA256(copyForHash)
	if err != nil || hash != snapshot.SourceSHA256 {
		t.Fatalf("source hash = %s, recomputed = %s, err = %v", snapshot.SourceSHA256, hash, err)
	}

	transitions, manifest, err := research.NewProjector().Project(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SourceCursor.AgentEventSeq != 9 || len(transitions) != 3 {
		t.Fatalf("projected manifest/transitions = %#v / %d", manifest, len(transitions))
	}
	keys := make([]string, 0, len(transitions))
	for _, transition := range transitions {
		keys = append(keys, transition.AppendKey)
	}
	if !reflect.DeepEqual(
		keys,
		[]string{"tool:tool-real", "unknown:8", "terminal:" + agentRun.ID},
	) {
		t.Fatalf("transition keys = %#v", keys)
	}

	second, err := reader.Read(fixture.ctx, researchRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("repeatable source read changed without source mutation")
	}

	if _, updateErr := fixture.db.ExecContext(
		fixture.ctx,
		`UPDATE agent_runs SET last_event_seq = last_event_seq + 1 WHERE id = $1`,
		agentRun.ID,
	); updateErr != nil {
		t.Fatal(updateErr)
	}
	if _, readErr := reader.Read(
		fixture.ctx,
		researchRun.ID,
	); !errors.Is(readErr, research.ErrSourceChanged) {
		t.Fatalf("cursor drift error = %v, want ErrSourceChanged", readErr)
	}
}

func toolResultEventMap(
	t *testing.T,
	tool string,
	content json.RawMessage,
) map[string]any {
	t.Helper()
	payload, err := agent.NewToolResultEventPayload(tool, content)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
