package execution

import (
	"context"
	"crypto/sha1"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	latestFailedRunsLimit = 5
	topFailedCasesLimit   = 5
	failureRootCauseLimit = 10
)

var failureCategoryOrder = []string{
	"configuration",
	"locator",
	"assertion",
	"navigation",
	"network",
	"runner",
}

type overviewWindow struct {
	Start time.Time
	End   time.Time
}

type overviewFailure struct {
	Fingerprint string
	Title       string
}

type overviewSnapshot struct {
	TotalCount    int
	PassedCount   int
	FailedCount   int
	RunningCount  int
	PassRate      float64
	AvgDurationMS int64
}

type failedCaseGroup struct {
	Count  int
	Latest map[string]any
}

type rootCauseGroup struct {
	Count        int
	AffectedCase map[int64]struct{}
	Latest       map[string]any
	Failure      overviewFailure
}

func normalizeOverviewRequest(request OverviewRequest) OverviewRequest {
	if request.WindowDays != 7 && request.WindowDays != 14 && request.WindowDays != 30 {
		request.WindowDays = 7
	}
	if request.ScopeType == "" {
		switch {
		case request.CaseID != nil:
			request.ScopeType = "case"
		case request.ProjectID != nil:
			request.ScopeType = "project"
		default:
			request.ScopeType = "global"
		}
	}
	switch request.ScopeType {
	case "global":
		request.ProjectID = nil
		request.CaseID = nil
	case "project":
		request.CaseID = nil
	}
	return request
}

func overviewWindows(now time.Time, windowDays int) (overviewWindow, overviewWindow) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	current := overviewWindow{
		Start: today.AddDate(0, 0, -(windowDays - 1)),
		End:   today,
	}
	previousEnd := current.Start.AddDate(0, 0, -1)
	return current, overviewWindow{
		Start: previousEnd.AddDate(0, 0, -(windowDays - 1)),
		End:   previousEnd,
	}
}

