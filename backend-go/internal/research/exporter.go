package research

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	DefaultExportPageSize = 100
	MaxExportLineBytes    = 512 * 1024
	MaxExportStringBytes  = 16 * 1024
	MaxExportArrayItems   = 2048
	MaxExportObjectFields = 512
)

var (
	secretKeyPattern = regexp.MustCompile(
		`^(authorization|proxyauthorization|cookie|setcookie|password|passwd|secret|clientsecret|apikey|accesstoken|refreshtoken|csrf|csrftoken|csrfmiddlewaretoken)$`,
	)
	signedURLQueryKeys = map[string]bool{
		"signature": true, "sig": true, "token": true, "expires": true,
		"x-amz-signature": true, "x-amz-credential": true,
		"x-amz-security-token": true, "x-goog-signature": true,
		"key-pair-id": true,
	}
)

type TrajectoryJSONL struct {
	SchemaVersion    string          `json:"schema_version"`
	ProjectorVersion string          `json:"projector_version"`
	ResearchRunID    string          `json:"research_run_id"`
	ExperimentID     string          `json:"experiment_id"`
	RepetitionIndex  int             `json:"repetition_index"`
	Warmup           bool            `json:"warmup"`
	Ordinal          int64           `json:"ordinal"`
	AppendKey        string          `json:"append_key"`
	ContentSHA256    string          `json:"content_sha256"`
	Transition       json.RawMessage `json:"transition"`
	ArtifactRefs     []ArtifactRef   `json:"artifact_refs"`
}

type ExportRepository interface {
	GetRun(context.Context, string) (ResearchRun, error)
	ListRuns(context.Context, RunFilter) ([]ResearchRun, error)
	ListTransitions(context.Context, TransitionFilter) ([]Transition, error)
}

type exportSnapshotter interface {
	withExportSnapshot(context.Context, func(ExportRepository) error) error
}

type JSONLExporter struct {
	repository ExportRepository
	pageSize   int
}

func NewJSONLExporter(repository ExportRepository) *JSONLExporter {
	return &JSONLExporter{
		repository: repository,
		pageSize:   DefaultExportPageSize,
	}
}

func (e *JSONLExporter) ExportRun(
	ctx context.Context,
	writer io.Writer,
	researchRunID string,
) error {
	return e.withSnapshot(ctx, func(snapshot *JSONLExporter) error {
		return snapshot.exportRun(ctx, writer, researchRunID)
	})
}

func (e *JSONLExporter) exportRun(
	ctx context.Context,
	writer io.Writer,
	researchRunID string,
) error {
	run, err := e.repository.GetRun(ctx, strings.TrimSpace(researchRunID))
	if err != nil {
		return err
	}
	buffered := bufio.NewWriter(writer)
	after := (*int64)(nil)
	expectedOrdinal := int64(0)
	for {
		page, err := e.repository.ListTransitions(ctx, TransitionFilter{
			ResearchRunID: run.ID,
			AfterOrdinal:  after,
			Limit:         e.pageSize,
		})
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		for _, transition := range page {
			if transition.Ordinal != expectedOrdinal {
				return fmt.Errorf(
					"%w: export ordinal %d, expected %d",
					ErrSourceChanged, transition.Ordinal, expectedOrdinal,
				)
			}
			line := TrajectoryJSONL{
				SchemaVersion:    ExportSchemaVersion,
				ProjectorVersion: run.Versions.ProjectorVersion,
				ResearchRunID:    run.ID, ExperimentID: run.ExperimentID,
				RepetitionIndex: run.RepetitionIndex, Warmup: run.Warmup,
				Ordinal: transition.Ordinal, AppendKey: transition.AppendKey,
				ContentSHA256: transition.ContentSHA256,
				Transition:    transition.PayloadJSON,
				ArtifactRefs:  append([]ArtifactRef(nil), transition.ArtifactRefs...),
			}
			encoded, err := encodeExportLine(line)
			if err != nil {
				return fmt.Errorf("export transition %d: %w", transition.Ordinal, err)
			}
			if _, err := buffered.Write(encoded); err != nil {
				return fmt.Errorf("write transition %d: %w", transition.Ordinal, err)
			}
			if err := buffered.WriteByte('\n'); err != nil {
				return fmt.Errorf("write transition newline: %w", err)
			}
			expectedOrdinal++
		}
		last := page[len(page)-1].Ordinal
		after = &last
		if len(page) < e.pageSize {
			break
		}
	}
	if expectedOrdinal == 0 {
		return fmt.Errorf("%w: research run has no projection", ErrNotFound)
	}
	if err := buffered.Flush(); err != nil {
		return fmt.Errorf("flush trajectory export: %w", err)
	}
	return nil
}

