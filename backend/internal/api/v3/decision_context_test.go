package v3

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestProjectDecisionContext(t *testing.T) {
	longContent := strings.Repeat("界", 2001)
	truncatedContent := strings.Repeat("界", 1997) + "..."
	longInput, err := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": longContent},
			{"role": "tool", "content": longContent},
		},
	})
	if err != nil {
		t.Fatalf("marshal long input: %v", err)
	}
	longOutput, err := json.Marshal(map[string]any{
		"message": map[string]string{"content": longContent},
	})
	if err != nil {
		t.Fatalf("marshal long output: %v", err)
	}

	tests := []struct {
		name      string
		input     json.RawMessage
		output    json.RawMessage
		want      *publicDecisionContext
		forbidden []string
	}{
		{
			name: "projects observable fields and first tool call",
			input: json.RawMessage(`{"messages":[
				{"role":"user","content":"Check checkout","name":"unsupported"},
				{"role":"assistant","content":"calling tool"},
				{"role":"tool","content":"login succeeded","tool_call_id":"call_previous"}
			],"unsupported_request_field":"ignored"}`),
			output: json.RawMessage(`{"message":{
				"role":"assistant",
				"content":"I will validate checkout.",
				"reasoning_content":"must not be projected",
				"tool_calls":[
					{"id":"call_1","name":"browser_execute_case","arguments":{"case":"checkout"}},
					{"id":"call_2","name":"ignored_operation","arguments":{"case":"ignored"}}
				]
			},"report":"unsupported","usage":{"total_tokens":7}}`),
			want: &publicDecisionContext{
				Objective: &publicDecisionMessage{
					Role: "user", Content: "Check checkout",
				},
				PriorObservation: &publicDecisionMessage{
					Role: "tool", Content: "login succeeded",
				},
				SelectedOperation: &publicSelectedOperation{
					Name: "browser_execute_case", Arguments: json.RawMessage(`{"case":"checkout"}`),
				},
				VisibleExplanation: "I will validate checkout.",
			},
			forbidden: []string{
				"must not be projected", `"reasoning_content"`, `"id"`, `"name":"unsupported"`,
				`"tool_call_id"`, `"ignored_operation"`, `"report"`, `"usage"`,
			},
		},
		{
			name: "uses latest user and latest tool after that user",
			input: json.RawMessage(`{"messages":[
				{"role":"user","content":"old objective"},
				{"role":"tool","content":"old observation"},
				{"role":"user","content":"latest objective"},
				{"role":"tool","content":"first observation after latest objective"},
				{"role":"assistant","content":"continuing"},
				{"role":"tool","content":"latest observation after latest objective"}
			]}`),
			output: json.RawMessage(`{}`),
			want: &publicDecisionContext{
				Objective: &publicDecisionMessage{
					Role: "user", Content: "latest objective",
				},
				PriorObservation: &publicDecisionMessage{
					Role: "tool", Content: "latest observation after latest objective",
				},
			},
			forbidden: []string{"old objective", "old observation", "first observation after latest objective"},
		},
		{
			name:   "omits selected operation when there is no tool call",
			input:  json.RawMessage(`{"messages":[{"role":"user","content":"Summarize results"}]}`),
			output: json.RawMessage(`{"message":{"content":"The run passed."}}`),
			want: &publicDecisionContext{
				Objective: &publicDecisionMessage{
					Role: "user", Content: "Summarize results",
				},
				VisibleExplanation: "The run passed.",
			},
			forbidden: []string{`"selected_operation"`},
		},
		{
			name:  "omits visible explanation when assistant content is absent",
			input: json.RawMessage(`{"messages":[{"role":"user","content":"Check checkout"}]}`),
			output: json.RawMessage(`{"message":{"tool_calls":[
				{"name":"browser_execute_case","arguments":{"case":"checkout"}}
			]}}`),
			want: &publicDecisionContext{
				Objective: &publicDecisionMessage{
					Role: "user", Content: "Check checkout",
				},
				SelectedOperation: &publicSelectedOperation{
					Name: "browser_execute_case", Arguments: json.RawMessage(`{"case":"checkout"}`),
				},
			},
			forbidden: []string{`"visible_explanation"`},
		},
		{
			name:   "truncates content by rune including ellipsis in limit",
			input:  longInput,
			output: longOutput,
			want: &publicDecisionContext{
				Objective: &publicDecisionMessage{
					Role: "user", Content: truncatedContent,
				},
				PriorObservation: &publicDecisionMessage{
					Role: "tool", Content: truncatedContent,
				},
				VisibleExplanation: truncatedContent,
			},
		},
		{
			name:  "keeps output evidence when input is malformed",
			input: json.RawMessage(`{"messages":[`),
			output: json.RawMessage(`{"message":{
				"content":"I will inspect the page.",
				"tool_calls":[{"name":"browser_execute_case","arguments":{"case":"checkout"}}]
			}}`),
			want: &publicDecisionContext{
				SelectedOperation: &publicSelectedOperation{
					Name: "browser_execute_case", Arguments: json.RawMessage(`{"case":"checkout"}`),
				},
				VisibleExplanation: "I will inspect the page.",
			},
		},
		{
			name:   "keeps input evidence when output is malformed",
			input:  json.RawMessage(`{"messages":[{"role":"user","content":"Check checkout"}]}`),
			output: json.RawMessage(`{"message":`),
			want: &publicDecisionContext{
				Objective: &publicDecisionMessage{
					Role: "user", Content: "Check checkout",
				},
			},
		},
		{
			name:   "returns nil when neither side proves evidence",
			input:  json.RawMessage(`{"messages":[`),
			output: json.RawMessage(`{"message":`),
			want:   nil,
		},
		{
			name:   "does not project a tool observation without a user objective",
			input:  json.RawMessage(`{"messages":[{"role":"tool","content":"orphan observation"}]}`),
			output: json.RawMessage(`{}`),
			want:   nil,
		},
		{
			name:  "projects only already redacted operation arguments",
			input: json.RawMessage(`{"messages":[{"role":"user","content":"Check checkout"}]}`),
			output: redactTraceJSON(json.RawMessage(`{"message":{"tool_calls":[{
				"name":"browser_execute_case",
				"arguments":{"case":"checkout","token":"remove-me","nested":{"password":"remove-me","visible":true}}
			}]}}`)),
			want: &publicDecisionContext{
				Objective: &publicDecisionMessage{
					Role: "user", Content: "Check checkout",
				},
				SelectedOperation: &publicSelectedOperation{
					Name: "browser_execute_case",
					Arguments: json.RawMessage(
						`{"case":"checkout","nested":{"visible":true}}`,
					),
				},
			},
			forbidden: []string{`"token"`, `"password"`, "remove-me"},
		},
		{
			name: "does not infer evidence from unsupported fields",
			input: json.RawMessage(`{"messages":[
				{"role":"system","content":"system prompt"},
				{"role":"assistant","content":"assistant text"}
			],"objective":"fabricated objective"}`),
			output: json.RawMessage(`{
				"message":{"role":"assistant"},
				"report":"fabricated explanation",
				"operation":{"name":"fabricated operation"}
			}`),
			want: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := projectDecisionContext(test.input, test.output)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("projectDecisionContext() = %#v, want %#v", got, test.want)
			}

			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal projected context: %v", err)
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("projected context contains %q: %s", forbidden, encoded)
				}
			}
			if test.name == "truncates content by rune including ellipsis in limit" {
				for field, content := range map[string]string{
					"objective":           got.Objective.Content,
					"prior observation":   got.PriorObservation.Content,
					"visible explanation": got.VisibleExplanation,
				} {
					if !utf8.ValidString(content) || utf8.RuneCountInString(content) != 2000 {
						t.Fatalf("%s is not a valid 2,000-rune string", field)
					}
				}
			}
		})
	}
}
