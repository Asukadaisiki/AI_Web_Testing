package research

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend-go/internal/dsl"
)

func TestValidateAgentEventSchemaAcceptsLegacyAndCompleteProviderEvidence(t *testing.T) {
	legacy := AgentEventSnapshot{
		Seq:  1,
		Type: "research.llm_call",
		Payload: json.RawMessage(`{
			"schema_version":"research.llm_call.v1",
			"logical_call_id":"legacy-call",
			"attempt":1,
			"tool_call_status":"unavailable"
		}`),
	}
	if err := validateAgentEventSchema(legacy); err != nil {
		t.Fatalf("legacy event error = %v", err)
	}

	complete := AgentEventSnapshot{
		Seq:  2,
		Type: "research.llm_call",
		Payload: json.RawMessage(`{
			"schema_version":"research.llm_call.v1",
			"logical_call_id":"new-call",
			"provider":"deepseek",
			"requested_model":"deepseek-chat",
			"attempt":1,
			"tool_call_status":"unavailable",
			"client_request_id":"e2e_0123456789abcdef0123456789abcdef",
			"endpoint_scheme":"https",
			"endpoint_host":"api.deepseek.com",
			"credential_fingerprint":"sha256:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"provider_response_id":"response-1",
			"provider_header_request_id":"header-1",
			"provider_header_request_id_header":"x-request-id",
			"provider_request_id":"response-1",
			"local_response_cache":"not_configured"
		}`),
	}
	if err := validateAgentEventSchema(complete); err != nil {
		t.Fatalf("complete provider evidence error = %v", err)
	}
}

func TestValidateAgentEventSchemaRejectsInvalidProviderEvidence(t *testing.T) {
	valid := `{
		"schema_version":"research.llm_call.v1",
		"logical_call_id":"new-call",
		"provider":"deepseek",
		"requested_model":"deepseek-chat",
		"attempt":1,
		"tool_call_status":"unavailable",
		"client_request_id":"e2e_0123456789abcdef0123456789abcdef",
		"endpoint_scheme":"https",
		"endpoint_host":"api.deepseek.com",
		"credential_fingerprint":"sha256:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"provider_response_id":"response-1",
		"provider_header_request_id":"header-1",
		"provider_header_request_id_header":"x-request-id",
		"provider_request_id":"response-1",
		"local_response_cache":"not_configured"
	}`
	tests := []struct {
		name    string
		payload string
	}{
		{
			name: "incomplete",
			payload: strings.Replace(
				valid,
				`"endpoint_host":"api.deepseek.com",`,
				"",
				1,
			),
		},
		{
			name:    "insecure endpoint",
			payload: strings.Replace(valid, `"https"`, `"http"`, 1),
		},
		{
			name: "query leaked into host",
			payload: strings.Replace(
				valid,
				`"api.deepseek.com"`,
				`"api.deepseek.com?api_key=secret"`,
				1,
			),
		},
		{
			name: "credential is not fingerprinted",
			payload: strings.Replace(
				valid,
				`"sha256:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`,
				`"sk-secret-prefix"`,
				1,
			),
		},
		{
			name: "legacy ID confuses body and header",
			payload: strings.Replace(
				valid,
				`"provider_request_id":"response-1"`,
				`"provider_request_id":"header-1"`,
				1,
			),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			event := AgentEventSnapshot{
				Seq:     1,
				Type:    "research.llm_call",
				Payload: json.RawMessage(testCase.payload),
			}
			if err := validateAgentEventSchema(event); !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("validation error = %v, want ErrSourceChanged", err)
			}
		})
	}
}

func TestValidateGenerationDSLAcceptsCanonicalV1AndV2(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{
			name: "legacy v1",
			raw: json.RawMessage(
				`{"name":"legacy","steps":[{"action":"goto","value":"/"}]}`,
			),
		},
		{
			name: "research v2",
			raw: json.RawMessage(`{
				"profile":"research-v1","name":"research","steps":[{
					"action":"goto","intent":"Open home","target":"Home page","value":"/",
					"preconditions":[],
					"postconditions":[{"type":"url_contains","value":"/"}],
					"idempotency":"idempotent","side_effect":"browser_state"
				}]
			}`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validated, err := dsl.ValidateExecutableCase(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := validateGenerationDSL(
				validated.CanonicalJSON,
				dsl.SHA256(validated.CanonicalJSON),
				validated.CanonicalVersion,
			)
			if err != nil {
				t.Fatalf("validateGenerationDSL() error = %v", err)
			}
			if string(canonical) != string(validated.CanonicalJSON) {
				t.Fatalf("canonical DSL changed: %s", canonical)
			}
		})
	}
}

func TestValidateGenerationDSLRejectsVersionMismatch(t *testing.T) {
	validated, err := dsl.ValidateExecutableCase(json.RawMessage(
		`{"name":"legacy","steps":[{"action":"goto","value":"/"}]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateGenerationDSL(
		validated.CanonicalJSON,
		dsl.SHA256(validated.CanonicalJSON),
		dsl.CanonicalVersionV2,
	); err == nil {
		t.Fatal("validateGenerationDSL() error = nil")
	}
}