func (e *JSONLExporter) ExportExperiment(
	ctx context.Context,
	writer io.Writer,
	experimentID string,
) error {
	return e.withSnapshot(ctx, func(snapshot *JSONLExporter) error {
		return snapshot.exportExperiment(ctx, writer, experimentID)
	})
}

func (e *JSONLExporter) exportExperiment(
	ctx context.Context,
	writer io.Writer,
	experimentID string,
) error {
	experimentID = strings.TrimSpace(experimentID)
	offset := 0
	runs := make([]ResearchRun, 0)
	for {
		page, err := e.repository.ListRuns(ctx, RunFilter{
			ExperimentID: &experimentID,
			Limit:        e.pageSize,
			Offset:       offset,
		})
		if err != nil {
			return err
		}
		runs = append(runs, page...)
		if len(page) < e.pageSize {
			break
		}
		offset += len(page)
	}
	if len(runs) == 0 {
		return ErrNotFound
	}
	slices.SortFunc(runs, func(left, right ResearchRun) int {
		if left.RepetitionIndex != right.RepetitionIndex {
			if left.RepetitionIndex < right.RepetitionIndex {
				return -1
			}
			return 1
		}
		if left.Warmup != right.Warmup {
			if left.Warmup {
				return -1
			}
			return 1
		}
		return strings.Compare(left.ID, right.ID)
	})
	for _, run := range runs {
		if err := e.exportRun(ctx, writer, run.ID); err != nil {
			return err
		}
	}
	return nil
}

func (e *JSONLExporter) withSnapshot(
	ctx context.Context,
	export func(*JSONLExporter) error,
) error {
	snapshotter, ok := e.repository.(exportSnapshotter)
	if !ok {
		return export(e)
	}
	return snapshotter.withExportSnapshot(ctx, func(repository ExportRepository) error {
		return export(&JSONLExporter{repository: repository, pageSize: e.pageSize})
	})
}

func ValidateTrajectoryJSONLLine(raw []byte) error {
	if len(raw) == 0 || len(raw) > MaxExportLineBytes || !utf8.Valid(raw) {
		return fmt.Errorf("%w: invalid JSONL line size or encoding", ErrInvalid)
	}
	if bytes.ContainsAny(raw, "\r\n") {
		return fmt.Errorf("%w: JSONL line contains newline", ErrInvalid)
	}
	var line TrajectoryJSONL
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&line); err != nil {
		return fmt.Errorf("%w: JSONL line: %v", ErrInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%w: JSONL line has trailing data", ErrInvalid)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil ||
		bytes.Equal(bytes.TrimSpace(envelope["artifact_refs"]), []byte("null")) {
		return fmt.Errorf("%w: artifact_refs must be an array", ErrInvalid)
	}
	if line.SchemaVersion != ExportSchemaVersion ||
		line.ProjectorVersion != ProjectorVersion ||
		line.ResearchRunID == "" || line.ExperimentID == "" ||
		len(line.ResearchRunID) > 64 || len(line.ExperimentID) > 64 ||
		line.RepetitionIndex < 0 || line.Ordinal < 0 ||
		line.AppendKey == "" || len(line.AppendKey) > 200 ||
		!sha256Pattern.MatchString(line.ContentSHA256) ||
		len(line.ArtifactRefs) > MaxArtifactRefs {
		return fmt.Errorf("%w: JSONL envelope", ErrInvalid)
	}
	var payload TransitionPayloadV1
	decoder = json.NewDecoder(bytes.NewReader(line.Transition))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("%w: JSONL transition payload", ErrInvalid)
	}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%w: JSONL transition has trailing data", ErrInvalid)
	}
	if err := payload.Validate(); err != nil {
		return err
	}
	for index := range line.ArtifactRefs {
		if err := line.ArtifactRefs[index].NormalizeAndValidate(); err != nil {
			return err
		}
		if err := validateArtifactURI(line.ArtifactRefs[index].URI); err != nil {
			return fmt.Errorf("artifact_refs[%d]: %w", index, err)
		}
	}
	hash, err := TransitionContentSHA256(
		SchemaVersion, line.Transition, line.ArtifactRefs,
	)
	if err != nil || hash != line.ContentSHA256 {
		return fmt.Errorf("%w: JSONL transition content hash", ErrInvalid)
	}
	var value any
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	return validateExportSafety(value, "$", 0)
}

