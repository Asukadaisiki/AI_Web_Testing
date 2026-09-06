package research

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

func TestJSONLExporterRunPaginationDeterminismAndEncoding(t *testing.T) {
	const count = DefaultExportPageSize + 37
	run := exportTestRun("run-page", "experiment-page", 2, false)
	repository := &exportMemoryRepository{
		runs:        []ResearchRun{run},
		transitions: map[string][]Transition{run.ID: {}},
	}
	for ordinal := range count {
		text := "ordinary"
		if ordinal == 0 {
			text = "中文 <>& line\nbreak"
		}
		repository.transitions[run.ID] = append(
			repository.transitions[run.ID],
			exportTestTransition(t, run.ID, int64(ordinal), count, text, true),
		)
	}

	var first bytes.Buffer
	if err := NewJSONLExporter(repository).ExportRun(
		context.Background(), &first, run.ID,
	); err != nil {
		t.Fatalf("ExportRun() error = %v", err)
	}
	if repository.transitionCalls < 2 {
		t.Fatalf("ListTransitions() calls = %d, want pagination", repository.transitionCalls)
	}
	if !utf8.Valid(first.Bytes()) || bytes.HasPrefix(first.Bytes(), []byte{0xef, 0xbb, 0xbf}) {
		t.Fatal("export must be BOM-free UTF-8")
	}
	if bytes.Contains(first.Bytes(), []byte{'\r'}) ||
		bytes.Count(first.Bytes(), []byte{'\n'}) != count {
		t.Fatal("export must use exactly one LF terminator per JSON object")
	}
	if !bytes.Contains(first.Bytes(), []byte("中文")) ||
		!bytes.Contains(first.Bytes(), []byte(`\u003c\u003e\u0026 line\nbreak`)) {
		t.Fatalf("UTF-8 or JSON escaping changed: %.500s", first.Bytes())
	}

	repository.transitionCalls = 0
	var second bytes.Buffer
	if err := NewJSONLExporter(repository).ExportRun(
		context.Background(), &second, run.ID,
	); err != nil {
		t.Fatalf("second ExportRun() error = %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("repeated run exports are not byte-identical")
	}
	validateJSONLAgainstSchema(t, first.Bytes())
	lines := splitJSONLLines(t, first.Bytes())
	for ordinal, raw := range lines {
		var line TrajectoryJSONL
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("line %d: %v", ordinal, err)
		}
		if line.Ordinal != int64(ordinal) {
			t.Fatalf("line %d ordinal = %d", ordinal, line.Ordinal)
		}
	}
}

func TestJSONLExporterExperimentStableOrderAcrossPages(t *testing.T) {
	const repetitions = 51
	runs := make([]ResearchRun, 0, repetitions*2)
	transitions := make(map[string][]Transition, repetitions*2)
	for repetition := range repetitions {
		for _, warmup := range []bool{false, true} {
			id := fmt.Sprintf("run-%03d-%t", repetition, warmup)
			run := exportTestRun(id, "experiment-many", repetition, warmup)
			runs = append(runs, run)
			transitions[id] = []Transition{
				exportTestTransition(t, id, 0, 1, id, false),
			}
		}
	}
	slices.Reverse(runs)
	repository := &exportMemoryRepository{runs: runs, transitions: transitions}

	var first bytes.Buffer
	if err := NewJSONLExporter(repository).ExportExperiment(
		context.Background(), &first, "experiment-many",
	); err != nil {
		t.Fatalf("ExportExperiment() error = %v", err)
	}
	if repository.runCalls < 2 {
		t.Fatalf("ListRuns() calls = %d, want pagination", repository.runCalls)
	}
	lines := splitJSONLLines(t, first.Bytes())
	if len(lines) != repetitions*2 {
		t.Fatalf("line count = %d", len(lines))
	}
	for index, raw := range lines {
		var line TrajectoryJSONL
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatal(err)
		}
		wantRepetition := index / 2
		wantWarmup := index%2 == 0
		if line.RepetitionIndex != wantRepetition || line.Warmup != wantWarmup {
			t.Fatalf(
				"line %d order = repetition %d warmup %t",
				index, line.RepetitionIndex, line.Warmup,
			)
		}
	}

	repository.runCalls = 0
	repository.transitionCalls = 0
	var second bytes.Buffer
	if err := NewJSONLExporter(repository).ExportExperiment(
		context.Background(), &second, "experiment-many",
	); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("repeated experiment exports are not byte-identical")
	}
	validateJSONLAgainstSchema(t, first.Bytes())
}

