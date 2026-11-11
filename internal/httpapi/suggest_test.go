package httpapi_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aalvaropc/commitpool/internal/ai"
	"github.com/aalvaropc/commitpool/internal/domain"
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
	for _, want := range []string{"Sat, Nov 14, 3 PM–6 PM and Sun, Nov 15, 10 AM–1 PM are tied", "1 of you", "Sat, Nov 14, 3 PM–6 PM comes first"} {
		if !strings.Contains(reply.Message, want) {
			t.Errorf("tied group reply %q lacks %q", reply.Message, want)
		}
	}
	if strings.Contains(strings.ToLower(reply.Message), "recommend") {
		t.Errorf("tied group reply %q calls a date recommended", reply.Message)
	}
	facts := rec.suggested("group_reply")
	got := facts[len(facts)-1]
	if !slices.Equal(got.Tied, []string{"Sat, Nov 14, 3 PM–6 PM", "Sun, Nov 15, 10 AM–1 PM"}) || got.Hours != "" {
		t.Errorf("tied facts = %v with hours %q, want each date with its own hours and no single hours", got.Tied, got.Hours)
	}
}

func onDates(starts ...time.Time) func(map[string]any) {
	return func(body map[string]any) {
		var dates []map[string]string
		for _, at := range starts {
			dates = append(dates, map[string]string{"starts_at": at.Format(time.RFC3339), "ends_at": at.Add(3 * time.Hour).Format(time.RFC3339)})
		}
		body["dates"] = dates
	}
}

func TestGroupReply_TiedSlotsKeepTheirOwnHours(t *testing.T) {
	cases := []struct {
		name      string
		starts    []time.Time
		labels    []string
		hours     string
		mentioned string
	}{
		{"two slots on one day", []time.Time{satStart, satStart.Add(4 * time.Hour)}, []string{"Sat, Nov 14, 3 PM–6 PM", "Sat, Nov 14, 7 PM–10 PM"}, "", "Sat, Nov 14, 3 PM–6 PM and Sat, Nov 14, 7 PM–10 PM are tied"},
		{"two days at the same hours", []time.Time{satStart, satStart.Add(24 * time.Hour)}, []string{"Sat, Nov 14", "Sun, Nov 15"}, "3 PM–6 PM", "Sat, Nov 14 and Sun, Nov 15 are tied"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &recordingAI{}
			e := newEnv(t, withAI(rec))
			h := e.createWorkshop(onDates(c.starts...))
			e.joinAndSay(h, "Ada", "Any of the dates works for me.")
			e.joinAndSay(h, "Ben", "Any of the dates works for me.")
			reply := groupReplyOf(t, e.settledOwner(h))
			if !strings.Contains(reply.Message, c.mentioned) || strings.Contains(reply.Message, "Sat, Nov 14 and Sat, Nov 14") {
				t.Errorf("group reply = %q, want %q", reply.Message, c.mentioned)
			}
			facts := rec.suggested("group_reply")
			if got := facts[len(facts)-1]; !slices.Equal(got.Tied, c.labels) || got.Hours != c.hours {
				t.Errorf("tied facts = %v with hours %q, want %v with %q", got.Tied, got.Hours, c.labels, c.hours)
			}
		})
	}
}

func (e *env) joinWithOwnQuestion(h workshopHandle, name, question string) {
	e.t.Helper()
	p := e.join(h, name)
	first := e.say(p, "Any of the dates works for me.")
	corrected := first.Interpretation
	corrected.NeedsClarification = true
	corrected.Ambiguities = []domain.Ambiguity{{Question: question, Why: "I wrote it myself."}}
	e.call(http.MethodPut, p.path+"/availability/"+first.ID, map[string]any{"corrected": corrected}, http.StatusOK, nil)
}

func TestGroupReply_ACorrectionCannotWriteTheGroupQuestion(t *testing.T) {
	cases := []struct {
		name, question string
		kept           bool
	}{
		{"money and a date that is not a candidate", "Good news: the class is now free ($0) and moves to Mon, Nov 30. Can you come then?", false},
		{"a date that is not a candidate", "The class moves to Mon, Nov 30. Can you come then?", false},
		{"a link", "Can you come? Details at www.example.org", false},
		{"a phone number", "Text me at +1 718 555 0199 to confirm?", false},
		{"a refund", "Can you come if everyone gets a refund?", false},
		{"a short phone number", "Call me at 555-0199 to say which day?", false},
		{"a short link", "See goo.gl/abc and say which day?", false},
		{"only invisible characters", "\u200b\u200e\ufeff", false},
		{"a space", " ", false},
		{"a newline", "\n", false},
		{"nothing", "", false},
		{"a fair question", "Can you make Saturday or Sunday, and which?", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			h := e.createWorkshop()
			e.joinAndSay(h, "Priya Raman", saturdayOnly)
			e.joinWithOwnQuestion(h, "Mallory", c.question)
			reply := groupReplyOf(t, e.settledOwner(h))
			if !strings.Contains(reply.Message, "Sat, Nov 14 works for 1 of you so far") {
				t.Errorf("group reply = %q, want the server's date and count", reply.Message)
			}
			if c.kept {
				if !strings.Contains(reply.Message, "Mallory: "+c.question) {
					t.Errorf("group reply = %q, want Mallory's fair question", reply.Message)
				}
				return
			}
			text, linked := strings.CutSuffix(reply.Message, "\n"+h.created.ShareURL)
			for _, banned := range []string{"Mallory", "$0", "free", "Nov 30", "Mon", "example.org", "555", "refund", "http", "goo.gl"} {
				if strings.Contains(text, banned) || !linked {
					t.Errorf("group reply %q carries %q from Mallory's own question", reply.Message, banned)
				}
			}
		})
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