func encodeExportLine(line TrajectoryJSONL) ([]byte, error) {
	if line.ArtifactRefs == nil {
		line.ArtifactRefs = []ArtifactRef{}
	}
	if err := validateExportLineInput(line); err != nil {
		return nil, err
	}
	sortArtifactRefs(line.ArtifactRefs)
	raw, err := json.Marshal(line)
	if err != nil {
		return nil, fmt.Errorf("encode JSONL line: %w", err)
	}
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return nil, err
	}
	if err := ValidateTrajectoryJSONLLine(canonical); err != nil {
		return nil, err
	}
	return canonical, nil
}

func validateExportLineInput(line TrajectoryJSONL) error {
	values := []string{
		line.SchemaVersion,
		line.ProjectorVersion,
		line.ResearchRunID,
		line.ExperimentID,
		line.AppendKey,
		line.ContentSHA256,
	}
	for _, value := range values {
		if !utf8.ValidString(value) {
			return fmt.Errorf("%w: export envelope is not UTF-8", ErrInvalid)
		}
	}
	if !utf8.Valid(line.Transition) {
		return fmt.Errorf("%w: transition is not UTF-8", ErrInvalid)
	}
	for index := range line.ArtifactRefs {
		artifact := line.ArtifactRefs[index]
		for _, value := range []string{
			artifact.Kind,
			artifact.URI,
			artifact.SHA256,
			artifact.MediaType,
			artifact.SchemaVersion,
		} {
			if !utf8.ValidString(value) {
				return fmt.Errorf(
					"%w: artifact_refs[%d] is not UTF-8",
					ErrInvalid, index,
				)
			}
		}
		if err := artifact.NormalizeAndValidate(); err != nil {
			return err
		}
		if err := validateArtifactURI(artifact.URI); err != nil {
			return fmt.Errorf("artifact_refs[%d]: %w", index, err)
		}
		line.ArtifactRefs[index] = artifact
	}
	return nil
}

func validateExportSafety(value any, path string, depth int) error {
	if depth > 64 {
		return fmt.Errorf("%w: export nesting exceeds limit at %s", ErrInvalid, path)
	}
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) > MaxExportObjectFields {
			return fmt.Errorf("%w: export object too large at %s", ErrInvalid, path)
		}
		for key, child := range typed {
			normalized := normalizedSecurityKey(key)
			if secretKeyPattern.MatchString(normalized) {
				return fmt.Errorf("%w: secret field rejected at %s.%s", ErrInvalid, path, key)
			}
			if err := validateExportSafety(child, path+"."+key, depth+1); err != nil {
				return err
			}
		}
	case []any:
		if len(typed) > MaxExportArrayItems {
			return fmt.Errorf("%w: export array too large at %s", ErrInvalid, path)
		}
		for index, child := range typed {
			if err := validateExportSafety(
				child, fmt.Sprintf("%s[%d]", path, index), depth+1,
			); err != nil {
				return err
			}
		}
	case string:
		if !utf8.ValidString(typed) || len(typed) > MaxExportStringBytes {
			return fmt.Errorf("%w: export string too large or invalid at %s", ErrInvalid, path)
		}
		if err := rejectSignedURL(typed); err != nil {
			return fmt.Errorf("%w at %s", err, path)
		}
	}
	return nil
}

