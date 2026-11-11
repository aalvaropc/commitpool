package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/demo"
	"github.com/aalvaropc/commitpool/internal/domain"
)

type recordingAI struct {
	ai.Fake
	mu       sync.Mutex
	reads    []ai.InterpretInput
	suggests []ai.SuggestInput
	nudge    string
}

func (r *recordingAI) Interpret(ctx context.Context, in ai.InterpretInput) (domain.Interpretation, ai.Meta, error) {
	r.mu.Lock()
	r.reads = append(r.reads, in)
	r.mu.Unlock()
	return r.Fake.Interpret(ctx, in)
}

func (r *recordingAI) Suggest(ctx context.Context, in ai.SuggestInput) (string, ai.Meta, error) {
	r.mu.Lock()
	r.suggests = append(r.suggests, in)
	nudge := r.nudge
	r.mu.Unlock()
	if nudge == "" {
		return r.Fake.Suggest(ctx, in)
	}
	return strings.ReplaceAll(nudge, "{name}", strings.Fields(in.Name + " there")[0]), ai.Meta{Provider: "test", Model: "recording"}, nil
}

func (r *recordingAI) earlier() []*ai.EarlierAnswer {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*ai.EarlierAnswer, len(r.reads))
	for i, in := range r.reads {
		out[i] = in.Earlier
	}
	return out
}

func (r *recordingAI) suggested(kind string) []ai.SuggestInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ai.SuggestInput
	for _, in := range r.suggests {
		if in.Kind == kind {
			out = append(out, in)
		}
	}
	return out
}

func TestAvailability_TheInterpreterSeesTheEarlierAnswerAndItsQuestions(t *testing.T) {
	rec := &recordingAI{}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	grace := e.join(h, "Grace Liu")

	first := e.say(grace, "I can make it either day, ideally in the afternoon.")
	if !first.NeedsClarification || len(first.Interpretation.Ambiguities) != 1 {
		t.Fatalf("first answer = %+v, want a question", first)
	}
	e.say(grace, "Yes, Saturday works")
	e.say(grace, "Sunday too")

	got := rec.earlier()
	if len(got) != 3 {
		t.Fatalf("interpreted %d messages, want 3", len(got))
	}
	if got[0] != nil {
		t.Errorf("the first answer came with an earlier one: %+v", got[0])
	}
	want := &ai.EarlierAnswer{Messages: []string{"I can make it either day, ideally in the afternoon."}, Questions: []string{first.Interpretation.Ambiguities[0].Question}}
	if !reflect.DeepEqual(got[1], want) {
		t.Errorf("the reply came with %+v, want %+v", got[1], want)
	}
	if got[2] != nil {
		t.Errorf("an update after a clear answer came with %+v, want the new message alone", got[2])
	}
}

func TestAvailability_TheInterpreterSeesEveryRoundOfAClarificationChain(t *testing.T) {
	rec := &recordingAI{}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	grace := e.join(h, "Grace Liu")

	first := "I can make it either day, ideally in the afternoon."
	second := "Maybe an evening, I am not sure yet."
	third := "Late afternoon would be fine."
	e.say(grace, first)
	secondReading := e.say(grace, second)
	if !secondReading.NeedsClarification {
		t.Fatalf("second answer = %+v, want another question", secondReading)
	}
	e.say(grace, third)
	e.say(grace, "Saturday works")

	got := rec.earlier()
	if len(got) != 4 {
		t.Fatalf("interpreted %d messages, want 4", len(got))
	}
	if got[0] != nil {
		t.Errorf("the first answer came with an earlier one: %+v", got[0])
	}
	if want := []string{first}; !reflect.DeepEqual(got[1].Messages, want) {
		t.Errorf("the second round came with %q, want %q", got[1].Messages, want)
	}
	wantThird := &ai.EarlierAnswer{Messages: []string{first, second}, Questions: []string{secondReading.Interpretation.Ambiguities[0].Question}}
	if !reflect.DeepEqual(got[2], wantThird) {
		t.Errorf("the third round came with %+v, want %+v", got[2], wantThird)
	}
	if want := []string{first, second, third}; !reflect.DeepEqual(got[3].Messages, want) {
		t.Errorf("the fourth message came with %q, want the whole chain %q", got[3].Messages, want)
	}
}

func TestAvailability_AClarificationChainKeepsAtMostFourMessages(t *testing.T) {
	rec := &recordingAI{}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	grace := e.join(h, "Grace Liu")
	var sent []string
	for i := range 7 {
		message := fmt.Sprintf("Round %d: maybe an afternoon.", i)
		sent = append(sent, message)
		e.say(grace, message)
	}
	got := rec.earlier()
	if want := sent[2:6]; !reflect.DeepEqual(got[6].Messages, want) {
		t.Errorf("the seventh round came with %q, want the four latest earlier messages %q", got[6].Messages, want)
	}
}

