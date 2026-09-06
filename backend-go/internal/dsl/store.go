package dsl

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	ErrNotFound     = errors.New("DSL generation not found")
	ErrAccessDenied = errors.New("DSL generation access denied")
)

const CanonicalVersion = "dsl.canonical.v1"

var supportedActions = map[string]struct{}{
	"goto": {}, "click": {}, "input": {}, "wait_for": {},
	"assert_text": {}, "assert_url_contains": {}, "capture_text": {},
}

var (
	contextKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	variableTypes     = stringSet("string", "number", "boolean", "object", "array")
	variableSources   = stringSet(
		"latest_url", "error_message", "status", "last_step_url",
		"last_step_page_title", "last_step_target", "last_step_value",
		"last_step_error_message",
	)
	locatorConfidences = stringSet("high", "medium", "low")
	targetStrategies   = stringSet(
		"css", "xpath", "data-testid", "element_id", "tag",
	)
	postconditionTypes = stringSet(
		"url_contains", "url_changes", "text_visible", "text_gone",
		"element_visible", "element_gone", "network_request", "dom_changed",
		"value_changed",
	)
)

type Generation struct {
	ID               int64
	Case             json.RawMessage
	DSLHash          string
	CanonicalVersion string
	Success          bool
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateGeneration(
	ctx context.Context,
	actorUserID, projectID int64,
	candidate json.RawMessage,
	warnings []string,
) (Generation, error) {
	normalized, baseURL, err := ValidateCase(candidate)
	if err != nil {
		return Generation{}, err
	}
	var member bool
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM project_members WHERE project_id = $1 AND user_id = $2
		)`, projectID, actorUserID).Scan(&member); err != nil {
		return Generation{}, err
	}
	if !member {
		return Generation{}, ErrAccessDenied
	}
	dslHash := SHA256(normalized)
	warningsJSON, _ := json.Marshal(warnings)
	baseURLSource := "none"
	if baseURL != "" {
		baseURLSource = "request"
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO dsl_generation_runs (
			actor_user_id, project_id, case_id, prompt_preview, prompt_sha256,
			prompt_version, prompt_variant, retry_from_generation_id,
			retry_reason_code, retry_note, request_base_url, generation_mode,
			import_mode, model_name, success, error_type, error_message,
			used_current_case_context, used_current_steps_context, context_profile,
			base_url_source, base_url_backfilled, repaired_invalid_actions,
			removed_invalid_steps, removed_invalid_contracts,
			preserve_contracts_requested, preserve_contracts_applied,
			warnings_count, normalization_notes_count, warnings_json,
			normalization_notes_json, governance_focus_reasons_json,
			risk_flags_json, generated_case_json, dsl_sha256,
			dsl_canonical_version, feedback_status,
			feedback_import_mode, rejection_reason_code, feedback_note,
			feedback_recorded_at
		) VALUES (
			$1, $2, NULL, 'AgentService DSL candidate', $3,
			'agentservice.dsl.v1', 'baseline_draft', NULL,
			NULL, NULL, $4, 'draft',
			'replace', 'agentservice', true, NULL, NULL,
			false, false, 'blank_request',
			$5, false, 0,
			0, 0,
			false, false,
			$6, 0, $7,
			'[]'::json, '[]'::json,
			'[]'::json, $8, $9, $10, 'pending',
			NULL, NULL, NULL,
			NULL
		)
		RETURNING id`,
		actorUserID,
		projectID,
		dslHash,
		nullableText(baseURL),
		baseURLSource,
		len(warnings),
		string(warningsJSON),
		string(normalized),
		dslHash,
		CanonicalVersion,
	).Scan(&id)
	if err != nil {
		return Generation{}, fmt.Errorf("persist DSL generation: %w", err)
	}
	return Generation{
		ID: id, Case: normalized, DSLHash: dslHash,
		CanonicalVersion: CanonicalVersion, Success: true,
	}, nil
}

