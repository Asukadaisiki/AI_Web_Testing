package corrections

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var ErrNotFound = errors.New("correction source execution not found")

var (
	uuidSegment  = regexp.MustCompile(`(?i)^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)
	tokenSegment = regexp.MustCompile(`^[[:alnum:]]{16,}$`)
)

type CreateRequest struct {
	PageURL           string `json:"page_url" vd:"len($)>0"`
	TargetDescription string `json:"target_description" vd:"len($)>0"`
	CorrectionType    string `json:"correction_type" vd:"in($,'css','xpath','test_id')"`
	CorrectionValue   string `json:"correction_value" vd:"len($)>0"`
	SourceExecutionID int64  `json:"source_execution_id" vd:"$>0"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Create(
	ctx context.Context,
	actorUserID int64,
	request CreateRequest,
) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var projectID int64
	err = tx.QueryRowContext(ctx, `
		SELECT r.project_id
		FROM test_case_runs r
		JOIN project_members pm ON pm.project_id = r.project_id
		WHERE r.id = $1 AND pm.user_id = $2`,
		request.SourceExecutionID, actorUserID,
	).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	pattern, err := generalizeURL(request.PageURL)
	if err != nil {
		return nil, err
	}
	normalizedTarget := strings.ToLower(strings.Join(strings.Fields(request.TargetDescription), " "))
	var correctionID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM locator_corrections
		WHERE page_url_pattern = $1
		  AND normalized_target_description = $2
		  AND is_active = true
		FOR UPDATE`, pattern, normalizedTarget,
	).Scan(&correctionID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO locator_corrections (
				page_url_pattern, target_description, normalized_target_description,
				correction_type, correction_value, source_execution_id, created_by,
				verified_count, consecutive_failures, is_active, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, 0, 0, true, now(), now())
			RETURNING id`,
			pattern, request.TargetDescription, normalizedTarget,
			request.CorrectionType, request.CorrectionValue,
			request.SourceExecutionID, actorUserID,
		).Scan(&correctionID)
	} else if err == nil {
		_, err = tx.ExecContext(ctx, `
			UPDATE locator_corrections
			SET correction_type = $2, correction_value = $3,
			    source_execution_id = $4, updated_at = now()
			WHERE id = $1`,
			correctionID, request.CorrectionType, request.CorrectionValue,
			request.SourceExecutionID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("persist locator correction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO locator_correction_events (
			correction_id, event_type, page_url_pattern, target_description,
			execution_id, verified_count_after, consecutive_failures_after, is_active_after
		) VALUES ($1, 'created', $2, $3, $4, 0, 0, true)`,
		correctionID, pattern, request.TargetDescription, request.SourceExecutionID,
	); err != nil {
		return nil, fmt.Errorf("record locator correction event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, actorUserID, correctionID)
}

func (s *Store) Get(ctx context.Context, actorUserID, correctionID int64) (map[string]any, error) {
	var (
		id, verified, failures, sourceExecutionID, createdBy int64
		pattern, target, correctionType, value               string
		active                                               bool
		createdAt, updatedAt                                 time.Time
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT c.id, c.page_url_pattern, c.target_description, c.correction_type,
		       c.correction_value, c.verified_count, c.consecutive_failures,
		       c.is_active, c.source_execution_id, c.created_by, c.created_at, c.updated_at
		FROM locator_corrections c
		JOIN test_case_runs r ON r.id = c.source_execution_id
		JOIN project_members pm ON pm.project_id = r.project_id
		WHERE c.id = $1 AND pm.user_id = $2`, correctionID, actorUserID,
	).Scan(
		&id, &pattern, &target, &correctionType, &value, &verified, &failures,
		&active, &sourceExecutionID, &createdBy, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "page_url_pattern": pattern, "target_description": target,
		"correction_type": correctionType, "correction_value": value,
		"verified_count": verified, "consecutive_failures": failures,
		"is_active": active, "source_execution_id": sourceExecutionID,
		"created_by": createdBy, "created_at": createdAt, "updated_at": updatedAt,
	}, nil
}

func generalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("page_url must be an absolute URL")
	}
	segments := strings.Split(parsed.Path, "/")
	for index, segment := range segments {
		if segment == "" {
			continue
		}
		if allDigits(segment) || uuidSegment.MatchString(segment) ||
			(tokenSegment.MatchString(segment) && containsLetterAndDigit(segment)) {
			segments[index] = "*"
		}
	}
	query := parsed.Query()
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		value := query.Get(key)
		if allDigits(value) || uuidSegment.MatchString(value) ||
			(tokenSegment.MatchString(value) && containsLetterAndDigit(value)) {
			value = "*"
		}
		values = append(values, url.QueryEscape(key)+"="+url.QueryEscape(value))
	}
	result := parsed.Scheme + "://" + parsed.Host + strings.Join(segments, "/")
	if len(values) > 0 {
		result += "?" + strings.Join(values, "&")
	}
	return result, nil
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func containsLetterAndDigit(value string) bool {
	hasLetter, hasDigit := false, false
	for _, character := range value {
		hasLetter = hasLetter || character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z'
		hasDigit = hasDigit || character >= '0' && character <= '9'
	}
	return hasLetter && hasDigit
}
