package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

const (
	maxFailureSignatures = 8
	CodeStrategyRepeated = "strategy_repeated"
)

const strategyRepeatedDetail = "this action and target already failed on the unchanged page; change target, scope, action, or page state"

// FailureSignature describes an action strategy that failed on a semantic page state.
type FailureSignature struct {
	PageFingerprint string          `json:"page_fingerprint"`
	Action          contract.Action `json:"action"`
	TargetKey       string          `json:"target_key"`
	ErrorCode       string          `json:"error_code"`
}

type pageFingerprintPayload struct {
	URL                string                    `json:"url"`
	Title              string                    `json:"title"`
	ActionCandidateIDs []string                  `json:"action_candidate_ids"`
	ControlValues      []fingerprintControlValue `json:"control_values"`
	BlockerKinds       []string                  `json:"blocker_kinds"`
}

type fingerprintControlValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type fingerprintLocator struct {
	Kind  string `json:"kind"`
	Role  string `json:"role,omitempty"`
	Name  string `json:"name,omitempty"`
	Exact bool   `json:"exact,omitempty"`
	Text  string `json:"text,omitempty"`
	CSS   string `json:"css,omitempty"`
}

// PageFingerprint hashes the stable, user-visible parts of an observation.
func PageFingerprint(observation contract.Observation) string {
	payload := pageFingerprintPayload{
		URL:   canonicalPageURL(observation.URL),
		Title: canonicalText(observation.Title),
	}
	for _, candidate := range observation.ActionCandidates {
		if id := strings.TrimSpace(candidate.CandidateID); id != "" {
			payload.ActionCandidateIDs = append(payload.ActionCandidateIDs, id)
		}
	}
	payload.ActionCandidateIDs = sortedUnique(payload.ActionCandidateIDs)

	for _, element := range observation.Elements {
		if !element.Visible || element.Value == nil || len(element.Locators) == 0 {
			continue
		}
		payload.ControlValues = append(payload.ControlValues, fingerprintControlValue{
			Key:   canonicalLocator(element.Locators[0]),
			Value: *element.Value,
		})
	}
	sort.Slice(payload.ControlValues, func(i, j int) bool {
		if payload.ControlValues[i].Key != payload.ControlValues[j].Key {
			return payload.ControlValues[i].Key < payload.ControlValues[j].Key
		}
		return payload.ControlValues[i].Value < payload.ControlValues[j].Value
	})

	for _, blocker := range observation.Blockers {
		if kind := canonicalText(blocker.Kind); kind != "" {
			payload.BlockerKinds = append(payload.BlockerKinds, kind)
		}
	}
	payload.BlockerKinds = sortedUnique(payload.BlockerKinds)

	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func (p *Planner) failedStrategy(pageFingerprint string, action contract.Action, targetKey string) bool {
	for _, signature := range p.failures {
		if signature.PageFingerprint == pageFingerprint &&
			signature.Action == action &&
			signature.TargetKey == targetKey {
			return true
		}
	}
	return false
}

func repeatedStrategyFailure() Result {
	return failure(CodeStrategyRepeated, strategyRepeatedDetail)
}

func (p *Planner) rememberFailure(signature FailureSignature) {
	unique := make([]FailureSignature, 0, len(p.failures)+1)
	for _, existing := range p.failures {
		if existing != signature {
			unique = append(unique, existing)
		}
	}
	unique = append(unique, signature)
	if len(unique) > maxFailureSignatures {
		unique = unique[len(unique)-maxFailureSignatures:]
	}
	p.failures = unique
}

func failureTargetKey(candidateID string, spec *contract.TargetSpec, hint string) string {
	if key := strings.TrimSpace(candidateID); key != "" {
		return key
	}
	if spec == nil {
		return normalize(hint)
	}
	normalized := *spec
	normalized.Object = normalizeTargetObject(*spec)
	normalized.Object.Role = normalize(normalized.Object.Role)
	normalized.Object.Text = normalize(normalized.Object.Text)
	normalized.Object.Name = normalize(normalized.Object.Name)
	normalized.Object.Aliases = normalizedStrings(normalized.Object.Aliases)
	normalized.Relation = normalize(normalized.Relation)
	normalized.Role = ""
	normalized.Text = ""
	normalized.Name = ""
	normalized.Aliases = nil
	if normalized.Scope != nil {
		scope := *normalized.Scope
		scope.Kind = normalize(scope.Kind)
		scope.ContainsText = normalize(scope.ContainsText)
		scope.Ref = strings.TrimSpace(scope.Ref)
		normalized.Scope = &scope
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func canonicalLocator(locator contract.Locator) string {
	encoded, err := json.Marshal(fingerprintLocator{
		Kind:  locator.Kind,
		Role:  locator.Role,
		Name:  locator.Name,
		Exact: locator.Exact,
		Text:  locator.Text,
		CSS:   locator.CSS,
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func canonicalPageURL(raw string) string {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	} else {
		parsed.Host = host
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	parsed.RawQuery = parsed.Query().Encode()
	parsed.ForceQuery = false
	return parsed.String()
}

func canonicalText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizedStrings(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if value = normalize(value); value != "" {
			normalized = append(normalized, value)
		}
	}
	return sortedUnique(normalized)
}

func sortedUnique(values []string) []string {
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}