func (s *Store) GetGeneration(
	ctx context.Context,
	actorUserID, projectID, generationID int64,
) (Generation, error) {
	var generation Generation
	var raw []byte
	var storedHash sql.NullString
	var storedVersion sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT g.id, g.generated_case_json, g.dsl_sha256,
		       g.dsl_canonical_version, g.success
		FROM dsl_generation_runs g
		JOIN project_members pm ON pm.project_id = g.project_id
		WHERE g.id = $1 AND g.project_id = $2
		  AND g.actor_user_id = $3 AND pm.user_id = $3`,
		generationID, projectID, actorUserID,
	).Scan(&generation.ID, &raw, &storedHash, &storedVersion, &generation.Success)
	if errors.Is(err, sql.ErrNoRows) {
		return Generation{}, ErrNotFound
	}
	if err != nil {
		return Generation{}, err
	}
	if len(raw) == 0 {
		return Generation{}, ErrNotFound
	}
	canonical, _, err := ValidateCase(raw)
	if err != nil {
		return Generation{}, fmt.Errorf("canonicalize persisted DSL generation: %w", err)
	}
	generation.Case = canonical
	generation.DSLHash = SHA256(canonical)
	generation.CanonicalVersion = CanonicalVersion
	if storedHash.Valid && storedHash.String != generation.DSLHash {
		return Generation{}, errors.New("persisted DSL generation SHA does not match its canonical JSON")
	}
	if storedVersion.Valid && storedVersion.String != CanonicalVersion {
		return Generation{}, fmt.Errorf("unsupported DSL canonical version: %s", storedVersion.String)
	}
	if !storedHash.Valid || !storedVersion.Valid || string(raw) != string(canonical) {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE dsl_generation_runs
			SET generated_case_json = $2, dsl_sha256 = $3, dsl_canonical_version = $4
			WHERE id = $1`,
			generation.ID, string(canonical), generation.DSLHash, CanonicalVersion,
		); err != nil {
			return Generation{}, fmt.Errorf("backfill canonical DSL generation: %w", err)
		}
	}
	return generation, nil
}

func ValidateCase(raw json.RawMessage) (json.RawMessage, string, error) {
	var candidate map[string]any
	if !json.Valid(raw) || json.Unmarshal(raw, &candidate) != nil {
		return nil, "", errors.New("case must be an object")
	}
	delete(candidate, "_preflight")
	name, _ := candidate["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return nil, "", errors.New("case.name must contain 1 to 200 characters")
	}
	candidate["name"] = name
	if err := validateOptionalText(candidate, "description", 1000); err != nil {
		return nil, "", err
	}
	if err := validateOptionalText(candidate, "base_url", 500); err != nil {
		return nil, "", err
	}
	steps, ok := candidate["steps"].([]any)
	if !ok || len(steps) == 0 {
		return nil, "", errors.New("case.steps must be a non-empty array")
	}
	for index, rawStep := range steps {
		step, ok := rawStep.(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("case.steps[%d] must be an object", index)
		}
		action, _ := step["action"].(string)
		if _, ok := supportedActions[action]; !ok {
			return nil, "", fmt.Errorf("unsupported DSL action: %s", action)
		}
		if err := validateStep(index, action, step); err != nil {
			return nil, "", err
		}
	}
	for _, field := range []string{"input_contract", "output_contract"} {
		if value, exists := candidate[field]; exists {
			contracts, ok := value.([]any)
			if !ok {
				return nil, "", fmt.Errorf("case.%s must be an array", field)
			}
			if err := validateContracts(field, contracts); err != nil {
				return nil, "", err
			}
		} else {
			candidate[field] = []any{}
		}
	}
	candidate = canonicalCase(candidate)
	baseURL, _ := candidate["base_url"].(string)
	normalized, err := json.Marshal(candidate)
	return normalized, baseURL, err
}

// SHA256 hashes the exact UTF-8 JSON bytes returned by ValidateCase.
func SHA256(canonicalJSON json.RawMessage) string {
	hash := sha256.Sum256(canonicalJSON)
	return hex.EncodeToString(hash[:])
}

func canonicalCase(candidate map[string]any) map[string]any {
	return map[string]any{
		"name":            trimmedString(candidate["name"]),
		"description":     optionalTrimmedString(candidate["description"]),
		"base_url":        optionalTrimmedString(candidate["base_url"]),
		"input_contract":  canonicalContracts(candidate["input_contract"], true),
		"output_contract": canonicalContracts(candidate["output_contract"], false),
		"steps":           canonicalSteps(candidate["steps"].([]any)),
	}
}

func canonicalContracts(raw any, input bool) []any {
	contracts, _ := raw.([]any)
	result := make([]any, 0, len(contracts))
	for _, item := range contracts {
		contract := item.(map[string]any)
		canonical := map[string]any{
			"name":        trimmedString(contract["name"]),
			"context_key": trimmedString(contract["context_key"]),
			"value_type":  trimmedString(contract["value_type"]),
			"description": optionalTrimmedString(contract["description"]),
		}
		if input {
			required, exists := contract["required"]
			if !exists {
				required = true
			}
			canonical["required"] = required
			canonical["value"] = optionalTrimmedString(contract["value"])
		} else {
			canonical["source"] = optionalTrimmedString(contract["source"])
		}
		result = append(result, canonical)
	}
	return result
}