func TestAvailability_AClarificationChainKeepsAtMostTwoThousandCharacters(t *testing.T) {
	rec := &recordingAI{}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	grace := e.join(h, "Grace Liu")
	padding := strings.Repeat("é", 880)
	var sent []string
	for i := range 4 {
		message := fmt.Sprintf("Round %d: maybe an afternoon. %s", i, padding)
		sent = append(sent, message)
		e.say(grace, message)
	}
	got := rec.earlier()
	if want := sent[1:3]; !reflect.DeepEqual(got[3].Messages, want) {
		t.Errorf("the fourth round came with %d messages, want the two latest that fit in 2000 characters", len(got[3].Messages))
	}
}

func TestAvailability_ACorrectedReadingEndsTheClarificationChain(t *testing.T) {
	rec := &recordingAI{}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	grace := e.join(h, "Grace Liu")
	older := e.say(grace, "I can make it either day, ideally in the afternoon.")
	corrected := older.Interpretation
	corrected.NeedsClarification, corrected.Ambiguities = true, []domain.Ambiguity{{Question: "Which day?", Why: "It decides the date."}}
	e.call(http.MethodPut, grace.path+"/availability/"+older.ID, map[string]any{"corrected": corrected}, http.StatusOK, nil)
	newer := "Maybe an evening, I am not sure yet."
	e.say(grace, newer)
	e.say(grace, "Saturday works")
	got := rec.earlier()
	if len(got) != 3 || got[2] == nil || !reflect.DeepEqual(got[2].Messages, []string{newer}) {
		t.Errorf("the reply came with %+v, want only the newer message %q after the corrected one", got, newer)
	}
}

func TestAvailability_ACorrectedReadingIsNotReopened(t *testing.T) {
	rec := &recordingAI{}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	grace := e.join(h, "Grace Liu")
	first := e.say(grace, "I can make it either day, ideally in the afternoon.")
	corrected := first.Interpretation
	corrected.NeedsClarification, corrected.Ambiguities = false, []domain.Ambiguity{}
	for i := range corrected.PerDate {
		corrected.PerDate[i].Verdict, corrected.PerDate[i].Condition = domain.VerdictAvailable, domain.Condition{Type: domain.ConditionNone}
	}
	e.call(http.MethodPut, grace.path+"/availability/"+first.ID, map[string]any{"corrected": corrected}, http.StatusOK, nil)
	e.say(grace, "Sunday too")
	if got := rec.earlier(); len(got) != 2 || got[1] != nil {
		t.Errorf("after the person corrected the reading, the next message came with %+v, want it alone", got)
	}
}

func TestAvailability_AnUnreadableEarlierAnswerDoesNotStopTheNewOne(t *testing.T) {
	rec := &recordingAI{}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	grace := e.join(h, "Grace Liu")
	first := e.say(grace, "I can make it either day, ideally in the afternoon.")
	if _, err := e.st.Pool.Exec(e.ctx, `update availabilities set interpretation = '"broken"'::jsonb where id = $1`, first.ID); err != nil {
		t.Fatal(err)
	}
	second := e.say(grace, "Saturday works")
	if got := rec.earlier(); len(got) != 2 || got[1] != nil || second.ID == "" {
		t.Errorf("after an unreadable earlier reading: earlier %+v, saved %+v; want the new message read alone and saved", got, second)
	}
	if logs := e.logs.String(); !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, "earlier answer") {
		t.Errorf("log does not warn about the earlier answer:\n%s", logs)
	}
}

func TestSuggestions_ClaudeMayNameEveryDateWhenItAsksAQuestion(t *testing.T) {
	rec := &recordingAI{nudge: "Hi {name}! The class runs on Saturday or on Sunday. Can you make either one?"}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "I can't do Sunday, but Saturday is fine")
	e.joinAndSay(h, "Grace Liu", "I can make it either day, ideally in the afternoon.")
	v := e.settledOwner(h)
	if len(v.Suggestions) != 1 || v.Suggestions[0].Kind != "clarify" {
		t.Fatalf("suggestions = %+v, want one question for Grace", v.Suggestions)
	}
	if got := v.Suggestions[0].Message; got != "Hi Grace! The class runs on Saturday or on Sunday. Can you make either one?" {
		t.Errorf("clarify message = %q, want Claude's words, which name both dates", got)
	}
	clarify := rec.suggested("clarify")
	if len(clarify) == 0 || clarify[0].Options != "Sat, Nov 14, 3 PM–6 PM or Sun, Nov 15, 10 AM–1 PM" {
		t.Errorf("clarify facts = %+v, want both dates with their hours as the options", clarify)
	}
}

