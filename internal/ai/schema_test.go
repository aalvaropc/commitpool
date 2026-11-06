package ai_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
)

var dateIDs = []string{"sat", "sun"}

const privateNote = "Ana's private note about a hospital visit"

func validReading() string {
	return `{
		"per_date": [
			{"candidate_date_id": "sat", "verdict": "conditional", "condition": {"type": "end_before", "time": "18:00"}, "evidence": "` + privateNote + `"},
			{"candidate_date_id": "sun", "verdict": "unavailable", "condition": {"type": "none", "time": ""}, "evidence": "not Sunday"}
		],
		"hard_constraints": ["finish before six"],
		"preferences": [],
		"ambiguities": [],
		"needs_clarification": false
	}`
}

func TestParseInterpretation_AcceptsAStrictReading(t *testing.T) {
	got, err := ai.ParseInterpretation([]byte(validReading()), dateIDs)
	if err != nil {
		t.Fatalf("ParseInterpretation: %v", err)
	}
	want := domain.Interpretation{
		PerDate: []domain.DateVerdict{
			{CandidateDateID: "sat", Verdict: domain.VerdictConditional, Condition: domain.Condition{Type: domain.ConditionEndBefore, Time: "18:00"}, Evidence: privateNote},
			{CandidateDateID: "sun", Verdict: domain.VerdictUnavailable, Condition: domain.Condition{Type: domain.ConditionNone}, Evidence: "not Sunday"},
		},
		HardConstraints: []string{"finish before six"},
		Preferences:     []string{},
		Ambiguities:     []domain.Ambiguity{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseInterpretation =\n%+v\nwant\n%+v", got, want)
	}
}

func TestInterpret_SchemaRejectsExtraFields(t *testing.T) {
	edit := func(f func(map[string]any)) string {
		var doc map[string]any
		if err := json.Unmarshal([]byte(validReading()), &doc); err != nil {
			t.Fatalf("valid reading: %v", err)
		}
		f(doc)
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(raw)
	}
	firstDate := func(doc map[string]any) map[string]any {
		return doc["per_date"].([]any)[0].(map[string]any)
	}
	cases := map[string]string{
		"a price at the top level":   edit(func(d map[string]any) { d["price"] = 0 }),
		"a minimum at the top level": edit(func(d map[string]any) { d["minimum"] = 1 }),
		"an extra field on a date":   edit(func(d map[string]any) { firstDate(d)["capacity"] = 99 }),
		"an extra field on a condition": edit(func(d map[string]any) {
			firstDate(d)["condition"].(map[string]any)["currency"] = "EUR"
		}),
		"an extra field on an ambiguity": edit(func(d map[string]any) {
			d["ambiguities"] = []any{map[string]any{"question": "Which hours?", "why": "vague", "price": 0}}
		}),
		"a date outside the workshop": edit(func(d map[string]any) { firstDate(d)["candidate_date_id"] = "mon" }),
		"an unknown verdict":          edit(func(d map[string]any) { firstDate(d)["verdict"] = "maybe" }),
		"an unknown condition type": edit(func(d map[string]any) {
			firstDate(d)["condition"].(map[string]any)["type"] = "around"
		}),
		"a missing field":   edit(func(d map[string]any) { delete(d, "needs_clarification") }),
		"a wrong type":      edit(func(d map[string]any) { d["needs_clarification"] = "no" }),
		"not an object":     `[` + validReading() + `]`,
		"not JSON":          `{"per_date": [{"evidence": "` + privateNote + `"`,
		"text around JSON":  "Here you go: " + validReading(),
		"two JSON values":   validReading() + validReading(),
		"an empty response": ``,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ai.ParseInterpretation([]byte(raw), dateIDs)
			if err == nil {
				t.Fatalf("ParseInterpretation accepted %s:\n%s", name, raw)
			}
			if strings.Contains(err.Error(), privateNote) || strings.Contains(err.Error(), "hospital") {
				t.Errorf("error repeats the completion: %v", err)
			}
		})
	}
	if _, err := ai.ParseInterpretation([]byte(validReading()), nil); err == nil {
		t.Error("ParseInterpretation without candidate dates accepted a reading")
	}
}

