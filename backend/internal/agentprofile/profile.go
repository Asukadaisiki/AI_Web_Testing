// Package agentprofile loads immutable, server-owned agent definitions.
package agentprofile

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

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

// Profile is an immutable server-owned model and safety policy. The Pi
// browser harness owns the model turn loop and browser tool dispatch, so the
// profile no longer advertises a model-facing operation.
type Profile struct {
	ID          string        `json:"id"`
	ProductName string        `json:"product_name"`
	Model       ModelDefaults `json:"model"`
	Safety      Safety        `json:"safety"`
}

// LoadBrowserEval loads the embedded browser profile.
func LoadBrowserEval() (Profile, error) {
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
	return profile, nil
}

func (p Profile) validate() error {
	if p.ID == "" || p.ProductName != "AgentTrace Eval Hub" {
		return errors.New("browser profile identity is invalid")
	}
	if p.Model.Name == "" || p.Model.ReasoningEffort == "" {
		return errors.New("browser profile model defaults are required")
	}
	if !p.Safety.FreshContext ||
		!p.Safety.DisposableFixtureData ||
		p.Safety.IrreversibleProductionOperations != "unsupported" {
		return errors.New("browser profile safety boundary is invalid")
	}
	return nil
}