func normalizedSecurityKey(value string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

func rejectSignedURL(value string) error {
	lower := strings.ToLower(strings.TrimSpace(value))
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%w: malformed URL", ErrInvalid)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: URL userinfo rejected", ErrInvalid)
	}
	for key := range parsed.Query() {
		if signedURLQueryKeys[strings.ToLower(key)] ||
			secretKeyPattern.MatchString(normalizedSecurityKey(key)) {
			return fmt.Errorf("%w: signed URL rejected", ErrInvalid)
		}
	}
	return nil
}

func validateArtifactURI(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("%w: malformed artifact URI", ErrInvalid)
	}
	if parsed.Scheme != "artifact" || parsed.Host == "" ||
		parsed.Path == "" || parsed.Path == "/" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%w: artifact URI must be an immutable artifact:// reference", ErrInvalid)
	}
	return nil
}

type postgresExportRepository struct {
	tx *sql.Tx
}

func (r *PostgresRepository) withExportSnapshot(
	ctx context.Context,
	export func(ExportRepository) error,
) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return fmt.Errorf("begin trajectory export snapshot: %w", err)
	}
	defer tx.Rollback()
	if err := export(&postgresExportRepository{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit trajectory export snapshot: %w", err)
	}
	return nil
}

func (r *postgresExportRepository) GetRun(
	ctx context.Context,
	runID string,
) (ResearchRun, error) {
	result, err := scanRun(r.tx.QueryRowContext(ctx,
		`SELECT `+runColumns+` FROM research_runs WHERE id = $1`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return ResearchRun{}, ErrNotFound
	}
	if err != nil {
		return ResearchRun{}, fmt.Errorf("get research run for export: %w", err)
	}
	return result, nil
}

func (r *postgresExportRepository) ListRuns(
	ctx context.Context,
	filter RunFilter,
) ([]ResearchRun, error) {
	limit, offset := pagination(filter.Limit, filter.Offset)
	rows, err := r.tx.QueryContext(ctx, `
		SELECT `+runColumns+`
		FROM research_runs
		WHERE ($1::text IS NULL OR experiment_id = $1)
		  AND ($2::bigint IS NULL OR project_id = $2)
		  AND ($3::text IS NULL OR status = $3)
		  AND ($4::text IS NULL OR agent_run_id = $4)
		ORDER BY created_at DESC, id
		LIMIT $5 OFFSET $6`,
		filter.ExperimentID, filter.ProjectID, filter.Status, filter.AgentRunID,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list research runs for export: %w", err)
	}
	defer rows.Close()
	result := make([]ResearchRun, 0)
	for rows.Next() {
		item, err := scanRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan research run for export: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate research runs for export: %w", err)
	}
	return result, nil
}

func (r *postgresExportRepository) ListTransitions(
	ctx context.Context,
	filter TransitionFilter,
) ([]Transition, error) {
	if strings.TrimSpace(filter.ResearchRunID) == "" {
		return nil, fmt.Errorf("%w: research_run_id", ErrInvalid)
	}
	limit, _ := pagination(filter.Limit, 0)
	rows, err := r.tx.QueryContext(ctx, `
		SELECT `+transitionColumns+`
		FROM research_transitions
		WHERE research_run_id = $1
		  AND ($2::bigint IS NULL OR ordinal > $2)
		ORDER BY ordinal
		LIMIT $3`,
		filter.ResearchRunID, filter.AfterOrdinal, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list research transitions for export: %w", err)
	}
	defer rows.Close()
	result := make([]Transition, 0)
	for rows.Next() {
		item, err := scanTransition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan research transition for export: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate research transitions for export: %w", err)
	}
	return result, nil
}