func TestInterpretationSchema_StrictEverywhere(t *testing.T) {
	schema := ai.InterpretationSchema(dateIDs)
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("schema JSON: %v", err)
	}
	objects := 0
	var walk func(path string, node any)
	walk = func(path string, node any) {
		switch n := node.(type) {
		case map[string]any:
			if n["type"] == "object" {
				objects++
				if n["additionalProperties"] != false {
					t.Errorf("%s: additionalProperties = %v, want false", path, n["additionalProperties"])
				}
				props, _ := n["properties"].(map[string]any)
				required, _ := n["required"].([]any)
				if len(required) != len(props) {
					t.Errorf("%s: required %v does not list every property of %v", path, required, props)
				}
				for _, r := range required {
					if _, ok := props[r.(string)]; !ok {
						t.Errorf("%s: required %q is not a property", path, r)
					}
				}
				for name := range props {
					for _, banned := range []string{"price", "currency", "minimum", "capacity", "amount", "status"} {
						if strings.Contains(name, banned) {
							t.Errorf("%s: property %q lets the model speak about %s", path, name, banned)
						}
					}
				}
			}
			for k, v := range n {
				walk(path+"/"+k, v)
			}
		case []any:
			for i, v := range n {
				walk(path+"/"+string(rune('0'+i)), v)
			}
		}
	}
	walk("#", doc)
	if objects != 4 {
		t.Errorf("schema has %d objects, want 4 (reading, date verdict, condition, ambiguity)", objects)
	}
	m := doc.(map[string]any)
	item := m["properties"].(map[string]any)["per_date"].(map[string]any)["items"].(map[string]any)
	enum := item["properties"].(map[string]any)["candidate_date_id"].(map[string]any)["enum"].([]any)
	if !slices.Equal(enum, []any{"sat", "sun"}) {
		t.Errorf("candidate_date_id enum = %v, want the workshop's date IDs", enum)
	}
}

func TestInterpret_AmbiguousMessageSetsNeedsClarification(t *testing.T) {
	in := interpret(t, "Saturday afternoon")
	if !in.NeedsClarification {
		t.Errorf("%q needs_clarification = false, want true", "Saturday afternoon")
	}
	if len(in.Ambiguities) != 1 || in.Ambiguities[0].Question == "" || in.Ambiguities[0].Why == "" {
		t.Errorf("ambiguities = %+v, want one question with its reason", in.Ambiguities)
	}
	if v := verdictFor(t, in, "sat"); v.Verdict != domain.VerdictUnknown {
		t.Errorf("Saturday = %+v, want unknown until clarified", v)
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := ai.ParseInterpretation(raw, dateIDs); err != nil {
		t.Errorf("the fake's reading does not match the schema: %v", err)
	}
}

func TestParseInterpretation_NormalisesTheClarificationFlags(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(validReading()), &doc); err != nil {
		t.Fatal(err)
	}
	doc["needs_clarification"] = true
	raw, _ := json.Marshal(doc)
	got, err := ai.ParseInterpretation(raw, dateIDs)
	if err != nil {
		t.Fatalf("ParseInterpretation: %v", err)
	}
	if !got.NeedsClarification || len(got.Ambiguities) != 1 || got.Ambiguities[0].Question == "" ||
		got.Ambiguities[0].Why != "The message did not make the availability clear." {
		t.Errorf("needs_clarification without ambiguities = %+v, want one generic ambiguity", got)
	}

	doc["needs_clarification"] = false
	doc["ambiguities"] = []any{map[string]any{"question": "Which hours in the afternoon?", "why": "no hour given"}}
	raw, _ = json.Marshal(doc)
	if got, err = ai.ParseInterpretation(raw, dateIDs); err != nil {
		t.Fatalf("ParseInterpretation: %v", err)
	}
	if !got.NeedsClarification || len(got.Ambiguities) != 1 || got.Ambiguities[0].Question != "Which hours in the afternoon?" {
		t.Errorf("ambiguities without needs_clarification = %+v, want needs_clarification true and the ambiguity kept", got)
	}
}
