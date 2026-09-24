package planner

import (
	"reflect"
	"testing"

	"github.com/Asukadaisiki/AI_Web_Testing/backend/internal/contract"
)

func TestObservationHasTextChecksEachVisibleNodeWithoutJoiningNodes(t *testing.T) {
	tests := []struct {
		name        string
		observation contract.Observation
		text        string
		want        bool
	}{
		{
			name: "element name",
			observation: contract.Observation{Elements: []contract.Element{{
				Name: "  Logged   in as AI Web Test ", Visible: true,
			}}},
			text: "logged IN as ai web test",
			want: true,
		},
		{
			name: "element text",
			observation: contract.Observation{Elements: []contract.Element{{
				Text: "  Logged   in as AI Web Test ", Visible: true,
			}}},
			text: "logged IN as ai web test",
			want: true,
		},
		{
			name: "element full text",
			observation: contract.Observation{Elements: []contract.Element{{
				FullText: "  Logged   in as AI Web Test ", Visible: true,
			}}},
			text: "logged IN as ai web test",
			want: true,
		},
		{
			name: "structure full text",
			observation: contract.Observation{Structures: []contract.StructureNode{{
				FullText: "  Logged   in as AI Web Test ", Visible: true,
			}}},
			text: "logged IN as ai web test",
			want: true,
		},
		{
			name: "hidden element",
			observation: contract.Observation{Elements: []contract.Element{{
				Name: "Logged in as AI Web Test", Text: "Logged in as AI Web Test",
				FullText: "Logged in as AI Web Test", Visible: false,
			}}},
			text: "Logged in as AI Web Test",
			want: false,
		},
		{
			name: "hidden structure",
			observation: contract.Observation{Structures: []contract.StructureNode{{
				FullText: "Logged in as AI Web Test", Visible: false,
			}}},
			text: "Logged in as AI Web Test",
			want: false,
		},
		{
			name: "unrelated nodes are not joined",
			observation: contract.Observation{
				Elements: []contract.Element{{Text: "Logged in as", Visible: true}},
				Structures: []contract.StructureNode{{
					FullText: "AI Web Test", Visible: true,
				}},
			},
			text: "Logged in as AI Web Test",
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := observationHasText(test.observation, test.text); got != test.want {
				t.Fatalf("observationHasText() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestUnsatisfiedPageAssertionsDoNotCommit(t *testing.T) {
	gotoStep, err := contract.DeriveGotoStep(
		0, "open products", "https://shop.test/products",
	)
	if err != nil {
		t.Fatalf("derive goto: %v", err)
	}
	original := []contract.Step{gotoStep}

	tests := []struct {
		name string
		call func(*Planner) (Result, error)
	}{
		{
			name: "text",
			call: func(planner *Planner) (Result, error) {
				return planner.assertText("Missing text", "verify missing text")
			},
		},
		{
			name: "url",
			call: func(planner *Planner) (Result, error) {
				return planner.assertURL("/missing", "verify missing url")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planner := &Planner{
				observation: contract.Observation{
					URL: "https://shop.test/products",
					Elements: []contract.Element{{
						Text: "Products", Visible: true,
					}},
				},
				hasPage: true,
				steps:   append([]contract.Step(nil), original...),
			}

			result, err := test.call(planner)
			if err != nil {
				t.Fatalf("assertion: %v", err)
			}
			if result.OK || result.Error != string(contract.SignalConditionUnmet) {
				t.Fatalf("assertion result = %+v, want condition_unmet failure", result)
			}
			if got := planner.Steps(); !reflect.DeepEqual(got, original) {
				t.Fatalf("committed steps = %#v, want unchanged %#v", got, original)
			}
		})
	}
}

func TestSatisfiedPageAssertionsCommit(t *testing.T) {
	planner := &Planner{
		observation: contract.Observation{
			URL: "https://shop.test/products?search=Blue%20Top",
			Structures: []contract.StructureNode{{
				FullText: "Searched Products Blue Top", Visible: true,
			}},
		},
		hasPage: true,
	}

	textResult, err := planner.assertText(" blue   top ", "verify product")
	if err != nil || !textResult.OK {
		t.Fatalf("assert text: result=%+v err=%v", textResult, err)
	}
	urlResult, err := planner.assertURL("search=Blue%20Top", "verify search url")
	if err != nil || !urlResult.OK {
		t.Fatalf("assert url: result=%+v err=%v", urlResult, err)
	}
	if got := planner.Steps(); len(got) != 2 ||
		got[0].Action != contract.ActionAssertText ||
		got[1].Action != contract.ActionAssertURL {
		t.Fatalf("committed steps = %#v, want text and url assertions", got)
	}
}