func canonicalSteps(steps []any) []any {
	result := make([]any, 0, len(steps))
	for _, item := range steps {
		step := item.(map[string]any)
		action := trimmedString(step["action"])
		canonical := map[string]any{"action": action}
		switch action {
		case "goto", "assert_url_contains":
			canonical["value"] = trimmedString(step["value"])
			canonicalOptionalConditions(canonical, step)
		case "click", "wait_for", "assert_text", "capture_text":
			canonicalLocatorFields(canonical, step)
			if action == "wait_for" {
				canonical["timeout_ms"] = valueOrDefault(step, "timeout_ms", float64(5000))
			}
			if action == "assert_text" {
				canonical["value"] = trimmedString(step["value"])
			}
			if action == "capture_text" {
				canonical["context_key"] = trimmedString(step["context_key"])
			}
		case "input":
			canonical["target"] = trimmedString(step["target"])
			canonical["value"] = trimmedString(step["value"])
			canonical["trigger"] = optionalTrimmedString(step["trigger"])
			canonicalLocatorDefaults(canonical, step)
		}
		result = append(result, canonical)
	}
	return result
}

func canonicalLocatorFields(canonical, step map[string]any) {
	canonical["target"] = trimmedString(step["target"])
	canonicalLocatorDefaults(canonical, step)
}

func canonicalLocatorDefaults(canonical, step map[string]any) {
	canonical["page_state"] = optionalTrimmedString(step["page_state"])
	canonical["target_strategy"] = optionalTrimmedString(step["target_strategy"])
	canonical["locator_confidence"] = optionalTrimmedString(step["locator_confidence"])
	canonical["candidates"] = canonicalCandidates(step["candidates"])
	if _, exists := step["preconditions"]; exists {
		canonical["preconditions"] = canonicalConditions(step["preconditions"])
	}
	canonical["postconditions"] = canonicalConditions(step["postconditions"])
}

func canonicalCandidates(raw any) []any {
	candidates, _ := raw.([]any)
	result := make([]any, 0, len(candidates))
	for _, item := range candidates {
		candidate := item.(map[string]any)
		strategy := trimmedString(candidate["strategy"])
		switch strategy {
		case "css_selector":
			strategy = "css"
		case "data_testid":
			strategy = "data-testid"
		case "elementId":
			strategy = "element_id"
		case "href":
			strategy = "css"
		case "link", "button", "aria":
			strategy = "role"
		case "id":
			strategy = "element_id"
		case "name":
			strategy = "tag"
		}
		canonical := map[string]any{
			"strategy":       strategy,
			"selector":       optionalTrimmedString(candidate["selector"]),
			"semantic_value": optionalTrimmedString(candidate["semantic_value"]),
			"pre_score":      candidate["pre_score"],
			"pre_features":   valueOrDefault(candidate, "pre_features", nil),
		}
		result = append(result, canonical)
	}
	return result
}

func canonicalOptionalConditions(canonical, step map[string]any) {
	for _, field := range []string{"preconditions", "postconditions"} {
		if _, exists := step[field]; exists {
			canonical[field] = canonicalConditions(step[field])
		}
	}
}

func canonicalConditions(raw any) []any {
	conditions, _ := raw.([]any)
	result := make([]any, 0, len(conditions))
	for _, item := range conditions {
		condition := item.(map[string]any)
		canonical := map[string]any{
			"type":       trimmedString(condition["type"]),
			"value":      optionalTrimmedString(condition["value"]),
			"timeout_ms": valueOrDefault(condition, "timeout_ms", float64(3000)),
		}
		if _, exists := condition["method"]; exists {
			canonical["method"] = strings.ToUpper(trimmedString(condition["method"]))
		}
		if _, exists := condition["status"]; exists {
			canonical["status"] = condition["status"]
		}
		result = append(result, canonical)
	}
	return result
}

func trimmedString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func optionalTrimmedString(value any) any {
	if value == nil {
		return nil
	}
	return trimmedString(value)
}

func valueOrDefault(object map[string]any, field string, fallback any) any {
	if value, exists := object[field]; exists {
		return value
	}
	return fallback
}

