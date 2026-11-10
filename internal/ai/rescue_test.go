package ai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
)

func rescue() ai.SuggestInput {
	in := nudge("rescue")
	in.Name, in.Question, in.Condition, in.Options = "", "", "", ""
	in.Deadline, in.Earlier, in.Missing = "Sat, Nov 14 at 6 AM", "Fri, Nov 13 at 10 AM", 1
	return in
}

func TestPlainSuggestion_RescueTellsTheGroupTheNewDeadline(t *testing.T) {
	got := ai.PlainSuggestion(rescue())
	for _, want := range []string{"Life drawing for beginners on Sat, Nov 14", "until Sat, Nov 14 at 6 AM", "1 more person", "nobody pays", "pass it on"} {
		if !strings.Contains(got, want) {
			t.Errorf("plain rescue %q lacks %q", got, want)
		}
	}
	if strings.HasPrefix(got, "Hi") {
		t.Errorf("a group message greets one person: %q", got)
	}
}

func TestSuggestFacts_RescueGivesBothDeadlinesAndNoPerson(t *testing.T) {
	facts := ai.SuggestFacts(rescue())
	for _, want := range []string{"Kind of message: rescue", `Reservations close: "Sat, Nov 14 at 6 AM"`, `Earlier deadline, before the organizer extended it: "Fri, Nov 13 at 10 AM"`, "More people needed: 1"} {
		if !strings.Contains(facts, want) {
			t.Errorf("rescue facts lack %q:\n%s", want, facts)
		}
	}
	if strings.Contains(facts, "Person:") {
		t.Errorf("rescue facts name a person:\n%s", facts)
	}
}

func TestSuggestPrompt_HasARescueRule(t *testing.T) {
	m := newFakeBedrock(t, message(testModel, "Life drawing for beginners has more time: reserve by Sat, Nov 14 at 6 AM.", "end_turn"))
	if _, _, err := newTestBedrock(t, m, nil).Suggest(context.Background(), rescue()); err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	system := toString(m.recorded()[0].body["system"])
	for _, want := range []string{"rescue:", "extended the deadline, once", "Reservations close"} {
		if !strings.Contains(system, want) {
			t.Errorf("suggest system prompt does not contain %q", want)
		}
	}
}
