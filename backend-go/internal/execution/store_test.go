package execution

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
)

func TestElapsedMillisecondsNeverReturnsNegativeDuration(t *testing.T) {
	startedAt := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	if got := elapsedMilliseconds(startedAt, startedAt.Add(1500*time.Millisecond)); got != 1500 {
		t.Fatalf("elapsedMilliseconds() = %d, want 1500", got)
	}
	if got := elapsedMilliseconds(startedAt, startedAt.Add(-8*time.Hour)); got != 0 {
		t.Fatalf("elapsedMilliseconds() = %d, want 0 for reversed timestamps", got)
	}
}

func TestExecutionDetailExposesCanonicalBindingInReport(t *testing.T) {
	const (
		hash    = "ec70764ce72b2edfbc0835988c690a1e90716f32de8d3ae575c4163aff00863c"
		version = dsl.CanonicalVersionV2
	)
	report := map[string]any{"status": "passed", "steps": []any{}}

	result := executionDetail(
		9, 7, "research case", 5, int64(3), int64(4), 1,
		hash, version, "execution.report.v2", 1, "passed", nil,
		time.Now().UTC(), nil, map[string]any{"profile": "research-v1"},
		report, nil, "pending", nil,
	)

	for label, value := range map[string]map[string]any{
		"execution": result,
		"report":    result["report"].(map[string]any),
	} {
		if value["dsl_profile"] != string(dsl.ProfileResearchV1) ||
			value["dsl_canonical_version"] != version ||
			value["dsl_sha256"] != hash {
			t.Fatalf("%s canonical binding = %#v", label, value)
		}
	}
}

func TestResolveExecutionCanonicalBindingPreservesLegacyAndRejectsMismatch(t *testing.T) {
	runHash := sql.NullString{String: "run-sha", Valid: true}

	hash, version, err := resolveExecutionCanonicalBinding(false, runHash, sql.NullString{}, sql.NullString{})
	if err != nil || hash != "run-sha" || version != "" {
		t.Fatalf("legacy binding = (%q, %q, %v)", hash, version, err)
	}
	hash, version, err = resolveExecutionCanonicalBinding(true, runHash, sql.NullString{}, sql.NullString{})
	if err != nil || hash != "run-sha" || version != "" {
		t.Fatalf("legacy job binding = (%q, %q, %v)", hash, version, err)
	}

	jobHash := sql.NullString{String: "job-sha", Valid: true}
	jobVersion := sql.NullString{String: dsl.CanonicalVersionV2, Valid: true}
	hash, version, err = resolveExecutionCanonicalBinding(true, jobHash, jobHash, jobVersion)
	if err != nil || hash != "job-sha" || version != dsl.CanonicalVersionV2 {
		t.Fatalf("job binding = (%q, %q, %v)", hash, version, err)
	}

	_, _, err = resolveExecutionCanonicalBinding(true, runHash, jobHash, jobVersion)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatch error = %v, want ErrConflict", err)
	}
	_, _, err = resolveExecutionCanonicalBinding(true, sql.NullString{}, jobHash, jobVersion)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("missing run SHA error = %v, want ErrConflict", err)
	}
}

func TestExecutionDetailDoesNotFabricateLegacyProfile(t *testing.T) {
	result := executionDetail(
		9, 7, "legacy case", 5, nil, nil, 1,
		"legacy-sha", "", "execution.report.v1", 1, "passed", nil,
		time.Now().UTC(), nil, map[string]any{"name": "legacy"},
		map[string]any{"status": "passed", "steps": []any{}},
		nil, "skipped", nil,
	)

	if result["dsl_canonical_version"] != nil || result["dsl_profile"] != nil {
		t.Fatalf("legacy canonical metadata = %#v", result)
	}
	report := result["report"].(map[string]any)
	if report["dsl_canonical_version"] != nil || report["dsl_profile"] != nil {
		t.Fatalf("legacy report canonical metadata = %#v", report)
	}
}