func validateStep(index int, action string, step map[string]any) error {
	switch action {
	case "goto", "assert_url_contains":
		if err := requireText(step, "value", index); err != nil {
			return err
		}
	case "click", "wait_for":
		if err := requireText(step, "target", index); err != nil {
			return err
		}
	case "input", "assert_text":
		if err := requireText(step, "target", index); err != nil {
			return err
		}
		value, ok := step["value"].(string)
		if !ok || (action == "assert_text" && strings.TrimSpace(value) == "") {
			return fmt.Errorf("case.steps[%d].value is required", index)
		}
	case "capture_text":
		if err := requireText(step, "target", index); err != nil {
			return err
		}
		key, _ := step["context_key"].(string)
		if !validContextKey(strings.TrimSpace(key)) {
			return fmt.Errorf("case.steps[%d].context_key is invalid", index)
		}
	}
	if !validOptionalEnum(step, "locator_confidence", locatorConfidences) {
		return fmt.Errorf("case.steps[%d].locator_confidence is invalid", index)
	}
	if !validOptionalEnum(step, "target_strategy", targetStrategies) {
		return fmt.Errorf("case.steps[%d].target_strategy is invalid", index)
	}
	for _, field := range []string{"page_state", "trigger"} {
		if value, exists := step[field]; exists && value != nil {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("case.steps[%d].%s must be a string", index, field)
			}
		}
	}
	if action == "wait_for" {
		if rawTimeout, exists := step["timeout_ms"]; exists {
			timeout, ok := rawTimeout.(float64)
			if !ok || timeout < 1 || timeout > 60000 {
				return fmt.Errorf("case.steps[%d].timeout_ms must be between 1 and 60000", index)
			}
		}
	}
	if err := validateCandidates(index, step["candidates"]); err != nil {
		return err
	}
	if err := validateConditions(index, "preconditions", step["preconditions"]); err != nil {
		return err
	}
	if err := validateConditions(index, "postconditions", step["postconditions"]); err != nil {
		return err
	}
	if action == "click" {
		if href := verifiedCrossPageAnchorHref(step["candidates"]); href != "" &&
			!hasTargetURLPostcondition(step["postconditions"], href) {
			return fmt.Errorf(
				"case.steps[%d] cross-page anchor click requires a url_contains postcondition matching %q",
				index,
				href,
			)
		}
	}
	return nil
}

func validOptionalEnum(object map[string]any, field string, allowed map[string]bool) bool {
	value, exists := object[field]
	if !exists || value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && allowed[strings.TrimSpace(text)]
}

func validateContracts(field string, contracts []any) error {
	for index, raw := range contracts {
		contract, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("case.%s[%d] must be an object", field, index)
		}
		name, _ := contract["name"].(string)
		key, _ := contract["context_key"].(string)
		valueType, _ := contract["value_type"].(string)
		if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 100 {
			return fmt.Errorf("case.%s[%d].name is invalid", field, index)
		}
		if !validContextKey(strings.TrimSpace(key)) || !variableTypes[strings.TrimSpace(valueType)] {
			return fmt.Errorf("case.%s[%d] has an invalid context_key or value_type", field, index)
		}
		if required, exists := contract["required"]; exists {
			if _, ok := required.(bool); !ok {
				return fmt.Errorf("case.%s[%d].required must be a boolean", field, index)
			}
		}
		for _, textField := range []string{"description", "value"} {
			if value, exists := contract[textField]; exists && value != nil {
				if _, ok := value.(string); !ok {
					return fmt.Errorf("case.%s[%d].%s must be a string", field, index, textField)
				}
			}
		}
		if field == "output_contract" {
			if source, exists := contract["source"]; exists && source != nil {
				value, ok := source.(string)
				if !ok || !variableSources[strings.TrimSpace(value)] {
					return fmt.Errorf("case.%s[%d].source is invalid", field, index)
				}
			}
		}
	}
	return nil
}

func validateCandidates(stepIndex int, raw any) error {
	if raw == nil {
		return nil
	}
	candidates, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("case.steps[%d].candidates must be an array", stepIndex)
	}
	for index, rawCandidate := range candidates {
		candidate, ok := rawCandidate.(map[string]any)
		if !ok {
			return fmt.Errorf("case.steps[%d].candidates[%d] must be an object", stepIndex, index)
		}
		if err := requireText(candidate, "strategy", index); err != nil {
			return fmt.Errorf("case.steps[%d].candidates[%d].strategy is required", stepIndex, index)
		}
		score, ok := candidate["pre_score"].(float64)
		if !ok || score < 0 || score > 1 {
			return fmt.Errorf("case.steps[%d].candidates[%d].pre_score must be between 0 and 1", stepIndex, index)
		}
		for _, field := range []string{"selector", "semantic_value"} {
			if value, exists := candidate[field]; exists && value != nil {
				if _, ok := value.(string); !ok {
					return fmt.Errorf("case.steps[%d].candidates[%d].%s must be a string", stepIndex, index, field)
				}
			}
		}
		if features, exists := candidate["pre_features"]; exists && features != nil {
			if _, ok := features.(map[string]any); !ok {
				return fmt.Errorf("case.steps[%d].candidates[%d].pre_features must be an object", stepIndex, index)
			}
		}
	}
	return nil
}