func TestSuggestions_AQuestionNamingADayThatIsNotADateFallsBack(t *testing.T) {
	rec := &recordingAI{nudge: "Hi {name}! Could you come on Monday instead, or on Nov 20?"}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "I can't do Sunday, but Saturday is fine")
	e.joinAndSay(h, "Grace Liu", "I can make it either day, ideally in the afternoon.")
	v := e.settledOwner(h)
	if len(v.Suggestions) != 1 || v.Suggestions[0].Kind != "clarify" {
		t.Fatalf("suggestions = %+v, want one question for Grace", v.Suggestions)
	}
	if got := v.Suggestions[0].Message; strings.Contains(got, "Monday") || got != ai.PlainSuggestion(rec.suggested("clarify")[0]) {
		t.Errorf("clarify message = %q, want the plain message instead of one naming a day that is not a candidate", got)
	}
}

func TestSuggestions_OtherNudgesStillMayNotNameAnotherDate(t *testing.T) {
	other := &recordingAI{nudge: "Hi {name}! The class runs on Saturday or on Sunday. Can you make either one?"}
	e := newEnv(t, withAI(other))
	h := e.createWorkshop()
	e.joinAndSay(h, "Ana", "I can't do Sunday, but Saturday is fine")
	e.joinAndSay(h, "Bo", "Saturday works if we finish before five")
	v := e.settledOwner(h)
	if len(v.Suggestions) != 1 || v.Suggestions[0].Kind != "condition" {
		t.Fatalf("suggestions = %+v, want one condition nudge for Bo", v.Suggestions)
	}
	if got := v.Suggestions[0].Message; strings.Contains(got, "Sunday") || !strings.HasPrefix(got, "Hi Bo!") {
		t.Errorf("condition message = %q, want the plain message instead of one naming another date", got)
	}
	for _, in := range other.suggested("condition") {
		if in.Options != "" {
			t.Errorf("condition facts carry the options %q; only a question needs them", in.Options)
		}
	}
}

func TestDemo_GracesAnswerToClaudeOpensSaturday(t *testing.T) {
	e := newEnv(t, withDemoMode())
	d, ownerPath := e.startDemo()
	h := workshopHandle{ownerPath: ownerPath}

	v := e.settledOwner(h)
	sat, sun := v.Dates[0].ID, v.Dates[1].ID
	compatible := func(v ownerViewJSON) map[string]int {
		out := map[string]int{}
		for _, s := range v.Recommendation.PerDate {
			out[s.DateID] = len(s.Compatible)
		}
		return out
	}
	if got := compatible(v); v.Recommendation.DateID != sat || v.Recommendation.MeetsMinimum || got[sat] != 2 || got[sun] != 2 {
		t.Fatalf("before Grace answers: recommendation %s meets %v with %v, want Saturday with 2 and Sunday with 2", v.Recommendation.DateID, v.Recommendation.MeetsMinimum, got)
	}
	kinds := map[string]suggestionJSON{}
	for _, sg := range v.Suggestions {
		kinds[sg.Kind+":"+sg.Name] = sg
	}
	clarify, ok := kinds["clarify:Grace Liu"]
	if !ok || !strings.Contains(clarify.Message, "10 AM–12 PM") || !strings.HasPrefix(clarify.Message, "Hi Grace!") {
		t.Errorf("clarify nudge = %+v, want one for Grace tied to the class hours", clarify)
	}
	if _, ok := kinds["condition:Hannah Kim"]; !ok {
		t.Errorf("suggestions = %+v, want a nudge for Hannah, whose 11:30 limit misses the class", v.Suggestions)
	}
	refused := e.wantError(http.MethodPost, h.ownerPath+"/proposal", map[string]string{"date_id": sat}, http.StatusConflict, "proposal_invalid")
	if refused.Reason != "not_enough_compatible" {
		t.Errorf("opening Saturday at 2 of 3: reason %q, want not_enough_compatible", refused.Reason)
	}

	var grace string
	for _, p := range d.Participants {
		if p.Name == "Grace Liu" {
			grace = "/api/v1/p/" + strings.TrimPrefix(p.ParticipantURL, appBaseURL+"/p/")
		}
	}
	reply := e.say(participantHandle{path: grace}, demo.LifeDrawing.Replies[0].Message)
	if reply.NeedsClarification {
		t.Fatalf("Grace's answer still needs clarifying: %+v", reply)
	}
	v = e.settledOwner(h)
	if got := compatible(v); v.Recommendation.DateID != sat || !v.Recommendation.MeetsMinimum || got[sat] != 3 {
		t.Fatalf("after Grace answers: recommendation %s meets %v with %v, want Saturday with 3", v.Recommendation.DateID, v.Recommendation.MeetsMinimum, got)
	}
	e.openProposal(h, sat)
}