func TestTrajectoryJSONLRejectsUnsafeOrAmbiguousContent(t *testing.T) {
	valid := exportTestTransition(t, "run-safety", 0, 1, "safe", true)
	line := exportTestLine(valid, exportTestRun("run-safety", "experiment-safety", 0, false))
	raw, err := encodeExportLine(line)
	if err != nil {
		t.Fatal(err)
	}
	var nullArtifacts map[string]any
	if decodeErr := json.Unmarshal(raw, &nullArtifacts); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	nullArtifacts["artifact_refs"] = nil
	nullArtifactRaw, err := json.Marshal(nullArtifacts)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		raw  []byte
	}{
		{"invalid UTF-8", []byte{'"', 0xff, '"'}},
		{"UTF-8 BOM", append([]byte{0xef, 0xbb, 0xbf}, raw...)},
		{"embedded LF", append(append([]byte(nil), raw...), '\n')},
		{"over 512 KiB", bytes.Repeat([]byte{'x'}, MaxExportLineBytes+1)},
		{"null artifact refs", nullArtifactRaw},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateTrajectoryJSONLLine(test.raw); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}

	t.Run("invalid UTF-8 before JSON replacement", func(t *testing.T) {
		invalid := line
		invalid.AppendKey = string([]byte{0xff})
		if _, err := encodeExportLine(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("error = %v, want ErrInvalid", err)
		}
	})
	t.Run("secret field", func(t *testing.T) {
		transition := exportTestTransitionWithData(
			t, "run-secret", 0, 1,
			map[string]any{"nested": map[string]any{"api_key": "must-not-export"}},
			nil,
		)
		if _, err := encodeExportLine(exportTestLine(
			transition,
			exportTestRun("run-secret", "experiment-safety", 0, false),
		)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("error = %v, want ErrInvalid", err)
		}
	})
	t.Run("signed HTTP artifact URI", func(t *testing.T) {
		artifact := exportTestArtifact("https://example.com/report?token=secret")
		transition := exportTestTransitionWithData(
			t, "run-uri", 0, 1, map[string]any{"safe": true}, []ArtifactRef{artifact},
		)
		if _, err := encodeExportLine(exportTestLine(
			transition,
			exportTestRun("run-uri", "experiment-safety", 0, false),
		)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("error = %v, want ErrInvalid", err)
		}
	})
	t.Run("oversized object", func(t *testing.T) {
		data := make(map[string]any, MaxExportObjectFields+1)
		for index := 0; index <= MaxExportObjectFields; index++ {
			data[fmt.Sprintf("field_%03d", index)] = index
		}
		transition := exportTestTransitionWithData(
			t, "run-object", 0, 1, data, nil,
		)
		if _, err := encodeExportLine(exportTestLine(
			transition,
			exportTestRun("run-object", "experiment-safety", 0, false),
		)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("error = %v, want ErrInvalid", err)
		}
	})
	t.Run("unknown transition field", func(t *testing.T) {
		mutated := bytes.Replace(
			raw,
			[]byte(`"transition":{`),
			[]byte(`"transition":{"unexpected":true,`),
			1,
		)
		if err := ValidateTrajectoryJSONLLine(mutated); !errors.Is(err, ErrInvalid) {
			t.Fatalf("error = %v, want ErrInvalid", err)
		}
	})
}

func TestTrajectoryJSONLGoldenValidatesWithDraft202012Schema(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	run := exportTestRun("run-golden", "experiment-golden", 0, false)
	transition := exportTestTransition(t, run.ID, 0, 1, "中文 <>& line\nbreak", true)
	actual, err := encodeExportLine(exportTestLine(transition, run))
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')

	goldenPath := filepath.Join(root, "testdata", "research_trajectory_v1.jsonl")
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v\ngolden content:\n%s", err, actual)
	}
	validateJSONLAgainstSchema(t, golden)
	for _, marker := range [][]byte{
		[]byte(`"status":"available"`),
		[]byte(`"status":"unavailable"`),
		[]byte(`"uri":"artifact://`),
	} {
		if !bytes.Contains(golden, marker) {
			t.Fatalf("golden does not cover explicit availability/reference marker %q", marker)
		}
	}

	if !bytes.Equal(actual, golden) {
		t.Fatalf("golden drift\nactual: %s\nwant:   %s", actual, golden)
	}
}