func validateConditions(stepIndex int, field string, raw any) error {
	if raw == nil {
		return nil
	}
	conditions, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("case.steps[%d].%s must be an array", stepIndex, field)
	}
	for index, rawCondition := range conditions {
		condition, ok := rawCondition.(map[string]any)
		if !ok {
			return fmt.Errorf("case.steps[%d].%s[%d] must be an object", stepIndex, field, index)
		}
		kind, _ := condition["type"].(string)
		if !postconditionTypes[strings.TrimSpace(kind)] {
			return fmt.Errorf("case.steps[%d].%s[%d].type is invalid", stepIndex, field, index)
		}
		if strings.TrimSpace(kind) == "url_contains" {
			value, _ := condition["value"].(string)
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf(
					"case.steps[%d].%s[%d].value is required for url_contains",
					stepIndex,
					field,
					index,
				)
			}
		}
		if strings.TrimSpace(kind) == "network_request" &&
			condition["value"] == nil && condition["method"] == nil && condition["status"] == nil {
			return fmt.Errorf(
				"case.steps[%d].%s[%d] requires URL, method, or status",
				stepIndex, field, index,
			)
		}
		if value, exists := condition["value"]; exists && value != nil {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("case.steps[%d].%s[%d].value must be a string", stepIndex, field, index)
			}
		}
		if method, exists := condition["method"]; exists && method != nil {
			text, ok := method.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return fmt.Errorf("case.steps[%d].%s[%d].method must be a string", stepIndex, field, index)
			}
		}
		if status, exists := condition["status"]; exists && status != nil {
			number, ok := status.(float64)
			if !ok || number < 100 || number > 599 || number != float64(int(number)) {
				return fmt.Errorf("case.steps[%d].%s[%d].status must be between 100 and 599", stepIndex, field, index)
			}
		}
		if rawTimeout, exists := condition["timeout_ms"]; exists {
			timeout, ok := rawTimeout.(float64)
			if !ok || timeout < 100 || timeout > 30000 {
				return fmt.Errorf("case.steps[%d].%s[%d].timeout_ms must be between 100 and 30000", stepIndex, field, index)
			}
		}
	}
	return nil
}

func verifiedCrossPageAnchorHref(raw any) string {
	candidates, _ := raw.([]any)
	for _, rawCandidate := range candidates {
		candidate, _ := rawCandidate.(map[string]any)
		features, _ := candidate["pre_features"].(map[string]any)
		href, _ := features["verified_href"].(string)
		href = strings.TrimSpace(href)
		if href == "" || strings.HasPrefix(href, "#") {
			continue
		}
		parsed, err := url.Parse(href)
		if err != nil || (parsed.IsAbs() && parsed.Scheme != "http" && parsed.Scheme != "https") {
			continue
		}
		return href
	}
	return ""
}

func hasTargetURLPostcondition(raw any, href string) bool {
	postconditions, _ := raw.([]any)
	target, err := url.Parse(href)
	if err != nil {
		return false
	}
	targetPath := target.EscapedPath()
	if target.RawQuery != "" {
		targetPath += "?" + target.RawQuery
	}
	for _, rawPostcondition := range postconditions {
		postcondition, _ := rawPostcondition.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(postcondition["type"])) != "url_contains" {
			continue
		}
		value := strings.TrimSpace(fmt.Sprint(postcondition["value"]))
		if value != "" &&
			(strings.Contains(href, value) ||
				strings.Contains(value, href) ||
				(targetPath != "" && (strings.Contains(targetPath, value) || strings.Contains(value, targetPath)))) {
			return true
		}
	}
	return false
}

func validateOptionalText(object map[string]any, field string, maxLength int) error {
	value, exists := object[field]
	if !exists || value == nil {
		return nil
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > maxLength {
		return fmt.Errorf("case.%s is invalid", field)
	}
	return nil
}

func requireText(object map[string]any, field string, index int) error {
	value, ok := object[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fmt.Errorf("case.steps[%d].%s is required", index, field)
	}
	return nil
}

func validContextKey(value string) bool {
	return utf8.RuneCountInString(value) <= 100 && contextKeyPattern.MatchString(value)
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func nullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