func (s *Store) listOverviewExecutions(
	ctx context.Context,
	actorUserID int64,
	request OverviewRequest,
	startInclusive, endExclusive time.Time,
) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.case_id, tc.name, r.project_id, r.batch_id, r.job_id,
		       r.attempt_number, r.dsl_sha256, r.report_schema_version, r.triggered_by,
		       r.status, r.error_message, r.started_at, r.finished_at, r.dsl_snapshot,
		       r.report, r.failure_signal_json, r.analysis_status, r.analysis_json
		FROM test_case_runs r
		JOIN test_cases tc ON tc.id = r.case_id
		JOIN project_members pm ON pm.project_id = r.project_id
		WHERE pm.user_id = $1
		  AND ($2::bigint IS NULL OR r.project_id = $2)
		  AND ($3::bigint IS NULL OR r.case_id = $3)
		  AND r.started_at >= $4
		  AND r.started_at < $5
		ORDER BY r.started_at DESC, r.id DESC`,
		actorUserID,
		request.ProjectID,
		request.CaseID,
		startInclusive,
		endExclusive,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]map[string]any, 0)
	for rows.Next() {
		item, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func buildOverview(
	request OverviewRequest,
	rows []map[string]any,
	current, previous overviewWindow,
) map[string]any {
	currentRows := filterOverviewRows(rows, current, request.FailureFingerprint)
	previousRows := filterOverviewRows(rows, previous, request.FailureFingerprint)
	currentStats := buildOverviewSnapshot(currentRows)
	previousStats := buildOverviewSnapshot(previousRows)
	completedCount := currentStats.PassedCount + currentStats.FailedCount
	interventionCount := countStatus(currentRows, "needs_intervention")
	autoCompletedCount := countStatus(currentRows, "passed") + countStatus(currentRows, "failed")

	return map[string]any{
		"scope_type":               request.ScopeType,
		"scope_project_id":         pointerValue(request.ProjectID),
		"scope_case_id":            pointerValue(request.CaseID),
		"total_count":              currentStats.TotalCount,
		"passed_count":             currentStats.PassedCount,
		"failed_count":             currentStats.FailedCount,
		"running_count":            currentStats.RunningCount,
		"auto_completed_count":     autoCompletedCount,
		"intervention_count":       interventionCount,
		"pass_rate":                currentStats.PassRate,
		"automation_rate":          ratio(autoCompletedCount, completedCount),
		"intervention_rate":        ratio(interventionCount, completedCount),
		"avg_duration_ms":          currentStats.AvgDurationMS,
		"current_window_range":     windowMap(current),
		"previous_window_range":    windowMap(previous),
		"previous_window_stats":    snapshotMap(previousStats),
		"window_comparison":        comparisonMap(currentStats, previousStats),
		"latest_failed_runs":       latestRuns(currentRows, true),
		"latest_intervention_runs": latestRuns(currentRows, false),
		"failure_categories":       buildFailureCategories(currentRows),
		"trend_points":             buildTrendPoints(currentRows, current),
		"failure_step_actions":     buildFailureStepActions(currentRows),
		"top_failed_cases":         buildTopFailedCases(currentRows),
		"failure_root_causes":      buildFailureRootCauses(currentRows),
	}
}

func filterOverviewRows(
	rows []map[string]any,
	window overviewWindow,
	fingerprint string,
) []map[string]any {
	filtered := make([]map[string]any, 0, len(rows))
	endExclusive := window.End.AddDate(0, 0, 1)
	for _, row := range rows {
		startedAt, ok := row["started_at"].(time.Time)
		if !ok || startedAt.Before(window.Start) || !startedAt.Before(endExclusive) {
			continue
		}
		if fingerprint != "" && describeOverviewFailure(row).Fingerprint != fingerprint {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func buildOverviewSnapshot(rows []map[string]any) overviewSnapshot {
	snapshot := overviewSnapshot{TotalCount: len(rows)}
	var totalDuration int64
	var durationCount int64
	for _, row := range rows {
		status, _ := row["status"].(string)
		switch status {
		case "passed":
			snapshot.PassedCount++
		case "failed", "needs_intervention":
			snapshot.FailedCount++
		case "running":
			snapshot.RunningCount++
		}
		if status != "running" {
			if duration, ok := row["duration_ms"].(int64); ok {
				totalDuration += duration
				durationCount++
			}
		}
	}
	snapshot.PassRate = ratio(snapshot.PassedCount, snapshot.PassedCount+snapshot.FailedCount)
	if durationCount > 0 {
		snapshot.AvgDurationMS = totalDuration / durationCount
	}
	return snapshot
}

func buildTrendPoints(rows []map[string]any, window overviewWindow) []map[string]any {
	buckets := make(map[string][]map[string]any)
	for _, row := range rows {
		startedAt := row["started_at"].(time.Time).UTC()
		key := startedAt.Format("2006-01-02")
		buckets[key] = append(buckets[key], row)
	}
	result := make([]map[string]any, 0)
	for day := window.Start; !day.After(window.End); day = day.AddDate(0, 0, 1) {
		items := buckets[day.Format("2006-01-02")]
		snapshot := buildOverviewSnapshot(items)
		result = append(result, map[string]any{
			"date":                 day.Format("2006-01-02"),
			"total_count":          snapshot.TotalCount,
			"passed_count":         snapshot.PassedCount,
			"failed_count":         snapshot.FailedCount,
			"auto_completed_count": countStatus(items, "passed") + countStatus(items, "failed"),
			"intervention_count":   countStatus(items, "needs_intervention"),
			"pass_rate":            snapshot.PassRate,
			"avg_duration_ms":      snapshot.AvgDurationMS,
		})
	}
	return result
}

func buildFailureCategories(rows []map[string]any) []map[string]any {
	counts := make(map[string]int)
	for _, row := range rows {
		if !isFailureStatus(row["status"]) {
			continue
		}
		if category, ok := row["failure_category"].(string); ok {
			counts[category]++
		}
	}
	result := make([]map[string]any, 0, len(failureCategoryOrder))
	for _, category := range failureCategoryOrder {
		result = append(result, map[string]any{"category": category, "count": counts[category]})
	}
	return result
}

func buildFailureStepActions(rows []map[string]any) []map[string]any {
	counts := make(map[string]int)
	for _, row := range rows {
		if !isFailureStatus(row["status"]) {
			continue
		}
		if action, ok := row["failure_step_action"].(string); ok && action != "" {
			counts[action]++
		}
	}
	result := make([]map[string]any, 0, len(counts))
	for action, count := range counts {
		result = append(result, map[string]any{"action": action, "count": count})
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := result[i]["count"].(int), result[j]["count"].(int)
		if left != right {
			return left > right
		}
		return result[i]["action"].(string) < result[j]["action"].(string)
	})
	return result
}

func buildTopFailedCases(rows []map[string]any) []map[string]any {
	groups := make(map[int64]*failedCaseGroup)
	for _, row := range rows {
		if !isFailureStatus(row["status"]) {
			continue
		}
		caseID, _ := row["case_id"].(int64)
		group := groups[caseID]
		if group == nil {
			group = &failedCaseGroup{}
			groups[caseID] = group
		}
		group.Count++
		if group.Latest == nil || executionLater(row, group.Latest) {
			group.Latest = row
		}
	}
	result := make([]map[string]any, 0, len(groups))
	for caseID, group := range groups {
		result = append(result, map[string]any{
			"case_id":                 caseID,
			"case_name":               group.Latest["case_name"],
			"failure_count":           group.Count,
			"latest_execution_id":     group.Latest["id"],
			"latest_failure_category": group.Latest["failure_category"],
			"_latest_started_at":      group.Latest["started_at"],
		})
	}
	sort.Slice(result, func(i, j int) bool {
		leftCount, rightCount := result[i]["failure_count"].(int), result[j]["failure_count"].(int)
		if leftCount != rightCount {
			return leftCount > rightCount
		}
		leftTime := result[i]["_latest_started_at"].(time.Time)
		rightTime := result[j]["_latest_started_at"].(time.Time)
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return result[i]["latest_execution_id"].(int64) > result[j]["latest_execution_id"].(int64)
	})
	if len(result) > topFailedCasesLimit {
		result = result[:topFailedCasesLimit]
	}
	for _, item := range result {
		delete(item, "_latest_started_at")
	}
	return result
}

func buildFailureRootCauses(rows []map[string]any) []map[string]any {
	groups := make(map[string]*rootCauseGroup)
	for _, row := range rows {
		if !isFailureStatus(row["status"]) {
			continue
		}
		failure := describeOverviewFailure(row)
		if failure.Fingerprint == "" || failure.Title == "" {
			continue
		}
		group := groups[failure.Fingerprint]
		if group == nil {
			group = &rootCauseGroup{AffectedCase: make(map[int64]struct{})}
			groups[failure.Fingerprint] = group
		}
		group.Count++
		caseID, _ := row["case_id"].(int64)
		group.AffectedCase[caseID] = struct{}{}
		if group.Latest == nil || executionLater(row, group.Latest) {
			group.Latest = row
			group.Failure = failure
		}
	}
	result := make([]map[string]any, 0, len(groups))
	for fingerprint, group := range groups {
		result = append(result, map[string]any{
			"fingerprint":             fingerprint,
			"title":                   group.Failure.Title,
			"count":                   group.Count,
			"affected_case_count":     len(group.AffectedCase),
			"latest_execution_id":     group.Latest["id"],
			"latest_failure_category": group.Latest["failure_category"],
		})
	}
	sort.Slice(result, func(i, j int) bool {
		leftCount, rightCount := result[i]["count"].(int), result[j]["count"].(int)
		if leftCount != rightCount {
			return leftCount > rightCount
		}
		leftCases, rightCases := result[i]["affected_case_count"].(int), result[j]["affected_case_count"].(int)
		if leftCases != rightCases {
			return leftCases > rightCases
		}
		leftID, rightID := result[i]["latest_execution_id"].(int64), result[j]["latest_execution_id"].(int64)
		if leftID != rightID {
			return leftID > rightID
		}
		return result[i]["title"].(string) < result[j]["title"].(string)
	})
	if len(result) > failureRootCauseLimit {
		result = result[:failureRootCauseLimit]
	}
	return result
}

func describeOverviewFailure(row map[string]any) overviewFailure {
	signal, _ := row["failure_signal"].(map[string]any)
	fingerprint, _ := signal["fingerprint"].(string)
	title, _ := signal["title"].(string)
	if title == "" {
		title = failureTitle(row)
	}
	if fingerprint == "" && isFailureStatus(row["status"]) {
		category, _ := row["failure_category"].(string)
		action, _ := row["failure_step_action"].(string)
		source := strings.Join([]string{
			defaultString(category, "unknown"),
			defaultString(action, "unknown"),
			strings.ToLower(title),
		}, "|")
		fingerprint = fmt.Sprintf("%x", sha1.Sum([]byte(source)))[:16]
	}
	return overviewFailure{Fingerprint: fingerprint, Title: title}
}

func failureTitle(row map[string]any) string {
	if report, ok := row["report"].(map[string]any); ok {
		if steps, ok := report["steps"].([]any); ok {
			for _, raw := range steps {
				step, ok := raw.(map[string]any)
				if !ok || step["status"] != "failed" {
					continue
				}
				if message, ok := step["error_message"].(string); ok {
					if normalized := normalizeMessage(message); normalized != "" {
						return normalized
					}
				}
				break
			}
		}
	}
	if message, ok := row["error_message"].(string); ok {
		if normalized := normalizeMessage(message); normalized != "" {
			return normalized
		}
	}
	category, _ := row["failure_category"].(string)
	action, _ := row["failure_step_action"].(string)
	return defaultString(category, "runner") + ":" + defaultString(action, "unknown")
}

func latestRuns(rows []map[string]any, includeFailures bool) []map[string]any {
	result := make([]map[string]any, 0, latestFailedRunsLimit)
	for _, row := range rows {
		status, _ := row["status"].(string)
		if (includeFailures && status != "failed" && status != "needs_intervention") ||
			(!includeFailures && status != "needs_intervention") {
			continue
		}
		result = append(result, executionSummary(row))
		if len(result) == latestFailedRunsLimit {
			break
		}
	}
	return result
}

func executionSummary(row map[string]any) map[string]any {
	result := make(map[string]any, len(row))
	for key, value := range row {
		switch key {
		case "dsl_snapshot", "report", "analysis_status", "analysis":
			continue
		default:
			result[key] = value
		}
	}
	return result
}

func snapshotMap(snapshot overviewSnapshot) map[string]any {
	return map[string]any{
		"total_count":     snapshot.TotalCount,
		"passed_count":    snapshot.PassedCount,
		"failed_count":    snapshot.FailedCount,
		"running_count":   snapshot.RunningCount,
		"pass_rate":       snapshot.PassRate,
		"avg_duration_ms": snapshot.AvgDurationMS,
	}
}

func comparisonMap(current, previous overviewSnapshot) map[string]any {
	return map[string]any{
		"total_count_delta":     current.TotalCount - previous.TotalCount,
		"passed_count_delta":    current.PassedCount - previous.PassedCount,
		"failed_count_delta":    current.FailedCount - previous.FailedCount,
		"running_count_delta":   current.RunningCount - previous.RunningCount,
		"pass_rate_delta":       roundRate(current.PassRate - previous.PassRate),
		"avg_duration_ms_delta": current.AvgDurationMS - previous.AvgDurationMS,
	}
}

func windowMap(window overviewWindow) map[string]any {
	return map[string]any{
		"start_date": window.Start.Format("2006-01-02"),
		"end_date":   window.End.Format("2006-01-02"),
	}
}

func countStatus(rows []map[string]any, status string) int {
	count := 0
	for _, row := range rows {
		if row["status"] == status {
			count++
		}
	}
	return count
}

func executionLater(left, right map[string]any) bool {
	leftTime, _ := left["started_at"].(time.Time)
	rightTime, _ := right["started_at"].(time.Time)
	if !leftTime.Equal(rightTime) {
		return leftTime.After(rightTime)
	}
	leftID, _ := left["id"].(int64)
	rightID, _ := right["id"].(int64)
	return leftID > rightID
}

func isFailureStatus(value any) bool {
	return value == "failed" || value == "needs_intervention"
}

func pointerValue(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return roundRate(float64(numerator) / float64(denominator))
}

func roundRate(value float64) float64 {
	return math.Round(value*10000) / 10000
}

func normalizeMessage(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