func TestValidateDSLBindingsRejectsUnapprovedBytes(t *testing.T) {
	canonical, _, err := dsl.ValidateCase(json.RawMessage(
		`{"name":"approved","steps":[{"action":"goto","value":"/"}]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	hash := dsl.SHA256(canonical)

	if err := validateDSLBindings(
		[]int64{7},
		map[int64]CanonicalDSLBinding{7: {
			CanonicalJSON: canonical, SHA256: hash, Version: dsl.CanonicalVersion,
		}},
	); err != nil {
		t.Fatalf("validateDSLBindings() error = %v", err)
	}
	if err := validateDSLBindings(
		[]int64{7},
		map[int64]CanonicalDSLBinding{7: {
			CanonicalJSON: canonical, SHA256: "0" + hash[1:], Version: dsl.CanonicalVersion,
		}},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("SHA mismatch error = %v, want ErrConflict", err)
	}
	if err := validateDSLBindings(
		[]int64{7},
		map[int64]CanonicalDSLBinding{8: {
			CanonicalJSON: canonical, SHA256: hash, Version: dsl.CanonicalVersion,
		}},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("unselected case error = %v, want ErrConflict", err)
	}
}

func TestValidateDSLBindingsRevalidatesResearchExecutableBeforeQueue(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "dsl_research_v1_contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		CanonicalJSON string `json:"canonical_json"`
		SHA256        string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	binding := CanonicalDSLBinding{
		CanonicalJSON: json.RawMessage(fixture.CanonicalJSON),
		SHA256:        fixture.SHA256,
		Version:       dsl.CanonicalVersionV2,
	}
	if err := validateDSLBindings(
		[]int64{7},
		map[int64]CanonicalDSLBinding{7: binding},
	); err != nil {
		t.Fatalf("validateDSLBindings() error = %v", err)
	}
	if err := validatePersistedCaseBindings(
		[]int64{7},
		map[int64]json.RawMessage{7: binding.CanonicalJSON},
		map[int64]CanonicalDSLBinding{7: binding},
	); err != nil {
		t.Fatalf("validatePersistedCaseBindings() error = %v", err)
	}

	var candidate map[string]any
	if err := json.Unmarshal(binding.CanonicalJSON, &candidate); err != nil {
		t.Fatal(err)
	}
	step := candidate["steps"].([]any)[1].(map[string]any)
	step["locator_confidence"] = "low"
	mutated, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	binding.CanonicalJSON = mutated
	binding.SHA256 = dsl.SHA256(mutated)
	if err := validateDSLBindings(
		[]int64{7},
		map[int64]CanonicalDSLBinding{7: binding},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("non-executable binding error = %v, want ErrConflict", err)
	}
}

func TestValidatePersistedCaseBindingsRequiresResearchBinding(t *testing.T) {
	legacy, _, err := dsl.ValidateCase(json.RawMessage(
		`{"name":"legacy","steps":[{"action":"goto","value":"/"}]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDSLBindings([]int64{7}, nil); err != nil {
		t.Fatalf("ordinary legacy batch error = %v", err)
	}
	if err := validatePersistedCaseBindings(
		[]int64{7},
		map[int64]json.RawMessage{7: legacy},
		nil,
	); err != nil {
		t.Fatalf("ordinary legacy batch error = %v", err)
	}

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "dsl_research_v1_contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		CanonicalJSON string `json:"canonical_json"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if err := validatePersistedCaseBindings(
		[]int64{7},
		map[int64]json.RawMessage{7: json.RawMessage(fixture.CanonicalJSON)},
		nil,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("research batch without binding error = %v, want ErrConflict", err)
	}
}

func TestValidatePersistedCaseBindingsRejectsResearchMismatch(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "dsl_research_v1_contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		CanonicalJSON string `json:"canonical_json"`
		SHA256        string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	binding := CanonicalDSLBinding{
		CanonicalJSON: json.RawMessage(fixture.CanonicalJSON),
		SHA256:        fixture.SHA256,
		Version:       dsl.CanonicalVersionV2,
	}
	var persisted map[string]any
	if err := json.Unmarshal(json.RawMessage(fixture.CanonicalJSON), &persisted); err != nil {
		t.Fatal(err)
	}
	persisted["name"] = "different"
	persistedRaw, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePersistedCaseBindings(
		[]int64{7},
		map[int64]json.RawMessage{7: persistedRaw},
		map[int64]CanonicalDSLBinding{7: binding},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("research binding mismatch error = %v, want ErrConflict", err)
	}
}