func TestTrajectoryJSONLExternalFileValidatesLineByLine(t *testing.T) {
	path := strings.TrimSpace(os.Getenv("RESEARCH_JSONL_VALIDATE_PATH"))
	if path == "" {
		t.Skip("RESEARCH_JSONL_VALIDATE_PATH is not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	validateJSONLAgainstSchema(t, raw)
}

func TestPostgresJSONLExporterUsesRepeatableReadSnapshot(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	ctx := context.Background()
	if err := database.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	var researchTable sql.NullString
	if err := database.QueryRowContext(
		ctx, `SELECT to_regclass('public.research_runs')::text`,
	).Scan(&researchTable); err != nil || !researchTable.Valid {
		t.Fatalf("research migration is not applied: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var userID, projectID int64
	if err := database.QueryRowContext(ctx, `
		INSERT INTO users (email, display_name)
		VALUES ($1, 'Exporter Integration')
		RETURNING id`,
		"exporter-"+suffix+"@example.com",
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, `
		INSERT INTO projects (name, description)
		VALUES ($1, 'exporter snapshot integration')
		RETURNING id`,
		"exporter-"+suffix,
	).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.ExecContext(
			context.Background(),
			`DELETE FROM research_experiments WHERE id = $1`,
			"experiment-exporter-"+suffix,
		)
		_, _ = database.ExecContext(
			context.Background(), `DELETE FROM projects WHERE id = $1`, projectID,
		)
		_, _ = database.ExecContext(
			context.Background(), `DELETE FROM users WHERE id = $1`, userID,
		)
	})

	repository := NewPostgresRepository(database)
	experiment := Experiment{
		ID:                 "experiment-exporter-" + suffix,
		ProjectID:          projectID,
		Name:               "exporter snapshot",
		Goal:               "prove one repeatable-read export snapshot",
		DatasetVersion:     "dataset.v1",
		ModelProvider:      "provider",
		ModelName:          "model",
		ModelVersion:       "model.v1",
		PromptVersion:      "prompt.v1",
		BrowserName:        "chromium",
		BrowserVersion:     "1",
		ViewportJSON:       json.RawMessage(`{"width":1280,"height":720}`),
		CodeSHA256:         strings.Repeat("a", 64),
		PolicyVersion:      PolicyVersion,
		ObservationProfile: "a11y-dom",
		DSLProfile:         "legacy",
		Seed:               1,
		Variant:            "snapshot",
		Repetitions:        1,
		Status:             ExperimentStatusDraft,
		ConfigJSON:         json.RawMessage(`{}`),
	}
	if _, err := repository.CreateExperiment(ctx, experiment); err != nil {
		t.Fatal(err)
	}
	run := exportTestRun("run-exporter-"+suffix, experiment.ID, 0, false)
	run.ProjectID = projectID
	if _, err := repository.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	const count = DefaultExportPageSize + 1
	transitions := make([]Transition, 0, count)
	for ordinal := range count {
		transitions = append(
			transitions,
			exportTestTransition(t, run.ID, int64(ordinal), count, "snapshot", false),
		)
	}
	if _, err := repository.AppendTransitions(ctx, run.ID, transitions); err != nil {
		t.Fatal(err)
	}

	writer := &mutatingExportWriter{mutate: func() error {
		_, err := database.ExecContext(ctx, `
			UPDATE research_transitions
			SET append_key = 'mutated-after-first-page'
			WHERE research_run_id = $1 AND ordinal = $2`,
			run.ID, count-1,
		)
		return err
	}}
	if err := NewJSONLExporter(repository).ExportRun(ctx, writer, run.ID); err != nil {
		t.Fatalf("ExportRun() error = %v", err)
	}
	if writer.mutationErr != nil {
		t.Fatalf("concurrent mutation: %v", writer.mutationErr)
	}
	lines := splitJSONLLines(t, writer.Bytes())
	var last TrajectoryJSONL
	if err := json.Unmarshal(lines[len(lines)-1], &last); err != nil {
		t.Fatal(err)
	}
	if last.AppendKey != fmt.Sprintf("append-%03d", count-1) {
		t.Fatalf("last append key = %q; export did not retain its initial snapshot", last.AppendKey)
	}
	var persisted string
	if err := database.QueryRowContext(ctx, `
		SELECT append_key
		FROM research_transitions
		WHERE research_run_id = $1 AND ordinal = $2`,
		run.ID, count-1,
	).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != "mutated-after-first-page" {
		t.Fatalf("concurrent mutation did not commit: %q", persisted)
	}
}

type exportMemoryRepository struct {
	runs            []ResearchRun
	transitions     map[string][]Transition
	runCalls        int
	transitionCalls int
}

func (r *exportMemoryRepository) GetRun(_ context.Context, id string) (ResearchRun, error) {
	for _, run := range r.runs {
		if run.ID == id {
			return run, nil
		}
	}
	return ResearchRun{}, ErrNotFound
}

func (r *exportMemoryRepository) ListRuns(
	_ context.Context,
	filter RunFilter,
) ([]ResearchRun, error) {
	r.runCalls++
	filtered := make([]ResearchRun, 0, len(r.runs))
	for _, run := range r.runs {
		if filter.ExperimentID == nil || run.ExperimentID == *filter.ExperimentID {
			filtered = append(filtered, run)
		}
	}
	if filter.Offset >= len(filtered) {
		return []ResearchRun{}, nil
	}
	end := min(filter.Offset+filter.Limit, len(filtered))
	return append([]ResearchRun(nil), filtered[filter.Offset:end]...), nil
}

func (r *exportMemoryRepository) ListTransitions(
	_ context.Context,
	filter TransitionFilter,
) ([]Transition, error) {
	r.transitionCalls++
	all := r.transitions[filter.ResearchRunID]
	start := 0
	if filter.AfterOrdinal != nil {
		start = len(all)
		for index := range all {
			if all[index].Ordinal > *filter.AfterOrdinal {
				start = index
				break
			}
		}
	}
	end := min(start+filter.Limit, len(all))
	return append([]Transition(nil), all[start:end]...), nil
}

type mutatingExportWriter struct {
	bytes.Buffer
	once        sync.Once
	mutate      func() error
	mutationErr error
}

func (w *mutatingExportWriter) Write(value []byte) (int, error) {
	w.once.Do(func() {
		w.mutationErr = w.mutate()
	})
	if w.mutationErr != nil {
		return 0, w.mutationErr
	}
	return w.Buffer.Write(value)
}

func exportTestRun(id, experimentID string, repetition int, warmup bool) ResearchRun {
	return ResearchRun{
		ID:              id,
		ExperimentID:    experimentID,
		ProjectID:       1,
		IdempotencyKey:  id,
		RepetitionIndex: repetition,
		Warmup:          warmup,
		Status:          RunStatusPending,
		Versions:        DefaultVersionSnapshot(),
	}
}

func exportTestTransition(
	t *testing.T,
	runID string,
	ordinal int64,
	count int,
	text string,
	withArtifact bool,
) Transition {
	t.Helper()
	var artifacts []ArtifactRef
	if withArtifact {
		artifacts = []ArtifactRef{exportTestArtifact("artifact://execution/1/step/1")}
	}
	return exportTestTransitionWithData(
		t, runID, ordinal, count, map[string]any{"text": text}, artifacts,
	)
}

func exportTestTransitionWithData(
	t *testing.T,
	runID string,
	ordinal int64,
	count int,
	data map[string]any,
	artifacts []ArtifactRef,
) Transition {
	t.Helper()
	source := testSourceRef(t, SourceAgentEvent, runID+":0", Available(int64(0)))
	event, err := NewResearchEvent(ResearchEvent{
		Kind:          EventKindObservation,
		ResearchRunID: runID,
		CorrelationID: Unavailable[string]("correlation_not_persisted"),
		CausationID:   NotApplicable[string]("root_observation"),
		Attempt:       NotApplicable[int64]("no_attempt"),
		StepIndex:     Available(int64(0)),
		Sources:       []SourceRef{source},
		Data:          mustJSON(t, data),
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := NewProjectionManifest(
		SourceCursor{
			SchemaVersion: EventSchemaVersion,
			AgentRunID:    "agent-" + runID,
			AgentEventSeq: int64(count),
		},
		strings.Repeat("b", 64),
		int64(count),
	)
	if err != nil {
		t.Fatal(err)
	}
	payload := TransitionPayloadV1{
		SchemaVersion:    TransitionSchemaVersion,
		ProjectorVersion: ProjectorVersion,
		Unit:             TransitionUnit{Type: "agent_event", ID: fmt.Sprintf("%s:%d", runID, ordinal)},
		State:            Available(event),
		Observation:      Unavailable[ResearchEvent]("observation_not_persisted"),
		Candidate:        NotApplicable[ResearchEvent]("not_a_candidate_unit"),
		Decision:         NotApplicable[ResearchEvent]("not_a_decision_unit"),
		Action:           NotApplicable[ResearchEvent]("not_an_action_unit"),
		Execution:        NotApplicable[ResearchEvent]("not_an_execution_unit"),
		Verification:     NotApplicable[ResearchEvent]("not_a_verification_unit"),
		Failure:          NotApplicable[ResearchEvent]("no_failure"),
		Recovery:         NotApplicable[ResearchEvent]("no_recovery"),
		Reward:           Unavailable[ResearchEvent]("independent_oracle_not_persisted"),
		Unknown:          NotApplicable[ResearchEvent]("known_event"),
		Cost:             Unavailable[CostSummary]("model_cost_not_persisted"),
		Done:             ordinal == int64(count-1),
		Projection:       manifest,
	}
	payloadJSON := mustJSON(t, payload)
	hash, err := TransitionContentSHA256(SchemaVersion, payloadJSON, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	return Transition{
		ResearchRunID: runID,
		Ordinal:       ordinal,
		AppendKey:     fmt.Sprintf("append-%03d", ordinal),
		ContentSHA256: hash,
		SchemaVersion: SchemaVersion,
		PayloadJSON:   payloadJSON,
		ArtifactRefs:  artifacts,
	}
}

func exportTestArtifact(uri string) ArtifactRef {
	size := int64(128)
	return ArtifactRef{
		Kind:          "step_evidence",
		URI:           uri,
		SHA256:        strings.Repeat("c", 64),
		MediaType:     "application/json",
		SchemaVersion: "evidence.v1",
		SizeBytes:     &size,
	}
}

func exportTestLine(transition Transition, run ResearchRun) TrajectoryJSONL {
	return TrajectoryJSONL{
		SchemaVersion:    ExportSchemaVersion,
		ProjectorVersion: run.Versions.ProjectorVersion,
		ResearchRunID:    run.ID,
		ExperimentID:     run.ExperimentID,
		RepetitionIndex:  run.RepetitionIndex,
		Warmup:           run.Warmup,
		Ordinal:          transition.Ordinal,
		AppendKey:        transition.AppendKey,
		ContentSHA256:    transition.ContentSHA256,
		Transition:       transition.PayloadJSON,
		ArtifactRefs:     append([]ArtifactRef(nil), transition.ArtifactRefs...),
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func splitJSONLLines(t *testing.T, raw []byte) [][]byte {
	t.Helper()
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Fatal("JSONL output must be non-empty and LF-terminated")
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), MaxExportLineBytes+1)
	lines := make([][]byte, 0)
	for scanner.Scan() {
		lines = append(lines, append([]byte(nil), scanner.Bytes()...))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func validateJSONLAgainstSchema(t *testing.T, raw []byte) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	schemaPath := filepath.Join(root, "research", "schemas", "trajectory-line.v1.schema.json")
	schemaFile, fileErr := os.Open(schemaPath)
	if fileErr != nil {
		t.Fatal(fileErr)
	}
	defer schemaFile.Close()
	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft2020
	const schemaURL = "https://ai-web-testing.local/schemas/trajectory-line.v1.schema.json"
	if addErr := compiler.AddResource(schemaURL, schemaFile); addErr != nil {
		t.Fatal(addErr)
	}
	schema, compileErr := compiler.Compile(schemaURL)
	if compileErr != nil {
		t.Fatalf("compile Draft 2020-12 schema: %v", compileErr)
	}
	lines := splitJSONLLines(t, raw)
	if len(lines) == 0 {
		t.Fatal("JSONL contains no trajectory lines")
	}
	for index, line := range lines {
		if err := ValidateTrajectoryJSONLLine(line); err != nil {
			t.Fatalf("line %d semantic validation: %v", index+1, err)
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("line %d decode: %v", index+1, err)
		}
		if err := schema.Validate(value); err != nil {
			t.Fatalf("line %d schema validation: %v", index+1, err)
		}
	}
}
