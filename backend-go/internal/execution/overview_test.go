package execution

import (
	"testing"
	"time"
)

func TestBuildOverviewMatchesReportContract(t *testing.T) {
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	current, previous := overviewWindows(now, 7)
	rows := []map[string]any{
		overviewTestRow(5, 10, "Checkout", "failed", time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC), 1000, "locator", "click", "locator-a"),
		overviewTestRow(4, 11, "Search", "needs_intervention", time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC), 3000, "locator", "click", "locator-a"),
		overviewTestRow(3, 10, "Checkout", "passed", time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC), 2000, "", "", ""),
		overviewTestRow(2, 12, "Profile", "running", time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC), -1, "", "", ""),
		overviewTestRow(1, 12, "Profile", "cancelled", time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), 4000, "", "", ""),
		overviewTestRow(7, 10, "Checkout", "passed", time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC), 500, "", "", ""),
		overviewTestRow(6, 10, "Checkout", "failed", time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC), 1500, "assertion", "assert_text", "assertion-a"),
	}

	result := buildOverview(
		OverviewRequest{ScopeType: "global", WindowDays: 7},
		rows,
		current,
		previous,
	)

	assertOverviewValue(t, result, "total_count", 5)
	assertOverviewValue(t, result, "passed_count", 1)
	assertOverviewValue(t, result, "failed_count", 2)
	assertOverviewValue(t, result, "running_count", 1)
	assertOverviewValue(t, result, "auto_completed_count", 2)
	assertOverviewValue(t, result, "intervention_count", 1)
	assertOverviewValue(t, result, "pass_rate", 0.3333)
	assertOverviewValue(t, result, "automation_rate", 0.6667)
	assertOverviewValue(t, result, "intervention_rate", 0.3333)
	assertOverviewValue(t, result, "avg_duration_ms", int64(2500))

	previousStats := result["previous_window_stats"].(map[string]any)
	assertOverviewValue(t, previousStats, "total_count", 2)
	assertOverviewValue(t, previousStats, "pass_rate", 0.5)
	assertOverviewValue(t, previousStats, "avg_duration_ms", int64(1000))

	comparison := result["window_comparison"].(map[string]any)
	assertOverviewValue(t, comparison, "total_count_delta", 3)
	assertOverviewValue(t, comparison, "failed_count_delta", 1)
	assertOverviewValue(t, comparison, "avg_duration_ms_delta", int64(1500))

	trend := result["trend_points"].([]map[string]any)
	if len(trend) != 7 {
		t.Fatalf("trend point count = %d, want 7", len(trend))
	}
	assertOverviewValue(t, trend[0], "date", "2026-08-30")
	assertOverviewValue(t, trend[0], "total_count", 0)
	assertOverviewValue(t, trend[6], "date", "2026-09-05")
	assertOverviewValue(t, trend[6], "failed_count", 1)

	categories := result["failure_categories"].([]map[string]any)
	if len(categories) != len(failureCategoryOrder) {
		t.Fatalf("failure category count = %d, want %d", len(categories), len(failureCategoryOrder))
	}
	assertOverviewValue(t, categories[1], "category", "locator")
	assertOverviewValue(t, categories[1], "count", 2)

	topCases := result["top_failed_cases"].([]map[string]any)
	if len(topCases) != 2 {
		t.Fatalf("top failed case count = %d, want 2", len(topCases))
	}
	assertOverviewValue(t, topCases[0], "case_id", int64(10))
	assertOverviewValue(t, topCases[0], "latest_execution_id", int64(5))

	rootCauses := result["failure_root_causes"].([]map[string]any)
	if len(rootCauses) != 1 {
		t.Fatalf("root cause count = %d, want 1", len(rootCauses))
	}
	assertOverviewValue(t, rootCauses[0], "fingerprint", "locator-a")
	assertOverviewValue(t, rootCauses[0], "count", 2)
	assertOverviewValue(t, rootCauses[0], "affected_case_count", 2)

	latestFailed := result["latest_failed_runs"].([]map[string]any)
	if len(latestFailed) != 2 {
		t.Fatalf("latest failed count = %d, want 2", len(latestFailed))
	}
	assertOverviewValue(t, latestFailed[1], "status", "needs_intervention")
}

func TestBuildOverviewFiltersFingerprintAcrossWindows(t *testing.T) {
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	current, previous := overviewWindows(now, 7)
	rows := []map[string]any{
		overviewTestRow(2, 10, "Checkout", "failed", time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC), 1000, "locator", "click", "wanted"),
		overviewTestRow(1, 10, "Checkout", "failed", time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC), 1000, "locator", "click", "other"),
	}

	result := buildOverview(
		OverviewRequest{ScopeType: "case", CaseID: int64Pointer(10), WindowDays: 7, FailureFingerprint: "wanted"},
		rows,
		current,
		previous,
	)

	assertOverviewValue(t, result, "total_count", 1)
	assertOverviewValue(t, result["previous_window_stats"].(map[string]any), "total_count", 0)
}

func TestNormalizeOverviewRequestInfersAndRestrictsScope(t *testing.T) {
	projectID, caseID := int64(3), int64(8)
	request := normalizeOverviewRequest(OverviewRequest{
		ProjectID: &projectID,
		CaseID:    &caseID,
	})
	if request.ScopeType != "case" || request.WindowDays != 7 {
		t.Fatalf("normalizeOverviewRequest() = %#v", request)
	}

	request = normalizeOverviewRequest(OverviewRequest{
		ScopeType:  "global",
		ProjectID:  &projectID,
		CaseID:     &caseID,
		WindowDays: 14,
	})
	if request.ProjectID != nil || request.CaseID != nil {
		t.Fatalf("global scope retained filters: %#v", request)
	}
}

func overviewTestRow(
	id, caseID int64,
	caseName, status string,
	startedAt time.Time,
	durationMS int64,
	category, action, fingerprint string,
) map[string]any {
	row := map[string]any{
		"id": id, "case_id": caseID, "case_name": caseName,
		"status": status, "started_at": startedAt,
		"failure_category": category, "failure_step_action": action,
	}
	if durationMS >= 0 {
		row["duration_ms"] = durationMS
	}
	if fingerprint != "" {
		row["failure_signal"] = map[string]any{
			"fingerprint": fingerprint,
			"title":       "Element not found",
			"category":    category,
		}
	}
	return row
}

func assertOverviewValue(t *testing.T, values map[string]any, key string, want any) {
	t.Helper()
	if got := values[key]; got != want {
		t.Fatalf("%s = %#v, want %#v", key, got, want)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}
