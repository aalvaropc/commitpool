package ai_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
)

var lima = func() *time.Location {
	loc, err := time.LoadLocation("America/Lima")
	if err != nil {
		panic(err)
	}
	return loc
}()

var dates = []ai.DateOption{
	{ID: "sat", Label: "Sat, Nov 14, 3 PM–6 PM (Lima time)", StartsAt: time.Date(2026, 11, 14, 15, 0, 0, 0, lima)},
	{ID: "sun", Label: "Sun, Nov 15, 10 AM–1 PM (Lima time)", StartsAt: time.Date(2026, 11, 15, 10, 0, 0, 0, lima)},
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
	for _, want := range []string{"Sat, Nov 14", "Ana", "Luis"} {
		if !strings.Contains(text, want) {
			t.Errorf("explanation %q does not mention %q", text, want)
		}
	}
}

func TestFake_AsksWhetherThePersonCanMakeTheClassHours(t *testing.T) {
	vague := interpret(t, "Saturday evening maybe")
	if len(vague.Ambiguities) != 1 {
		t.Fatalf("ambiguities = %+v, want one question", vague.Ambiguities)
	}
	q := vague.Ambiguities[0].Question
	for _, want := range []string{dates[0].Label, dates[1].Label, "Can you make it"} {
		if !strings.Contains(q, want) {
			t.Errorf("question %q does not contain %q", q, want)
		}
	}
	if strings.Contains(q, "Which hours") {
		t.Errorf("question %q asks for another time; the class hours are fixed", q)
	}
}

const (
	scriptedVague = "Count me in if it's an afternoon thing."
	scriptedReply = "No problem — I swapped Saturday."
)

func scriptedFake() ai.Fake {
	none := domain.Condition{Type: domain.ConditionNone}
	return ai.Fake{Script: map[string]ai.Scripted{
		scriptedVague: {
			Verdicts:        []domain.DateVerdict{{Verdict: domain.VerdictUnknown, Evidence: "if it's an afternoon thing"}, {Verdict: domain.VerdictUnknown}},
			Question:        "The class runs 3 PM–6 PM on Saturday and 10 AM–1 PM on Sunday. Can you make either?",
			Why:             "An afternoon wish against fixed hours.",
			HardConstraints: []string{"afternoons only"},
		},
		scriptedReply: {
			Verdicts: []domain.DateVerdict{{Verdict: domain.VerdictAvailable, Condition: none, Evidence: "I swapped Saturday"}, {Verdict: domain.VerdictUnavailable}},
			Follows:  scriptedVague,
		},
	}}
}

func TestFake_PlaysTheScriptForTheExactMessage(t *testing.T) {
	f := scriptedFake()
	in, meta, err := f.Interpret(context.Background(), ai.InterpretInput{Dates: dates, Message: "  Count me in if it’s an afternoon thing.\n"})
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	if meta.Provider != "fake" || !strings.Contains(meta.Model, "script") {
		t.Errorf("meta = %+v, want the fake provider naming its script", meta)
	}
	if !in.NeedsClarification || len(in.Ambiguities) != 1 || in.Ambiguities[0].Question != f.Script[scriptedVague].Question || in.Ambiguities[0].Why == "" {
		t.Errorf("reading = %+v, want the scripted question", in)
	}
	for i, v := range in.PerDate {
		if v.CandidateDateID != dates[i].ID || v.Verdict != domain.VerdictUnknown || v.Condition.Type != domain.ConditionNone {
			t.Errorf("per_date[%d] = %+v, want unknown for %s with no condition", i, v, dates[i].ID)
		}
	}
	if !reflect.DeepEqual(in.HardConstraints, []string{"afternoons only"}) || in.Preferences == nil {
		t.Errorf("constraints = %v, preferences = %v", in.HardConstraints, in.Preferences)
	}
	raw, _ := json.Marshal(in)
	if _, err := ai.ParseInterpretation(raw, dateIDs); err != nil {
		t.Errorf("the scripted reading does not match the schema: %v", err)
	}
}

func TestFake_ScriptedReplyNeedsItsEarlierMessage(t *testing.T) {
	f := scriptedFake()
	read := func(earlier *ai.EarlierAnswer) domain.Interpretation {
		in, _, err := f.Interpret(context.Background(), ai.InterpretInput{Dates: dates, Message: scriptedReply, Earlier: earlier})
		if err != nil {
			t.Fatalf("Interpret: %v", err)
		}
		return in
	}
	withContext := read(&ai.EarlierAnswer{Message: "Count me in if it’s an afternoon thing.", Questions: []string{"Can you make either?"}})
	if sat, sun := verdictFor(t, withContext, "sat"), verdictFor(t, withContext, "sun"); sat.Verdict != domain.VerdictAvailable || sun.Verdict != domain.VerdictUnavailable || withContext.NeedsClarification {
		t.Errorf("reply with its earlier message = %+v, want Saturday yes, Sunday no", withContext)
	}
	alone := read(nil)
	if verdictFor(t, alone, "sat").Verdict != domain.VerdictUnavailable {
		t.Errorf("reply without its earlier message = %+v, want the keyword reading of a bare %q", alone, scriptedReply)
	}
	other := read(&ai.EarlierAnswer{Message: "Something else entirely"})
	if reflect.DeepEqual(other, withContext) {
		t.Errorf("reply after another message played the script: %+v", other)
	}
}

func TestFake_FallsBackToKeywordsOutsideTheScript(t *testing.T) {
	f := scriptedFake()
	got, meta, err := f.Interpret(context.Background(), ai.InterpretInput{Dates: dates, Message: "Saturday works if we finish before six"})
	if err != nil {
		t.Fatalf("Interpret: %v", err)
	}
	want := interpret(t, "Saturday works if we finish before six")
	if !reflect.DeepEqual(got, want) || meta.Model != "keywords-v1" {
		t.Errorf("unscripted message = %+v (%+v), want the keyword reading %+v", got, meta, want)
	}
}

func TestFake_ScriptMatchesWhateverDashTheReplyUses(t *testing.T) {
	f := scriptedFake()
	earlier := &ai.EarlierAnswer{Message: "Count me in if it's an afternoon thing."}
	for _, reply := range []string{"No problem - I swapped Saturday.", "No problem -- I swapped Saturday.", "No problem–I swapped Saturday.", "No problem — I swapped Saturday."} {
		in, _, err := f.Interpret(context.Background(), ai.InterpretInput{Dates: dates, Message: reply, Earlier: earlier})
		if err != nil {
			t.Fatalf("Interpret(%q): %v", reply, err)
		}
		if verdictFor(t, in, "sat").Verdict != domain.VerdictAvailable || verdictFor(t, in, "sun").Verdict != domain.VerdictUnavailable {
			t.Errorf("%q = %+v, want the scripted reply", reply, in.PerDate)
		}
	}
}
