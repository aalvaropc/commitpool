package httpapi_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aalvaropc/commitpool/internal/ai"
)

const (
	saturdayOnly = "Saturday works for me, Sunday I can't."
	sundayOnly   = "Sunday works for me, Saturday I can't."
	vague        = "I can make it either day, ideally in the afternoon."
)

func personal(list []suggestionJSON) []suggestionJSON {
	return slices.DeleteFunc(slices.Clone(list), func(sg suggestionJSON) bool { return sg.Kind == "group_reply" })
}

func groupReplyOf(t *testing.T, v ownerViewJSON) suggestionJSON {
	t.Helper()
	if len(v.Suggestions) == 0 || v.Suggestions[0].Kind != "group_reply" {
		t.Fatalf("suggestions = %+v, want the group reply first", v.Suggestions)
	}
	return v.Suggestions[0]
}

func TestGroupReply_ComesFirstWithTheFactsTheServerDecided(t *testing.T) {
	rec := &recordingAI{}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	e.joinAndSay(h, "Priya Raman", saturdayOnly)
	e.joinAndSay(h, "Grace Liu", vague)
	v := e.settledOwner(h)

	reply := groupReplyOf(t, v)
	if reply.Name != "" || reply.ParticipantID != "" || reply.Reason != "2 people have answered. Tell the group where things stand." {
		t.Errorf("group reply = %+v", reply)
	}
	question := rec.suggested("clarify")[0].Question
	for _, want := range []string{"Go for operators", "Sat, Nov 14 works for 1 of you so far", "1 more person", "Grace: " + question} {
		if !strings.Contains(reply.Message, want) {
			t.Errorf("group reply %q lacks %q", reply.Message, want)
		}
	}
	if !strings.HasSuffix(reply.Message, "\n"+h.created.ShareURL) {
		t.Errorf("group reply %q does not end with the invite link", reply.Message)
	}
	if rest := personal(v.Suggestions); len(rest) != 1 || rest[0].Kind != "clarify" || rest[0].Name != "Grace Liu" {
		t.Errorf("personal suggestions = %+v, want Grace's own question too", rest)
	}

	facts := rec.suggested("group_reply")
	if len(facts) == 0 {
		t.Fatal("Claude was not asked for the group reply")
	}
	got := facts[len(facts)-1]
	if got.Date != "Sat, Nov 14" || got.CanCome != 1 || got.Needed != 2 || got.Missing != 1 || len(got.Tied) != 0 {
		t.Errorf("group reply facts = %+v, want Saturday with 1 of the 2 needed", got)
	}
	if want := []ai.NamedQuestion{{Name: "Grace Liu", Question: question}}; !slices.Equal(got.Questions, want) {
		t.Errorf("group reply questions = %+v, want %+v", got.Questions, want)
	}
}

func TestGroupReply_TiedDatesComeFirstAndNeitherIsRecommended(t *testing.T) {
	rec := &recordingAI{}
	e := newEnv(t, withAI(rec))
	h := e.createWorkshop()
	e.joinAndSay(h, "Priya Raman", saturdayOnly)
	e.joinAndSay(h, "Tomás Silva", sundayOnly)
	reply := groupReplyOf(t, e.settledOwner(h))
	for _, want := range []string{"Sat, Nov 14 and Sun, Nov 15 are tied", "1 of you", "Sat, Nov 14 comes first"} {
		if !strings.Contains(reply.Message, want) {
			t.Errorf("tied group reply %q lacks %q", reply.Message, want)
		}
	}
	if strings.Contains(strings.ToLower(reply.Message), "recommend") {
		t.Errorf("tied group reply %q calls a date recommended", reply.Message)
	}
	facts := rec.suggested("group_reply")
	if got := facts[len(facts)-1].Tied; !slices.Equal(got, []string{"Sat, Nov 14", "Sun, Nov 15"}) {
		t.Errorf("tied dates in the facts = %v", got)
	}
}

func TestGroupReply_OnlyWithTwoAnswersAndBeforeReservationsOpen(t *testing.T) {
	e := newEnv(t)
	h := e.createWorkshop()
	e.joinAndSay(h, "Grace Liu", vague)
	for _, sg := range e.settledOwner(h).Suggestions {
		if sg.Kind == "group_reply" {
			t.Errorf("one answer: %+v, want no group reply yet", sg)
		}
	}
	funding, _, _ := e.fundingWorkshop()
	for _, sg := range e.settledOwner(funding).Suggestions {
		if sg.Kind == "group_reply" {
			t.Errorf("reservations open: %+v, want no group reply", sg)
		}
	}
}

type groupReplyAI struct {
	ai.Fake
	mu    sync.Mutex
	write func(ai.SuggestInput) string
}

func (g *groupReplyAI) Suggest(ctx context.Context, in ai.SuggestInput) (string, ai.Meta, error) {
	g.mu.Lock()
	write := g.write
	g.mu.Unlock()
	if in.Kind != "group_reply" || write == nil {
		return g.Fake.Suggest(ctx, in)
	}
	return write(in), ai.Meta{Provider: "test", Model: "group-reply"}, nil
}

func TestGroupReply_FilterRejectsOtherDate(t *testing.T) {
	cases := []struct {
		name  string
		write func(ai.SuggestInput) string
		kept  bool
	}{
		{"another candidate date", func(ai.SuggestInput) string {
			return "Go for operators: Sun, Nov 15 works for 1 of you so far. Anyone else, say when you can come."
		}, false},
		{"a day that is not a candidate", func(ai.SuggestInput) string {
			return "Go for operators: Sat, Nov 14 works for 1 of you so far. Could we try Monday instead?"
		}, false},
		{"only the date the server gave", func(in ai.SuggestInput) string {
			return "Go for operators: thanks, all! Sat, Nov 14 works for 1 of you so far; we need 1 more. Anyone else, say when you can come."
		}, true},
		{"another date inside the question, exactly as given", func(in ai.SuggestInput) string {
			return "Go for operators: Sat, Nov 14 works for 1 of you so far. Grace: " + in.Questions[0].Question + " Anyone else, say when you can come."
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model := &groupReplyAI{write: c.write}
			e := newEnv(t, withAI(model))
			h := e.createWorkshop()
			e.joinAndSay(h, "Priya Raman", saturdayOnly)
			e.joinAndSay(h, "Grace Liu", vague)
			reply := groupReplyOf(t, e.settledOwner(h))
			claude := strings.HasPrefix(reply.Message, "Go for operators: Sat, Nov 14") || strings.HasPrefix(reply.Message, "Go for operators: Sun") || strings.HasPrefix(reply.Message, "Go for operators: thanks, all!")
			if claude != c.kept {
				t.Errorf("group reply = %q; Claude's words kept = %v, want %v", reply.Message, claude, c.kept)
			}
			if !c.kept && !strings.Contains(reply.Message, "Sat, Nov 14 works for 1 of you so far") {
				t.Errorf("group reply = %q, want the plain message", reply.Message)
			}
			if !strings.HasSuffix(reply.Message, "\n"+h.created.ShareURL) {
				t.Errorf("group reply %q does not end with the invite link", reply.Message)
			}
		})
	}
}
