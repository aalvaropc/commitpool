package ai_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
)

var dates = []ai.DateOption{
	{ID: "sat", Label: "Sat 14 Nov 2026, 15:00–18:00 America/Lima"},
	{ID: "sun", Label: "Sun 15 Nov 2026, 10:00–13:00 America/Lima"},
}

func interpret(t *testing.T, message string) domain.Interpretation {
	t.Helper()
	in, meta, err := ai.Fake{}.Interpret(context.Background(), ai.InterpretInput{
		WorkshopTitle: "Go for operators", Timezone: "America/Lima", Dates: dates, Message: message,
	})
	if err != nil {
		t.Fatalf("Interpret(%q): %v", message, err)
	}
	if meta.Provider != "fake" || meta.Model == "" || meta.Fallback {
		t.Errorf("Interpret(%q) meta = %+v, want the fake provider without fallback", message, meta)
	}
	return in
}

func verdictFor(t *testing.T, in domain.Interpretation, id string) domain.DateVerdict {
	t.Helper()
	for _, v := range in.PerDate {
		if v.CandidateDateID == id {
			return v
		}
	}
	t.Fatalf("no verdict for date %s in %+v", id, in.PerDate)
	return domain.DateVerdict{}
}

func TestFake_InterpretsKeywordsDeterministically(t *testing.T) {
	const message = "Saturday works if we finish before six"
	first := interpret(t, message)
	sat := verdictFor(t, first, "sat")
	if sat.Verdict != domain.VerdictConditional || sat.Condition != (domain.Condition{Type: domain.ConditionEndBefore, Time: "18:00"}) {
		t.Errorf("Saturday = %+v, want conditional end_before 18:00", sat)
	}
	if sat.Evidence == "" {
		t.Errorf("Saturday verdict has no evidence")
	}
	if first.NeedsClarification {
		t.Errorf("%q needs clarification, want a clear reading", message)
	}
	if again := interpret(t, message); !reflect.DeepEqual(again, first) {
		t.Errorf("second reading differs:\n%+v\nwant\n%+v", again, first)
	}

	vague := interpret(t, "afternoon")
	if !vague.NeedsClarification || len(vague.Ambiguities) == 0 {
		t.Errorf("%q = %+v, want needs_clarification with a question", "afternoon", vague)
	}
}

func TestFake_KeywordRules(t *testing.T) {
	cases := []struct {
		message  string
		sat, sun domain.DateVerdict
		unclear  bool
	}{
		{
			message: "Any day works for me",
			sat:     domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
			sun:     domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
		},
		{
			message: "I can't do Sunday, but Saturday is fine",
			sat:     domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
			sun:     domain.DateVerdict{Verdict: domain.VerdictUnavailable, Condition: domain.Condition{Type: domain.ConditionNone}},
		},
		{
			message: "No Saturdays please",
			sat:     domain.DateVerdict{Verdict: domain.VerdictUnavailable, Condition: domain.Condition{Type: domain.ConditionNone}},
			sun:     domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
		},
		{
			message: "Sunday after 9:30am",
			sat:     domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
			sun:     domain.DateVerdict{Verdict: domain.VerdictConditional, Condition: domain.Condition{Type: domain.ConditionStartAfter, Time: "09:30"}},
		},
		{
			message: "Either day, as long as I am home by 17",
			sat:     domain.DateVerdict{Verdict: domain.VerdictConditional, Condition: domain.Condition{Type: domain.ConditionEndBefore, Time: "17:00"}},
			sun:     domain.DateVerdict{Verdict: domain.VerdictConditional, Condition: domain.Condition{Type: domain.ConditionEndBefore, Time: "17:00"}},
		},
		{
			message: "Monday works if we finish before six",
			sat:     domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
			sun:     domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
		},
		{
			message: "Saturday evening maybe",
			sat:     domain.DateVerdict{Verdict: domain.VerdictUnknown, Condition: domain.Condition{Type: domain.ConditionNone}},
			sun:     domain.DateVerdict{Verdict: domain.VerdictAvailable, Condition: domain.Condition{Type: domain.ConditionNone}},
			unclear: true,
		},
	}
	for _, c := range cases {
		t.Run(c.message, func(t *testing.T) {
			in := interpret(t, c.message)
			for id, want := range map[string]domain.DateVerdict{"sat": c.sat, "sun": c.sun} {
				got := verdictFor(t, in, id)
				if got.Verdict != want.Verdict || got.Condition != want.Condition {
					t.Errorf("%s = %s %+v, want %s %+v", id, got.Verdict, got.Condition, want.Verdict, want.Condition)
				}
			}
			if in.NeedsClarification != c.unclear {
				t.Errorf("needs_clarification = %v, want %v", in.NeedsClarification, c.unclear)
			}
			if in.HardConstraints == nil || in.Preferences == nil || in.Ambiguities == nil {
				t.Errorf("nil list in %+v: every list must encode as []", in)
			}
		})
	}
}

func TestFake_ExplainNamesTheDateAndPeople(t *testing.T) {
	text, meta, err := ai.Fake{}.Explain(context.Background(), ai.ExplainInput{
		WorkshopTitle: "Go for operators",
		Dates:         dates,
		Recommendation: domain.Recommendation{
			DateID:       "sat",
			MeetsMinimum: true,
			PerDate: []domain.DateSummary{
				{DateID: "sat", Compatible: []string{"p1", "p2"}},
				{DateID: "sun", Compatible: []string{"p1"}},
			},
		},
		Names:   map[string]string{"p1": "Ana", "p2": "Luis"},
		Minimum: 2,
	})
	if err != nil || meta.Provider != "fake" {
		t.Fatalf("Explain = %q, %+v, %v", text, meta, err)
	}
	for _, want := range []string{dates[0].Label, "Ana", "Luis"} {
		if !strings.Contains(text, want) {
			t.Errorf("explanation %q does not mention %q", text, want)
		}
	}
}
