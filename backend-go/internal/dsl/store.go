package dsl

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	ErrNotFound     = errors.New("DSL generation not found")
	ErrAccessDenied = errors.New("DSL generation access denied")
)

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
		"css", "css_selector", "xpath", "data-testid", "data_testid",
		"element_id", "elementId", "tag", "role", "role_fuzzy",
		"link_role", "link_role_fuzzy", "label", "label_fuzzy",
		"placeholder", "placeholder_fuzzy", "text", "text_fuzzy",
		"semantic", "vlm", "verified_role", "verified_role_fuzzy",
		"verified_css", "verified_xpath", "verified_placeholder",
		"verified_placeholder_fuzzy", "verified_label", "verified_label_fuzzy",
		"verified_text", "verified_element_id", "verified_name",
		"href", "link", "button", "aria", "id",
	)
	postconditionTypes = stringSet(
		"url_contains", "url_changes", "text_visible", "text_gone",
		"element_visible", "element_gone", "network_request", "dom_changed",
		"value_changed",
	)
)

type Generation struct {
	ID      int64
	Case    json.RawMessage
	Success bool
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
	hash := sha256.Sum256(normalized)
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
			risk_flags_json, generated_case_json, feedback_status,
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
			'[]'::json, $8, 'pending',
			NULL, NULL, NULL,
			NULL
		)
		RETURNING id`,
		actorUserID,
		projectID,
		hex.EncodeToString(hash[:]),
		nullableText(baseURL),
		baseURLSource,
		len(warnings),
		string(warningsJSON),
		string(normalized),
	).Scan(&id)
	if err != nil {
		return Generation{}, fmt.Errorf("persist DSL generation: %w", err)
	}
	return Generation{ID: id, Case: normalized, Success: true}, nil
}

func (s *Store) GetGeneration(
	ctx context.Context,
	actorUserID, projectID, generationID int64,
) (Generation, error) {
	var generation Generation
	var raw []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT g.id, g.generated_case_json, g.success
		FROM dsl_generation_runs g
		JOIN project_members pm ON pm.project_id = g.project_id
		WHERE g.id = $1 AND g.project_id = $2
		  AND g.actor_user_id = $3 AND pm.user_id = $3`,
		generationID, projectID, actorUserID,
	).Scan(&generation.ID, &raw, &generation.Success)
	if errors.Is(err, sql.ErrNoRows) {
		return Generation{}, ErrNotFound
	}
	if err != nil {
		return Generation{}, err
	}
	if len(raw) == 0 {
		return Generation{}, ErrNotFound
	}
	generation.Case = json.RawMessage(raw)
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
	baseURL, _ := candidate["base_url"].(string)
	normalized, err := json.Marshal(candidate)
	return normalized, baseURL, err
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
		if !validContextKey(key) {
			return fmt.Errorf("case.steps[%d].context_key is invalid", index)
		}
	}
	if confidence, exists := step["locator_confidence"]; exists {
		value, ok := confidence.(string)
		if !ok || !locatorConfidences[value] {
			return fmt.Errorf("case.steps[%d].locator_confidence is invalid", index)
		}
	}
	if strategy, exists := step["target_strategy"]; exists {
		value, ok := strategy.(string)
		if !ok || !targetStrategies[value] {
			return fmt.Errorf("case.steps[%d].target_strategy is invalid", index)
		}
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
	if err := validatePostconditions(index, step["postconditions"]); err != nil {
		return err
	}
	return nil
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
		if !validContextKey(key) || !variableTypes[valueType] {
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
				if !ok || !variableSources[value] {
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

func validatePostconditions(stepIndex int, raw any) error {
	if raw == nil {
		return nil
	}
	postconditions, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("case.steps[%d].postconditions must be an array", stepIndex)
	}
	for index, rawPostcondition := range postconditions {
		postcondition, ok := rawPostcondition.(map[string]any)
		if !ok {
			return fmt.Errorf("case.steps[%d].postconditions[%d] must be an object", stepIndex, index)
		}
		kind, _ := postcondition["type"].(string)
		if !postconditionTypes[kind] {
			return fmt.Errorf("case.steps[%d].postconditions[%d].type is invalid", stepIndex, index)
		}
		if value, exists := postcondition["value"]; exists && value != nil {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("case.steps[%d].postconditions[%d].value must be a string", stepIndex, index)
			}
		}
		if rawTimeout, exists := postcondition["timeout_ms"]; exists {
			timeout, ok := rawTimeout.(float64)
			if !ok || timeout < 100 || timeout > 30000 {
				return fmt.Errorf("case.steps[%d].postconditions[%d].timeout_ms must be between 100 and 30000", stepIndex, index)
			}
		}
	}
	return nil
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
