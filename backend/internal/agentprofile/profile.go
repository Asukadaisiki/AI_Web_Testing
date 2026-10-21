// Package agentprofile loads immutable, server-owned agent definitions.
package agentprofile

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/event"
	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/operation"
)

const browserOperationPath = "/operations/browser_execute_case"

//go:embed browser_eval.json
var browserEvalJSON []byte

// ModelDefaults identifies a model without embedding runtime credentials.
type ModelDefaults struct {
	Provider        string `json:"provider"`
	Name            string `json:"name"`
	ReasoningEffort string `json:"reasoning_effort"`
	Endpoint        string `json:"-"`
	APIKey          string `json:"-"`
}

// Safety describes the supported side-effect boundary for a profile.
type Safety struct {
	FreshContext                     bool   `json:"fresh_context"`
	DisposableFixtureData            bool   `json:"disposable_fixture_data"`
	IrreversibleProductionOperations string `json:"irreversible_production_operations"`
}

type callback struct {
	Kind        string `json:"kind"`
	URLTemplate string `json:"url_template"`
}

type retryPolicy struct {
	MaxAttempts int `json:"max_attempts"`
	BackoffMS   int `json:"backoff_ms"`
}

type profileOperation struct {
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	InputSchema    json.RawMessage `json:"input_schema"`
	Callback       callback        `json:"callback"`
	TimeoutMS      int             `json:"timeout_ms"`
	RetryPolicy    retryPolicy     `json:"retry_policy"`
	MaxResultBytes int64           `json:"max_result_bytes"`

	callbackURL string
}

// Profile is an immutable server-owned model and operation policy.
type Profile struct {
	ID          string             `json:"id"`
	ProductName string             `json:"product_name"`
	Model       ModelDefaults      `json:"model"`
	Operations  []profileOperation `json:"operations"`
	Safety      Safety             `json:"safety"`
}

// LoadBrowserEval loads the embedded browser profile and derives its callback
// from the server-configured worker base URL.
func LoadBrowserEval(workerBaseURL string) (Profile, error) {
	var profile Profile
	decoder := json.NewDecoder(bytes.NewReader(browserEvalJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("decode browser profile: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Profile{}, errors.New("decode browser profile: multiple JSON values")
		}
		return Profile{}, fmt.Errorf("decode browser profile: %w", err)
	}
	if err := profile.validate(); err != nil {
		return Profile{}, err
	}

	callbackURL, err := browserCallbackURL(workerBaseURL)
	if err != nil {
		return Profile{}, err
	}
	for index := range profile.Operations {
		operation := &profile.Operations[index]
		if operation.Callback.Kind != "http" {
			return Profile{}, fmt.Errorf("operation %q callback kind must be http", operation.Name)
		}
		if operation.Callback.URLTemplate != "{{worker_base_url}}"+browserOperationPath {
			return Profile{}, fmt.Errorf(
				"operation %q has unsupported callback URL template %q",
				operation.Name,
				operation.Callback.URLTemplate,
			)
		}
		operation.callbackURL = callbackURL
	}
	return profile, nil
}

// OperationDefinitions returns transport definitions suitable for the HTTP dispatcher.
func (p Profile) OperationDefinitions() []operation.Definition {
	definitions := make([]operation.Definition, 0, len(p.Operations))
	for _, configured := range p.Operations {
		definitions = append(definitions, operation.Definition{
			Name:           configured.Name,
			Description:    configured.Description,
			InputSchema:    append(json.RawMessage(nil), configured.InputSchema...),
			CallbackURL:    configured.callbackURL,
			Timeout:        time.Duration(configured.TimeoutMS) * time.Millisecond,
			MaxAttempts:    configured.RetryPolicy.MaxAttempts,
			RetryBackoff:   time.Duration(configured.RetryPolicy.BackoffMS) * time.Millisecond,
			MaxResultBytes: configured.MaxResultBytes,
		})
	}
	return definitions
}

// RunOperations returns the immutable event contract exposed to a model run.
func (p Profile) RunOperations() []event.Operation {
	operations := make([]event.Operation, 0, len(p.Operations))
	for _, configured := range p.Operations {
		operations = append(operations, event.Operation{
			Name:        configured.Name,
			Description: configured.Description,
			InputSchema: append(json.RawMessage(nil), configured.InputSchema...),
			Callback: event.Callback{
				Kind: configured.Callback.Kind,
				URL:  configured.callbackURL,
			},
		})
	}
	return operations
}

// ApprovedCallbackURLs returns the callbacks derived from server configuration.
func (p Profile) ApprovedCallbackURLs() []string {
	callbacks := make([]string, 0, len(p.Operations))
	for _, configured := range p.Operations {
		callbacks = append(callbacks, configured.callbackURL)
	}
	return callbacks
}

func (p Profile) validate() error {
	if p.ID == "" || p.ProductName != "AgentTrace Eval Hub" {
		return errors.New("browser profile identity is invalid")
	}
	if p.Model.Name == "" || p.Model.ReasoningEffort == "" {
		return errors.New("browser profile model defaults are required")
	}
	if len(p.Operations) != 1 || p.Operations[0].Name != "browser_execute_case" {
		return errors.New("browser profile must define only browser_execute_case")
	}
	configured := p.Operations[0]
	if len(configured.InputSchema) == 0 ||
		configured.TimeoutMS < 1 ||
		configured.RetryPolicy.MaxAttempts < 1 ||
		configured.RetryPolicy.BackoffMS < 0 ||
		configured.MaxResultBytes < 1 {
		return errors.New("browser operation limits and schema are required")
	}
	if !p.Safety.FreshContext ||
		!p.Safety.DisposableFixtureData ||
		p.Safety.IrreversibleProductionOperations != "unsupported" {
		return errors.New("browser profile safety boundary is invalid")
	}
	return nil
}

func browserCallbackURL(workerBaseURL string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(workerBaseURL))
	if err != nil {
		return "", fmt.Errorf("parse worker base URL: %w", err)
	}
	if (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return "", fmt.Errorf("worker base URL %q must be an absolute HTTP URL", workerBaseURL)
	}
	if base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return "", errors.New("worker base URL must not contain user information, query, or fragment")
	}
	callback, err := url.JoinPath(base.String(), strings.TrimPrefix(browserOperationPath, "/"))
	if err != nil {
		return "", fmt.Errorf("derive browser callback URL: %w", err)
	}
	return callback, nil
}
