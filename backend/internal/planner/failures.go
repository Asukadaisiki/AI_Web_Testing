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
		if !element.Visible || element.Value == nil {
			continue
		}
		payload.ControlValues = append(payload.ControlValues, fingerprintControlValue{
			Key: strings.Join([]string{
				strings.TrimSpace(element.Ref),
				canonicalText(element.Tag),
				canonicalText(element.Role),
				canonicalText(element.Name),
			}, "\x00"),
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

func failureTargetKey(candidateID string, spec *contract.TargetSpec) string {
	key := strings.TrimSpace(candidateID)
	if spec == nil || spec.Scope == nil {
		return key
	}
	scope := struct {
		Kind         string `json:"kind,omitempty"`
		ContainsText string `json:"contains_text,omitempty"`
		Ref          string `json:"ref,omitempty"`
		Relation     string `json:"relation,omitempty"`
	}{
		Kind:         normalize(spec.Scope.Kind),
		ContainsText: normalize(spec.Scope.ContainsText),
		Ref:          strings.TrimSpace(spec.Scope.Ref),
		Relation:     normalize(spec.Relation),
	}
	encoded, err := json.Marshal(scope)
	if err != nil {
		panic(err)
	}
	return key + "|scope:" + string(encoded)
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
