package research

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	OracleSchemaVersion     = "research.oracle.v1"
	MaxOracleFactValueBytes = 4 * 1024
)

var oracleCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)

type OracleDecisionFact struct {
	Name     string          `json:"name"`
	Passed   bool            `json:"passed"`
	Actual   json.RawMessage `json:"actual,omitempty"`
	Expected json.RawMessage `json:"expected,omitempty"`
	Sources  []SourceRef     `json:"sources"`
}

type OracleDecision struct {
	SchemaVersion string               `json:"schema_version"`
	ID            string               `json:"id"`
	Evaluator     string               `json:"evaluator"`
	Passed        bool                 `json:"passed"`
	ReasonCode    string               `json:"reason_code"`
	DecisionFacts []OracleDecisionFact `json:"decision_facts"`
	Sources       []SourceRef          `json:"sources"`
	ContentSHA256 string               `json:"content_sha256"`
}

func NewOracleDecision(decision OracleDecision) (OracleDecision, error) {
	decision.SchemaVersion = OracleSchemaVersion
	decision.ContentSHA256 = ""
	if err := decision.normalize(false); err != nil {
		return OracleDecision{}, err
	}
	hash, err := oracleDecisionHash(decision)
	if err != nil {
		return OracleDecision{}, err
	}
	decision.ContentSHA256 = hash
	return decision, nil
}

func (d *OracleDecision) NormalizeAndValidate() error {
	if err := d.normalize(true); err != nil {
		return err
	}
	hash, err := oracleDecisionHash(*d)
	if err != nil {
		return err
	}
	if hash != d.ContentSHA256 {
		return fmt.Errorf("%w: oracle content_sha256 mismatch", ErrInvalid)
	}
	return nil
}

func (d *OracleDecision) UnmarshalJSON(raw []byte) error {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return fmt.Errorf("%w: oracle JSON", ErrInvalid)
	}
	if err := rejectForbiddenKeys(value); err != nil {
		return err
	}
	type plain OracleDecision
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*d = OracleDecision(decoded)
	return nil
}

func (d *OracleDecision) normalize(requireHash bool) error {
	d.SchemaVersion = strings.TrimSpace(d.SchemaVersion)
	d.ID = strings.TrimSpace(d.ID)
	d.Evaluator = strings.TrimSpace(d.Evaluator)
	d.ReasonCode = strings.TrimSpace(d.ReasonCode)
	d.ContentSHA256 = strings.ToLower(strings.TrimSpace(d.ContentSHA256))
	if d.SchemaVersion != OracleSchemaVersion ||
		d.ID == "" || len(d.ID) > 200 ||
		d.Evaluator == "" || len(d.Evaluator) > 200 ||
		!oracleCodePattern.MatchString(d.ReasonCode) {
		return fmt.Errorf("%w: oracle envelope", ErrInvalid)
	}
	if requireHash && !sha256Pattern.MatchString(d.ContentSHA256) {
		return fmt.Errorf("%w: oracle content_sha256", ErrInvalid)
	}
	if len(d.Sources) == 0 {
		return fmt.Errorf("%w: oracle requires source references", ErrInvalid)
	}
	topLevelSources := make(map[string]struct{}, len(d.Sources))
	for index := range d.Sources {
		if err := d.Sources[index].NormalizeAndValidate(); err != nil {
			return err
		}
		key, err := oracleSourceKey(d.Sources[index])
		if err != nil {
			return err
		}
		if _, exists := topLevelSources[key]; exists {
			return fmt.Errorf("%w: duplicate oracle source", ErrInvalid)
		}
		topLevelSources[key] = struct{}{}
	}
	slices.SortFunc(d.Sources, compareSourceRefs)
	if len(d.DecisionFacts) == 0 {
		return fmt.Errorf("%w: oracle requires decision facts", ErrInvalid)
	}
	allFactsPassed := true
	for index := range d.DecisionFacts {
		fact := &d.DecisionFacts[index]
		fact.Name = strings.TrimSpace(fact.Name)
		if !oracleCodePattern.MatchString(fact.Name) || len(fact.Sources) == 0 {
			return fmt.Errorf("%w: oracle decision fact", ErrInvalid)
		}
		var err error
		fact.Actual, err = normalizeOracleFactValue(fact.Actual)
		if err != nil {
			return fmt.Errorf("%w: oracle fact %s actual", err, fact.Name)
		}
		fact.Expected, err = normalizeOracleFactValue(fact.Expected)
		if err != nil {
			return fmt.Errorf("%w: oracle fact %s expected", err, fact.Name)
		}
		hasIndependentSource := false
		factSources := make(map[string]struct{}, len(fact.Sources))
		for sourceIndex := range fact.Sources {
			if err := fact.Sources[sourceIndex].NormalizeAndValidate(); err != nil {
				return err
			}
			source := fact.Sources[sourceIndex]
			key, err := oracleSourceKey(source)
			if err != nil {
				return err
			}
			if _, exists := topLevelSources[key]; !exists {
				return fmt.Errorf("%w: oracle fact source is not declared", ErrInvalid)
			}
			if _, exists := factSources[key]; exists {
				return fmt.Errorf("%w: duplicate oracle fact source", ErrInvalid)
			}
			factSources[key] = struct{}{}
			hasIndependentSource = hasIndependentSource || source.Kind == SourceOracle
		}
		if !hasIndependentSource {
			return fmt.Errorf("%w: oracle fact requires independent source", ErrInvalid)
		}
		slices.SortFunc(fact.Sources, compareSourceRefs)
		allFactsPassed = allFactsPassed && fact.Passed
	}
	if d.Passed != allFactsPassed {
		return fmt.Errorf("%w: oracle passed must equal all decision facts", ErrInvalid)
	}
	slices.SortFunc(d.DecisionFacts, func(left, right OracleDecisionFact) int {
		return strings.Compare(left.Name, right.Name)
	})
	for index := 1; index < len(d.DecisionFacts); index++ {
		if d.DecisionFacts[index-1].Name == d.DecisionFacts[index].Name {
			return fmt.Errorf("%w: duplicate oracle decision fact", ErrInvalid)
		}
	}
	return nil
}

func normalizeOracleFactValue(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > MaxOracleFactValueBytes {
		return nil, fmt.Errorf("%w: value exceeds limit", ErrInvalid)
	}
	canonical, err := CanonicalJSON(raw)
	if err != nil || len(canonical) > MaxOracleFactValueBytes {
		return nil, fmt.Errorf("%w: invalid or oversized value", ErrInvalid)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: invalid value", ErrInvalid)
	}
	if err := rejectForbiddenKeys(value); err != nil {
		return nil, err
	}
	return json.RawMessage(canonical), nil
}

func oracleSourceKey(source SourceRef) (string, error) {
	return CanonicalSHA256(source)
}

func oracleDecisionHash(decision OracleDecision) (string, error) {
	decision.ContentSHA256 = ""
	return CanonicalSHA256(decision)
}
